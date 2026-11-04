package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMemberActivityToolsAndUIBoundaries(t *testing.T) {
	for _, schema := range RuntimeTools() {
		if strings.Contains(schema["name"].(string), "member_activity") || schema["name"] == "set_member_follow" {
			t.Fatal("runtime exposes personal member state")
		}
	}
	for _, path := range []string{"activity?limit=1", "collaborations/space/activity?category=mentions&unread=true", "collaborations/space/follows?kind=discussion&subjectId=root&executionId=execution", "collaborations/space/mention-members"} {
		if method, err := uiRoute(path, nil, false); err != nil || method != "GET" {
			t.Fatal(path, method, err)
		}
	}
	for _, path := range []string{"collaborations/space/follow-preferences", "collaborations/space/activity/read"} {
		if _, err := uiRoute(path, nil, false); err == nil {
			t.Fatal("write allowed through UI read", path)
		}
		if method, err := uiRoute(path, map[string]any{}, true); err != nil || method != "POST" {
			t.Fatal(path, method, err)
		}
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/api/collaborations/space/activity" || r.URL.Query().Get("category") != "mentions" {
			t.Error(r.Method, r.URL)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"unread": 2, "snapshot": "same-read-state"})
	}))
	defer server.Close()
	b := Backend{URL: server.URL, Token: "local", Client: localClient()}
	handled, out, err := b.invokeMemberActivity(context.Background(), "list_member_activity", map[string]any{"id": "space", "category": "mentions"})
	if !handled || err != nil || calls != 1 || !strings.Contains(string(out), "same-read-state") {
		t.Fatal(handled, string(out), err, calls)
	}
	_, _, err = b.invokeMemberActivity(context.Background(), "list_member_activity", map[string]any{"id": "space", "memberId": "other"})
	if err == nil || calls != 1 {
		t.Fatal("forged recipient reached Core")
	}
}
