package shadow

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"teamcross/internal/spacecontext"
)

func TestTaskSourceChangesResumeSameWorkerWithoutFollowAfterRestart(t *testing.T) {
	e, native, cfg := setup(t)
	store, _ := attachEngineAttention(t, e)
	f := saveTaskContextFacts(t, store, "interface", "VERSION_ONE", 1)
	var changed atomic.Bool
	reader := func(_ context.Context, query TaskSourceQuery) (TaskSourceUpdate, error) {
		out := TaskSourceUpdate{Cursor: query.Cursor}
		out.Cursor.Initialized = true
		if changed.Load() && query.Cursor.Seq < 2 {
			out.Cursor.Seq = 2
			out.Signals = []DependencySignal{{ID: "material-event-two", Source: spacecontext.SourceRef{SpaceID: "space-test", MaterialID: "interface", Version: 2}, State: "material_updated", Preview: "VERSION_TWO"}}
		}
		return out, nil
	}
	e.cfg.ReadTaskSource, cfg.ReadTaskSource = reader, reader
	e.Start()
	send(t, e, "owner", "Continue this independent migration using the current interface facts")
	main := active(t, e, "main")
	created := cloneMap(tool(t, e, native, main, "shadow_task", map[string]any{"operation": "start", "title": "Migration", "prompt": "Use current source facts", "completionCriteria": []string{"Use version two"}, "relatedFocuses": []TaskContextInput{{ID: f.ID, Reason: "Current interface semantics"}}}))
	worker := jobRun(t, e, created["id"].(string))
	until(t, func() bool {
		j := currentJob(t, e, worker.JobID)
		return j.Contexts[0].Source != nil && j.Contexts[0].Source.Cursor.Initialized
	})
	j := currentJob(t, e, worker.JobID)
	wait := taskCommandFor(j, "wait")
	wait["contextReviews"] = reviewTaskContexts(j)
	tool(t, e, native, worker, "shadow_task", wait)
	native.complete(worker.ThreadID, "Waiting for changed facts", "completed", true)
	until(t, func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.record.Jobs[j.ID].State == "waiting" && !e.taskRunActiveLocked(j.ID)
	})
	e.Close()
	store.mu.Lock()
	next := clone(store.record)
	if next.Follows == nil {
		next.Follows = map[string]spacecontext.FollowSetting{}
	}
	next.Follows[attentionHash(f.Subject)] = spacecontext.FollowSetting{Subject: f.Subject, Mode: "muted", Revision: 2}
	paused := next.Focuses[f.ID]
	paused.State = "paused"
	next.Focuses[f.ID] = paused
	if err := store.saveLocked(next); err != nil {
		t.Fatal(err)
	}
	store.mu.Unlock()
	changed.Store(true)
	e2, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer e2.Close()
	if err = e2.AttachAttention(store); err != nil {
		t.Fatal(err)
	}
	e2.Start()
	resumed := nextJobRun(t, e2, worker)
	j = currentJob(t, e2, j.ID)
	if resumed.ThreadID != worker.ThreadID || j.Version != 1 || j.Follow != nil || len(j.Contexts[0].Source.Signals) != 1 {
		t.Fatal("source change did not resume original independent work", j)
	}
	e2.mu.Lock()
	targets := e2.taskUnderstandingTargetsLocked(e2.record.Jobs[j.ID])
	e2.mu.Unlock()
	if len(targets) != 1 || len(targets[0].Answers) != 1 {
		t.Fatal(targets)
	}
	target := targets[0]
	args := map[string]any{"operation": "understand", "id": j.ID, "baseVersion": j.Version, "baseRevision": j.Revision, "requestId": "new-interface-facts", "understandings": []TaskUnderstandingUpdate{{ID: target.ID, BaseRevision: target.Current.Revision, SignalIDs: target.Answers, Summary: "VERSION_TWO", Evidence: []spacecontext.SourceRef{j.Contexts[0].Source.Signals[0].Source}}}}
	if err = taskToolError(e2, resumed, args); err != nil {
		t.Fatal(err)
	}
	if err = e2.syncTaskFollows(); err != nil {
		t.Fatal(err)
	}
	j = currentJob(t, e2, j.ID)
	if j.Contexts[0].Current.Summary != "VERSION_TWO" || len(j.Contexts[0].Source.Signals) != 0 || j.Contexts[0].Reviewed.Summary != "VERSION_ONE" {
		t.Fatal("facts and prior task checkpoint were not preserved", j)
	}
	finish := taskCommandFor(j, "finish")
	finish["contextReviews"] = reviewTaskContexts(j)
	finish["outcome"], finish["result"], finish["criteriaChecks"] = "completed", "Migrated with v2", []string{"Read v2 and checked compatibility"}
	if err = taskToolError(e2, resumed, finish); err != nil {
		t.Fatal(err)
	}
	if len(e2.View().Jobs[0].Contexts) != 0 || len(store.Snapshot().Continuations) != 0 {
		t.Fatal("private source changes leaked or created a second coordinator")
	}
}

