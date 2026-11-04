package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/mcp"
	"teamcross/internal/spacecontext"
)

func directAttention(t *testing.T) (*Engine, *fakeNative, Config, *AttentionStore, spacecontext.Signal) {
	t.Helper()
	e, f, cfg := setup(t)
	e.cfg.ReadSource, cfg.ReadSource = observationReader, observationReader
	e.cfg.CollaborationTools, cfg.CollaborationTools = mcp.ShadowTools(), mcp.ShadowTools()
	call := func(_ context.Context, _ string, args map[string]any) (any, error) {
		return map[string]any{"status": "saved", "requestId": args["requestId"]}, nil
	}
	e.cfg.CallTool, cfg.CallTool = call, call
	s, signal := attachEngineAttention(t, e)
	signal.Reasons = []string{"mention", "direct_reply", "follow_discussion"}
	return e, f, cfg, s, signal
}

func TestAttentionDirectMentionActsAndUnderstandsInOneTurn(t *testing.T) {
	e, f, _, s, signal := directAttention(t)
	acceptAttentionSignals(t, s, signal)
	e.Start()
	run := active(t, e, "continuation")
	f.mu.Lock()
	prompt := itemText(f.threads[run.ThreadID][0].Items[0])
	f.mu.Unlock()
	if !strings.Contains(prompt, "Exact body includes") || strings.Contains(prompt, `"role":"user"`) {
		t.Fatal("direct context lacks exact source or invents a member message", prompt)
	}
	id := uuid.NewString()
	tool(t, e, f, run, "reply_to_annotation", map[string]any{"annotationId": signal.Source.AnnotationID, "replyTo": map[string]any{"kind": "reply", "id": signal.Source.ReplyID}, "requestId": id, "text": "The supported version is v3."})
	result := Observation{Summary: "Answered the concrete compatibility question.", Evidence: []spacecontext.SourceRef{signal.Source}, Outcome: "no_action"}
	f.complete(run.ThreadID, observationJSON(t, run, result), "completed", true)
	until(t, func() bool { return attentionCommitted(e, run.ID) })
	record := s.Snapshot()
	c := record.Continuations[run.EventID]
	if c.Mode != "direct" || c.State != "processed" || len(c.ActionIDs) != 1 || c.ActionIDs[0] != id || record.Events[signal.ID].State != "processed" || len(e.View().Messages) != 0 {
		t.Fatal("direct handling was not committed privately", c, record.Events)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.record.Runs) != 1 || e.record.Runs[run.ID].Kind != "continuation" {
		t.Fatal("one input unnecessarily ran observation and coordination", e.record.Runs)
	}
}

func TestAttentionDirectReplyDoesNotRequireUnderstandingJSON(t *testing.T) {
	e, f, _, s, signal := directAttention(t)
	acceptAttentionSignals(t, s, signal)
	e.Start()
	run := active(t, e, "continuation")
	id := uuid.NewString()
	tool(t, e, f, run, "reply_to_annotation", map[string]any{"annotationId": "discussion", "requestId": id, "text": "One saved answer."})
	f.complete(run.ThreadID, "Answer sent but no structured understanding.", "completed", true)
	until(t, func() bool { return attentionCommitted(e, run.ID) })
	r := s.Snapshot()
	c, focus := r.Continuations[run.EventID], r.Focuses[r.Events[signal.ID].FocusID]
	if c.State != "processed" || len(c.ActionIDs) != 1 || focus.ProcessedSeq != signal.Seq || focus.Error != "" || focus.Summary != "" || r.Events[signal.ID].State != "processed" {
		t.Fatal("ordinary reply depended on summary JSON or fabricated facts", c, focus)
	}
	if batch, err := s.ClaimBatch(e.View().ID, time.Now().Add(time.Hour), 0, 0); err != nil || batch != nil {
		t.Fatal("failed direct action was automatically repeated", batch, err)
	}
}

func TestAttentionDirectRecoveryReusesNativeCompletion(t *testing.T) {
	e, f, cfg, s, signal := directAttention(t)
	acceptAttentionSignals(t, s, signal)
	e.Start()
	run := active(t, e, "continuation")
	id := uuid.NewString()
	tool(t, e, f, run, "reply_to_annotation", map[string]any{"annotationId": "discussion", "requestId": id, "text": "Persist this one reply."})
	e.Close()
	f.complete(run.ThreadID, observationJSON(t, run, Observation{Summary: "The answer was saved.", Outcome: "no_action"}), "completed", false)
	e2, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e2.Close)
	store, err := OpenAttention(s.directory, "space-test", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err = e2.AttachAttention(store); err != nil {
		t.Fatal(err)
	}
	e2.Start()
	until(t, func() bool { return attentionCommitted(e2, run.ID) })
	f.mu.Lock()
	turns := len(f.threads[run.ThreadID])
	f.mu.Unlock()
	if turns != 1 || store.Snapshot().Events[signal.ID].State != "processed" || len(e2.View().Messages) != 0 {
		t.Fatal("restart replayed the direct action or lost its completion", turns)
	}
}

