package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"teamcross/internal/domain"
	"teamcross/internal/transfer"
)

func TestSealedRoundsPreserveCapturedFilesAfterIgnoreWithoutWidening(t *testing.T) {
	for _, kind := range []string{"agent_turn", "agent_switch"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			repo := serverTestRepository(t)
			captured := []byte{0, 255, 2, 9, 0, 42}
			writeCapturedFixture(t, filepath.Join(repo, "captured.bin"), captured)
			app := newIntegrationApp(t, repo)
			detail := captureSelectedFixture(t, app, []string{"captured.bin"})
			thread, err := app.store.GetThread(ctx, detail.ID)
			if err != nil {
				t.Fatal(err)
			}
			initial, err := app.store.GetRound(ctx, detail.Rounds[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			before := serverTestGit(t, repo, "status", "--porcelain=v2", "-z", "--untracked-files=all")
			writeCapturedFixture(t, filepath.Join(thread.WorktreePath, ".gitignore"), []byte("*.bin\nprivate.txt\n"))
			writeCapturedFixture(t, filepath.Join(thread.WorktreePath, "private.txt"), []byte("UNSHARED IGNORED SECRET\n"))
			round := sealCapturedFixture(t, app, thread, kind)
			manifestData, err := app.store.GetObject(ctx, round.ManifestObject)
			if err != nil {
				t.Fatal(err)
			}
			var manifest turnManifest
			if err := json.Unmarshal(manifestData, &manifest); err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(manifest.CapturedPaths, "captured.bin") || slices.Contains(manifest.CapturedPaths, "private.txt") {
				t.Fatalf("wrong cumulative capture grant: %q", manifest.CapturedPaths)
			}
			bundle, err := app.buildOfflineBundle(ctx, thread.ID, round.ID, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := transfer.Materialize(ctx, bundle, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Cleanup()
			data, err := os.ReadFile(filepath.Join(restored.Worktree, "captured.bin"))
			if err != nil || !bytes.Equal(data, captured) {
				t.Fatalf("sealed captured content=%v err=%v", data, err)
			}
			if _, err := os.Lstat(filepath.Join(restored.Worktree, "private.txt")); !os.IsNotExist(err) {
				t.Fatal("unselected ignored file escaped into the immutable Round")
			}
			// A received Round 0 stores new ignored files inside its patch, not
			// necessarily in an Untracked manifest. They must survive resealing.
			receiver := newIntegrationApp(t, "")
			fork, err := receiver.importOfflineBundle(ctx, bundle, "")
			if err != nil {
				t.Fatal(err)
			}
			resealed := sealCapturedFixture(t, receiver, fork, "agent_turn")
			reexported, err := receiver.buildOfflineBundle(ctx, fork.ID, resealed.ID, nil, nil)
			if err != nil || !bytes.Contains(reexported.Snapshot.UnstagedPatch, []byte("captured.bin")) {
				t.Fatalf("received/resealed snapshot dropped ignored captured file: %v", err)
			}
			// Same-Thread capture provenance does not restore deleted bytes.
			if err := os.Remove(filepath.Join(thread.WorktreePath, "captured.bin")); err != nil {
				t.Fatal(err)
			}
			deleted := sealCapturedFixture(t, app, thread, kind)
			deletedBundle, err := app.buildOfflineBundle(ctx, thread.ID, deleted.ID, nil, nil)
			if err != nil || bytes.Contains(deletedBundle.Snapshot.UnstagedPatch, []byte("captured.bin")) {
				t.Fatalf("deleted capture resurrected in export: %v", err)
			}
			unchanged, err := app.store.GetRound(ctx, initial.ID)
			if err != nil || unchanged != initial {
				t.Fatal("later capture rewrote original immutable Round")
			}
			if !bytes.Equal(before, serverTestGit(t, repo, "status", "--porcelain=v2", "-z", "--untracked-files=all")) {
				t.Fatal("sealing modified original checkout")
			}
		})
	}
}

func TestExportReadsLegacySealedPatchPathGrants(t *testing.T) {
	ctx := context.Background()
	app := newIntegrationApp(t, serverTestRepository(t))
	detail := captureForContinuation(t, app)
	thread, err := app.store.GetThread(ctx, detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	writeCapturedFixture(t, filepath.Join(thread.WorktreePath, "legacy.bin"), []byte{0, 1, 255})
	patch, err := app.exportThreadPatch(ctx, thread)
	if err != nil {
		t.Fatal(err)
	}
	object, err := app.store.PutObject(ctx, patch, "application/x-git-diff")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := app.store.PutObject(ctx, jsonBytes(map[string]string{"patchObject": object.Hash}), "application/json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.CreateRound(ctx, domain.Round{ThreadID: thread.ID, Number: 1, Kind: "agent_turn", ManifestObject: manifest.Hash}); err != nil {
		t.Fatal(err)
	}
	writeCapturedFixture(t, filepath.Join(thread.WorktreePath, ".gitignore"), []byte("*.bin\n"))
	writeCapturedFixture(t, filepath.Join(thread.WorktreePath, "unselected.bin"), []byte("PRIVATE"))
	patch, err = app.exportThreadPatch(ctx, thread)
	if err != nil || !bytes.Contains(patch, []byte("legacy.bin")) || bytes.Contains(patch, []byte("unselected.bin")) {
		t.Fatalf("legacy grant export failed: %v %s", err, patch)
	}
}

func TestExportRejectsCrossThreadSnapshotAndUnsafeCapturedGrant(t *testing.T) {
	for _, unsafe := range []bool{false, true} {
		t.Run(map[bool]string{false: "cross-thread", true: "unsafe-path"}[unsafe], func(t *testing.T) {
			ctx := context.Background()
			app := newIntegrationApp(t, serverTestRepository(t))
			detail := captureForContinuation(t, app)
			thread, err := app.store.GetThread(ctx, detail.ID)
			if err != nil {
				t.Fatal(err)
			}
			round := domain.Round{ThreadID: thread.ID, Number: 1, Kind: "fixture"}
			if unsafe {
				object, err := app.store.PutObject(ctx, jsonBytes(map[string]any{"capturedPaths": []string{"../secret"}}), "application/json")
				if err != nil {
					t.Fatal(err)
				}
				round.ManifestObject = object.Hash
			} else {
				other := captureForContinuation(t, app)
				otherRound, err := app.store.GetRound(ctx, other.Rounds[0].ID)
				if err != nil {
					t.Fatal(err)
				}
				round.SnapshotID = otherRound.SnapshotID
			}
			if _, err := app.store.CreateRound(ctx, round); err != nil {
				t.Fatal(err)
			}
			if patch, err := app.exportThreadPatch(ctx, thread); err == nil {
				t.Fatalf("untrusted captured scope accepted: %q", patch)
			}
		})
	}
}

func captureSelectedFixture(t *testing.T, app *App, selected []string) threadDetail {
	t.Helper()
	response := requestJSON(t, app.Handler(), http.MethodPost, "/api/v1/threads", createThreadRequest{Repo: app.config.Repo, Title: "Captured binary", Goal: "Preserve selected context", Untracked: selected}, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("capture=%d %s", response.Code, response.Body.String())
	}
	var detail threadDetail
	decodeResponse(t, response, &detail)
	return detail
}

func writeCapturedFixture(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

// Synthetic completion/checkpoint only; never starts a Provider.
func sealCapturedFixture(t *testing.T, app *App, thread domain.Thread, kind string) domain.Round {
	t.Helper()
	ctx := context.Background()
	status := "idle"
	if kind == "agent_switch" {
		status = "closed" // Deterministic handoff, with no Bridge RPC.
	}
	stored, err := app.store.CreateAgentRun(ctx, domain.AgentRun{ThreadID: thread.ID, Provider: "mock", Status: status})
	if err != nil {
		t.Fatal(err)
	}
	run := &managedRun{ID: stored.ID, ThreadID: thread.ID, Provider: "mock", Status: status}
	app.runs[thread.ID] = run
	if kind == "agent_switch" {
		prepared, err := app.prepareAgentSwitch(ctx, thread, run, "mock")
		if err != nil {
			t.Fatal(err)
		}
		if err := app.commitPreparedAgentSwitch(ctx, prepared); err != nil {
			t.Fatal(err)
		}
	} else {
		completed, err := app.store.AppendEvent(ctx, thread.ID, "turn.completed", jsonBytes(map[string]string{"runId": run.ID, "turnId": stored.ID, "status": "completed"}))
		if err != nil {
			t.Fatal(err)
		}
		app.sealCompletedTurn(run, stored.ID, completed, map[string]any{"status": "completed"})
	}
	rounds, err := app.store.ListRounds(ctx, thread.ID)
	if err != nil || len(rounds) < 2 || rounds[len(rounds)-1].AgentRunID != run.ID || rounds[len(rounds)-1].Kind != kind {
		t.Fatalf("checkpoint not sealed: %v %v", rounds, err)
	}
	return rounds[len(rounds)-1]
}
