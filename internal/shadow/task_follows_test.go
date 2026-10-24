package shadow

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/spacecontext"
)

func changeTaskFocus(t *testing.T, store *AttentionStore, id, operation, goal string) Focus {
	t.Helper()
	f, _ := store.taskFocus(id)
	in := WatchInput{Operation: operation, ID: id, Goal: goal, BaseGoalVersion: f.GoalVersion, RequestID: uuid.NewString()}
	if operation == "start" {
		in.Subject = spacecontext.Subject{Kind: "discussion", ID: "follow-task-discussion"}
	}
	if _, err := store.BeginWatch(in, "actual-owner-request"); err != nil {
		t.Fatal(err)
	}
	f, err := store.FinishWatch(in.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func jobRun(t *testing.T, e *Engine, id string) Run {
	t.Helper()
	var out Run
	until(t, func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		for _, r := range e.record.Runs {
			if r.JobID == id && r.Kind == "job" && r.State == "submitted" && r.TurnID != "" {
				out = r
				return true
			}
		}
		return false
	})
	return out
}

func taskToolError(e *Engine, run Run, args any) error {
	raw, _ := json.Marshal(args)
	e.mu.Lock()
	defer e.mu.Unlock()
	_, err := e.toolLocked(toolCall{ThreadID: run.ThreadID, TurnID: run.TurnID, CallID: uuid.NewString(), Tool: "shadow_task", Arguments: raw})
	return err
}

func TestTaskFollowPauseReplanRestartKeepsWorkerAndAnswers(t *testing.T) {
	e, f, cfg := setup(t)
	store, _ := attachEngineAttention(t, e)
	focus := changeTaskFocus(t, store, "", "start", "PRIVATE_GOAL_OLD")
	e.cfg.ReadDependency = func(context.Context, TaskDependency) (DependencyUpdate, error) { return DependencyUpdate{}, nil }
	e.cfg.ResolveDependency = func(_ context.Context, d TaskDependency, signal spacecontext.Signal) (*DependencySignal, error) {
		return &DependencySignal{ID: "reply:" + signal.Source.ReplyID, Source: signal.Source, State: "answered", Preview: "v3 remains supported"}, nil
	}
	cfg.ReadDependency, cfg.ResolveDependency = e.cfg.ReadDependency, e.cfg.ResolveDependency
	e.Start()
	send(t, e, "owner", "Work on the existing followed discussion")
	main := active(t, e, "main")
	created := cloneMap(tool(t, e, f, main, "shadow_task", map[string]any{"operation": "start", "title": "Bound task", "prompt": "Check compatibility", "followId": focus.ID}))
	id := created["id"].(string)
	worker := jobRun(t, e, id)
	independent := cloneMap(tool(t, e, f, main, "shadow_task", map[string]any{"operation": "start", "title": "Independent task", "prompt": "Check a different deliverable"}))
	other := jobRun(t, e, independent["id"].(string))
	j := currentJob(t, e, id)
	input := taskCommandFor(j, "wait")
	input["dependencies"] = []TaskDependency{{ID: "compatibility", Kind: "discussion", Condition: "Confirm supported version", Source: spacecontext.SourceRef{SpaceID: "space-test", AnnotationID: "follow-task-discussion", ReplyID: "question"}}}
	tool(t, e, f, worker, "shadow_task", input)
	f.mu.Lock()
	f.holdInterrupt = true
	f.mu.Unlock()
	changeTaskFocus(t, store, focus.ID, "pause", "")
	e.notify()
	until(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, call := range f.calls {
			if call.Method == "turn/interrupt" && call.Params["threadId"] == worker.ThreadID && call.Params["turnId"] == worker.TurnID {
				return true
			}
		}
		return false
	})
	if j = currentJob(t, e, id); j.State != "stopping" || j.Version != 2 || j.Checkpoint == nil || len(j.Dependencies) != 1 {
		t.Fatal("interrupt receipt was treated as stopped or progress lost", j)
	}
	changeTaskFocus(t, store, focus.ID, "update", "PRIVATE_GOAL_REVISED")
	e.notify()
	until(t, func() bool { return currentJob(t, e, id).Version == 3 })
	f.complete(worker.ThreadID, "OLD_GOAL_RESULT_MUST_NOT_COMPLETE", "completed", true)
	until(t, func() bool { return currentJob(t, e, id).State == "paused" })
	if j = currentJob(t, e, id); j.ResultEventID != "" || j.Result != "" || j.Outcome == "completed" {
		t.Fatal("late old completion became current business result", j)
	}
	signal := spacecontext.Signal{ID: "paused-answer", Kind: "reply", Source: spacecontext.SourceRef{SpaceID: "space-test", AnnotationID: "follow-task-discussion", ReplyID: "answer"}, CreatedAt: time.Now()}
	delivered, err := e.RouteDependencyActivity(context.Background(), signal)
	if err != nil || len(delivered) != 1 || currentJob(t, e, id).State != "paused" || len(currentJob(t, e, id).Dependencies[0].Signals) != 1 {
		t.Fatal("paused answer was dropped or started a model", delivered, err)
	}
	if j := currentJob(t, e, other.JobID); j.Version != 1 || j.State != "running" || j.Follow != nil {
		t.Fatal("independent delegation changed with follow", j)
	}
	encoded, _ := json.Marshal(e.View())
	if strings.Contains(string(encoded), "PRIVATE_GOAL") || strings.Contains(string(encoded), `"follow":`) {
		t.Fatal("private follow state entered shared view")
	}
	e.Close()
	f.complete(main.ThreadID, "Dispatched the work", "completed", false)
	e2, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e2.Close)
	reopened, err := OpenAttention(store.directory, "space-test", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err = e2.AttachAttention(reopened); err != nil {
		t.Fatal(err)
	}
	e2.Start()
	until(t, func() bool { return e2.View().Online })
	changeTaskFocus(t, reopened, focus.ID, "resume", "")
	e2.notify()
	next := jobRun(t, e2, id)
	if next.ThreadID != worker.ThreadID || next.JobVersion != 4 {
		t.Fatal("follow resume replaced worker or lost latest goal", next, worker)
	}
	j = currentJob(t, e2, id)
	if !j.Follow.NeedsReplan || len(j.Dependencies[0].Signals) != 1 || j.Checkpoint == nil {
		t.Fatal("restart lost goal review or paused answer", j)
	}
	e2.mu.Lock()
	prompt := e2.workerPromptLocked(j)
	e2.mu.Unlock()
	if !strings.Contains(prompt, "PRIVATE_GOAL_REVISED") || !strings.Contains(prompt, "retainDependencyIds") {
		t.Fatal("worker did not receive bounded current goal review")
	}
	if err := taskToolError(e2, next, taskCommandFor(j, "finish")); err == nil {
		t.Fatal("new goal completed against stale criteria")
	}
	replan := taskCommandFor(j, "replan")
	replan["prompt"], replan["completionCriteria"], replan["retainDependencyIds"] = "Verify revised compatibility requirement", []string{"Revised criterion checked"}, []string{"compatibility"}
	tool(t, e2, f, next, "shadow_task", replan)
	j = currentJob(t, e2, id)
	if j.Follow.NeedsReplan || j.Dependencies[0].Signals[0].ID != "reply:answer" || j.CompletionCriteria[0] != "Revised criterion checked" {
		t.Fatal("replan dropped retained evidence", j)
	}
	tool(t, e2, f, next, "shadow_task", answerUnderstanding(t, e2, next, "The retained answer confirms v3 under the revised compatibility requirement."))
	input = taskCommandFor(j, "finish")
	input["resolutions"] = []DependencyResolution{{ID: "compatibility", Explanation: "The supported version satisfies the revised requirement", SignalIDs: []string{"reply:answer"}}}
	input["outcome"], input["result"], input["criteriaChecks"] = "completed", "Revised goal checked", []string{"v3 checked against the answer"}
	tool(t, e2, f, next, "shadow_task", input)
	f.complete(next.ThreadID, "REVISED_GOAL_DONE", "completed", true)
	until(t, func() bool { return currentJob(t, e2, id).ResultEventID != "" })
	if j := currentJob(t, e2, id); j.State != "completed" || j.ResultVersion != 4 {
		t.Fatal(j)
	}
}

