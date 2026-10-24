package shadow

import (
	"sort"
	"time"

	"teamcross/internal/spacecontext"
)

const TaskResultHistoryLimit = 256

// TaskResult is an immutable business result, independent of the current job
// and the foreground turn that later explains it. Native final text alone is
// still needs_followup; only the task's checked outcome can say completed.
type TaskResult struct {
	ID                 string                   `json:"id"`
	TaskID             string                   `json:"taskId"`
	TaskVersion        int                      `json:"taskVersion"`
	Title              string                   `json:"title"`
	SourceID           string                   `json:"sourceId"`
	MessageID          string                   `json:"messageId,omitempty"`
	ParentID           string                   `json:"parentId,omitempty"`
	State              string                   `json:"state"`
	Outcome            string                   `json:"outcome"`
	Text               string                   `json:"text,omitempty"`
	CompletionCriteria []string                 `json:"completionCriteria,omitempty"`
	CompletionChecks   []string                 `json:"completionChecks,omitempty"`
	Checkpoint         *TaskCheckpoint          `json:"checkpoint,omitempty"`
	Sources            []spacecontext.SourceRef `json:"sources,omitempty"`
	CreatedAt          time.Time                `json:"createdAt"`
}

// Called in the same durable replacement as the job transition. A subsequent
// update/resume may clear the job result, but cannot change this saved version.
func (e *Engine) captureTaskResultsLocked() {
	previous := e.record.TaskResults
	e.record.TaskResults = make(map[string]TaskResult, len(previous))
	for id, result := range previous {
		e.record.TaskResults[id] = result
	}
	for _, job := range e.record.Jobs {
		if job.ResultEventID == "" {
			continue
		}
		switch job.State {
		case "completed", "needs_followup", "failed", "cancelled", "interrupted":
		default:
			continue
		}
		if job.ResultVersion != job.Version && job.State != "cancelled" {
			continue
		}
		if _, exists := e.record.TaskResults[job.ResultEventID]; exists {
			continue
		}
		r := TaskResult{ID: job.ResultEventID, TaskID: job.ID, TaskVersion: job.Version, Title: job.Title, SourceID: job.SourceID, ParentID: job.ParentID, State: job.State, Outcome: job.Outcome, CompletionCriteria: clone(job.CompletionCriteria), CreatedAt: job.UpdatedAt}
		if job.ResultVersion == job.Version {
			r.Text, r.CompletionChecks = job.Result, clone(job.CompletionChecks)
		}
		r.Checkpoint = clone(job.Checkpoint)
		if job.Trigger != nil {
			r.MessageID = job.Trigger.MessageID
			r.Sources = clone(job.Trigger.Sources)
		} else if e.hasMessageLocked(job.SourceID) {
			r.MessageID = job.SourceID
		}
		e.record.TaskResults[r.ID] = r
	}
	if len(e.record.TaskResults) > TaskResultHistoryLimit {
		results := sortedTaskResults(e.record.TaskResults)
		for _, result := range results[:len(results)-TaskResultHistoryLimit] {
			delete(e.record.TaskResults, result.ID)
		}
	}
}

func sortedTaskResults(records map[string]TaskResult) []TaskResult {
	results := make([]TaskResult, 0, len(records))
	for _, result := range records {
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].CreatedAt.Equal(results[j].CreatedAt) {
			return results[i].ID < results[j].ID
		}
		return results[i].CreatedAt.Before(results[j].CreatedAt)
	})
	return results
}
