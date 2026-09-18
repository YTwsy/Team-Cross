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

func joinFixture(t *testing.T, s *Session) *Joined {
	t.Helper()
	b, err := Open(Config{DataDir: t.TempDir(), Binary: "/missing-codex"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	j, err := b.Join(context.Background(), s.share.Token())
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func TestMCPInputRelayUsesCoreOwnershipAndEpoch(t *testing.T) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	ctx := context.Background()
	if err := s.Share(ctx, "lan"); err != nil {
		t.Fatal(err)
	}
	aHTTP := httptest.NewServer(a.Handler(http.NotFoundHandler()))
	defer aHTTP.Close()
	owner := mcp.Backend{URL: aHTTP.URL, Client: aHTTP.Client()}
	args := func() map[string]any { return map[string]any{"id": s.record.ID, "epoch": s.view()["epoch"]} }
	if _, err := owner.Invoke(ctx, "handoff_input", args()); err == nil {
		t.Fatal("handoff without member")
	}
	j := joinFixture(t, s)
	bHTTP := httptest.NewServer(j.app.Handler(http.NotFoundHandler()))
	defer bHTTP.Close()
	guest := mcp.Backend{URL: bHTTP.URL, Client: bHTTP.Client()}
	remote := func() map[string]any { return map[string]any{"id": j.ID, "epoch": s.view()["epoch"]} }
	if _, err := guest.Invoke(ctx, "handoff_input", remote()); err == nil {
		t.Fatal("guest acted as owner")
	}
	if _, err := owner.Invoke(ctx, "request_input", args()); err == nil {
		t.Fatal("owner acted as guest")
	}
	if _, err := guest.Invoke(ctx, "request_input", remote()); err != nil {
		t.Fatal(err)
	}
	if s.view()["inputRequested"] != true || s.view()["writer"] != "owner" {
		t.Fatal("request changed writer")
	}
	if _, err := guest.Invoke(ctx, "cancel_input_request", remote()); err != nil {
		t.Fatal(err)
	}
	if s.view()["inputRequested"] != false {
		t.Fatal("request not cancelled")
	}
	s.mu.Lock()
	s.busy = true
	s.mu.Unlock()
	if _, err := owner.Invoke(ctx, "handoff_input", args()); err == nil {
		t.Fatal("busy handoff accepted")
	}
	s.mu.Lock()
	s.busy = false
	s.mu.Unlock()
	stale := args()
	if out, err := owner.Invoke(ctx, "handoff_input", args()); err != nil || strings.Contains(string(out), "tcx3.") {
		t.Fatal(string(out), err)
	}
	if _, err := owner.Invoke(ctx, "reclaim_input", stale); err == nil {
		t.Fatal("stale reclaim accepted")
	}
	if _, err := guest.Invoke(ctx, "request_input", remote()); err == nil {
		t.Fatal("current writer requested input")
	}
	if _, err := owner.Invoke(ctx, "send_input", map[string]any{"id": s.record.ID, "mode": "start", "text": "blocked", "requestId": "blocked"}); err == nil {
		t.Fatal("old writer sent input")
	}
	s.mu.Lock()
	s.busy = true
	s.mu.Unlock()
	if _, err := guest.Invoke(ctx, "return_input", remote()); err == nil {
		t.Fatal("busy return accepted")
	}
	if _, err := owner.Invoke(ctx, "reclaim_input", args()); err != nil {
		t.Fatal(err)
	}
	if s.view()["busy"] != true {
		t.Fatal("reclaim interrupted model")
	}
	s.mu.Lock()
	s.busy = false
	s.mu.Unlock()
	if _, err := owner.Invoke(ctx, "handoff_input", args()); err != nil {
		t.Fatal(err)
	}
	if _, err := guest.Invoke(ctx, "return_input", remote()); err != nil {
		t.Fatal(err)
	}
	if s.view()["writer"] != "owner" {
		t.Fatal("return did not restore owner")
	}
	f.mu.Lock()
	calls := append([]string(nil), f.calls...)
	f.mu.Unlock()
	for _, method := range calls {
		if strings.HasPrefix(method, "turn/") {
			t.Fatal("input management invoked model", method)
		}
	}
	if err := s.Action(ctx, "end"); err != nil {
		t.Fatal(err)
	}
	if _, err := guest.Invoke(ctx, "request_input", remote()); err == nil {
		t.Fatal("ended share accepted input request")
	}
}

func TestInputToolsRejectMissingOrInvalidEpoch(t *testing.T) {
	for _, value := range []any{nil, 0, -1, 1.5, "1", float64(9007199254740992)} {
		b := mcp.Backend{}
		if _, err := b.Invoke(context.Background(), "handoff_input", map[string]any{"id": "fixture", "epoch": value}); err == nil {
			t.Fatal(value)
		}
	}
	// Ordinary state reads continue to omit invitation credentials.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": "fixture", "invitation": "private", "epoch": 1})
	}))
	defer server.Close()
	b := mcp.Backend{URL: server.URL, Client: server.Client()}
	result, err := b.Invoke(context.Background(), "get_collaboration", map[string]any{"id": "fixture"})
	if err != nil || strings.Contains(string(result), "private") {
		t.Fatal(string(result), err)
	}
}

func firstRemote(s *Session) string {
	if s.share != nil {
		for _, m := range s.share.Members() {
			if m.Active {
				return m.ID
			}
		}
	}
	return "unjoined"
}
