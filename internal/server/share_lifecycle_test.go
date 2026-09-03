package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"teamcross/internal/domain"
	"teamcross/internal/invite"
	"teamcross/internal/share"
)

// No listener, native Provider, mDNS, Tailnet or Tailcat is used in these tests.
type fakeShareRuntime struct {
	inv       invite.InvitationV1
	closed    atomic.Bool
	revoked   atomic.Bool
	closeOnce sync.Once
	onClose   func()
}

func newFakeShareRuntime(config share.RuntimeConfig) *fakeShareRuntime {
	return &fakeShareRuntime{inv: invite.InvitationV1{ShareID: config.ShareID, ExpiresAt: config.ExpiresAt.Unix(), Secret: []byte("synthetic-share-secret"), ServerSPKISHA256: []byte("synthetic-spki"), Capabilities: config.Capabilities}}
}
func (r *fakeShareRuntime) Token() (string, error)          { return "synthetic-invite-" + r.inv.ShareID, nil }
func (r *fakeShareRuntime) Invitation() invite.InvitationV1 { return r.inv }
func (r *fakeShareRuntime) Warnings() []string              { return nil }
func (r *fakeShareRuntime) Revoke()                         { r.revoked.Store(true) }
func (r *fakeShareRuntime) Close() error {
	r.closeOnce.Do(func() {
		if r.onClose != nil {
			r.onClose()
		}
		r.closed.Store(true)
	})
	return nil
}

func shareRequestBody(detail threadDetail) map[string]any {
	return map[string]any{"scope": domain.ShareScope{SnapshotID: detail.SessionSnapshots[0].ID, EntryIDs: []string{"public"}}}
}

func waitShareResult(t *testing.T, result <-chan *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case r := <-result:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("Share request did not drain")
		return nil
	}
}

func waitShareSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("Share lifecycle signal timed out")
	}
}

func TestShareCreationSerializesBeforeTransportStart(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	t.Cleanup(app.closeShares)
	detail := createReviewThread(t, app)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	var runtime *fakeShareRuntime
	app.shareStarter = func(_ context.Context, config share.RuntimeConfig) (shareRuntime, error) {
		calls.Add(1)
		runtime = newFakeShareRuntime(config)
		close(started)
		<-release
		return runtime, nil
	}
	path := "/api/v1/threads/" + detail.ID + "/shares"
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- requestJSON(t, app.Handler(), "POST", path, shareRequestBody(detail), nil) }()
	waitShareSignal(t, started)
	second := requestJSON(t, app.Handler(), "POST", path, shareRequestBody(detail), nil)
	if second.Code != 409 || !strings.Contains(second.Body.String(), "share_starting") || calls.Load() != 1 {
		t.Errorf("concurrent start escaped: %d %s calls=%d", second.Code, second.Body.String(), calls.Load())
	}
	close(release)
	first := waitShareResult(t, result)
	if first.Code != 201 || !strings.Contains(first.Body.String(), "synthetic-invite-") {
		t.Fatalf("publish: %s", first.Body.String())
	}
	third := requestJSON(t, app.Handler(), "POST", path, shareRequestBody(detail), nil)
	if third.Code != 409 || calls.Load() != 1 {
		t.Fatalf("active Share replaced: %s", third.Body.String())
	}
	revoked := requestJSON(t, app.Handler(), "DELETE", path+"/current", nil, nil)
	if revoked.Code != 200 || !runtime.closed.Load() || !runtime.revoked.Load() {
		t.Fatalf("revoke did not stop runtime: %s", revoked.Body.String())
	}
	if app.isPublishedShare(detail.ID, runtime.inv.ShareID) {
		t.Fatal("revoked runtime remained published")
	}
}

