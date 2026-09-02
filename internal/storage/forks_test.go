package storage

import (
	"context"
	"errors"
	"testing"

	"teamcross/internal/domain"
)

func TestForkImportTransactionRollsBackVisibleThreadOnFailure(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	object, err := store.PutObject(ctx, []byte("{}"), "application/json")
	if err != nil {
		t.Fatal(err)
	}
	thread := domain.Thread{ID: "fork-thread", Title: "Fork", RepoRoot: "/owned/repo", WorktreePath: "/owned/worktree", BaselineCommit: "baseline"}
	snapshot := domain.GitSnapshot{ID: "fork-git", ThreadID: thread.ID, Head: thread.BaselineCommit, StagedPatchObject: object.Hash}
	round := domain.Round{ID: "fork-round", ThreadID: thread.ID, SnapshotID: snapshot.ID, Kind: "fork", ManifestObject: object.Hash}
	// Fail after Thread, Git snapshot and Round insertions, not during preflight.
	if _, err := store.db.ExecContext(ctx, `CREATE TRIGGER reject_fork_origin BEFORE INSERT ON events BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateForkImport(ctx, thread, snapshot, round, nil, nil, []byte(`{"origin":"source"}`)); err == nil {
		t.Fatal("injected failure ignored")
	}
	if _, err := store.GetThread(ctx, thread.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("partial Thread visible: %v", err)
	}
	if _, err := store.GetRound(ctx, round.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("partial Round visible: %v", err)
	}
	if _, err := store.GetGitSnapshot(ctx, snapshot.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("partial snapshot visible: %v", err)
	}
}
