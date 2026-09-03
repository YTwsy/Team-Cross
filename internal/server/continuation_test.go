package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"teamcross/internal/bridgeclient"
	"teamcross/internal/domain"
	"teamcross/internal/transfer"
)

func captureForContinuation(t *testing.T, app *App) threadDetail {
	t.Helper()
	response := requestJSON(t, app.Handler(), http.MethodPost, "/api/v1/threads", createThreadRequest{Repo: app.config.Repo, Title: "Continue review", Goal: "Review parser"}, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("capture=%d %s", response.Code, response.Body.String())
	}
	var detail threadDetail
	decodeResponse(t, response, &detail)
	return detail
}

func continuationBridge(t *testing.T, app *App, failCreate bool) string {
	t.Helper()
	log := filepath.Join(t.TempDir(), "calls.jsonl")
	script := writeBridgeFixture(t, t.TempDir(), `#!/bin/sh
log=$1
fail=$2
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$log"
  id=$(printf '%s' "$line" | sed -E 's/^\{"id":"([^"]+)".*/\1/')
  case "$line" in
    *'"method":"runs.create"'*)
      if [ "$fail" = yes ]; then
        printf '{"jsonrpc":"2.0","id":"%s","error":{"code":-32000,"message":"creation failed"}}\n' "$id"
      else
        printf '{"jsonrpc":"2.0","id":"%s","result":{"sessionId":"new-session-%s","status":"idle"}}\n' "$id" "$id"
      fi;;
    *'"method":"runs.send"'*) printf '{"jsonrpc":"2.0","id":"%s","result":{"turnId":"new-turn"}}\n' "$id";;
    *) printf '{"jsonrpc":"2.0","id":"%s","result":{}}\n' "$id";;
  esac
done
`)
	fail := "no"
	if failCreate {
		fail = "yes"
	}
	bridge, err := bridgeclient.Start(context.Background(), bridgeclient.StartOptions{Command: script, Args: []string{log, fail}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bridge.Close() })
	app.bridge = bridge
	return log
}

func TestContinueRoundExplicitNewSessionAndRetryDoesNotExecuteTwice(t *testing.T) {
	repo := serverTestRepository(t)
	app := newIntegrationApp(t, repo)
	detail := captureForContinuation(t, app)
	counter := continuationBridge(t, app, false)
	before := serverTestGit(t, repo, "status", "--porcelain=v2", "-z")
	body := map[string]any{"roundId": detail.Rounds[0].ID, "provider": "mock", "prompt": "Inspect the parser", "expectedRevision": detail.Revision}
	response := requestJSON(t, app.Handler(), http.MethodPost, "/api/v1/threads/"+detail.ID+"/continue", body, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("continue=%d %s", response.Code, response.Body.String())
	}
	var continued threadDetail
	decodeResponse(t, response, &continued)
	if continued.ID != detail.ID || continued.AgentRun == nil || continued.AgentRun.SessionID == "" || continued.AgentRun.NetworkEnabled {
		t.Fatalf("continued=%#v", continued)
	}
	runs, err := app.store.ListAgentRuns(context.Background(), detail.ID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs=%#v err=%v", runs, err)
	}
	callBytes, err := os.ReadFile(counter)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(callBytes), `"method":"runs.send"`) != 1 || !bytes.Contains(callBytes, []byte(`"method":"runs.importContext"`)) {
		t.Fatalf("unexpected calls: %s", callBytes)
	}
	if !bytes.Contains(callBytes, []byte("NEW Session")) || !bytes.Contains(callBytes, []byte("Inspect the parser")) {
		t.Fatal("explicit instruction/new-session context missing")
	}
	retry := requestJSON(t, app.Handler(), http.MethodPost, "/api/v1/threads/"+detail.ID+"/continue", body, nil)
	if retry.Code != http.StatusConflict {
		t.Fatalf("retry=%d %s", retry.Code, retry.Body.String())
	}
	afterCalls, _ := os.ReadFile(counter)
	if !bytes.Equal(callBytes, afterCalls) {
		t.Fatal("retry executed Provider RPC again")
	}
	if !bytes.Equal(before, serverTestGit(t, repo, "status", "--porcelain=v2", "-z")) {
		t.Fatal("original checkout changed")
	}
}

