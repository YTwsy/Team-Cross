package shadow

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/spacecontext"
)

var errObsoleteBatch = errors.New("观察批次已过时，未提交模型输入")
var errInvalidObservation = errors.New("观察结果无效，未推进处理位置")

type ObservationBatch struct {
	ID            string           `json:"id"`
	Mode          string           `json:"mode,omitempty"` // observe or direct
	ShadowID      string           `json:"shadowId"`
	RouteRevision uint64           `json:"routeRevision"`
	Focus         Focus            `json:"focus"`
	EventIDs      []string         `json:"eventIds"`
	Sources       []SourceSnapshot `json:"sources,omitempty"`
	State         string           `json:"state"` // preparing, running, completed, superseded, failed
	ResultHash    string           `json:"resultHash,omitempty"`
	Error         string           `json:"error,omitempty"`
	CreatedAt     time.Time        `json:"createdAt"`
}

type Observation struct {
	Summary       string                   `json:"summary"`
	Evidence      []spacecontext.SourceRef `json:"evidence"`
	OpenQuestions []string                 `json:"openQuestions"`
	Outcome       string                   `json:"outcome"` // no_action, continue, needs_owner
	NextStep      string                   `json:"nextStep,omitempty"`
}

type AttentionContinuation struct {
	HandlingID       string                   `json:"handlingId,omitempty"`
	HandlingRevision uint64                   `json:"handlingRevision,omitempty"`
	Sources          []spacecontext.SourceRef `json:"sources,omitempty"`
	ID               string                   `json:"id"`
	BatchID          string                   `json:"batchId"`
	Mode             string                   `json:"mode,omitempty"`
	ShadowID         string                   `json:"shadowId"`
	FocusID          string                   `json:"focusId"`
	GoalVersion      uint64                   `json:"goalVersion"`
	EventIDs         []string                 `json:"eventIds"`
	Analysis         Observation              `json:"analysis"`
	State            string                   `json:"state"`
	CreatedAt        time.Time                `json:"createdAt"`
	Result           string                   `json:"result,omitempty"`
	ResultHash       string                   `json:"resultHash,omitempty"`
	ActionIDs        []string                 `json:"actionIds,omitempty"`
	Error            string                   `json:"error,omitempty"`
	UpdatedAt        time.Time                `json:"updatedAt"`
}

func signalDirect(signal spacecontext.Signal) bool {
	for _, reason := range signal.Reasons {
		switch reason {
		case "mention", "direct_reply", "own_request_result", "own_turn_result":
			return true
		}
	}
	return false
}

