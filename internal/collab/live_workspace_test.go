package collab

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/workspace"
)

// Uses only the dedicated TestLiveCodex fixture and its Luna model. The native
// call represents a host-side relocation, not an untrusted remote client's cwd.
func verifyLiveNativeWorkspace(t *testing.T, ctx context.Context, a *App, source string, first *Session) {
	t.Helper()
	in := CreateInput{SpaceID: first.record.SpaceID, SourceID: source, WorkspaceMode: "existing", RequestID: uuid.NewString(), Title: "Same-directory native fixture"}
	preview, err := a.Preview(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	in.PreviewHash = preview.Hash
	second, err := a.Create(ctx, in)
	if err != nil {
		t.Fatal("second session in original directory", err)
	}
	original := first.record.ExecutionCwd
	if first.record.SessionID == second.record.SessionID || second.record.ExecutionCwd != original || !first.process.Alive() || !second.process.Alive() {
		t.Fatal("same-directory native sessions are not independent and online")
	}
	t.Log("two distinct native sessions active in the same original directory")
	destination := filepath.Join(filepath.Dir(a.Config.DataDir), "agent-worktree")
	if _, err = workspace.Git(ctx, original, "worktree", "add", "-b", "codex/native-workspace-fixture", destination, "HEAD"); err != nil {
		t.Fatal(err)
	}
	wait := func(name string) {
		t.Helper()
		for {
			first.mu.Lock()
			busy, pending, calls := first.busy, len(first.approvals), first.activeCalls
			first.mu.Unlock()
			if pending > 0 {
				t.Fatal("unexpected native fixture approval", name)
			}
			if !busy && calls == 0 {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(100 * time.Millisecond):
			}
		}
		data, err := os.ReadFile(filepath.Join(destination, name))
		if err != nil || strings.TrimSpace(string(data)) != "NATIVE_WORKSPACE_OK" {
			t.Fatal("native execution used the wrong directory", name, string(data), err)
		}
		if first.view()["executionCwd"] != destination {
			t.Fatal("native cwd was not reflected in the existing view", first.view()["executionCwd"])
		}
	}
	input := func(name string) []any {
		return []any{map[string]any{"type": "text", "text": "This dedicated test has moved this native thread to a new worktree. Use your command or file tool to create " + name + " in the current working directory with exactly NATIVE_WORKSPACE_OK. Do not use a directory from earlier conversation history. Do not modify other files. Then reply with the absolute path."}}
	}
	first.mu.Lock()
	p := first.process
	first.busy = true
	first.mu.Unlock()
	if err = p.Call(ctx, "turn/start", map[string]any{"threadId": first.record.SessionID, "cwd": destination, "runtimeWorkspaceRoots": []string{destination}, "model": liveModel, "effort": "low", "input": input("native-moved.txt")}, nil); err != nil {
		t.Fatal(err)
	}
	wait("native-moved.txt")
	if _, err = first.RPC(ctx, "owner", "turn/start", map[string]any{"cwd": "/remote-client-only", "runtimeWorkspaceRoots": []string{"/remote-client-only"}, "model": liveModel, "effort": "low", "input": input("native-continued.txt")}, "native-workspace-continue"); err != nil {
		t.Fatal(err)
	}
	wait("native-continued.txt")
	if err = first.Action(ctx, "pause"); err != nil {
		t.Fatal(err)
	}
	waitExecutionReleased(t, first)
	if err = first.Action(ctx, "start"); err != nil {
		t.Fatal(err)
	}
	if first.view()["executionCwd"] != destination || second.record.ExecutionCwd != original || !second.process.Alive() {
		t.Fatal("restore lost native cwd or disrupted the neighboring session")
	}
	file, err := first.Context(ctx, "file", "native-continued.txt", 0)
	if err != nil || strings.TrimSpace(file.(map[string]any)["text"].(string)) != "NATIVE_WORKSPACE_OK" {
		t.Fatal("file context did not follow native cwd", file, err)
	}
	t.Log("native cwd change, continued file execution, existing directory view and same-thread restore confirmed")
}
