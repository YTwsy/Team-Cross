package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"teamcross/internal/domain"
)

func remoteAdmissionFixture(t *testing.T) (*Store, domain.Thread, domain.Share, domain.Participant, domain.ControlLease, domain.Command) {
	t.Helper()
	store := openTestStore(t)
	ctx := context.Background()
	thread := createTestThread(t, store)
	share, err := store.CreateShare(ctx, domain.Share{ThreadID: thread.ID, SecretHash: "synthetic", ServerSPKI: "synthetic", Capabilities: []byte(`["view","annotate","send","steer","interrupt"]`), ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	participant, err := store.UpsertParticipant(ctx, domain.Participant{ShareID: share.ID, Name: "synthetic", Role: "observer"})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.AcquireControl(ctx, share.ID, participant.ID, time.Time{}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	object, err := store.PutObject(ctx, []byte(`{"synthetic":true}`), "application/json")
	if err != nil {
		t.Fatal(err)
	}
	command := domain.Command{ID: "synthetic-command", ShareID: share.ID, Kind: "agent.send", ExpectedRevision: thread.Revision, LeaseEpoch: lease.Epoch, RequestObject: object.Hash}
	return store, thread, share, participant, lease, command
}

func TestRemoteAdmissionPermissionAndLeaseMatrix(t *testing.T) {
	for _, test := range []struct {
		kind       string
		lease      bool
		capability string
		allowed    bool
	}{
		{"annotate", false, "annotate", true}, {"control.request", false, "send", true},
		{"control.renew", false, "send", false}, {"control.release", false, "send", false},
		{"control.renew", true, "send", true}, {"control.release", true, "send", true},
		{"agent.send", true, "send", true}, {"agent.input", true, "send", true},
		{"agent.steer", true, "steer", true}, {"agent.interrupt", true, "interrupt", true},
		{"agent.send", false, "send", false}, {"agent.steer", true, "send", false},
		{"agent.interrupt", true, "send", false}, {"annotate", true, "view", false},
	} {
		t.Run(fmt.Sprintf("%s/lease=%t/cap=%s", test.kind, test.lease, test.capability), func(t *testing.T) {
			store, _, share, participant, _, command := remoteAdmissionFixture(t)
			ctx := context.Background()
			command.Kind = test.kind
			if _, err := store.DB().Exec(`UPDATE shares SET capabilities=? WHERE id=?`, fmt.Sprintf(`["%s"]`, test.capability), share.ID); err != nil {
				t.Fatal(err)
			}
			if !test.lease {
				if _, err := store.DB().Exec(`DELETE FROM control_leases WHERE share_id=?`, share.ID); err != nil {
					t.Fatal(err)
				}
			}
			got, created, err := store.AdmitRemoteCommand(ctx, command, participant.ID, time.Time{})
			if test.allowed {
				if err != nil || !created || got.Status != "running" {
					t.Fatalf("admission=%#v created=%t err=%v", got, created, err)
				}
			} else {
				if err == nil || created {
					t.Fatalf("denied command admitted: %#v %v", got, err)
				}
				stored, err := store.GetCommand(ctx, share.ID, command.ID)
				if err == nil && stored.Status != "error" {
					t.Fatalf("denied claim acquired execution: %#v", stored)
				}
				if err != nil && !errors.Is(err, domain.ErrNotFound) {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestRemoteAdmissionRejectsRevokedExpiredAndStaleCommands(t *testing.T) {
	for _, reason := range []string{"revoked", "expired-share", "expired-lease", "epoch", "revision", "participant"} {
		t.Run(reason, func(t *testing.T) {
			store, _, share, participant, _, command := remoteAdmissionFixture(t)
			ctx := context.Background()
			switch reason {
			case "revoked":
				if err := store.RevokeShare(ctx, share.ID, time.Time{}); err != nil {
					t.Fatal(err)
				}
			case "expired-share":
				if _, err := store.DB().Exec(`UPDATE shares SET expires_at=1 WHERE id=?`, share.ID); err != nil {
					t.Fatal(err)
				}
			case "expired-lease":
				if _, err := store.DB().Exec(`UPDATE control_leases SET expires_at=1 WHERE share_id=?`, share.ID); err != nil {
					t.Fatal(err)
				}
			case "epoch":
				command.LeaseEpoch++
			case "revision":
				command.ExpectedRevision++
			case "participant":
				participant.ID = "not-a-share-member"
			}
			if _, created, err := store.AdmitRemoteCommand(ctx, command, participant.ID, time.Time{}); err == nil || created {
				t.Fatalf("unsafe admission: created=%t err=%v", created, err)
			}
			stored, err := store.GetCommand(ctx, share.ID, command.ID)
			if reason == "revoked" || reason == "expired-share" || reason == "participant" {
				if !errors.Is(err, domain.ErrNotFound) {
					t.Fatalf("unauthorized request persisted: %v", err)
				}
			} else if err != nil || stored.Status != "error" || stored.CompletedAt.IsZero() {
				t.Fatalf("fence rejection not durable: %#v %v", stored, err)
			}
		})
	}
}

func TestRemoteAdmissionReplayNeverDispatchesAgainAfterRevocation(t *testing.T) {
	store, _, share, participant, _, command := remoteAdmissionFixture(t)
	ctx := context.Background()
	if _, created, err := store.AdmitRemoteCommand(ctx, command, participant.ID, time.Time{}); err != nil || !created {
		t.Fatalf("admit=%t %v", created, err)
	}
	if err := store.RevokeShare(ctx, share.ID, time.Time{}); err != nil {
		t.Fatal(err)
	}
	// Admission won the race. Finishing is allowed; no new instruction is claimed.
	if _, err := store.CompleteCommand(ctx, share.ID, command.ID, "completed", "", "", time.Time{}); err != nil {
		t.Fatal(err)
	}
	got, created, err := store.AdmitRemoteCommand(ctx, command, participant.ID, time.Time{})
	if err != nil || created || got.Status != "completed" {
		t.Fatalf("replay=%#v created=%t err=%v", got, created, err)
	}
	conflict := command
	conflict.RequestObject = "different-request"
	if _, _, err := store.AdmitRemoteCommand(ctx, conflict, participant.ID, time.Time{}); !errors.Is(err, domain.ErrCommandConflict) {
		t.Fatalf("conflict=%v", err)
	}
	command.ID = "new-after-revoke"
	if _, created, err := store.AdmitRemoteCommand(ctx, command, participant.ID, time.Time{}); err == nil || created {
		t.Fatalf("new post-revoke admission=%t %v", created, err)
	}
}

func TestConcurrentRemoteAdmissionClaimsExactlyOnce(t *testing.T) {
	store, _, _, participant, _, command := remoteAdmissionFixture(t)
	var wg sync.WaitGroup
	results := make(chan bool, 12)
	failures := make(chan error, 12)
	for range 12 {
		wg.Go(func() {
			_, created, err := store.AdmitRemoteCommand(context.Background(), command, participant.ID, time.Time{})
			results <- created
			failures <- err
		})
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	count := 0
	for created := range results {
		if created {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("new dispatch claims=%d", count)
	}
}

func TestRemoteAdmissionQueuedBehindRevokeCannotUseOldShareState(t *testing.T) {
	store, _, share, participant, _, command := remoteAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := store.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(context.Background(), `ROLLBACK`)
	if _, err = conn.ExecContext(ctx, `UPDATE shares SET revoked_at=? WHERE id=?`, time.Now().UnixMilli(), share.ID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, created, err := store.AdmitRemoteCommand(ctx, command, participant.ID, time.Time{})
		if created {
			err = errors.New("admitted behind committed revoke")
		}
		result <- err
	}()
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("late admission=%v", err)
	}
	if _, err := store.GetCommand(ctx, share.ID, command.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("late command row=%v", err)
	}
}

func TestRemoteAdmissionUsesExpiryAtStatementTimeAfterWriteWait(t *testing.T) {
	store, _, share, participant, _, command := remoteAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	expires := time.Now().Add(100 * time.Millisecond)
	if _, err := store.DB().Exec(`UPDATE shares SET expires_at=? WHERE id=?`, expires.UnixMilli(), share.ID); err != nil {
		t.Fatal(err)
	}
	conn, err := store.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(context.Background(), `ROLLBACK`)
	result := make(chan error, 1)
	go func() {
		_, created, err := store.AdmitRemoteCommand(ctx, command, participant.ID, time.Time{})
		if created {
			err = errors.New("admitted with stale pre-lock expiry")
		}
		result <- err
	}()
	// Hold the write lock across expiry. No Provider or runtime is involved.
	<-time.After(time.Until(expires.Add(25 * time.Millisecond)))
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expired admission=%v", err)
	}
	if _, err := store.GetCommand(ctx, share.ID, command.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expired command row=%v", err)
	}
}
