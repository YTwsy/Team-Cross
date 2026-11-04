package collab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"teamcross/internal/mcp"
)

func connectInFlowFixture(t *testing.T, a *App, space string, caller mcp.CallerSource) SpaceTarget {
	t.Helper()
	out, err := a.connectInFlow(context.Background(), space, caller, "")
	if err != nil {
		t.Fatal(err)
	}
	id := out.(map[string]any)["targetId"].(string)
	view := workbenchDecode[WorkbenchView](wb(t, a, space, "view", workbenchInput{}))
	for _, target := range view.Targets {
		if target.ID == id {
			return target
		}
	}
	t.Fatal("missing registered target")
	return SpaceTarget{}
}

func TestInFlowConnectWithoutTurnOrActiveReceiver(t *testing.T) {
	a, f, s := spaceFixture(t)
	caller := mcp.CallerSource{Provider: "codex", SourceID: f.source.ID}
	f.source.Name = "准确的本机会话"
	target := connectInFlowFixture(t, a, s.record.ID, caller)
	if !target.InFlow || target.Available || target.Name != f.source.Name {
		t.Fatal(target)
	}
	if len(a.agentProxies) != 0 || len(a.agentReceivers) != 0 || len(a.executions) != 0 || len(a.receivers) != 0 {
		t.Fatal("created an active receiver")
	}
	s.mu.Lock()
	baseline := s.record.Workbench.InFlow[inFlowRecipient(target.ID, "")].Since
	s.mu.Unlock()
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := a.connectInFlow(context.Background(), s.record.ID, caller, "")
			if err != nil || out.(map[string]any)["targetId"] != target.ID {
				t.Error(out, err)
			}
		}()
	}
	wg.Wait()
	s.mu.Lock()
	if !s.record.Workbench.InFlow[inFlowRecipient(target.ID, "")].Since.Equal(baseline) || len(s.record.Workbench.Targets) != 1 {
		t.Error("retry changed baseline or target")
	}
	s.mu.Unlock()
	if len(a.agentPairings()) != 1 {
		t.Fatal("duplicate local association")
	}
	note := workbenchDecode[SpaceNote](wb(t, a, s.record.ID, "inflow-note", workbenchInput{RequestID: uuid.NewString(), TargetID: target.ID, Instruction: "after enabling"}))
	batch := inflowBatch(t, a, caller)
	if len(batch.Items) != 1 || batch.Items[0].SourceID != note.ID {
		t.Fatal(batch)
	}
	finishInflow(t, a, caller, batch, "delivered")
	// Reload the durable identity and preferences without borrowing a client.
	if err := a.loadAgents(); err != nil {
		t.Fatal(err)
	}
	if len(inflowBatch(t, a, caller).Items) != 0 {
		t.Fatal("replayed on reload")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, method := range f.calls {
		if method != "thread/read" {
			t.Errorf("unexpected native operation: %s", method)
		}
	}
}

func TestInFlowConnectClaudeCatalogWithoutToolProof(t *testing.T) {
	a, _, s := spaceFixture(t)
	a.Config.ClaudeHome = t.TempDir()
	c := agentCaller{Caller: mcp.CallerSource{Provider: "claude", SourceID: uuid.NewString(), ToolUseID: "fixture"}}
	writeAgentClaudeTranscript(t, a, c)
	c.Caller.ToolUseID = ""
	target := connectInFlowFixture(t, a, s.record.ID, c.Caller)
	if !target.InFlow || target.Provider != "claude" || target.Available {
		t.Fatal(target)
	}
	if len(a.agentReceivers) != 0 {
		t.Fatal("required Channel verification")
	}
}

func TestInFlowConnectPreservesVerifiedReceiverAndCustomName(t *testing.T) {
	a, _, s := spaceFixture(t)
	p, caller, target := spaceChannel(t, a, s.record.ID)
	caller.Caller.ToolUseID = ""
	out, err := a.connectInFlow(context.Background(), s.record.ID, caller.Caller, "")
	if err != nil || out.(map[string]any)["targetId"] != target.ID {
		t.Fatal(out, err)
	}
	a.agentMu.Lock()
	stored := a.agents.Pairings[p.ID]
	a.agentMu.Unlock()
	if stored.State != "paired" || stored.Transport != p.Transport || stored.ReceiverID != p.ReceiverID || stored.Name != p.Name {
		t.Fatal("passive setup changed verified reception", stored)
	}
	wb(t, a, s.record.ID, "remove", workbenchInput{TargetID: target.ID})
	if len(inflowBatch(t, a, caller.Caller).Items) != 0 {
		t.Fatal("removed target received updates")
	}
	if _, err := a.connectInFlow(context.Background(), s.record.ID, caller.Caller, ""); err != nil {
		t.Fatal(err)
	}
	view := workbenchDecode[WorkbenchView](wb(t, a, s.record.ID, "view", workbenchInput{}))
	if len(view.Targets) != 1 || view.Targets[0].ID != target.ID || !view.Targets[0].InFlow {
		t.Fatal(view.Targets)
	}
}

