package collab

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPersonalDesktopOpensPersistedForkWithoutRuntimeChanges(t *testing.T) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	ctx := context.Background()
	app := filepath.Join(t.TempDir(), "Personal Codex's App.app")
	if err := os.Mkdir(app, 0700); err != nil {
		t.Fatal(err)
	}
	a.settings.DesktopApp = app
	bin := t.TempDir()
	capture := filepath.Join(bin, "arguments")
	if err := os.WriteFile(filepath.Join(bin, "open"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$TEAMCROSS_OPEN_CAPTURE\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("TEAMCROSS_OPEN_CAPTURE", capture)
	if err := s.Action(ctx, "share"); err != nil {
		t.Fatal(err)
	}
	if err := s.Action(ctx, "handoff"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	before := append([]string(nil), f.calls...)
	f.mu.Unlock()
	epoch := s.view()["epoch"]
	wantURL := "codex://threads/" + s.record.SessionID
	for _, launch := range []bool{false, true, true} {
		result, err := a.PersonalDesktopPlan(ctx, s.record.ID, launch)
		if err != nil {
			t.Fatal(err)
		}
		if result["url"] != wantURL || result["sessionId"] == s.record.SourceID || result["launched"] != launch {
			t.Fatal(result)
		}
		if !launch {
			if _, err := os.Stat(capture); !os.IsNotExist(err) {
				t.Fatal("plan launched Desktop")
			}
			continue
		}
		args, err := os.ReadFile(capture)
		if err != nil || string(args) != "-a\n"+app+"\n"+wantURL+"\n" {
			t.Fatalf("unexpected open arguments: %q, %v", args, err)
		}
	}
	view := s.view()
	if view["writer"] != "remote" || view["epoch"] != epoch || view["connected"] != false {
		t.Fatal("opening changed input or direct connection", view)
	}
	if err := s.Action(ctx, "end"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return s.view()["runtimeState"] == "released" })
	if _, err := a.PersonalDesktopPlan(ctx, s.record.ID, true); err != nil {
		t.Fatal(err)
	}
	if s.view()["runtimeState"] != "released" || f.Alive() {
		t.Fatal("opening restored released runtime")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !reflect.DeepEqual(before, f.calls) || f.forks != 1 {
		t.Fatal("opening invoked runtime or forked again", f.calls)
	}
}

func TestPersonalDesktopRejectsUnavailableTargetsAndLaunchFailure(t *testing.T) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	ctx := context.Background()
	app := t.TempDir()
	a.settings.DesktopApp = app
	if _, err := a.PersonalDesktopPlan(ctx, "missing", false); err == nil {
		t.Fatal("accepted unknown collaboration")
	}
	b, _, _ := fixture(t)
	if err := s.Action(ctx, "share"); err != nil {
		t.Fatal(err)
	}
	j, err := b.Join(ctx, s.share.Token())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.PersonalDesktopPlan(ctx, j.ID, false); err == nil {
		t.Fatal("accepted remote collaboration")
	}
	s.mu.Lock()
	s.record.Provider = "claude"
	s.mu.Unlock()
	if _, err := a.PersonalDesktopPlan(ctx, s.record.ID, false); err == nil {
		t.Fatal("accepted Claude session")
	}
	s.mu.Lock()
	s.record.Provider = "codex"
	s.mu.Unlock()
	id := s.record.SessionID
	for _, invalid := range []string{"", s.record.SourceID} {
		s.mu.Lock()
		s.record.SessionID = invalid
		s.mu.Unlock()
		if _, err := a.PersonalDesktopPlan(ctx, s.record.ID, false); err == nil {
			t.Fatal("accepted missing fork")
		}
	}
	s.mu.Lock()
	s.record.SessionID = id
	s.mu.Unlock()
	a.settings.DesktopApp = filepath.Join(app, "missing.app")
	if _, err := a.PersonalDesktopPlan(ctx, s.record.ID, false); err == nil {
		t.Fatal("accepted missing Desktop")
	}
	a.settings.DesktopApp = app
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "open"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	if _, err := a.PersonalDesktopPlan(ctx, s.record.ID, true); err == nil {
		t.Fatal("reported failed launch as successful")
	}
}

func TestPersonalDesktopHTTPUsesStoredSessionAndSameOrigin(t *testing.T) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	a.settings.DesktopApp = t.TempDir()
	handler := a.Handler(http.NotFoundHandler())
	for _, origin := range []string{"http://127.0.0.1:43210", "https://example.invalid"} {
		req := httptest.NewRequest("POST", "http://127.0.0.1:43210/api/collaborations/"+s.record.ID+"/personal-desktop", bytes.NewBufferString(`{"launch":false,"sessionId":"untrusted","url":"https://example.invalid"}`))
		req.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if strings.HasPrefix(origin, "https:") {
			if w.Code != http.StatusForbidden {
				t.Fatal("accepted cross-origin open", w.Code)
			}
			continue
		}
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 || result["url"] != "codex://threads/"+s.record.SessionID {
			t.Fatal(w.Code, w.Body.String(), err)
		}
	}
}
