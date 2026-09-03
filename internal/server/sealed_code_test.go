package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"teamcross/internal/domain"
)

func TestSealedCodeParserKeepsExactPathsSidesAndHunkBoundaries(t *testing.T) {
	patch := "diff --git a/before.txt b/after.txt\n--- a/before.txt\n+++ b/after.txt\n@@ -1,2 +1,2 @@\n--- a/not-a-header\n+++ b/not-a-header\n stable\ndiff --git a/binary b/binary\nGIT binary patch\nliteral 4\nAXXX\n\ndiff --git a/space name.txt b/space name.txt\n--- a/space name.txt\t\n+++ b/space name.txt\t\n@@ -1 +1 @@\n-old\n+new\ndiff --git \"a/\\303\\251.txt\" \"b/\\303\\251.txt\"\n--- \"a/\\303\\251.txt\"\n+++ \"b/\\303\\251.txt\"\n@@ -1 +1 @@\n-a\n+b\n"
	lines, err := parseSealedCode(patch)
	if err != nil {
		t.Fatal(err)
	}
	view := sealedCodeView{Lines: lines}
	for _, item := range []struct{ file, side, text string }{{"before.txt", "old", "--- a/not-a-header"}, {"after.txt", "new", "+++ b/not-a-header"}, {"space name.txt", "new", "+new"}, {"é.txt", "old", "-a"}} {
		index, err := view.anchorIndex(item.file, item.side, 1)
		if err != nil || view.Lines[index].Text != item.text {
			t.Fatalf("wrong anchor for %+v: %v %+v", item, err, view.Lines)
		}
	}
	if _, err := view.anchorIndex("binary", "new", 1); err == nil {
		t.Fatal("binary payload became a source line")
	}
	if _, err := view.anchorIndex("after.txt", "old", 1); err == nil {
		t.Fatal("rename destination silently replaced old path")
	}
	for _, invalid := range []string{
		"--- a/../outside\n+++ b/file\n@@ -1 +1 @@\n-a\n+b\n",
		"--- a/.git/config\n+++ b/file\n@@ -1 +1 @@\n-a\n+b\n",
		"--- a/file\n+++ b/file\n@@ -1,2 +1,2 @@\n-a\n+b\n",
		"--- a/file\n+++ b/file\n@@ -0 +1 @@\n-a\n+b\n",
	} {
		if _, err := parseSealedCode(invalid); err == nil {
			t.Fatalf("invalid patch accepted: %q", invalid)
		}
	}
}