func TestContinueRoundRejectsChangedWorktreeIncludingIgnoredFiles(t *testing.T) {
	for _, ignored := range []bool{false, true} {
		t.Run(map[bool]string{false: "tracked", true: "ignored"}[ignored], func(t *testing.T) {
			repo := serverTestRepository(t)
			if ignored {
				if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("private.local\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				serverTestGit(t, repo, "add", ".gitignore")
				serverTestGit(t, repo, "commit", "-m", "ignore")
			}
			app := newIntegrationApp(t, repo)
			detail := captureForContinuation(t, app)
			counter := continuationBridge(t, app, false)
			filename := "tracked.txt"
			if ignored {
				filename = "private.local"
			}
			if err := os.WriteFile(filepath.Join(detail.Worktree, filename), []byte("later work\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			response := requestJSON(t, app.Handler(), http.MethodPost, "/api/v1/threads/"+detail.ID+"/continue", map[string]any{"roundId": detail.Rounds[0].ID, "provider": "mock", "prompt": "continue", "expectedRevision": detail.Revision}, nil)
			if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "worktree_diverged") {
				t.Fatalf("continue=%d %s", response.Code, response.Body.String())
			}
			if calls, _ := os.ReadFile(counter); len(calls) > 0 {
				t.Fatalf("diverged worktree dispatched Agent: %s", calls)
			}
			assertFileContent(t, filepath.Join(detail.Worktree, filename), "later work\n")
		})
	}
}

func TestContinueHistoricalRoundForksWithoutRewindingOriginal(t *testing.T) {
	app := newIntegrationApp(t, serverTestRepository(t))
	detail := captureForContinuation(t, app)
	continuationBridge(t, app, false)
	originalRound, err := app.store.GetRound(context.Background(), detail.Rounds[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.store.CreateRound(context.Background(), domain.Round{ThreadID: detail.ID, Number: 1, Kind: "checkpoint", SnapshotID: originalRound.SnapshotID, ManifestObject: originalRound.ManifestObject})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(detail.Worktree, "tracked.txt"), []byte("later work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	response := requestJSON(t, app.Handler(), http.MethodPost, "/api/v1/threads/"+detail.ID+"/continue", map[string]any{"roundId": originalRound.ID, "provider": "mock", "prompt": "explore old snapshot", "expectedRevision": detail.Revision}, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("continue old=%d %s", response.Code, response.Body.String())
	}
	var fork threadDetail
	decodeResponse(t, response, &fork)
	if fork.ID == detail.ID || fork.Worktree == detail.Worktree || fork.AgentRun == nil {
		t.Fatalf("fork=%#v", fork)
	}
	assertFileContent(t, filepath.Join(detail.Worktree, "tracked.txt"), "later work\n")
	assertFileContent(t, filepath.Join(fork.Worktree, "tracked.txt"), "baseline\n")
	thread, err := app.store.GetThread(context.Background(), fork.ID)
	if err != nil || thread.BaselineCommit != detail.Git.Head {
		t.Fatalf("baseline mismatch %#v %v", thread, err)
	}
	forkRounds, _ := app.store.ListRounds(context.Background(), fork.ID)
	manifestData, _ := app.store.GetObject(context.Background(), forkRounds[0].ManifestObject)
	var manifest sealedRoundContext
	if err := json.Unmarshal(manifestData, &manifest); err != nil || manifest.Origin == nil || manifest.Origin.RoundID != originalRound.ID {
		t.Fatalf("fork provenance %s %v", manifestData, err)
	}
}

func TestFailedContinuationInitializationKeepsOldRunAndRound(t *testing.T) {
	app := newIntegrationApp(t, serverTestRepository(t))
	detail := captureForContinuation(t, app)
	continuationBridge(t, app, true)
	old := &managedRun{ID: "old-run", ThreadID: detail.ID, Provider: "mock", SessionID: "old-session", Status: "idle"}
	app.runs[detail.ID], app.runsByID[old.ID] = old, old
	if _, err := app.store.CreateAgentRun(context.Background(), domain.AgentRun{ID: old.ID, ThreadID: detail.ID, Provider: "mock", SessionID: old.SessionID, Status: "idle"}); err != nil {
		t.Fatal(err)
	}
	response := requestJSON(t, app.Handler(), http.MethodPost, "/api/v1/threads/"+detail.ID+"/continue", map[string]any{"roundId": detail.Rounds[0].ID, "provider": "mock", "prompt": "continue", "expectedRevision": detail.Revision}, nil)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("failed continue=%d %s", response.Code, response.Body.String())
	}
	if app.runs[detail.ID] != old || old.Status != "idle" {
		t.Fatalf("old Run lost: %#v", old)
	}
	rounds, _ := app.store.ListRounds(context.Background(), detail.ID)
	if len(rounds) != 1 || rounds[0].ID != detail.Rounds[0].ID {
		t.Fatal("failed prepare changed immutable Rounds")
	}
}

func TestRoundForkAndOfflineImportNeverExecuteAgent(t *testing.T) {
	app := newIntegrationApp(t, serverTestRepository(t))
	detail := captureForContinuation(t, app)
	counter := continuationBridge(t, app, false)
	fork := requestJSON(t, app.Handler(), http.MethodPost, "/api/v1/threads/"+detail.ID+"/fork", map[string]any{"roundId": detail.Rounds[0].ID}, nil)
	if fork.Code != http.StatusCreated {
		t.Fatalf("fork=%d %s", fork.Code, fork.Body.String())
	}
	bundle, err := app.buildOfflineBundle(context.Background(), detail.ID, detail.Rounds[0].ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := requestJSON(t, app.Handler(), http.MethodPost, "/api/v1/bundles/import", bundle, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("import=%d %s", response.Code, response.Body.String())
	}
	var imported threadDetail
	decodeResponse(t, response, &imported)
	if imported.AgentRun != nil || imported.ID == detail.ID {
		t.Fatal("import did not create a passive independent Thread")
	}
	if calls, _ := os.ReadFile(counter); len(calls) > 0 {
		t.Fatalf("passive operation dispatched Agent: %s", calls)
	}
	bad := bundle
	bad.Format = "invalid"
	before, _ := app.store.ListThreads(context.Background())
	rejected := requestJSON(t, app.Handler(), http.MethodPost, "/api/v1/bundles/import", bad, nil)
	after, _ := app.store.ListThreads(context.Background())
	if rejected.Code != http.StatusBadRequest || len(before) != len(after) {
		t.Fatal("invalid import left visible Thread")
	}
}

func TestBundleSelectionExcludesUnselectedEvidenceAndCapabilities(t *testing.T) {
	app := newIntegrationApp(t, serverTestRepository(t))
	detail := captureForContinuation(t, app)
	ctx := context.Background()
	selectedObject, _ := app.store.PutObject(ctx, []byte("selected log"), "text/plain")
	privateObject, _ := app.store.PutObject(ctx, []byte("PRIVATE_SECRET"), "text/plain")
	selected, _ := app.store.CreateEvidence(ctx, domain.Evidence{ThreadID: detail.ID, Kind: "text", Title: "Selected", ObjectHash: selectedObject.Hash})
	_, _ = app.store.CreateEvidence(ctx, domain.Evidence{ThreadID: detail.ID, Kind: "text", Title: "Private", ObjectHash: privateObject.Hash})
	session, err := app.store.CreateSessionSnapshot(ctx, domain.SessionSnapshot{ThreadID: detail.ID, Source: domain.SessionRef{Provider: "codex", SessionID: "native-source", Cwd: "/Users/source/private"}, Entries: []domain.SessionEntry{{ID: "entry", Kind: "message", Text: "reviewed"}}, Capabilities: domain.SessionCapabilities{Read: true, Resume: true, TakeControl: true}})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := app.buildOfflineBundle(ctx, detail.ID, detail.Rounds[0].ID, []string{selected.ID}, []string{session.ID})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(bundle)
	if bytes.Contains(encoded, []byte("PRIVATE_SECRET")) || bytes.Contains(encoded, []byte("/Users/source/private")) {
		t.Fatal("unselected context or machine path leaked")
	}
	sessions, err := decodePortableSessions(bundle)
	if err != nil || len(sessions) != 1 || sessions[0].Capabilities.Resume || sessions[0].Capabilities.TakeControl {
		t.Fatalf("portable capability state=%#v %v", sessions, err)
	}
	if len(bundle.Evidence) != 1 || len(bundle.Objects) != 2 {
		t.Fatal("wrong selected object closure")
	}
	if err := bundle.Validate(); err != nil {
		t.Fatal(err)
	}
	bundle.Objects = append(bundle.Objects, transfer.Object{Hash: transfer.ContentHash([]byte("PRIVATE_SECRET")), Data: []byte("PRIVATE_SECRET")})
	if err := bundle.Validate(); err == nil {
		t.Fatal("unselected content object accepted")
	}
}
