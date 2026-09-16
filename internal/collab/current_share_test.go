package collab

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/mcp"
	"teamcross/internal/problem"
	"teamcross/internal/workspace"
)

func setSourceTurn(f *fakeRuntime, id, status string) {
	f.mu.Lock()
	f.sourceTurnID, f.sourceTurnStatus = id, status
	f.mu.Unlock()
}

func currentShareFixture(t *testing.T, a *App, f *fakeRuntime) currentShareInput {
	t.Helper()
	setSourceTurn(f, "current-turn", "inProgress")
	in := currentShareInput{CreateInput: CreateInput{Provider: "codex", SourceID: f.source.ID, RequestID: uuid.NewString(), WorkspaceMode: "existing"}, Caller: mcp.CallerSource{Provider: "codex", SourceID: f.source.ID, TurnID: "current-turn"}, Transport: "lan"}
	p, err := a.previewCurrentShare(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	in.PreviewHash = p.Hash
	if p.SourceTurnStatus != "inProgress" {
		t.Fatal(p)
	}
	return in
}

func waitShare(t *testing.T, j *shareRequest, state string) shareRequestRecord {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r := j.snapshot()
		if r.State == state {
			return r
		}
		if !shareRequestActive(r.State) {
			t.Fatalf("share ended unexpectedly: %+v", r)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("share did not become %s: %+v", state, j.snapshot())
	return shareRequestRecord{}
}

func TestCurrentShareWaitsForExactRoundAndCreatesOnce(t *testing.T) {
	a, f, _ := fixture(t)
	in := currentShareFixture(t, a, f)
	ctx := context.Background()
	if _, err := a.Preview(ctx, in.CreateInput); err == nil {
		t.Fatal("ordinary creation accepted active source")
	}
	j, err := a.submitCurrentShare(ctx, in)
	if err != nil || j.snapshot().State != "waiting" {
		t.Fatal(j, err)
	}
	if a.Active() != 1 {
		t.Fatal("pending share was omitted from Core stop guard")
	}
	again, err := a.submitCurrentShare(ctx, in)
	if err != nil || again != j {
		t.Fatal("duplicate request", err)
	}
	changed := in
	changed.Transport = "tailcat"
	if _, err = a.submitCurrentShare(ctx, changed); err == nil {
		t.Fatal("retry changed transport")
	}
	f.mu.Lock()
	forks := f.forks
	f.mu.Unlock()
	if forks != 0 {
		t.Fatal("forked before completion")
	}
	setSourceTurn(f, "current-turn", "completed")
	r := waitShare(t, j, "ready")
	if r.CollaborationID != in.RequestID {
		t.Fatal(r)
	}
	if a.Active() != 1 {
		t.Fatal("share and collaboration counted twice")
	}
	b := managementBackend(t, a)
	status := invokeObject(t, b, "get_share_request", map[string]any{"id": in.RequestID})
	if status["state"] != "ready" || status["invitation"] != nil {
		t.Fatal(status)
	}
	c := invokeObject(t, b, "get_collaboration", map[string]any{"id": r.CollaborationID})
	if c["sessionId"] == in.SourceID {
		t.Fatal("shared original source")
	}
	inv := invokeObject(t, b, "create_invitation", map[string]any{"id": r.CollaborationID, "transport": "lan"})
	if inv["invitation"] == nil {
		t.Fatal("no completed invitation")
	}
	setSourceTurn(f, "later-turn", "inProgress")
	if replay, err := a.submitCurrentShare(ctx, in); err != nil || replay != j {
		t.Fatal("completed retry did not return original outcome", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.forks != 1 {
		t.Fatal("duplicate fork", f.forks)
	}
	for _, call := range f.calls {
		if strings.HasPrefix(call, "turn/") {
			t.Fatal("creation sent input", call)
		}
	}
}

func TestCurrentShareStopsOnSourceDriftOrIncompleteRound(t *testing.T) {
	for _, what := range []string{"new-round", "git-head", "interrupted"} {
		t.Run(what, func(t *testing.T) {
			a, f, repo := fixture(t)
			in := currentShareFixture(t, a, f)
			j, err := a.submitCurrentShare(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			switch what {
			case "new-round":
				setSourceTurn(f, "later-turn", "completed")
			case "git-head":
				if _, err := workspace.Git(context.Background(), repo, "commit", "--allow-empty", "-m", "changed head"); err != nil {
					t.Fatal(err)
				}
				setSourceTurn(f, "current-turn", "completed")
			case "interrupted":
				setSourceTurn(f, "current-turn", "interrupted")
			}
			r := waitShare(t, j, "failed")
			want := "preview_changed"
			if what == "interrupted" {
				want = "source_turn_incomplete"
			}
			if r.Error == nil || r.Error.Code != want {
				t.Fatal(r)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.forks != 0 {
				t.Fatal("forked invalid source")
			}
		})
	}
}

func TestCurrentShareRetainsPartialCreationForRecovery(t *testing.T) {
	a, f, _ := fixture(t)
	in := currentShareFixture(t, a, f)
	in.WorkspaceMode = "worktree"
	p, err := a.previewCurrentShare(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	in.PreviewHash = p.Hash
	f.mu.Lock()
	f.source.Model = "rejected-fixture-model"
	f.mu.Unlock()
	j, err := a.submitCurrentShare(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	setSourceTurn(f, "current-turn", "completed")
	r := waitShare(t, j, "failed")
	if r.CollaborationID != in.RequestID {
		t.Fatal("lost partial collaboration identity", r)
	}
	a.mu.Lock()
	s := a.sessions[r.CollaborationID]
	a.mu.Unlock()
	if s == nil || s.view()["state"] != "error" {
		t.Fatal("lost partial creation")
	}
	if _, err := os.Stat(filepath.Join(a.Config.DataDir, "collaborations", r.CollaborationID, "worktree", "file.txt")); err != nil {
		t.Fatal("removed partial worktree", err)
	}
	if again, err := a.submitCurrentShare(context.Background(), in); err != nil || again != j {
		t.Fatal("replayed failed creation", err)
	}
}

func TestCurrentShareCancelAndRestartDoNotReplay(t *testing.T) {
	a, f, _ := fixture(t)
	in := currentShareFixture(t, a, f)
	j, err := a.submitCurrentShare(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	b := managementBackend(t, a)
	cancelled := invokeObject(t, b, "cancel_share_request", map[string]any{"id": in.RequestID})
	if cancelled["state"] != "cancelled" {
		t.Fatal(cancelled)
	}
	if a.Active() != 0 {
		t.Fatal("cancelled request still active")
	}
	in.RequestID = uuid.NewString()
	p, err := a.previewCurrentShare(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	in.PreviewHash = p.Hash
	pending, err := a.submitCurrentShare(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	a.Close()
	if j.snapshot().State != "cancelled" || pending.snapshot().State != "interrupted" {
		t.Fatal(j.snapshot(), pending.snapshot())
	}
	// Model an abrupt exit after persisting a waiting/creating/inviting record.
	crashed := pending.snapshot()
	crashed.State = "creating"
	if err := writeJSONFile(filepath.Join(a.Config.DataDir, "share-requests", crashed.ID+".json"), crashed); err != nil {
		t.Fatal(err)
	}
	setSourceTurn(f, "current-turn", "completed")
	restarted, err := Open(a.Config)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	r := restarted.shareRequests[crashed.ID].snapshot()
	if r.State != "interrupted" || r.Error == nil || r.Error.Code != "share_interrupted" {
		t.Fatal(r)
	}
	if restarted.shareRequests[j.snapshot().ID].snapshot().State != "cancelled" {
		t.Fatal("lost cancellation")
	}
	if len(restarted.sessions) != 0 {
		t.Fatal("restart created a collaboration")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.forks != 0 {
		t.Fatal("restart replayed creation")
	}
}

func TestCurrentSourceRequiresLatestTurnAndClaudeToolEvidence(t *testing.T) {
	a, f, repo := fixture(t)
	_, err := a.currentSource(context.Background(), mcp.CallerSource{Provider: "codex", SourceID: f.source.ID, TurnID: "old-turn"})
	if err == nil || problem.Describe(err).Code != "source_context_changed" {
		t.Fatal(err)
	}
	home, id := t.TempDir(), uuid.NewString()
	a.Config.ClaudeHome = home
	path := filepath.Join(home, "projects", "fixture", id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(file)
	for _, row := range []map[string]any{
		{"type": "user", "uuid": "u1", "cwd": repo, "message": map[string]any{"content": "Share current session"}},
		{"type": "assistant", "uuid": "a1", "parentUuid": "u1", "cwd": repo, "message": map[string]any{"content": []map[string]any{{"type": "tool_use", "id": "call-current", "name": "get_current_source", "input": map[string]any{}}}, "stop_reason": "tool_use"}},
	} {
		if err := enc.Encode(row); err != nil {
			t.Fatal(err)
		}
	}
	file.Close()
	caller := mcp.CallerSource{Provider: "claude", SourceID: id, ToolUseID: "call-current"}
	got, err := a.currentSource(context.Background(), caller)
	if err != nil || got["turnId"] != "u1" || got["turnStatus"] != "inProgress" {
		t.Fatal(got, err)
	}
	appended := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
		if err == nil {
			_, err = f.WriteString("{\"type\":\"assistant\",\"uuid\":\"a2\",\"parentUuid\":\"a1\",\"message\":{\"content\":[{\"type\":\"tool_use\",\"id\":\"call-delayed\",\"name\":\"preview_current_share\",\"input\":{}}],\"stop_reason\":\"tool_use\"}}\n")
			f.Close()
		}
		appended <- err
	}()
	caller.ToolUseID = "call-delayed"
	if _, err := a.currentSource(context.Background(), caller); err != nil {
		t.Fatal("did not wait for native transcript flush", err)
	}
	if err := <-appended; err != nil {
		t.Fatal(err)
	}
	caller.ToolUseID = "call-from-resumed-session"
	if _, err := a.currentSource(context.Background(), caller); err == nil || problem.Describe(err).Code != "source_context_unavailable" {
		t.Fatal("accepted stale process session identity", err)
	}
}
