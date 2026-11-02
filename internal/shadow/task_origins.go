package shadow

import (
	"fmt"
	"sort"
	"strings"

	"teamcross/internal/spacecontext"
)

// Origin is the actual member message or event. Never manufacture a C1 user
// message to satisfy a task API that was originally foreground-only.
func (e *Engine) taskOriginLocked(run Run) (*TaskTrigger, error) {
	switch run.Kind {
	case "main":
		if e.hasMessageLocked(run.SourceID) {
			return &TaskTrigger{Kind: "message", SourceID: run.SourceID, MessageID: run.SourceID}, nil
		}
	case "continuation":
		if e.attention != nil {
			packet, err := e.attention.CurrentContinuation(run.EventID, e.record.ID)
			if err != nil {
				return nil, err
			}
			if packet.Continuation.Mode == "direct" && packet.Continuation.HandlingID == "" {
				for _, input := range runInputs(run) {
					for _, origin := range input.Origins {
						if input.EventID == run.EventID && origin.SourceID == run.SourceID {
							packet.Sources = []spacecontext.SourceRef{origin.Source}
							packet.Continuation.EventIDs = []string{origin.SourceID}
						}
					}
				}
			}
			return &TaskTrigger{Kind: "activity", SourceID: run.SourceID, ContinuationID: run.EventID, FocusID: packet.Focus.ID, GoalVersion: packet.Focus.GoalVersion, FollowGoal: packet.Continuation.Mode != "direct" && packet.Focus.State == "active", EventIDs: packet.Continuation.EventIDs, Sources: packet.Sources}, nil
		}
	case "job", "result":
		job, ok := e.record.Jobs[run.JobID]
		if !ok || job.Version != run.JobVersion || job.State == "stopping" || job.State == "cancelled" {
			return nil, fmt.Errorf("任务已变化，不能按旧目标派发或修改工作")
		}
		if run.Kind == "job" && job.DecisionRunID == run.ID && job.State != "running" {
			return nil, fmt.Errorf("本轮已提交等待或结果，请结束当前回合")
		}
		if err := e.validateTaskFollowLocked(job); err != nil {
			return nil, err
		}
		if job.Trigger != nil {
			return clone(job.Trigger), nil
		}
		if e.hasMessageLocked(job.SourceID) {
			return &TaskTrigger{Kind: "message", SourceID: job.SourceID, MessageID: job.SourceID}, nil
		}
	}
	return nil, fmt.Errorf("缺少当前工作的真实成员消息或事件来源")
}

func (e *Engine) actionProjectionLocked(query string) []map[string]any {
	actions := []CollaborationAction{}
	for _, action := range e.record.Actions {
		if query == "" || action.ID == query || action.JobID == query || action.EventID == query || strings.Contains(string(action.Arguments), query) {
			actions = append(actions, action)
		}
	}
	sort.Slice(actions, func(i, j int) bool { return actions[i].CreatedAt.After(actions[j].CreatedAt) })
	out := []map[string]any{}
	for _, action := range actions[:min(12, len(actions))] {
		out = append(out, map[string]any{"id": action.ID, "tool": action.Tool, "state": action.State, "jobId": action.JobID, "sourceId": action.SourceID, "argumentsPreview": clip(string(action.Arguments), 600), "resultPreview": clip(string(action.Result), 1000), "error": action.Error})
	}
	return out
}

func (e *Engine) hasTaskSourceLocked(id string) bool {
	if e.hasMessageLocked(id) {
		return true
	}
	if e.attention != nil {
		state := e.attention.Snapshot()
		_, exists := state.Events[id]
		return exists || state.Continuations[id].HandlingID != ""
	}
	return false
}

func (e *Engine) taskActionsLocked(jobID string) []map[string]any {
	actions := []CollaborationAction{}
	for _, action := range e.record.Actions {
		if action.JobID == jobID {
			actions = append(actions, action)
		}
	}
	sort.Slice(actions, func(i, j int) bool { return actions[i].CreatedAt.After(actions[j].CreatedAt) })
	out := []map[string]any{}
	for _, action := range actions[:min(16, len(actions))] {
		out = append(out, map[string]any{"id": action.ID, "tool": action.Tool, "state": action.State, "arguments": clip(string(action.Arguments), 1200), "result": clip(string(action.Result), 1600)})
	}
	return out
}

// The main conversation sees an index. Exact task requirements, checkpoints
// and incoming answer signals are fetched with shadow_task status + id.
func taskIndex(all []Job) []Job {
	open, closed := []Job{}, []Job{}
	for _, job := range all {
		switch job.State {
		case "completed", "cancelled", "failed", "interrupted":
			closed = append(closed, job)
		default:
			open = append(open, job)
		}
	}
	if len(open) > 32 {
		open = open[:32]
	}
	if len(closed) > 8 {
		closed = closed[len(closed)-8:]
	}
	out := append(open, closed...)
	for i := range out {
		j := clone(out[i])
		j.Prompt, j.Result, j.Error = clip(j.Prompt, 600), clip(j.Result, 600), clip(j.Error, 300)
		j.CompletionCriteria, j.CompletionChecks, j.Trigger = nil, nil, nil
		j.Follow = nil
		j.Contexts = nil
		j.Understandings = nil
		j.ToolsetDigest, j.PreviousThreads = "", nil
		if j.Checkpoint != nil {
			j.Checkpoint = &TaskCheckpoint{Summary: clip(j.Checkpoint.Summary, 400), NextStep: clip(j.Checkpoint.NextStep, 240)}
		}
		for k, d := range j.Dependencies {
			j.Dependencies[k] = TaskDependency{ID: d.ID, Kind: d.Kind, State: d.State, Condition: clip(d.Condition, 160), JobID: d.JobID, JobVersion: d.JobVersion, DueAt: d.DueAt, Error: clip(d.Error, 200)}
		}
		out[i] = j
	}
	return out
}

// Receipt history is Core transport bookkeeping. Only pending signals and the
// task's actual evidence belong in model packets or the shared space view.
func taskForContext(job Job) Job {
	job = clone(job)
	job.ToolsetDigest = ""
	for i := range job.Contexts {
		job.Contexts[i].NeedsReview = contextPending(job.Contexts[i])
		job.Contexts[i].Current.Fingerprint, job.Contexts[i].Reviewed.Fingerprint = "", ""
	}
	for i := range job.Dependencies {
		job.Dependencies[i].Received = nil
		job.Dependencies[i].QuestionActionID, job.Dependencies[i].QuestionTool = "", ""
		for n := range job.Dependencies[i].Signals {
			job.Dependencies[i].Signals[n].QuestionSource = nil
		}
	}
	return job
}
