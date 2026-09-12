package nativeclaude

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPrepareKeepsRoutingOnAAndRejectsChangedSnapshot(t *testing.T) {
	sourceHome := t.TempDir()
	path := historyFixture(t, sourceHome, uuid.NewString())
	source, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	settings := `{"env":{"ANTHROPIC_AUTH_TOKEN":"SECRET_ON_A","ANTHROPIC_MODEL":"fixture-model","HTTPS_PROXY":"http://local-fixture","CLAUDE_CONFIG_DIR":"/personal","BASH_ENV":"/untrusted"},"hooks":{"danger":true},"enabledPlugins":{"private":true}}`
	if err = os.WriteFile(filepath.Join(sourceHome, "settings.json"), []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}
	c := Config{SourceHome: sourceHome, Home: filepath.Join(t.TempDir(), "owned"), Cwd: "/fork"}
	if err = prepare(c, source); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(c.Home, "settings.json"))
	text := string(b)
	if !strings.Contains(text, "SECRET_ON_A") || !strings.Contains(text, "fixture-model") {
		t.Fatal("routing was lost")
	}
	for _, s := range []string{"/personal", "BASH_ENV", "danger", "private"} {
		if strings.Contains(text, s) {
			t.Fatal("inherited unrelated configuration", s)
		}
	}
	original, _ := os.ReadFile(filepath.Join(sourceHome, "settings.json"))
	if string(original) != settings {
		t.Fatal("modified user settings")
	}
	appendRecord(t, path, `{"type":"last-prompt"}`)
	c.Home = filepath.Join(t.TempDir(), "new-owned")
	if err = prepare(c, source); err == nil {
		t.Fatal("accepted stale source snapshot")
	}
}
func TestNativeTransportScopeCapsAndBufferedOutput(t *testing.T) {
	socket := shortSocket(t)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	request := make(chan map[string]any, 1)
	go func() {
		c, e := ln.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		var v map[string]any
		b, _ := bufio.NewReader(c).ReadBytes('\n')
		_ = json.Unmarshal(b, &v)
		request <- v
		_, _ = c.Write([]byte("{\"ok\":true,\"imarkNonce\":\"nonce\"}\nFIRST_PAINT"))
	}()
	p := Process{Job: Job{ID: "12345678"}, Socket: socket}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, ack, err := p.Attach(ctx, TerminalRequest{Op: "attach", Cols: 80, Rows: 24, AttachID: "b", Caps: json.RawMessage(`{"colorLevel":3,"editor":"run-on-A","browser":"open-secret","tmuxSocket":"/private/sock","imark":false}`)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	b, err := io.ReadAll(c)
	if err != nil || string(b) != "FIRST_PAINT" || !strings.Contains(string(ack), "nonce") {
		t.Fatal(string(b), string(ack), err)
	}
	v := <-request
	if v["short"] != "12345678" || v["proto"] != float64(1) || v["auth"] != nil {
		t.Fatal("unscoped/authenticated with remote secret", v)
	}
	caps := v["caps"].(map[string]any)
	if caps["editor"] != nil || caps["browser"] != nil || caps["tmuxSocket"] != nil || caps["imark"] != true || caps["colorLevel"] != float64(3) {
		t.Fatal("unsafe caps", caps)
	}
}
func TestNativeHandshakeHonorsCancellation(t *testing.T) {
	socket := shortSocket(t)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, e := ln.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		close(accepted)
		_, _ = io.Copy(io.Discard, c)
	}()
	p := Process{Job: Job{ID: "12345678"}, Socket: socket}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { _, _, e := p.connect(ctx, map[string]any{"op": "attach"}); result <- e }()
	<-accepted
	cancel()
	select {
	case e := <-result:
		if e == nil {
			t.Fatal("accepted unacknowledged input")
		}
	case <-time.After(time.Second):
		t.Fatal("handshake ignored cancellation")
	}
	<-done
}
func TestAckRejectionIsDefinitive(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	go func() { defer b.Close(); _, _ = b.Write([]byte("{\"ok\":false,\"error\":\"busy\"}\n")) }()
	_, _, err := readAck(a)
	if !errors.Is(err, ErrRejected) {
		t.Fatal(err)
	}
}

func shortSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "tcx-unix-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "worker.sock")
}

