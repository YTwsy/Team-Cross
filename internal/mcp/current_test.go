package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCurrentSourceUsesProviderSpecificTransportIdentity(t *testing.T) {
	id, unrelated := uuid.NewString(), uuid.NewString()
	t.Setenv("CODEX_THREAD_ID", unrelated)
	t.Setenv("CLAUDE_CODE_SESSION_ID", id)
	meta := func(s string) map[string]json.RawMessage {
		var v map[string]json.RawMessage
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, row := range []struct{ name, provider, metadata, wantID, wantTurn, wantTool string }{
		{"codex", "codex", `{"threadId":"` + id + `","x-codex-turn-metadata":{"thread_id":"` + id + `","turn_id":"turn-1"}}`, id, "turn-1", ""},
		{"codex-details", "codex", `{"x-codex-turn-metadata":{"thread_id":"` + id + `","turn_id":"turn-1"}}`, id, "turn-1", ""},
		{"claude", "claude", `{"claudecode/toolUseId":"call-1","threadId":"` + unrelated + `"}`, id, "", "call-1"},
		{"conflicting-codex", "codex", `{"threadId":"` + id + `","x-codex-turn-metadata":{"thread_id":"` + unrelated + `","turn_id":"turn-1"}}`, "", "", ""},
		{"missing-codex-turn", "codex", `{"threadId":"` + id + `"}`, "", "", ""},
		{"no-environment-fallback", "codex", `{}`, "", "", ""},
		{"missing-claude-tool", "claude", `{}`, "", "", ""},
		{"unknown-client", "", `{"threadId":"` + id + `","claudecode/toolUseId":"call-1"}`, "", "", ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			got, err := callerSource(callContext(context.Background(), row.provider, meta(row.metadata)))
			if row.wantID == "" {
				if err == nil {
					t.Fatal("accepted unverifiable caller", got)
				}
				return
			}
			if err != nil || got.SourceID != row.wantID || got.TurnID != row.wantTurn || got.ToolUseID != row.wantTool {
				t.Fatal(got, err)
			}
		})
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	if _, err := callerSource(callContext(context.Background(), "claude", meta(`{"claudecode/toolUseId":"call-1"}`))); err == nil {
		t.Fatal("used unrelated Codex environment for Claude")
	}
}

func TestStdioCarriesCurrentCallerOutsideToolArguments(t *testing.T) {
	id := uuid.NewString()
	input := strings.NewReader(`{"id":1,"method":"initialize","params":{"clientInfo":{"name":"codex-mcp-client"}}}` + "\n" + `{"id":2,"method":"tools/call","params":{"name":"get_current_source","arguments":{},"_meta":{"threadId":"` + id + `","x-codex-turn-metadata":{"thread_id":"` + id + `","turn_id":"current-turn"}}}}` + "\n")
	var output bytes.Buffer
	called := false
	err := serve(context.Background(), input, &output, currentTools(), "", func(ctx context.Context, name string, args map[string]any, provider string) (json.RawMessage, error) {
		called = true
		c, err := callerSource(ctx)
		if err != nil || c.SourceID != id || c.TurnID != "current-turn" || provider != "codex" || len(args) != 0 {
			t.Fatal(c, args, err)
		}
		return json.RawMessage(`{}`), nil
	})
	if err != nil || !called {
		t.Fatal(err, output.String())
	}
	for _, name := range []string{"get_current_source", "preview_current_share", "share_current_session"} {
		_, err := (Backend{}).Invoke(context.Background(), name, map[string]any{"provider": "codex", "sourceId": id})
		if err == nil {
			t.Fatal("tool arguments replaced caller identity", name)
		}
	}
}