func TestAttentionBodyPrefixChoosesItsActualProcessingPath(t *testing.T) {
	s, receipt, signal := attentionFixture(t)
	direct := clone(signal)
	direct.ID, direct.Seq, direct.Source.ReplyID, direct.Reasons = "later-direct", 2, "later-question", []string{"mention"}
	acceptAttentionSignals(t, s, signal, direct)
	batch, err := s.ClaimBatch(receipt.Route.ShadowID, time.Now(), time.Hour, time.Hour)
	if err != nil || batch == nil || batch.Mode != "direct" {
		t.Fatal(batch, err)
	}
	batch = prepareObservation(t, s, batch, 1)
	if batch.Mode != "observe" || len(s.Snapshot().Continuations) != 0 || s.Snapshot().Events[direct.ID].State != "queued" {
		t.Fatal("a source outside the body prefix selected direct handling", batch)
	}
}

func exactAnswerResolver(_ context.Context, d TaskDependency, signal spacecontext.Signal) (*DependencySignal, error) {
	if signal.Source.ReplyID != "answer" || d.Source.ReplyID != "question" {
		return nil, nil
	}
	return &DependencySignal{ID: "reply:answer", Source: signal.Source, State: "answered", Preview: "Support client v3."}, nil
}

func testWaitingJob() Job {
	return Job{ID: "job", State: "waiting", Version: 1, Revision: 1, Dependencies: []TaskDependency{{ID: "version", Kind: "discussion", State: "waiting", Source: spacecontext.SourceRef{SpaceID: "space-test", AnnotationID: "discussion", ReplyID: "question"}}}}
}

func TestAttentionTaskReceiptJoinsPollingAndActivityAcrossRestart(t *testing.T) {
	for _, pollFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "poll-first", false: "activity-first"}[pollFirst], func(t *testing.T) {
			e, _, cfg, store, signal := directAttention(t)
			e.cfg.ResolveDependency, cfg.ResolveDependency = exactAnswerResolver, exactAnswerResolver
			signal.Source.ReplyID = "answer"
			job := testWaitingJob()
			e.mu.Lock()
			err := e.mutateLocked(func() error { e.record.Jobs[job.ID] = job; return nil })
			e.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			answer, _ := exactAnswerResolver(context.Background(), job.Dependencies[0], signal)
			if pollFirst {
				e.mu.Lock()
				err = e.applyDependencyUpdateLocked(job.ID, job.Version, job.Dependencies[0], DependencyUpdate{Cursor: "reply:2", Signals: []DependencySignal{*answer}}, nil)
				e.mu.Unlock()
				if err != nil {
					t.Fatal(err)
				}
			}
			acceptAttentionSignals(t, store, signal)
			batch, err := store.ClaimBatch(e.View().ID, time.Now(), 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			// Save the task answer, then lose the attention routing write.
			delivered, err := e.RouteDependencyActivity(context.Background(), signal)
			if err != nil || len(delivered) != 1 {
				t.Fatal(delivered, err)
			}
			e2, err := Open(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(e2.Close)
			if err := e2.AttachAttention(store); err != nil {
				t.Fatal(err)
			}
			batch, err = e2.routeBatch(context.Background(), store, batch.ID)
			if err != nil || batch.State != "task" {
				t.Fatal(batch, err)
			}
			received := currentJob(t, e2, job.ID)
			if len(received.Dependencies[0].Signals) != 1 || !received.Dependencies[0].Received[answer.ID] || store.Snapshot().Events[signal.ID].State != "task" {
				t.Fatal("same answer entered two paths", received, store.Snapshot())
			}
			// The task considered the answer but still needs another detail.
			received.State, received.Dependencies[0].Signals = "waiting", nil
			e2.mu.Lock()
			e2.record.Jobs[job.ID] = received
			err = e2.applyDependencyUpdateLocked(job.ID, job.Version, received.Dependencies[0], DependencyUpdate{Cursor: "reply:2", Signals: []DependencySignal{*answer}}, nil)
			e2.mu.Unlock()
			if err != nil || currentJob(t, e2, job.ID).State != "waiting" || len(currentJob(t, e2, job.ID).Dependencies[0].Signals) != 0 {
				t.Fatal("poll rediscovered an already considered activity answer", err)
			}
		})
	}
}

