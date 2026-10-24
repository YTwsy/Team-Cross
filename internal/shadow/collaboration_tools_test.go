package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
	"teamcross/internal/mcp"
)

func TestCollaborationLedgersRemainWritableAfterFailedRollback(t *testing.T) {
	e, _, cfg := setup(t)
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.mutateLocked(func() error { return fmt.Errorf("rejected before save") }); err == nil {
		t.Fatal("fixture mutation should fail")
	}
	path := filepath.Join(cfg.Directory, "shadow.json")
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := e.mutateLocked(func() error {
		e.record.Actions["failed"] = CollaborationAction{ID: "failed", State: "pending"}
		e.record.AttentionThreads["failed"] = "not-committed"
		return nil
	}); err == nil {
		t.Fatal("failed storage was accepted")
	}
	if len(e.record.Actions) != 0 || len(e.record.AttentionThreads) != 0 {
		t.Fatal("failed action survived rollback")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	if err := e.mutateLocked(func() error {
		e.record.Actions["after-recovery"] = CollaborationAction{ID: "after-recovery", State: "pending"}
		e.record.AttentionThreads["after-recovery"] = "actual-thread"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCollaborationActionSurvivesReceiptLossAndNativeTurnChange(t *testing.T) {
	e, f, cfg := setup(t)
	e.cfg.CollaborationTools = mcp.ShadowTools()
	var mu sync.Mutex
	calls, writes := 0, 0
	receipts := map[string]any{}
	e.cfg.CallTool = func(_ context.Context, name string, args map[string]any) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		id := args["requestId"].(string)
		if old, ok := receipts[id]; ok {
			return old, nil
		}
		writes++
		receipts[id] = map[string]any{"status": "saved", "replyId": "persisted-reply"}
		return nil, fmt.Errorf("receipt lost after host saved reply")
	}
	cfg.CollaborationTools, cfg.CallTool = e.cfg.CollaborationTools, e.cfg.CallTool
	e.Start()
	send(t, e, "owner", "Answer this discussion")
	first := active(t, e, "main")
	id := uuid.NewString()
	args := map[string]any{"annotationId": "discussion", "requestId": id, "text": "The evidenced answer."}
	encoded, _ := json.Marshal(args)
	call := toolCall{ThreadID: first.ThreadID, TurnID: first.TurnID, CallID: uuid.NewString(), Tool: "reply_to_annotation", Arguments: encoded}
	if _, err := e.callCollaborationTool(context.Background(), call); err == nil {
		t.Fatal("lost receipt reported saved")
	}
	e.mu.Lock()
	state := e.record.Actions[id].State
	e.mu.Unlock()
	if state != "unknown" {
		t.Fatal(state)
	}
	f.complete(first.ThreadID, "I will reconcile that saved action.", "completed", true)
	until(t, func() bool { return !e.View().Busy })
	send(t, e, "owner", "Continue the same action")
	second := active(t, e, "main")
	tool(t, e, f, second, "reply_to_annotation", args)
	e.Close()
	restored, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Close)
	restored.Start()
	second = active(t, restored, "main")
	tool(t, restored, f, second, "reply_to_annotation", args)
	mu.Lock()
	gotCalls, gotWrites := calls, writes
	mu.Unlock()
	if gotCalls != 2 || gotWrites != 1 {
		t.Fatal("saved action reran after native turn/restart", gotCalls, gotWrites)
	}
	args["text"] = "Changed action body"
	encoded, _ = json.Marshal(args)
	call.ThreadID, call.TurnID, call.Arguments = second.ThreadID, second.TurnID, encoded
	if _, err := restored.callCollaborationTool(context.Background(), call); err == nil {
		t.Fatal("changed action body reused ID")
	}
}

func TestCollaborationActionPersistsIntentBeforeCallingBusinessService(t *testing.T) {
	e, f, cfg := setup(t)
	e.cfg.CollaborationTools = mcp.ShadowTools()
	called := false
	e.cfg.CallTool = func(context.Context, string, map[string]any) (any, error) { called = true; return nil, nil }
	e.Start()
	send(t, e, "owner", "Reply with evidence")
	run := active(t, e, "main")
	if err := os.Rename(cfg.Directory, cfg.Directory+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.Directory, []byte("block persistence"), 0600); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"requestId": uuid.NewString(), "annotationId": "discussion", "text": "answer"})
	_, err := e.callCollaborationTool(context.Background(), toolCall{ThreadID: run.ThreadID, TurnID: run.TurnID, CallID: uuid.NewString(), Tool: "reply_to_annotation", Arguments: args})
	if err == nil || called {
		t.Fatal("business write occurred without durable intent", err)
	}
	if err := os.Remove(cfg.Directory); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(cfg.Directory+"-saved", cfg.Directory); err != nil {
		t.Fatal(err)
	}
	f.complete(run.ThreadID, "Source preserved.", "completed", true)
}
