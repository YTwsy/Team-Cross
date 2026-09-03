package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"teamcross/internal/domain"
)

func createFollowReview(t *testing.T, app *App) (threadDetail, domain.SessionFollow) {
	t.Helper()
	app.snapshotReader = func(context.Context, string, string) (domain.SessionSnapshot, error) {
		snapshot := reviewFixture()
		snapshot.Capabilities.Follow = true // Synthetic contract fixture, not native validation.
		return snapshot, nil
	}
	response := requestJSON(t, app.Handler(), "POST", "/api/v1/threads/from-session", sessionImportRequest{Provider: "codex", SessionID: "native-conversation"}, nil)
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	var detail threadDetail
	decodeResponse(t, response, &detail)
	follow, _, err := app.store.CreateSessionFollow(context.Background(), detail.ID, detail.SessionSnapshots[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	return detail, follow
}

func TestFollowRequiresExplicitConfirmationAndFreshLocalCapability(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	detail := createReviewThread(t, app)
	path := "/api/v1/threads/" + detail.ID + "/follows"
	for _, input := range []startFollowRequest{{SnapshotID: detail.SessionSnapshots[0].ID}, {SnapshotID: detail.SessionSnapshots[0].ID, ConfirmReadOnly: true}} {
		response := requestJSON(t, app.Handler(), "POST", path, input, nil)
		if response.Code != 422 {
			t.Fatalf("unsupported start accepted: %d %s", response.Code, response.Body.String())
		}
	}
	// Forged/stale capability in stored evidence must never authorize local reads.
	forged := reviewFixture()
	forged.Capabilities.Follow = true
	if err := app.captureReviewSnapshot(context.Background(), detail.ID, forged); err != nil {
		t.Fatal(err)
	}
	snapshots, _ := app.store.ListSessionSnapshots(context.Background(), detail.ID)
	response := requestJSON(t, app.Handler(), "POST", path, startFollowRequest{SnapshotID: snapshots[len(snapshots)-1].ID, ConfirmReadOnly: true}, nil)
	if response.Code != 422 {
		t.Fatalf("old evidence became native authority: %s", response.Body.String())
	}
	follows, _ := app.store.ListSessionFollows(context.Background(), detail.ID)
	runs, _ := app.store.ListAgentRuns(context.Background(), detail.ID)
	if len(follows) != 0 || len(runs) != 0 {
		t.Fatal("unsupported Follow created runtime state")
	}
}

func TestFollowIncrementalUpsertsImmutableCapturesAndFrozenShare(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	detail, follow := createFollowReview(t, app)
	ctx := context.Background()
	remote, headers := installReviewShare(t, app, detail.ID, domain.ShareScope{SnapshotID: detail.SessionSnapshots[0].ID, EntryIDs: []string{"public"}, IncludeEvents: true}, false)
	poll := domain.SessionPoll{Source: follow.Source, Cursor: "cursor-1", Reset: true, CapturedAt: time.Now().UTC(), Entries: []domain.SessionEntry{{ID: "recent", Kind: "message", Text: "SECRET FOLLOW OUTPUT"}}, Truncated: true, Gaps: []string{"initial tail"}, Warnings: []string{}}
	app.followReader = func(context.Context, domain.SessionFollow) (domain.SessionPoll, error) { return poll, nil }
	if err := app.pollSessionFollow(ctx, follow); err != nil {
		t.Fatal(err)
	}
	current, _ := app.store.GetSessionFollow(ctx, detail.ID, follow.ID)
	firstID := current.CurrentSnapshotID
	if current.Cursor != "cursor-1" || firstID == follow.SourceSnapshotID || len(current.Gaps) != 1 {
		t.Fatalf("checkpoint missing: %#v", current)
	}
	thread, _ := app.store.GetThread(ctx, detail.ID)
	poll.Reset, poll.Cursor = false, "cursor-2"
	if err := app.pollSessionFollow(ctx, current); err != nil {
		t.Fatal(err)
	}
	current, _ = app.store.GetSessionFollow(ctx, detail.ID, follow.ID)
	after, _ := app.store.GetThread(ctx, detail.ID)
	if current.CurrentSnapshotID != firstID || current.Cursor != "cursor-2" || after.Revision != thread.Revision {
		t.Fatal("duplicate output created another capture")
	}
	poll.Cursor, poll.Entries[0].Text = "cursor-3", "SECRET UPDATED TOOL OUTPUT"
	if err := app.pollSessionFollow(ctx, current); err != nil {
		t.Fatal(err)
	}
	current, _ = app.store.GetSessionFollow(ctx, detail.ID, follow.ID)
	first, _ := app.store.GetSessionSnapshot(ctx, detail.ID, firstID)
	latest, _ := app.store.GetSessionSnapshot(ctx, detail.ID, current.CurrentSnapshotID)
	if first.Entries[0].Text != "SECRET FOLLOW OUTPUT" || latest.Entries[0].Text != poll.Entries[0].Text {
		t.Fatal("stable-ID update rewrote an immutable capture")
	}
	if err := app.validateAnnotationTarget(ctx, detail.ID, annotationRequest{Target: &domain.AnnotationTarget{SnapshotID: firstID, EntryID: "recent"}}, access{Mode: "host"}); err != nil {
		t.Fatalf("old anchor was lost: %v", err)
	}
	response := requestJSON(t, remote, "GET", "/api/v1/threads/"+detail.ID, nil, headers)
	if response.Code != 200 || strings.Contains(response.Body.String(), "SECRET") || strings.Contains(response.Body.String(), follow.ID) || strings.Contains(response.Body.String(), "cursor-") {
		t.Fatalf("Follow expanded the Share: %s", response.Body.String())
	}
	for _, method := range []string{"POST", "DELETE"} {
		path := "/api/v1/threads/" + detail.ID + "/follows"
		if method == "DELETE" {
			path += "/" + follow.ID
		}
		response := requestJSON(t, remote, method, path, startFollowRequest{SnapshotID: follow.SourceSnapshotID, ConfirmReadOnly: true}, headers)
		if response.Code != http.StatusForbidden {
			t.Fatalf("remote Follow control accepted: %d %s", response.Code, response.Body.String())
		}
	}
	runs, _ := app.store.ListAgentRuns(ctx, detail.ID)
	if len(runs) != 0 {
		t.Fatal("Follow created a Run")
	}
}

func TestFollowRejectsWrongIdentityDuplicateIDsAndPreservesCursorOnFailure(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	detail, follow := createFollowReview(t, app)
	ctx := context.Background()
	valid := domain.SessionPoll{Source: follow.Source, Cursor: "cursor", Reset: true, Entries: []domain.SessionEntry{{ID: "one", Text: "text"}}, Warnings: []string{}}
	for _, kind := range []string{"identity", "version", "duplicate", "oversized-cursor", "missing-reset", "transport"} {
		t.Run(kind, func(t *testing.T) {
			poll := valid
			var failure error
			switch kind {
			case "identity":
				poll.Source.SessionID = "another-native-session"
			case "version":
				poll.Source.ProviderVersion = "changed-unverified-version"
			case "duplicate":
				poll.Entries = append(poll.Entries, poll.Entries[0])
			case "oversized-cursor":
				poll.Cursor = strings.Repeat("x", 129<<10)
			case "missing-reset":
				poll.Reset = false
			case "transport":
				failure = errors.New("simulated disconnected source")
			}
			app.followReader = func(context.Context, domain.SessionFollow) (domain.SessionPoll, error) { return poll, failure }
			if err := app.pollSessionFollow(ctx, follow); err == nil {
				t.Fatal("invalid poll accepted")
			}
			current, _ := app.store.GetSessionFollow(ctx, detail.ID, follow.ID)
			rounds, _ := app.store.ListRounds(ctx, detail.ID)
			if current.Cursor != "" || current.CurrentSnapshotID != follow.SourceSnapshotID || len(rounds) != 1 {
				t.Fatal("failure consumed a checkpoint")
			}
		})
	}
}

func TestFollowResetKeepsOldReferencesAndReportsGap(t *testing.T) {
	previous := reviewFixture()
	follow := domain.SessionFollow{Source: previous.Source, Cursor: "old-cursor", Gaps: []string{"earlier gap"}}
	poll := domain.SessionPoll{Reset: true, Entries: []domain.SessionEntry{{ID: "replacement", Text: "rewritten history"}}, Gaps: []string{"missing boundary"}, Warnings: []string{}, Truncated: true}
	next, gaps, changed, err := mergeFollowPoll(previous, follow, poll)
	if err != nil || !changed || len(next.Entries) != 1 || next.Entries[0].ID != "replacement" || len(gaps) != 3 || !next.Truncated {
		t.Fatalf("reset lost gap: %#v %#v %v", next, gaps, err)
	}
	if len(previous.Entries) != 2 || previous.Entries[0].ID != "public" {
		t.Fatal("reset mutated old snapshot")
	}
}

func TestFollowReaderRestartsAtDurableCursorAndStopFencesInFlightRead(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	t.Cleanup(app.stopFollowWorkers)
	detail, follow := createFollowReview(t, app)
	ctx := context.Background()
	checkpoint := follow
	checkpoint.Cursor = "persisted-before-restart"
	if err := app.store.CommitFollowPoll(ctx, follow, checkpoint, nil, nil); err != nil {
		t.Fatal(err)
	}
	// Restore a persisted reader, just as Open does, without executing an Agent.
	entered := make(chan domain.SessionFollow, 1)
	release := make(chan struct{})
	app.followReader = func(ctx context.Context, current domain.SessionFollow) (domain.SessionPoll, error) {
		entered <- current
		select {
		case <-release:
		case <-ctx.Done():
		}
		// Deliberately return output even on cancellation to exercise durable fencing.
		return domain.SessionPoll{Source: current.Source, Cursor: "late", Entries: []domain.SessionEntry{{ID: "late", Text: "late output"}}}, nil
	}
	app.startSessionFollows(ctx)
	select {
	case got := <-entered:
		if got.ID != follow.ID || got.Cursor != checkpoint.Cursor {
			t.Fatal("restart did not use persisted source")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("persisted reader did not restart")
	}
	response := requestJSON(t, app.Handler(), "DELETE", "/api/v1/threads/"+detail.ID+"/follows/"+follow.ID, nil, nil)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	close(release)
	app.stopFollowWorkers()
	current, _ := app.store.GetSessionFollow(ctx, detail.ID, follow.ID)
	rounds, _ := app.store.ListRounds(ctx, detail.ID)
	if current.State != "stopped" || current.Cursor != checkpoint.Cursor || len(rounds) != 1 {
		t.Fatal("stopped reader published late output")
	}
}

func TestFollowRPCOmitsInitialCursor(t *testing.T) {
	follow := domain.SessionFollow{Source: reviewFixture().Source}
	if _, present := followPollParams(follow)["cursor"]; present {
		t.Fatal("initial empty cursor must be absent in RPC")
	}
	follow.Cursor = "opaque"
	if followPollParams(follow)["cursor"] != follow.Cursor {
		t.Fatal("durable cursor was not passed to reader")
	}
}

func TestFollowLargeToolOutputsEvictOldEntriesWithoutLosingProgress(t *testing.T) {
	previous := reviewFixture()
	previous.Entries = []domain.SessionEntry{}
	follow := domain.SessionFollow{Source: previous.Source, Cursor: "existing", Gaps: []string{}}
	for i := 0; i < 12; i++ {
		poll := domain.SessionPoll{Entries: []domain.SessionEntry{{ID: string(rune('a' + i)), Kind: "tool", Text: strings.Repeat("x", 2<<20)}}, Warnings: []string{}}
		next, gaps, changed, err := mergeFollowPoll(previous, follow, poll)
		if err != nil || !changed || next.Entries[len(next.Entries)-1].ID != poll.Entries[0].ID || len(jsonBytes(next)) > 20<<20 {
			t.Fatalf("large output blocked forward progress: step=%d err=%v", i, err)
		}
		previous, follow.Gaps = next, gaps
	}
	if !previous.Truncated || len(follow.Gaps) == 0 || len(previous.Entries) >= 12 {
		t.Fatal("byte-bound eviction was not marked")
	}
}

func TestFollowStartIsOwnerOnlyIdempotentAndNeverExecutes(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	t.Cleanup(app.stopFollowWorkers)
	ctx := context.Background()
	snapshot := reviewFixture()
	snapshot.ID, snapshot.Capabilities.Follow = "verified-fixture", true
	thread, err := app.store.CreateSessionReviewThread(ctx, "Owner follow", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	app.snapshotReader = func(context.Context, string, string) (domain.SessionSnapshot, error) { return snapshot, nil }
	app.followReader = func(ctx context.Context, _ domain.SessionFollow) (domain.SessionPoll, error) {
		<-ctx.Done()
		return domain.SessionPoll{}, ctx.Err()
	}
	path := "/api/v1/threads/" + thread.ID + "/follows"
	input := startFollowRequest{SnapshotID: snapshot.ID, ConfirmReadOnly: true}
	first := requestJSON(t, app.Handler(), "POST", path, input, nil)
	second := requestJSON(t, app.Handler(), "POST", path, input, nil)
	if first.Code != 201 || second.Code != 200 {
		t.Fatalf("start/replay: %d %s; %d %s", first.Code, first.Body.String(), second.Code, second.Body.String())
	}
	var a, b domain.SessionFollow
	decodeResponse(t, first, &a)
	decodeResponse(t, second, &b)
	if a.ID != b.ID || a.Source.SessionID != snapshot.Source.SessionID {
		t.Fatal("duplicate start changed source or Follow identity")
	}
	follows, _ := app.store.ListSessionFollows(ctx, thread.ID)
	runs, _ := app.store.ListAgentRuns(ctx, thread.ID)
	rounds, _ := app.store.ListRounds(ctx, thread.ID)
	if len(follows) != 1 || len(runs) != 0 || len(rounds) != 1 {
		t.Fatal("starting a reader created execution or duplicate state")
	}
}

func TestFollowReconnectRevalidatesDowngradedReaderBeforePolling(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	t.Cleanup(app.stopFollowWorkers)
	_, _ = createFollowReview(t, app)
	var checks, polls atomic.Int32
	rechecked := make(chan struct{})
	app.snapshotReader = func(context.Context, string, string) (domain.SessionSnapshot, error) {
		snapshot := reviewFixture()
		snapshot.Capabilities.Follow = checks.Add(1) == 1
		if !snapshot.Capabilities.Follow {
			select {
			case <-rechecked:
			default:
				close(rechecked)
			}
		}
		return snapshot, nil
	}
	app.followReader = func(context.Context, domain.SessionFollow) (domain.SessionPoll, error) {
		polls.Add(1)
		return domain.SessionPoll{}, errors.New("reader disconnected")
	}
	app.startSessionFollows(context.Background())
	select {
	case <-rechecked:
	case <-time.After(10 * time.Second):
		t.Fatal("reconnecting reader never revalidated capability")
	}
	app.stopFollowWorkers()
	if polls.Load() != 1 || checks.Load() < 2 {
		t.Fatalf("unverified replacement reader polled: checks=%d polls=%d", checks.Load(), polls.Load())
	}
}
