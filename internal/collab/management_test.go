package collab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"teamcross/internal/mcp"
)

func managementBackend(t *testing.T, a *App) mcp.Backend {
	t.Helper()
	s := httptest.NewServer(a.Handler(http.NotFoundHandler()))
	t.Cleanup(s.Close)
	return mcp.Backend{URL: s.URL, Token: a.Token, Client: s.Client()}
}

func invokeObject(t *testing.T, b mcp.Backend, name string, args map[string]any) map[string]any {
	t.Helper()
	raw, err := b.Invoke(context.Background(), name, args)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var out map[string]any
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMCPManagementCreatesInvitesJoinsAndResumesSameFork(t *testing.T) {
	for _, mode := range []string{"existing", "worktree"} {
		t.Run(mode, func(t *testing.T) {
			a, f, _ := fixture(t)
			b, _, _ := fixture(t)
			owner, guest := managementBackend(t, a), managementBackend(t, b)
			ctx := context.Background()
			listed := invokeObject(t, owner, "list_source_sessions", map[string]any{"provider": "codex", "search": "fixture"})
			if len(listed["data"].([]any)) != 1 {
				t.Fatal(listed)
			}
			params := map[string]any{"provider": "codex", "sourceId": f.source.ID, "workspaceMode": mode, "runtimeMode": "restricted"}
			p := invokeObject(t, owner, "preview_collaboration", params)
			if p["requestId"] == nil || p["previewHash"] == nil || f.forks != 0 {
				t.Fatal(p)
			}
			params["requestId"], params["previewHash"] = p["requestId"], p["previewHash"]
			c := invokeObject(t, owner, "create_collaboration", params)
			id := c["id"].(string)
			if c["sessionId"] == f.source.ID || c["state"] != "ready" {
				t.Fatal(c)
			}
			again := invokeObject(t, owner, "create_collaboration", params)
			if again["sessionId"] != c["sessionId"] || f.forks != 1 {
				t.Fatal("duplicate fork")
			}
			params["runtimeMode"] = "trusted"
			if _, err := owner.Invoke(ctx, "create_collaboration", params); err == nil {
				t.Fatal("retry changed mode")
			}
			shared := invokeObject(t, owner, "create_invitation", map[string]any{"id": id, "transport": "lan"})
			invitation := shared["invitation"].(string)
			if !strings.HasPrefix(shared["invitationUrl"].(string), "teamcross://join?invite=") {
				t.Fatal("missing App link")
			}
			read := invokeObject(t, owner, "get_collaboration", map[string]any{"id": id})
			if read["invitation"] != nil {
				t.Fatal("query exposed invitation")
			}
			preview := invokeObject(t, guest, "preview_invitation", map[string]any{"invitation": invitation})
			if preview["runtimeMode"] != "restricted" || len(b.joined) != 0 {
				t.Fatal("preview joined")
			}
			joined := invokeObject(t, guest, "join_collaboration", map[string]any{"invitation": invitation})
			guestID := joined["id"].(string)
			duplicate := invokeObject(t, guest, "join_collaboration", map[string]any{"invitation": invitation})
			if duplicate["id"] != guestID || len(b.joined) != 1 {
				t.Fatal("duplicate membership")
			}
			for _, tool := range []string{"create_invitation", "end_sharing", "resume_collaboration"} {
				args := map[string]any{"id": guestID}
				if tool == "create_invitation" {
					args["transport"] = "lan"
				}
				if _, err := guest.Invoke(ctx, tool, args); err == nil {
					t.Fatal("guest performed owner operation", tool)
				}
			}
			if _, err := guest.Invoke(ctx, "open_client", map[string]any{"id": guestID, "client": "tui", "launch": false}); err == nil {
				t.Fatal("non-writer opened direct client")
			}
			invokeObject(t, owner, "handoff_input", map[string]any{"id": id, "epoch": c["epoch"]})
			plan := invokeObject(t, guest, "open_client", map[string]any{"id": guestID, "client": "tui", "launch": false})
			if plan["sessionId"] != c["sessionId"] || plan["launched"] != false {
				t.Fatal(plan)
			}
			used := invokeObject(t, owner, "create_invitation", map[string]any{"id": id, "transport": "lan"})
			if used["invitation"] != invitation || used["invitationUrl"] == nil || used["invitationState"] != "active" {
				t.Fatal("reusable invitation disappeared after joining")
			}
			invokeObject(t, guest, "leave_collaboration", map[string]any{"id": guestID})
			invokeObject(t, owner, "end_sharing", map[string]any{"id": id})
			resumed := invokeObject(t, owner, "resume_collaboration", map[string]any{"id": id})
			if resumed["sessionId"] != c["sessionId"] || resumed["sharing"] != false || f.forks != 1 {
				t.Fatal("resume changed fork or sharing")
			}
			f.mu.Lock()
			calls := append([]string(nil), f.calls...)
			f.mu.Unlock()
			for _, method := range calls {
				if strings.HasPrefix(method, "turn/") {
					t.Fatal("management sent prompt", method)
				}
			}
		})
	}
}