// ClaimBatch is metadata-only. It fixes an exact prefix for one object; newer
// signals remain queued. Calling it never advances understanding or human reads.
func (s *AttentionStore) ClaimBatch(shadowID string, now time.Time, mergeDelay, maxWait time.Duration) (*ObservationBatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	activeFocus := map[string]bool{}
	for _, batch := range s.record.Batches {
		if batch.State == "preparing" || batch.State == "running" || batch.State == "continuing" {
			if batch.State == "continuing" {
				activeFocus[batch.Focus.ID] = true
				continue
			}
			if batch.ShadowID == shadowID {
				return ptrBatch(batch), nil
			}
			activeFocus[batch.Focus.ID] = true
		}
	}
	if s.record.Route.ShadowID != shadowID || !s.record.Route.Enabled {
		return nil, nil
	}
	groups := map[string][]AttentionEvent{}
	for _, event := range s.record.Events {
		focus := s.record.Focuses[event.FocusID]
		retryReady := focus.Error == "" || (!focus.RetryAfter.IsZero() && !focus.RetryAfter.After(now))
		if event.State == "queued" && attentionEligible(s.record, event.Signal) && !activeFocus[event.FocusID] && focus.State != "paused" && retryReady {
			groups[event.FocusID] = append(groups[event.FocusID], event)
		}
	}
	type candidate struct {
		id     string
		urgent bool
		oldest time.Time
	}
	candidates := []candidate{}
	for id, events := range groups {
		oldest, newest, urgent := events[0].ReceivedAt, events[0].ReceivedAt, false
		for _, event := range events {
			if event.ReceivedAt.Before(oldest) {
				oldest = event.ReceivedAt
			}
			if event.ReceivedAt.After(newest) {
				newest = event.ReceivedAt
			}
			urgent = urgent || signalDirect(event.Signal)
		}
		if !urgent && now.Sub(newest) < mergeDelay && now.Sub(oldest) < maxWait {
			continue
		}
		urgent = urgent || now.Sub(oldest) >= maxWait
		candidates = append(candidates, candidate{id, urgent, oldest})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].urgent != candidates[j].urgent {
			return candidates[i].urgent
		}
		if !candidates[i].oldest.Equal(candidates[j].oldest) {
			return candidates[i].oldest.Before(candidates[j].oldest)
		}
		return candidates[i].id < candidates[j].id
	})
	if len(candidates) == 0 {
		return nil, nil
	}
	id := candidates[0].id
	events := groups[id]
	sort.Slice(events, func(i, j int) bool {
		if events[i].Signal.Seq == events[j].Signal.Seq {
			return events[i].Signal.ID < events[j].Signal.ID
		}
		return events[i].Signal.Seq < events[j].Signal.Seq
	})
	next := clone(s.record)
	batch := ObservationBatch{ID: uuid.NewString(), ShadowID: shadowID, RouteRevision: next.Route.Revision, Focus: clone(next.Focuses[id]), EventIDs: []string{}, State: "preparing", CreatedAt: now}
	bytes := 0
	for _, event := range events {
		encoded, _ := json.Marshal(event.Signal)
		if len(batch.EventIDs) >= 20 || bytes+len(encoded) > 16<<10 {
			break
		}
		bytes += len(encoded)
		batch.EventIDs = append(batch.EventIDs, event.Signal.ID)
		if signalDirect(event.Signal) && !event.ObserveOnly {
			batch.Mode = "direct"
		}
		event.State = "observing"
		next.Events[event.Signal.ID] = event
	}
	if len(batch.EventIDs) == 0 {
		return nil, fmt.Errorf("单条来源索引超过观察预算，事件仍待处理")
	}
	focus := next.Focuses[id]
	focus.Processing, focus.Error = "preparing", ""
	next.Focuses[id] = focus
	next.Batches[batch.ID] = batch
	if err := s.saveLocked(next); err != nil {
		return nil, err
	}
	return ptrBatch(batch), nil
}

func ptrBatch(batch ObservationBatch) *ObservationBatch { b := clone(batch); return &b }

func (s *AttentionStore) BatchSignals(id string) ([]spacecontext.Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	batch, ok := s.record.Batches[id]
	if !ok {
		return nil, fmt.Errorf("观察批次不存在")
	}
	out := make([]spacecontext.Signal, 0, len(batch.EventIDs))
	for _, id := range batch.EventIDs {
		out = append(out, clone(s.record.Events[id].Signal))
	}
	return out, nil
}

// PrepareBatch commits the exact source bodies before native submission. If
// the body budget fits only a prefix, the untouched suffix is queued again.
func (s *AttentionStore) PrepareBatch(id string, sources []SourceSnapshot) (*ObservationBatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	batch, ok := s.record.Batches[id]
	if !ok || batch.State != "preparing" || batch.RouteRevision != s.record.Route.Revision || batch.ShadowID != s.record.Route.ShadowID || !s.record.Route.Enabled || batch.Focus.GoalVersion != s.record.Focuses[batch.Focus.ID].GoalVersion {
		return nil, errObsoleteBatch
	}
	if len(sources) == 0 || len(sources) > len(batch.EventIDs) {
		return nil, fmt.Errorf("观察批次缺少准确原文")
	}
	encoded, _ := json.Marshal(sources)
	if len(encoded) > 96<<10 {
		return nil, fmt.Errorf("观察原文超过批次预算")
	}
	for i, source := range sources {
		if !json.Valid(source.Data) || attentionHash(source.Source) != attentionHash(s.record.Events[batch.EventIDs[i]].Signal.Source) {
			return nil, fmt.Errorf("观察原文与事件来源不匹配")
		}
	}
	next := clone(s.record)
	for _, eventID := range batch.EventIDs[len(sources):] {
		event := next.Events[eventID]
		event.State = "queued"
		next.Events[eventID] = event
	}
	batch.EventIDs = batch.EventIDs[:len(sources)]
	batch.Sources, batch.State = clone(sources), "running"
	// The source budget may have excluded the urgent signal. Choose the path
	// from the actual persisted prefix, never from a truncated group preview.
	batch.Mode = "observe"
	for _, eventID := range batch.EventIDs {
		if signalDirect(next.Events[eventID].Signal) && !next.Events[eventID].ObserveOnly {
			batch.Mode = "direct"
		}
	}
	if batch.Mode == "direct" {
		batch.State = "continuing"
		next.Continuations[id] = AttentionContinuation{ID: id, BatchID: id, Mode: "direct", ShadowID: batch.ShadowID, FocusID: batch.Focus.ID, GoalVersion: batch.Focus.GoalVersion, EventIDs: clone(batch.EventIDs), State: "queued", CreatedAt: time.Now()}
	}
	next.Batches[id] = batch
	focus := next.Focuses[batch.Focus.ID]
	focus.Processing = "observing"
	if batch.Mode == "direct" {
		focus.Processing = "queued"
	}
	next.Focuses[focus.ID] = focus
	if err := s.saveLocked(next); err != nil {
		return nil, err
	}
	return ptrBatch(batch), nil
}

