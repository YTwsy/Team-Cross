package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"teamcross/internal/nativecodex"
	"teamcross/internal/workspace"
)

func TestNativeWorkspaceFollowsThreadThroughInputAndCompletion(t *testing.T) {
	a, f, repo := fixture(t)
	s := createFixture(t, a, f, "existing")
	original := s.record.ExecutionCwd
	ctx := context.Background()
	destination := filepath.Join(t.TempDir(), "agent-worktree")
	if _, err := workspace.Git(ctx, repo, "worktree", "add", "-b", "agent-branch", destination, "HEAD"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "file.txt"), []byte("native workspace edit"), 0600); err != nil {
		t.Fatal(err)
	}
	id := s.record.SessionID
	// Simulate a native relocation, independent of the remote client's path.
	if err := f.Call(ctx, "thread/resume", map[string]any{"threadId": id, "cwd": destination}, nil); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"thread/resume", "thread/settings/update", "turn/start"} {
		_, err := s.RPC(ctx, "owner", method, map[string]any{"cwd": "/client-only", "runtimeWorkspaceRoots": []string{"/client-only"}, "input": []any{}}, method)
		if err != nil {
			t.Fatal(err)
		}
		f.mu.Lock()
		request := f.requests[method]
		nativeCwd := f.threads[id].Cwd
		f.mu.Unlock()
		if request["cwd"] != nil || request["runtimeWorkspaceRoots"] != nil || nativeCwd != destination {
			t.Fatalf("%s reset native workspace: %v, %s", method, request, nativeCwd)
		}
		if s.view()["executionCwd"] != destination {
			t.Fatal("existing directory display did not follow native thread")
		}
	}
	file, err := s.Context(ctx, "file", "file.txt", 0)
	if err != nil || file.(map[string]any)["text"] != "native workspace edit" {
		t.Fatal("file view used old workspace", file, err)
	}
	changes, err := s.Context(ctx, "changes", "", 0)
	if err != nil || !strings.Contains(changes.(map[string]any)["diff"].(string), "+native workspace edit") {
		t.Fatal("changes view used old workspace", changes, err)
	}
	if s.record.WorkspaceOwned || s.record.WorkspaceRoot != original || s.record.WorkspaceMode != "existing" {
		t.Fatal("native relocation changed directory ownership or creation provenance")
	}
	// A shell's workdir does not relocate the thread.
	params, _ := json.Marshal(map[string]any{"threadId": id, "item": map[string]any{"type": "commandExecution", "cwd": repo}})
	s.onMessage(nativecodex.Message{Method: "item/completed", Params: params})
	if s.view()["executionCwd"] != destination {
		t.Fatal("command cwd was treated as session cwd")
	}
	// A native change during the turn is observed after completion, without a
	// follow-up prompt or opening another client.
	if err := f.Call(ctx, "thread/resume", map[string]any{"threadId": id, "cwd": repo}, nil); err != nil {
		t.Fatal(err)
	}
	params, _ = json.Marshal(map[string]any{"threadId": id, "turn": map[string]any{"id": "active-turn"}})
	before := s.view()["sequence"].(uint64)
	s.onMessage(nativecodex.Message{Method: "turn/completed", Params: params})
	deadline := time.Now().Add(time.Second)
	for s.view()["executionCwd"] != repo && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.view()["executionCwd"] != repo {
		t.Fatal("completion did not refresh native cwd")
	}
	if s.view()["sequence"].(uint64) <= before+1 {
		t.Fatal("directory update did not invalidate the existing context views")
	}
}

func TestRestoreStartsInNativeWorkspace(t *testing.T) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	original := s.record.ExecutionCwd
	id, execution := s.record.SessionID, s.record.ID
	providerHome := s.record.ProviderHome
	a.Close()
	// Recovery must read the saved native home even if Core's default changed.
	t.Setenv("CODEX_HOME", t.TempDir())
	destination := t.TempDir()
	if err := f.Call(context.Background(), "thread/resume", map[string]any{"threadId": id, "cwd": destination}, nil); err != nil {
		t.Fatal(err)
	}
	config := a.Config
	var launchedCwd, readHome string
	config.StartProcess = func(_, home, cwd, log string) (Runtime, error) {
		if filepath.Base(log) == "runtime.log" {
			launchedCwd = cwd
		} else if filepath.Base(log) == "reader.log" {
			readHome = home
		}
		f.mu.Lock()
		f.alive = true
		f.mu.Unlock()
		return f, nil
	}
	b, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	restored := b.executions[execution]
	if restored.record.ExecutionCwd != original {
		t.Fatal("fixture did not retain the stale saved directory")
	}
	if err = restored.Action(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	if readHome != providerHome || launchedCwd != destination || restored.view()["executionCwd"] != destination || f.requests["thread/resume"]["cwd"] != destination {
		t.Fatal("restored process or native session used the old cwd", launchedCwd, restored.view())
	}
	if restored.record.SessionID != id || f.forks != 1 {
		t.Fatal("native relocation created another fork")
	}
	for _, method := range f.calls {
		if method == "turn/start" {
			t.Fatal("restore sent a prompt")
		}
	}
	b.Close()
	c, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.executions[execution].record.ExecutionCwd != destination {
		t.Fatal("confirmed native cwd was not persisted")
	}
}

type workspaceReadRuntime struct {
	Runtime
	read func(out any) error
}

func (p *workspaceReadRuntime) Call(ctx context.Context, method string, in, out any) error {
	if method == "thread/read" {
		return p.read(out)
	}
	return p.Runtime.Call(ctx, method, in, out)
}

func TestNativeWorkspaceRejectsUnconfirmedOrStaleReads(t *testing.T) {
	for _, scenario := range []string{"empty", "relative", "wrong-thread", "read-error", "restarted", "closed"} {
		t.Run(scenario, func(t *testing.T) {
			a, f, _ := fixture(t)
			s := createFixture(t, a, f, "existing")
			original := s.record.ExecutionCwd
			p := &workspaceReadRuntime{Runtime: f}
			p.read = func(out any) error {
				thread := Source{ID: s.record.SessionID, Cwd: t.TempDir()}
				switch scenario {
				case "empty":
					thread.Cwd = ""
				case "relative":
					thread.Cwd = "../unconfirmed"
				case "wrong-thread":
					thread.ID = "unrelated"
				case "read-error":
					return fmt.Errorf("native read unavailable")
				case "restarted":
					s.mu.Lock()
					s.generation++
					s.mu.Unlock()
				case "closed":
					a.Close()
				}
				encoded, _ := json.Marshal(map[string]any{"thread": thread})
				return json.Unmarshal(encoded, out)
			}
			s.process = p
			s.refreshNativeState(context.Background(), p, true)
			if s.record.ExecutionCwd != original {
				t.Fatal("unconfirmed response changed the workspace")
			}
		})
	}
}
