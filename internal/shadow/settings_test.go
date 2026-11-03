package shadow

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLaunchSettingsPersistAcrossEveryThreadAndResume(t *testing.T) {
	e, f, cfg := setup(t)
	settings := e.View().Settings
	e.runtime = f
	ids := map[string]bool{}
	for _, kind := range []string{"main", "job", "steward", "memory", "segment"} {
		if kind == "job" {
			e.record.Jobs["job"] = Job{ID: "job", State: "queued", Version: 1}
		}
		if kind == "segment" {
			e.record.Transition = &Transition{From: e.record.Current}
		}
		id, err := e.createThread(context.Background(), kind, "job")
		if err != nil {
			t.Fatal(kind, err)
		}
		ids[id] = true
		if err = e.submit(context.Background(), Run{Kind: kind, ThreadID: id, JobID: "job", JobVersion: 1}, "test settings", nil); err != nil {
			t.Fatal(err)
		}
	}
	// Simulate changing the native defaults outside Team Cross, then reload only
	// the persisted shadow. Resume must override defaults on all active threads.
	if err := os.WriteFile(filepath.Join(e.record.Home, "config.toml"), []byte(`model = "changed-default"
model_reasoning_effort = "low"
`), 0600); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if restored.View().Settings != settings {
		t.Fatal("launch settings lost", restored.View())
	}
	restored.runtime = f
	if err = restored.restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	resumed := map[string]bool{}
	for _, call := range f.calls {
		switch call.Method {
		case "thread/start", "thread/resume":
			if call.Params["model"] != settings.Model || call.Params["modelProvider"] != settings.ModelProvider || call.Params["config"].(map[string]any)["model_reasoning_effort"] != settings.ReasoningEffort {
				t.Fatal(call)
			}
			if call.Method == "thread/resume" {
				resumed[call.Params["threadId"].(string)] = true
			}
		case "turn/start":
			if call.Params["model"] != settings.Model || call.Params["effort"] != settings.ReasoningEffort {
				t.Fatal(call)
			}
		}
	}
	if len(resumed) != len(ids) {
		t.Fatalf("resumed %d of %d threads", len(resumed), len(ids))
	}
	if err = restored.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if err = restored.SetEnabled(true); err != nil || restored.View().Settings != settings {
		t.Fatal("pause/resume changed settings", err)
	}
}

func TestNativeSettingsMismatchPreservesThreadAndPreventsInput(t *testing.T) {
	e, f, cfg := setup(t)
	f.wrongSettings = true
	e.Start()
	send(t, e, "B", "must not run under another model")
	until(t, func() bool { return !e.View().Enabled })
	view := e.View()
	if view.Current == "" || view.Model != "unexpected-default" || view.Settings.Model == view.Model {
		t.Fatal("lost real confirmation or created thread", view)
	}
	if err := e.SetEnabled(true); err == nil {
		t.Fatal("allowed input after rejected settings")
	}
	e.Close()
	restored, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if restored.View().Current != view.Current || restored.View().Enabled {
		t.Fatal("lost mismatch checkpoint")
	}
	for _, call := range f.calls {
		if call.Method == "turn/start" {
			t.Fatal("submitted under wrong settings")
		}
	}
}

func TestResumeSettingsMismatchClosesPartiallyRestoredRuntime(t *testing.T) {
	e, f, cfg := setup(t)
	e.runtime = f
	if _, err := e.createThread(context.Background(), "main", ""); err != nil {
		t.Fatal(err)
	}
	f.wrongSettings = true
	restored, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	restored.Start()
	until(t, func() bool { return !restored.View().Enabled })
	if f.Alive() {
		t.Fatal("left partially restored runtime eligible for input")
	}
}
