package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"teamcross/internal/domain"
)

func nativeLiveFollowFixture(t *testing.T, store *Store) domain.SessionFollow {
	t.Helper()
	ctx := context.Background()
	snapshot := domain.SessionSnapshot{ID: newID(), Source: domain.SessionRef{Provider: "codex", SessionID: "native-only", IdentityKind: "thread.id", Surface: "synthetic", ProviderVersion: "test", Cwd: "/private/source/must-not-be-read", Title: "private title", NativeIDs: map[string]string{"secret": "private native id"}}, Capabilities: domain.SessionCapabilities{Read: true, Follow: true, Open: true, Resume: true, TakeControl: true, Reason: "private capability reason"}, Entries: []domain.SessionEntry{{ID: "message", Kind: "message", Text: "old message"}, {ID: "tool", Kind: "tool", Text: "private tool"}, {ID: "notice", Kind: "notice", Text: "private notice"}, {ID: "unknown", Kind: "future-kind", Text: "unknown private entry"}}, Truncated: true, Warnings: []string{"private raw warning"}}
	thread, err := store.CreateSessionReviewThread(ctx, "Native live test", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	follow, _, err := store.CreateSessionFollow(ctx, thread.ID, snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	next := follow
	now := time.Now().UTC()
	next.Cursor, next.LastPolledAt, next.UpdatedAt = "private-reader-cursor", &now, now
	if err = store.CommitFollowPoll(ctx, follow, next, nil, nil); err != nil {
		t.Fatal(err)
	}
	return next
}

func nativeLiveShareFixture(t *testing.T, store *Store, follow domain.SessionFollow) (domain.Share, domain.NativeLiveBinding) {
	t.Helper()
	share, err := store.CreateShare(context.Background(), domain.Share{ThreadID: follow.ThreadID, SecretHash: "not exported", ServerSPKI: "test", Capabilities: []byte(`["view","annotate"]`), ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	binding := domain.NativeLiveBinding{FollowID: follow.ID, FollowEpoch: follow.Epoch, Source: follow.Source, EntryKinds: []string{"message"}, ExpectedSnapshotID: follow.CurrentSnapshotID}
	return share, binding
}

func saveNativeLive(t *testing.T, store *Store, share domain.Share, binding domain.NativeLiveBinding) {
	t.Helper()
	if err := store.SaveScopedShareProjection(context.Background(), share.ID, []byte(`{"static":"frozen"}`), &binding); err != nil {
		t.Fatal(err)
	}
}

func TestNativeLiveProjectionFiltersAndSanitizesWithoutMutatingSource(t *testing.T) {
	store := openTestStore(t)
	follow := nativeLiveFollowFixture(t, store)
	raw, err := store.GetSessionSnapshot(context.Background(), follow.ThreadID, follow.CurrentSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(raw)
	projected, fully, err := domain.ProjectNativeSnapshot(raw, []string{"notice", "message"})
	if err != nil || fully || len(projected.Entries) != 2 || projected.Entries[0].ID != "message" || projected.Entries[1].ID != "notice" {
		t.Fatalf("projection: %#v fully=%v err=%v", projected, fully, err)
	}
	encoded, _ := json.Marshal(projected)
	for _, secret := range []string{"private raw warning", "private capability reason", "private title", "private native id", "/private/source", "future-kind", "private tool"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("private metadata escaped: %s", secret)
		}
	}
	if projected.Source.Provider != raw.Source.Provider || projected.Source.SessionID != raw.Source.SessionID || projected.Source.ProviderVersion != raw.Source.ProviderVersion || projected.Capabilities.Follow || projected.Capabilities.TakeControl || !projected.Capabilities.Read || !projected.Truncated || len(projected.Warnings) != 1 {
		t.Fatal("projection lost source identity or retained native capabilities")
	}
	after, _ := json.Marshal(raw)
	if string(before) != string(after) {
		t.Fatal("projection mutated private source")
	}
	if _, fully, err := domain.ProjectNativeSnapshot(raw, []string{"message", "tool", "notice"}); err != nil || fully {
		t.Fatal("unknown source kind was implicitly authorized")
	}
	for _, kinds := range [][]string{nil, {}, {"message", "message"}, {"future-kind"}, {"message", "future-kind"}} {
		if _, _, err := domain.ProjectNativeSnapshot(raw, kinds); err == nil {
			t.Fatalf("invalid kind grant accepted: %v", kinds)
		}
	}
	for _, mutate := range []func(*domain.SessionSnapshot){func(s *domain.SessionSnapshot) { s.ID = "" }, func(s *domain.SessionSnapshot) { s.ThreadID = "" }, func(s *domain.SessionSnapshot) { s.CapturedAt = time.Time{} }, func(s *domain.SessionSnapshot) { s.Source.SessionID = "" }, func(s *domain.SessionSnapshot) { s.Source.Provider = "unknown" }, func(s *domain.SessionSnapshot) {
		s.Entries = []domain.SessionEntry{{ID: "same", Kind: "message"}, {ID: "same", Kind: "tool"}}
	}} {
		invalid := raw
		mutate(&invalid)
		if _, _, err := domain.ProjectNativeSnapshot(invalid, []string{"message"}); err == nil {
			t.Fatal("invalid snapshot shape accepted")
		}
	}
}

func TestNativeLiveSeedPollAndOldAnchorsSurviveRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dir := t.TempDir()
	store, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	store.DB().SetMaxOpenConns(1)
	follow := nativeLiveFollowFixture(t, store)
	share, binding := nativeLiveShareFixture(t, store, follow)
	var startSeq int64
	if err = store.DB().QueryRowContext(ctx, "SELECT MAX(seq) FROM events WHERE thread_id=?", follow.ThreadID).Scan(&startSeq); err != nil {
		t.Fatal(err)
	}
	saveNativeLive(t, store, share, binding)
	initial, fully, err := store.GetLiveShareSnapshot(ctx, share.ID, follow.CurrentSnapshotID)
	if err != nil || fully || len(initial.Entries) != 1 || initial.Entries[0].Text != "old message" {
		t.Fatalf("seed: %#v %v %v", initial, fully, err)
	}
	next, snapshot, round := followCapture(t, store, follow)
	snapshot.Entries = []domain.SessionEntry{{ID: "message", Kind: "message", Text: "new message"}, {ID: "private-new-tool", Kind: "tool", Text: "never shared"}}
	if err = store.CommitFollowPoll(ctx, follow, next, &snapshot, &round); err != nil {
		t.Fatal(err)
	}
	state, err := store.GetLiveShareState(ctx, share.ID)
	if err != nil || state.Count != 2 || state.LatestSnapshotID != snapshot.ID || state.State != "active" || state.StartSeq != startSeq {
		t.Fatalf("live state: %#v %v", state, err)
	}
	encoded, _ := json.Marshal(state)
	for _, secret := range []string{"private-reader-cursor", "/private/source", "private title", "private native id"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("state leaked %s", secret)
		}
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	store.DB().SetMaxOpenConns(1)
	old, _, err := store.GetLiveShareSnapshot(ctx, share.ID, initial.ID)
	if err != nil || !reflect.DeepEqual(old, initial) {
		t.Fatalf("old anchor changed: %#v %v", old, err)
	}
	current, _, err := store.GetLiveShareSnapshot(ctx, share.ID, snapshot.ID)
	if err != nil || len(current.Entries) != 1 || current.Entries[0].ID != "message" || current.Entries[0].Text != "new message" {
		t.Fatal("same entry ID in a new snapshot overwrote the old anchor")
	}
	reopened, err := store.GetLiveShareState(ctx, share.ID)
	if err != nil || !reflect.DeepEqual(state, reopened) {
		t.Fatal("binding, byte accounting or start sequence lost on reopen")
	}
	for _, statement := range []string{"UPDATE share_live_snapshots SET fully_shared=1", "DELETE FROM share_live_snapshots", "UPDATE share_native_live SET follow_epoch=99", "UPDATE share_native_live SET start_seq=0"} {
		if _, err = store.DB().ExecContext(ctx, statement); err == nil {
			t.Fatalf("immutable grant was changed: %s", statement)
		}
	}
	var count int
	if err = store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM agent_runs").Scan(&count); err != nil || count != 0 {
		t.Fatal("live sharing created an Agent Run")
	}
}

func TestNativeLiveCreationFencesAndRollsBack(t *testing.T) {
	for _, scenario := range []string{"unpolled", "retrying", "stopped", "epoch", "source", "version", "surface", "thread", "stale", "revoked", "expired", "empty-kinds"} {
		t.Run(scenario, func(t *testing.T) {
			store := openTestStore(t)
			ctx := context.Background()
			follow := nativeLiveFollowFixture(t, store)
			share, binding := nativeLiveShareFixture(t, store, follow)
			want := domain.ErrLiveShareFence
			switch scenario {
			case "unpolled":
				if _, err := store.StopSessionFollow(ctx, follow.ThreadID, follow.ID); err != nil {
					t.Fatal(err)
				}
				fresh, _, err := store.CreateSessionFollow(ctx, follow.ThreadID, follow.CurrentSnapshotID)
				if err != nil {
					t.Fatal(err)
				}
				binding.FollowID, binding.FollowEpoch = fresh.ID, fresh.Epoch
			case "retrying":
				if err := store.RecordFollowRetry(ctx, follow, "private error"); err != nil {
					t.Fatal(err)
				}
			case "stopped":
				if _, err := store.StopSessionFollow(ctx, follow.ThreadID, follow.ID); err != nil {
					t.Fatal(err)
				}
			case "epoch":
				binding.FollowEpoch++
			case "source":
				binding.Source.SessionID = "other-native"
			case "version":
				binding.Source.ProviderVersion = "other-version"
			case "surface":
				binding.Source.Surface = "other-surface"
			case "thread":
				other := createTestThread(t, store)
				if _, err := store.DB().ExecContext(ctx, "UPDATE shares SET thread_id=? WHERE id=?", other.ID, share.ID); err != nil {
					t.Fatal(err)
				}
			case "stale":
				binding.ExpectedSnapshotID = "not-current"
				want = domain.ErrLiveShareStale
			case "revoked":
				if err := store.RevokeShare(ctx, share.ID, time.Now()); err != nil {
					t.Fatal(err)
				}
			case "expired":
				if _, err := store.DB().ExecContext(ctx, "UPDATE shares SET expires_at=1 WHERE id=?", share.ID); err != nil {
					t.Fatal(err)
				}
			case "empty-kinds":
				binding.EntryKinds = nil
				want = nil
			}
			err := store.SaveScopedShareProjection(ctx, share.ID, []byte(`{}`), &binding)
			if err == nil || (want != nil && !errors.Is(err, want)) {
				t.Fatalf("%s: %v, want %v", scenario, err, want)
			}
			for _, table := range []string{"share_projections", "share_native_live", "share_live_snapshots"} {
				var count int
				if err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 0 {
					t.Fatalf("failed grant left %s: %d %v", table, count, err)
				}
			}
		})
	}
}

func TestNativeLiveSeedAndAdmissionRollbackTogether(t *testing.T) {
	for _, operation := range []string{"seed", "poll"} {
		for _, table := range []string{"objects", "share_projections", "share_live_snapshots", "share_native_live", "events"} {
			if operation == "seed" && table == "events" {
				continue
			}
			if operation == "poll" && table == "share_projections" {
				continue
			}
			t.Run(operation+"/"+table, func(t *testing.T) {
				store := openTestStore(t)
				store.DB().SetMaxOpenConns(1)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				follow := nativeLiveFollowFixture(t, store)
				share, binding := nativeLiveShareFixture(t, store, follow)
				if operation == "poll" {
					saveNativeLive(t, store, share, binding)
				}
				next, snapshot, round := followCapture(t, store, follow)
				before, _ := store.GetThread(ctx, follow.ThreadID)
				sqlOperation := "INSERT"
				if table == "share_native_live" && operation == "poll" {
					sqlOperation = "UPDATE"
				}
				condition := ""
				if table == "objects" {
					// Fail the transactional projection object registration, not
					// the private source PutObject before CommitFollowPoll begins.
					condition = " WHEN NEW.mime='application/vnd.teamcross.native-live-projection+json'"
				}
				trigger := fmt.Sprintf("CREATE TRIGGER fail_live BEFORE %s ON %s%s BEGIN SELECT RAISE(ABORT, 'injected live failure'); END", sqlOperation, table, condition)
				if _, err := store.DB().ExecContext(ctx, trigger); err != nil {
					t.Fatal(err)
				}
				var err error
				if operation == "seed" {
					err = store.SaveScopedShareProjection(ctx, share.ID, []byte(`{}`), &binding)
				} else {
					err = store.CommitFollowPoll(ctx, follow, next, &snapshot, &round)
				}
				if err == nil {
					t.Fatal("injected failure accepted")
				}
				current, _ := store.GetSessionFollow(ctx, follow.ThreadID, follow.ID)
				after, _ := store.GetThread(ctx, follow.ThreadID)
				if current.CurrentSnapshotID != follow.CurrentSnapshotID || current.Cursor != follow.Cursor || before.Revision != after.Revision {
					t.Fatal("failure partially advanced Follow")
				}
				var count int
				if err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM share_live_snapshots").Scan(&count); err != nil {
					t.Fatal(err)
				}
				want := 0
				if operation == "poll" {
					want = 1
				}
				if count != want {
					t.Fatalf("partial live window: count=%d want=%d", count, want)
				}
				if _, err := store.DB().ExecContext(ctx, "DROP TRIGGER fail_live"); err != nil {
					t.Fatal(err)
				}
				if operation == "seed" {
					err = store.SaveScopedShareProjection(ctx, share.ID, []byte(`{}`), &binding)
				} else {
					err = store.CommitFollowPoll(ctx, follow, next, &snapshot, &round)
				}
				if err != nil {
					t.Fatalf("transaction did not recover: %v", err)
				}
			})
		}
	}
}

func TestNativeLiveShareCreationRacesPollWithoutMissingWindow(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	for i := 0; i < 12; i++ {
		follow := nativeLiveFollowFixture(t, store)
		share, binding := nativeLiveShareFixture(t, store, follow)
		next, snapshot, round := followCapture(t, store, follow)
		start := make(chan struct{})
		var saved, polled error
		var wait sync.WaitGroup
		wait.Add(2)
		go func() {
			defer wait.Done()
			<-start
			saved = store.SaveScopedShareProjection(ctx, share.ID, []byte(`{}`), &binding)
		}()
		go func() {
			defer wait.Done()
			<-start
			polled = store.CommitFollowPoll(ctx, follow, next, &snapshot, &round)
		}()
		close(start)
		wait.Wait()
		if polled != nil {
			t.Fatalf("poll race failed: %v", polled)
		}
		if saved != nil && !errors.Is(saved, domain.ErrLiveShareStale) {
			t.Fatalf("grant race failed without preview fence: %v", saved)
		}
		if errors.Is(saved, domain.ErrLiveShareStale) {
			binding.ExpectedSnapshotID = snapshot.ID
			saveNativeLive(t, store, share, binding)
		}
		state, err := store.GetLiveShareState(ctx, share.ID)
		if err != nil || state.LatestSnapshotID != snapshot.ID {
			t.Fatalf("race lost window: %#v %v", state, err)
		}
		want := int64(2)
		if saved != nil {
			want = 1
		}
		if state.Count != want {
			t.Fatalf("race count=%d want=%d", state.Count, want)
		}
		if has, err := store.HasLiveShareSnapshot(ctx, share.ID, snapshot.ID); err != nil || !has {
			t.Fatalf("race window unauthorized: %v", err)
		}
	}
}

func TestNativeLiveStopRetryRevocationAndSeparateFollowDoNotExpand(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	follow := nativeLiveFollowFixture(t, store)
	share, binding := nativeLiveShareFixture(t, store, follow)
	saveNativeLive(t, store, share, binding)
	if err := store.RecordFollowRetry(ctx, follow, "private error path /secret"); err != nil {
		t.Fatal(err)
	}
	state, err := store.GetLiveShareState(ctx, share.ID)
	if err != nil || state.State != "retrying" || strings.Contains(state.Reason, "private") {
		t.Fatalf("retry state: %#v %v", state, err)
	}
	retrying, _ := store.GetSessionFollow(ctx, follow.ThreadID, follow.ID)
	recovered := retrying
	recovered.State, recovered.Reason, recovered.Cursor = "active", "", "recovered-private-cursor"
	if err := store.CommitFollowPoll(ctx, retrying, recovered, nil, nil); err != nil {
		t.Fatal(err)
	}
	state, err = store.GetLiveShareState(ctx, share.ID)
	if err != nil || state.State != "active" || state.Count != 1 {
		t.Fatal("reconnect duplicated a window")
	}
	if _, err := store.StopSessionFollow(ctx, follow.ThreadID, follow.ID); err != nil {
		t.Fatal(err)
	}
	state, err = store.GetLiveShareState(ctx, share.ID)
	if err != nil || state.State != "stopped" {
		t.Fatal("stopped grant stayed live")
	}
	fresh, _, err := store.CreateSessionFollow(ctx, follow.ThreadID, follow.CurrentSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	next, snapshot, round := followCapture(t, store, fresh)
	if err = store.CommitFollowPoll(ctx, fresh, next, &snapshot, &round); err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.GetLiveShareSnapshot(ctx, share.ID, snapshot.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("new Follow inherited old grant: %v", err)
	}
	if old, _, err := store.GetLiveShareSnapshot(ctx, share.ID, binding.ExpectedSnapshotID); err != nil || old.ID != binding.ExpectedSnapshotID {
		t.Fatal("Follow stop erased old anchor")
	}
	if has, err := store.HasLiveShareSnapshot(ctx, share.ID, snapshot.ID); err != nil || has {
		t.Fatal("unadmitted snapshot authorized")
	}
	if err = store.RevokeShare(ctx, share.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.GetLiveShareSnapshot(ctx, share.ID, binding.ExpectedSnapshotID); !errors.Is(err, domain.ErrLiveShareFence) {
		t.Fatal("revoked Share returned an old anchor")
	}
	if has, err := store.HasLiveShareSnapshot(ctx, share.ID, binding.ExpectedSnapshotID); err != nil || has {
		t.Fatal("revoked membership authorized")
	}
	state, err = store.GetLiveShareState(ctx, share.ID)
	if err != nil || state.State != "revoked" {
		t.Fatal("revocation state not visible")
	}
}

func TestNativeLiveWindowBudgetPreservesHistoryAndFollowProgress(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	follow := nativeLiveFollowFixture(t, store)
	share, binding := nativeLiveShareFixture(t, store, follow)
	saveNativeLive(t, store, share, binding)
	lastAdmitted := follow.CurrentSnapshotID
	for i := 1; i <= MaxNativeLiveWindows; i++ {
		next, snapshot, round := followCapture(t, store, follow)
		next.Cursor = fmt.Sprintf("cursor-%d", i)
		if err := store.CommitFollowPoll(ctx, follow, next, &snapshot, &round); err != nil {
			t.Fatal(err)
		}
		follow = next
		if i < MaxNativeLiveWindows {
			lastAdmitted = snapshot.ID
		}
	}
	state, err := store.GetLiveShareState(ctx, share.ID)
	if err != nil || state.Count != MaxNativeLiveWindows || state.State != "limited" || state.LatestSnapshotID != lastAdmitted {
		t.Fatalf("window budget: %#v %v", state, err)
	}
	if _, _, err := store.GetLiveShareSnapshot(ctx, share.ID, follow.CurrentSnapshotID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("overflow window admitted")
	}
	if _, _, err := store.GetLiveShareSnapshot(ctx, share.ID, binding.ExpectedSnapshotID); err != nil {
		t.Fatal("budget dropped first anchor")
	}
	next, snapshot, round := followCapture(t, store, follow)
	if err := store.CommitFollowPoll(ctx, follow, next, &snapshot, &round); err != nil {
		t.Fatal("limited Share blocked Follow")
	}
	current, err := store.GetSessionFollow(ctx, follow.ThreadID, follow.ID)
	if err != nil || current.CurrentSnapshotID != snapshot.ID {
		t.Fatal("limited Share stopped native observation")
	}
	var sum int64
	if err := store.DB().QueryRowContext(ctx, "SELECT SUM(projection_bytes) FROM share_live_snapshots WHERE share_id=?", share.ID).Scan(&sum); err != nil || sum != state.Bytes {
		t.Fatal("projection byte accounting is not exact")
	}
}

func TestNativeLiveByteBudgetAndExpirationStopOnlyNewAdmission(t *testing.T) {
	for _, scenario := range []string{"bytes", "expired", "revoked"} {
		t.Run(scenario, func(t *testing.T) {
			store := openTestStore(t)
			ctx := context.Background()
			follow := nativeLiveFollowFixture(t, store)
			share, binding := nativeLiveShareFixture(t, store, follow)
			saveNativeLive(t, store, share, binding)
			switch scenario {
			case "bytes":
				// Exercise the exact 64 MiB boundary without a large duplicated
				// fixture; ordinary admissions above verify the persisted sum.
				if _, err := store.DB().ExecContext(ctx, "UPDATE share_native_live SET projection_bytes=? WHERE share_id=?", MaxNativeLiveProjectionBytes-1, share.ID); err != nil {
					t.Fatal(err)
				}
			case "expired":
				if _, err := store.DB().ExecContext(ctx, "UPDATE shares SET expires_at=1 WHERE id=?", share.ID); err != nil {
					t.Fatal(err)
				}
			case "revoked":
				if err := store.RevokeShare(ctx, share.ID, time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			next, snapshot, round := followCapture(t, store, follow)
			if err := store.CommitFollowPoll(ctx, follow, next, &snapshot, &round); err != nil {
				t.Fatal(err)
			}
			state, err := store.GetLiveShareState(ctx, share.ID)
			want := scenario
			if scenario == "bytes" {
				want = "limited"
			}
			if err != nil || state.Count != 1 || state.State != want || state.LatestSnapshotID != binding.ExpectedSnapshotID {
				t.Fatalf("stopped disclosure: %#v %v", state, err)
			}
			if has, err := store.HasLiveShareSnapshot(ctx, share.ID, snapshot.ID); err != nil || has {
				t.Fatal("unauthorized future window appeared")
			}
			_, _, err = store.GetLiveShareSnapshot(ctx, share.ID, binding.ExpectedSnapshotID)
			if scenario == "bytes" && err != nil {
				t.Fatal("byte budget erased old anchor")
			}
			if scenario != "bytes" && !errors.Is(err, domain.ErrLiveShareFence) {
				t.Fatal("inactive Share returned old data")
			}
		})
	}
}

func TestNativeLiveHashValidationAndMembershipOnlyNotification(t *testing.T) {
	for _, corrupt := range []string{"source", "projection", "private-after-seed"} {
		t.Run(corrupt, func(t *testing.T) {
			store := openTestStore(t)
			ctx := context.Background()
			follow := nativeLiveFollowFixture(t, store)
			share, binding := nativeLiveShareFixture(t, store, follow)
			var hash string
			if corrupt == "private-after-seed" {
				saveNativeLive(t, store, share, binding)
			}
			if corrupt != "projection" {
				if err := store.DB().QueryRowContext(ctx, "SELECT object_hash FROM session_snapshots WHERE id=?", binding.ExpectedSnapshotID).Scan(&hash); err != nil {
					t.Fatal(err)
				}
			} else {
				saveNativeLive(t, store, share, binding)
				if err := store.DB().QueryRowContext(ctx, "SELECT projection_object FROM share_live_snapshots WHERE share_id=?", share.ID).Scan(&hash); err != nil {
					t.Fatal(err)
				}
			}
			path, err := store.objects.Path(hash)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data[len(data)-2] ^= 1
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if corrupt == "source" {
				if err := store.SaveScopedShareProjection(ctx, share.ID, []byte(`{}`), &binding); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
					t.Fatalf("bad source hash accepted: %v", err)
				}
			} else if corrupt == "projection" {
				if _, _, err := store.GetLiveShareSnapshot(ctx, share.ID, binding.ExpectedSnapshotID); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
					t.Fatalf("bad projection hash accepted: %v", err)
				}
				// SSE membership is intentionally independent of CAS body IO.
				if has, err := store.HasLiveShareSnapshot(ctx, share.ID, binding.ExpectedSnapshotID); err != nil || !has {
					t.Fatal("notification path unnecessarily reads the corrupt CAS body")
				}
			} else {
				// A durable anchor reads only its authorized projection. It does
				// not reopen the private transcript even to check membership.
				if snapshot, _, err := store.GetLiveShareSnapshot(ctx, share.ID, binding.ExpectedSnapshotID); err != nil || len(snapshot.Entries) != 1 || snapshot.Entries[0].Text != "old message" {
					t.Fatalf("shared read depended on private CAS: %#v %v", snapshot, err)
				}
			}
		})
	}
}

func TestNativeLiveStaticProjectionDoesNotSubscribe(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	follow := nativeLiveFollowFixture(t, store)
	share, _ := nativeLiveShareFixture(t, store, follow)
	if err := store.SaveShareProjection(ctx, share.ID, []byte(`{"static":"frozen"}`)); err != nil {
		t.Fatal(err)
	}
	next, snapshot, round := followCapture(t, store, follow)
	if err := store.CommitFollowPoll(ctx, follow, next, &snapshot, &round); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetLiveShareState(ctx, share.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("static Share acquired live scope")
	}
	if has, err := store.HasLiveShareSnapshot(ctx, share.ID, snapshot.ID); err != nil || has {
		t.Fatal("static Share widened after Follow update")
	}
	data, err := store.GetShareProjection(ctx, share.ID)
	if err != nil || string(data) != `{"static":"frozen"}` {
		t.Fatal("static projection changed")
	}
}

func TestNativeLiveMigrationFromV2PreservesFollowAndStaticShare(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	follow := nativeLiveFollowFixture(t, store)
	share, _ := nativeLiveShareFixture(t, store, follow)
	if err := store.SaveShareProjection(ctx, share.ID, []byte(`{"static":"v2-frozen"}`)); err != nil {
		t.Fatal(err)
	}
	// Reconstruct a genuine v2 fixture with a persisted reader checkpoint, not
	// an empty schema, and check migration never enables a new disclosure.
	if _, err := store.DB().ExecContext(ctx, "DROP TABLE share_live_snapshots; DROP TABLE share_native_live; PRAGMA user_version=2"); err != nil {
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
	reopened, err := store.GetSessionFollow(ctx, follow.ThreadID, follow.ID)
	if err != nil || !reflect.DeepEqual(reopened, follow) {
		t.Fatalf("v2 Follow checkpoint changed: %#v %v", reopened, err)
	}
	data, err := store.GetShareProjection(ctx, share.ID)
	if err != nil || string(data) != `{"static":"v2-frozen"}` {
		t.Fatal("v2 static disclosure changed")
	}
	if _, err := store.GetLiveShareState(ctx, share.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("v2 Share gained live scope during migration")
	}
	var version int
	if err := store.DB().QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != schemaVersion {
		t.Fatal("v3 migration version not persisted")
	}
}
