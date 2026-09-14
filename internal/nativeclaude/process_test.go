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

func TestPrepareKeepsRoutingScopedAndPersonalFilesUntouched(t *testing.T) {
	home := t.TempDir()
	path := historyFixture(t, home, uuid.NewString())
	unrelatedID := uuid.NewString()
	unrelatedPath := historyFixture(t, home, unrelatedID)
	source, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	settings := `{"env":{"ANTHROPIC_AUTH_TOKEN":"SECRET_ON_A","ANTHROPIC_MODEL":"fixture-model","HTTPS_PROXY":"http://local-fixture","CLAUDE_CONFIG_DIR":"/personal","BASH_ENV":"/untrusted"},"hooks":{"danger":true},"enabledPlugins":{"private":true}}`
	credentials := `{"oauthAccount":{"emailAddress":"owner@example.invalid"}}`
	state := `{"theme":"dark","mcpServers":{"personal":{"command":"/usr/bin/false"}}}`
	if err = os.WriteFile(filepath.Join(home, "settings.json"), []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(home, ".credentials.json"), []byte(credentials), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(home, ".claude.json"), []byte(state), 0600); err != nil {
		t.Fatal(err)
	}
	sourceBefore, _ := os.ReadFile(path)
	c := Config{Home: home, RuntimeDir: filepath.Join(t.TempDir(), "owned"), Cwd: "/fork"}
	if err = prepare(c, source); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(c.RuntimeDir, "settings.json"))
	text := string(b)
	if !strings.Contains(text, "SECRET_ON_A") || !strings.Contains(text, "fixture-model") {
		t.Fatal("routing was lost")
	}
	for _, s := range []string{"/personal", "BASH_ENV", "danger", "private"} {
		if strings.Contains(text, s) {
			t.Fatal("inherited unrelated configuration", s)
		}
	}
	for file, want := range map[string]string{"settings.json": settings, ".credentials.json": credentials, ".claude.json": state} {
		original, readErr := os.ReadFile(filepath.Join(home, file))
		if readErr != nil || string(original) != want {
			t.Fatalf("modified personal %s: %v", file, readErr)
		}
	}
	if after, readErr := os.ReadFile(path); readErr != nil || string(after) != string(sourceBefore) {
		t.Fatal("modified source history", readErr)
	}
	projects, readErr := os.Lstat(filepath.Join(c.RuntimeDir, "projects"))
	if readErr != nil || !projects.IsDir() || projects.Mode()&os.ModeSymlink != 0 {
		t.Fatal("runtime projects is not an isolated directory", projects, readErr)
	}
	snapshot := filepath.Join(c.RuntimeDir, "projects", filepath.Base(filepath.Dir(path)), filepath.Base(path))
	if copied, readErr := os.ReadFile(snapshot); readErr != nil || string(copied) != string(sourceBefore) {
		t.Fatal("selected source snapshot is invalid", readErr)
	}
	if _, readErr := os.Stat(filepath.Join(c.RuntimeDir, "projects", filepath.Base(filepath.Dir(unrelatedPath)), unrelatedID+".jsonl")); !errors.Is(readErr, os.ErrNotExist) {
		t.Fatal("runtime exposed unrelated personal history", readErr)
	}
	if copied, readErr := os.ReadFile(filepath.Join(c.RuntimeDir, ".credentials.json")); readErr != nil || string(copied) != credentials {
		t.Fatal("runtime authentication snapshot is invalid", readErr)
	}
	overlap := c
	overlap.RuntimeDir = filepath.Join(home, "teamcross-runtime")
	if err = prepare(overlap, source); err == nil {
		t.Fatal("accepted collaboration runtime inside personal history home")
	}
	appendRecord(t, path, `{"type":"last-prompt"}`)
	c.RuntimeDir = filepath.Join(t.TempDir(), "new-owned")
	if err = prepare(c, source); err == nil {
		t.Fatal("accepted stale source snapshot")
	}
	outside := historyFixture(t, t.TempDir(), uuid.NewString())
	other, err := ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	c.RuntimeDir = filepath.Join(t.TempDir(), "outside-source")
	if err = prepare(c, other); err == nil {
		t.Fatal("accepted source outside personal history")
	}
}

