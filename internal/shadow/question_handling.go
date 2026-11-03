package shadow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"teamcross/internal/spacecontext"
)

var errQuestionOwned = errors.New("这条问题由本人回复。保留调查与已做动作，继续其他独立工作；不要另起讨论重复代答或追问")

type HandlingInput struct {
	RequestID    string                 `json:"requestId"`
	Source       spacecontext.SourceRef `json:"source"`
	Handler      string                 `json:"handler"` // owner or shadow
	BaseRevision uint64                 `json:"baseRevision"`
}

type QuestionHandling struct {
	ID        string                 `json:"id"`
	Source    spacecontext.SourceRef `json:"source"`
	Handler   string                 `json:"handler"`
	Revision  uint64                 `json:"revision"`
	Question  string                 `json:"question"`
	State     string                 `json:"state"` // waiting, answered, active, unavailable
	Cursor    string                 `json:"cursor,omitempty"`
	Answer    *DependencySignal      `json:"answer,omitempty"`
	Error     string                 `json:"error,omitempty"`
	UpdatedAt time.Time              `json:"updatedAt"`
}

type HandlingCommand struct {
	Fingerprint string           `json:"fingerprint"`
	SourceID    string           `json:"sourceId"`
	Result      QuestionHandling `json:"result"`
}

func questionSource(ref spacecontext.SourceRef) spacecontext.SourceRef {
	return spacecontext.SourceRef{SpaceID: ref.SpaceID, ExecutionID: ref.ExecutionID, AnnotationID: ref.AnnotationID, ReplyID: ref.ReplyID}
}

func questionSourceID(ref spacecontext.SourceRef) string { return attentionHash(questionSource(ref)) }

func validQuestionSource(ref spacecontext.SourceRef, space string) bool {
	return ref.SpaceID == space && ref.AnnotationID != "" && ref.RequestID == "" && ref.TurnID == "" && len(ref.ItemIDs) == 0
}

func (s *AttentionStore) HandlingFor(ref spacecontext.SourceRef) (QuestionHandling, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validQuestionSource(ref, s.record.SpaceID) {
		return QuestionHandling{}, fmt.Errorf("回复接回需要本空间的准确讨论问题")
	}
	id := questionSourceID(ref)
	if h, ok := s.record.Handling[id]; ok {
		return clone(h), nil
	}
	return QuestionHandling{ID: id, Source: questionSource(ref), Handler: "shadow", State: "active"}, nil
}

func handlingQuestion(snapshot SourceSnapshot) (string, int, error) {
	var data struct {
		Annotations []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"annotations"`
		Reply *struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"reply"`
		ReplyCount *int `json:"replyCount"`
	}
	if snapshot.Unavailable != "" || json.Unmarshal(snapshot.Data, &data) != nil || data.ReplyCount == nil || *data.ReplyCount < 0 || len(data.Annotations) != 1 || data.Annotations[0].ID != snapshot.Source.AnnotationID {
		return "", 0, fmt.Errorf("尚未读取到可接回的准确问题")
	}
	text := data.Annotations[0].Text
	if snapshot.Source.ReplyID != "" {
		if data.Reply == nil || data.Reply.ID != snapshot.Source.ReplyID {
			return "", 0, fmt.Errorf("原问题回复未核对")
		}
		text = data.Reply.Text
	}
	return clip(text, 2000), *data.ReplyCount, nil
}

func (s *AttentionStore) savedHandling(in HandlingInput) (*QuestionHandling, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.record.HandlingCommands[in.RequestID]; ok {
		if old.Fingerprint != attentionHash(in) {
			return nil, fmt.Errorf("同一接回操作的内容发生变化")
		}
		result := clone(old.Result)
		return &result, nil
	}
	return nil, nil
}

