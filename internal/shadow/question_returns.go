package shadow

import (
	"encoding/json"
	"sort"
	"time"

	"teamcross/internal/spacecontext"
)

const QuestionReturnBatchLimit = 64
const QuestionReturnBatchBytes = 64 << 10

// One real question linked to one task and its actual originating message.
// Native sessions, action arguments and private task prompts are not exported.
type QuestionReturn struct {
	ID          string                 `json:"id"`
	Revision    int                    `json:"revision"`
	QuestionID  string                 `json:"questionId"`
	TaskID      string                 `json:"taskId"`
	TaskVersion int                    `json:"taskVersion"`
	MessageID   string                 `json:"messageId"`
	Source      spacecontext.SourceRef `json:"source"`
	Text        string                 `json:"text"`
	Condition   string                 `json:"condition"`
	NextStep    string                 `json:"nextStep,omitempty"`
	Recipients  []string               `json:"recipients,omitempty"`
	State       string                 `json:"state"`
	UpdatedAt   time.Time              `json:"updatedAt"`
}

func (e *Engine) taskMessageIDLocked(job Job) string {
	if job.Trigger != nil {
		return job.Trigger.MessageID
	}
	if e.hasMessageLocked(job.SourceID) {
		return job.SourceID
	}
	return ""
}

func questionReturnFingerprint(q QuestionReturn) string {
	q.Revision, q.UpdatedAt = 0, time.Time{}
	return attentionHash(q)
}

// The latest state replaces an older unsynchronized state; acknowledging an
// old batch cannot drop a newer answer or retirement. Save with the task change.
func (e *Engine) captureQuestionReturnsLocked() {
	states := make(map[string]QuestionReturn, len(e.record.QuestionReturns))
	outbox := make(map[string]QuestionReturn, len(e.record.QuestionReturnOutbox))
	for id, q := range e.record.QuestionReturns {
		states[id] = q
	}
	for id, q := range e.record.QuestionReturnOutbox {
		outbox[id] = q
	}
	seen := map[string]bool{}
	put := func(q QuestionReturn) {
		seen[q.ID] = true
		old, exists := states[q.ID]
		if exists && questionReturnFingerprint(q) == questionReturnFingerprint(old) {
			return
		}
		q.Revision, q.UpdatedAt = old.Revision+1, time.Now()
		states[q.ID], outbox[q.ID] = q, q
	}
	for _, job := range e.record.Jobs {
		messageID := e.taskMessageIDLocked(job)
		if messageID == "" {
			continue
		}
		for _, d := range job.Dependencies {
			q, exists := e.record.Questions[d.QuestionID]
			if !exists || q.Delivery != "saved" || d.QuestionSourcePending {
				continue
			}
			r := QuestionReturn{ID: attentionHash([]string{job.ID, d.ID, q.ID}), QuestionID: q.ID, TaskID: job.ID, TaskVersion: job.Version, MessageID: messageID, Source: clone(q.Source), Text: clip(q.Text, 600), Condition: clip(d.Condition, 500), Recipients: clone(q.Recipients), State: "waiting"}
			if job.Checkpoint != nil {
				r.NextStep = clip(job.Checkpoint.NextStep, 500)
			}
			sort.Strings(r.Recipients)
			switch {
			case !taskAcceptsAnswers(job):
				r.State = "closed"
			case d.State == "satisfied":
				r.State = "conditions_met"
			case dependencyAnswerReceived(d):
				r.State = "answer_received"
			case job.State == "paused" || job.State == "stopping":
				r.State = "paused"
			}
			for _, signal := range d.Signals {
				if signal.State == "unavailable" && r.State != "closed" {
					r.State = "unavailable"
				}
			}
			put(r)
		}
	}
	for id, q := range states {
		if !seen[id] && q.State != "closed" {
			q.State = "closed"
			put(q)
		}
	}
	e.record.QuestionReturns, e.record.QuestionReturnOutbox = states, outbox
}

func (e *Engine) QuestionReturnExport() []QuestionReturn {
	e.mu.Lock()
	defer e.mu.Unlock()
	all := make([]QuestionReturn, 0, len(e.record.QuestionReturnOutbox))
	for _, q := range e.record.QuestionReturnOutbox {
		all = append(all, q)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].UpdatedAt.Equal(all[j].UpdatedAt) {
			return all[i].ID < all[j].ID
		}
		return all[i].UpdatedAt.Before(all[j].UpdatedAt)
	})
	batch := []QuestionReturn{}
	for _, q := range all {
		candidate := append(append([]QuestionReturn{}, batch...), q)
		b, _ := json.Marshal(candidate)
		if len(b) > QuestionReturnBatchBytes || len(candidate) > QuestionReturnBatchLimit {
			break
		}
		batch = candidate
	}
	return clone(batch)
}

func (e *Engine) ConfirmQuestionReturnExport(batch []QuestionReturn) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	matching := []string{}
	for _, q := range batch {
		if current, ok := e.record.QuestionReturnOutbox[q.ID]; ok && current.Revision == q.Revision {
			matching = append(matching, q.ID)
		}
	}
	if len(matching) == 0 {
		return nil
	}
	return e.mutateLocked(func() error {
		for _, id := range matching {
			delete(e.record.QuestionReturnOutbox, id)
		}
		return nil
	})
}
