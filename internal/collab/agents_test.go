package collab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/mcp"
)

func agentFixture(t *testing.T) (*App, *fakeRuntime, *Session, AgentPairing, agentCaller) {
	t.Helper()
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	created, err := a.createAgentPairing("设计讨论")
	if err != nil {
		t.Fatal(err)
	}
	c := agentCaller{Caller: mcp.CallerSource{Provider: "codex", SourceID: s.record.SessionID, TurnID: "turn-fixture"}}
	paired, err := a.pairAgent(context.Background(), agentPairInput{agentCaller: c, Code: created["code"].(string)}, s.record.ID)
	if err != nil {
		t.Fatal(err)
	}
	return a, f, s, paired, c
}
func agentInput(p AgentPairing, s *Session) agentRequestInput {
	return agentRequestInput{RequestID: uuid.NewString(), PairingID: p.ID, References: []LibraryReference{{SpaceID: s.record.ID, Kind: "context"}}, Instruction: "核对原文后告诉我建议", Intent: "analyze"}
}

func TestPairingBindsExactRuntimeAndHidesSecrets(t *testing.T) {
	a, f, s, p, c := agentFixture(t)
	if p.State != "paired" || p.SessionID != s.record.SessionID || p.Transport != "native" || p.CodeHash != "" || p.Challenge != "" {
		t.Fatal(p)
	}
	raw, err := os.ReadFile(filepath.Join(a.Config.DataDir, "agent-pairings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "TCP-") {
		t.Fatal("persisted plaintext pairing code")
	}
	for _, v := range a.agentPairings() {
		if v.CodeHash != "" || v.ReceiverID != "" || v.Challenge != "" {
			t.Fatal("leaked secret", v)
		}
	}
	f.mu.Lock()
	for _, call := range f.calls {
		if call == "turn/start" {
			t.Fatal("pairing started a model turn")
		}
	}
	f.mu.Unlock()
	other, err := a.createAgentPairing("other")
	if err != nil {
		t.Fatal(err)
	}
	bad := c
	bad.Caller.SourceID = uuid.NewString()
	if _, err = a.pairAgent(context.Background(), agentPairInput{agentCaller: bad, Code: other["code"].(string)}, s.record.ID); err == nil {
		t.Fatal("accepted another conversation using a runtime token")
	}
}

func TestPairingExpiryOneUseAndPersonalCodexNotFalselyReady(t *testing.T) {
	a, f, _ := fixture(t)
	ctx := context.Background()
	created, _ := a.createAgentPairing("个人会话")
	code := created["code"].(string)
	c := agentCaller{Caller: mcp.CallerSource{Provider: "codex", SourceID: f.source.ID, TurnID: "turn-fixture"}}
	p, err := a.pairAgent(ctx, agentPairInput{agentCaller: c, Code: code}, "")
	if err != nil || p.State != "unsupported" {
		t.Fatal(p, err)
	}
	if _, err = a.sendAgentRequest(ctx, agentRequestInput{RequestID: uuid.NewString(), PairingID: p.ID, Intent: "analyze", Instruction: "read"}); err == nil {
		t.Fatal("unsupported target sent input")
	}
	c.Caller.SourceID = uuid.NewString()
	if _, err = a.pairAgent(ctx, agentPairInput{agentCaller: c, Code: code}, ""); err == nil {
		t.Fatal("reused code in another conversation")
	}
	a.agentMu.Lock()
	old := a.agents.Pairings[p.ID]
	old.ExpiresAt = time.Now().Add(-time.Second)
	a.agents.Pairings[p.ID] = old
	a.agentMu.Unlock()
	if _, err = a.pairAgent(ctx, agentPairInput{agentCaller: c, Code: code}, ""); err == nil {
		t.Fatal("accepted expired code")
	}
}

func TestAgentRequestIdempotencyReceiptAndCompletion(t *testing.T) {
	a, f, s, p, c := agentFixture(t)
	ctx := context.Background()
	in := agentInput(p, s)
	r, err := a.sendAgentRequest(ctx, in)
	if err != nil || r.State != "submitted" {
		t.Fatal(r, err)
	}
	again, err := a.sendAgentRequest(ctx, in)
	if err != nil || again.ID != r.ID {
		t.Fatal(again, err)
	}
	changed := in
	changed.Instruction = "different"
	if _, err = a.sendAgentRequest(ctx, changed); err == nil {
		t.Fatal("reused request ID for another instruction")
	}
	f.mu.Lock()
	calls := 0
	for _, v := range f.calls {
		if v == "turn/start" {
			calls++
		}
	}
	prompt, _ := json.Marshal(f.requests["turn/start"])
	f.mu.Unlock()
	if calls != 1 || !strings.Contains(string(prompt), r.ID) || strings.Contains(string(prompt), in.Instruction) {
		t.Fatal("unexpected delivery", calls, string(prompt))
	}
	if _, err = a.finishAgentRequest(ctx, c, r.ID, s.record.ID, "completed", "done"); err == nil {
		t.Fatal("finished without reading")
	}
	wrong := c
	wrong.Caller.SourceID = uuid.NewString()
	if _, err = a.readAgentRequest(ctx, wrong, r.ID, "", 0); err == nil {
		t.Fatal("another conversation read request")
	}
	read, err := a.readAgentRequest(ctx, c, r.ID, s.record.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if read.(map[string]any)["request"].(AgentRequest).State != "received" {
		t.Fatal(read)
	}
	done, err := a.finishAgentRequest(ctx, c, r.ID, s.record.ID, "completed", "已核对，建议补充样本。")
	if err != nil || done.State != "completed" {
		t.Fatal(done, err)
	}
	if _, err = a.finishAgentRequest(ctx, c, r.ID, s.record.ID, "completed", done.Summary); err != nil {
		t.Fatal(err)
	}
	if _, err = a.finishAgentRequest(ctx, c, r.ID, s.record.ID, "completed", "different"); err == nil {
		t.Fatal("overwrote a completion")
	}
}

func TestAgentRequestsRespectOwnershipScopeAndRevocation(t *testing.T) {
	for _, what := range []string{"writer", "busy", "approval", "scope", "unpaired"} {
		t.Run(what, func(t *testing.T) {
			a, f, s, p, _ := agentFixture(t)
			in := agentInput(p, s)
			s.mu.Lock()
			switch what {
			case "writer":
				s.writer = "other"
			case "busy":
				s.busy = true
			case "approval":
				s.approvals["pending"] = Approval{}
			case "scope":
				in.References[0].SpaceID = uuid.NewString()
			}
			s.mu.Unlock()
			if what == "unpaired" {
				a.agentMu.Lock()
				delete(a.agents.Pairings, p.ID)
				a.agentMu.Unlock()
			}
			if _, err := a.sendAgentRequest(context.Background(), in); err == nil {
				t.Fatal("accepted invalid send")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			for _, call := range f.calls {
				if call == "turn/start" {
					t.Fatal("started a model")
				}
			}
		})
	}
	a, _, s, p, c := agentFixture(t)
	in := agentInput(p, s)
	r, err := a.sendAgentRequest(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	a.agentMu.Lock()
	delete(a.agents.Pairings, p.ID)
	a.agentMu.Unlock()
	if _, err = a.readAgentRequest(context.Background(), c, r.ID, s.record.ID, 0); err == nil {
		t.Fatal("read after unpairing")
	}
}

func TestConcurrentSendStartsOnlyOneTurn(t *testing.T) {
	a, f, s, p, _ := agentFixture(t)
	in := agentInput(p, s)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := a.sendAgentRequest(context.Background(), in)
			if err != nil && !strings.Contains(err.Error(), "正在运行") {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, call := range f.calls {
		if call == "turn/start" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("duplicate input", count)
	}
}

func TestChannelRequiresSecretReceiptAndNeverPersistsLiveReceiver(t *testing.T) {
	a, _, _ := fixture(t)
	c := agentCaller{Caller: mcp.CallerSource{Provider: "claude", SourceID: uuid.NewString(), ToolUseID: "test"}, ReceiverID: uuid.NewString()}
	a.Config.ClaudeHome = t.TempDir()
	writeAgentClaudeTranscript(t, a, c)
	p := AgentPairing{ID: uuid.NewString(), Name: "Claude", Provider: "claude", SessionID: c.Caller.SourceID, State: "verifying", Transport: "claude_channel", ReceiverID: c.ReceiverID, Challenge: "notification-only", ExpiresAt: time.Now().Add(time.Minute)}
	a.agentMu.Lock()
	a.agents.Pairings[p.ID] = p
	a.agentMu.Unlock()
	if _, err := a.confirmAgentPairing(context.Background(), c, p.ID, "wrong"); err == nil {
		t.Fatal("accepted incorrect receipt")
	}
	other := c
	other.Caller.SourceID = uuid.NewString()
	if _, err := a.confirmAgentPairing(context.Background(), other, p.ID, p.Challenge); err == nil {
		t.Fatal("accepted receipt from another session")
	}
	if a.agentPairings()[0].State != "verifying" {
		t.Fatal("ready before proof")
	}
	confirmed, err := a.confirmAgentPairing(context.Background(), c, p.ID, p.Challenge)
	if err != nil || confirmed.State != "paired" || confirmed.Challenge != "" {
		t.Fatal(confirmed, err)
	}
	if a.agentPairings()[0].Reason == "" {
		t.Fatal("disconnected channel appeared online")
	}
	a.agentMu.Lock()
	a.receiverLocked(c.ReceiverID).lastSeen = time.Now()
	a.agentMu.Unlock()
	if a.agentPairings()[0].Reason != "" {
		t.Fatal("active receiver offline")
	}
	if err = a.loadAgents(); err != nil {
		t.Fatal(err)
	}
	if a.agentPairings()[0].Reason == "" {
		t.Fatal("persisted lease survived restart")
	}
}

func TestAgentToolRoutesRequireCredentialAndNoOrigin(t *testing.T) {
	a, _, _ := fixture(t)
	for _, row := range []struct{ token, origin string }{{"", ""}, {a.Token, "http://127.0.0.1"}} {
		req := httptest.NewRequest("POST", "http://127.0.0.1/api/agent-tools/call", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+row.token)
		req.Header.Set("Origin", row.origin)
		res := httptest.NewRecorder()
		a.Handler(http.NotFoundHandler()).ServeHTTP(res, req)
		if res.Code != 403 {
			t.Fatal(res.Code, res.Body.String())
		}
	}
}

func writeAgentClaudeTranscript(t *testing.T, a *App, c agentCaller) {
	t.Helper()
	path := filepath.Join(a.Config.ClaudeHome, "projects", "fixture", c.Caller.SourceID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, row := range []map[string]any{
		{"type": "user", "uuid": "u1", "cwd": a.Config.Repo, "message": map[string]any{"content": "Pair this session"}},
		{"type": "assistant", "uuid": "a1", "parentUuid": "u1", "cwd": a.Config.Repo, "message": map[string]any{"content": []map[string]any{{"type": "tool_use", "id": c.Caller.ToolUseID, "name": "confirm_pairing", "input": map[string]any{}}}, "stop_reason": "tool_use"}},
	} {
		if err := enc.Encode(row); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMissingIdentityIsVisibleWithoutPairing(t *testing.T) {
	a, _, _ := fixture(t)
	created, _ := a.createAgentPairing("ChatGPT")
	p, err := a.pairAgent(context.Background(), agentPairInput{Code: created["code"].(string)}, "")
	if err != nil || p.State != "unsupported" || p.SessionID != "" {
		t.Fatal(p, err)
	}
}

func TestAgentRequestPagesReferencesAndDoesNotReplayAfterRestart(t *testing.T) {
	a, f, s, p, c := agentFixture(t)
	ctx := context.Background()
	in := agentInput(p, s)
	note, err := s.annotate(Annotation{Text: "second reference"}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	in.References = append(in.References, LibraryReference{Kind: "annotation", SpaceID: s.record.ID, AnnotationID: note.ID})
	r, err := a.sendAgentRequest(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	first, err := a.readAgentRequest(ctx, c, r.ID, s.record.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	page := first.(map[string]any)
	if page["nextOffset"] != 1 || len(page["request"].(AgentRequest).References) != 1 {
		t.Fatal(page)
	}
	second, err := a.readAgentRequest(ctx, c, r.ID, s.record.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	page = second.(map[string]any)
	if page["nextOffset"] != nil || page["request"].(AgentRequest).References[0].AnnotationID != note.ID {
		t.Fatal(page)
	}
	if _, err = a.readAgentRequest(ctx, c, r.ID, s.record.ID, 2); err == nil {
		t.Fatal("accepted out-of-range offset")
	}
	a.agentMu.Lock()
	next := libraryClone(a.agents)
	v := next.Requests[r.ID]
	v.State = "submitting"
	next.Requests[r.ID] = v
	err = a.saveAgentsLocked(next)
	a.agentMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err = a.loadAgents(); err != nil {
		t.Fatal(err)
	}
	recovered, err := a.sendAgentRequest(ctx, in)
	if err != nil || recovered.State != "unknown" {
		t.Fatal(recovered, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, method := range f.calls {
		if method == "turn/start" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("replayed after restart", count)
	}
}

func TestPairingFullChannelQueueDoesNotBlockCore(t *testing.T) {
	a, _, _ := fixture(t)
	a.Config.ClaudeHome = t.TempDir()
	c := agentCaller{Caller: mcp.CallerSource{Provider: "claude", SourceID: uuid.NewString(), ToolUseID: "pair-call"}, ReceiverID: uuid.NewString()}
	writeAgentClaudeTranscript(t, a, c)
	a.agentMu.Lock()
	receiver := a.receiverLocked(c.ReceiverID)
	for range cap(receiver.events) {
		receiver.events <- agentEvent{Kind: "fixture"}
	}
	a.agentMu.Unlock()
	created, err := a.createAgentPairing("Busy receiver")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := a.pairAgent(context.Background(), agentPairInput{agentCaller: c, Code: created["code"].(string)}, "")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("accepted full channel queue")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked Core on channel send")
	}
	if a.agentPairings()[0].State != "waiting" {
		t.Fatal("persisted an undeliverable probe")
	}
}
