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
	"teamcross/internal/spacecontext"
)

func currentJob(t *testing.T, e *Engine, id string) Job {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	j, ok := e.record.Jobs[id]
	if !ok {
		t.Fatal("missing task", id)
	}
	return clone(j)
}

func taskCommandFor(j Job, operation string) map[string]any {
	return map[string]any{"operation": operation, "id": j.ID, "baseVersion": j.Version, "baseRevision": j.Revision, "checkpoint": TaskCheckpoint{Summary: "Verified current progress.", NextStep: "Read the answer and continue the original goal."}}
}

func startWorkingTask(t *testing.T, e *Engine, f *fakeNative, main Run) Run {
	t.Helper()
	tool(t, e, f, main, "shadow_task", map[string]any{"operation": "start", "title": "Compatibility", "prompt": "Resolve the compatibility question and report evidence.", "completionCriteria": []string{"Compatibility question answered using the actual evidence."}})
	return active(t, e, "job")
}

func nextJobRun(t *testing.T, e *Engine, previous Run) Run {
	t.Helper()
	var next Run
	until(t, func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		for _, r := range e.record.Runs {
			if r.Kind == "job" && r.JobID == previous.JobID && r.ID != previous.ID && r.State == "submitted" && r.TurnID != "" {
				next = r
				return true
			}
		}
		return false
	})
	return next
}

func TestTaskNativeFinalDoesNotCompleteBusinessGoal(t *testing.T) {
	e, f, _ := setup(t)
	e.Start()
	send(t, e, "owner", "Investigate and finish the task")
	main := active(t, e, "main")
	worker := startWorkingTask(t, e, f, main)
	f.complete(worker.ThreadID, "I asked a question; waiting for the answer.", "completed", true)
	until(t, func() bool { return currentJob(t, e, worker.JobID).State == "needs_followup" })
	j := currentJob(t, e, worker.JobID)
	if j.Outcome != "needs_followup" || j.ResultEventID == "" || j.LastRunState != "completed" {
		t.Fatal(j)
	}
	args := taskCommandFor(j, "finish")
	args["outcome"] = "completed"
	args["result"] = "A claim without checking completion."
	raw, _ := json.Marshal(args)
	e.mu.Lock()
	_, err := e.toolLocked(toolCall{ThreadID: main.ThreadID, TurnID: main.TurnID, CallID: uuid.NewString(), Tool: "shadow_task", Arguments: raw})
	e.mu.Unlock()
	if err == nil || currentJob(t, e, j.ID).State == "completed" {
		t.Fatal("unchecked final completed business task")
	}
}