func TestRevokeFencesLateShareStartup(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	t.Cleanup(app.closeShares)
	detail := createReviewThread(t, app)
	started, release, cancelled := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var runtime *fakeShareRuntime
	app.shareStarter = func(ctx context.Context, config share.RuntimeConfig) (shareRuntime, error) {
		runtime = newFakeShareRuntime(config)
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-release // Deliberately ignore cancellation until a late successful result.
		return runtime, nil
	}
	path := "/api/v1/threads/" + detail.ID + "/shares"
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- requestJSON(t, app.Handler(), "POST", path, shareRequestBody(detail), nil) }()
	waitShareSignal(t, started)
	revoked := requestJSON(t, app.Handler(), "DELETE", path+"/current", nil, nil)
	if revoked.Code != 200 {
		t.Errorf("pending revoke: %s", revoked.Body.String())
	}
	waitShareSignal(t, cancelled)
	retry := requestJSON(t, app.Handler(), "POST", path, shareRequestBody(detail), nil)
	if retry.Code != 409 {
		t.Errorf("cancelled startup slot released before drain: %s", retry.Body.String())
	}
	close(release)
	first := waitShareResult(t, result)
	if first.Code != 409 || strings.Contains(first.Body.String(), "synthetic-invite-") || !runtime.closed.Load() || !runtime.revoked.Load() {
		t.Fatalf("late runtime published: %s", first.Body.String())
	}
	if _, err := app.store.GetShare(context.Background(), runtime.inv.ShareID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cancelled startup persisted: %v", err)
	}
	app.shareStarter = func(_ context.Context, c share.RuntimeConfig) (shareRuntime, error) {
		return newFakeShareRuntime(c), nil
	}
	retry = requestJSON(t, app.Handler(), "POST", path, shareRequestBody(detail), nil)
	if retry.Code != 201 {
		t.Fatalf("drained slot cannot retry: %s", retry.Body.String())
	}
}

