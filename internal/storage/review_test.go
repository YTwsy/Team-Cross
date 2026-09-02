package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"teamcross/internal/domain"
)

func TestOpenRejectsFutureSchemaBeforeLegacyInitialization(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "teamcross.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf("CREATE TABLE future_only(value TEXT); INSERT INTO future_only VALUES('preserved'); PRAGMA user_version = %d;", schemaVersion+1)); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, dir)
	if err == nil || !strings.Contains(err.Error(), "newer than supported") {
		if store != nil {
			store.Close()
		}
		t.Fatalf("future schema was not rejected: %v", err)
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count, version int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name <> 'future_only'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("future database received legacy tables: count=%d err=%v", count, err)
	}
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != schemaVersion+1 {
		t.Fatalf("future version changed: version=%d err=%v", version, err)
	}
	var value string
	if err := db.QueryRowContext(ctx, "SELECT value FROM future_only").Scan(&value); err != nil || value != "preserved" {
		t.Fatalf("future data changed: value=%q err=%v", value, err)
	}
}

func TestCreateSessionReviewThreadPreservesReadOnlySnapshot(t *testing.T) {
	store := openTestStore(t)
	store.DB().SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	snapshot := domain.SessionSnapshot{
		ID: "snapshot", ThreadID: "untrusted-old-thread", CapturedAt: time.Date(2026, 9, 3, 12, 30, 0, 12345, time.UTC),
		Source:    domain.SessionRef{Provider: "codex", ProviderVersion: "test-version", SessionID: "native-thread", IdentityKind: "thread.id", Surface: "desktop", Title: "Native title", Cwd: "/missing/untrusted/provider/path", NativeIDs: map[string]string{"sessionTreeId": "native-tree"}},
		Entries:   []domain.SessionEntry{{ID: "entry", Kind: "message", Role: "assistant", Text: "Untrusted history", SourceID: "native-item", TurnID: "native-turn"}},
		Truncated: true, Warnings: []string{"partial history"}, Capabilities: domain.SessionCapabilities{Read: true, Reason: "read only"},
	}
	thread, err := store.CreateSessionReviewThread(ctx, "Review title", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if thread.ID == "" || thread.Title != "Review title" || !thread.ReadOnly || thread.Revision != 1 || thread.RepoRoot != "" || thread.BaselineCommit != "" || thread.Branch != "" || thread.WorktreePath != "" {
		t.Fatalf("review acquired execution state: %#v", thread)
	}
	got, err := store.GetSessionSnapshot(ctx, thread.ID, snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.ThreadID = thread.ID
	if !reflect.DeepEqual(got, snapshot) {
		t.Fatalf("source snapshot metadata changed:\ngot %#v\nwant %#v", got, snapshot)
	}
	rounds, err := store.ListRounds(ctx, thread.ID)
	if err != nil || len(rounds) != 1 {
		t.Fatalf("initial Round missing: %#v %v", rounds, err)
	}
	round := rounds[0]
	if round.Number != 0 || round.Kind != "session_snapshot" || round.ParentRoundID != "" || round.SnapshotID != "" || round.AgentRunID != "" {
		t.Fatalf("invalid read-only Round: %#v", round)
	}
	manifestData, err := store.GetObject(ctx, round.ManifestObject)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		SnapshotIDs   []string `json:"sessionSnapshotIds"`
		ContextOrigin string   `json:"contextOrigin"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil || !reflect.DeepEqual(manifest.SnapshotIDs, []string{snapshot.ID}) || !strings.Contains(manifest.ContextOrigin, "untrusted") {
		t.Fatalf("initial manifest lost source reference: %s %v", manifestData, err)
	}
	events, err := store.EventsAfter(ctx, thread.ID, 0, 10)
	if err != nil || len(events) != 1 || events[0].Type != "session.imported" {
		t.Fatalf("initial import event missing: %#v %v", events, err)
	}
	var payload struct {
		SnapshotID string `json:"snapshotId"`
		RoundID    string `json:"roundId"`
		Revision   int64  `json:"revision"`
	}
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil || payload.SnapshotID != snapshot.ID || payload.RoundID != round.ID || payload.Revision != 1 {
		t.Fatalf("invalid import event: %s %v", events[0].Payload, err)
	}
	for _, table := range []string{"agent_runs", "git_snapshots"} {
		var count int
		if err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("read-only import created %s: %d %v", table, count, err)
		}
	}
}

func TestCreateSessionReviewThreadRollsBackEveryRecordFailure(t *testing.T) {
	for _, failedTable := range []string{"objects", "session_snapshots", "rounds", "events"} {
		t.Run(failedTable, func(t *testing.T) {
			store := openTestStore(t)
			store.DB().SetMaxOpenConns(1)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			trigger := fmt.Sprintf("CREATE TRIGGER fail_session_review BEFORE INSERT ON %s BEGIN SELECT RAISE(ABORT, 'injected review failure'); END", failedTable)
			if _, err := store.DB().ExecContext(ctx, trigger); err != nil {
				t.Fatal(err)
			}
			snapshot := domain.SessionSnapshot{ID: "snapshot", Source: domain.SessionRef{Provider: "codex", SessionID: "native", Cwd: "/must/not/be/opened"}, Entries: []domain.SessionEntry{{ID: "entry", Kind: "message", Text: "reference"}}}
			if thread, err := store.CreateSessionReviewThread(ctx, "Review", snapshot); err == nil || thread.ID != "" || !strings.Contains(err.Error(), "injected review failure") {
				t.Fatalf("injected %s failure was not returned: %#v %v", failedTable, thread, err)
			}
			for _, table := range []string{"threads", "session_snapshots", "rounds", "events", "agent_runs", "git_snapshots"} {
				var count int
				if err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 0 {
					t.Fatalf("failed import left partial %s: %d %v", table, count, err)
				}
			}
			if _, err := store.DB().ExecContext(ctx, "DROP TRIGGER fail_session_review"); err != nil {
				t.Fatal(err)
			}
			if _, err := store.CreateSessionReviewThread(ctx, "Retry", snapshot); err != nil {
				t.Fatalf("failed transaction prevented retry: %v", err)
			}
		})
	}
}

func TestReviewMigrationPreservesV0AndReopens(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "teamcross.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, schema); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, "INSERT INTO threads(id,title,repo_root,created_at,updated_at) VALUES('old','Original','/source',1,1)"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, "INSERT INTO rounds(id,thread_id,round_number,kind,summary,created_at) VALUES('round','old',0,'capture','Original checkpoint',1)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	store, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	round, err := store.GetRound(ctx, "round")
	if err != nil || round.Summary != "Original checkpoint" {
		t.Fatalf("migration changed old Round: %#v %v", round, err)
	}
	var version int
	if err = store.DB().QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != schemaVersion {
		t.Fatal("schema version not advanced")
	}
	store.Close()
	store, err = Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.GetRound(ctx, "round"); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotAndAnchorPersistWithoutMutableHistory(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	thread := createTestThread(t, store)
	snapshot, err := store.CreateSessionSnapshot(ctx, domain.SessionSnapshot{ThreadID: thread.ID, Source: domain.SessionRef{Provider: "codex", SessionID: "native"}, CapturedAt: time.Now().UTC(), Entries: []domain.SessionEntry{{ID: "entry", Kind: "message", Text: "Original"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, "UPDATE session_snapshots SET captured_at=1 WHERE id=?", snapshot.ID); err == nil {
		t.Fatal("snapshot row was mutable")
	}
	if _, err = store.DB().ExecContext(ctx, "DELETE FROM session_snapshots WHERE id=?", snapshot.ID); err == nil {
		t.Fatal("snapshot row was deletable")
	}
	_, _, err = store.CreateAnnotationExpected(ctx, domain.Annotation{ThreadID: thread.ID, Body: "Review", Target: &domain.AnnotationTarget{SnapshotID: snapshot.ID, EntryID: "entry"}}, thread.Revision)
	if err != nil {
		t.Fatal(err)
	}
	annotations, err := store.ListAnnotations(ctx, thread.ID)
	if err != nil || len(annotations) != 1 || annotations[0].Target.EntryID != "entry" {
		t.Fatalf("anchor lost: %#v %v", annotations, err)
	}
}

func TestListSessionSnapshotsWithOneConnection(t *testing.T) {
	store := openTestStore(t)
	store.DB().SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	thread := createTestThread(t, store)
	_, err := store.CreateSessionSnapshot(ctx, domain.SessionSnapshot{ThreadID: thread.ID, Entries: []domain.SessionEntry{{ID: "entry", Text: "test"}}})
	if err != nil {
		t.Fatal(err)
	}
	snapshots, err := store.ListSessionSnapshots(ctx, thread.ID)
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("nested connection deadlock: %v %v", snapshots, err)
	}
}
