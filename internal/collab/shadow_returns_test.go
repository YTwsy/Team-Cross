package collab

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/inflow"
	"teamcross/internal/mcp"
	"teamcross/internal/shadow"
)

func shadowReturnCaller(t *testing.T, a *App) agentCaller {
	t.Helper()
	if a.Config.ClaudeHome == "" {
		a.Config.ClaudeHome = t.TempDir()
	}
	c := agentCaller{Caller: mcp.CallerSource{Provider: "claude", SourceID: uuid.NewString(), ToolUseID: "verify"}}
	writeAgentClaudeTranscript(t, a, c)
	return c
}

func sendShadowFromCaller(t *testing.T, a *App, space, id string, c agentCaller) (ShadowDelivery, map[string]any) {
	t.Helper()
	args := map[string]any{"spaceId": space, "shadowId": id, "messageId": uuid.NewString(), "text": "Investigate compatibility and return evidence"}
	out, err := a.invokeWorkbenchTool(context.Background(), "send_space_shadow", args, c, "")
	if err != nil {
		t.Fatal(err)
	}
	return workbenchDecode[ShadowDelivery](out), args
}

func TestShadowReturnsUseExactOriginWithoutPairingAcrossCores(t *testing.T) {
	a, s, b, j, _ := consumerFixture(t)
	ctx := context.Background()
	v := consumerRegister(t, a, s.record.ID)
	v.Online = true
	v.Revision++
	shadowOp(t, a, s.record.ID, "sync", shadowInput{ID: v.ID, Snapshot: &v})
	c := shadowReturnCaller(t, b)
	d, args := sendShadowFromCaller(t, b, j.ID, v.ID, c)
	if d.State != "sending" || d.ReturnRouteID == "" {
		t.Fatal(d)
	}
	if len(b.agents.Pairings) != 0 {
		t.Fatal("delegation created a pairing")
	}
	// The same ID is stable across retries and cannot be stolen by another
	// conversation, even when both conversations belong to the same member.
	out, err := b.invokeWorkbenchTool(ctx, "send_space_shadow", args, c, "")
	if err != nil || workbenchDecode[ShadowDelivery](out).ReturnRouteID != d.ReturnRouteID {
		t.Fatal(out, err)
	}
	other := shadowReturnCaller(t, b)
	if _, err = b.invokeWorkbenchTool(ctx, "send_space_shadow", args, other, ""); err == nil {
		t.Fatal("another caller stole the route")
	}
	now := time.Now()
	input := d.Message
	input.ReceivedAt = &now
	input.State = "completed"
	reply := shadow.Message{ID: "reply-original", Role: "assistant", Actor: shadow.Actor{ID: v.ID, Name: v.Name}, ReplyTo: input.ID, Text: "I have checked the request.", State: "completed", CreatedAt: now}
	r := shadow.TaskResult{ID: "result-v1", TaskID: "compatibility", TaskVersion: 1, MessageID: input.ID, SourceID: input.ID, Title: "Compatibility", State: "completed", Outcome: "completed", Text: "Original fixed result", CreatedAt: now.Add(time.Second)}
	// Result coordination prose must not duplicate the business-result return.
	notice := reply
	notice.ID = "result-notice"
	notice.TaskResultID = r.ID
	v.Messages = []shadow.Message{input, reply, notice}
	v.TaskResults = []shadow.TaskResult{r}
	v.Revision++
	shadowOp(t, a, s.record.ID, "sync", shadowInput{ID: v.ID, Snapshot: &v, Results: []shadow.TaskResult{r}})
	// Normal browsing retention does not drop an offline origin's pending result.
	v.TaskResults = nil
	v.Revision++
	shadowOp(t, a, s.record.ID, "sync", shadowInput{ID: v.ID, Snapshot: &v})
	fixed := workbenchDecode[ShadowSourceResult](shadowOp(t, b, j.ID, "source", shadowInput{ID: v.ID, ResultID: r.ID}))
	if fixed.Result == nil || fixed.Result.Text != r.Text {
		t.Fatal("pending source expired", fixed)
	}
	s.mu.Lock()
	hostJSON, _ := json.Marshal(s.record)
	s.mu.Unlock()
	if strings.Contains(string(hostJSON), c.Caller.SourceID) {
		t.Fatal("native session ID leaked to the space host")
	}
	if len(inflowBatch(t, b, other.Caller).Items) != 0 {
		t.Fatal("another session received the return")
	}
	if len(inflowBatch(t, a, c.Caller).Items) != 0 {
		t.Fatal("another Core guessed a return route")
	}
	if err = b.loadAgents(); err != nil {
		t.Fatal(err)
	}
	batch := inflowBatch(t, b, c.Caller)
	if batch.SpaceID != j.ID || batch.ReturnRouteID != d.ReturnRouteID || len(batch.Items) != 2 || batch.Items[1].ResultID != r.ID || batch.Items[1].TaskVersion != 1 || batch.Items[1].SourceID != input.ID {
		t.Fatal(batch)
	}
	encoded, _ := json.Marshal(batch.Items)
	if len(encoded) > inflow.MaxBytes {
		t.Fatal("unbounded return")
	}
	if len(inflowBatch(t, b, c.Caller).Items) != 0 {
		t.Fatal("parallel transport duplicated a lease")
	}
	finishInflow(t, b, c.Caller, batch, "release")
	if _, err = b.configureCurrentInFlow(ctx, j.ID, c.Caller, "", false); err != nil {
		t.Fatal(err)
	}
	if len(inflowBatch(t, b, c.Caller).Items) != 0 {
		t.Fatal("explicit mute ignored")
	}
	if _, err = b.configureCurrentInFlow(ctx, j.ID, c.Caller, "", true); err != nil {
		t.Fatal(err)
	}
	batch = inflowBatch(t, b, c.Caller)
	// A disk failure must not mark pending entries delivered in memory.
	file := filepath.Join(s.runtimeDirectory(), "collaboration.json")
	backup := file + ".fixture-backup"
	if err = os.Rename(file, backup); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(file, 0700); err != nil {
		t.Fatal(err)
	}
	_, err = b.personalInFlow(ctx, inFlowCall{Caller: c.Caller, Operation: "delivered", Batch: batch})
	if err == nil {
		t.Fatal("failed save reported delivery")
	}
	if err = os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(backup, file); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	delivered := len(s.record.Shadows[v.ID].Returns[d.ReturnRouteID].Delivered)
	s.mu.Unlock()
	if delivered != 0 {
		t.Fatal("failed save advanced receipt")
	}
	finishInflow(t, b, c.Caller, batch, "delivered")
	finishInflow(t, b, c.Caller, batch, "delivered")
	var disk Record
	if err = readJSON(file, &disk); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.record.Shadows = disk.Shadows
	s.inFlowLeases = nil
	s.mu.Unlock()
	if len(inflowBatch(t, b, c.Caller).Items) != 0 {
		t.Fatal("delivered return replayed after restart")
	}
	// Retried exports cannot recreate an acknowledged result or rewrite it.
	v.Revision++
	shadowOp(t, a, s.record.ID, "sync", shadowInput{ID: v.ID, Snapshot: &v, Results: []shadow.TaskResult{r}})
	if len(inflowBatch(t, b, c.Caller).Items) != 0 {
		t.Fatal("retried export was redelivered")
	}
	corrupt := r
	corrupt.Text = "changed"
	if _, err = a.shadowCall(ctx, s.record.ID, "sync", shadowInput{ID: v.ID, Snapshot: &v, Results: []shadow.TaskResult{corrupt}}); err == nil {
		t.Fatal("retained result was overwritten")
	}
	r2 := r
	r2.ID = "result-v2"
	r2.TaskVersion = 2
	r2.Text = "New goal result"
	v.Revision++
	v.TaskResults = []shadow.TaskResult{r2}
	shadowOp(t, a, s.record.ID, "sync", shadowInput{ID: v.ID, Snapshot: &v, Results: []shadow.TaskResult{r2}})
	batch = inflowBatch(t, b, c.Caller)
	if len(batch.Items) != 1 || batch.Items[0].ResultID != r2.ID {
		t.Fatal(batch)
	}
	if err = s.RevokeMember(d.Message.Actor.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = b.personalInFlow(ctx, inFlowCall{Caller: c.Caller, Operation: "delivered", Batch: batch}); err == nil {
		t.Fatal("revoked member committed a return")
	}
}

func TestShadowReturnBindingFailsBeforeSendAndUsesBoundRuntimeIdentity(t *testing.T) {
	a, _, s := spaceFixture(t)
	v := consumerRegister(t, a, s.record.ID)
	v.Online = true
	v.Revision++
	shadowOp(t, a, s.record.ID, "sync", shadowInput{ID: v.ID, Snapshot: &v})
	c := shadowReturnCaller(t, a)
	file := filepath.Join(a.Config.DataDir, "agent-pairings.json")
	a.agentMu.Lock()
	err := a.saveAgentsLocked(libraryClone(a.agents))
	a.agentMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(file, file+".fixture-backup"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(file, 0700); err != nil {
		t.Fatal(err)
	}
	in := shadowInput{ID: v.ID, Message: shadow.Message{ID: uuid.NewString(), Text: "No send if origin cannot persist"}}
	if _, err := a.bindShadowReturn(s.record.ID, in, c.Caller); err == nil {
		t.Fatal("unpersisted origin accepted")
	}
	if err = os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(file+".fixture-backup", file); err != nil {
		t.Fatal(err)
	}
	if len(a.agents.ShadowReturns) != 0 {
		t.Fatal("failed save mutated local routes")
	}
	runtimeID, sourceID := uuid.NewString(), uuid.NewString()
	ex := &Session{mu: &sync.Mutex{}, record: Record{ID: runtimeID, ExecutionRecord: &ExecutionRecord{Provider: "codex", SessionID: sourceID}, SpaceID: s.record.ID}}
	a.mu.Lock()
	a.executions[runtimeID] = ex
	a.mu.Unlock()
	t.Cleanup(func() { a.mu.Lock(); delete(a.executions, runtimeID); a.mu.Unlock() })
	args := map[string]any{"shadowId": v.ID, "messageId": uuid.NewString(), "text": "Bound runtime request"}
	out, err := a.invokeWorkbenchTool(context.Background(), "send_space_shadow", args, c, runtimeID)
	if err != nil {
		t.Fatal(err)
	}
	d := workbenchDecode[ShadowDelivery](out)
	route := a.agents.ShadowReturns[d.ReturnRouteID]
	if route.Provider != "codex" || route.SessionID != sourceID || route.SessionID == c.Caller.SourceID {
		t.Fatal("runtime identity not authoritative", route)
	}
}

func TestShadowReturnsHonorActiveTargetMuteWithoutDependingOnAssociation(t *testing.T) {
	a, _, s := spaceFixture(t)
	_, caller, target := spaceChannel(t, a, s.record.ID)
	v := consumerRegister(t, a, s.record.ID)
	v.Online = true
	v.Revision++
	shadowOp(t, a, s.record.ID, "sync", shadowInput{ID: v.ID, Snapshot: &v})
	d, _ := sendShadowFromCaller(t, a, s.record.ID, v.ID, caller)
	now := time.Now()
	input := d.Message
	input.ReceivedAt = &now
	v.Messages = []shadow.Message{input, {ID: "exact-reply", Role: "assistant", Actor: shadow.Actor{ID: v.ID, Name: v.Name}, ReplyTo: input.ID, Text: "The original answer", State: "completed", CreatedAt: now}}
	v.Revision++
	shadowOp(t, a, s.record.ID, "sync", shadowInput{ID: v.ID, Snapshot: &v})
	wb(t, a, s.record.ID, "inflow-config", workbenchInput{TargetID: target.ID, Enabled: false})
	if len(inflowBatch(t, a, caller.Caller).Items) != 0 {
		t.Fatal("active target's explicit mute was ignored")
	}
	wb(t, a, s.record.ID, "inflow-config", workbenchInput{TargetID: target.ID, Enabled: true})
	batch := inflowBatch(t, a, caller.Caller)
	if len(batch.Items) != 1 || batch.Items[0].Kind != "shadow_reply" {
		t.Fatal(batch)
	}
	finishInflow(t, a, caller.Caller, batch, "release")
	// Removing an ordinary association disables that obsolete preference, but
	// must not silently cancel the independently established task return route.
	wb(t, a, s.record.ID, "remove", workbenchInput{TargetID: target.ID})
	batch = inflowBatch(t, a, caller.Caller)
	if len(batch.Items) != 1 || batch.Items[0].MessageID != "exact-reply" {
		t.Fatal("removed target suppressed a task return", batch)
	}
	finishInflow(t, a, caller.Caller, batch, "delivered")
}
