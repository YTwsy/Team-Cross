package collab

import (
	"context"
	"encoding/json"
	"teamcross/internal/nativecodex"
	"testing"
)

func TestModelSettingsFollowNativeChoices(t *testing.T) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	ctx := context.Background()
	assertSettings := func(model, effort string) {
		t.Helper()
		view := s.view()
		got, _ := view["reasoningEffort"].(*string)
		if view["model"] != model || got == nil || *got != effort {
			t.Fatalf("unexpected native settings: %s %v", view["model"], got)
		}
	}
	assertSettings("fixture-source-model", "high")
	_, err := s.RPC(ctx, "owner", "thread/settings/update", map[string]any{"model": "fixture-selected-model", "effort": "medium", "cwd": "/wrong"}, "select-model")
	if err != nil {
		t.Fatal(err)
	}
	assertSettings("fixture-selected-model", "medium")
	if f.requests["thread/settings/update"]["cwd"] != s.record.ExecutionCwd || f.requests["thread/settings/update"]["permissions"] != nativecodex.Profile {
		t.Fatal("execution binding changed")
	}
	_, err = s.RPC(ctx, "owner", "turn/start", map[string]any{"input": []any{}}, "keep-settings")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := f.requests["turn/start"]["model"]; exists {
		t.Fatal("model forced on omitted input")
	}
	if _, exists := f.requests["turn/start"]["effort"]; exists {
		t.Fatal("effort forced on omitted input")
	}
	assertSettings("fixture-selected-model", "medium")
	s.onMessage(nativecodex.Message{Method: "turn/completed"})
	_, err = s.RPC(ctx, "owner", "turn/start", map[string]any{"model": "rejected-fixture-model", "effort": "low"}, "reject-model")
	if err == nil {
		t.Fatal("invalid model accepted")
	}
	assertSettings("fixture-selected-model", "medium")
	_, err = s.RPC(ctx, "owner", "thread/resume", map[string]any{"model": "fixture-resumed-model", "config": map[string]any{"model_reasoning_effort": "xhigh", "unrelated_config": "blocked"}}, "resume-model")
	if err != nil {
		t.Fatal(err)
	}
	assertSettings("fixture-resumed-model", "xhigh")
	if f.requests["thread/resume"]["config"].(map[string]any)["unrelated_config"] != nil {
		t.Fatal("unrelated config forwarded")
	}
	f.Close()
	if err = s.Action(ctx, "start"); err != nil {
		t.Fatal(err)
	}
	assertSettings("fixture-resumed-model", "xhigh")
	if err = s.Action(ctx, "share"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RPC(ctx, firstRemote(s), "thread/settings/update", map[string]any{"model": "blocked"}, "blocked-update"); err == nil {
		t.Fatal("nonwriter selected model")
	}
	if _, err = s.RPC(ctx, firstRemote(s), "thread/resume", map[string]any{"model": "blocked"}, "blocked-resume"); err == nil {
		t.Fatal("nonwriter changed model through resume")
	}
	notification, _ := json.Marshal(map[string]any{"threadId": s.record.SessionID, "threadSettings": map[string]any{"model": "fixture-notified-model", "modelProvider": "fixture-provider", "effort": "high"}})
	s.onMessage(nativecodex.Message{Method: "thread/settings/updated", Params: notification})
	assertSettings("fixture-notified-model", "high")
}