func TestTaskEarlyAnswerResumesSameWorkerAndRequiresResolution(t *testing.T) {
	e, f, cfg := setup(t)
	var calls atomic.Int32
	reader := func(_ context.Context, d TaskDependency) (DependencyUpdate, error) {
		calls.Add(1)
		ref := d.Source
		ref.ReplyID = "answer-already-present"
		return DependencyUpdate{Cursor: "reply:2", Signals: []DependencySignal{{ID: "answer", Source: ref, State: "answered", Preview: "Support v3 clients."}}}, nil
	}
	e.cfg.ReadDependency, e.cfg.DependencyPollInterval = reader, 5*time.Millisecond
	cfg.ReadDependency, cfg.DependencyPollInterval = reader, 5*time.Millisecond
	e.Start()
	send(t, e, "owner", "Ask, wait and continue")
	main := active(t, e, "main")
	worker := startWorkingTask(t, e, f, main)
	j := currentJob(t, e, worker.JobID)
	args := taskCommandFor(j, "wait")
	args["dependencies"] = []TaskDependency{{ID: "client-version", Kind: "discussion", Condition: "The supported client version is stated", Source: spacecontext.SourceRef{SpaceID: "space-test", AnnotationID: "discussion", ReplyID: "question"}}}
	tool(t, e, f, worker, "shadow_task", args)
	if calls.Load() != 0 {
		t.Fatal("polled dependency before working turn ended")
	}
	f.complete(worker.ThreadID, "Waiting for client compatibility facts.", "completed", true)
	next := nextJobRun(t, e, worker)
	if next.ThreadID != worker.ThreadID {
		t.Fatal("answer started a different native worker")
	}
	j = currentJob(t, e, worker.JobID)
	if len(j.Dependencies) != 1 || j.Dependencies[0].State != "waiting" || len(j.Dependencies[0].Signals) != 1 {
		t.Fatal("arrival was incorrectly treated as sufficient", j)
	}
	args = taskCommandFor(j, "finish")
	args["outcome"] = "completed"
	args["result"] = "Client v3 is supported."
	args["criteriaChecks"] = []string{"Checked exact reply and compatibility evidence."}
	raw, _ := json.Marshal(args)
	e.mu.Lock()
	_, err := e.toolLocked(toolCall{ThreadID: next.ThreadID, TurnID: next.TurnID, CallID: uuid.NewString(), Tool: "shadow_task", Arguments: raw})
	e.mu.Unlock()
	if err == nil {
		t.Fatal("unresolved dependency silently completed")
	}
	args["resolutions"] = []DependencyResolution{{ID: "client-version", Explanation: "The original question was about supported versions; the reply specifies v3.", SignalIDs: []string{"answer"}}}
	tool(t, e, f, next, "shadow_task", args)
	f.complete(next.ThreadID, "DONE", "completed", true)
	until(t, func() bool {
		return currentJob(t, e, j.ID).LastRunState == "completed" && currentJob(t, e, j.ID).ResultEventID != ""
	})
	j = currentJob(t, e, j.ID)
	if j.State != "completed" || j.Dependencies[0].State != "satisfied" || len(j.CompletionChecks) != 1 || j.Result != "Client v3 is supported." {
		t.Fatal(j)
	}
}

func TestTaskWaitingReleasesCapacityAndSurvivesRestart(t *testing.T) {
	e, f, cfg := setup(t)
	var ready atomic.Bool
	reader := func(_ context.Context, d TaskDependency) (DependencyUpdate, error) {
		if !ready.Load() {
			return DependencyUpdate{Cursor: "reply:1"}, nil
		}
		ref := d.Source
		ref.ReplyID = "answer"
		return DependencyUpdate{Cursor: "reply:2", Signals: []DependencySignal{{ID: "answer", Source: ref}}}, nil
	}
	e.cfg.MaxBackground, cfg.MaxBackground = 1, 1
	e.cfg.ReadDependency, cfg.ReadDependency = reader, reader
	e.cfg.DependencyPollInterval, cfg.DependencyPollInterval = 5*time.Millisecond, 5*time.Millisecond
	e.Start()
	send(t, e, "owner", "Track the answer")
	main := active(t, e, "main")
	worker := startWorkingTask(t, e, f, main)
	args := taskCommandFor(currentJob(t, e, worker.JobID), "wait")
	args["dependencies"] = []TaskDependency{{ID: "answer", Kind: "discussion", Condition: "Specific version is provided", Source: spacecontext.SourceRef{AnnotationID: "discussion"}}}
	tool(t, e, f, worker, "shadow_task", args)
	f.complete(worker.ThreadID, "Waiting", "completed", true)
	until(t, func() bool { return currentJob(t, e, worker.JobID).Dependencies[0].Cursor == "reply:1" })
	other := cloneMap(tool(t, e, f, main, "shadow_task", map[string]any{"operation": "start", "title": "Independent work", "prompt": "Do independent work."}))
	second := active(t, e, "job")
	if second.JobID != other["id"] || second.JobID == worker.JobID {
		t.Fatal("waiting task occupied the worker slot", second)
	}
	f.complete(second.ThreadID, "Partial progress", "completed", true)
	until(t, func() bool { return currentJob(t, e, second.JobID).State == "needs_followup" })
	e.Close()
	ready.Store(true)
	restored, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Close)
	restored.Start()
	next := nextJobRun(t, restored, worker)
	if next.ThreadID != worker.ThreadID {
		t.Fatal("restart replaced the worker", next)
	}
	if currentJob(t, restored, worker.JobID).Checkpoint == nil {
		t.Fatal("checkpoint lost")
	}
}

