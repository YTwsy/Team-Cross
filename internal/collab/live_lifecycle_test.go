package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/coder/websocket"
	"os"
	"path/filepath"
	"testing"
	"time"

	"teamcross/internal/nativecodex"
)

// Called only by the opt-in TestLiveCodex fixture. Verifies the real native
// writer lock, beyond fake Runtime.Close expectations, without another prompt.
func verifyLiveRelease(t *testing.T, ctx context.Context, s *Session) {
	t.Helper()
	probe, e := nativecodex.Start(ctx, mustBinary(t, s.app), s.record.ProviderHome, s.record.ExecutionCwd, filepath.Join(s.app.Config.DataDir, "lock-probe-"+s.record.ID+".log"))
	if e != nil {
		t.Fatal(e)
	}
	defer probe.Close()
	params := nativecodex.Overrides(s.record.SessionID, s.record.ExecutionCwd)
	params["excludeTurns"] = true
	if e = probe.Call(ctx, "thread/resume", params, nil); e == nil {
		t.Fatal("fixture did not hold an exclusive native writer lock")
	}
	if e = s.Action(ctx, "share"); e != nil {
		t.Fatal(e)
	}
	guest, e := Open(Config{DataDir: filepath.Join(s.app.Config.DataDir, "guest-"+s.record.ID), Repo: s.record.ExecutionCwd, Loopback: true})
	if e != nil {
		t.Fatal(e)
	}
	defer guest.Close()
	j, e := guest.Join(ctx, s.share.Token())
	if e != nil {
		t.Fatal(e)
	}
	if view := j.view(ctx); view["online"] != true {
		t.Fatal("real membership status unavailable")
	}
	if e = s.Action(ctx, "end"); e != nil {
		t.Fatal(e)
	}
	eventually(t, func() bool { return s.view()["runtimeState"] == "released" })
	if view := j.view(ctx); view["state"] != "ended" {
		t.Fatal("member retained access after end")
	}
	if _, e = s.Context(ctx, "history", "", 0); e != nil {
		t.Fatal("released history unreadable", e)
	}
	if e = probe.Call(ctx, "thread/resume", params, nil); e != nil {
		t.Fatal("native writer lock was not released", e)
	}
	t.Log("independent native app-server resumed released thread", s.record.SessionID)
	probe.Close()
	before := s.record.SessionID
	if e = s.Action(ctx, "start"); e != nil {
		t.Fatal(e)
	}
	if s.record.SessionID != before || s.record.Model != liveModel {
		t.Fatal("restore changed identity/model")
	}
	t.Log("Team Cross restored the same native thread with persisted model", before, s.record.Model)
}
func mustBinary(t *testing.T, a *App) string {
	t.Helper()
	binary, e := a.binary()
	if e != nil {
		t.Fatal(e)
	}
	return binary
}

// Uses only a completed TestLiveCodex manifest, never a user's ordinary home.
// No prompt is sent; both participants resume the existing dedicated fork.
func TestLiveNativeHandoff(t *testing.T) {
	root := os.Getenv("TEAMCROSS_LIVE_EXISTING_FIXTURE")
	if root == "" {
		t.Skip("set TEAMCROSS_LIVE_EXISTING_FIXTURE to a completed dedicated TestLiveCodex fixture")
	}
	var manifest struct {
		Root           string            `json:"root"`
		Model          string            `json:"model"`
		Home           string            `json:"codexHome"`
		Data           string            `json:"dataDir"`
		Repo           string            `json:"repo"`
		Collaborations map[string]string `json:"collaborations"`
	}
	if e := readJSON(filepath.Join(root, "fixture.json"), &manifest); e != nil {
		t.Fatal(e)
	}
	if manifest.Root != root || manifest.Model != liveModel || manifest.Home != filepath.Join(root, "codex-home") || len(manifest.Collaborations) != 2 {
		t.Fatal("not a dedicated live fixture")
	}
	t.Setenv("CODEX_HOME", manifest.Home)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	a, e := Open(Config{DataDir: manifest.Data, Repo: manifest.Repo, Loopback: true})
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	b, e := Open(Config{DataDir: t.TempDir(), Repo: manifest.Repo, Loopback: true})
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	for mode, id := range manifest.Collaborations {
		s := a.sessions[id]
		if s == nil {
			t.Fatal("fixture collaboration missing")
		}
		if e = s.Action(ctx, "start"); e != nil {
			t.Fatal(e)
		}
		if e = s.Action(ctx, "share"); e != nil {
			t.Fatal(e)
		}
		j, e := b.Join(ctx, s.share.Token())
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Action(ctx, "handoff"); e != nil {
			t.Fatal(e)
		}
		endpoint, e := b.endpoint(j.ID)
		if e != nil {
			t.Fatal(e)
		}
		nativeResumeThroughGateway(t, ctx, endpoint, s.record.SessionID)
		if e = s.Action(ctx, "reclaim"); e != nil {
			t.Fatal(e)
		}
		endpoint, e = a.endpoint(id)
		if e != nil {
			t.Fatal(e)
		}
		nativeResumeThroughGateway(t, ctx, endpoint, s.record.SessionID)
		if e = s.Action(ctx, "end"); e != nil {
			t.Fatal(e)
		}
		eventually(t, func() bool { return s.view()["runtimeState"] == "released" })
		t.Log(mode, "remote -> owner native resume and release passed", s.record.SessionID)
	}
}

func nativeResumeThroughGateway(t *testing.T, ctx context.Context, endpoint, id string) {
	t.Helper()
	conn, _, e := websocket.Dial(ctx, endpoint, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.CloseNow()
	call := func(n int, method string, params any) json.RawMessage {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{"id": n, "method": method, "params": params})
		if e := conn.Write(ctx, websocket.MessageText, payload); e != nil {
			t.Fatal(e)
		}
		for {
			_, payload, e := conn.Read(ctx)
			if e != nil {
				t.Fatal(e)
			}
			var m nativecodex.Message
			if e = json.Unmarshal(payload, &m); e != nil {
				t.Fatal(e)
			}
			if string(m.ID) != fmt.Sprint(n) {
				continue
			}
			if len(m.Error) > 0 {
				t.Fatalf("%s: %s", method, m.Error)
			}
			return m.Result
		}
	}
	call(1, "initialize", map[string]any{"clientInfo": map[string]string{"name": "teamcross-native-handoff-test", "version": "1"}, "capabilities": map[string]bool{"experimentalApi": true}})
	result := call(2, "thread/resume", map[string]any{"threadId": id, "excludeTurns": true})
	var out struct {
		Thread struct {
			ID                   string `json:"id"`
			CanAcceptDirectInput bool   `json:"canAcceptDirectInput"`
		} `json:"thread"`
	}
	if e = json.Unmarshal(result, &out); e != nil || out.Thread.ID != id || !out.Thread.CanAcceptDirectInput {
		t.Fatal("gateway could not open writable native thread", string(result), e)
	}
}
