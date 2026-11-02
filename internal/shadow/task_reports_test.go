package shadow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestWorkReportProgressAndCompletionBypassMain(t *testing.T) {
	e, f, _ := setup(t)
	e.Start()
	original := send(t, e, "A", "Investigate and report")
	main := active(t, e, "main")
	w := startWorkingTask(t, e, f, main)
	tool(t, e, f, w, "shadow_task", map[string]any{"operation": "report", "state": "continue", "result": "Verified the first fact"})
	if currentJob(t, e, w.JobID).State != "running" || currentRun(e, main.ID).State != "submitted" {
		t.Fatal("progress ended W or waited for S")
	}
	messages := e.ReplyExport()
	if len(messages) != 1 || messages[0].ReplyTo != original.ID || messages[0].SegmentID != w.ThreadID {
		t.Fatal("progress lost original destination", messages)
	}
	invalid := map[string]any{"operation": "report", "state": "complete", "result": "Unverified claim"}
	if taskToolError(e, w, invalid) == nil {
		t.Fatal("completed without criteria")
	}
	tool(t, e, f, w, "shadow_task", map[string]any{"operation": "report", "state": "complete", "result": "All facts verified", "criteriaChecks": []string{"Verified original evidence"}})
	j := currentJob(t, e, w.JobID)
	if j.State != "completed" || len(e.ResultExport()) != 1 || len(e.ReplyExport()) != 2 || e.ReplyExport()[1].TaskResultID != j.ResultEventID {
		t.Fatal("fixed result and outbox were not saved together", j, e.ResultExport(), e.ReplyExport())
	}
	f.complete(w.ThreadID, "Private final must not be broadcast", "completed", true)
	until(t, func() bool { return currentRun(e, w.ID).State == "completed" })
	if j.ResultEventID != currentJob(t, e, j.ID).ResultEventID || len(e.ResultExport()) != 1 || len(e.ReplyExport()) != 2 {
		t.Fatal("native terminal repeated report or replaced fixed result")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, r := range e.record.Runs {
		if r.Kind == "result" {
			t.Fatal("report created a foreground rewrite turn")
		}
	}
}

func TestWorkReportCompletionDoesNotRequireUnderstanding(t *testing.T) {
	e, _, store, run, signal := understandingFixture(t)
	focus := seedDiscussionUnderstanding(t, store, signal)
	if _, err := e.RouteDependencyActivity(context.Background(), signal); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	j := e.record.Jobs[run.JobID]
	j.Contexts = []TaskContext{{ID: focus.ID, Current: TaskContextSnapshot{Revision: 3, Summary: "Unreviewed facts"}}}
	e.record.Jobs[j.ID] = j
	e.mu.Unlock()
	args := map[string]any{"operation": "report", "state": "complete", "result": "Version v3 confirmed", "criteriaChecks": []string{"Read the exact answer"}, "resolutions": []DependencyResolution{{ID: "version", Explanation: "Actual answer names v3", SignalIDs: []string{"reply:answer"}}}}
	if err := taskToolError(e, run, args); err != nil {
		t.Fatal(err)
	}
	if currentJob(t, e, j.ID).State != "completed" || len(e.ResultExport()) != 1 || store.Snapshot().Focuses[focus.ID].Summary != focus.Summary {
		t.Fatal("completion depended on summary or silently rewrote it")
	}
}

func TestWorkReportResumedAttemptKeepsItsOwnResult(t *testing.T) {
	e, f, _ := setup(t)
	e.Start()
	original := send(t, e, "A", "Investigate and report")
	main := active(t, e, "main")
	w := startWorkingTask(t, e, f, main)
	report := func(run Run, summary string) {
		tool(t, e, f, run, "shadow_task", map[string]any{"operation": "report", "state": "fail", "result": "Service unavailable", "checkpoint": TaskCheckpoint{Summary: summary}})
	}
	report(w, "First attempt reached the service")
	first := currentJob(t, e, w.JobID).ResultEventID
	f.complete(w.ThreadID, "Private final", "completed", true)
	until(t, func() bool { return currentRun(e, w.ID).State == "completed" })
	tool(t, e, f, main, "shadow_task", map[string]any{"operation": "resume", "id": w.JobID, "baseVersion": w.JobVersion})
	next := nextJobRun(t, e, w)
	report(next, "Second attempt reached the service again")
	second := currentJob(t, e, w.JobID).ResultEventID
	results, replies := e.ResultExport(), e.ReplyExport()
	if first == second || len(results) != 2 || len(replies) != 2 {
		t.Fatal("resumed result was swallowed by the previous report", first, second, results, replies)
	}
	for _, result := range results {
		want := "First attempt reached the service"
		if result.ID == second {
			want = "Second attempt reached the service again"
		}
		if result.Checkpoint == nil || result.Checkpoint.Summary != want {
			t.Fatal("attempt evidence was rewritten", result)
		}
	}
	for _, reply := range replies {
		if reply.ReplyTo != original.ID {
			t.Fatal("resumed report lost its original destination", reply)
		}
	}
}

func TestWorkReportFailedSaveSendsNothingAndOldVersionCannotReport(t *testing.T) {
	e, f, cfg := setup(t)
	e.Start()
	send(t, e, "owner", "Investigate")
	main := active(t, e, "main")
	w := startWorkingTask(t, e, f, main)
	file := filepath.Join(cfg.Directory, "shadow.json")
	if err := os.Rename(file, file+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0700); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"operation": "report", "state": "complete", "result": "Checked", "criteriaChecks": []string{"Checked evidence"}}
	err := taskToolError(e, w, args)
	if err == nil || currentJob(t, e, w.JobID).State != "running" || len(e.ReplyExport()) != 0 || len(e.ResultExport()) != 0 {
		t.Fatal("failed persistence partially completed work", err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(file+".saved", file); err != nil {
		t.Fatal(err)
	}
	tool(t, e, f, main, "shadow_task", map[string]any{"operation": "cancel", "id": w.JobID, "baseVersion": w.JobVersion})
	if taskToolError(e, w, args) == nil || len(e.ReplyExport()) != 0 {
		t.Fatal("cancelled work submitted a current report")
	}
}

func TestWorkReportDiscussionOutboxSurvivesRestart(t *testing.T) {
	e, f, cfg, store, signal := directAttention(t)
	var mu sync.Mutex
	var calls []string
	available := false
	transport := func(_ context.Context, name string, args map[string]any) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		if name != "reply_to_annotation" {
			return nil, fmt.Errorf("unexpected tool")
		}
		calls = append(calls, args["requestId"].(string))
		if args["annotationId"] != signal.Source.AnnotationID || args["replyTo"].(map[string]any)["id"] != signal.Source.ReplyID {
			return nil, fmt.Errorf("wrong original discussion target")
		}
		if !available {
			return nil, fmt.Errorf("lost host receipt")
		}
		return map[string]any{"status": "saved", "id": "actual-reply"}, nil
	}
	e.cfg.CallTool, cfg.CallTool = transport, transport
	acceptAttentionSignals(t, store, signal)
	e.Start()
	s := active(t, e, "continuation")
	w := startWorkingTask(t, e, f, s)
	tool(t, e, f, w, "shadow_task", map[string]any{"operation": "report", "state": "complete", "result": "Original discussion answer", "criteriaChecks": []string{"Exact evidence checked"}})
	until(t, func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		for _, a := range e.record.Actions {
			if a.AutomaticReport && a.State == "unknown" {
				return true
			}
		}
		return false
	})
	if currentJob(t, e, w.JobID).State != "completed" || len(e.View().Messages) != 0 {
		t.Fatal("delivery failure rolled back business result or leaked discussion to shared chat")
	}
	f.complete(w.ThreadID, "internal", "completed", true)
	f.complete(s.ThreadID, "internal", "completed", true)
	until(t, func() bool {
		return currentRun(e, w.ID).State == "completed" && currentRun(e, s.ID).State == "completed"
	})
	e.Close()
	mu.Lock()
	available = true
	mu.Unlock()
	e2, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer e2.Close()
	if err := e2.AttachAttention(store); err != nil {
		t.Fatal(err)
	}
	e2.Start()
	until(t, func() bool {
		j := currentJob(t, e2, w.JobID)
		return j.ResultEventID != "" && j.DeliveredEventID == j.ResultEventID
	})
	mu.Lock()
	defer mu.Unlock()
	if len(calls) < 2 {
		t.Fatal("outbox did not resume", calls)
	}
	for _, id := range calls {
		if id != calls[0] {
			t.Fatal("retry changed durable business identity", calls)
		}
	}
}