func TestPublishHistoryExposesOnlyTheCreatedFork(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "personal")
	runtimeDir := filepath.Join(root, "runtime")
	sourcePath := historyFixture(t, home, uuid.NewString())
	unrelatedID := uuid.NewString()
	unrelatedPath := historyFixture(t, home, unrelatedID)
	source, err := ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	c := Config{Home: home, RuntimeDir: runtimeDir, Cwd: "/fork"}
	if err = prepare(c, source); err != nil {
		t.Fatal(err)
	}
	newID := uuid.NewString()
	runtimePath := historyFixture(t, runtimeDir, newID)
	p := Process{Config: c, Job: Job{ID: newID[:8], SessionID: newID}}
	history, err := p.History()
	if err != nil || history.ID != newID {
		t.Fatal("new fork was not published", history, err)
	}
	personalPath, err := historyPath(home, newID)
	if err != nil {
		t.Fatal(err)
	}
	runtimeInfo, err := os.Stat(runtimePath)
	if err != nil {
		t.Fatal(err)
	}
	personalInfo, err := os.Stat(personalPath)
	if err != nil || !os.SameFile(runtimeInfo, personalInfo) {
		t.Fatal("personal entry does not reference the native runtime transcript", err)
	}
	if personalLink, err := os.Lstat(personalPath); err != nil || !personalLink.Mode().IsRegular() {
		t.Fatal("same-volume personal history should be a regular hard link", personalLink, err)
	}
	if got, err := os.ReadFile(unrelatedPath); err != nil || !strings.Contains(string(got), "source question") {
		t.Fatal("modified unrelated personal history", err)
	}
	if _, err = os.Stat(filepath.Join(runtimeDir, "projects", "fixture", unrelatedID+".jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("published unrelated history into the runtime", err)
	}
}

func TestForkUsesNativePersonalHistoryAndStopsOnlyOwnedJob(t *testing.T) {
	home := t.TempDir()
	path := historyFixture(t, home, uuid.NewString())
	source, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	settings := `{"env":{"ANTHROPIC_AUTH_TOKEN":"SECRET_ON_A"},"hooks":{"personal":true}}`
	if err = os.WriteFile(filepath.Join(home, "settings.json"), []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}
	newID := uuid.NewString()
	jobID := "abcdef12"
	work := t.TempDir()
	runtimeDir := filepath.Join(work, "runtime")
	argsPath := filepath.Join(work, "launch-args")
	stopPath := filepath.Join(work, "stop-args")
	configPath := filepath.Join(work, "launch-config-home")
	daemonStopPath := filepath.Join(work, "daemon-stop-config-home")
	jobsPath := filepath.Join(work, "jobs.json")
	daemonDir, err := os.MkdirTemp("/tmp", "cc-daemon-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(daemonDir) })
	if err = writeJSON(jobsPath, []Job{{ID: jobID, SessionID: newID, Cwd: work, PID: 12345, Status: "idle"}}); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
 --version) printf '%%s\n' '2.1.270 (Claude Code)';;
	 --resume) printf '%%s\n' "$@" > %q; printf '%%s\n' "$CLAUDE_CONFIG_DIR" > %q; printf '%%s\n' 'backgrounded · %s';;
 agents) cat %q;;
	 daemon) if test "$2" = stop; then printf '%%s\n' "$CLAUDE_CONFIG_DIR" > %q; else printf '%%s\n' %q; fi;;
 stop) printf '%%s\n' "$@" > %q;;
 *) exit 1;;
esac
`, argsPath, configPath, jobID, jobsPath, daemonStopPath, daemonDir, stopPath)
	binary := filepath.Join(work, "claude")
	if err = os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	c := Config{Binary: binary, Home: home, RuntimeDir: runtimeDir, Cwd: work}
	p, err := Fork(context.Background(), c, source, "Personal history fork")
	if err != nil {
		t.Fatal(err)
	}
	if p.Job.SessionID != newID || p.Job.ID != jobID {
		t.Fatal("wrong native fork identity", p.Job)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(args), "\n"), "\n")
	want := []string{"--resume", source.ID, "--fork-session", "--bg", "--name", "Personal history fork", "--settings", filepath.Join(runtimeDir, "settings.json")}
	if len(lines) < len(want) {
		t.Fatal("short native launch", lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("launch arg %d = %q, want %q", i, lines[i], want[i])
		}
	}
	if _, err = Read(home, newID); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fork required Team Cross to materialize personal JSONL", err)
	}
	personalSettings, _ := os.ReadFile(filepath.Join(home, "settings.json"))
	if string(personalSettings) != settings {
		t.Fatal("native fork preparation modified personal settings")
	}
	if used, _ := os.ReadFile(configPath); strings.TrimSpace(string(used)) != runtimeDir {
		t.Fatal("native worker used personal config as a write target", string(used))
	}
	if snapshot, snapshotErr := os.ReadFile(filepath.Join(runtimeDir, "projects", filepath.Base(filepath.Dir(path)), filepath.Base(path))); snapshotErr != nil || !strings.Contains(string(snapshot), "source question") {
		t.Fatal("native worker did not receive only the selected source snapshot", snapshotErr)
	}
	var saved marker
	b, err := os.ReadFile(filepath.Join(runtimeDir, "teamcross-runtime.json"))
	if err != nil || json.Unmarshal(b, &saved) != nil || saved.Version != markerVersion || saved.SourceID != source.ID || saved.SessionID != newID || saved.JobID != jobID {
		t.Fatal("invalid collaboration ownership marker", saved, err)
	}
	p.Close()
	stopped, err := os.ReadFile(stopPath)
	if err != nil || string(stopped) != "stop\n"+jobID+"\n" {
		t.Fatal("did not stop exact owned job", string(stopped), err)
	}
	if used, _ := os.ReadFile(daemonStopPath); strings.TrimSpace(string(used)) != runtimeDir {
		t.Fatal("stopped the personal Claude daemon", string(used))
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
			runtimeDir := t.TempDir()
			id := uuid.NewString()
			job := id[:8]
			dir, err := os.MkdirTemp("/tmp", "cc-daemon-fixture-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(dir) })
			socket := filepath.Join(dir, "control.sock")
			meta := marker{Version: markerVersion, SourceID: uuid.NewString(), SessionID: id, JobID: job, Cwd: home}
			if err = writeJSON(filepath.Join(runtimeDir, "teamcross-runtime.json"), meta); err != nil {
				t.Fatal(err)
			}
			writeJSON(filepath.Join(runtimeDir, "saved.json"), []Job{{ID: job, SessionID: id, Cwd: home, State: "done"}})
			writeJSON(filepath.Join(runtimeDir, "running.json"), []Job{{ID: job, SessionID: id, Cwd: home, PID: 12345, Status: "idle", State: "done"}})
			script := `#!/bin/sh