func TestTaskPartialAnswersLeaveIndependentDependenciesWaiting(t *testing.T) {
	e, f, _ := setup(t)
	var serial atomic.Int32
	e.cfg.DependencyPollInterval = 5 * time.Millisecond
	e.cfg.ReadDependency = func(_ context.Context, d TaskDependency) (DependencyUpdate, error) {
		if d.ID != "facts" || serial.Load() == 0 {
			return DependencyUpdate{Cursor: d.Cursor}, nil
		}
		cursor := fmt.Sprintf("reply:%d", serial.Load())
		if cursor == d.Cursor {
			return DependencyUpdate{Cursor: cursor}, nil
		}
		ref := d.Source
		ref.ReplyID = cursor
		return DependencyUpdate{Cursor: cursor, Signals: []DependencySignal{{ID: cursor, Source: ref, Preview: "Partial technical facts"}}}, nil
	}
	e.Start()
	send(t, e, "owner", "Need facts and a product decision")
	main := active(t, e, "main")
	worker := startWorkingTask(t, e, f, main)
	args := taskCommandFor(currentJob(t, e, worker.JobID), "wait")
	args["dependencies"] = []TaskDependency{{ID: "facts", Kind: "discussion", Condition: "Technical fact", Source: spacecontext.SourceRef{AnnotationID: "facts"}}, {ID: "decision", Kind: "discussion", Condition: "Product choice", Source: spacecontext.SourceRef{AnnotationID: "decision"}}}
	tool(t, e, f, worker, "shadow_task", args)
	serial.Store(1)
	f.complete(worker.ThreadID, "Waiting", "completed", true)
	next := nextJobRun(t, e, worker)
	j := currentJob(t, e, worker.JobID)
	args = taskCommandFor(j, "wait")
	args["resolutions"] = []DependencyResolution{{ID: "facts", Explanation: "The technical part is now answered; the product choice is still open.", SignalIDs: []string{"reply:1"}}}
	tool(t, e, f, next, "shadow_task", args)
	f.complete(next.ThreadID, "Technical analysis saved; awaiting choice.", "completed", true)
	until(t, func() bool {
		return currentJob(t, e, j.ID).LastRunState == "completed" && !e.UpgradeBusy() || currentJob(t, e, j.ID).State == "waiting"
	})
	j = currentJob(t, e, j.ID)
	if j.Dependencies[0].State != "satisfied" || j.Dependencies[1].State != "waiting" || j.State != "waiting" {
		t.Fatal(j)
	}
}

