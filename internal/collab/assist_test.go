package collab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"teamcross/internal/mcp"
	"teamcross/internal/nativecodex"
)

func TestClaudeAssistUsesPersonalClientAcrossProviders(t *testing.T) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	a.Config.ClaudeHome = filepath.Join(t.TempDir(), "personal home")
	a.settings.ClaudeBinary = filepath.Join(t.TempDir(), "claude")
	os.WriteFile(a.settings.ClaudeBinary, []byte("#!/bin/sh\n[ \"$1\" = --version ] || exit 1\nprintf '2.2.0 (Claude Code)\\n'\n"), 0700)
	for _, provider := range []string{"codex", "claude"} {
		s.mu.Lock()
		s.record.Provider = provider
		s.mu.Unlock()
		plan, err := a.AssistPlan(context.Background(), s.record.ID, "claude", "tui", false)
		if err != nil {
			t.Fatal(err)
		}
		command := plan["command"].(string)
		if !strings.Contains(command, a.Config.ClaudeHome) || !strings.Contains(command, a.Config.Repo) {
			t.Fatal(command)
		}
		for _, forbidden := range []string{"--resume", "attach", "--model", "--effort", "--mcp-config", "--strict-mcp-config", "--bg", s.record.SessionID} {
			if strings.Contains(command, forbidden) {
				t.Fatalf("personal plan includes %q: %s", forbidden, command)
			}
		}
	}
	if _, err := a.AssistPlan(context.Background(), s.record.ID, "claude", "desktop", false); err == nil {
		t.Fatal("advertised unsupported Desktop")
	}
	if _, err := a.AssistPlan(context.Background(), s.record.ID, "invalid", "tui", false); err == nil {
		t.Fatal("accepted unknown provider")
	}
	if _, err := a.AssistPlan(context.Background(), "not-a-member", "claude", "tui", false); err == nil {
		t.Fatal("missing collaboration was accepted")
	}
	if _, err := a.AssistPlan(context.Background(), s.record.ID, "claude", "tui", true); err == nil || !strings.Contains(err.Error(), "尚未接入") {
		t.Fatal("launch must require installed MCP", err)
	}
}

func TestAuxiliaryMCPKeepsCodexControlsAndOwnership(t *testing.T) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	server := httptest.NewServer(a.Handler(http.NotFoundHandler()))
	defer server.Close()
	b := mcp.Backend{URL: server.URL, Token: a.Token, Client: server.Client()}
	ctx := context.Background()
	id := s.record.ID
	for _, tool := range []string{"list_collaborations", "get_collaboration"} {
		if _, err := b.Invoke(ctx, tool, map[string]any{"id": id}); err != nil {
			t.Fatal(tool, err)
		}
	}
	start := map[string]any{"id": id, "mode": "start", "requestId": "mcp-start", "text": "selected input"}
	if _, err := b.Invoke(ctx, "send_input", start); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Invoke(ctx, "send_input", map[string]any{"id": id, "mode": "steer", "requestId": "mcp-steer", "turnId": "active-turn", "text": "selected followup"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Invoke(ctx, "interrupt_turn", map[string]any{"id": id, "requestId": "mcp-interrupt", "turnId": "active-turn"}); err != nil {
		t.Fatal(err)
	}
	s.onMessage(nativecodex.Message{Method: "turn/completed"})
	s.onMessage(nativecodex.Message{ID: json.RawMessage(`12`), Method: "item/commandExecution/requestApproval", Params: json.RawMessage(`{}`)})
	response := map[string]any{"id": id, "requestId": 12, "result": map[string]any{"decision": "decline"}}
	if _, err := b.Invoke(ctx, "respond_to_request", response); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Invoke(ctx, "respond_to_request", response); err == nil {
		t.Fatal("duplicate approval accepted")
	}
	if err := s.Action(ctx, "share"); err != nil {
		t.Fatal(err)
	}
	joinFixture(t, s)
	if err := s.Action(ctx, "handoff"); err != nil {
		t.Fatal(err)
	}
	start["requestId"] = "stale-owner"
	if _, err := b.Invoke(ctx, "send_input", start); err == nil {
		t.Fatal("auxiliary client bypassed input ownership")
	}
	if _, err := b.Invoke(ctx, "read_context", map[string]any{"id": id, "kind": "annotations"}); err != nil {
		t.Fatal("read blocked by ownership", err)
	}
}

func TestMCPObservedDoesNotMixClientsOrTrustBrowser(t *testing.T) {
	a := &App{Token: "local-token"}
	for _, row := range []struct {
		provider, token string
		accepted        bool
	}{{"claude", "", false}, {"", "local-token", false}, {"unknown", "local-token", false}, {"claude", "local-token", true}} {
		r := httptest.NewRequest("POST", "/api/mcp/observed", strings.NewReader(`{"provider":"`+row.provider+`"}`))
		if row.token != "" {
			r.Header.Set("Authorization", "Bearer "+row.token)
		}
		w := httptest.NewRecorder()
		a.onboarding(w, r, "mcp/observed")
		if (!a.mcpObserved["claude"].IsZero()) != row.accepted {
			t.Fatal(row, w.Body.String())
		}
		if !a.mcpObserved["codex"].IsZero() || a.mcpProbed {
			t.Fatal("client observation altered unrelated evidence")
		}
	}
}
