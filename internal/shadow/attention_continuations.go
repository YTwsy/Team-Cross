package shadow

import (
	"context"
	"encoding/json"
	"errors"
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.record.Route.Enabled {
		return nil, nil
	}
	items := []AttentionContinuation{}
	for _, c := range s.record.Continuations {
		if c.ShadowID == shadowID && (c.State == "queued" || c.State == "running") {
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

// Finishing a native coordination turn records its result. Business tasks and
// unresolved dependencies have their own completion state.
func (s *AttentionStore) FinishContinuation(id, shadowID, runState, output string, actionIDs []string, work ContinuationWork, readSources ...spacecontext.SourceRef) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.record.Continuations[id]
	if !ok || c.ShadowID != shadowID {
		return fmt.Errorf("接续结果归属不匹配")
	}
	sort.Strings(actionIDs)
	fingerprint := attentionHash([]any{runState, output, actionIDs})
	if c.ResultHash != "" {
		if c.ResultHash != fingerprint {
			return fmt.Errorf("同一接续轮返回了不同结果")
		}
		return nil
	}
	next := clone(s.record)
	c.Result, c.ResultHash, c.ActionIDs, c.UpdatedAt = clip(output, 4000), fingerprint, clone(actionIDs), time.Now()
	if c.HandlingID == "" && c.GoalVersion != next.Focuses[c.FocusID].GoalVersion || !continuationFollowEligible(next, c) {
		c.State = "superseded"
	} else if runState != "completed" {
		c.State, c.Error = "failed", "接续回合未正常结束；保留已做动作及原生记录"
	} else {
		c.State, c.Error = "processed", ""
	}
	if c.Mode == "direct" {
		batch := next.Batches[c.BatchID]
		// The engine records these references only after a successful bound
		// read. They are not claims supplied by the completion JSON itself.
		for _, ref := range readSources {
			if ref.SpaceID != next.SpaceID {
				return fmt.Errorf("直接处理的读取证据空间不符")
			}
			batch.Focus.Evidence = append(batch.Focus.Evidence, ref)
		}
		next.Batches[batch.ID] = batch
		if c.State == "processed" {
			var understanding Observation
			err := json.Unmarshal([]byte(output), &understanding)
			if err != nil {
				err = fmt.Errorf("%w: 直接处理结果不是认识记录", errInvalidObservation)
			} else if understanding.Outcome == "continue" && !work.OpenWork || understanding.Outcome == "needs_owner" && !work.OwnerQuestion {
				err = fmt.Errorf("%w: 后续工作或本人待答问题尚未实际登记", errInvalidObservation)
			} else {
				err = finishObservation(&next, batch.ID, understanding, false)
			}
			if err != nil {
				if !errors.Is(err, errInvalidObservation) {
					return err
				}
				c.State, c.Error = "failed", "直接处理尚未保存有效认识；保留已执行动作，请核对原生结果"
				resetBatch(&next, batch, "failed", c.Error, time.Time{})
			} else {
				c.Analysis = understanding
			}
		} else {
			resetBatch(&next, batch, c.State, c.Error, time.Time{})
		}
	}
	next.Continuations[id] = c
	f := next.Focuses[c.FocusID]
	f.JobIDs = appendUniqueLinks(f.JobIDs, work.JobIDs)
	f.QuestionIDs = appendUniqueLinks(f.QuestionIDs, work.QuestionIDs)
	for _, action := range actionIDs {
		found := false
		for _, known := range f.ActionIDs {
			found = found || known == action
		}
		if !found {
			f.ActionIDs = append(f.ActionIDs, action)
		}
	}
	next.Focuses[f.ID] = f
	return s.saveLocked(next)
}

func (e *Engine) scheduleContinuation(ctx context.Context) (bool, error) {
	e.mu.Lock()
	store, id, thread := e.attention, e.record.ID, e.record.Current
	ready := thread != "" && !e.threadBusyLocked(thread) && e.cfg.CallTool != nil
	e.mu.Unlock()
	if store == nil || !ready {
		return false, nil
	}
	packet, err := store.ClaimContinuation(id)
	if err != nil || packet == nil {
		return false, err
	}
	e.mu.Lock()
	for _, run := range e.record.Runs {
		if run.Kind == "continuation" && run.EventID == packet.Continuation.ID {
			e.mu.Unlock()
			return false, nil
		}
	}
	e.mu.Unlock()
	if packet.Continuation.Mode == "direct" {
		// The main conversation may have registered a wait while this direct
		// input was queued. Recheck just before starting its native turn.
		if !e.checkDirectRoute(ctx, store, packet.Continuation.BatchID) {
			return false, nil
		}
		current, err := store.CurrentContinuation(packet.Continuation.ID, id)
		if err != nil {
			return false, err
		}
		packet = &current
	}
	e.mu.Lock()
	actions := []CollaborationAction{}
	for _, action := range e.record.Actions {
		include := action.EventID == packet.Continuation.ID
		for _, id := range packet.Focus.ActionIDs {
			include = include || action.ID == id
		}
		if include {
			actions = append(actions, clone(action))
		}
	}
	e.mu.Unlock()
	prompt := rolePrompt + "\nContinue this actual collaboration event, not an invented human chat message. Read the original discussion and current replies using the supplied exact sources before deciding what is still needed. Take the required factual reply or consultation through collaboration tools; a final text here is only an internal progress record and is not sent to the discussion. Preserve previous action IDs and do not repeat already completed work. If substantial work is needed, use shadow_task and retain the original evidence and goal. Ask the owner for a concrete missing choice through the original discussion, not for routine permission. Do not publish private receipt reasons.\nCurrent bounded work record:\n" + jsonText(packet) + "\nPrior actions for this continuation:\n" + jsonText(actions)
	if len(packet.MemberChanges) > 0 {
		prompt += "\nmemberChanges identifies facts already changed or answered by this member. Reconcile the remaining work with these sources; do not repeat the member's completed actions or re-ask answered questions."
	}
	var schema any
	if packet.Continuation.Mode == "direct" {
		prompt += "\nThis direct mention or reply has not been sent through an observation model. Understand and handle it in this same turn using the provided source bodies and prior understanding. A copied progress update may need no public reply. After acting, return only the structured current understanding: summary, exact evidence references, openQuestions, outcome and nextStep. Use no_action with an empty nextStep when nothing remains; continue/needs_owner describe work actually registered with shadow_task or a concrete question actually asked. Do not leave an unregistered future action in final text. Evidence must use supplied source references or still-valid previous evidence. An unavailable source is a tombstone, never proof of its former content. The final JSON is private and will not be posted to the discussion."
		schema = observationSchema()
	}
	prompt += questionHandlingNote(packet.Handling)
	if len(prompt) > 192<<10 {
		return false, fmt.Errorf("动态处理上下文超过预算，原批次已保留")
	}
	sourceID := packet.Continuation.ID
	if len(packet.Continuation.EventIDs) > 0 {
		sourceID = packet.Continuation.EventIDs[0]
	}
	return true, e.submit(ctx, Run{Kind: "continuation", ThreadID: thread, SourceID: sourceID, EventID: packet.Continuation.ID}, prompt, schema)
}