func TestTaskDependencyFailureDoesNotConsumeCursorOrDisableChat(t *testing.T) {
	e, _, cfg := setup(t)
	job := Job{ID: "job", Version: 1, Revision: 1, State: "waiting", Dependencies: []TaskDependency{{ID: "d", Kind: "request", Condition: "Result", State: "waiting", Source: spacecontext.SourceRef{SpaceID: "space-test", RequestID: "request"}}}}
	e.mu.Lock()
	e.record.Jobs[job.ID] = job
	e.mu.Unlock()
	file := filepath.Join(cfg.Directory, "shadow.json")
	if err := os.Rename(file, file+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0700); err != nil {
		t.Fatal(err)
	}
	update := DependencyUpdate{Cursor: "result", Signals: []DependencySignal{{ID: "result", Source: job.Dependencies[0].Source}}}
	e.mu.Lock()
	err := e.applyDependencyUpdateLocked(job.ID, 1, job.Dependencies[0], update, nil)
	e.mu.Unlock()
	if err == nil || currentJob(t, e, job.ID).Dependencies[0].Cursor != "" || currentJob(t, e, job.ID).State != "waiting" {
		t.Fatal("failed persistence consumed answer", err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(file+".saved", file); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	err = e.applyDependencyUpdateLocked(job.ID, 1, job.Dependencies[0], update, nil)
	e.mu.Unlock()
	if err != nil || currentJob(t, e, job.ID).State != "queued" {
		t.Fatal(err, currentJob(t, e, job.ID))
	}
}

func TestTaskDependenciesDoNotFinishAnotherIndependentWork(t *testing.T) {
	e, f, _ := setup(t)
	e.Start()
	send(t, e, "owner", "Coordinate the work")
	main := active(t, e, "main")
	created := cloneMap(tool(t, e, f, main, "shadow_task", map[string]any{"operation": "start", "kind": "work", "title": "Coordinate", "prompt": "Coordinate two independent facts."}))
	f.complete(main.ThreadID, "Coordinating.", "completed", true)
	run := active(t, e, "job")
	if run.ThreadID == main.ThreadID || run.JobID != created["id"] {
		t.Fatal(run)
	}
	child := cloneMap(tool(t, e, f, run, "shadow_task", map[string]any{"operation": "start", "title": "One fact", "prompt": "Verify one fact."}))
	args := taskCommandFor(currentJob(t, e, run.JobID), "wait")
	args["dependencies"] = []TaskDependency{{ID: "fact", Kind: "job", Condition: "The child verifies the fact", JobID: child["id"].(string), JobVersion: 1}}
	tool(t, e, f, run, "shadow_task", args)
	f.complete(run.ThreadID, "Waiting for fact.", "completed", true)
	var childRun Run
	until(t, func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		for _, r := range e.record.Runs {
			if r.JobID == child["id"] && activeRun(r) && r.TurnID != "" {
				childRun = r
				return true
			}
		}
		return false
	})
	cycle := taskCommandFor(currentJob(t, e, childRun.JobID), "wait")
	cycle["dependencies"] = []TaskDependency{{ID: "cycle", Kind: "job", Condition: "Wait for parent", JobID: run.JobID, JobVersion: 1}}
	raw, _ := json.Marshal(cycle)
	e.mu.Lock()
	_, cycleErr := e.toolLocked(toolCall{ThreadID: childRun.ThreadID, TurnID: childRun.TurnID, CallID: uuid.NewString(), Tool: "shadow_task", Arguments: raw})
	e.mu.Unlock()
	if cycleErr == nil {
		t.Fatal("cyclic task dependency accepted")
	}
	args = taskCommandFor(currentJob(t, e, childRun.JobID), "finish")
	args["outcome"] = "completed"
	args["result"] = "Verified fact"
	args["criteriaChecks"] = []string{"The evidence establishes the fact."}
	tool(t, e, f, childRun, "shadow_task", args)
	f.complete(childRun.ThreadID, "Done", "completed", true)
	next := nextJobRun(t, e, run)
	if next.ThreadID != run.ThreadID || currentJob(t, e, run.JobID).State != "running" {
		t.Fatal("child prematurely completed parent", currentJob(t, e, run.JobID))
	}
}

func TestTaskDependencyReadDoesNotBlockChatOrApplyAfterCancellation(t *testing.T) {
	e, f, _ := setup(t)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	e.cfg.ReadDependency = func(ctx context.Context, d TaskDependency) (DependencyUpdate, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
			return DependencyUpdate{}, ctx.Err()
		case <-release:
		}
		return DependencyUpdate{Cursor: "answer", Signals: []DependencySignal{{ID: "answer", Source: d.Source}}}, nil
	}
	e.Start()
	send(t, e, "owner", "Wait for a result")
	main := active(t, e, "main")
	worker := startWorkingTask(t, e, f, main)
	args := taskCommandFor(currentJob(t, e, worker.JobID), "wait")
	args["dependencies"] = []TaskDependency{{ID: "request", Kind: "request", Condition: "Actual result", Source: spacecontext.SourceRef{RequestID: "request"}}}
	tool(t, e, f, worker, "shadow_task", args)
	f.complete(worker.ThreadID, "Waiting", "completed", true)
	f.complete(main.ThreadID, "I will continue when the answer arrives.", "completed", true)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("dependency read did not start")
	}
	message := send(t, e, "owner", "Cancel that waiting task")
	var current Run
	until(t, func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		for _, r := range e.record.Runs {
			if r.Kind == "main" && r.SourceID == message.ID && r.State == "submitted" {
				current = r
				return true
			}
		}
		return false
	})
	tool(t, e, f, current, "shadow_task", map[string]any{"operation": "cancel", "id": worker.JobID, "baseVersion": 1})
	close(release)
	until(t, func() bool { e.mu.Lock(); defer e.mu.Unlock(); return len(e.dependencyReading) == 0 })
	if j := currentJob(t, e, worker.JobID); j.State != "cancelled" || j.Dependencies[0].Cursor != "" {
		t.Fatal("late answer revived cancelled task", j)
	}
}