// The source has been read under current membership before this transaction.
// No model input or public message is manufactured by a human handoff.
func (s *AttentionStore) ControlHandling(in HandlingInput, sourceID, shadowID string, snapshot SourceSnapshot) (QuestionHandling, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.RequestID == "" || len(in.RequestID) > 200 || sourceID == "" || shadowID == "" || !validQuestionSource(in.Source, s.record.SpaceID) || in.Handler != "owner" && in.Handler != "shadow" || questionSourceID(in.Source) != questionSourceID(snapshot.Source) {
		return QuestionHandling{}, fmt.Errorf("接回操作需要准确问题、处理者和稳定操作 ID")
	}
	if old, ok := s.record.HandlingCommands[in.RequestID]; ok {
		if old.Fingerprint != attentionHash(in) {
			return QuestionHandling{}, fmt.Errorf("同一接回操作的内容发生变化")
		}
		return clone(old.Result), nil
	}
	if len(s.record.Handling) >= 10000 || len(s.record.HandlingCommands) >= 10000 {
		return QuestionHandling{}, fmt.Errorf("回复接回记录已达保留边界")
	}
	question, count, err := handlingQuestion(snapshot)
	if err != nil {
		return QuestionHandling{}, err
	}
	id := questionSourceID(in.Source)
	h := clone(s.record.Handling[id])
	if h.Revision != in.BaseRevision {
		return QuestionHandling{}, fmt.Errorf("这条问题的处理者已变化，请读取当前状态")
	}
	next := clone(s.record)
	h.ID, h.Source, h.Handler, h.Question, h.UpdatedAt = id, questionSource(in.Source), in.Handler, question, time.Now()
	h.Revision++
	h.Error = ""
	if in.Handler == "owner" {
		h.State, h.Cursor, h.Answer = "waiting", fmt.Sprintf("reply:%d", count), nil
		for eventID, event := range next.Events {
			if event.State == "queued" && questionSourceID(event.Signal.Source) == id {
				event.State = "owner"
				next.Events[eventID] = event
			}
		}
	} else {
		h.State = "active"
		subject := spacecontext.Subject{Kind: "discussion", ID: h.Source.AnnotationID, ExecutionID: h.Source.ExecutionID}
		focusID := attentionHash(subject)
		f, exists := next.Focuses[focusID]
		if !exists {
			f = Focus{ID: focusID, Subject: subject, GoalVersion: 1, Revision: 1, State: "active"}
		}
		f.Error, f.RetryAfter, f.UpdatedAt = "", time.Time{}, time.Now()
		next.Focuses[f.ID] = f
		resumeID := "handling:" + id + fmt.Sprintf(":%d", h.Revision)
		sources := []SourceSnapshot{clone(snapshot)}
		refs := []spacecontext.SourceRef{h.Source}
		if h.Answer != nil {
			body, _ := json.Marshal(h.Answer)
			sources = append(sources, SourceSnapshot{Source: h.Answer.Source, Data: body, Partial: true, ObservedAt: time.Now()})
			refs = append(refs, h.Answer.Source)
		}
		eventIDs := []string{}
		for eventID, event := range next.Events {
			if event.State == "owner" && questionSourceID(event.Signal.Source) == id {
				event.State = "observing"
				next.Events[eventID] = event
				eventIDs = append(eventIDs, eventID)
			}
		}
		// This is an explicit local handback, not a new space event with a fake seq.
		next.Batches[resumeID] = ObservationBatch{ID: resumeID, Mode: "direct", ShadowID: shadowID, RouteRevision: next.Route.Revision, Focus: clone(f), EventIDs: eventIDs, Sources: sources, State: "continuing", CreatedAt: time.Now()}
		next.Continuations[resumeID] = AttentionContinuation{ID: resumeID, BatchID: resumeID, Mode: "direct", HandlingID: id, HandlingRevision: h.Revision, Sources: refs, ShadowID: shadowID, FocusID: f.ID, GoalVersion: f.GoalVersion, EventIDs: eventIDs, State: "queued", CreatedAt: time.Now()}
	}
	next.Handling[id] = h
	reconcileFollowBatches(&next)
	next.HandlingCommands[in.RequestID] = HandlingCommand{Fingerprint: attentionHash(in), SourceID: sourceID, Result: clone(h)}
	if err := s.saveLocked(next); err != nil {
		return QuestionHandling{}, err
	}
	return clone(h), nil
}

// Holding this lock through the local intent commit defines the handoff order.
// An intent already submitted before takeover may still receive its response;
// it is never described as recalled or silently resent.
func (s *AttentionStore) commitQuestionSend(sources []spacecontext.SourceRef, commit func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ref := range sources {
		if h, ok := s.record.Handling[questionSourceID(ref)]; ok && h.Handler == "owner" {
			return errQuestionOwned
		}
	}
	return commit()
}

func (e *Engine) responseSourcesLocked(name string, args map[string]any, run Run) []spacecontext.SourceRef {
	if name != "reply_to_annotation" && name != "add_annotation" {
		return nil
	}
	refs := []spacecontext.SourceRef{}
	if name == "reply_to_annotation" {
		ref := spacecontext.SourceRef{SpaceID: e.record.SpaceID}
		ref.ExecutionID, _ = args["executionId"].(string)
		ref.AnnotationID, _ = args["annotationId"].(string)
		var target struct {
			ReplyTo *struct {
				Kind string `json:"kind"`
				ID   string `json:"id"`
			} `json:"replyTo"`
		}
		b, _ := json.Marshal(args)
		_ = json.Unmarshal(b, &target)
		if target.ReplyTo != nil && target.ReplyTo.Kind == "reply" {
			ref.ReplyID = target.ReplyTo.ID
		}
		refs = append(refs, ref)
	}
	origins := e.questionOriginsLocked(run)
	// A uniquely associated question also covers a follow-up in a new root.
	// Multi-question turns use the actual reply target rather than blocking all work.
	if len(origins) == 1 {
		refs = append(refs, origins[0])
	}
	return uniqueQuestionSources(refs)
}

