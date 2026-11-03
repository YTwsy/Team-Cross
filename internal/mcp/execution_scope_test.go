package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRuntimeToolsRequireExplicitSpaceAndSession(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/collaborations/space/sessions/session/rpc" {
			t.Error(r.URL.Path)
		}
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	backend := Backend{URL: server.URL, Client: server.Client()}
	args := map[string]any{"id": "space", "mode": "start", "text": "scoped input", "requestId": "request"}
	if _, err := backend.Invoke(context.Background(), "send_input", args); err == nil || calls != 0 {
		t.Fatal("implicit session allowed", err, calls)
	}
	args["executionId"] = "session"
	if _, err := backend.Invoke(context.Background(), "send_input", args); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
	for _, name := range []string{"read_context", "pause_execution", "end_execution_sharing", "resume_collaboration", "open_client"} {
		if _, err := scopedToolPath(name, map[string]any{"id": "space", "kind": "history"}); err == nil {
			t.Fatal(name, "accepted missing session")
		}
	}
	if path, err := scopedToolPath("read_context", map[string]any{"id": "space", "kind": "annotations"}); err != nil || path != "collaborations/space" {
		t.Fatal(path, err)
	}
}
