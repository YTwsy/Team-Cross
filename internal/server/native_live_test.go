package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"teamcross/internal/domain"
)

func readyNativeLive(t *testing.T, app *App) (threadDetail, domain.SessionFollow) {
	t.Helper()
	detail, follow := createFollowReview(t, app)
	next := follow
	now := time.Now().UTC()
	next.LastPolledAt, next.UpdatedAt, next.Cursor = &now, now, "initial-read"
	if err := app.store.CommitFollowPoll(context.Background(), follow, next, nil, nil); err != nil {
		t.Fatal(err)
	}
	return detail, next
}

func nativeLiveScope(follow domain.SessionFollow) domain.ShareScope {
	return domain.ShareScope{NativeLive: &domain.NativeLiveScope{FollowID: follow.ID, ExpectedSnapshotID: follow.CurrentSnapshotID, EntryKinds: []string{"message"}, ConfirmCurrentAndFuture: true}}
}

func advanceNativeLive(t *testing.T, app *App, previous domain.SessionFollow, text string) domain.SessionFollow {
	t.Helper()
	app.followReader = func(context.Context, domain.SessionFollow) (domain.SessionPoll, error) {
		return domain.SessionPoll{Source: previous.Source, Reset: previous.Cursor == "", CapturedAt: time.Now().UTC(), Cursor: previous.Cursor + "+", Entries: []domain.SessionEntry{
			{ID: "public", Kind: "message", Role: "assistant", Text: text},
			{ID: "private", Kind: "tool", Text: "SECRET tool parameters and output"},
			{ID: "notice", Kind: "notice", Text: "SECRET native notice"},
		}, Warnings: []string{"SECRET source diagnostics"}}, nil
	}
	if err := app.pollSessionFollow(context.Background(), previous); err != nil {
		t.Fatal(err)
	}
	follow, err := app.store.GetSessionFollow(context.Background(), previous.ThreadID, previous.ID)
	if err != nil {
		t.Fatal(err)
	}
	return follow
}

func TestNativeLiveConsentRequiresExactSuccessfulCurrentFollow(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	detail, initial := createFollowReview(t, app)
	if _, err := app.prepareShareProjection(context.Background(), detail.ID, ptrNativeScope(initial), false); err == nil {
		t.Fatal("unpolled reader was accepted")
	}
	next := initial
	now := time.Now().UTC()
	next.LastPolledAt = &now
	if err := app.store.CommitFollowPoll(context.Background(), initial, next, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"confirmation", "snapshot", "follow", "empty", "unknown", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			scope := nativeLiveScope(next)
			switch kind {
			case "confirmation":
				scope.NativeLive.ConfirmCurrentAndFuture = false
			case "snapshot":
				scope.NativeLive.ExpectedSnapshotID = "private-other-snapshot"
			case "follow":
				scope.NativeLive.FollowID = "other-follow"
			case "empty":
				scope.NativeLive.EntryKinds = nil
			case "unknown":
				scope.NativeLive.EntryKinds = []string{"thoughts"}
			case "duplicate":
				scope.NativeLive.EntryKinds = []string{"message", "message"}
			}
			if _, err := app.prepareShareProjection(context.Background(), detail.ID, &scope, false); err == nil {
				t.Fatal("invalid live consent accepted")
			}
		})
	}
	if _, err := app.store.StopSessionFollow(context.Background(), detail.ID, next.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.prepareShareProjection(context.Background(), detail.ID, ptrNativeScope(next), false); err == nil {
		t.Fatal("stopped Follow was accepted")
	}
}

func ptrNativeScope(follow domain.SessionFollow) *domain.ShareScope {
	scope := nativeLiveScope(follow)
	return &scope
}