func resetBatch(next *AttentionRecord, batch ObservationBatch, state, reason string, retryAfter time.Time) {
	batch.State, batch.Error, batch.Sources = state, reason, nil
	next.Batches[batch.ID] = batch
	for _, id := range batch.EventIDs {
		event := next.Events[id]
		event.State = "queued"
		if next.Handling[questionSourceID(event.Signal.Source)].Handler == "owner" {
			event.State = "owner"
		}
		next.Events[id] = event
	}
	focus := next.Focuses[batch.Focus.ID]
	focus.Processing, focus.Error, focus.RetryAfter = "queued", reason, retryAfter
	if reason != "" {
		focus.Processing = "error"
	}
	next.Focuses[focus.ID] = focus
}

func (s *AttentionStore) FailBatch(id, reason string, retryAfter time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	batch, ok := s.record.Batches[id]
	if !ok {
		return fmt.Errorf("观察批次不存在")
	}
	if batch.State != "preparing" && batch.State != "running" && batch.State != "continuing" {
		return nil
	}
	next := clone(s.record)
	resetBatch(&next, batch, "failed", reason, retryAfter)
	return s.saveLocked(next)
}

func (s *AttentionStore) FinishBatch(id string, observation Observation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.record)
	if err := finishObservation(&next, id, observation, true); err != nil {
		return err
	}
	return s.saveLocked(next)
}

