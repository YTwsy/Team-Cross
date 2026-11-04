package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestUIPassiveConnectionUsesTransportIdentityWithoutModelTurn(t *testing.T) {
	id := uuid.NewString()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/api/agent-current-inflow" || r.Header.Get("Authorization") != "Bearer local" {
			t.Error(r.Method, r.URL.Path)
		}
		var in struct {
			Caller  CallerSource `json:"caller"`
			SpaceID string       `json:"spaceId"`
			Name    string       `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		if in.SpaceID != "space" || in.Caller.SourceID != id || in.Caller.TurnID != "" || in.Name != "my session" {
			t.Error(in)
		}
		w.Write([]byte(`{"linked":true,"inFlow":true}`))
	}))
	defer server.Close()
	b := Backend{URL: server.URL, Token: "local", Client: localClient()}
	ctx := callContext(context.Background(), "codex", map[string]json.RawMessage{"threadId": mustJSON(id)})
	args := map[string]any{"spaceId": "space", "name": "my session"}
	if _, err := b.uiRequest(ctx, "plugin/current-inflow", args, true); err != nil {
		t.Fatal(err)
	}
	if _, err := callerSource(ctx); err == nil {
		t.Fatal("weakened native model identity proof")
	}
	for _, bad := range []map[string]any{
		{"spaceId": "space", "sourceId": id}, {"spaceId": "space", "provider": "claude"},
		{"spaceId": "space", "caller": map[string]string{"sourceId": id}}, {"spaceId": ""}, {"spaceId": 42},
	} {
		if _, err := b.uiRequest(ctx, "plugin/current-inflow", bad, true); err == nil {
			t.Fatal("accepted forged fields", bad)
		}
	}
	if _, err := b.uiRequest(context.Background(), "plugin/current-inflow", args, true); err == nil {
		t.Fatal("accepted missing identity")
	}
	if _, err := b.uiRequest(ctx, "plugin/current-inflow", args, false); err == nil {
		t.Fatal("write accepted as read")
	}
	if _, err := b.uiRequest(ctx, "plugin/current-inflow?sourceId="+id, args, true); err == nil {
		t.Fatal("accepted query identity")
	}
	if _, err := b.uiRequest(ctx, "agent-current-inflow", args, true); err == nil {
		t.Fatal("privileged route exposed")
	}
	if calls != 1 {
		t.Fatal("invalid call reached Core", calls)
	}
	if _, err := uiRoute("collaborations/space/workbench/inflow-connect", map[string]any{"provider": "codex", "sourceId": id}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := uiRoute("collaborations/space/workbench/inflow-connect", nil, false); err == nil {
		t.Fatal("source connection accepted as read")
	}
}
