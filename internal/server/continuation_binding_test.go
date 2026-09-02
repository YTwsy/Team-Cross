package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestContinueRejectsGitPointerToOriginalCheckout(t *testing.T) {
	repo := serverTestRepository(t)
	app := newIntegrationApp(t, repo)
	detail := captureForContinuation(t, app)
	counter := continuationBridge(t, app, false)
	if err := os.WriteFile(filepath.Join(detail.Worktree, ".git"), []byte("gitdir: "+filepath.Join(repo, ".git")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	response := requestJSON(t, app.Handler(), http.MethodPost, "/api/v1/threads/"+detail.ID+"/continue", map[string]any{"roundId": detail.Rounds[0].ID, "provider": "mock", "prompt": "continue", "expectedRevision": detail.Revision}, nil)
	if response.Code != http.StatusConflict {
		t.Fatalf("rebound continuation=%d %s", response.Code, response.Body.String())
	}
	if calls, _ := os.ReadFile(counter); len(calls) > 0 {
		t.Fatal("rebound root dispatched Provider")
	}
	assertFileContent(t, filepath.Join(repo, "tracked.txt"), "baseline\n")
}