func TestTaskFollowDirectMentionRemainsIndependent(t *testing.T) {
	e, f, _, store, signal := directAttention(t)
	acceptAttentionSignals(t, store, signal)
	e.Start()
	run := active(t, e, "continuation")
	created := cloneMap(tool(t, e, f, run, "shadow_task", map[string]any{"operation": "start", "title": "Direct question", "prompt": "Answer this explicit question"}))
	worker := jobRun(t, e, created["id"].(string))
	j := currentJob(t, e, worker.JobID)
	if j.Follow != nil || j.Trigger.FocusID == "" {
		t.Fatal(j)
	}
	changeTaskFocus(t, store, j.Trigger.FocusID, "complete", "")
	if err := e.syncTaskFollows(); err != nil {
		t.Fatal(err)
	}
	j = currentJob(t, e, j.ID)
	tool(t, e, f, worker, "shadow_task", taskCommandFor(j, "checkpoint"))
	if j = currentJob(t, e, j.ID); j.Version != 1 || j.State != "running" {
		t.Fatal(j)
	}
}

func TestTaskFollowCompletionDoesNotFabricateTaskSuccess(t *testing.T) {
	e, f, _, store, run := continuingAttention(t)
	created := cloneMap(tool(t, e, f, run, "shadow_task", map[string]any{"operation": "start", "title": "Follow work", "prompt": "Investigate the actual follow question"}))
	worker := jobRun(t, e, created["id"].(string))
	j := currentJob(t, e, worker.JobID)
	if j.Follow == nil {
		t.Fatal("ordinary follow work lost binding")
	}
	for _, visible := range e.View().Jobs {
		if visible.ID == j.ID && (visible.Follow != nil || visible.Trigger.FocusID != "" || visible.Trigger.GoalVersion != 0 || visible.Trigger.FollowGoal) {
			t.Fatal("private goal binding leaked through public provenance", visible)
		}
	}
	changeTaskFocus(t, store, j.Follow.FocusID, "complete", "")
	e.notify()
	until(t, func() bool { return currentJob(t, e, j.ID).State == "cancelled" })
	j = currentJob(t, e, j.ID)
	if j.Outcome == "completed" || len(j.CompletionChecks) != 0 || j.ResultEventID != "" {
		t.Fatal(j)
	}
}