func TestWorkReportReusesAlreadyPublishedDiscussionReply(t *testing.T) {
	e, f, _, store, signal := directAttention(t)
	acceptAttentionSignals(t, store, signal)
	e.Start()
	s := active(t, e, "continuation")
	w := startWorkingTask(t, e, f, s)
	actionID := uuid.NewString()
	args := map[string]any{"annotationId": signal.Source.AnnotationID, "requestId": actionID, "text": "Published answer", "replyTo": map[string]any{"kind": "reply", "id": signal.Source.ReplyID}}
	tool(t, e, f, w, "reply_to_annotation", args)
	tool(t, e, f, w, "shadow_task", map[string]any{"operation": "report", "state": "complete", "result": "Published answer", "criteriaChecks": []string{"Evidence checked"}, "publishedActionId": actionID})
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.record.Actions) != 1 || len(e.record.Messages) != 0 || e.record.Jobs[w.JobID].State != "completed" {
		t.Fatal("report duplicated a saved public answer", e.record.Actions)
	}
}

func TestOrdinaryAskCannotCreateImplicitCoordination(t *testing.T) {
	e, f, _ := setup(t)
	connectQuestionTools(e)
	e.Start()
	send(t, e, "owner", "Ask which deadline")
	s := active(t, e, "main")
	args := questionCommand()
	delete(args, "kind")
	_, err := e.callTaskAsk(context.Background(), askCall(s, args))
	if err == nil || !strings.Contains(err.Error(), "普通补问") || len(e.View().Jobs) != 0 {
		t.Fatal("ordinary clarification implicitly became a job", err)
	}
	tool(t, e, f, s, "shadow_reply", map[string]any{"text": "Which deadline?"})
	if len(e.View().Jobs) != 0 {
		t.Fatal("ordinary reply created work")
	}
}

func TestWorkReportWaitRetainsRealDependencies(t *testing.T) {
	e, f, _ := setup(t)
	e.Start()
	send(t, e, "owner", "Check at the requested time")
	s := active(t, e, "main")
	w := startWorkingTask(t, e, f, s)
	due := time.Now().Add(time.Hour)
	tool(t, e, f, w, "shadow_task", map[string]any{"operation": "report", "state": "wait", "result": "Waiting for the requested time", "checkpoint": TaskCheckpoint{Summary: "Initial check complete", NextStep: "Check again at the requested time"}, "dependencies": []TaskDependency{{ID: "time", Kind: "time", Condition: "Requested check time", DueAt: &due}}})
	if j := currentJob(t, e, w.JobID); j.State != "waiting" || len(j.Dependencies) != 1 || len(e.ReplyExport()) != 1 {
		t.Fatal(j)
	}
}
