package collab

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestAnnotationRepliesPersistAreFlatAndDeduplicate(t *testing.T) {
	a, runtime, _ := fixture(t)
	s := createFixture(t, a, runtime, "existing")
	ctx := context.Background()
	root, err := s.annotate(Annotation{Text: "Review this", Replies: []AnnotationReply{{Text: "forged"}}}, "发起者")
	if err != nil || len(root.Replies) != 0 {
		t.Fatal(root, err)
	}
	before, err := s.Context(ctx, "annotations", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	input := AnnotationReplyInput{AnnotationID: root.ID, Text: " Answer ", RequestID: "reply-1"}
	got, err := s.replyAnnotation(ctx, input, "协作者")
	if err != nil || len(got.Replies) != 1 || got.Replies[0].Text != "Answer" || got.Replies[0].Author != "协作者" {
		t.Fatal(got, err)
	}
	duplicate, err := s.replyAnnotation(ctx, input, "协作者")
	if err != nil || len(duplicate.Replies) != 1 || duplicate.Replies[0].ID != got.Replies[0].ID {
		t.Fatal(duplicate, err)
	}
	if len(before.(map[string]any)["annotations"].([]Annotation)[0].Replies) != 0 {
		t.Fatal("old context mutated")
	}
	input.Text = "changed"
	if _, err = s.replyAnnotation(ctx, input, "协作者"); err == nil {
		t.Fatal("same request ID accepted different text")
	}
	for _, id := range []string{got.Replies[0].ID, "other-collaboration"} {
		if _, err = s.replyAnnotation(ctx, AnnotationReplyInput{AnnotationID: id, Text: "nested", RequestID: "other"}, "发起者"); err == nil {
			t.Fatal("accepted non-root target", id)
		}
	}
	var disk Record
	if err = readJSON(filepath.Join(a.Config.DataDir, "collaborations", s.record.ID, "collaboration.json"), &disk); err != nil {
		t.Fatal(err)
	}
	if len(disk.Annotations) != 1 || len(disk.Annotations[0].Replies) != 1 {
		t.Fatal(disk.Annotations)
	}
	if s.writer != "owner" || s.busy {
		t.Fatal("reply changed native input state")
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	for _, method := range runtime.calls {
		if method == "turn/start" || method == "turn/steer" {
			t.Fatal("reply started model")
		}
	}
}

func TestAnnotationReplyConcurrentReadersAndRollback(t *testing.T) {
	a, runtime, _ := fixture(t)
	s := createFixture(t, a, runtime, "existing")
	ctx := context.Background()
	root, err := s.annotate(Annotation{Text: "review"}, "发起者")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				view := s.view()
				if _, err := json.Marshal(view); err != nil {
					t.Error(err)
				}
				if _, err := s.replyAnnotation(ctx, AnnotationReplyInput{AnnotationID: root.ID, Text: "answer", RequestID: "same"}, "协作者"); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	if len(s.record.Annotations[0].Replies) != 1 {
		t.Fatal("duplicate concurrent replies")
	}
	dir := filepath.Join(a.Config.DataDir, "collaborations", s.record.ID)
	if err := os.Rename(dir, dir+"-temporarily-moved"); err != nil {
		t.Fatal(err)
	}
	defer os.Rename(dir+"-temporarily-moved", dir)
	if _, err := s.replyAnnotation(ctx, AnnotationReplyInput{AnnotationID: root.ID, Text: "cannot persist", RequestID: "failed"}, "发起者"); err == nil {
		t.Fatal("save should fail")
	}
	if len(s.record.Annotations[0].Replies) != 1 {
		t.Fatal("failed reply retained in memory")
	}
}

func TestRemoteAnnotationReplyMembershipAndStrictShape(t *testing.T) {
	a, runtime, _ := fixture(t)
	s := createFixture(t, a, runtime, "existing")
	b, _, _ := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	root, err := s.annotate(Annotation{Text: "review"}, "发起者")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Action(ctx, "share"); err != nil {
		t.Fatal(err)
	}
	j, err := b.Join(ctx, s.share.Token())
	if err != nil {
		t.Fatal(err)
	}
	in := AnnotationReplyInput{AnnotationID: root.ID, Text: "response", RequestID: "remote-reply"}
	var out Annotation
	if err = j.request(ctx, "POST", "/v2/annotation-replies", in, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Replies) != 1 || out.Replies[0].Author != b.Host || out.Replies[0].AuthorID != firstRemote(s) || s.writer != "owner" {
		t.Fatal(out)
	}
	if err = j.request(ctx, "POST", "/v2/annotation-replies", map[string]any{"annotationId": root.ID, "text": "bad", "requestId": "bad", "target": map[string]string{"kind": "annotation"}}, &out); err == nil {
		t.Fatal("nested source accepted")
	}
	if err = s.Action(ctx, "end"); err != nil {
		t.Fatal(err)
	}
	if err = j.request(ctx, "POST", "/v2/annotation-replies", in, &out); err == nil {
		t.Fatal("ended member replied")
	}
}

func TestRuntimeAnnotationsScopeAuthorAndRevocation(t *testing.T) {
	for _, provider := range []string{"codex", "claude"} {
		t.Run(provider, func(t *testing.T) {
			a, runtime, _ := fixture(t)
			s := createFixture(t, a, runtime, "existing")
			s.mu.Lock()
			s.record.Provider = provider
			s.mu.Unlock()
			root, err := s.annotate(Annotation{Text: "review"}, "协作者")
			if err != nil {
				t.Fatal(err)
			}
			launch, err := s.annotationLaunch()
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(a.Handler(http.NotFoundHandler()))
			defer server.Close()
			call := func(token, name string, args map[string]any) (int, []byte) {
				payload, _ := json.Marshal(map[string]any{"name": name, "arguments": args})
				req := httptest.NewRequest("POST", server.URL+"/api/runtime-annotations/"+s.record.ID, bytes.NewReader(payload))
				req.Header.Set("Authorization", "Bearer "+token)
				recorder := httptest.NewRecorder()
				a.Handler(http.NotFoundHandler()).ServeHTTP(recorder, req)
				return recorder.Code, recorder.Body.Bytes()
			}
			token := launch.Env["TEAMCROSS_ANNOTATION_TOKEN"]
			if code, _ := call("wrong", "read_annotations", nil); code != 403 {
				t.Fatal(code)
			}
			for name, args := range map[string]map[string]any{"send_input": {"text": "self input"}, "read_annotations": {"id": "another"}, "reply_to_annotation": {"annotationId": root.ID, "text": "bad", "requestId": "bad", "target": map[string]any{}}} {
				if code, _ := call(token, name, args); code < 400 {
					t.Fatal("scope accepted", name)
				}
			}
			if code, raw := call(token, "read_annotations", nil); code != 200 || !bytes.Contains(raw, []byte(root.Text)) {
				t.Fatal(code, string(raw))
			}
			code, raw := call(token, "reply_to_annotation", map[string]any{"annotationId": root.ID, "text": "Agent answer", "requestId": "model-reply"})
			author := "Codex"
			if provider == "claude" {
				author = "Claude Code"
			}
			if code != 200 || !bytes.Contains(raw, []byte(`"author":"`+author+`"`)) {
				t.Fatal(code, string(raw))
			}
			view, _ := json.Marshal(s.view())
			if bytes.Contains(view, []byte(token)) {
				t.Fatal("view exposed runtime credential")
			}
			s.mu.Lock()
			s.annotationAccess = false
			s.mu.Unlock()
			if code, _ := call(token, "read_annotations", nil); code < 400 {
				t.Fatal("released scope still accessible")
			}
			// Replacing an ended/expired invitation can retain the same worker
			// while it is busy. A successful new share must reopen its tools.
			s.mu.Lock()
			s.busy = true
			s.mu.Unlock()
			if err := s.Action(context.Background(), "end"); err != nil {
				t.Fatal(err)
			}
			if err := s.Action(context.Background(), "share"); err != nil {
				t.Fatal(err)
			}
			if code, raw := call(token, "read_annotations", nil); code != 200 {
				t.Fatal("new share did not reopen tools", code, string(raw))
			}
			s.mu.Lock()
			s.busy = false
			s.mu.Unlock()
			if provider == "codex" {
				ctx := context.Background()
				if err := s.Action(ctx, "end"); err != nil {
					t.Fatal(err)
				}
				if err := s.Action(ctx, "start"); err != nil {
					t.Fatal(err)
				}
				// An owner's direct connection without a share schedules an idle
				// release. That scheduling flag must not revoke the restored tools.
				s.mu.Lock()
				s.releaseWhenIdle = true
				s.mu.Unlock()
				if code, raw := call(token, "read_annotations", nil); code != 200 {
					t.Fatal("restored owner without share cannot read", code, string(raw))
				}
			}
		})
	}
}
