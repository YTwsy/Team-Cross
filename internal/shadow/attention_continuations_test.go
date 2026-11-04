package shadow

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/mcp"
)

func continuingAttention(t *testing.T) (*Engine, *fakeNative, Config, *AttentionStore, Run) {
	t.Helper()
	e, f, cfg := setup(t)
	e.cfg.ReadSource, cfg.ReadSource = observationReader, observationReader
	e.cfg.CollaborationTools = mcp.ShadowTools()
	e.cfg.CallTool = func(_ context.Context, name string, args map[string]any) (any, error) {
		return map[string]any{"status": "saved", "requestId": args["requestId"]}, nil
	}
	cfg.CollaborationTools, cfg.CallTool = e.cfg.CollaborationTools, e.cfg.CallTool
	s, signal := attachEngineAttention(t, e)
	acceptAttentionSignals(t, s, signal)
	e.Start()
	observation := active(t, e, "attention")
	f.complete(observation.ThreadID, observationJSON(t, observation, Observation{Summary: "The original question needs a factual answer.", Outcome: "continue", NextStep: "Read the current discussion and answer with the compatibility evidence."}), "completed", true)
	run := active(t, e, "continuation")
	return e, f, cfg, s, run
}

func TestAttentionContinuationActsWithoutInventingSharedUserMessages(t *testing.T) {
	e, f, _, s, run := continuingAttention(t)
	if len(e.View().Messages) != 0 {
		t.Fatal("automatic event forged a chat message")
	}
	id := uuid.NewString()
	tool(t, e, f, run, "read_annotations", map[string]any{"annotationId": "discussion"})
	tool(t, e, f, run, "reply_to_annotation", map[string]any{"annotationId": "discussion", "requestId": id, "text": "The verified compatibility answer."})
	f.complete(run.ThreadID, "Answered the original discussion using the verified source.", "completed", true)
	until(t, func() bool { return attentionCommitted(e, run.ID) })
	c := s.Snapshot().Continuations[run.EventID]
	if c.State != "processed" || len(c.ActionIDs) != 1 || c.ActionIDs[0] != id || len(e.View().Messages) != 0 {
		t.Fatal("coordination was lost or published as extra chat", c, e.View().Messages)
	}
}

func TestAttentionContinuationTaskKeepsEventOriginAndWorkersCanContinue(t *testing.T) {
	e, f, _, _, run := continuingAttention(t)
	e.mu.Lock()
	e.record.Topics["unrelated"] = Topic{ID: "unrelated", Goal: "UNRELATED_PRIVATE_CONTEXT", State: "active"}
	e.mu.Unlock()
	created := cloneMap(tool(t, e, f, run, "shadow_task", map[string]any{"operation": "start", "title": "Check compatibility", "prompt": "Read the exact discussion and compare supported client versions."}))
	jobID := created["id"].(string)
	worker := active(t, e, "job")
	e.mu.Lock()
	job := clone(e.record.Jobs[jobID])
	prompt := e.workerPromptLocked(job)
	e.mu.Unlock()
	if job.Trigger == nil || job.Trigger.Kind != "activity" || job.SourceID != run.SourceID || job.Trigger.ContinuationID != run.EventID || len(job.Trigger.Sources) != 1 || strings.Contains(prompt, "UNRELATED_PRIVATE_CONTEXT") {
		t.Fatal("worker lost the actual bounded source", job, prompt)
	}
	child := cloneMap(tool(t, e, f, worker, "shadow_task", map[string]any{"operation": "start", "title": "Verify one constraint", "prompt": "Independently verify the old client parser limitation using the referenced discussion."}))
	e.mu.Lock()
	childJob := e.record.Jobs[child["id"].(string)]
	e.mu.Unlock()
	if childJob.ParentID != jobID || childJob.Trigger == nil || childJob.Trigger.ContinuationID != run.EventID {
		t.Fatal("worker could not continue same-origin work", childJob)
	}
	tool(t, e, f, worker, "shadow_topics", map[string]any{"id": "compatibility", "goal": "Finish the original compatibility question", "state": "active", "constraints": []string{"Use fixed source versions"}, "sourceIds": []string{run.SourceID}})
}

func TestAttentionContinuationLateActionCannotUseOldGoal(t *testing.T) {
	e, _, _, s, run := continuingAttention(t)
	c := s.Snapshot().Continuations[run.EventID]
	focus := s.Snapshot().Focuses[c.FocusID]
	if _, err := s.UpdateFocus(focus.ID, focus.Revision, "Investigate the revised requirement", "active"); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"annotationId": "discussion", "requestId": uuid.NewString(), "text": "obsolete answer"})
	if _, err := e.callCollaborationTool(context.Background(), toolCall{ThreadID: run.ThreadID, TurnID: run.TurnID, CallID: uuid.NewString(), Tool: "reply_to_annotation", Arguments: args}); err == nil {
		t.Fatal("old goal still wrote a public answer")
	}
	e.mu.Lock()
	count := len(e.record.Actions)
	e.mu.Unlock()
	if count != 0 {
		t.Fatal("stale action was submitted")
	}
}

func TestAttentionContinuationRecoveryDoesNotResendCompletedAction(t *testing.T) {
	e, f, cfg, s, run := continuingAttention(t)
	id := uuid.NewString()
	tool(t, e, f, run, "reply_to_annotation", map[string]any{"annotationId": "discussion", "requestId": id, "text": "One persisted reply."})
	e.Close()
	f.complete(run.ThreadID, "The original reply was saved; no further action.", "completed", false)
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
	turnCount := len(f.threads[run.ThreadID])
	f.mu.Unlock()
	if turnCount != 1 || store.Snapshot().Continuations[run.EventID].State != "processed" || len(e2.View().Messages) != 0 {
		t.Fatal("recovery repeated the continuation or leaked internal output", turnCount)
	}
}

func TestAttentionQueuedContinuationTransfersWithDefaultRoute(t *testing.T) {
	s, r, signal := attentionFixture(t)
	acceptAttentionSignals(t, s, signal)
	batch, err := s.ClaimBatch(r.Route.ShadowID, time.Now(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	batch = prepareObservation(t, s, batch, 1)
	if err = s.FinishBatch(batch.ID, Observation{Summary: "A concrete question is ready.", Outcome: "continue", NextStep: "Answer the question."}); err != nil {
		t.Fatal(err)
	}
	next := r.Route
	next.ShadowID, next.Revision = "next-shadow", 2
	if err = s.SetRoute(next); err != nil {
		t.Fatal(err)
	}
	if s.HasContinuation(r.Route.ShadowID) || !s.HasContinuation(next.ShadowID) {
		t.Fatal("queued continuation stayed on old route")
	}
	c, err := s.ClaimContinuation(next.ShadowID)
	if err != nil || c == nil || c.Continuation.State != "running" {
		t.Fatal(c, err)
	}
}