func TestControlWritesUseOnlyOwnedDaemonKey(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "daemon"), 0700); err != nil {
		t.Fatal(err)
	}
	key := "0123456789abcdef0123456789abcdef"
	keyPath := filepath.Join(home, "daemon", "control.key")
	if err := os.WriteFile(keyPath, []byte(key), 0600); err != nil {
		t.Fatal(err)
	}
	socket := shortSocket(t)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	requests := make(chan map[string]any, 1)
	go func() {
		c, e := ln.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		var v map[string]any
		_ = json.NewDecoder(c).Decode(&v)
		requests <- v
		_, _ = c.Write([]byte("{\"ok\":true}\n"))
	}()
	p := Process{Config: Config{Home: home}, Job: Job{ID: "12345678"}, Socket: socket}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, _, err := p.connect(ctx, map[string]any{"op": "reply", "text": "one input", "auth": "remote-token"})
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if got := <-requests; got["auth"] != key || got["short"] != "12345678" {
		t.Fatal("control write did not use owned credentials")
	}
	_ = os.Chmod(keyPath, 0644)
	if _, err = p.controlKey(); err == nil {
		t.Fatal("accepted exposed control key")
	}
}

func TestLiveIdleStatusOverridesStaleJobProgress(t *testing.T) {
	for _, c := range []struct {
		job  Job
		busy bool
	}{
		{Job{State: "working", Status: "idle"}, false},
		{Job{State: "done", Status: "busy"}, true},
		{Job{State: "blocked", Status: "waiting", WaitingFor: "permission prompt"}, true},
		{Job{State: "working"}, true},
	} {
		if c.job.Busy() != c.busy {
			t.Fatalf("wrong live busy state: %+v", c.job)
		}
	}
}

func TestRestoreDistinguishesSavedDoneJobFromLiveWorker(t *testing.T) {
	for _, live := range []bool{false, true} {
		t.Run(fmt.Sprint(live), func(t *testing.T) {
			home := t.TempDir()
			id := uuid.NewString()
			job := id[:8]
			dir, err := os.MkdirTemp("/tmp", "cc-daemon-fixture-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(dir) })
			socket := filepath.Join(dir, "control.sock")
			meta := marker{Version: 1, SourceID: uuid.NewString(), SessionID: id, Cwd: home}
			if err = writeJSON(filepath.Join(home, "teamcross-runtime.json"), meta); err != nil {
				t.Fatal(err)
			}
			writeJSON(filepath.Join(home, "saved.json"), []Job{{ID: job, SessionID: id, Cwd: home, State: "done"}})
			writeJSON(filepath.Join(home, "running.json"), []Job{{ID: job, SessionID: id, Cwd: home, PID: 12345, Status: "idle", State: "done"}})
			script := `#!/bin/sh
case "$1" in
 --version) printf '%s\n' '2.1.268 (Claude Code)';;
 agents) if test -f "$CLAUDE_CONFIG_DIR/live"; then cat "$CLAUDE_CONFIG_DIR/running.json"; else cat "$CLAUDE_CONFIG_DIR/saved.json"; fi;;
 --resume) test "$#" = 3 && test "$3" = --bg || exit 1
   touch "$CLAUDE_CONFIG_DIR/resume-called" "$CLAUDE_CONFIG_DIR/live"
   printf '%s\n' 'backgrounded · ` + job + `';;
 daemon) printf '%s\n' '` + dir + `';;
 stop) exit 0;;
esac
`
			binary := filepath.Join(home, "claude")
			os.WriteFile(binary, []byte(script), 0700)
			if live {
				os.WriteFile(filepath.Join(home, "live"), nil, 0600)
				ln, e := net.Listen("unix", socket)
				if e != nil {
					t.Fatal(e)
				}
				defer ln.Close()
				go func() {
					c, e := ln.Accept()
					if e != nil {
						return
					}
					defer c.Close()
					var v map[string]any
					_ = json.NewDecoder(c).Decode(&v)
					_, _ = c.Write([]byte("{\"ok\":true,\"op\":\"has\",\"alive\":true}\n"))
				}()
			}
			p, err := Restore(context.Background(), Config{Binary: binary, Home: home, Cwd: home}, id)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			if p.Job.SessionID != id || p.Job.PID != 12345 {
				t.Fatal("wrong restored worker", p.Job)
			}
			_, err = os.Stat(filepath.Join(home, "resume-called"))
			if live && !errors.Is(err, os.ErrNotExist) {
				t.Fatal("live worker was resumed as another copy")
			}
			if !live && err != nil {
				t.Fatal("saved done job was mistaken for a live worker")
			}
			os.Remove(filepath.Join(home, "live"))
			if _, err = p.Status(context.Background()); err == nil {
				t.Fatal("missing process still reported online")
			}
		})
	}
}