func TestSealedCodeReviewAndAnnotationsNeverFollowMutableWorktree(t *testing.T) {
	ctx := context.Background()
	repo := serverTestRepository(t)
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("sealed first\nsealed second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := newIntegrationApp(t, repo)
	detail := captureForContinuation(t, app)
	roundID := detail.Rounds[0].ID
	path := "/api/v1/threads/" + detail.ID
	read := requestJSON(t, app.Handler(), "GET", path+"/rounds/"+roundID+"/code", nil, nil)
	var initial sealedCodeView
	decodeResponse(t, read, &initial)
	if read.Code != 200 || initial.RoundID != roundID || initial.Baseline != detail.Git.Head || !strings.Contains(initial.Patch, "+sealed second") {
		t.Fatal(read.Body.String())
	}
	if err := os.WriteFile(filepath.Join(detail.Worktree, "tracked.txt"), []byte("LATER PRIVATE WORK\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	read = requestJSON(t, app.Handler(), "GET", path+"/rounds/"+roundID+"/code", nil, nil)
	var after sealedCodeView
	decodeResponse(t, read, &after)
	if !reflect.DeepEqual(initial, after) {
		t.Fatal("sealed review changed with worktree")
	}
	for _, side := range []string{"old", "new"} {
		posted := requestJSON(t, app.Handler(), "POST", path+"/annotations", annotationRequest{Body: "review " + side, File: "tracked.txt", Line: 1, Target: &domain.AnnotationTarget{RoundID: roundID, Side: side}}, nil)
		if posted.Code != 201 {
			t.Fatal(posted.Body.String())
		}
	}
	for _, input := range []annotationRequest{
		{File: "tracked.txt", Line: 1},
		{File: "tracked.txt", Line: 1, Target: &domain.AnnotationTarget{RoundID: roundID}},
		{File: "tracked.txt", Line: 1, Target: &domain.AnnotationTarget{RoundID: roundID, Side: "both"}},
		{File: "tracked.txt", Line: 99, Target: &domain.AnnotationTarget{RoundID: roundID, Side: "new"}},
		{File: "tracked.txt", Line: 2, Target: &domain.AnnotationTarget{RoundID: roundID, Side: "old"}},
		{File: "../tracked.txt", Line: 1, Target: &domain.AnnotationTarget{RoundID: roundID, Side: "new"}},
		{File: "missing.txt", Line: 1, Target: &domain.AnnotationTarget{RoundID: roundID, Side: "new"}},
		{Line: 1, Target: &domain.AnnotationTarget{RoundID: roundID, Side: "new"}},
	} {
		input.Body = "invalid code target"
		response := requestJSON(t, app.Handler(), "POST", path+"/annotations", input, nil)
		if response.Code != 422 {
			t.Fatalf("invalid target accepted: %+v %s", input, response.Body.String())
		}
	}
	legacy, err := app.store.CreateAnnotation(ctx, domain.Annotation{ThreadID: detail.ID, Path: "tracked.txt", StartLine: 1, Body: "Legacy, do not re-anchor"})
	if err != nil {
		t.Fatal(err)
	}
	feedback := requestJSON(t, app.Handler(), "GET", path+"/feedback", nil, nil)
	for _, expected := range []string{"Round=" + roundID, "baseline=" + detail.Git.Head, "side=old", "side=new", "baseline", "sealed first", "未自动归锚"} {
		if !strings.Contains(feedback.Body.String(), expected) {
			t.Fatalf("feedback missing %q: %s", expected, feedback.Body.String())
		}
	}
	if strings.Contains(feedback.Body.String(), "LATER PRIVATE WORK") {
		t.Fatal("feedback quoted mutable worktree")
	}
	annotations, _ := app.store.ListAnnotations(ctx, detail.ID)
	for _, annotation := range annotations {
		if annotation.ID == legacy.ID && annotation.Target != nil {
			t.Fatal("legacy annotation was rewritten")
		}
	}
	runs, _ := app.store.ListAgentRuns(ctx, detail.ID)
	if len(runs) != 0 {
		t.Fatal("read-only code review created an Agent Run")
	}
	assertFileContent(t, filepath.Join(repo, "tracked.txt"), "sealed first\nsealed second\n")
}

func TestSealedCodeShareLimitsRoundsAndPreservesAuthorizedFeedback(t *testing.T) {
	ctx := context.Background()
	repo := serverTestRepository(t)
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("PUBLIC sealed code\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := newIntegrationApp(t, repo)
	detail := captureForContinuation(t, app)
	roundID := detail.Rounds[0].ID
	path := "/api/v1/threads/" + detail.ID
	for _, side := range []string{"old", "new"} {
		response := requestJSON(t, app.Handler(), "POST", path+"/annotations", annotationRequest{Body: "PUBLIC " + side, File: "tracked.txt", Line: 1, Target: &domain.AnnotationTarget{RoundID: roundID, Side: side}}, nil)
		if response.Code != 201 {
			t.Fatal(response.Body.String())
		}
	}
	remote, headers := installReviewShare(t, app, detail.ID, domain.ShareScope{IncludeCode: true}, false)
	if err := os.WriteFile(filepath.Join(detail.Worktree, "tracked.txt"), []byte("SECRET later code\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.captureReviewSnapshot(ctx, detail.ID, reviewFixture()); err != nil {
		t.Fatal(err)
	}
	rounds, _ := app.store.ListRounds(ctx, detail.ID)
	for _, id := range []string{rounds[len(rounds)-1].ID, "other-round"} {
		response := requestJSON(t, remote, "GET", path+"/rounds/"+id+"/code", nil, headers)
		if response.Code != 404 || strings.Contains(response.Body.String(), "SECRET") {
			t.Fatalf("unshared Round readable: %s", response.Body.String())
		}
		response = requestJSON(t, remote, "POST", path+"/annotations", annotationRequest{Body: "not allowed", File: "tracked.txt", Line: 1, Target: &domain.AnnotationTarget{RoundID: id, Side: "new"}, CommandID: "deny-" + id}, headers)
		if response.Code != 422 {
			t.Fatal(response.Body.String())
		}
	}
	response := requestJSON(t, remote, "GET", path+"/rounds/"+roundID+"/code", nil, headers)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "PUBLIC sealed code") || strings.Contains(response.Body.String(), "SECRET") {
		t.Fatal(response.Body.String())
	}
	thread, _ := app.store.GetThread(ctx, detail.ID)
	response = requestJSON(t, remote, "POST", path+"/annotations", annotationRequest{Body: "PUBLIC remote code review", File: "tracked.txt", Line: 1, Target: &domain.AnnotationTarget{RoundID: roundID, Side: "new"}, CommandID: "remote-code", ExpectedRevision: thread.Revision}, headers)
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	var shared threadDetail
	decodeResponse(t, response, &shared)
	if len(shared.Annotations) != 3 {
		t.Fatalf("pre-share code annotations were lost: %s", response.Body.String())
	}
	feedback := app.reviewFeedback(ctx, shared, access{Mode: "share", ShareID: app.shares[detail.ID].ID})
	if !strings.Contains(feedback, "PUBLIC sealed code") || strings.Contains(feedback, "SECRET") {
		t.Fatal(feedback)
	}
	response = requestJSON(t, remote, http.MethodGet, path+"/feedback", nil, headers)
	if response.Code != 403 {
		t.Fatal("owner-only feedback export was exposed")
	}
	if _, err := app.store.DB().ExecContext(ctx, "UPDATE shares SET revoked_at=? WHERE id=?", 1, app.shares[detail.ID].ID); err != nil {
		t.Fatal(err)
	}
	response = requestJSON(t, remote, "GET", path+"/rounds/"+roundID+"/code", nil, headers)
	if response.Code != 403 {
		t.Fatal("revoked code remained accessible")
	}
	remote, headers = installReviewShare(t, app, detail.ID, domain.ShareScope{}, false)
	response = requestJSON(t, remote, "GET", path+"/rounds/"+roundID+"/code", nil, headers)
	if response.Code != 404 {
		t.Fatal("includeCode=false granted code access")
	}
}

func TestReviewFeedbackQuotesOnlyAuthorizedEvidenceWithSafeBoundedText(t *testing.T) {
	ctx := context.Background()
	app := newIntegrationApp(t, t.TempDir())
	detail := createReviewThread(t, app)
	text := "PUBLIC evidence\n```\n<img src=\"https://invalid.example/never-fetch\">\n" + strings.Repeat("long text ", 500)
	object, _ := app.store.PutObject(ctx, []byte(text), "text/plain")
	item, _ := app.store.CreateEvidence(ctx, domain.Evidence{ThreadID: detail.ID, Kind: "log", Title: "PUBLIC diagnostic", ObjectHash: object.Hash, Metadata: []byte(`{"mimeType":"text/plain"}`)})
	privateObject, _ := app.store.PutObject(ctx, []byte("SECRET unshared evidence"), "text/plain")
	private, _ := app.store.CreateEvidence(ctx, domain.Evidence{ThreadID: detail.ID, Kind: "note", Title: "SECRET title", ObjectHash: privateObject.Hash})
	_, _ = app.store.CreateAnnotation(ctx, domain.Annotation{ThreadID: detail.ID, Body: "PUBLIC evidence comment", Target: &domain.AnnotationTarget{EvidenceID: item.ID}})
	_, _ = app.store.CreateAnnotation(ctx, domain.Annotation{ThreadID: detail.ID, Body: "SECRET comment", Target: &domain.AnnotationTarget{EvidenceID: private.ID}})
	remote, headers := installReviewShare(t, app, detail.ID, domain.ShareScope{EvidenceIDs: []string{item.ID}}, false)
	response := requestJSON(t, remote, "GET", "/api/v1/threads/"+detail.ID, nil, headers)
	var shared threadDetail
	decodeResponse(t, response, &shared)
	feedback := app.reviewFeedback(ctx, shared, access{Mode: "share", ShareID: app.shares[detail.ID].ID})
	for _, expected := range []string{"PUBLIC diagnostic", "text/plain", `"kind":"log"`, object.Hash, "PUBLIC evidence", "````text", "引用截断"} {
		if !strings.Contains(feedback, expected) {
			t.Fatalf("missing %q: %s", expected, feedback)
		}
	}
	if strings.Contains(feedback, "SECRET") || strings.Contains(feedback, privateObject.Hash) || len(feedback) > 4000 {
		t.Fatalf("unbounded/private Evidence feedback: %s", feedback)
	}
	binaryObject, _ := app.store.PutObject(ctx, []byte{0, 1, 2, 3}, "image/png")
	binary, _ := app.store.CreateEvidence(ctx, domain.Evidence{ThreadID: detail.ID, Kind: "file", Title: "image", ObjectHash: binaryObject.Hash, Metadata: []byte(`{"mimeType":"image/png"}`)})
	_, _ = app.store.CreateAnnotation(ctx, domain.Annotation{ThreadID: detail.ID, Body: "image comment", Target: &domain.AnnotationTarget{EvidenceID: binary.ID}})
	host, _ := app.buildThreadDetail(ctx, detail.ID, access{Mode: "host"})
	feedback = app.reviewFeedback(ctx, host, access{Mode: "host"})
	if !strings.Contains(feedback, "非文本 Evidence") || !strings.Contains(feedback, binaryObject.Hash) || strings.ContainsRune(feedback, 0) {
		t.Fatal("binary Evidence was not safely represented")
	}
}