func TestNativeLivePublishesOnlyAuthorizedWindowsAndKeepsOldAnchors(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	detail, follow := readyNativeLive(t, app)
	ctx := context.Background()
	firstID := follow.CurrentSnapshotID
	_, err := app.store.CreateAnnotation(ctx, domain.Annotation{ThreadID: detail.ID, Body: "Before sharing: exact public feedback", Target: &domain.AnnotationTarget{SnapshotID: firstID, EntryID: "public"}})
	if err != nil {
		t.Fatal(err)
	}
	remote, headers := installReviewShare(t, app, detail.ID, nativeLiveScope(follow), false)
	shareID := app.shares[detail.ID].ID
	path := "/api/v1/threads/" + detail.ID
	read := func(route string) *httptest.ResponseRecorder {
		t.Helper()
		response := requestJSON(t, remote, "GET", route, nil, headers)
		if response.Code != 200 || strings.Contains(response.Body.String(), "SECRET") || strings.Contains(response.Body.String(), "initial-read") || strings.Contains(response.Body.String(), `"sessionFollows"`) {
			t.Fatalf("live projection leaked or failed: %d %s", response.Code, response.Body.String())
		}
		return response
	}
	initial := read(path)
	if !strings.Contains(initial.Body.String(), "PUBLIC answer") || !strings.Contains(initial.Body.String(), "Before sharing") {
		t.Fatal("initial window or its exact annotations omitted")
	}
	follow = advanceNativeLive(t, app, follow, "Public revised answer")
	updated := read(path)
	var view threadDetail
	decodeResponse(t, updated, &view)
	if len(view.SessionSnapshots) != 1 || view.SessionSnapshots[0].ID != follow.CurrentSnapshotID || len(view.SessionSnapshots[0].Entries) != 1 || view.NativeLive == nil {
		t.Fatalf("unbounded or incorrect latest view: %s", updated.Body.String())
	}
	old := read(path + "/sessions/snapshots/" + firstID)
	if !strings.Contains(old.Body.String(), "PUBLIC answer") || strings.Contains(old.Body.String(), "Public revised answer") {
		t.Fatal("old anchor was rebound to later text")
	}
	thread, _ := app.store.GetThread(ctx, detail.ID)
	comment := annotationRequest{Body: "Old-window feedback", CommandID: "live-annotation", ExpectedRevision: thread.Revision, Target: &domain.AnnotationTarget{SnapshotID: firstID, EntryID: "public"}}
	posted := requestJSON(t, remote, "POST", path+"/annotations", comment, headers)
	if posted.Code != 201 {
		t.Fatal(posted.Body.String())
	}
	replay := requestJSON(t, remote, "POST", path+"/annotations", comment, headers)
	if replay.Code != 200 {
		t.Fatal(replay.Body.String())
	}
	for _, target := range []domain.AnnotationTarget{{SnapshotID: firstID}, {SnapshotID: follow.CurrentSnapshotID, EntryID: "private"}, {SnapshotID: follow.CurrentSnapshotID, EntryID: "notice"}} {
		response := requestJSON(t, remote, "POST", path+"/annotations", annotationRequest{Body: "hidden", CommandID: "bad-" + target.EntryID, Target: &target}, headers)
		if response.Code != 422 {
			t.Fatalf("hidden target accepted: %s", response.Body.String())
		}
	}
	// A later import has the same native identity and same stable entry ID, but
	// was never admitted by this particular Follow/Share transaction.
	private := reviewFixture()
	private.ID = "never-admitted"
	private.Entries[0].Text = "SECRET separate import"
	if err := app.captureReviewSnapshot(ctx, detail.ID, private); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{private.ID, "guessed-snapshot"} {
		response := requestJSON(t, remote, "GET", path+"/sessions/snapshots/"+id, nil, headers)
		if response.Code != 404 || strings.Contains(response.Body.String(), "SECRET") {
			t.Fatal(response.Body.String())
		}
		response = requestJSON(t, remote, "POST", path+"/annotations", annotationRequest{Body: "hidden", CommandID: id, Target: &domain.AnnotationTarget{SnapshotID: id, EntryID: "public"}}, headers)
		if response.Code != 422 {
			t.Fatal("same stable entry ID bypassed membership")
		}
	}
	read(path)
	for _, route := range []string{"/follows", "/sessions/open", "/agent/send", "/continue", "/fork", "/bundles"} {
		response := requestJSON(t, remote, "POST", path+route, map[string]any{}, headers)
		if response.Code != 403 {
			t.Fatalf("live share granted execution: %s %d", route, response.Code)
		}
	}
	runs, _ := app.store.ListAgentRuns(ctx, detail.ID)
	if len(runs) != 0 {
		t.Fatal("live review started Agent execution")
	}
	if err := app.store.RevokeShare(ctx, shareID, time.Time{}); err != nil {
		t.Fatal(err)
	}
	response := requestJSON(t, remote, "GET", path+"/sessions/snapshots/"+firstID, nil, headers)
	if response.Code != 403 {
		t.Fatal("revoked membership remained remotely accessible")
	}
}