func TestTaskFollowFailedPersistenceDoesNotStopOrAdvance(t *testing.T) {
	e, _, cfg := setup(t)
	store, _ := attachEngineAttention(t, e)
	focus := changeTaskFocus(t, store, "", "start", "Initial")
	e.mu.Lock()
	err := e.mutateLocked(func() error {
		e.record.Jobs["job"] = Job{ID: "job", Version: 1, Revision: 1, State: "queued", Follow: &TaskFollow{FocusID: focus.ID, GoalVersion: focus.GoalVersion, State: "active"}}
		return nil
	})
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	changeTaskFocus(t, store, focus.ID, "pause", "")
	if err = os.Rename(cfg.Directory, cfg.Directory+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(cfg.Directory, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = e.syncTaskFollows(); err == nil || currentJob(t, e, "job").Version != 1 {
		t.Fatal("failed save advanced task", err)
	}
	if err = os.Remove(cfg.Directory); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(cfg.Directory+"-saved", cfg.Directory); err != nil {
		t.Fatal(err)
	}
	if err = e.syncTaskFollows(); err != nil {
		t.Fatal(err)
	}
	if j := currentJob(t, e, "job"); j.State != "paused" || j.Version != 2 {
		t.Fatal(j)
	}
}

func TestTaskFollowLateNativeCompletionChecksLatestGoalBeforeScheduler(t *testing.T) {
	e, _, _ := setup(t)
	store, _ := attachEngineAttention(t, e)
	focus := changeTaskFocus(t, store, "", "start", "Old goal")
	run := Run{ID: "working-run", Kind: "job", JobID: "job", JobVersion: 1, ThreadID: "worker", TurnID: "turn", State: "submitted", Output: "OBSOLETE_NATIVE_FINAL"}
	e.mu.Lock()
	e.record.Jobs["job"] = Job{ID: "job", Version: 1, Revision: 1, State: "running", ThreadID: "worker", Follow: &TaskFollow{FocusID: focus.ID, GoalVersion: 1, State: "active"}, Checkpoint: &TaskCheckpoint{Summary: "Persisted old progress"}}
	e.record.Runs[run.ID] = run
	e.mu.Unlock()
	changeTaskFocus(t, store, focus.ID, "update", "New goal")
	e.mu.Lock()
	err := e.mutateLocked(func() error { e.finishRunLocked(run.ID, "completed", ""); return nil })
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	j := currentJob(t, e, "job")
	if j.Version != 2 || j.State != "queued" || !j.Follow.NeedsReplan || j.Result != "" || j.ResultEventID != "" || j.Checkpoint == nil {
		t.Fatal("late terminal event outran current goal check", j)
	}
}

func TestTaskFollowUnknownRunIsReconciledBeforeStopping(t *testing.T) {
	e, f, cfg := setup(t)
	store, _ := attachEngineAttention(t, e)
	focus := changeTaskFocus(t, store, "", "start", "Original goal")
	e.Start()
	send(t, e, "owner", "Start work")
	main := active(t, e, "main")
	created := cloneMap(tool(t, e, f, main, "shadow_task", map[string]any{"operation": "start", "title": "Work", "prompt": "Check evidence", "followId": focus.ID}))
	worker := jobRun(t, e, created["id"].(string))
	e.Close()
	f.complete(main.ThreadID, "Started", "completed", false)
	changeTaskFocus(t, store, focus.ID, "pause", "")
	e2, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e2.Close)
	if err = e2.AttachAttention(store); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
	e2.Start()
	until(t, func() bool { return currentJob(t, e2, worker.JobID).State == "paused" })
	f.mu.Lock()
	defer f.mu.Unlock()
	read, interrupted, starts := -1, -1, 0
	for i, call := range f.calls {
		if call.Params["threadId"] != worker.ThreadID {
			continue
		}
		switch call.Method {
		case "thread/turns/list":
			read = i
		case "turn/interrupt":
			interrupted = i
		case "turn/start":
			starts++
		}
	}
	if read < 0 || interrupted <= read || starts != 0 {
		t.Fatal("unknown work was replayed or interrupted without exact reconciliation", f.calls)
	}
}

func TestTaskFollowSharedMemberRequestDoesNotReadPrivateGoal(t *testing.T) {
	e, f, _ := setup(t)
	store, _ := attachEngineAttention(t, e)
	focus := changeTaskFocus(t, store, "", "start", "OWNER_PRIVATE_REASON")
	e.Start()
	send(t, e, "colleague", "Please do this separate task")
	main := active(t, e, "main")
	if err := taskToolError(e, main, map[string]any{"operation": "start", "title": "Other request", "prompt": "Use the owner goal", "followId": focus.ID}); err == nil {
		t.Fatal("shared colleague request bound the owner's private goal")
	}
	created := cloneMap(tool(t, e, f, main, "shadow_task", map[string]any{"operation": "start", "title": "Other request", "prompt": "Do the requested separate work"}))
	j := currentJob(t, e, created["id"].(string))
	if j.Follow != nil {
		t.Fatal(j)
	}
}

func TestTaskFollowControlStillStopsWhenReceivingIsPaused(t *testing.T) {
	e, f, _, store, run := continuingAttention(t)
	created := cloneMap(tool(t, e, f, run, "shadow_task", map[string]any{"operation": "start", "title": "Follow work", "prompt": "Investigate"}))
	worker := jobRun(t, e, created["id"].(string))
	j := currentJob(t, e, worker.JobID)
	e.mu.Lock()
	err := e.mutateLocked(func() error { e.record.Enabled = false; return nil })
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	changeTaskFocus(t, store, j.Follow.FocusID, "pause", "")
	e.notify()
	until(t, func() bool { return currentJob(t, e, j.ID).State == "paused" })
	if e.View().Enabled {
		t.Fatal("goal control resumed new input reception")
	}
}