func sourceApplyFixture(t *testing.T) (*Engine, Config, *AttentionStore, TaskContext) {
	t.Helper()
	e, _, cfg := setup(t)
	store, _ := attachEngineAttention(t, e)
	f := saveTaskContextFacts(t, store, "material", "KNOWN_V1", 1)
	c := TaskContext{ID: f.ID, Subject: f.Subject, Reason: "Current facts", Current: contextSnapshot(f), Reviewed: contextSnapshot(f), Source: &TaskSourceState{Cursor: TaskSourceCursor{Seq: 1, Initialized: true}}}
	e.mu.Lock()
	err := e.mutateLocked(func() error {
		e.record.Jobs["task"] = Job{ID: "task", Version: 1, Revision: 1, State: "waiting", Contexts: []TaskContext{c}}
		return nil
	})
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	return e, cfg, store, c
}

func sourceApplyPage() TaskSourceUpdate {
	return TaskSourceUpdate{Cursor: TaskSourceCursor{Seq: 2, Initialized: true}, Signals: []DependencySignal{{ID: "event-two", State: "material_updated", Source: spacecontext.SourceRef{SpaceID: "space-test", MaterialID: "material", Version: 2}, Preview: "NEW_V2"}}}
}

func TestTaskSourceCursorAndSignalCommitTogether(t *testing.T) {
	e, cfg, _, c := sourceApplyFixture(t)
	before := currentJob(t, e, "task")
	if err := os.Rename(cfg.Directory, cfg.Directory+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.Directory, []byte("block storage"), 0600); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	err := e.applyTaskSourceLocked("task", 1, c, sourceApplyPage(), nil)
	e.mu.Unlock()
	if err == nil || jsonText(before) != jsonText(currentJob(t, e, "task")) {
		t.Fatal("failed save consumed source cursor", err)
	}
	if err = os.Remove(cfg.Directory); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(cfg.Directory+"-saved", cfg.Directory); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	err = e.applyTaskSourceLocked("task", 1, c, sourceApplyPage(), nil)
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	e.Close()
	restored, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	j := currentJob(t, restored, "task")
	if j.State != "queued" || j.Contexts[0].Source.Cursor.Seq != 2 || len(j.Contexts[0].Source.Signals) != 1 {
		t.Fatal("restart lost an acknowledged source page", j)
	}
}

func TestTaskSourceRejectsForeignInvalidAndStaleResults(t *testing.T) {
	for _, scenario := range []string{"foreign", "wrong-object", "regressed", "duplicate", "oversize", "paused", "completed", "old-version", "snapshot", "read-error"} {
		t.Run(scenario, func(t *testing.T) {
			e, _, _, c := sourceApplyFixture(t)
			page := sourceApplyPage()
			version := 1
			var readErr error
			e.mu.Lock()
			j := e.record.Jobs["task"]
			switch scenario {
			case "foreign":
				page.Signals[0].Source.SpaceID = "foreign"
			case "wrong-object":
				page.Signals[0].Source.MaterialID = "other"
			case "regressed":
				page.Cursor.Seq = 0
			case "duplicate":
				page.Signals = append(page.Signals, page.Signals[0])
			case "oversize":
				page.Signals[0].Preview = strings.Repeat("x", 33<<10)
			case "paused", "completed":
				j.State = scenario
			case "old-version":
				version = 2
			case "snapshot":
				j.Contexts[0].Mode = "snapshot"
			case "read-error":
				readErr = fmt.Errorf("source temporarily offline")
			}
			e.record.Jobs["task"] = j
			err := e.applyTaskSourceLocked("task", version, c, page, readErr)
			e.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			after := currentJob(t, e, "task")
			if after.Contexts[0].Source.Cursor.Seq != 1 || len(after.Contexts[0].Source.Signals) != 0 {
				t.Fatal("invalid result changed durable source position", after)
			}
		})
	}
}

