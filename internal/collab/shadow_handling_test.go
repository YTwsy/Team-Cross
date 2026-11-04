package collab

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/shadow"
	"teamcross/internal/spacecontext"
)

func handlingHTTP(t *testing.T, a *App, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	method := "GET"
	var data []byte
	if body != nil {
		method = "POST"
		data, _ = json.Marshal(body)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, "/api/"+path, bytes.NewReader(data))
	if !a.shadowHandlingHTTP(w, r, strings.SplitN(path, "?", 2)[0]) {
		t.Fatal("route not handled", path)
	}
	return w
}

func TestShadowHandlingLocalUIReadsActualRemoteQuestionAndOwnAnswer(t *testing.T) {
	_, s, b, j, n := consumerFixture(t)
	v := consumerRegister(t, b, j.ID)
	root := t.TempDir()
	cfg := shadow.Config{Directory: filepath.Join(root, "record"), DependencyPollInterval: 10 * time.Millisecond,
		ReadSource: func(ctx context.Context, ref spacecontext.SourceRef) (shadow.SourceSnapshot, error) {
			return b.readShadowSource(ctx, v.ID, ref)
		},
		ReadDependency: func(ctx context.Context, d shadow.TaskDependency) (shadow.DependencyUpdate, error) {
			return b.readShadowDependency(ctx, v.ID, d)
		},
	}
	e, err := shadow.New(cfg, v.ID, j.ID, v.Name, root, root, v.Owner, shadow.ModelSettings{Model: "fixture-model", ModelProvider: "fixture-provider", ReasoningEffort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	if err := e.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	b.shadows[v.ID] = e
	b.mu.Unlock()
	base := "collaborations/" + j.ID + "/attention"
	ref := spacecontext.SourceRef{SpaceID: j.ID, AnnotationID: n.ID}
	in := map[string]any{"requestId": uuid.NewString(), "source": ref, "handler": "owner", "baseRevision": 0, "shadowId": v.ID}
	w := handlingHTTP(t, b, base+"/handling", in)
	var h shadow.QuestionHandling
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &h) != nil || h.Question != n.Text || h.Cursor != "reply:0" {
		t.Fatal(w.Code, w.Body.String())
	}
	if h.Source.SpaceID != j.ID {
		t.Fatal("host alias leaked into local handling", h)
	}
	w = handlingHTTP(t, b, base+"?direction=needs_owner", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), n.Text) {
		t.Fatal(w.Code, w.Body.String())
	}
	// Another member's answer does not finish this member's response assignment.
	if _, err := s.replyAnnotation(context.Background(), AnnotationReplyInput{AnnotationID: n.ID, Text: "A colleague's answer", RequestID: uuid.NewString()}, "Owner"); err != nil {
		t.Fatal(err)
	}
	e.Start()
	store, err := b.memberAttention(j.ID, v.Owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := store.HandlingFor(ref)
		if got.Cursor == "reply:1" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, _ := store.HandlingFor(ref)
	if got.State != "waiting" || got.Cursor != "reply:1" {
		t.Fatal(got)
	}
	consumerReply(t, j, n, "The owner chooses support for version 2.")
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, _ = store.HandlingFor(ref)
		if got.State == "answered" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got.State != "answered" || got.Answer == nil || got.Answer.Actor.MemberID != v.Owner.ID || got.Answer.Source.SpaceID != j.ID {
		t.Fatal(got)
	}
	w = handlingHTTP(t, b, base+"?direction=needs_owner", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":0`) {
		t.Fatal(w.Code, w.Body.String())
	}
	in["requestId"], in["handler"], in["baseRevision"] = uuid.NewString(), "shadow", got.Revision
	w = handlingHTTP(t, b, base+"/handling", in)
	if w.Code != 200 || len(store.Snapshot().Continuations) != 1 || len(e.View().Messages) != 0 {
		t.Fatal("handback did not persist exact private continuation", w.Code, w.Body.String())
	}
	if e.View().Busy {
		t.Fatal("paused shadow unexpectedly started native model")
	}
	// A source from another space and a non-owning shadow cannot be selected.
	in["source"] = spacecontext.SourceRef{SpaceID: "other-space", AnnotationID: n.ID}
	if w := handlingHTTP(t, b, base+"/handling", in); w.Code == 200 {
		t.Fatal("cross-space control accepted")
	}
	in["source"], in["shadowId"] = ref, "someone-elses-shadow"
	if w := handlingHTTP(t, b, base+"/handling", in); w.Code == 200 {
		t.Fatal("foreign engine selected")
	}
}
