package shadow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/spacecontext"
)

func handlingSnapshot(ref spacecontext.SourceRef, count int) SourceSnapshot {
	data := map[string]any{"annotations": []any{map[string]any{"id": ref.AnnotationID, "text": "Which version should we support?"}}, "replyCount": count}
	if ref.ReplyID != "" {
		data["reply"] = map[string]any{"id": ref.ReplyID, "text": "Confirm the supported version."}
	}
	b, _ := json.Marshal(data)
	return SourceSnapshot{Source: ref, Data: b, ObservedAt: time.Now()}
}

func controlHandling(t *testing.T, s *AttentionStore, ref spacecontext.SourceRef, handler string, rev uint64) QuestionHandling {
	t.Helper()
	h, err := s.ControlHandling(HandlingInput{RequestID: uuid.NewString(), Source: ref, Handler: handler, BaseRevision: rev}, "owner-command", s.Snapshot().Route.ShadowID, handlingSnapshot(ref, 3))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestQuestionHandlingExactTakeoverReplayAndHandback(t *testing.T) {
	s, receipt, signal := attentionFixture(t)
	sibling := clone(signal)
	sibling.ID = "sibling"
	sibling.Seq = 2
	sibling.Source.ReplyID = "other-question"
	if err := s.Accept(receipt, []spacecontext.Signal{signal, sibling}); err != nil {
		t.Fatal(err)
	}
	in := HandlingInput{RequestID: "takeover", Source: signal.Source, Handler: "owner"}
	h, err := s.ControlHandling(in, "owner-message", "first-shadow", handlingSnapshot(signal.Source, 3))
	if err != nil || h.Cursor != "reply:3" {
		t.Fatal(h, err)
	}
	state := s.Snapshot()
	if state.Events[signal.ID].State != "owner" || state.Events[sibling.ID].State != "queued" || len(state.Continuations) != 0 {
		t.Fatal("takeover changed unrelated work or invoked the model", state)
	}
	if err := s.commitQuestionSend([]spacecontext.SourceRef{signal.Source}, func() error { t.Fatal("owned question reached send"); return nil }); !errors.Is(err, errQuestionOwned) {
		t.Fatal(err)
	}
	if err := s.commitQuestionSend([]spacecontext.SourceRef{sibling.Source}, func() error { return nil }); err != nil {
		t.Fatal("sibling blocked", err)
	}
	fid := attentionHash(signal.Subject)
	s.mu.Lock()
	f := s.record.Focuses[fid]
	f.State = "paused"
	f.GoalVersion++
	s.record.Focuses[fid] = f
	s.mu.Unlock()
	back := controlHandling(t, s, signal.Source, "shadow", h.Revision)
	if back.Revision != 2 {
		t.Fatal(back)
	}
	if _, err := s.ControlHandling(in, "owner-message", "first-shadow", handlingSnapshot(signal.Source, 3)); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.HandlingFor(signal.Source); got.Handler != "shadow" {
		t.Fatal("replayed takeover undid newer handback", got)
	}
	// Handback is an explicit question, independent of a paused ordinary follow.
	packet, err := s.ClaimContinuation("first-shadow")
	if err != nil || packet == nil || len(packet.Continuation.EventIDs) != 1 || len(packet.Sources) != 1 {
		t.Fatal(packet, err)
	}
	if len(s.Snapshot().Events) != 2 {
		t.Fatal("handback forged a space event")
	}
	s.mu.Lock()
	changed := s.record.Focuses[fid]
	changed.GoalVersion++
	changed.Summary = "New follow goal understanding"
	s.record.Focuses[fid] = changed
	s.mu.Unlock()
	if _, err := s.CurrentContinuation(packet.Continuation.ID, "first-shadow"); err != nil {
		t.Fatal("ordinary follow goal cancelled explicit handback", err)
	}
	output := jsonText(Observation{Summary: "Read the exact question and existing answer; no new reply is needed.", Outcome: "no_action", Evidence: packet.Sources})
	if err := s.FinishContinuation(packet.Continuation.ID, "first-shadow", "completed", output, nil, ContinuationWork{}); err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().Events[signal.ID].State != "processed" || s.Snapshot().Events[sibling.ID].State != "queued" {
		t.Fatal("handback failed to acknowledge only its source")
	}
	if s.Snapshot().Focuses[fid].Summary != "New follow goal understanding" || s.Snapshot().Continuations[packet.Continuation.ID].State != "processed" {
		t.Fatal("handback overwrote new goal understanding or was lost")
	}
	reopened, err := OpenAttention(s.directory, "space", "owner")
	if err != nil || !reflect.DeepEqual(s.Snapshot(), reopened.Snapshot()) {
		t.Fatal("restart lost question ownership", err)
	}
}

func TestQuestionHandlingAnswerCursorAndFailureRollback(t *testing.T) {
	s, _, signal := attentionFixture(t)
	h := controlHandling(t, s, signal.Source, "owner", 0)
	peer := DependencySignal{ID: "peer", Source: spacecontext.SourceRef{SpaceID: "space", AnnotationID: "discussion", ReplyID: "peer-answer"}, Actor: spacecontext.Actor{Kind: "human", MemberID: "peer"}, State: "answered"}
	if err := s.applyHandlingUpdate(h, DependencyUpdate{Cursor: "reply:4", Signals: []DependencySignal{peer}}, nil); err != nil {
		t.Fatal(err)
	}
	h, _ = s.HandlingFor(signal.Source)
	if h.State != "waiting" || h.Cursor != "reply:4" {
		t.Fatal(h)
	}
	answer := clone(peer)
	answer.ID = "owner"
	answer.Actor.MemberID = "owner"
	answer.Source.ReplyID = "owner-answer"
	before := s.Snapshot()
	original := s.directory
	s.directory = filepath.Join(original, "not-a-directory")
	if err := os.WriteFile(s.directory, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.applyHandlingUpdate(h, DependencyUpdate{Cursor: "reply:5", Signals: []DependencySignal{answer}}, nil); err == nil {
		t.Fatal("disk failure advanced answer")
	}
	s.directory = original
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("failed persistence changed memory")
	}
	if err := s.applyHandlingUpdate(h, DependencyUpdate{Cursor: "reply:5", Signals: []DependencySignal{answer}}, nil); err != nil {
		t.Fatal(err)
	}
	answered, _ := s.HandlingFor(signal.Source)
	if answered.State != "answered" || answered.Answer == nil || answered.Answer.Source.ReplyID != "owner-answer" {
		t.Fatal(answered)
	}
	back := controlHandling(t, s, signal.Source, "shadow", answered.Revision)
	if back.Answer == nil {
		t.Fatal("handback lost the human answer")
	}
	packet, err := s.ClaimContinuation("first-shadow")
	if err != nil || packet == nil || len(packet.SourceBodies) != 2 || len(packet.Continuation.EventIDs) != 0 {
		t.Fatal(packet, err)
	}
	if err := s.applyHandlingUpdate(h, DependencyUpdate{Cursor: "reply:99"}, nil); err != nil {
		t.Fatal(err)
	}
	if latest, _ := s.HandlingFor(signal.Source); latest.Cursor != "reply:5" {
		t.Fatal("late old polling changed handed-back source", latest)
	}
}

func TestQuestionHandlingNoWriteAfterTakeoverAndInflightReceiptPreserved(t *testing.T) {
	e, f, _ := setup(t)
	connectQuestionTools(e)
	s, signal := attachEngineAttention(t, e)
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	var calls atomic.Int32
	e.cfg.CallTool = func(_ context.Context, _ string, args map[string]any) (any, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return map[string]any{"status": "saved", "requestId": args["requestId"]}, nil
	}
	e.Start()
	send(t, e, "owner", "Answer the exact question")
	run := active(t, e, "main")
	args := map[string]any{"annotationId": "discussion", "replyTo": map[string]any{"kind": "reply", "id": signal.Source.ReplyID}, "requestId": uuid.NewString(), "text": "Answer"}
	call := askCall(run, args)
	call.Tool = "reply_to_annotation"
	done := make(chan error, 1)
	go func() { _, err := e.callCollaborationTool(context.Background(), call); done <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("send did not start")
	}
	controlHandling(t, s, signal.Source, "owner", 0)
	args["requestId"] = uuid.NewString()
	call2 := askCall(run, args)
	call2.Tool = "reply_to_annotation"
	if _, err := e.callCollaborationTool(context.Background(), call2); !errors.Is(err, errQuestionOwned) {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("new send crossed takeover")
	}
	// Release the in-flight call without closing the channel twice.
	release <- struct{}{}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	a := e.record.Actions[args["requestId"].(string)]
	e.mu.Unlock()
	if a.State != "held_for_owner" || a.SubmittedAt != nil {
		t.Fatal(a)
	}
	f.complete(run.ThreadID, "Done", "completed", true)
}

func TestQuestionHandlingPreventsAskReservationAndRequiresOwnerOrigin(t *testing.T) {
	e, f, _ := setup(t)
	connectQuestionTools(e)
	s, signal := attachEngineAttention(t, e)
	e.cfg.ReadSource = func(_ context.Context, ref spacecontext.SourceRef) (SourceSnapshot, error) {
		return handlingSnapshot(ref, 0), nil
	}
	controlHandling(t, s, signal.Source, "owner", 0)
	e.Start()
	send(t, e, "owner", "Ask about the question")
	run := active(t, e, "main")
	args := questionCommand()
	qargs := args["question"].(map[string]any)["arguments"].(map[string]any)
	qargs["replyTo"] = map[string]any{"kind": "reply", "id": signal.Source.ReplyID}
	if _, err := e.callTaskAsk(context.Background(), askCall(run, args)); !errors.Is(err, errQuestionOwned) {
		t.Fatal(err)
	}
	e.mu.Lock()
	questions, jobs := len(e.record.Questions), len(e.record.Jobs)
	e.mu.Unlock()
	if questions != 0 || jobs != 0 {
		t.Fatal("takeover left an unsent question and task", questions, jobs)
	}
	f.complete(run.ThreadID, "Kept the question with its owner.", "completed", true)
	until(t, func() bool { e.mu.Lock(); defer e.mu.Unlock(); return !activeRun(e.record.Runs[run.ID]) })
	send(t, e, "other-member", "The owner should hand it back")
	peer := active(t, e, "main")
	_, err := e.callTaskHandoff(context.Background(), askCall(peer, HandlingInput{RequestID: uuid.NewString(), Source: signal.Source, Handler: "shadow", BaseRevision: 1}))
	if err == nil {
		t.Fatal("peer changed owner response assignment")
	}
}

func TestQuestionHandlingUniqueOriginProtectsNewRootButNotSiblingWork(t *testing.T) {
	e, _, _ := setup(t)
	s, signal := attachEngineAttention(t, e)
	controlHandling(t, s, signal.Source, "owner", 0)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.record.Jobs["one"] = Job{ID: "one", Trigger: &TaskTrigger{Sources: []spacecontext.SourceRef{signal.Source, signal.Source}}}
	refs := e.responseSourcesLocked("add_annotation", nil, Run{JobID: "one"})
	if err := e.commitQuestionSendLocked(refs, func() error { return nil }); !errors.Is(err, errQuestionOwned) {
		t.Fatal("new root bypassed original question", err)
	}
	other := signal.Source
	other.ReplyID = "other"
	j := e.record.Jobs["one"]
	j.Trigger.Sources = append(j.Trigger.Sources, other)
	e.record.Jobs[j.ID] = j
	refs = e.responseSourcesLocked("reply_to_annotation", map[string]any{"annotationId": "discussion", "replyTo": map[string]any{"kind": "reply", "id": "other"}}, Run{JobID: "one"})
	if err := e.commitQuestionSendLocked(refs, func() error { return nil }); err != nil {
		t.Fatal("mixed task lost independent sibling", err)
	}
	if !strings.Contains(e.taskHandlingNoteLocked(j), "owner") {
		t.Fatal("worker omitted current question handling")
	}
}

func TestQuestionHandlingHandbackRunsWithoutSyntheticEventOrUserMessage(t *testing.T) {
	e, f, _ := setup(t)
	connectQuestionTools(e)
	s, signal := attachEngineAttention(t, e)
	e.cfg.ReadSource = func(_ context.Context, ref spacecontext.SourceRef) (SourceSnapshot, error) {
		return handlingSnapshot(ref, 0), nil
	}
	h, err := e.SetQuestionHandler(context.Background(), HandlingInput{RequestID: "take", Source: signal.Source, Handler: "owner"}, "local-ui:take")
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.SetQuestionHandler(context.Background(), HandlingInput{RequestID: "back", Source: signal.Source, Handler: "shadow", BaseRevision: h.Revision}, "local-ui:back")
	if err != nil {
		t.Fatal(err)
	}
	e.Start()
	run := active(t, e, "continuation")
	if run.SourceID != run.EventID || len(s.Snapshot().Events) != 0 || len(e.View().Messages) != 0 {
		t.Fatal("handback invented a space event or shared user message", run)
	}
	f.complete(run.ThreadID, observationJSON(t, run, Observation{Summary: "The original human answer is enough; no additional reply is needed.", Outcome: "no_action", Evidence: []spacecontext.SourceRef{signal.Source}}), "completed", true)
	until(t, func() bool { return attentionCommitted(e, run.ID) })
	c := s.Snapshot().Continuations[run.EventID]
	if c.State != "processed" || len(e.View().Messages) != 0 {
		t.Fatal(c, e.View().Messages)
	}
}