func TestShareProjectionFailureRevokesAndReleasesCreation(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	t.Cleanup(app.closeShares)
	detail := createReviewThread(t, app)
	var runtimes []*fakeShareRuntime
	app.shareStarter = func(_ context.Context, c share.RuntimeConfig) (shareRuntime, error) {
		r := newFakeShareRuntime(c)
		runtimes = append(runtimes, r)
		return r, nil
	}
	if _, err := app.store.DB().Exec(`CREATE TRIGGER reject_share_projection BEFORE INSERT ON share_projections BEGIN SELECT RAISE(FAIL, 'projection unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/threads/" + detail.ID + "/shares"
	failed := requestJSON(t, app.Handler(), "POST", path, shareRequestBody(detail), nil)
	if failed.Code < 400 || strings.Contains(failed.Body.String(), "synthetic-invite-") || len(runtimes) != 1 || !runtimes[0].closed.Load() {
		t.Fatalf("projection failure leaked listener: %s", failed.Body.String())
	}
	stored, err := app.store.GetShare(context.Background(), runtimes[0].inv.ShareID)
	if err != nil || stored.RevokedAt.IsZero() || app.isPublishedShare(detail.ID, stored.ID) {
		t.Fatalf("failed Share remains authorized: %#v %v", stored, err)
	}
	if _, err = app.store.DB().Exec(`DROP TRIGGER reject_share_projection`); err != nil {
		t.Fatal(err)
	}
	retry := requestJSON(t, app.Handler(), "POST", path, shareRequestBody(detail), nil)
	if retry.Code != 201 || len(runtimes) != 2 {
		t.Fatalf("failed startup held slot: %s", retry.Body.String())
	}
}

func TestShareCloseCancelsAndDrainsStartupBeforeStoreClose(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	detail := createReviewThread(t, app)
	started, release, cancelled := make(chan struct{}), make(chan struct{}), make(chan struct{})
	cleanupRead := make(chan error, 1)
	app.shareStarter = func(ctx context.Context, config share.RuntimeConfig) (shareRuntime, error) {
		r := newFakeShareRuntime(config)
		r.onClose = func() { _, err := app.store.GetThread(context.Background(), detail.ID); cleanupRead <- err }
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-release
		return r, nil
	}
	path := "/api/v1/threads/" + detail.ID + "/shares"
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- requestJSON(t, app.Handler(), "POST", path, shareRequestBody(detail), nil) }()
	waitShareSignal(t, started)
	closed := make(chan struct{})
	go func() { _ = app.Close(); close(closed) }()
	waitShareSignal(t, cancelled)
	select {
	case <-closed:
		t.Error("App closed before startup cleanup")
	default:
	}
	retry := requestJSON(t, app.Handler(), "POST", path, shareRequestBody(detail), nil)
	if retry.Code != 503 {
		t.Errorf("closing app accepted new startup: %s", retry.Body.String())
	}
	close(release)
	failed := waitShareResult(t, result)
	if failed.Code != 409 {
		t.Errorf("close did not fence startup: %s", failed.Body.String())
	}
	waitShareSignal(t, closed)
	if err := <-cleanupRead; err != nil {
		t.Fatalf("Store closed before runtime cleanup: %v", err)
	}
}

func TestUnpublishedShareCannotReadOrBorrowCurrentScope(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	detail := createReviewThread(t, app)
	old, headers := installReviewShare(t, app, detail.ID, domain.ShareScope{SnapshotID: detail.SessionSnapshots[0].ID, EntryIDs: []string{"public"}}, false)
	oldID := app.shares[detail.ID].ID
	delete(app.shares, detail.ID) // A DB row alone cannot authorize an unpublished runtime.
	path := "/api/v1/threads/" + detail.ID
	if response := requestJSON(t, old, "GET", path, nil, headers); response.Code != http.StatusGone {
		t.Fatalf("unpublished Share served detail: %s", response.Body.String())
	}
	if err := app.requireCurrentShare(context.Background(), oldID, detail.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unpublished snapshot access: %v", err)
	}
	_, _ = installReviewShare(t, app, detail.ID, domain.ShareScope{SnapshotID: detail.SessionSnapshots[0].ID, EntryIDs: []string{"private"}}, false)
	if response := requestJSON(t, old, "GET", path, nil, headers); response.Code != http.StatusGone {
		t.Fatalf("old Share borrowed new publication: %s", response.Body.String())
	}
	view := threadDetail{threadSummary: threadSummary{ID: detail.ID}}
	app.attachEphemeralState(context.Background(), &view, access{Mode: "share", ShareID: oldID})
	if view.Share != nil || len(view.Participants) != 0 {
		t.Fatal("old identity received current Share metadata")
	}
}

func TestExpiryClosesOrphanWithoutRemovingNewShare(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	t.Cleanup(app.closeShares)
	detail := createReviewThread(t, app)
	_, _ = installReviewShare(t, app, detail.ID, domain.ShareScope{}, false)
	old := app.shares[detail.ID]
	runtime := newFakeShareRuntime(share.RuntimeConfig{ShareID: old.ID, ExpiresAt: old.ExpiresAt})
	old.Runtime = runtime
	app.shareByID[old.ID] = old
	_, _ = installReviewShare(t, app, detail.ID, domain.ShareScope{}, false)
	current := app.shares[detail.ID]
	app.expireHostedShare(detail.ID, old.ID, time.Now())
	if !runtime.closed.Load() || !runtime.revoked.Load() || app.shares[detail.ID] != current || app.shareByID[old.ID] != nil {
		t.Fatal("orphan expiry leaked or removed the replacement Share")
	}
}

func TestConcurrentAppCloseWaitsForPublishedRuntimeCleanup(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	detail := createReviewThread(t, app)
	entered, release := make(chan struct{}), make(chan struct{})
	app.shareStarter = func(_ context.Context, config share.RuntimeConfig) (shareRuntime, error) {
		runtime := newFakeShareRuntime(config)
		runtime.onClose = func() { close(entered); <-release }
		return runtime, nil
	}
	response := requestJSON(t, app.Handler(), "POST", "/api/v1/threads/"+detail.ID+"/shares", shareRequestBody(detail), nil)
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	first, second := make(chan struct{}), make(chan struct{})
	go func() { _ = app.Close(); close(first) }()
	waitShareSignal(t, entered)
	go func() { _ = app.Close(); close(second) }()
	// Reading must remain possible throughout the first runtime's cleanup.
	if _, err := app.store.GetThread(context.Background(), detail.ID); err != nil {
		t.Errorf("Store closed during runtime cleanup: %v", err)
	}
	select {
	case <-first:
		t.Error("first Close returned before cleanup")
	default:
	}
	select {
	case <-second:
		t.Error("second Close returned before cleanup")
	default:
	}
	close(release)
	waitShareSignal(t, first)
	waitShareSignal(t, second)
}
