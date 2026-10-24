package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/mcp"
	"teamcross/internal/spacecontext"
)

func connectQuestionTools(e *Engine) {
	e.cfg.CollaborationTools, e.cfg.ValidateTool = mcp.ShadowTools(), mcp.ValidateShadowCall
	e.cfg.ReadDependency = func(_ context.Context, d TaskDependency) (DependencyUpdate, error) {
		return DependencyUpdate{Cursor: d.Cursor}, nil
	}
	e.cfg.CallTool = func(_ context.Context, name string, args map[string]any) (any, error) {
		id, _, _, _ := e.Identity()
		_, owner := e.MemberIdentity()
		posted := map[string]any{"id": "actual-question", "requestId": args["requestId"], "actor": spacecontext.Actor{Kind: "shadow", ShadowID: id, MemberID: owner}}
		if name == "reply_to_annotation" {
			return map[string]any{"status": "saved", "reply": posted}, nil
		}
		return posted, nil
	}
}

func questionCommand() map[string]any {
	return map[string]any{"operation": "ask", "title": "Resolve compatibility", "prompt": "Confirm the exact supported client version and answer the original question.", "completionCriteria": []string{"Answer the original question with the exact version and source."}, "checkpoint": TaskCheckpoint{Summary: "Missing the supported client version.", NextStep: "Read the exact answer and finish the compatibility recommendation."}, "question": map[string]any{"tool": "reply_to_annotation", "arguments": map[string]any{"annotationId": "discussion", "requestId": uuid.NewString(), "text": "Which client version must remain supported?"}, "condition": "The supported client version is specified."}}
}

func askCall(run Run, args any) toolCall {
	raw, _ := json.Marshal(args)
	return toolCall{ThreadID: run.ThreadID, TurnID: run.TurnID, CallID: uuid.NewString(), Tool: "shadow_task", Arguments: raw}
}

func questionPair(t *testing.T, result any) (TaskQuestion, Job) {
	t.Helper()
	var pair struct {
		Question TaskQuestion `json:"question"`
		Task     Job          `json:"task"`
	}
	b, _ := json.Marshal(result)
	if err := json.Unmarshal(b, &pair); err != nil || pair.Question.ID == "" || pair.Task.ID == "" {
		t.Fatal("missing question/task receipt", string(b), err)
	}
	return pair.Question, pair.Task
}

func TestTaskQuestionPersistsBeforeSendAndReusesBusinessIdentity(t *testing.T) {
	e, f, _ := setup(t)
	connectQuestionTools(e)
	base := e.cfg.CallTool
	var sent atomic.Int32
	e.cfg.CallTool = func(ctx context.Context, name string, args map[string]any) (any, error) {
		b, err := os.ReadFile(filepath.Join(e.cfg.Directory, "shadow.json"))
		if err != nil {
			return nil, err
		}
		var disk Record
		if err := json.Unmarshal(b, &disk); err != nil {
			return nil, err
		}
		id := args["requestId"].(string)
		a := disk.Actions[id]
		q := disk.Questions[a.QuestionID]
		j := disk.Jobs[q.JobID]
		if a.State != "pending" || q.Delivery != "pending" || j.Checkpoint == nil || len(j.Dependencies) != 1 || !j.Dependencies[0].QuestionSourcePending {
			return nil, fmt.Errorf("question was sent before durable registration")
		}
		sent.Add(1)
		return base(ctx, name, args)
	}
	e.Start()
	send(t, e, "owner", "Ask and continue when the answer arrives")
	run := active(t, e, "main")
	args := questionCommand()
	q, job := questionPair(t, tool(t, e, f, run, "shadow_task", args))
	if q.Delivery != "saved" || q.ID == q.ActionID || q.Source.ReplyID != "actual-question" || job.State != "waiting" || job.Dependencies[0].QuestionSourcePending {
		t.Fatal(q, job)
	}
	q2, j2 := questionPair(t, tool(t, e, f, run, "shadow_task", args))
	if q2.ID != q.ID || j2.ID != job.ID || sent.Load() != 1 {
		t.Fatal("stable ask duplicated work or question", q2, j2, sent.Load())
	}
	questionPair(t, tool(t, e, f, run, "shadow_task", map[string]any{"operation": "questions", "id": q.ID}))
	for _, public := range e.View().Jobs {
		for _, d := range public.Dependencies {
			if d.QuestionID != "" || d.QuestionActionID != "" || d.QuestionTool != "" {
				t.Fatal("private question provenance leaked", d)
			}
		}
	}
	args["prompt"] = "A different goal using the same request ID"
	if _, err := e.callTaskAsk(context.Background(), askCall(run, args)); err == nil || sent.Load() != 1 {
		t.Fatal("same question ID silently changed goal")
	}
}

