package shadow

import (
	"fmt"
	"strings"
	"time"

	"teamcross/internal/spacecontext"
)

// A context link supplies facts to a task. It does not give another follow
// goal ownership of the task, and never copies the member's private goal.
type TaskContextInput struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
	Mode   string `json:"mode,omitempty"`
}

type TaskContextSnapshot struct {
	Revision      uint64                   `json:"revision"`
	ProcessedSeq  uint64                   `json:"processedSeq"`
	Summary       string                   `json:"summary,omitempty"`
	Evidence      []spacecontext.SourceRef `json:"evidence,omitempty"`
	OpenQuestions []string                 `json:"openQuestions,omitempty"`
	Partial       bool                     `json:"partial,omitempty"`
	Fingerprint   string                   `json:"fingerprint,omitempty"`
}

type TaskContext struct {
	ID          string               `json:"id"`
	Mode        string               `json:"mode,omitempty"`
	Source      *TaskSourceState     `json:"sourceChanges,omitempty"`
	Subject     spacecontext.Subject `json:"subject"`
	Reason      string               `json:"reason"`
	Current     TaskContextSnapshot  `json:"current"`
	Reviewed    TaskContextSnapshot  `json:"reviewed"`
	Explanation string               `json:"explanation,omitempty"`
	NeedsReview bool                 `json:"needsReview,omitempty"`
}

type TaskContextReview struct {
	ID          string `json:"id"`
	Revision    uint64 `json:"revision"`
	Explanation string `json:"explanation"`
}

type TaskContextUse struct {
	ShadowID string `json:"shadowId"`
	JobID    string `json:"jobId"`
	Title    string `json:"title"`
	State    string `json:"state"`
	Reason   string `json:"reason"`
}

type TaskContextUses struct {
	Items []TaskContextUse `json:"items"`
	Total int              `json:"total"`
}

func (e *Engine) ContextUses(id string) TaskContextUses {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := TaskContextUses{Items: []TaskContextUse{}}
	if e.closed {
		return out
	}
	for _, job := range sortedJobs(e.record.Jobs) {
		if !taskAcceptsAnswers(job) {
			continue
		}
		for _, c := range job.Contexts {
			if c.ID == id && c.Mode != "snapshot" {
				out.Total++
				if len(out.Items) < 32 {
					out.Items = append(out.Items, TaskContextUse{e.record.ID, job.ID, clip(job.Title, 160), job.State, clip(c.Reason, 400)})
				}
			}
		}
	}
	return out
}

func contextSnapshot(f Focus) TaskContextSnapshot {
	// Processing state, actions and task IDs are bookkeeping, not new facts.
	facts := []any{f.Summary, f.Evidence, f.OpenQuestions}
	s := TaskContextSnapshot{Revision: f.Revision, ProcessedSeq: f.ProcessedSeq, Summary: clip(f.Summary, 1600), Fingerprint: attentionHash(facts)}
	s.Partial = len([]rune(f.Summary)) > 1600 || len(f.Evidence) > 8 || len(f.OpenQuestions) > 4
	s.Evidence = clone(f.Evidence[:min(8, len(f.Evidence))])
	for _, q := range f.OpenQuestions[:min(4, len(f.OpenQuestions))] {
		s.OpenQuestions = append(s.OpenQuestions, clip(q, 400))
		s.Partial = s.Partial || len([]rune(q)) > 400
	}
	// Unicode character limits alone do not bound the model packet in bytes.
	// Keep exact evidence IDs or omit them with partial=true; never truncate IDs.
	for len(jsonText(s)) > 3<<10 {
		s.Partial = true
		switch {
		case len([]rune(s.Summary)) > 256:
			s.Summary = clip(s.Summary, len([]rune(s.Summary))/2)
		case len(s.OpenQuestions) > 0:
			s.OpenQuestions = s.OpenQuestions[:len(s.OpenQuestions)-1]
		case len(s.Evidence) > 0:
			s.Evidence = s.Evidence[:len(s.Evidence)-1]
		default:
			s.Summary = ""
		}
	}
	return s
}

func contextPending(c TaskContext) bool {
	return c.Current.Fingerprint != c.Reviewed.Fingerprint
}

func pendingTaskContexts(job Job) bool {
	for _, c := range job.Contexts {
		if contextPending(c) {
			return true
		}
	}
	return false
}

func unseenTaskContext(job Job, run Run) bool {
	for _, c := range job.Contexts {
		if contextPending(c) && run.ContextInputs[c.ID] != c.Current.Fingerprint {
			return true
		}
		if current := contextSourceInput(c); current != "" && run.ContextInputs["source:"+c.ID] != current {
			return true
		}
	}
	return false
}