// Understanding and its processed prefix commit together with the outcome.
// A direct turn has already acted; it must not schedule a second coordinator.
func finishObservation(next *AttentionRecord, id string, observation Observation, continueWork bool) error {
	batch, ok := next.Batches[id]
	if !ok {
		return fmt.Errorf("观察批次不存在")
	}
	if continueWork && batch.Mode == "direct" {
		return fmt.Errorf("直接处理必须提交同一接续轮的认识与行动结果")
	}
	fingerprint := attentionHash(observation)
	if batch.ResultHash != "" {
		if batch.ResultHash != fingerprint {
			return fmt.Errorf("同一观察批次提交了不同结果")
		}
		return nil
	}
	if batch.State != "running" && batch.State != "continuing" {
		return fmt.Errorf("观察批次尚未读取原文或已经失效")
	}
	if observation.Summary == "" || len(observation.Summary) > 8000 || len(observation.NextStep) > 8000 || len(observation.OpenQuestions) > 16 || len(observation.Evidence) > 32 {
		return fmt.Errorf("%w: 认识缺失或超出预算", errInvalidObservation)
	}
	if observation.Outcome != "no_action" && observation.Outcome != "continue" && observation.Outcome != "needs_owner" {
		return fmt.Errorf("%w: 结果类型无效", errInvalidObservation)
	}
	if (observation.Outcome == "no_action") != (observation.NextStep == "") {
		return fmt.Errorf("%w: 结果与下一步不一致", errInvalidObservation)
	}
	for _, question := range observation.OpenQuestions {
		if len(question) > 4000 {
			return fmt.Errorf("%w: 未决问题过长", errInvalidObservation)
		}
	}
	known := map[string]bool{}
	withdrawn := map[string]bool{}
	for _, source := range batch.Sources {
		if source.Unavailable != "" && source.Source.MaterialID != "" {
			withdrawn[source.Source.MaterialID] = true
		}
	}
	for _, source := range batch.Focus.Evidence {
		known[attentionHash(source)] = !withdrawn[source.MaterialID]
	}
	for _, source := range batch.Sources {
		known[attentionHash(source.Source)] = source.Unavailable == "" && !withdrawn[source.Source.MaterialID]
		for _, ref := range source.References {
			known[attentionHash(ref)] = source.Unavailable == "" && !withdrawn[ref.MaterialID]
		}
	}
	for _, source := range observation.Evidence {
		if source.SpaceID != next.SpaceID || !known[attentionHash(source)] {
			return fmt.Errorf("%w: 证据未在本批或此前有效认识中提供", errInvalidObservation)
		}
	}
	focus := next.Focuses[batch.Focus.ID]
	if focus.UnderstandingRevision != batch.Focus.UnderstandingRevision {
		// A task has reconciled newer answers while this observation was in
		// flight. Re-read the bounded inputs against the new facts; a direct
		// turn's already committed business actions must not be replayed.
		resetBatch(next, batch, "superseded", "", time.Time{})
		for _, eventID := range batch.EventIDs {
			event := next.Events[eventID]
			event.ObserveOnly = true
			next.Events[eventID] = event
		}
		old := next.Batches[id]
		old.ResultHash = fingerprint
		next.Batches[id] = old
		return nil
	}
	goalMatches := focus.GoalVersion == batch.Focus.GoalVersion
	if !goalMatches && next.Continuations[id].HandlingID == "" || !batchFollowEligible(*next, batch) {
		resetBatch(next, batch, "superseded", "", time.Time{})
		old := next.Batches[id]
		old.ResultHash = fingerprint
		next.Batches[id] = old
		return nil
	}
	focus.Revision++
	// Explicit question handback remains valid when an ordinary follow goal
	// changes. Its result stays in the continuation and cannot replace the
	// understanding already recorded for the new follow goal.
	if goalMatches {
		focus.Summary, focus.Evidence, focus.OpenQuestions = observation.Summary, clone(observation.Evidence), clone(observation.OpenQuestions)
	}
	focus.Processing, focus.Error, focus.RetryAfter, focus.UpdatedAt = "idle", "", time.Time{}, time.Now()
	for _, id := range batch.EventIDs {
		event := next.Events[id]
		event.State = "processed"
		next.Events[id] = event
		focus.ProcessedSeq = max(focus.ProcessedSeq, event.Signal.Seq)
		if goalMatches {
			if next.UnderstoodAnswers == nil {
				next.UnderstoodAnswers = map[string]bool{}
			}
			next.UnderstoodAnswers[understandingAnswerKey(focus.ID, DependencySignal{ID: event.Signal.ID, Source: event.Signal.Source})] = true
		}
	}
	for _, event := range next.Events {
		if event.FocusID == focus.ID && event.State == "queued" {
			focus.Processing = "queued"
			break
		}
	}
	next.Focuses[focus.ID] = focus
	batch.State, batch.ResultHash, batch.Sources = "completed", fingerprint, nil
	next.Batches[id] = batch
	if continueWork && observation.Outcome != "no_action" {
		next.Continuations[id] = AttentionContinuation{ID: id, BatchID: id, ShadowID: batch.ShadowID, FocusID: focus.ID, GoalVersion: focus.GoalVersion, EventIDs: clone(batch.EventIDs), Analysis: clone(observation), State: "queued", CreatedAt: time.Now()}
	}
	return nil
}

func (s *AttentionStore) UpdateFocus(id string, baseRevision uint64, goal, state string) (Focus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	focus, exists := s.record.Focuses[id]
	if !exists || focus.Revision != baseRevision {
		return Focus{}, fmt.Errorf("关注认识已变化，请重新读取")
	}
	if len(goal) > 8000 || (state != "active" && state != "paused" && state != "completed") {
		return Focus{}, fmt.Errorf("关注目标或状态无效")
	}
	next := clone(s.record)
	focus.Goal, focus.State, focus.Revision, focus.GoalVersion, focus.UpdatedAt = goal, state, focus.Revision+1, focus.GoalVersion+1, time.Now()
	focus.Error, focus.RetryAfter = "", time.Time{}
	next.Focuses[id] = focus
	for _, batch := range next.Batches {
		if batch.Focus.ID == id && batch.State == "preparing" {
			resetBatch(&next, batch, "superseded", "", time.Time{})
		}
	}
	if err := s.saveLocked(next); err != nil {
		return Focus{}, err
	}
	return clone(next.Focuses[id]), nil
}