func TestTaskQuestionFailedStorageNeverSendsOrLeavesHalfATask(t *testing.T) {
	e, _, cfg := setup(t)
	connectQuestionTools(e)
	var sent atomic.Int32
	e.cfg.CallTool = func(context.Context, string, map[string]any) (any, error) { sent.Add(1); return nil, nil }
	e.Start()
	send(t, e, "owner", "Ask one question")
	run := active(t, e, "main")
	path := filepath.Join(cfg.Directory, "shadow.json")
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	_, err := e.callTaskAsk(context.Background(), askCall(run, questionCommand()))
	e.mu.Lock()
	clean := len(e.record.Questions) == 0 && len(e.record.Jobs) == 0 && len(e.record.Actions) == 0 && e.record.Questions != nil
	e.mu.Unlock()
	if err == nil || !clean || sent.Load() != 0 {
		t.Fatal("failed storage sent or partially registered question", err, clean, sent.Load())
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
}

func TestTaskQuestionLostReceiptRecoversReadOnlyAndResumesOriginalWorker(t *testing.T) {
	e, f, _ := setup(t)
	connectQuestionTools(e)
	var ready atomic.Bool
	var sends atomic.Int32
	e.cfg.DependencyPollInterval = 5 * time.Millisecond
	e.cfg.CallTool = func(context.Context, string, map[string]any) (any, error) {
		sends.Add(1)
		return nil, fmt.Errorf("saved remotely but receipt lost")
	}
	e.cfg.ReadDependency = func(_ context.Context, d TaskDependency) (DependencyUpdate, error) {
		if !ready.Load() {
			return DependencyUpdate{Cursor: d.Cursor}, nil
		}
		question := spacecontext.SourceRef{SpaceID: "space-test", AnnotationID: "discussion", ReplyID: "actual-question"}
		answer := question
		answer.ReplyID = "owner-answer"
		return DependencyUpdate{QuestionSource: &question, Cursor: "reply:2", Signals: []DependencySignal{{ID: "reply:owner-answer", Source: answer, Actor: spacecontext.Actor{Kind: "human", MemberID: "owner"}, State: "answered", Preview: "Support client v3."}}}, nil
	}
	cfg := e.cfg
	e.Start()
	send(t, e, "owner", "Resolve compatibility")
	main := active(t, e, "main")
	worker := startWorkingTask(t, e, f, main)
	j := currentJob(t, e, worker.JobID)
	args := questionCommand()
	args["id"], args["baseVersion"], args["baseRevision"] = j.ID, j.Version, j.Revision
	q, j := questionPair(t, tool(t, e, f, worker, "shadow_task", args))
	if q.Delivery != "unknown" || !j.Dependencies[0].QuestionSourcePending || j.State != "running" {
		t.Fatal(q, j)
	}
	tool(t, e, f, worker, "shadow_task", taskCommandFor(j, "wait"))
	f.complete(worker.ThreadID, "Waiting for the exact answer.", "completed", true)
	until(t, func() bool { return currentJob(t, e, j.ID).LastRunState == "completed" })
	e.Close()
	ready.Store(true)
	restored, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Close)
	restored.Start()
	next := nextJobRun(t, restored, worker)
	j = currentJob(t, restored, j.ID)
	if next.ThreadID != worker.ThreadID || len(j.Dependencies[0].Signals) != 1 || j.Dependencies[0].State != "waiting" || j.Dependencies[0].QuestionSourcePending || sends.Load() != 1 {
		t.Fatal("recovery resent, lost answer, changed worker or fabricated satisfaction", next, j, sends.Load())
	}
	restored.mu.Lock()
	got := restored.record.Questions[q.ID]
	unknownAction := restored.record.Actions[q.ActionID].State
	restored.mu.Unlock()
	if got.Delivery != "saved" || unknownAction != "unknown" {
		t.Fatal("business source should confirm question without inventing original write receipt", got, unknownAction)
	}
	args = taskCommandFor(j, "finish")
	args["outcome"], args["result"], args["criteriaChecks"] = "completed", "Support client v3.", []string{"Original owner answer explicitly specifies v3."}
	args["resolutions"] = []DependencyResolution{{ID: j.Dependencies[0].ID, Explanation: "Version is now specified.", SignalIDs: []string{"reply:owner-answer"}}}
	tool(t, restored, f, next, "shadow_task", args)
	f.complete(next.ThreadID, "Done", "completed", true)
	until(t, func() bool { return currentJob(t, restored, j.ID).State == "completed" })
}