func TestTaskSourceSnapshotAndSharedReceipts(t *testing.T) {
	e, _, store, c := sourceApplyFixture(t)
	e.mu.Lock()
	snapshot, err := e.taskContextsLocked(nil, []TaskContextInput{{ID: c.ID, Reason: "Use this fixed evidence", Mode: "snapshot"}})
	if err != nil {
		t.Fatal(err)
	}
	j := e.record.Jobs["task"]
	j.Contexts = snapshot
	e.record.Jobs[j.ID] = j
	e.mu.Unlock()
	saveTaskContextFacts(t, store, "material", "CHANGED_V2", 2)
	if err := e.syncTaskFollows(); err != nil {
		t.Fatal(err)
	}
	if currentJob(t, e, "task").Contexts[0].Current.Summary != "KNOWN_V1" {
		t.Fatal("fixed evidence followed current facts")
	}
	store.mu.Lock()
	next := clone(store.record)
	page := sourceApplyPage()
	next.UnderstoodAnswers = map[string]bool{understandingAnswerKey(c.ID, page.Signals[0]): true}
	if err := store.saveLocked(next); err != nil {
		t.Fatal(err)
	}
	store.mu.Unlock()
	e.mu.Lock()
	j = e.record.Jobs["task"]
	j.Contexts = []TaskContext{c}
	e.record.Jobs[j.ID] = j
	err = e.applyTaskSourceLocked(j.ID, 1, c, page, nil)
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	got := currentJob(t, e, j.ID)
	if len(got.Contexts[0].Source.Signals) != 0 || got.State != "waiting" {
		t.Fatal("already understood ordinary event woke duplicate work", got)
	}
	ref := spacecontext.SourceRef{SpaceID: "space-test", AnnotationID: "discussion", ReplyID: "answer"}
	a := DependencySignal{ID: "reply:answer", Source: ref}
	ref.MaterialID, ref.Version = "material", 2
	b := DependencySignal{ID: "activity-hash", Source: ref}
	if understandingAnswerKey("focus", a) != understandingAnswerKey("focus", b) {
		t.Fatal("same immutable answer diverged across discovery routes")
	}
}