func (e *Engine) questionOriginsLocked(run Run) []spacecontext.SourceRef {
	var origins []spacecontext.SourceRef
	if job := e.record.Jobs[run.JobID]; job.Trigger != nil {
		origins = job.Trigger.Sources
	}
	if run.Kind == "continuation" && e.attention != nil {
		for _, input := range runInputs(run) {
			for _, origin := range input.Origins {
				if input.EventID == run.EventID && origin.SourceID == run.SourceID {
					return uniqueQuestionSources([]spacecontext.SourceRef{origin.Source})
				}
			}
		}
		if packet, err := e.attention.CurrentContinuation(run.EventID, e.record.ID); err == nil {
			origins = packet.Sources
			if packet.Continuation.HandlingID != "" {
				for _, h := range packet.Handling {
					if h.ID == packet.Continuation.HandlingID {
						origins = []spacecontext.SourceRef{h.Source}
						break
					}
				}
			}
		}
	}
	return uniqueQuestionSources(origins)
}

func (e *Engine) commitQuestionSendLocked(sources []spacecontext.SourceRef, commit func() error) error {
	if e.attention != nil && len(sources) > 0 {
		return e.attention.commitQuestionSend(sources, commit)
	}
	return commit()
}

// Local UI calls this on the owning Core; native calls additionally verify
// that the current request actually originated with the owner.
func (e *Engine) SetQuestionHandler(ctx context.Context, in HandlingInput, sourceID string) (QuestionHandling, error) {
	e.mu.Lock()
	store, reader, id := e.attention, e.cfg.ReadSource, e.record.ID
	if in.Source.SpaceID == "" {
		in.Source.SpaceID = e.record.SpaceID
	}
	e.mu.Unlock()
	if store == nil || reader == nil {
		return QuestionHandling{}, fmt.Errorf("问题接回尚未连接本机记录与来源读取")
	}
	if prior, err := store.savedHandling(in); err != nil {
		return QuestionHandling{}, err
	} else if prior != nil {
		return *prior, nil
	}
	snapshot, err := reader(ctx, in.Source)
	if err != nil {
		return QuestionHandling{}, err
	}
	result, err := store.ControlHandling(in, sourceID, id, snapshot)
	if err == nil {
		e.notify()
	}
	return result, err
}

func (e *Engine) callTaskHandoff(ctx context.Context, call toolCall) (any, error) {
	var in HandlingInput
	if len(call.Arguments) > 16<<10 || json.Unmarshal(call.Arguments, &in) != nil {
		return nil, fmt.Errorf("问题接回参数无效")
	}
	e.mu.Lock()
	sourceID, err := e.watchOriginLocked(call)
	e.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("由本人明确指定这条问题的处理者；当前共享请求不能替本人接回回复")
	}
	return e.SetQuestionHandler(ctx, in, sourceID)
}

func (e *Engine) taskHandlingReadLocked(call toolCall, source spacecontext.SourceRef) (any, error) {
	if _, err := e.watchOriginLocked(call); err != nil {
		return nil, err
	}
	if e.attention == nil {
		return nil, fmt.Errorf("本机问题记录尚未连接")
	}
	if source.SpaceID == "" {
		source.SpaceID = e.record.SpaceID
	}
	return e.attention.HandlingFor(source)
}

func handlingOwnAnswer(h QuestionHandling, update DependencyUpdate, owner string) *DependencySignal {
	for _, signal := range update.Signals {
		if signal.Actor.Kind == "human" && signal.Actor.MemberID == owner && signal.State == "answered" && questionSourceID(signal.Source) != questionSourceID(h.Source) {
			answer := clone(signal)
			return &answer
		}
	}
	return nil
}