func TestNativeLiveSSEUsesDynamicMembershipAndReconnectCursor(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	detail, follow := readyNativeLive(t, app)
	remote, headers := installReviewShare(t, app, detail.ID, nativeLiveScope(follow), false)
	shareID := app.shares[detail.ID].ID
	server := httptest.NewServer(remote)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	path := "/api/v1/threads/" + detail.ID + "/events"
	events, _ := app.store.EventsAfter(ctx, detail.ID, 0, 500)
	after := events[len(events)-1].Seq
	request, _ := http.NewRequestWithContext(ctx, "GET", server.URL+path, nil)
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	request.Header.Set("Last-Event-ID", fmt.Sprint(after))
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	// The SSE projection was loaded before this new membership was admitted.
	follow = advanceNativeLive(t, app, follow, "Public future output")
	scanner := bufio.NewScanner(response.Body)
	var notification eventView
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "SECRET") || strings.Contains(line, "initial-read") {
			t.Fatal("SSE leaked private data")
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &notification); err != nil {
			t.Fatal(err)
		}
		if notification.Type == "shared.session.updated" {
			break
		}
	}
	if notification.Payload["snapshotId"] != follow.CurrentSnapshotID || notification.Seq <= after || len(notification.Payload) != 4 {
		t.Fatalf("missing live notification: %#v", notification)
	}
	_ = response.Body.Close()
	// Reconnect from the older cursor repeats the same admitted reference, not
	// raw native history or whatever happens to be the newest private snapshot.
	replayCtx, stop := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer stop()
	replay := httptest.NewRequest("GET", path+"?after="+fmt.Sprint(after), nil).WithContext(replayCtx)
	for key, value := range headers {
		replay.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	remote.ServeHTTP(recorder, replay)
	if !strings.Contains(recorder.Body.String(), follow.CurrentSnapshotID) || strings.Contains(recorder.Body.String(), "Public future output") || strings.Contains(recorder.Body.String(), "roundId") {
		t.Fatalf("unsafe replay: %s", recorder.Body.String())
	}
	// Limits notify only the last admitted ID, never an unshared newer window.
	if _, err := app.store.DB().ExecContext(ctx, "UPDATE share_native_live SET state='limited' WHERE share_id=?", shareID); err != nil {
		t.Fatal(err)
	}
	p, _ := app.loadShareProjection(ctx, shareID)
	limited, err := app.projectSharedEvent(ctx, shareID, detail.ID, p, eventView{Seq: notification.Seq + 1, Type: "session.follow.updated", Payload: map[string]any{"followId": follow.ID, "snapshotId": "SECRET-not-admitted", "revision": 9}})
	if err != nil || limited.Payload["snapshotId"] != follow.CurrentSnapshotID || strings.Contains(string(jsonBytes(limited)), "SECRET") {
		t.Fatalf("limit leaked hidden checkpoint: %#v %v", limited, err)
	}
}

func TestNativeLiveStaticUnionCanAuthorizeWholeExactSnapshot(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	detail, follow := readyNativeLive(t, app)
	scope := nativeLiveScope(follow)
	scope.SnapshotID = follow.CurrentSnapshotID
	scope.EntryIDs = []string{"private"} // Explicit static tool selection.
	remote, headers := installReviewShare(t, app, detail.ID, scope, false)
	path := "/api/v1/threads/" + detail.ID
	response := requestJSON(t, remote, "GET", path+"/sessions/snapshots/"+follow.CurrentSnapshotID, nil, headers)
	var snapshot domain.SessionSnapshot
	decodeResponse(t, response, &snapshot)
	if response.Code != 200 || len(snapshot.Entries) != 2 {
		t.Fatal(response.Body.String())
	}
	thread, _ := app.store.GetThread(context.Background(), detail.ID)
	response = requestJSON(t, remote, "POST", path+"/annotations", annotationRequest{Body: "all selected", CommandID: "whole-union", ExpectedRevision: thread.Revision, Target: &domain.AnnotationTarget{SnapshotID: follow.CurrentSnapshotID}}, headers)
	if response.Code != 201 {
		t.Fatalf("union of exact authorizations rejected: %s", response.Body.String())
	}
}

