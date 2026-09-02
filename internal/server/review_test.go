package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"teamcross/internal/domain"
	"testing"
	"time"
)

func reviewFixture() domain.SessionSnapshot {
	return domain.SessionSnapshot{
		Source:     domain.SessionRef{Provider: "codex", SessionID: "native-conversation", IdentityKind: "thread.id", Surface: "cli", Title: "审阅测试", Cwd: "/private/owner/SECRET-CWD"},
		CapturedAt: time.Now().UTC(), Capabilities: domain.SessionCapabilities{Read: true},
		Entries:  []domain.SessionEntry{{ID: "public", Kind: "message", Role: "assistant", Text: "PUBLIC answer"}, {ID: "private", Kind: "tool", Text: "SECRET tool output"}},
		Warnings: []string{},
	}
}

func createReviewThread(t *testing.T, app *App) threadDetail {
	t.Helper()
	app.snapshotReader = func(context.Context, string, string) (domain.SessionSnapshot, error) { return reviewFixture(), nil }
	result := requestJSON(t, app.Handler(), "POST", "/api/v1/threads/from-session", sessionImportRequest{Provider: "codex", SessionID: "native-conversation"}, nil)
	if result.Code != 201 {
		t.Fatalf("create review: %d %s", result.Code, result.Body.String())
	}
	var detail threadDetail
	decodeResponse(t, result, &detail)
	return detail
}