func TestTaskQuestionEarlyAnswerBindsBeforeLostWriteReceipt(t *testing.T) {
	e, f, _ := setup(t)
	connectQuestionTools(e)
	question := spacecontext.SourceRef{SpaceID: "space-test", AnnotationID: "discussion", ReplyID: "actual-question"}
	answer := question
	answer.ReplyID = "early-answer"
	e.cfg.ResolveDependency = func(_ context.Context, d TaskDependency, signal spacecontext.Signal) (*DependencySignal, error) {
		if !d.QuestionSourcePending || d.QuestionActionID == "" {
			return nil, fmt.Errorf("pending question identity was lost")
		}
		return &DependencySignal{QuestionSource: &question, ID: "reply:early-answer", Source: signal.Source, Preview: "Client v3", State: "answered"}, nil
	}
	e.cfg.CallTool = func(ctx context.Context, _ string, _ map[string]any) (any, error) {
		routed, err := e.RouteDependencyActivity(ctx, spacecontext.Signal{Kind: "reply", Source: answer})
		if err != nil || len(routed) != 1 {
			return nil, fmt.Errorf("early answer not routed: %v, %v", routed, err)
		}
		return nil, fmt.Errorf("write receipt lost after early answer")
	}
	e.Start()
	send(t, e, "owner", "Ask")
	run := active(t, e, "main")
	q, j := questionPair(t, tool(t, e, f, run, "shadow_task", questionCommand()))
	if q.Delivery != "saved" || j.State != "queued" || j.Dependencies[0].Source.ReplyID != question.ReplyID || len(j.Dependencies[0].Signals) != 1 {
		t.Fatal(q, j)
	}
	routed, err := e.RouteDependencyActivity(context.Background(), spacecontext.Signal{Kind: "reply", Source: answer})
	if err != nil || len(routed) != 1 || len(currentJob(t, e, j.ID).Dependencies[0].Signals) != 1 {
		t.Fatal("same answer was lost or duplicated", routed, err)
	}
}