func TestAttentionTaskRoutingFailureKeepsBothStoresUnconsumed(t *testing.T) {
	e, _, _, s, signal := directAttention(t)
	e.cfg.ResolveDependency = exactAnswerResolver
	signal.Source.ReplyID = "answer"
	job := testWaitingJob()
	e.mu.Lock()
	err := e.mutateLocked(func() error { e.record.Jobs[job.ID] = job; return nil })
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	acceptAttentionSignals(t, s, signal)
	batch, _ := s.ClaimBatch(e.View().ID, time.Now(), 0, 0)
	path := filepath.Join(e.cfg.Directory, "shadow.json")
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := e.routeBatch(context.Background(), s, batch.ID); err == nil {
		t.Fatal("lost task write was acknowledged")
	}
	if currentJob(t, e, job.ID).State != "waiting" || len(currentJob(t, e, job.ID).Dependencies[0].Received) != 0 || s.Snapshot().Events[signal.ID].State == "task" {
		t.Fatal("failed persistence consumed the answer")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	if batch, err := e.routeBatch(context.Background(), s, batch.ID); err != nil || batch.State != "task" {
		t.Fatal(batch, err)
	}
}

func TestAttentionQueuedDirectAnswerRechecksNewWaitBeforeNativeTurn(t *testing.T) {
	e, f, _, s, signal := directAttention(t)
	e.cfg.ResolveDependency = exactAnswerResolver
	e.cfg.ReadDependency = func(_ context.Context, d TaskDependency) (DependencyUpdate, error) {
		return DependencyUpdate{Cursor: d.Cursor}, nil
	}
	e.Start()
	send(t, e, "owner", "Coordinate this question")
	main := active(t, e, "main")
	worker := startWorkingTask(t, e, f, main)
	e.mu.Lock()
	e.approvals["hold-input"] = Approval{Params: json.RawMessage(`{"threadId":"` + main.ThreadID + `"}`)}
	e.mu.Unlock()
	signal.Source.ReplyID = "answer"
	acceptAttentionSignals(t, s, signal)
	until(t, func() bool { return len(s.Snapshot().Continuations) == 1 })
	args := taskCommandFor(currentJob(t, e, worker.JobID), "wait")
	d := testWaitingJob().Dependencies[0]
	d.Condition = "The compatible version is stated."
	args["dependencies"] = []TaskDependency{d}
	tool(t, e, f, worker, "shadow_task", args)
	e.mu.Lock()
	delete(e.approvals, "hold-input")
	e.mu.Unlock()
	// Queued but not yet accepted inputs must recheck exact Work ownership.
	f.complete(main.ThreadID, "The question is being followed up.", "completed", true)
	until(t, func() bool { return s.Snapshot().Events[signal.ID].State == "task" })
	f.complete(worker.ThreadID, "Waiting on the exact answer.", "completed", true)
	next := nextJobRun(t, e, worker)
	if next.ThreadID != worker.ThreadID {
		t.Fatal("answer lost original worker identity")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, run := range e.record.Runs {
		if run.Kind == "attention" || run.Kind == "continuation" {
			t.Fatal("known answer also ran a second handler", run)
		}
	}
}

func TestAttentionDirectContinuationPersistsUnderstandingAfterStorageRepair(t *testing.T) {
	e, f, _, s, signal := directAttention(t)
	acceptAttentionSignals(t, s, signal)
	e.Start()
	run := active(t, e, "continuation")
	path := filepath.Join(s.directory, "attention.json")
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	f.complete(run.ThreadID, observationJSON(t, run, Observation{Summary: "A copied update needs no public answer.", Outcome: "no_action"}), "completed", true)
	until(t, func() bool { return e.View().Error != "" })
	if attentionCommitted(e, run.ID) || s.Snapshot().Events[signal.ID].State == "processed" {
		t.Fatal("failed save committed understanding")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	until(t, func() bool { return attentionCommitted(e, run.ID) })
	f.mu.Lock()
	turns := len(f.threads[run.ThreadID])
	f.mu.Unlock()
	if turns != 1 {
		t.Fatal("storage repair repeated a direct turn")
	}
}

func TestAttentionSlowPreparationAndRouteCheckDoNotBlockHumanChat(t *testing.T) {
	for _, stage := range []string{"source", "initial-route", "final-route"} {
		t.Run(stage, func(t *testing.T) {
			e, f, _, s, signal := directAttention(t)
			entered, release := make(chan struct{}, 1), make(chan struct{})
			defer close(release)
			wait := func(ctx context.Context) error {
				select {
				case entered <- struct{}{}:
				default:
				}
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			if stage == "source" {
				e.cfg.ReadSource = func(ctx context.Context, ref spacecontext.SourceRef) (SourceSnapshot, error) {
					if err := wait(ctx); err != nil {
						return SourceSnapshot{}, err
					}
					return observationReader(ctx, ref)
				}
			} else {
				var calls atomic.Int32
				e.cfg.RouteActivity = func(ctx context.Context, _ spacecontext.Signal) ([]TaskDelivery, error) {
					count := calls.Add(1)
					if stage == "initial-route" || count > 1 {
						return nil, wait(ctx)
					}
					return nil, nil
				}
			}
			acceptAttentionSignals(t, s, signal)
			e.Start()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("preparation did not begin")
			}
			send(t, e, "owner", "Respond while the background source is slow")
			started := time.Now()
			main := active(t, e, "main")
			if time.Since(started) >= 2*time.Second {
				t.Fatal("human message waited for background network")
			}
			f.complete(main.ThreadID, "The foreground conversation remains responsive.", "completed", true)
			until(t, func() bool { return len(e.View().Messages) == 2 })
		})
	}
}

func TestAttentionRouteFailureStaysLocalAndDoesNotSpin(t *testing.T) {
	e, f, _, s, signal := directAttention(t)
	var calls atomic.Int32
	e.cfg.RouteActivity = func(context.Context, spacecontext.Signal) ([]TaskDelivery, error) {
		calls.Add(1)
		return nil, fmt.Errorf("source is temporarily offline")
	}
	acceptAttentionSignals(t, s, signal)
	e.Start()
	until(t, func() bool { return s.Snapshot().Focuses[s.Snapshot().Events[signal.ID].FocusID].Error != "" })
	send(t, e, "owner", "Continue our conversation")
	main := active(t, e, "main")
	f.complete(main.ThreadID, "The unavailable question remains queued.", "completed", true)
	until(t, func() bool { return len(e.View().Messages) == 2 })
	if !e.View().Enabled || calls.Load() != 1 || s.Snapshot().Events[signal.ID].State != "queued" {
		t.Fatal("a route failure disabled chat or entered a retry loop", e.View(), calls.Load())
	}
}

func TestAttentionDirectFinalCannotPromoteUnverifiedEvidence(t *testing.T) {
	for _, read := range []bool{true, false} {
		t.Run(fmt.Sprintf("read-%t", read), func(t *testing.T) {
			e, f, _, s, signal := directAttention(t)
			acceptAttentionSignals(t, s, signal)
			e.Start()
			run := active(t, e, "continuation")
			ref := spacecontext.SourceRef{SpaceID: "space-test", MaterialID: "compatibility", Version: 2}
			if read {
				tool(t, e, f, run, "read_material", map[string]any{"materialId": ref.MaterialID, "version": ref.Version})
			}
			f.complete(run.ThreadID, observationJSON(t, run, Observation{Summary: "The compatibility evidence establishes support.", Evidence: []spacecontext.SourceRef{ref}, Outcome: "no_action"}), "completed", true)
			until(t, func() bool { return attentionCommitted(e, run.ID) })
			got := s.Snapshot().Continuations[run.EventID]
			if got.State != "processed" || len(got.Analysis.Evidence) != 0 {
				t.Fatal("ordinary processing inferred facts from final text", got)
			}
			actual := currentRun(e, run.ID).ReadSources
			if (len(actual) == 1) != read {
				t.Fatal("read receipt did not track actual successful tool use", actual)
			}
		})
	}
}

func TestAttentionDeliveryReceiptsStayOutOfSharedAndModelContexts(t *testing.T) {
	e, _, _ := setup(t)
	job := testWaitingJob()
	job.Dependencies[0].Received = map[string]bool{"INTERNAL_DELIVERY_RECEIPT": true}
	job.Dependencies[0].Signals = []DependencySignal{{ID: "pending-answer", Preview: "CURRENT_ANSWER_NEEDED_BY_TASK"}}
	e.mu.Lock()
	e.record.Jobs[job.ID] = job
	worker := e.workerPromptLocked(job)
	e.mu.Unlock()
	view := jsonText(e.View())
	if strings.Contains(view, "INTERNAL_DELIVERY_RECEIPT") || strings.Contains(worker, "INTERNAL_DELIVERY_RECEIPT") || !strings.Contains(worker, "CURRENT_ANSWER_NEEDED_BY_TASK") {
		t.Fatal("routing receipts displaced relevant task context", view, worker)
	}
	if !currentJob(t, e, job.ID).Dependencies[0].Received["INTERNAL_DELIVERY_RECEIPT"] {
		t.Fatal("projection erased durable deduplication")
	}
}