func TestTaskTimeDependencyWakesOnceAndUnchangedAnswerDoesNotLoop(t *testing.T) {
	e, f, _ := setup(t)
	e.cfg.DependencyPollInterval = 5 * time.Millisecond
	e.Start()
	send(t, e, "owner", "Check after the given time")
	main := active(t, e, "main")
	worker := startWorkingTask(t, e, f, main)
	due := time.Now().Add(-time.Second)
	args := taskCommandFor(currentJob(t, e, worker.JobID), "wait")
	args["dependencies"] = []TaskDependency{{ID: "deadline", Kind: "time", Condition: "Perform the explicit deadline check", DueAt: &due}}
	tool(t, e, f, worker, "shadow_task", args)
	f.complete(worker.ThreadID, "Waiting until deadline", "completed", true)
	next := nextJobRun(t, e, worker)
	j := currentJob(t, e, worker.JobID)
	if len(j.Dependencies[0].Signals) != 1 || j.Dependencies[0].Signals[0].State != "due" {
		t.Fatal(j)
	}
	args = taskCommandFor(j, "wait")
	tool(t, e, f, next, "shadow_task", args)
	f.complete(next.ThreadID, "Still no answer; keep the saved check.", "completed", true)
	until(t, func() bool { e.mu.Lock(); defer e.mu.Unlock(); return !activeRun(e.record.Runs[next.ID]) })
	for i := 0; i < 3; i++ {
		key := worker.JobID + ":deadline"
		e.mu.Lock()
		before := e.dependencyNext[key]
		e.mu.Unlock()
		until(t, func() bool {
			e.mu.Lock()
			defer e.mu.Unlock()
			return e.dependencyNext[key].After(before) && !e.dependencyReading[key]
		})
	}
	if j = currentJob(t, e, worker.JobID); j.State != "waiting" || len(j.Dependencies[0].Signals) != 0 {
		t.Fatal("same deadline caused repeated model wakeup", j)
	}
}

func TestTaskIndexKeepsFullDependencyEvidenceOutOfMainContext(t *testing.T) {
	e, _, _ := setup(t)
	e.mu.Lock()
	e.record.Jobs["job"] = Job{ID: "job", State: "waiting", Version: 1, Revision: 4, CompletionCriteria: []string{"DETAILED_GOAL_ONLY_FOR_THIS_WORKER"}, Dependencies: []TaskDependency{{ID: "d", Condition: "Need facts", Signals: []DependencySignal{{ID: "signal", Preview: "DETAILED_ANSWER_ONLY_FOR_THIS_WORKER"}}}}}
	index := e.contextLocked(false)
	e.mu.Unlock()
	if strings.Contains(index, "DETAILED_") || !strings.Contains(index, "Need facts") {
		t.Fatal("main context included full task bodies or lost index", index)
	}
}
