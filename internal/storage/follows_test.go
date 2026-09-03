package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"teamcross/internal/domain"
)

func followFixture(t *testing.T, store *Store) domain.SessionFollow {
	t.Helper()
	ctx := context.Background()
	snapshot := domain.SessionSnapshot{ID: "imported", Source: domain.SessionRef{Provider: "codex", SessionID: "native-only", IdentityKind: "thread.id", Surface: "synthetic", ProviderVersion: "test"}, Capabilities: domain.SessionCapabilities{Read: true, Follow: true}, Entries: []domain.SessionEntry{{ID: "entry", Kind: "message", Text: "old"}}, Warnings: []string{}}
	thread, err := store.CreateSessionReviewThread(ctx, "Follow test", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	follow, created, err := store.CreateSessionFollow(ctx, thread.ID, snapshot.ID)
	if err != nil || !created {
		t.Fatalf("start: %#v %v %v", follow, created, err)
	}
	return follow
}

func followCapture(t *testing.T, store *Store, follow domain.SessionFollow) (domain.SessionFollow, domain.SessionSnapshot, domain.Round) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	snapshot := domain.SessionSnapshot{ID: newID(), ThreadID: follow.ThreadID, Source: follow.Source, CapturedAt: now, Entries: []domain.SessionEntry{{ID: "entry", Kind: "message", Text: "new"}}, Capabilities: domain.SessionCapabilities{Read: true, Follow: true}, Warnings: []string{}}
	rounds, err := store.ListRounds(ctx, follow.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{"sessionSnapshotIds": []string{snapshot.ID}})
	object, err := store.PutObject(ctx, data, "application/json")
	if err != nil {
		t.Fatal(err)
	}
	round := domain.Round{ID: newID(), ThreadID: follow.ThreadID, Number: int64(len(rounds)), ParentRoundID: rounds[len(rounds)-1].ID, Kind: "session_follow", CreatedAt: now, ManifestObject: object.Hash}
	next := follow
	next.CurrentSnapshotID, next.Cursor, next.UpdatedAt, next.LastPolledAt = snapshot.ID, "opaque-local-cursor", now, &now
	return next, snapshot, round
}

func TestFollowCheckpointRestartAndNoExecution(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	store.DB().SetMaxOpenConns(1)
	follow := followFixture(t, store)
	again, created, err := store.CreateSessionFollow(ctx, follow.ThreadID, follow.SourceSnapshotID)
	if err != nil || created || again.ID != follow.ID {
		t.Fatalf("duplicate start: %#v %v %v", again, created, err)
	}
	next, snapshot, round := followCapture(t, store, follow)
	if err := store.CommitFollowPoll(ctx, follow, next, &snapshot, &round); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	reopened, err := store.GetSessionFollow(ctx, follow.ThreadID, follow.ID)
	if err != nil || reopened.Cursor != next.Cursor || reopened.CurrentSnapshotID != snapshot.ID || reopened.Epoch != 1 || reopened.State != "active" {
		t.Fatalf("restart lost checkpoint: %#v %v", reopened, err)
	}
	encoded, _ := json.Marshal(reopened)
	if strings.Contains(string(encoded), "cursor") || strings.Contains(string(encoded), "opaque-local") {
		t.Fatal("cursor escaped persistence")
	}
	before, _ := store.GetThread(ctx, follow.ThreadID)
	unchanged := reopened
	unchanged.Cursor = "next-cursor-no-new-output"
	if err := store.CommitFollowPoll(ctx, reopened, unchanged, nil, nil); err != nil {
		t.Fatal(err)
	}
	after, _ := store.GetThread(ctx, follow.ThreadID)
	if before.Revision != after.Revision {
		t.Fatal("unchanged polling changed the public revision")
	}
	old, err := store.GetSessionSnapshot(ctx, follow.ThreadID, follow.SourceSnapshotID)
	if err != nil || old.Entries[0].Text != "old" {
		t.Fatal("Follow modified imported evidence")
	}
	for _, table := range []string{"agent_runs", "git_snapshots"} {
		var count int
		if err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("Follow created %s: %d %v", table, count, err)
		}
	}
}

func TestFollowStopFencesDelayedCheckpointAndRetry(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	follow := followFixture(t, store)
	next, snapshot, round := followCapture(t, store, follow)
	stopped, err := store.StopSessionFollow(ctx, follow.ThreadID, follow.ID)
	if err != nil || stopped.State != "stopped" || stopped.Epoch != 2 {
		t.Fatalf("stop: %#v %v", stopped, err)
	}
	if err := store.CommitFollowPoll(ctx, follow, next, &snapshot, &round); !errors.Is(err, domain.ErrFollowFence) {
		t.Fatalf("late read survived stop: %v", err)
	}
	if err := store.RecordFollowRetry(ctx, follow, "late failure"); !errors.Is(err, domain.ErrFollowFence) {
		t.Fatalf("late failure revived reader: %v", err)
	}
	again, err := store.StopSessionFollow(ctx, follow.ThreadID, follow.ID)
	if err != nil || again.Epoch != stopped.Epoch {
		t.Fatal("repeated stop changed epoch")
	}
	fresh, created, err := store.CreateSessionFollow(ctx, follow.ThreadID, follow.SourceSnapshotID)
	if err != nil || !created || fresh.ID == follow.ID {
		t.Fatal("fresh reader reused stopped identity")
	}
	snapshots, _ := store.ListSessionSnapshots(ctx, follow.ThreadID)
	rounds, _ := store.ListRounds(ctx, follow.ThreadID)
	if len(snapshots) != 1 || len(rounds) != 1 {
		t.Fatal("late reader published immutable history")
	}
}