func TestTaskQuestionSourceBindingRollsBackAndCannotBeForged(t *testing.T) {
	e, f, cfg := setup(t)
	connectQuestionTools(e)
	e.cfg.CallTool = func(context.Context, string, map[string]any) (any, error) { return nil, fmt.Errorf("receipt unknown") }
	e.Start()
	send(t, e, "owner", "Ask")
	run := active(t, e, "main")
	q, j := questionPair(t, tool(t, e, f, run, "shadow_task", questionCommand()))
	ref := q.Source
	ref.ReplyID = "actual-question"
	path := filepath.Join(cfg.Directory, "shadow.json")
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	err := e.applyDependencyUpdateLocked(j.ID, j.Version, j.Dependencies[0], DependencyUpdate{QuestionSource: &ref}, nil)
	qAfter := e.record.Questions[q.ID]
	jAfter := clone(e.record.Jobs[j.ID])
	e.mu.Unlock()
	if err == nil || qAfter.Delivery != "unknown" || !jAfter.Dependencies[0].QuestionSourcePending || jAfter.Revision != j.Revision {
		t.Fatal("failed binding escaped rollback", err, qAfter, jAfter)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	err = e.applyDependencyUpdateLocked(j.ID, j.Version, j.Dependencies[0], DependencyUpdate{QuestionSource: &ref}, nil)
	forged := TaskDependency{ID: "forged", Kind: "discussion", Condition: "Pretend a question was sent", QuestionSourcePending: true, QuestionTool: "add_annotation", QuestionActionID: "invented"}
	validation := e.validateDependencyLocked(j, &forged)
	e.mu.Unlock()
	if err != nil || validation == nil {
		t.Fatal("binding could not recover or caller invented pending identity", err, validation)
	}
}

func TestAttentionDirectRequiresRegisteredFutureWork(t *testing.T) {
	for _, outcome := range []string{"continue", "needs_owner"} {
		t.Run(outcome, func(t *testing.T) {
			e, f, _, s, signal := directAttention(t)
			acceptAttentionSignals(t, s, signal)
			e.Start()
			run := active(t, e, "continuation")
			tool(t, e, f, run, "reply_to_annotation", map[string]any{"annotationId": "discussion", "requestId": uuid.NewString(), "text": "What is the deadline?"})
			f.complete(run.ThreadID, jsonText(Observation{Summary: "Waiting for a deadline.", Outcome: outcome, NextStep: "Resume after the answer."}), "completed", true)
			until(t, func() bool { return attentionCommitted(e, run.ID) })
			r := s.Snapshot()
			if r.Continuations[run.EventID].State != "failed" || r.Events[signal.ID].State != "queued" || len(r.Continuations[run.EventID].ActionIDs) != 1 {
				t.Fatal("plain question text fabricated a durable continuation", r)
			}
		})
	}
}

func TestAttentionDirectRegisteredQuestionLinksActualTaskAndOwner(t *testing.T) {
	for _, outcome := range []string{"needs_owner", "continue", "no_action"} {
		t.Run(outcome, func(t *testing.T) {
			e, f, _, s, signal := directAttention(t)
			connectQuestionTools(e)
			acceptAttentionSignals(t, s, signal)
			e.Start()
			run := active(t, e, "continuation")
			args := questionCommand()
			body := args["question"].(map[string]any)["arguments"].(map[string]any)
			body["text"] = "@Owner Which version must remain supported?"
			body["mentions"] = []map[string]any{{"memberId": "owner", "label": "Owner", "startOffset": 0, "endOffset": 6}}
			q, job := questionPair(t, tool(t, e, f, run, "shadow_task", args))
			observation := Observation{Summary: "Registered the exact compatibility question and continuation.", Outcome: outcome}
			if outcome != "no_action" {
				observation.NextStep = "Read the owner's version choice and continue the original task."
			}
			f.complete(run.ThreadID, jsonText(observation), "completed", true)
			until(t, func() bool { return attentionCommitted(e, run.ID) })
			r := s.Snapshot()
			focus := r.Focuses[r.Events[signal.ID].FocusID]
			if r.Continuations[run.EventID].State != "processed" || len(focus.JobIDs) != 1 || focus.JobIDs[0] != job.ID || len(focus.QuestionIDs) != 1 || focus.QuestionIDs[0] != q.ID || currentJob(t, e, job.ID).State != "waiting" {
				t.Fatal("direct understanding lost real work or completed it implicitly", r.Continuations, focus)
			}
		})
	}
}

func TestAttentionDirectTracksAnExistingTaskModifiedInThisRun(t *testing.T) {
	for _, operation := range []string{"checkpoint", "ask"} {
		t.Run(operation, func(t *testing.T) {
			e, f, _, s, signal := directAttention(t)
			connectQuestionTools(e)
			job := testWaitingJob()
			job.Title, job.Prompt, job.CompletionCriteria = "Existing independent work", "Complete the original work", []string{"Resolve the actual version"}
			e.mu.Lock()
			err := e.mutateLocked(func() error { e.record.Jobs[job.ID] = job; return nil })
			e.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			acceptAttentionSignals(t, s, signal)
			e.Start()
			run := active(t, e, "continuation")
			args := taskCommandFor(job, "checkpoint")
			if operation == "ask" {
				args = questionCommand()
				args["id"], args["baseVersion"], args["baseRevision"] = job.ID, job.Version, job.Revision
			}
			tool(t, e, f, run, "shadow_task", args)
			f.complete(run.ThreadID, jsonText(Observation{Summary: "Updated the actual existing work.", Outcome: "continue", NextStep: "Continue that task after the registered answer."}), "completed", true)
			until(t, func() bool { return attentionCommitted(e, run.ID) })
			r := s.Snapshot()
			focus := r.Focuses[r.Events[signal.ID].FocusID]
			if r.Continuations[run.EventID].State != "processed" || len(focus.JobIDs) != 1 || focus.JobIDs[0] != job.ID {
				t.Fatal("existing task update was absent from Core proof", focus, r.Continuations)
			}
		})
	}
}

func TestAttentionDirectDoesNotInventOwnerWaitFromQuestionToAnotherMember(t *testing.T) {
	e, f, _, s, signal := directAttention(t)
	connectQuestionTools(e)
	acceptAttentionSignals(t, s, signal)
	e.Start()
	run := active(t, e, "continuation")
	args := questionCommand()
	body := args["question"].(map[string]any)["arguments"].(map[string]any)
	body["text"] = "@Peer Which version?"
	body["mentions"] = []map[string]any{{"memberId": "peer", "label": "Peer", "startOffset": 0, "endOffset": 5}}
	q, j := questionPair(t, tool(t, e, f, run, "shadow_task", args))
	f.complete(run.ThreadID, jsonText(Observation{Summary: "Claimed owner is required.", Outcome: "needs_owner", NextStep: "Wait for owner."}), "completed", true)
	until(t, func() bool { return attentionCommitted(e, run.ID) })
	r := s.Snapshot()
	if r.Continuations[run.EventID].State != "failed" || q.Delivery != "saved" || currentJob(t, e, j.ID).State != "waiting" {
		t.Fatal("another recipient became an owner question or task was lost", r.Continuations, q)
	}
}

func TestTaskQuestionCanBeReusedByExactIDWithoutForgedSource(t *testing.T) {
	e, f, _ := setup(t)
	connectQuestionTools(e)
	e.Start()
	send(t, e, "owner", "Two tasks need the same version fact")
	run := active(t, e, "main")
	q, _ := questionPair(t, tool(t, e, f, run, "shadow_task", questionCommand()))
	created := tool(t, e, f, run, "shadow_task", map[string]any{"operation": "start", "title": "Second task", "prompt": "Use the same actual version answer", "kind": "coordination"})
	b, _ := json.Marshal(created)
	var j Job
	if err := json.Unmarshal(b, &j); err != nil {
		t.Fatal(err)
	}
	args := taskCommandFor(j, "wait")
	args["dependencies"] = []TaskDependency{{ID: "shared-version", Kind: "discussion", QuestionID: q.ID, Condition: "Use the confirmed version in this task", Source: spacecontext.SourceRef{SpaceID: "forged-space", AnnotationID: "unrelated"}, QuestionSourcePending: true, QuestionActionID: "forged"}}
	tool(t, e, f, run, "shadow_task", args)
	d := currentJob(t, e, j.ID).Dependencies[0]
	if attentionHash(d.Source) != attentionHash(q.Source) || d.QuestionActionID != q.ActionID || d.QuestionSourcePending {
		t.Fatal("reuse accepted caller-provided source", d, q)
	}
}
