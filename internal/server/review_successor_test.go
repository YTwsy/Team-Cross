package server

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"teamcross/internal/domain"
)

func previewSuccessor(t *testing.T, app *App, source threadDetail, input reviewSuccessorRequest) reviewSuccessorPreview {
	t.Helper()
	response := requestJSON(t, app.Handler(), "POST", "/api/v1/threads/"+source.ID+"/successor/preview", input, nil)
	if response.Code != 200 {
		t.Fatalf("successor preview=%d %s", response.Code, response.Body.String())
	}
	var preview reviewSuccessorPreview
	decodeResponse(t, response, &preview)
	return preview
}

func TestReviewSuccessorRetainsSavedContextWithoutNativeReadOrExecution(t *testing.T) {
	ctx := context.Background()
	repo := serverTestRepository(t)
	app := newIntegrationApp(t, repo)
	source := createReviewThread(t, app)
	sourceSnapshot := source.SessionSnapshots[0]
	if _, err := app.store.CreateAnnotation(ctx, domain.Annotation{ThreadID: source.ID, Body: "Carry this exact feedback", Target: &domain.AnnotationTarget{SnapshotID: sourceSnapshot.ID, EntryID: "public"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.CreateAnnotation(ctx, domain.Annotation{ThreadID: source.ID, Body: "UNANCHORED PRIVATE NOTE"}); err != nil {
		t.Fatal(err)
	}
	source, _ = app.buildThreadDetail(ctx, source.ID, access{Mode: "host"})
	beforeRound, _ := app.store.GetRound(ctx, source.Rounds[0].ID)
	beforeManifest, _ := app.store.GetObject(ctx, beforeRound.ManifestObject)
	app.snapshotReader = func(context.Context, string, string) (domain.SessionSnapshot, error) {
		t.Error("successor re-read native history")
		return domain.SessionSnapshot{}, errors.New("source is unavailable")
	}
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("present-day code\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "selected.txt"), []byte("captured explicitly\n"), 0600); err != nil {
		t.Fatal(err)
	}
	beforeStatus := serverTestGit(t, repo, "status", "--porcelain=v2", "-z")
	input := reviewSuccessorRequest{RoundID: source.Rounds[0].ID, Repo: repo, Untracked: []string{"selected.txt"}, Goal: "Work from this review", ExpectedRevision: source.Revision}
	preview := previewSuccessor(t, app, source, input)
	if preview.SnapshotCount != 1 || preview.FeedbackCount != 1 || preview.Baseline == "" || preview.PreviewHash == "" {
		t.Fatalf("incomplete preview: %#v", preview)
	}
	second := previewSuccessor(t, app, source, input)
	if second.PreviewHash != preview.PreviewHash {
		t.Fatal("identical read-only preview digest changed")
	}
	threads, _ := app.store.ListThreads(ctx)
	if len(threads) != 1 {
		t.Fatal("preview created a Thread")
	}
	input.PreviewHash, input.ConfirmSeparateBaseline = preview.PreviewHash, true
	created := requestJSON(t, app.Handler(), "POST", "/api/v1/threads/"+source.ID+"/successors", input, nil)
	if created.Code != 201 {
		t.Fatalf("successor=%d %s", created.Code, created.Body.String())
	}
	var next threadDetail
	decodeResponse(t, created, &next)
	if next.ID == source.ID || next.ReadOnly || next.AgentRun != nil || next.Worktree == "" || len(next.Rounds) != 1 || len(next.SessionSnapshots) != 1 {
		t.Fatalf("incomplete independent successor: %#v", next)
	}
	if next.SessionSnapshots[0].Source.SessionID != sourceSnapshot.Source.SessionID || next.SessionSnapshots[0].ID == sourceSnapshot.ID || next.SessionSnapshots[0].Source.Cwd != "" {
		t.Fatal("snapshot provenance was lost or unsafe cwd was carried")
	}
	if next.SessionSnapshots[0].Capabilities.Follow || next.SessionSnapshots[0].Capabilities.TakeControl || len(next.SessionFollows) != 0 || next.Share != nil || next.ControlLease != nil {
		t.Fatal("successor inherited authority")
	}
	var feedback string
	for _, evidence := range next.Evidence {
		stored, _ := app.store.GetEvidence(ctx, next.ID, evidence.ID)
		data, _ := app.store.GetObject(ctx, stored.ObjectHash)
		feedback += string(data)
	}
	if !strings.Contains(feedback, "Carry this exact feedback") || !strings.Contains(feedback, sourceSnapshot.ID) || strings.Contains(feedback, "UNANCHORED PRIVATE NOTE") {
		t.Fatalf("feedback scope/provenance: %s", feedback)
	}
	old, _ := app.store.GetThread(ctx, source.ID)
	afterRound, _ := app.store.GetRound(ctx, source.Rounds[0].ID)
	afterManifest, _ := app.store.GetObject(ctx, afterRound.ManifestObject)
	if !old.ReadOnly || old.WorktreePath != "" || old.BaselineCommit != "" || afterRound != beforeRound || !bytes.Equal(beforeManifest, afterManifest) {
		t.Fatal("successor rewrote original review")
	}
	if !bytes.Equal(beforeStatus, serverTestGit(t, repo, "status", "--porcelain=v2", "-z")) {
		t.Fatal("successor mutated selected source checkout")
	}
	for _, id := range []string{source.ID, next.ID} {
		runs, err := app.store.ListAgentRuns(ctx, id)
		if err != nil || len(runs) != 0 {
			t.Fatalf("successor created Run: %v %v", runs, err)
		}
	}
	retry := requestJSON(t, app.Handler(), "POST", "/api/v1/threads/"+source.ID+"/successors", input, nil)
	if retry.Code != 409 {
		t.Fatalf("uncertain retry duplicated successor: %s", retry.Body.String())
	}
	// The selected repository can disappear. The successor owns a independent
	// baseline/object database and still enters the explicit new-Session flow.
	if err := os.Rename(repo, repo+"-offline"); err != nil {
		t.Fatal(err)
	}
	log := continuationBridge(t, app, false)
	continued := requestJSON(t, app.Handler(), "POST", "/api/v1/threads/"+next.ID+"/continue", continueRoundRequest{RoundID: next.Rounds[0].ID, Provider: "mock", Prompt: "Explicit next action", ExpectedRevision: &next.Revision}, nil)
	if continued.Code != 201 {
		t.Fatalf("explicit continuation=%d %s", continued.Code, continued.Body.String())
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "runs.send") || !strings.Contains(string(calls), sourceSnapshot.Source.SessionID) || strings.Contains(string(calls), "thread/resume") {
		t.Fatalf("new Session did not receive saved evidence: %s", calls)
	}
}

func TestReviewSuccessorRejectsChangedCaptureAndUnconfirmedRequest(t *testing.T) {
	app := newIntegrationApp(t, serverTestRepository(t))
	source := createReviewThread(t, app)
	input := reviewSuccessorRequest{RoundID: source.Rounds[0].ID, Repo: app.config.Repo, Goal: "Continue", ExpectedRevision: source.Revision}
	preview := previewSuccessor(t, app, source, input)
	path := "/api/v1/threads/" + source.ID + "/successors"
	input.PreviewHash = preview.PreviewHash
	if r := requestJSON(t, app.Handler(), "POST", path, input, nil); r.Code != 400 {
		t.Fatalf("missing consent: %s", r.Body.String())
	}
	input.ConfirmSeparateBaseline = true
	if err := os.WriteFile(filepath.Join(app.config.Repo, "tracked.txt"), []byte("changed after preview\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if r := requestJSON(t, app.Handler(), "POST", path, input, nil); r.Code != 409 || !strings.Contains(r.Body.String(), "successor_preview_changed") {
		t.Fatalf("changed capture accepted: %s", r.Body.String())
	}
	threads, _ := app.store.ListThreads(context.Background())
	if len(threads) != 1 {
		t.Fatal("failed confirmation created Thread")
	}
	stored, _ := app.store.GetThread(context.Background(), source.ID)
	if stored.Revision != source.Revision {
		t.Fatal("rejected preview consumed revision")
	}
}

func TestReviewSuccessorFailurePublishesNoPartialThread(t *testing.T) {
	app := newIntegrationApp(t, serverTestRepository(t))
	source := createReviewThread(t, app)
	input := reviewSuccessorRequest{RoundID: source.Rounds[0].ID, Repo: app.config.Repo, Goal: "Continue", ExpectedRevision: source.Revision}
	preview := previewSuccessor(t, app, source, input)
	input.PreviewHash, input.ConfirmSeparateBaseline = preview.PreviewHash, true
	if _, err := app.store.DB().Exec(`CREATE TRIGGER reject_successor BEFORE INSERT ON events WHEN NEW.event_type='thread.forked' BEGIN SELECT RAISE(FAIL,'fork unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadDir(app.paths.Worktrees)
	r := requestJSON(t, app.Handler(), "POST", "/api/v1/threads/"+source.ID+"/successors", input, nil)
	if r.Code < 400 {
		t.Fatalf("failed import accepted: %s", r.Body.String())
	}
	threads, _ := app.store.ListThreads(context.Background())
	after, _ := os.ReadDir(app.paths.Worktrees)
	if len(threads) != 1 || len(before) != len(after) {
		t.Fatalf("partial successor remains: threads=%d dirs=%d/%d", len(threads), len(before), len(after))
	}
}

func TestReviewSuccessorRejectsRemoteAndExecutableSource(t *testing.T) {
	app := newIntegrationApp(t, serverTestRepository(t))
	source := createReviewThread(t, app)
	remote, headers := installReviewShare(t, app, source.ID, domain.ShareScope{}, false)
	for _, suffix := range []string{"/successor/preview", "/successors"} {
		r := requestJSON(t, remote, http.MethodPost, "/api/v1/threads/"+source.ID+suffix, map[string]any{}, headers)
		if r.Code != 403 {
			t.Fatalf("remote gained repository capture: %d %s", r.Code, r.Body.String())
		}
	}
	executable := captureForContinuation(t, app)
	r := requestJSON(t, app.Handler(), "POST", "/api/v1/threads/"+executable.ID+"/successor/preview", reviewSuccessorRequest{RoundID: executable.Rounds[0].ID, Repo: app.config.Repo, Goal: "Replace baseline", ExpectedRevision: executable.Revision}, nil)
	if r.Code != 422 {
		t.Fatalf("executable baseline replaced: %s", r.Body.String())
	}
}