func installReviewShare(t *testing.T, app *App, threadID string, scope domain.ShareScope, allowControl bool) (http.Handler, map[string]string) {
	t.Helper()
	ctx := context.Background()
	projection, err := app.prepareShareProjection(ctx, threadID, &scope, allowControl)
	if err != nil {
		t.Fatal(err)
	}
	capabilities := []string{"view", "annotate"}
	if allowControl {
		capabilities = append(capabilities, "send", "steer", "interrupt")
	}
	share, err := app.store.CreateShare(ctx, domain.Share{ThreadID: threadID, SecretHash: "test", ServerSPKI: "test", Capabilities: jsonBytes(capabilities), ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err = app.store.SaveShareProjection(ctx, share.ID, jsonBytes(projection)); err != nil {
		t.Fatal(err)
	}
	app.shares[threadID] = &hostedShare{ID: share.ID, ThreadID: threadID, ExpiresAt: share.ExpiresAt}
	return app.remoteHandler(threadID, share.ID), map[string]string{"X-TeamCross-Participant-ID": "reviewer", "X-TeamCross-Participant-Name": "Reviewer"}
}

func TestSessionPreviewImportNeverStartsRun(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	calls := 0
	app.snapshotReader = func(context.Context, string, string) (domain.SessionSnapshot, error) {
		calls++
		return reviewFixture(), nil
	}
	preview := requestJSON(t, app.Handler(), "POST", "/api/v1/sessions/preview", sessionImportRequest{Provider: "codex", SessionID: "native-conversation"}, nil)
	if preview.Code != 200 {
		t.Fatal(preview.Body.String())
	}
	threads, _ := app.store.ListThreads(context.Background())
	if len(threads) != 0 {
		t.Fatal("preview persisted a Thread")
	}
	created := requestJSON(t, app.Handler(), "POST", "/api/v1/threads/from-session", sessionImportRequest{Provider: "codex", SessionID: "native-conversation"}, nil)
	var detail threadDetail
	decodeResponse(t, created, &detail)
	if created.Code != 201 || !detail.ReadOnly || detail.Worktree != "" || len(detail.SessionSnapshots) != 1 || len(detail.Rounds) != 1 {
		t.Fatalf("readonly detail: %s", created.Body.String())
	}
	current := &managedRun{ID: "existing", SessionID: "existing-native", Provider: "codex", Status: "running"}
	app.runs[detail.ID] = current
	imported := requestJSON(t, app.Handler(), "POST", "/api/v1/threads/"+detail.ID+"/sessions/import", sessionImportRequest{Provider: "codex", SessionID: "native-conversation"}, nil)
	if imported.Code != 200 {
		t.Fatalf("import while running: %s", imported.Body.String())
	}
	if app.runs[detail.ID] != current || current.Status != "running" || current.SessionID != "existing-native" {
		t.Fatal("readonly import modified active Run")
	}
	runs, err := app.store.ListAgentRuns(context.Background(), detail.ID)
	if err != nil || len(runs) != 0 || calls != 3 {
		t.Fatalf("import executed work: runs=%v calls=%d err=%v", runs, calls, err)
	}
	decodeResponse(t, imported, &detail)
	if len(detail.Rounds) != 2 || len(detail.SessionSnapshots) != 2 {
		t.Fatal("import did not append immutable snapshot")
	}
	for _, kind := range []string{"agent.switched", "command.send", "turn.started"} {
		assertEventCount(t, app.store, detail.ID, kind, 0)
	}
}

func TestSessionIdentityMismatchCannotCreateThread(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	app.snapshotReader = func(context.Context, string, string) (domain.SessionSnapshot, error) {
		snapshot := reviewFixture()
		snapshot.Source.SessionID = "other"
		return snapshot, nil
	}
	result := requestJSON(t, app.Handler(), "POST", "/api/v1/threads/from-session", sessionImportRequest{Provider: "codex", SessionID: "native-conversation"}, nil)
	if result.Code != 422 {
		t.Fatalf("mismatched identity accepted: %s", result.Body.String())
	}
	threads, _ := app.store.ListThreads(context.Background())
	if len(threads) != 0 {
		t.Fatal("invalid snapshot created Thread")
	}
}

func TestReviewShareScopeCoversDetailDownloadsSSEAndWrites(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	detail := createReviewThread(t, app)
	ctx := context.Background()
	object, err := app.store.PutObject(ctx, []byte("SECRET raw transcript"), "application/json")
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := app.store.CreateEvidence(ctx, domain.Evidence{ThreadID: detail.ID, Kind: "agent_transcript", Title: "SECRET evidence", ObjectHash: object.Hash})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = app.store.CreateAnnotation(ctx, domain.Annotation{ThreadID: detail.ID, Body: "SECRET old unanchored annotation"})
	_, _ = app.store.CreateAnnotation(ctx, domain.Annotation{ThreadID: detail.ID, Body: "SECRET whole snapshot annotation", Target: &domain.AnnotationTarget{SnapshotID: detail.SessionSnapshots[0].ID}})
	_, _ = app.store.DB().ExecContext(ctx, "UPDATE threads SET repo_root='/owner/SECRET-repo',branch='SECRET-branch' WHERE id=?", detail.ID)
	_, _ = app.store.AppendEvent(ctx, detail.ID, "message.completed", jsonBytes(map[string]any{"text": "SECRET Agent event", "revision": 12}))
	remote, headers := installReviewShare(t, app, detail.ID, domain.ShareScope{SnapshotID: detail.SessionSnapshots[0].ID, EntryIDs: []string{"public"}}, false)
	path := "/api/v1/threads/" + detail.ID
	listing := requestJSON(t, remote, "GET", "/api/v1/threads", nil, headers)
	if listing.Code != 200 || strings.Contains(listing.Body.String(), "SECRET") {
		t.Fatalf("list projection leaked: %s", listing.Body.String())
	}
	_, _ = app.store.CreateAnnotation(ctx, domain.Annotation{ThreadID: detail.ID, Body: "SECRET later private Owner comment"})
	result := requestJSON(t, remote, "GET", path, nil, headers)
	if result.Code != 200 || !strings.Contains(result.Body.String(), "PUBLIC answer") || strings.Contains(result.Body.String(), "SECRET") {
		t.Fatalf("projection leaked: %s", result.Body.String())
	}
	for _, route := range []string{path + "/evidence/" + evidence.ID, path + "/patch"} {
		result = requestJSON(t, remote, "GET", route, nil, headers)
		if result.Code != 403 && result.Code != 404 {
			t.Fatalf("download not denied: %s %d", route, result.Code)
		}
	}
	for _, route := range []string{path + "/control", path + "/agent/send", "/api/v1/sessions/preview", path + "/bundles"} {
		result = requestJSON(t, remote, "POST", route, map[string]any{"commandId": "forbidden", "text": "write"}, headers)
		if result.Code != 403 {
			t.Fatalf("write not denied: %s %d %s", route, result.Code, result.Body.String())
		}
	}
	invalid := requestJSON(t, remote, "POST", path+"/annotations", annotationRequest{Body: "not allowed", Target: &domain.AnnotationTarget{SnapshotID: detail.SessionSnapshots[0].ID, EntryID: "private"}, CommandID: "hidden"}, headers)
	if invalid.Code != 422 {
		t.Fatalf("private anchor accepted: %s", invalid.Body.String())
	}
	// Subsequent imports remain private even if they are the newest snapshot.
	later := reviewFixture()
	later.ID = "later"
	later.Entries = []domain.SessionEntry{{ID: "later", Kind: "message", Text: "SECRET later import"}}
	if err = app.captureReviewSnapshot(ctx, detail.ID, later); err != nil {
		t.Fatal(err)
	}
	thread, _ := app.store.GetThread(ctx, detail.ID)
	annotation := annotationRequest{Body: "PUBLIC reviewer feedback", Target: &domain.AnnotationTarget{SnapshotID: detail.SessionSnapshots[0].ID, EntryID: "public"}, CommandID: "annotate-once", ExpectedRevision: thread.Revision}
	posted := requestJSON(t, remote, "POST", path+"/annotations", annotation, headers)
	if posted.Code != 201 {
		t.Fatalf("valid anchor: %s", posted.Body.String())
	}
	replay := requestJSON(t, remote, "POST", path+"/annotations", annotation, headers)
	if replay.Code != 200 || strings.Contains(replay.Body.String(), "SECRET") {
		t.Fatalf("replay leak: %s", replay.Body.String())
	}
	feedback := requestJSON(t, app.Handler(), "GET", path+"/feedback", nil, nil)
	if feedback.Code != 200 || !strings.Contains(feedback.Body.String(), "entry=public") || !strings.Contains(feedback.Body.String(), "PUBLIC answer") {
		t.Fatalf("feedback missing stable citation: %s", feedback.Body.String())
	}
	streamCtx, cancel := context.WithTimeout(ctx, 80*time.Millisecond)
	defer cancel()
	request := httptest.NewRequest("GET", path+"/events", nil).WithContext(streamCtx)
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	stream := httptest.NewRecorder()
	remote.ServeHTTP(stream, request)
	if stream.Code != 200 || strings.Contains(stream.Body.String(), "SECRET") || !strings.Contains(stream.Body.String(), "thread.updated") {
		t.Fatalf("SSE projection leaked: %s", stream.Body.String())
	}
}

func TestReviewShareRejectsRawTranscriptAndImplicitControl(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	detail := createReviewThread(t, app)
	ctx := context.Background()
	object, _ := app.store.PutObject(ctx, []byte("raw"), "application/json")
	evidence, _ := app.store.CreateEvidence(ctx, domain.Evidence{ThreadID: detail.ID, Kind: "agent_transcript", Title: "raw", ObjectHash: object.Hash})
	if _, err := app.prepareShareProjection(ctx, detail.ID, &domain.ShareScope{EvidenceIDs: []string{evidence.ID}}, false); err == nil {
		t.Fatal("raw transcript bypass allowed")
	}
	if _, err := app.prepareShareProjection(ctx, detail.ID, &domain.ShareScope{}, true); err == nil {
		t.Fatal("control granted without live-output consent")
	}
	if _, err := app.prepareShareProjection(ctx, detail.ID, &domain.ShareScope{IncludeEvents: true}, true); err == nil {
		t.Fatal("control granted without code consent")
	}
}

func TestActiveShareCannotBeImplicitlyRescoped(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	detail := createReviewThread(t, app)
	installReviewShare(t, app, detail.ID, domain.ShareScope{SnapshotID: detail.SessionSnapshots[0].ID, EntryIDs: []string{"public"}}, false)
	result := requestJSON(t, app.Handler(), "POST", "/api/v1/threads/"+detail.ID+"/shares", map[string]any{"scope": domain.ShareScope{}}, nil)
	if result.Code != http.StatusConflict || !strings.Contains(result.Body.String(), "share_active") {
		t.Fatalf("active Share rescope should require explicit revoke: %s", result.Body.String())
	}
}

func TestSharedCodeComesOnlyFromSealedRound(t *testing.T) {
	repo := serverTestRepository(t)
	if err := os.WriteFile(filepath.Join(repo, "SECRET-omitted.txt"), []byte("not selected"), 0600); err != nil {
		t.Fatal(err)
	}
	app := newIntegrationApp(t, repo)
	result := requestJSON(t, app.Handler(), "POST", "/api/v1/threads", createThreadRequest{Repo: repo, Title: "Code", Goal: "Capture"}, nil)
	var detail threadDetail
	decodeResponse(t, result, &detail)
	if result.Code != 201 {
		t.Fatal(result.Body.String())
	}
	if err := os.WriteFile(filepath.Join(detail.Worktree, "tracked.txt"), []byte("SECRET-UNSEALED-WORK"), 0600); err != nil {
		t.Fatal(err)
	}
	remote, headers := installReviewShare(t, app, detail.ID, domain.ShareScope{IncludeCode: true}, false)
	before := requestJSON(t, remote, "GET", "/api/v1/threads/"+detail.ID+"/patch", nil, headers)
	// A new event can never widen the captured code projection.
	_, _ = app.store.AppendEvent(context.Background(), detail.ID, "file.changed", jsonBytes(map[string]string{"path": "SECRET-later-file"}))
	after := requestJSON(t, remote, "GET", "/api/v1/threads/"+detail.ID, nil, headers)
	if before.Code != 200 || strings.Contains(before.Body.String(), "SECRET") || strings.Contains(after.Body.String(), "SECRET") {
		t.Fatal("code projection expanded")
	}
}

func TestImportedSessionsSurviveLaterCheckpoint(t *testing.T) {
	repo := serverTestRepository(t)
	app := newIntegrationApp(t, repo)
	created := requestJSON(t, app.Handler(), "POST", "/api/v1/threads", createThreadRequest{Repo: repo, Title: "Context", Goal: "Preserve history"}, nil)
	var detail threadDetail
	decodeResponse(t, created, &detail)
	ctx := context.Background()
	for _, id := range []string{"snapshot-A", "snapshot-B"} {
		snapshot := reviewFixture()
		snapshot.ID = id
		if err := app.captureReviewSnapshot(ctx, detail.ID, snapshot); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := app.store.CreateAgentRun(ctx, domain.AgentRun{ThreadID: detail.ID, Provider: "mock", Status: "idle"})
	if err != nil {
		t.Fatal(err)
	}
	run := &managedRun{ID: stored.ID, ThreadID: detail.ID, Provider: "mock", Status: "idle"}
	completed, err := app.store.AppendEvent(ctx, detail.ID, "turn.completed", jsonBytes(map[string]string{"runId": run.ID, "turnId": "turn", "status": "completed"}))
	if err != nil {
		t.Fatal(err)
	}
	app.sealCompletedTurn(run, "turn", completed, map[string]any{"status": "completed"})
	rounds, err := app.store.ListRounds(ctx, detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := app.latestSealedSessionIDs(ctx, rounds)
	if err != nil || len(ids) != 2 || ids[0] != "snapshot-A" || ids[1] != "snapshot-B" {
		t.Fatalf("lost imported context: %v %v", ids, err)
	}
	bundle, err := app.bundleForContinuation(ctx, detail.ID, rounds[len(rounds)-1].ID)
	if err != nil || len(bundle.SessionSnapshots) != 2 {
		t.Fatalf("continuation context lost: %v %v", bundle.SessionSnapshots, err)
	}
}