func TestFollowCommitRollsBackCursorSnapshotRoundAndEventTogether(t *testing.T) {
	for _, target := range []struct{ table, operation string }{{"session_snapshots", "INSERT"}, {"rounds", "INSERT"}, {"session_follows", "UPDATE"}, {"events", "INSERT"}} {
		t.Run(target.table, func(t *testing.T) {
			store := openTestStore(t)
			store.DB().SetMaxOpenConns(1)
			ctx := context.Background()
			follow := followFixture(t, store)
			next, snapshot, round := followCapture(t, store, follow)
			before, _ := store.GetThread(ctx, follow.ThreadID)
			_, err := store.DB().ExecContext(ctx, fmt.Sprintf("CREATE TRIGGER fail_follow BEFORE %s ON %s BEGIN SELECT RAISE(ABORT, 'injected failure'); END", target.operation, target.table))
			if err != nil {
				t.Fatal(err)
			}
			if err = store.CommitFollowPoll(ctx, follow, next, &snapshot, &round); err == nil {
				t.Fatal("injected failure accepted")
			}
			after, _ := store.GetThread(ctx, follow.ThreadID)
			current, _ := store.GetSessionFollow(ctx, follow.ThreadID, follow.ID)
			snapshots, _ := store.ListSessionSnapshots(ctx, follow.ThreadID)
			rounds, _ := store.ListRounds(ctx, follow.ThreadID)
			if current.Cursor != "" || current.CurrentSnapshotID != follow.SourceSnapshotID || len(snapshots) != 1 || len(rounds) != 1 || before.Revision != after.Revision {
				t.Fatalf("partial commit: %#v rounds=%d snapshots=%d revision=%d", current, len(rounds), len(snapshots), after.Revision)
			}
			if _, err = store.DB().ExecContext(ctx, "DROP TRIGGER fail_follow"); err != nil {
				t.Fatal(err)
			}
			if err = store.CommitFollowPoll(ctx, follow, next, &snapshot, &round); err != nil {
				t.Fatalf("retry blocked: %v", err)
			}
		})
	}
}

func TestFollowRetryDoesNotAdvanceCursorAndRecoveryIsObservable(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	follow := followFixture(t, store)
	if err := store.RecordFollowRetry(ctx, follow, "offline"); err != nil {
		t.Fatal(err)
	}
	before, _ := store.GetThread(ctx, follow.ThreadID)
	if err := store.RecordFollowRetry(ctx, follow, "offline"); err != nil {
		t.Fatal(err)
	}
	after, _ := store.GetThread(ctx, follow.ThreadID)
	if after.Revision != before.Revision {
		t.Fatal("same retry produced repeated events")
	}
	retrying, _ := store.GetSessionFollow(ctx, follow.ThreadID, follow.ID)
	if retrying.State != "retrying" || retrying.Cursor != follow.Cursor {
		t.Fatal("failure consumed cursor")
	}
	next := retrying
	next.State, next.Reason = "active", ""
	if err := store.CommitFollowPoll(ctx, retrying, next, nil, nil); err != nil {
		t.Fatal(err)
	}
	recovered, _ := store.GetThread(ctx, follow.ThreadID)
	if recovered.Revision != before.Revision+1 {
		t.Fatal("recovery was not observable")
	}
}

func TestFollowMigrationFromV1PreservesReviewHistory(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := store.CreateSessionReviewThread(ctx, "v1 review", domain.SessionSnapshot{ID: "v1-snapshot", Source: domain.SessionRef{Provider: "codex", SessionID: "v1-source"}, Entries: []domain.SessionEntry{{ID: "v1-entry", Text: "preserved"}}})
	if err != nil {
		t.Fatal(err)
	}
	// The later-schema tables are empty; reconstruct a genuine v1 schema in this
	// dedicated fixture, then exercise the public Open upgrade path.
	if _, err = store.DB().ExecContext(ctx, "DROP TABLE share_live_snapshots; DROP TABLE share_native_live; DROP TABLE session_follows; PRAGMA user_version = 1"); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	snapshot, err := store.GetSessionSnapshot(ctx, thread.ID, "v1-snapshot")
	if err != nil || len(snapshot.Entries) != 1 || snapshot.Entries[0].Text != "preserved" {
		t.Fatal("v1 upgrade rewrote evidence")
	}
	rounds, err := store.ListRounds(ctx, thread.ID)
	if err != nil || len(rounds) != 1 || rounds[0].Kind != "session_snapshot" {
		t.Fatal("v1 upgrade rewrote Rounds")
	}
	follows, err := store.ListSessionFollows(ctx, thread.ID)
	if err != nil || len(follows) != 0 {
		t.Fatal("migration started a reader")
	}
}

func TestFirstUnchangedFollowReadIsObservableWithoutAnotherRound(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	follow := followFixture(t, store)
	before, _ := store.GetThread(ctx, follow.ThreadID)
	now := time.Now().UTC()
	next := follow
	next.Cursor, next.LastPolledAt = "first-read", &now
	if err := store.CommitFollowPoll(ctx, follow, next, nil, nil); err != nil {
		t.Fatal(err)
	}
	after, _ := store.GetThread(ctx, follow.ThreadID)
	rounds, _ := store.ListRounds(ctx, follow.ThreadID)
	if after.Revision != before.Revision+1 || len(rounds) != 1 {
		t.Fatal("first success was invisible or created a duplicate Round")
	}
	last := next
	last.Cursor = "later-unchanged-read"
	if err := store.CommitFollowPoll(ctx, next, last, nil, nil); err != nil {
		t.Fatal(err)
	}
	again, _ := store.GetThread(ctx, follow.ThreadID)
	if again.Revision != after.Revision {
		t.Fatal("unchanged heartbeat produced repeated public events")
	}
}