func TestInFlowConnectHTTPKeepsIdentityBoundaries(t *testing.T) {
	a, f, s := spaceFixture(t)
	handler := a.Handler(http.NotFoundHandler())
	request := func(path string, body any, token, origin string) *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "/api/"+path, strings.NewReader(string(data)))
		r.Host, r.RemoteAddr = "127.0.0.1", "127.0.0.1:12345"
		r.Header.Set("Authorization", token)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	path := "collaborations/" + s.record.ID + "/workbench/inflow-connect"
	for _, body := range []any{
		map[string]any{"provider": "codex", "sourceId": uuid.NewString()},
		map[string]any{"provider": "chatgpt", "sourceId": strings.Repeat("a", 64)},
		map[string]any{"provider": "codex", "sourceId": f.source.ID, "actor": map[string]string{"memberId": "other"}},
	} {
		if w := request(path, body, "Bearer "+a.Token, ""); w.Code < 400 {
			t.Fatal("accepted invalid source", w.Body.String())
		}
	}
	if len(a.agentPairings()) != 0 {
		t.Fatal("invalid request persisted an association")
	}
	if w := request(path, inFlowSourceInput{Provider: "codex", SourceID: f.source.ID}, "Bearer "+a.Token, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	plugin := map[string]any{"spaceId": s.record.ID, "caller": mcp.CallerSource{Provider: "chatgpt", SourceID: strings.Repeat("a", 64)}}
	for _, auth := range []struct{ token, origin string }{{"", ""}, {"Bearer " + a.Token, "https://example.invalid"}} {
		if w := request("agent-current-inflow", plugin, auth.token, auth.origin); w.Code != 403 {
			t.Fatal("untrusted plugin caller", w.Code)
		}
	}
	if w := request("agent-current-inflow", plugin, "Bearer "+a.Token, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestInFlowConnectThreeCoresOwnSessionsAndRevocation(t *testing.T) {
	a, _, s := spaceFixture(t)
	ctx := context.Background()
	if err := s.Share(ctx, "lan"); err != nil {
		t.Fatal(err)
	}
	b, bf, _ := fixture(t)
	c, cf, _ := fixture(t)
	stopWorkbenchPump(b)
	stopWorkbenchPump(c)
	jb, err := b.Join(ctx, s.sharedRuntime().Token())
	if err != nil {
		t.Fatal(err)
	}
	jc, err := c.Join(ctx, s.sharedRuntime().Token())
	if err != nil {
		t.Fatal(err)
	}
	bc := mcp.CallerSource{Provider: "codex", SourceID: bf.source.ID}
	cc := mcp.CallerSource{Provider: "codex", SourceID: cf.source.ID}
	bt := connectInFlowFixture(t, b, jb.ID, bc)
	ct := connectInFlowFixture(t, c, jc.ID, cc)
	if bt.MemberID == ct.MemberID || bt.MemberID == "owner" {
		t.Fatal(bt, ct)
	}
	if _, err = c.workbenchCall(ctx, jc.ID, "inflow-config", workbenchInput{TargetID: bt.ID, Enabled: false}); err == nil {
		t.Fatal("another member changed preference")
	}
	note := workbenchDecode[SpaceNote](wb(t, a, s.record.ID, "inflow-note", workbenchInput{RequestID: uuid.NewString(), TargetID: bt.ID, Instruction: "for the second member"}))
	batch := inflowBatch(t, b, bc)
	if len(batch.Items) != 1 || batch.Items[0].SourceID != note.ID || batch.SpaceID != jb.ID {
		t.Fatal(batch)
	}
	if len(inflowBatch(t, c, cc).Items) != 0 || len(inflowBatch(t, b, cc).Items) != 0 {
		t.Fatal("delivered to another conversation")
	}
	public, _ := json.Marshal(wb(t, a, s.record.ID, "view", workbenchInput{}))
	if strings.Contains(string(public), bf.source.ID) || strings.Contains(string(public), cf.source.ID) {
		t.Fatal("native identity exposed")
	}
	if err := s.RevokeMember(bt.MemberID); err != nil {
		t.Fatal(err)
	}
	if len(inflowBatch(t, b, bc).Items) != 0 {
		t.Fatal("revoked member received updates")
	}
	if _, err = b.connectInFlow(ctx, jb.ID, bc, ""); err == nil {
		t.Fatal("revoked member reconnected")
	}
	if _, err = b.personalInFlow(ctx, inFlowCall{Caller: bc, Operation: "delivered", Batch: batch}); err == nil {
		t.Fatal("revoked member committed delivery")
	}
	if len(b.agentProxies) != 0 || len(c.agentProxies) != 0 {
		t.Fatal("passive members acquired writers")
	}
}
