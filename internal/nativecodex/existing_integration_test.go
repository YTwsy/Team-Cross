package nativecodex

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in protocol probe. This creates its own daemon/home/repository and never
// opens the user's native sessions or calls a model.
func TestExistingNativeDaemonConnection(t *testing.T) {
	if os.Getenv("TEAMCROSS_TEST_NATIVE_DAEMON") != "1" {
		t.Skip("explicit native daemon probe only")
	}
	binary, err := Binary()
	if err != nil {
		t.Fatal(err)
	}
	home, repo := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("model=\"gpt-5.6-luna\"\n[features]\nplugins=false\nmemories=false\nmulti_agent=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(filepath.Join(home, "daemon.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "app-server", "--listen", "unix://")
	cmd.Env, cmd.Dir, cmd.Stdout, cmd.Stderr = environment(home), repo, log, log
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		_ = log.Close()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var owner *Process
	for deadline := time.Now().Add(12 * time.Second); time.Now().Before(deadline); {
		owner, err = connectProxy(ctx, binary, home, filepath.Join(home, "owner.log"))
		if err == nil {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if err != nil {
		raw, _ := os.ReadFile(filepath.Join(home, "daemon.log"))
		t.Fatalf("%v\n%s", err, raw)
	}
	defer owner.Close()
	var created struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err = owner.Call(ctx, "thread/start", map[string]any{"cwd": repo, "model": "gpt-5.6-luna"}, &created); err != nil {
		t.Fatal(err)
	}
	borrowed, err := ConnectExisting(ctx, binary, home, filepath.Join(home, "receiver.log"), created.Thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer borrowed.Close()
	if _, err = ConnectExisting(ctx, binary, home, filepath.Join(home, "missing.log"), "not-loaded"); err == nil {
		t.Fatal("accepted an unloaded thread")
	}
	borrowed.Close()
	var read json.RawMessage
	if err = owner.Call(ctx, "thread/read", map[string]any{"threadId": created.Thread.ID, "includeTurns": false}, &read); err != nil {
		t.Fatal("closing the borrowed connection damaged the owner", err)
	}
	select {
	case <-done:
		t.Fatal("borrowed connection stopped daemon")
	default:
	}
	t.Log("isolated native daemon accepted a second WebSocket proxy; exact loaded-thread check and independent close passed")
}
