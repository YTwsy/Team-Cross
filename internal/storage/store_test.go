package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"teamcross/internal/domain"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func createTestThread(t *testing.T, store *Store) domain.Thread {
	t.Helper()
	thread, err := store.CreateThread(context.Background(), domain.Thread{
		Title:          "lease test",
		RepoRoot:       "/tmp/repository",
		BaselineCommit: "abc123",
		Branch:         "main",
	})
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	return thread
}

func TestOpenConfiguresSQLite(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	var foreignKeys, busyTimeout int
	var journalMode string
	if err := store.db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 || busyTimeout != 5000 || journalMode != "wal" {
		t.Fatalf("sqlite pragmas: foreign_keys=%d busy_timeout=%d journal_mode=%s", foreignKeys, busyTimeout, journalMode)
	}
}

func TestObjectStoreIsContentAddressed(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	first, err := store.PutObject(ctx, []byte("same bytes"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.PutObject(ctx, []byte("same bytes"), "application/octet-stream")
	if err != nil {
		t.Fatal(err)
	}
	if first.Hash != second.Hash {
		t.Fatalf("same bytes got different hashes: %s != %s", first.Hash, second.Hash)
	}
	got, err := store.GetObject(ctx, first.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("same bytes")) {
		t.Fatalf("object bytes = %q", got)
	}
}

func TestRoundsAreSequentialAndImmutable(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	thread := createTestThread(t, store)
	round0, err := store.CreateRound(ctx, domain.Round{ThreadID: thread.ID, Number: 0, Kind: "capture"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateRound(ctx, domain.Round{ThreadID: thread.ID, Number: 2}); !errors.Is(err, domain.ErrRoundSequence) {
		t.Fatalf("gap error = %v", err)
	}
	round1, err := store.CreateRound(ctx, domain.Round{ThreadID: thread.ID, Number: 1, Kind: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if round1.ParentRoundID != round0.ID {
		t.Fatalf("parent = %q, want %q", round1.ParentRoundID, round0.ID)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE rounds SET summary = 'changed' WHERE id = ?`, round0.ID); err == nil {
		t.Fatal("round update unexpectedly succeeded")
	}
	got, err := store.GetRound(ctx, round0.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "" {
		t.Fatalf("immutable round summary changed to %q", got.Summary)
	}
}

func TestEventsAdvanceRevisionAndResumeBySequence(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	thread := createTestThread(t, store)
	if latest, err := store.LatestEventSeq(ctx, thread.ID); err != nil || latest != 0 {
		t.Fatalf("initial event sequence = %d, err=%v", latest, err)
	}
	first, err := store.AppendEventExpected(ctx, thread.ID, 0, "run.started", []byte(`{"run":"one"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEventExpected(ctx, thread.ID, 0, "stale", nil); !errors.Is(err, domain.ErrRevisionConflict) {
		t.Fatalf("stale append error = %v", err)
	}
	second, err := store.AppendEvent(ctx, thread.ID, "message.completed", []byte(`{"text":"done"}`))
	if err != nil {
		t.Fatal(err)
	}
	if second.Seq <= first.Seq {
		t.Fatalf("event sequence did not advance: %d then %d", first.Seq, second.Seq)
	}
	assertEventRevision(t, first, 1)
	assertEventRevision(t, second, 2)
	latest, err := store.LatestEventSeq(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest != second.Seq {
		t.Fatalf("latest event sequence = %d, want %d", latest, second.Seq)
	}
	events, err := store.EventsAfter(ctx, thread.ID, first.Seq, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Seq != second.Seq {
		t.Fatalf("resumed events = %#v", events)
	}
	assertEventRevision(t, events[0], 2)
	updated, err := store.GetThread(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 {
		t.Fatalf("thread revision = %d, want 2", updated.Revision)
	}
}

func TestAnnotationEventIncludesAdvancedRevision(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	thread := createTestThread(t, store)
	annotation, event, err := store.CreateAnnotationExpected(ctx, domain.Annotation{
		ThreadID: thread.ID,
		Body:     "persist this note",
	}, thread.Revision)
	if err != nil {
		t.Fatal(err)
	}
	assertEventRevision(t, event, 1)
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["id"] != annotation.ID || payload["body"] != annotation.Body {
		t.Fatalf("annotation event payload = %#v", payload)
	}
	persisted, err := store.EventsAfter(ctx, thread.ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 1 {
		t.Fatalf("persisted annotation events = %#v", persisted)
	}
	assertEventRevision(t, persisted[0], 1)
	updated, err := store.GetThread(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 1 {
		t.Fatalf("thread revision after annotation = %d, want 1", updated.Revision)
	}
}

func assertEventRevision(t *testing.T, event domain.Event, want int64) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("decode event payload: %v; payload=%q", err, event.Payload)
	}
	if got, ok := payload["revision"].(float64); !ok || int64(got) != want {
		t.Fatalf("event revision = %#v, want %d; payload=%#v", payload["revision"], want, payload)
	}
}

func TestControlLeaseFencingAndPreemption(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	thread := createTestThread(t, store)
	share, err := store.CreateShare(ctx, domain.Share{
		ThreadID:     thread.ID,
		SecretHash:   "secret hash",
		ServerSPKI:   "pin",
		Capabilities: []byte(`["view","send"]`),
		ExpiresAt:    time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	p1, err := store.UpsertParticipant(ctx, domain.Participant{ShareID: share.ID, Name: "one", Role: "observer"})
	if err != nil {
		t.Fatal(err)
	}
	p2, err := store.UpsertParticipant(ctx, domain.Participant{ShareID: share.ID, Name: "two", Role: "observer"})
	if err != nil {
		t.Fatal(err)
	}
	participants, err := store.ListThreadParticipants(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(participants) != 2 {
		t.Fatalf("thread participants = %#v", participants)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	lease, err := store.AcquireControl(ctx, share.ID, p1.ID, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AcquireControl(ctx, share.ID, p2.ID, now.Add(time.Second), time.Minute); !errors.Is(err, domain.ErrLeaseHeld) {
		t.Fatalf("second acquire error = %v", err)
	}
	renewed, err := store.RenewControl(ctx, share.ID, p1.ID, lease.Epoch, now.Add(20*time.Second), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !renewed.ExpiresAt.Equal(now.Add(80 * time.Second)) {
		t.Fatalf("renewed until %v", renewed.ExpiresAt)
	}
	preempted, err := store.PreemptControl(ctx, share.ID, p2.ID, now.Add(21*time.Second), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if preempted.Epoch <= lease.Epoch || preempted.ParticipantID != p2.ID {
		t.Fatalf("preempted lease = %#v", preempted)
	}
	if err := store.ValidateControl(ctx, share.ID, p1.ID, lease.Epoch, now.Add(22*time.Second)); !errors.Is(err, domain.ErrLeaseFence) {
		t.Fatalf("old fence validation = %v", err)
	}
	if err := store.ValidateControl(ctx, share.ID, p2.ID, preempted.Epoch, now.Add(22*time.Second)); err != nil {
		t.Fatalf("new fence validation = %v", err)
	}
	if err := store.ValidateCommandFence(ctx, share.ID, p2.ID, thread.Revision, preempted.Epoch, now.Add(22*time.Second)); err != nil {
		t.Fatalf("command fence validation = %v", err)
	}
	if err := store.ValidateCommandFence(ctx, share.ID, p2.ID, thread.Revision+1, preempted.Epoch, now.Add(22*time.Second)); !errors.Is(err, domain.ErrRevisionConflict) {
		t.Fatalf("stale command revision = %v", err)
	}
}

func TestCommandIdempotency(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	thread := createTestThread(t, store)
	share, err := store.CreateShare(ctx, domain.Share{
		ThreadID: thread.ID, SecretHash: "hash", ServerSPKI: "pin",
		Capabilities: []byte(`[]`), ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	command := domain.Command{ShareID: share.ID, ID: "stable-id", Kind: "send", ExpectedRevision: 3, LeaseEpoch: 9}
	claimed, created, err := store.ClaimCommand(ctx, command)
	if err != nil || !created || claimed.Status != "running" {
		t.Fatalf("first claim: command=%#v created=%v err=%v", claimed, created, err)
	}
	duplicate, created, err := store.ClaimCommand(ctx, command)
	if err != nil || created || duplicate.ID != command.ID {
		t.Fatalf("duplicate: command=%#v created=%v err=%v", duplicate, created, err)
	}
	conflict := command
	conflict.Kind = "interrupt"
	if _, _, err := store.ClaimCommand(ctx, conflict); !errors.Is(err, domain.ErrCommandConflict) {
		t.Fatalf("conflicting id error = %v", err)
	}
	finished, err := store.CompleteCommand(ctx, share.ID, command.ID, "completed", "", "", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != "completed" || finished.CompletedAt.IsZero() {
		t.Fatalf("completed command = %#v", finished)
	}
	again, err := store.CompleteCommand(ctx, share.ID, command.ID, "failed", "", "late", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != "completed" {
		t.Fatalf("duplicate completion overwrote result: %#v", again)
	}
}