func TestTaskSourcePollingIsBoundedAndDoesNotStarveSources(t *testing.T) {
	e, _, _, c := sourceApplyFixture(t)
	var mu sync.Mutex
	seen := map[string]int{}
	var live, maximum atomic.Int32
	e.cfg.DependencyPollInterval = time.Millisecond
	e.cfg.ReadTaskSource = func(ctx context.Context, q TaskSourceQuery) (TaskSourceUpdate, error) {
		n := live.Add(1)
		defer live.Add(-1)
		for {
			old := maximum.Load()
			if old >= n || maximum.CompareAndSwap(old, n) {
				break
			}
		}
		select {
		case <-ctx.Done():
			return TaskSourceUpdate{}, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
		mu.Lock()
		seen[q.Subject.ID]++
		mu.Unlock()
		return TaskSourceUpdate{Cursor: q.Cursor}, nil
	}
	e.mu.Lock()
	for i := range 9 {
		j := e.record.Jobs["task"]
		j.ID = fmt.Sprintf("job-%d", i)
		j.Contexts = []TaskContext{clone(c)}
		j.Contexts[0].ID = j.ID
		j.Contexts[0].Subject.ID = j.ID
		e.record.Jobs[j.ID] = j
	}
	delete(e.record.Jobs, "task")
	e.mu.Unlock()
	for range 3 {
		e.refreshTaskSources(context.Background())
		e.dependencyWait.Wait()
		time.Sleep(2 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 9 || maximum.Load() > 4 {
		t.Fatal("source polling starved or exceeded concurrency", seen, maximum.Load())
	}
}

func TestTaskSourceLateInputResumesButIgnoredPacketDoesNotLoop(t *testing.T) {
	e, _, _, c := sourceApplyFixture(t)
	run := Run{ID: "late-input", Kind: "job", JobID: "task", JobVersion: 1, ThreadID: "worker", State: "submitted", ContextInputs: map[string]string{c.ID: c.Current.Fingerprint, "source:" + c.ID: ""}}
	e.mu.Lock()
	j := e.record.Jobs["task"]
	j.ThreadID, j.DecisionRunID = run.ThreadID, run.ID
	e.record.Jobs[j.ID] = j
	e.record.Runs[run.ID] = run
	if err := e.applyTaskSourceLocked(j.ID, 1, c, sourceApplyPage(), nil); err != nil {
		t.Fatal(err)
	}
	if e.record.Jobs[j.ID].State != "waiting" {
		t.Fatal("in-flight wait was replaced prematurely")
	}
	run.State = "completed"
	e.record.Runs[run.ID] = run
	e.finishTaskRunLocked(run, "completed")
	j = e.record.Jobs[j.ID]
	if j.State != "queued" {
		t.Fatal("source arriving after wait was lost", j)
	}
	run.ID = "ignored-input"
	run.ContextInputs["source:"+c.ID] = contextSourceInput(j.Contexts[0])
	j.State, j.DecisionRunID = "running", ""
	e.record.Jobs[j.ID], e.record.Runs[run.ID] = j, run
	e.finishTaskRunLocked(run, "completed")
	after := e.record.Jobs[j.ID]
	e.mu.Unlock()
	if after.State != "needs_followup" || len(after.Contexts[0].Source.Signals) != 1 {
		t.Fatal("ignored source packet retried endlessly or was discarded", after)
	}
}

func TestTaskSourceKnownFactsSkipOrdinaryObservationButKeepDirectQuestion(t *testing.T) {
	for _, direct := range []bool{false, true} {
		t.Run(fmt.Sprint(direct), func(t *testing.T) {
			e, _, _ := setup(t)
			store, signal := attachEngineAttention(t, e)
			signal.Reasons = []string{"follow_discussion"}
			if direct {
				signal.Reasons = append(signal.Reasons, "mention")
			}
			focus := seedDiscussionUnderstanding(t, store, signal)
			store.mu.Lock()
			next := clone(store.record)
			next.UnderstoodAnswers = map[string]bool{understandingAnswerKey(focus.ID, DependencySignal{ID: signal.ID, Source: signal.Source}): true}
			if err := store.saveLocked(next); err != nil {
				t.Fatal(err)
			}
			store.mu.Unlock()
			acceptAttentionSignals(t, store, signal)
			state := store.Snapshot().Events[signal.ID].State
			if direct && state != "queued" || !direct && state != "processed" {
				t.Fatal("fact receipt confused knowledge with answering a direct question", state)
			}
		})
	}
}

func TestTaskSourceHistoryGapSurvivesReconciliationAndRestart(t *testing.T) {
	e, cfg, store, c := sourceApplyFixture(t)
	page := sourceApplyPage()
	page.Resync, page.Cursor.Retention, page.Signals[0].State = true, 1, "source_resync_partial"
	e.mu.Lock()
	err := e.applyTaskSourceLocked("task", 1, c, page, nil)
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	// Another task has reconciled the same current-source evidence. Removing
	// the consumed page must retain the qualification on its missing history.
	store.mu.Lock()
	next := clone(store.record)
	next.UnderstoodAnswers = map[string]bool{understandingAnswerKey(c.ID, page.Signals[0]): true}
	if err = store.saveLocked(next); err != nil {
		t.Fatal(err)
	}
	store.mu.Unlock()
	if err = e.syncTaskFollows(); err != nil {
		t.Fatal(err)
	}
	e.Close()
	restored, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	source := currentJob(t, restored, "task").Contexts[0].Source
	if source == nil || len(source.Signals) != 0 || source.HistoryGap == nil || source.HistoryGap.AfterSeq != 1 || source.HistoryGap.ThroughSeq != 2 || source.HistoryGap.Retention != 1 {
		t.Fatal("reconciled snapshot silently erased the missing interval", source)
	}
}
