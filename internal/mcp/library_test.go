package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReadSelectionUsesExplicitCodeAndBoundedPage(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/library/read-selection" || r.Method != "POST" {
			t.Error(r.Method, r.URL.Path)
		}
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["code"] != "TC-EXPLICIT" || in["offset"] != float64(4) {
			t.Error(in)
		}
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()
	b := Backend{URL: server.URL, Client: server.Client()}
	if _, err := b.Invoke(context.Background(), "read_selection", map[string]any{"code": "TC-EXPLICIT", "offset": float64(4)}); err != nil {
		t.Fatal(err)
	}
	for _, args := range []map[string]any{{}, {"code": "TC-EXPLICIT", "id": "another-space"}, {"code": "TC-EXPLICIT", "offset": -1}, {"code": "TC-EXPLICIT", "offset": 1.5}} {
		if _, err := b.Invoke(context.Background(), "read_selection", args); err == nil {
			t.Fatal("invalid selection call reached backend", args)
		}
	}
	if calls != 1 {
		t.Fatal(calls)
	}
}