func (e *Engine) taskContextsLocked(previous []TaskContext, input []TaskContextInput) ([]TaskContext, error) {
	if len(input) > 8 {
		return nil, fmt.Errorf("单任务最多关联八个认识对象")
	}
	if len(input) > 0 && e.attention == nil {
		return nil, fmt.Errorf("任务认识尚未连接")
	}
	old := map[string]TaskContext{}
	for _, c := range previous {
		old[c.ID] = c
	}
	out, seen := []TaskContext{}, map[string]bool{}
	for _, in := range input {
		if in.Mode != "" && in.Mode != "live" && in.Mode != "snapshot" {
			return nil, fmt.Errorf("认识关联模式需要 live 或 snapshot")
		}
		if in.ID == "" || seen[in.ID] || strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 1200 {
			return nil, fmt.Errorf("关联认识需要唯一对象 ID 和具体用途")
		}
		seen[in.ID] = true
		f, ok := e.attention.taskFocus(in.ID)
		if !ok || f.Revision == 0 {
			return nil, fmt.Errorf("关联认识尚未建立，请先读取准确对象")
		}
		c, exists := old[in.ID]
		if !exists {
			c = TaskContext{ID: f.ID, Subject: f.Subject, Current: contextSnapshot(f)}
		} else if next := contextSnapshot(f); c.Mode != "snapshot" && next.Fingerprint != c.Current.Fingerprint {
			c.Current = next
		}
		if in.Mode != "" {
			c.Mode = in.Mode
		}
		if c.Mode == "snapshot" {
			c.Source = nil
		}
		c.Reason = strings.TrimSpace(in.Reason)
		out = append(out, clone(c))
	}
	return out, nil
}

// The last reviewed facts and the newest facts are retained independently.
// Coalescing several observations cannot erase what the task used previously.
func (e *Engine) refreshedTaskContextsLocked(job Job) (Job, bool) {
	if e.attention == nil || !taskAcceptsAnswers(job) || len(job.Contexts) == 0 {
		return job, false
	}
	job = clone(job)
	changed := false
	factsChanged := false
	for i := range job.Contexts {
		c := &job.Contexts[i]
		if c.Mode == "snapshot" {
			continue
		}
		if c.Source != nil {
			pending := e.pendingSourceSignalsLocked(*c)
			if len(pending) != len(c.Source.Signals) {
				c.Source.Signals, changed = pending, true
			}
		}
		f, exists := e.attention.taskFocus(c.ID)
		if !exists || f.Revision == 0 {
			continue
		}
		next := contextSnapshot(f)
		if next.Fingerprint != c.Current.Fingerprint {
			c.Current, changed = next, true
			factsChanged = true
		}
	}
	if !changed {
		return job, false
	}
	if factsChanged && !e.taskRunActiveLocked(job.ID) && (job.State == "waiting" || job.State == "needs_followup") {
		job.State, job.Outcome = "queued", ""
		job.ResultEventID, job.DeliveredEventID = "", ""
	}
	job.Revision++
	job.UpdatedAt = time.Now()
	return job, true
}

func (e *Engine) reviewTaskContextsLocked(job *Job, in taskLifecycleInput) error {
	if in.RelatedFocuses != nil {
		next, err := e.taskContextsLocked(job.Contexts, *in.RelatedFocuses)
		if err != nil {
			return err
		}
		job.Contexts = next
	}
	if len(in.ContextReviews) > 8 {
		return fmt.Errorf("认识核对超过单任务上限")
	}
	seen := map[string]bool{}
	for _, review := range in.ContextReviews {
		if seen[review.ID] || strings.TrimSpace(review.Explanation) == "" || len(review.Explanation) > 2400 {
			return fmt.Errorf("认识核对需要唯一对象 ID 和实际影响说明")
		}
		seen[review.ID] = true
		found := false
		for i := range job.Contexts {
			c := &job.Contexts[i]
			if c.ID != review.ID {
				continue
			}
			if review.Revision != c.Current.Revision {
				return fmt.Errorf("关联认识已有新版本，请按当前来源重新核对")
			}
			c.Reviewed, c.Explanation = clone(c.Current), review.Explanation
			found = true
		}
		if !found {
			return fmt.Errorf("核对对象不属于当前任务")
		}
	}
	return nil
}

// Ordinary continuation packets carry changed objects only. Complete bounded
// context remains available through the exact task's status operation.
func taskForRunContext(job Job) Job {
	job = taskForContext(job)
	for i := range job.Contexts {
		if !job.Contexts[i].NeedsReview {
			job.Contexts[i].Current = TaskContextSnapshot{Revision: job.Contexts[i].Current.Revision}
			job.Contexts[i].Reviewed = TaskContextSnapshot{Revision: job.Contexts[i].Reviewed.Revision}
		}
	}
	return job
}
