package shadow

import (
	"fmt"
	"sort"
	"time"

	"teamcross/internal/spacecontext"
)

type ContinuationContext struct {
	MemberID      string                   `json:"memberId"`
	MemberChanges []spacecontext.SourceRef `json:"memberChanges,omitempty"`
	Continuation  AttentionContinuation    `json:"continuation"`
	Focus         Focus                    `json:"focus"`
	Sources       []spacecontext.SourceRef `json:"sources"`
	SourceBodies  []SourceSnapshot         `json:"sourceBodies,omitempty"`
	Handling      []QuestionHandling       `json:"handling,omitempty"`
}

// Produced from durable Core records, never from the model's final JSON.
type ContinuationWork struct {
	JobIDs        []string
	QuestionIDs   []string
	OpenWork      bool
	OwnerQuestion bool
}

func (e *Engine) continuationWorkLocked(run Run) ContinuationWork {
	work := ContinuationWork{}
	jobs := map[string]bool{}
	for _, id := range run.TaskIDs {
		jobs[id] = true
	}
	for _, job := range e.record.Jobs {
		if job.Trigger != nil && job.Trigger.ContinuationID == run.EventID {
			jobs[job.ID] = true
		}
	}
	for _, q := range e.record.Questions {
		if q.RunID == run.ID {
			jobs[q.JobID] = true
		}
	}
	for id := range jobs {
		job, exists := e.record.Jobs[id]
		if !exists {
			continue
		}
		work.JobIDs = append(work.JobIDs, id)
		work.OpenWork = work.OpenWork || taskAcceptsAnswers(job)
	}
	for _, q := range e.record.Questions {
		if !jobs[q.JobID] && q.RunID != run.ID {
			continue
		}
		work.QuestionIDs = append(work.QuestionIDs, q.ID)
		job := e.record.Jobs[q.JobID]
		if q.Delivery != "saved" || !taskAcceptsAnswers(job) {
			continue
		}
		for _, d := range job.Dependencies {
			if d.QuestionID != q.ID || d.State == "satisfied" {
				continue
			}
			for _, member := range q.Recipients {
				work.OwnerQuestion = work.OwnerQuestion || member == e.record.Owner.ID
			}
		}
	}
	sort.Strings(work.JobIDs)
	sort.Strings(work.QuestionIDs)
	if e.attention != nil {
		if packet, err := e.attention.CurrentContinuation(run.EventID, e.record.ID); err == nil {
			for _, h := range packet.Handling {
				work.OwnerQuestion = work.OwnerQuestion || h.Handler == "owner" && h.State == "waiting"
			}
		}
	}
	return work
}

func appendUniqueLinks(existing, added []string) []string {
	for _, id := range added {
		found := false
		for _, old := range existing {
			found = found || old == id
		}
		if !found {
			existing = append(existing, id)
		}
	}
	if len(existing) > 64 {
		existing = existing[len(existing)-64:]
	}
	return existing
}

func (s *AttentionStore) HasContinuation(shadowID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.record.Route.Enabled {
		return false
	}
	for _, c := range s.record.Continuations {
		f := s.record.Focuses[c.FocusID]
		if c.ShadowID == shadowID && (c.State == "queued" || c.State == "running") && continuationFollowEligible(s.record, c) && (c.HandlingID != "" || f.State != "paused" && f.GoalVersion == c.GoalVersion) {
			return true
		}
	}
	return false
}

func (s *AttentionStore) continuationLocked(c AttentionContinuation) ContinuationContext {
	sources := clone(c.Sources)
	for _, id := range c.EventIDs {
		sources = append(sources, clone(s.record.Events[id].Signal.Source))
	}
	sources = uniqueQuestionAndOtherSources(sources)
	packet := ContinuationContext{MemberID: s.record.MemberID, MemberChanges: s.memberChangesLocked(c.EventIDs), Continuation: clone(c), Focus: clone(s.record.Focuses[c.FocusID]), Sources: sources}
	for _, ref := range uniqueQuestionSources(sources) {
		if h, ok := s.record.Handling[questionSourceID(ref)]; ok {
			packet.Handling = append(packet.Handling, clone(h))
		}
	}
	if c.Mode == "direct" {
		packet.SourceBodies = clone(s.record.Batches[c.BatchID].Sources)
	}
	return packet
}

func (s *AttentionStore) ClaimContinuation(shadowID string) (*ContinuationContext, error) {
	return s.claimContinuationExcept(shadowID, nil)
}

func (s *AttentionStore) claimContinuationExcept(shadowID string, excluded map[string]bool) (*ContinuationContext, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.record.Route.Enabled {
		return nil, nil
	}
	items := []AttentionContinuation{}
	for _, c := range s.record.Continuations {
		if !excluded[c.ID] && c.ShadowID == shadowID && (c.State == "queued" || c.State == "running") {
			items = append(items, c)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].State != items[j].State {
			return items[i].State == "running"
		}
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].ID < items[j].ID
	})
	for _, c := range items {
		focus := s.record.Focuses[c.FocusID]
		if c.HandlingID != "" && c.State == "queued" && continuationFollowEligible(s.record, c) && focus.GoalVersion != c.GoalVersion {
			next := clone(s.record)
			c.GoalVersion = focus.GoalVersion
			next.Continuations[c.ID] = c
			batch := next.Batches[c.BatchID]
			batch.Focus = clone(focus)
			next.Batches[batch.ID] = batch
			if err := s.saveLocked(next); err != nil {
				return nil, err
			}
		}
		if c.HandlingID == "" && focus.GoalVersion != c.GoalVersion || c.HandlingID != "" && !continuationFollowEligible(s.record, c) {
			next := clone(s.record)
			c.State, c.UpdatedAt = "superseded", time.Now()
			next.Continuations[c.ID] = c
			if c.Mode == "direct" {
				resetBatch(&next, next.Batches[c.BatchID], "superseded", "", time.Time{})
			}
			if err := s.saveLocked(next); err != nil {
				return nil, err
			}
			continue
		}
		if c.HandlingID == "" && focus.State == "paused" || !continuationFollowEligible(s.record, c) || focus.Error != "" && (focus.RetryAfter.IsZero() || time.Now().Before(focus.RetryAfter)) {
			continue
		}
		if c.State == "queued" {
			next := clone(s.record)
			c.State, c.UpdatedAt = "running", time.Now()
			next.Continuations[c.ID] = c
			if err := s.saveLocked(next); err != nil {
				return nil, err
			}
		}
		out := s.continuationLocked(c)
		return &out, nil
	}
	return nil, nil
}

func (s *AttentionStore) CurrentContinuation(id, shadowID string) (ContinuationContext, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.record.Continuations[id]
	f := s.record.Focuses[c.FocusID]
	if !ok || c.ShadowID != shadowID || c.State != "running" || c.HandlingID == "" && (c.GoalVersion != f.GoalVersion || f.State == "paused") || !continuationFollowEligible(s.record, c) {
		return ContinuationContext{}, fmt.Errorf("接续事项已经变更或暂停，停止旧行动并读取当前状态")
	}
	return s.continuationLocked(c), nil
}

// Older accepted runs use the same message receipt semantics as batched inputs.
func (s *AttentionStore) FinishContinuation(id, shadowID, runState, output string, actionIDs []string, work ContinuationWork, readSources ...spacecontext.SourceRef) error {
	return s.finishMessageInput(id, shadowID, runState, actionIDs, work.JobIDs, work.QuestionIDs)
}