func TestNativeLiveDetailKeepsStaticAndOldLiveUnion(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	detail, follow := readyNativeLive(t, app)
	firstID := follow.CurrentSnapshotID
	scope := nativeLiveScope(follow)
	scope.SnapshotID = firstID
	scope.EntryIDs = []string{"private"} // Only this window's tool is explicitly selected.
	remote, headers := installReviewShare(t, app, detail.ID, scope, false)
	path := "/api/v1/threads/" + detail.ID
	check := func(latestID string) {
		t.Helper()
		response := requestJSON(t, remote, "GET", path, nil, headers)
		var view threadDetail
		decodeResponse(t, response, &view)
		if response.Code != http.StatusOK {
			t.Fatal(response.Body.String())
		}
		wantCount := 2
		if latestID == firstID {
			wantCount = 1
		}
		if len(view.SessionSnapshots) != wantCount {
			t.Fatalf("detail returned duplicate or historical windows: %s", response.Body.String())
		}
		foundOld, foundLatest := false, false
		for _, snapshot := range view.SessionSnapshots {
			exactResponse := requestJSON(t, remote, "GET", path+"/sessions/snapshots/"+snapshot.ID, nil, headers)
			var exact domain.SessionSnapshot
			decodeResponse(t, exactResponse, &exact)
			if exactResponse.Code != http.StatusOK || !reflect.DeepEqual(snapshot, exact) {
				t.Fatalf("detail disagrees with exact authorized snapshot %s: detail=%+v exact=%s", snapshot.ID, snapshot, exactResponse.Body.String())
			}
			if snapshot.ID == firstID {
				foundOld = true
				if len(snapshot.Entries) != 2 || snapshot.Entries[0].ID != "private" || snapshot.Entries[1].ID != "public" || snapshot.Entries[1].Text != "PUBLIC answer" {
					t.Fatalf("old static/live union lost or rebound: %+v", snapshot.Entries)
				}
			}
			if snapshot.ID == latestID {
				foundLatest = true
				if latestID != firstID && (len(snapshot.Entries) != 1 || snapshot.Entries[0].ID != "public") {
					t.Fatalf("static permission widened to a future window: %+v", snapshot.Entries)
				}
			}
		}
		if !foundOld || !foundLatest {
			t.Fatalf("missing selected or latest window: %s", response.Body.String())
		}
	}
	check(firstID)
	follow = advanceNativeLive(t, app, follow, "Public second window")
	check(follow.CurrentSnapshotID)
	follow = advanceNativeLive(t, app, follow, "Public third window")
	check(follow.CurrentSnapshotID)
	if _, err := app.store.StopSessionFollow(context.Background(), detail.ID, follow.ID); err != nil {
		t.Fatal(err)
	}
	check(follow.CurrentSnapshotID)
}

func TestNativeLiveStopDoesNotTransferAuthorizationToReplacementFollow(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	detail, follow := readyNativeLive(t, app)
	remote, headers := installReviewShare(t, app, detail.ID, nativeLiveScope(follow), false)
	ctx := context.Background()
	if _, err := app.store.StopSessionFollow(ctx, detail.ID, follow.ID); err != nil {
		t.Fatal(err)
	}
	replacement, _, err := app.store.CreateSessionFollow(ctx, detail.ID, follow.SourceSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	replacement = advanceNativeLive(t, app, replacement, "SECRET replacement Follow")
	path := "/api/v1/threads/" + detail.ID
	response := requestJSON(t, remote, "GET", path, nil, headers)
	if response.Code != 200 || strings.Contains(response.Body.String(), "SECRET") || !strings.Contains(response.Body.String(), `"state":"stopped"`) {
		t.Fatalf("new Follow inherited disclosure: %s", response.Body.String())
	}
	response = requestJSON(t, remote, "GET", path+"/sessions/snapshots/"+replacement.CurrentSnapshotID, nil, headers)
	if response.Code != 404 {
		t.Fatal("replacement snapshot is readable")
	}
}