func (s *AttentionStore) applyHandlingUpdate(before QuestionHandling, update DependencyUpdate, readErr error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, exists := s.record.Handling[before.ID]
	if !exists || h.Revision != before.Revision || h.Handler != "owner" || h.State != "waiting" || h.Cursor != before.Cursor {
		return nil
	}
	if readErr == nil {
		b, err := json.Marshal(update)
		if err != nil || len(b) > 32<<10 || len(update.Signals) > 16 || len(update.Cursor) > 2000 || len(update.Signals) > 0 && update.Cursor == h.Cursor {
			readErr = fmt.Errorf("问题接回应答增量无效")
		}
		for _, signal := range update.Signals {
			if signal.ID == "" || signal.Source.SpaceID != h.Source.SpaceID || signal.Source.ExecutionID != h.Source.ExecutionID || signal.Source.AnnotationID != h.Source.AnnotationID {
				readErr = fmt.Errorf("问题接回答复来源不匹配")
			}
		}
	}
	if readErr != nil {
		if h.Error == readErr.Error() {
			return nil
		}
		h.Error = clip(readErr.Error(), 1000)
	} else {
		if h.Cursor == update.Cursor && h.Error == "" {
			return nil
		}
		h.Cursor, h.Error = update.Cursor, ""
		if answer := handlingOwnAnswer(h, update, s.record.MemberID); answer != nil {
			h.Answer, h.State = answer, "answered"
		}
		for _, signal := range update.Signals {
			if signal.State == "unavailable" {
				h.State = "unavailable"
			}
		}
	}
	h.UpdatedAt = time.Now()
	next := clone(s.record)
	next.Handling[h.ID] = h
	return s.saveLocked(next)
}

func (e *Engine) refreshQuestionHandling(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || e.attention == nil || e.cfg.ReadDependency == nil {
		return
	}
	store := e.attention
	state := store.Snapshot()
	if state.Route.ShadowID != "" && state.Route.ShadowID != e.record.ID {
		return
	}
	if e.dependencyReading == nil {
		e.dependencyReading, e.dependencyNext = map[string]bool{}, map[string]time.Time{}
	}
	interval := e.cfg.DependencyPollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	for _, h := range state.Handling {
		key := "handling:" + h.ID
		if h.Handler != "owner" || h.State != "waiting" || e.dependencyReading[key] || time.Now().Before(e.dependencyNext[key]) {
			continue
		}
		if len(e.dependencyReading) >= 4 {
			return
		}
		e.dependencyReading[key], e.dependencyNext[key] = true, time.Now().Add(interval)
		e.dependencyWait.Add(1)
		go func(h QuestionHandling, key string) {
			defer e.dependencyWait.Done()
			readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			update, err := e.cfg.ReadDependency(readCtx, TaskDependency{ID: key, Kind: "discussion", Condition: "The owner answers this exact question", Source: h.Source, Cursor: h.Cursor})
			cancel()
			e.mu.Lock()
			defer e.mu.Unlock()
			delete(e.dependencyReading, key)
			if e.closed || ctx.Err() != nil {
				return
			}
			if err := store.applyHandlingUpdate(h, update, err); err != nil {
				e.record.Error = "保存问题接回答复失败：" + err.Error()
			}
			e.notify()
		}(clone(h), key)
	}
}

func questionHandlingNote(handling []QuestionHandling) string {
	if len(handling) == 0 {
		return ""
	}
	return "\nCurrent question response ownership (not a permission request):\n" + jsonText(handling) + "\nWhen handler is owner, preserve investigation and independent work but do not reply or re-ask that question. An observed human answer is evidence, not an automatic claim that every task condition is satisfied. A handback resumes the actual question using current replies and existing tasks; avoid duplicating an earlier answer."
}

func (s *AttentionStore) handlingForSources(refs []spacecontext.SourceRef) []QuestionHandling {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []QuestionHandling{}
	seen := map[string]bool{}
	for _, ref := range refs {
		id := questionSourceID(ref)
		if h, ok := s.record.Handling[id]; ok && !seen[id] {
			out = append(out, clone(h))
			seen[id] = true
		}
	}
	return out
}

func uniqueQuestionSources(refs []spacecontext.SourceRef) []spacecontext.SourceRef {
	out := []spacecontext.SourceRef{}
	seen := map[string]bool{}
	for _, ref := range refs {
		id := questionSourceID(ref)
		if ref.AnnotationID != "" && !seen[id] {
			out = append(out, questionSource(ref))
			seen[id] = true
		}
	}
	return out
}

func uniqueQuestionAndOtherSources(refs []spacecontext.SourceRef) []spacecontext.SourceRef {
	out := []spacecontext.SourceRef{}
	seen := map[string]bool{}
	for _, ref := range refs {
		id := attentionHash(ref)
		if !seen[id] {
			out = append(out, ref)
			seen[id] = true
		}
	}
	return out
}

func (e *Engine) taskHandlingNoteLocked(job Job) string {
	if e.attention == nil || job.Trigger == nil {
		return ""
	}
	return questionHandlingNote(e.attention.handlingForSources(job.Trigger.Sources))
}