case "$1" in
 --version) printf '%s\n' '2.1.268 (Claude Code)';;
	 agents) if test "$CLAUDE_CONFIG_DIR" != "` + runtimeDir + `"; then printf '[]\n'; elif test -f "` + filepath.Join(runtimeDir, "live") + `"; then cat "` + filepath.Join(runtimeDir, "running.json") + `"; else cat "` + filepath.Join(runtimeDir, "saved.json") + `"; fi;;
 --resume) test "$#" = 3 && test "$3" = --bg || exit 1
   touch "` + filepath.Join(runtimeDir, "resume-called") + `" "` + filepath.Join(runtimeDir, "live") + `"
   printf '%s\n' 'backgrounded · ` + job + `';;
 daemon) printf '%s\n' '` + dir + `';;
 stop) exit 0;;
esac
`
			binary := filepath.Join(home, "claude")
			os.WriteFile(binary, []byte(script), 0700)
			if live {
				os.WriteFile(filepath.Join(runtimeDir, "live"), nil, 0600)
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
			p, err := Restore(context.Background(), Config{Binary: binary, Home: home, RuntimeDir: runtimeDir, Cwd: home}, id)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			if p.Job.SessionID != id || p.Job.PID != 12345 {
				t.Fatal("wrong restored worker", p.Job)
			}
			_, err = os.Stat(filepath.Join(runtimeDir, "resume-called"))
			if live && !errors.Is(err, os.ErrNotExist) {
				t.Fatal("live worker was resumed as another copy")
			}
			if !live && err != nil {
				t.Fatal("saved done job was mistaken for a live worker")
			}
			os.Remove(filepath.Join(runtimeDir, "live"))
			if _, err = p.Status(context.Background()); err == nil {
				t.Fatal("missing process still reported online")
			}
		})
	}
}

func TestRestoreDoesNotTakeOverPersonalWorkerOutsideOwnership(t *testing.T) {
	home := t.TempDir()
	runtimeDir := t.TempDir()
	id := uuid.NewString()
	ownedJob := "11111111"
	foreignJob := "22222222"
	meta := marker{Version: markerVersion, SourceID: uuid.NewString(), SessionID: id, JobID: ownedJob, Cwd: home}
	if err := writeJSON(filepath.Join(runtimeDir, "teamcross-runtime.json"), meta); err != nil {
		t.Fatal(err)
	}
	jobsPath := filepath.Join(runtimeDir, "jobs.json")
	if err := writeJSON(jobsPath, []Job{{ID: foreignJob, SessionID: id, Cwd: home, PID: 12345, Status: "idle"}}); err != nil {
		t.Fatal(err)
	}
	resumePath := filepath.Join(runtimeDir, "resume-called")
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
 --version) printf '%%s\n' '2.1.270 (Claude Code)';;
	 agents) if test "$CLAUDE_CONFIG_DIR" = %q; then cat %q; else printf '[]\n'; fi;;
 --resume) touch %q; exit 1;;
 *) exit 1;;
esac
`, home, jobsPath, resumePath)
	binary := filepath.Join(runtimeDir, "claude")
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := Restore(context.Background(), Config{Binary: binary, Home: home, RuntimeDir: runtimeDir, Cwd: home}, id)
	if err == nil || !strings.Contains(err.Error(), "Team Cross 之外") {
		t.Fatal("took over a personal worker", err)
	}
	if _, statErr := os.Stat(resumePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("attempted to fork an already-running personal session", statErr)
	}
}
