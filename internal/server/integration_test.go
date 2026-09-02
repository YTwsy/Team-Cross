package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"teamcross/internal/domain"
	"teamcross/internal/platform"
	"teamcross/internal/storage"
)

func TestCaptureAnnotationLeaseAndCommandIdempotency(t *testing.T) {
	ctx := context.Background()
	repo := serverTestRepository(t)
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("work in progress\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "handoff.txt"), []byte("parser is blocked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	statusBefore := serverTestGit(t, repo, "status", "--porcelain=v2", "-z", "--untracked-files=all")

	app := newIntegrationApp(t, repo)
	host := app.Handler()
	createdResponse := requestJSON(t, host, http.MethodPost, "/api/v1/threads", createThreadRequest{
		Repo: repo, Title: "Parser handoff", Goal: "Finish the parser",
		Progress: "Reproduction is captured", Blocker: "Unexpected token",
		Tried: "Adjusted precedence", Questions: "Which branch is correct?",
		Untracked: []string{"handoff.txt"},
	}, nil)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create thread status = %d, body = %s", createdResponse.Code, createdResponse.Body.String())
	}
	var detail threadDetail
	decodeResponse(t, createdResponse, &detail)
	if detail.ID == "" || detail.Revision != 2 || len(detail.Rounds) != 1 {
		t.Fatalf("created detail = %#v", detail)
	}
	if detail.Worktree == "" || detail.Worktree == repo {
		t.Fatalf("isolated worktree = %q", detail.Worktree)
	}
	assertFileContent(t, filepath.Join(detail.Worktree, "tracked.txt"), "work in progress\n")
	assertFileContent(t, filepath.Join(detail.Worktree, "handoff.txt"), "parser is blocked\n")
	statusAfter := serverTestGit(t, repo, "status", "--porcelain=v2", "-z", "--untracked-files=all")
	if !bytes.Equal(statusBefore, statusAfter) {
		t.Fatalf("original worktree changed\nbefore: %q\nafter:  %q", statusBefore, statusAfter)
	}

	hostAnnotation := requestJSON(t, host, http.MethodPost,
		"/api/v1/threads/"+detail.ID+"/annotations",
		annotationRequest{Body: "Owner note", File: "tracked.txt", Line: 1}, nil)
	if hostAnnotation.Code != http.StatusCreated {
		t.Fatalf("host annotation status = %d, body = %s", hostAnnotation.Code, hostAnnotation.Body.String())
	}
	decodeResponse(t, hostAnnotation, &detail)
	if len(detail.Annotations) != 1 || detail.Revision != 3 {
		t.Fatalf("detail after owner annotation = %#v", detail)
	}

	shareID := "share-integration"
	_, err := app.store.CreateShare(ctx, domain.Share{
		ID: shareID, ThreadID: detail.ID, SecretHash: "secret", ServerSPKI: "pin",
		Capabilities: []byte(`["view","annotate","send"]`), ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	app.shares[detail.ID] = &hostedShare{ID: shareID, ThreadID: detail.ID, Token: "test", ExpiresAt: time.Now().Add(time.Hour), Transports: []string{"lan"}}
	remote := app.remoteHandler(detail.ID, shareID)
	headers := map[string]string{
		"X-TeamCross-Participant-ID":   "participant-one",
		"X-TeamCross-Participant-Name": "Reviewer",
		"X-TeamCross-Transport":        "lan",
	}

	controlBody := controlRequest{Action: "request", CommandID: "control-request-1", ExpectedRevision: detail.Revision}
	control := requestJSON(t, remote, http.MethodPost, "/api/v1/threads/"+detail.ID+"/control", controlBody, headers)
	if control.Code != http.StatusOK {
		t.Fatalf("control request status = %d, body = %s", control.Code, control.Body.String())
	}
	decodeResponse(t, control, &detail)
	lease, err := app.store.GetControlLease(ctx, shareID)
	if err != nil {
		t.Fatal(err)
	}
	if lease.ParticipantID != "participant-one" || lease.Epoch < 1 {
		t.Fatalf("control lease = %#v", lease)
	}
	if detail.Revision != 4 {
		t.Fatalf("revision after control = %d, want 4", detail.Revision)
	}

	duplicateControl := requestJSON(t, remote, http.MethodPost, "/api/v1/threads/"+detail.ID+"/control", controlBody, headers)
	if duplicateControl.Code != http.StatusOK {
		t.Fatalf("duplicate control status = %d, body = %s", duplicateControl.Code, duplicateControl.Body.String())
	}
	leaseAfterDuplicate, err := app.store.GetControlLease(ctx, shareID)
	if err != nil {
		t.Fatal(err)
	}
	if leaseAfterDuplicate.Epoch != lease.Epoch || !leaseAfterDuplicate.ExpiresAt.Equal(lease.ExpiresAt) {
		t.Fatalf("duplicate changed lease: before=%#v after=%#v", lease, leaseAfterDuplicate)
	}
	assertEventCount(t, app.store, detail.ID, "control.acquired", 1)
	command, err := app.store.GetCommand(ctx, shareID, controlBody.CommandID)
	if err != nil || command.Status != "completed" {
		t.Fatalf("control command = %#v, err=%v", command, err)
	}

	invalid := requestJSON(t, remote, http.MethodPost, "/api/v1/threads/"+detail.ID+"/control",
		controlRequest{Action: "delete-everything", CommandID: "invalid-action", ExpectedRevision: detail.Revision}, headers)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid action status = %d, body = %s", invalid.Code, invalid.Body.String())
	}
	if _, err := app.store.GetCommand(ctx, shareID, "invalid-action"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("invalid action was persisted as a running command: %v", err)
	}

	renewBody := controlRequest{Action: "renew", CommandID: "control-renew-1", ExpectedRevision: detail.Revision, LeaseEpoch: lease.Epoch}
	renew := requestJSON(t, remote, http.MethodPost, "/api/v1/threads/"+detail.ID+"/control", renewBody, headers)
	if renew.Code != http.StatusOK {
		t.Fatalf("renew status = %d, body = %s", renew.Code, renew.Body.String())
	}
	staleRenewBody := controlRequest{Action: "renew", CommandID: "control-renew-stale", ExpectedRevision: detail.Revision - 1, LeaseEpoch: lease.Epoch}
	staleRenew := requestJSON(t, remote, http.MethodPost, "/api/v1/threads/"+detail.ID+"/control", staleRenewBody, headers)
	if staleRenew.Code != http.StatusConflict {
		t.Fatalf("stale renew status = %d, body = %s", staleRenew.Code, staleRenew.Body.String())
	}
	staleRenewReplay := requestJSON(t, remote, http.MethodPost, "/api/v1/threads/"+detail.ID+"/control", staleRenewBody, headers)
	if staleRenewReplay.Code != http.StatusConflict || !strings.Contains(staleRenewReplay.Body.String(), "command_failed") {
		t.Fatalf("failed command replay status = %d, body = %s", staleRenewReplay.Code, staleRenewReplay.Body.String())
	}

	remoteAnnotationBody := annotationRequest{
		Body: "Reviewer note", File: "tracked.txt", Line: 1,
		CommandID: "annotation-1", ExpectedRevision: detail.Revision,
	}
	remoteAnnotation := requestJSON(t, remote, http.MethodPost,
		"/api/v1/threads/"+detail.ID+"/annotations", remoteAnnotationBody, headers)
	if remoteAnnotation.Code != http.StatusCreated {
		t.Fatalf("remote annotation status = %d, body = %s", remoteAnnotation.Code, remoteAnnotation.Body.String())
	}
	decodeResponse(t, remoteAnnotation, &detail)
	duplicateAnnotation := requestJSON(t, remote, http.MethodPost,
		"/api/v1/threads/"+detail.ID+"/annotations", remoteAnnotationBody, headers)
	if duplicateAnnotation.Code != http.StatusOK {
		t.Fatalf("duplicate annotation status = %d, body = %s", duplicateAnnotation.Code, duplicateAnnotation.Body.String())
	}
	annotations, err := app.store.ListAnnotations(ctx, detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(annotations) != 2 {
		t.Fatalf("annotations after duplicate = %#v", annotations)
	}
	conflictingAnnotation := remoteAnnotationBody
	conflictingAnnotation.Body = "different payload with the same command id"
	conflict := requestJSON(t, remote, http.MethodPost,
		"/api/v1/threads/"+detail.ID+"/annotations", conflictingAnnotation, headers)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflicting command status = %d, body = %s", conflict.Code, conflict.Body.String())
	}

	currentThread, err := app.store.GetThread(ctx, detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	releaseBody := controlRequest{Action: "release", CommandID: "control-release-1", ExpectedRevision: currentThread.Revision, LeaseEpoch: lease.Epoch}
	release := requestJSON(t, remote, http.MethodPost, "/api/v1/threads/"+detail.ID+"/control", releaseBody, headers)
	if release.Code != http.StatusOK {
		t.Fatalf("release status = %d, body = %s", release.Code, release.Body.String())
	}
	if err := app.store.ValidateControl(ctx, shareID, "participant-one", lease.Epoch, time.Now()); !errors.Is(err, domain.ErrLeaseFence) {
		t.Fatalf("released lease remained valid: %v", err)
	}
	duplicateRelease := requestJSON(t, remote, http.MethodPost, "/api/v1/threads/"+detail.ID+"/control", releaseBody, headers)
	if duplicateRelease.Code != http.StatusOK {
		t.Fatalf("duplicate release status = %d, body = %s", duplicateRelease.Code, duplicateRelease.Body.String())
	}
	assertEventCount(t, app.store, detail.ID, "control.released", 1)

	currentThread, err = app.store.GetThread(ctx, detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	reacquireBody := controlRequest{Action: "request", CommandID: "control-request-2", ExpectedRevision: currentThread.Revision}
	reacquire := requestJSON(t, remote, http.MethodPost, "/api/v1/threads/"+detail.ID+"/control", reacquireBody, headers)
	if reacquire.Code != http.StatusOK {
		t.Fatalf("reacquire status = %d, body = %s", reacquire.Code, reacquire.Body.String())
	}
	reacquiredLease, err := app.store.GetControlLease(ctx, shareID)
	if err != nil {
		t.Fatal(err)
	}
	currentThread, err = app.store.GetThread(ctx, detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	ownerRevoke := requestJSON(t, host, http.MethodPost, "/api/v1/threads/"+detail.ID+"/control",
		controlRequest{Action: "revoke", ExpectedRevision: currentThread.Revision}, nil)
	if ownerRevoke.Code != http.StatusOK {
		t.Fatalf("owner revoke status = %d, body = %s", ownerRevoke.Code, ownerRevoke.Body.String())
	}
	if err := app.store.ValidateControl(ctx, shareID, "participant-one", reacquiredLease.Epoch, time.Now()); !errors.Is(err, domain.ErrLeaseFence) {
		t.Fatalf("owner did not fence remote controller: %v", err)
	}
	assertEventCount(t, app.store, detail.ID, "control.revoked", 1)
}

func TestThreadCollectionsSerializeAsArrays(t *testing.T) {
	repo := serverTestRepository(t)
	app := newIntegrationApp(t, repo)
	thread, err := app.store.CreateThread(context.Background(), domain.Thread{
		Title: "Empty thread", RepoRoot: repo, Branch: "main", ReadOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	response := requestJSON(t, app.Handler(), http.MethodGet, "/api/v1/threads/"+thread.ID, nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("get empty thread status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload map[string]json.RawMessage
	decodeResponse(t, response, &payload)
	for _, field := range []string{"rounds", "events", "annotations", "evidence", "participants"} {
		if got := string(payload[field]); got != "[]" {
			t.Errorf("%s = %s, want []", field, got)
		}
	}
	var gitPayload map[string]json.RawMessage
	if err := json.Unmarshal(payload["git"], &gitPayload); err != nil {
		t.Fatal(err)
	}
	if got := string(gitPayload["untracked"]); got != "[]" {
		t.Errorf("git.untracked = %s, want []", got)
	}

	preview := requestJSON(t, app.Handler(), http.MethodPost, "/api/v1/capture/preview", map[string]string{"repo": repo}, nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("capture preview status = %d, body = %s", preview.Code, preview.Body.String())
	}
	var previewPayload map[string]json.RawMessage
	decodeResponse(t, preview, &previewPayload)
	if got := string(previewPayload["untracked"]); got != "[]" {
		t.Errorf("capture untracked = %s, want []", got)
	}
}

func TestEvidenceAttachmentAndScopedDownload(t *testing.T) {
	ctx := context.Background()
	repo := serverTestRepository(t)
	app := newIntegrationApp(t, repo)
	thread, err := app.store.CreateThread(ctx, domain.Thread{
		Title: "Evidence thread", RepoRoot: repo, Branch: "main", ReadOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	content := []byte{0x00, 0x01, 0x02, 0xff}
	created := requestJSON(t, app.Handler(), http.MethodPost,
		"/api/v1/threads/"+thread.ID+"/evidence", map[string]any{
			"kind": "file", "name": "capture.bin", "mimeType": "application/octet-stream",
			"contentBase64": base64.StdEncoding.EncodeToString(content),
		}, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("attach evidence status = %d, body = %s", created.Code, created.Body.String())
	}
	var detail threadDetail
	decodeResponse(t, created, &detail)
	if len(detail.Evidence) != 1 || detail.Evidence[0].MIMEType != "application/octet-stream" {
		t.Fatalf("evidence detail = %#v", detail.Evidence)
	}
	download := httptest.NewRecorder()
	downloadRequest := httptest.NewRequest(http.MethodGet,
		"/api/v1/threads/"+thread.ID+"/evidence/"+detail.Evidence[0].ID+"?download=1", nil)
	app.Handler().ServeHTTP(download, downloadRequest)
	if download.Code != http.StatusOK || !bytes.Equal(download.Body.Bytes(), content) {
		t.Fatalf("evidence download status=%d body=%v", download.Code, download.Body.Bytes())
	}
	if !strings.Contains(download.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("evidence disposition = %q", download.Header().Get("Content-Disposition"))
	}
	other, err := app.store.CreateThread(ctx, domain.Thread{
		Title: "Other thread", RepoRoot: repo, Branch: "main", ReadOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	wrongThread := httptest.NewRecorder()
	wrongRequest := httptest.NewRequest(http.MethodGet,
		"/api/v1/threads/"+other.ID+"/evidence/"+detail.Evidence[0].ID, nil)
	app.Handler().ServeHTTP(wrongThread, wrongRequest)
	if wrongThread.Code != http.StatusNotFound {
		t.Fatalf("cross-thread evidence status = %d", wrongThread.Code)
	}
}

func TestPersistedParticipantMetadataAndExpiredLeasePresentation(t *testing.T) {
	ctx := context.Background()
	repo := serverTestRepository(t)
	app := newIntegrationApp(t, repo)
	thread, err := app.store.CreateThread(ctx, domain.Thread{
		Title: "Participant history", RepoRoot: repo, Branch: "main", ReadOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	share, err := app.store.CreateShare(ctx, domain.Share{
		ID: "historical-share", ThreadID: thread.ID, SecretHash: "hash", ServerSPKI: "pin",
		Capabilities: []byte(`[]`), ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	participant, err := app.store.UpsertParticipant(ctx, domain.Participant{
		ID: "persisted-reviewer", ShareID: share.ID, Name: "Persistent Reviewer",
		Role: persistedParticipantRole("tailcat"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.CreateAnnotation(ctx, domain.Annotation{
		ThreadID: thread.ID, ParticipantID: participant.ID, Body: "Remember my author",
	}); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * time.Minute)
	if _, err := app.store.PreemptControl(ctx, share.ID, participant.ID, past, time.Second); err != nil {
		t.Fatal(err)
	}
	app.shares[thread.ID] = &hostedShare{
		ID: share.ID, ThreadID: thread.ID, Token: "test", ExpiresAt: share.ExpiresAt,
		Transports: []string{"tailcat"},
	}

	detail, err := app.buildThreadDetail(ctx, thread.ID, access{Mode: "host", Role: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if detail.ControlLease != nil {
		t.Fatalf("expired control lease exposed as active: %#v", detail.ControlLease)
	}
	if len(detail.Participants) != 1 || detail.Participants[0].Role != "observer" || detail.Participants[0].Transport != "tailcat" {
		t.Fatalf("persisted participant view = %#v", detail.Participants)
	}
	if len(detail.Annotations) != 1 || detail.Annotations[0].Author != participant.Name {
		t.Fatalf("persisted annotation author = %#v", detail.Annotations)
	}

	// Annotation attribution comes from persisted participants, not only the
	// current in-memory Share runtime.
	delete(app.shares, thread.ID)
	detail, err = app.buildThreadDetail(ctx, thread.ID, access{Mode: "host", Role: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Annotations) != 1 || detail.Annotations[0].Author != participant.Name {
		t.Fatalf("annotation author after runtime loss = %#v", detail.Annotations)
	}
}

func newIntegrationApp(t *testing.T, repo string) *App {
	t.Helper()
	paths, err := platform.DataPaths(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(context.Background(), paths.Root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	app := &App{
		config: Config{Repo: repo, DataDir: paths.Root, Version: "test"}, paths: paths,
		store: store, logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		runs: make(map[string]*managedRun), shares: make(map[string]*hostedShare),
		runsByID: make(map[string]*managedRun), switching: make(map[string]bool),
		agentOps: make(map[string]int), shareByID: make(map[string]*hostedShare),
		handoff: make(map[string]*handoffWaiter),
	}
	app.api = app.routes()
	return app
}

func requestJSON(t *testing.T, handler http.Handler, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), destination); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, response.Body.String())
	}
}

func serverTestRepository(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	serverTestGit(t, repo, "init", "-b", "main")
	serverTestGit(t, repo, "config", "user.name", "Team Cross Test")
	serverTestGit(t, repo, "config", "user.email", "teamcross@example.invalid")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("baseline\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	serverTestGit(t, repo, "add", "tracked.txt")
	serverTestGit(t, repo, "commit", "-m", "baseline")
	return repo
}

func serverTestGit(t *testing.T, repo string, args ...string) []byte {
	t.Helper()
	commandArgs := append([]string{"-C", repo}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return output
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

func assertEventCount(t *testing.T, store *storage.Store, threadID, eventType string, want int) {
	t.Helper()
	events, err := store.EventsAfter(context.Background(), threadID, 0, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	got := 0
	for _, event := range events {
		if event.Type == eventType {
			got++
		}
	}
	if got != want {
		t.Fatalf("%s event count = %d, want %d", eventType, got, want)
	}
}
