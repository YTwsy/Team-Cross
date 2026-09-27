package collab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"teamcross/internal/mcp"
	"teamcross/internal/readview"
)

func TestAgentAnnotationReadsAndReceiptsStayScoped(t *testing.T) {
	a, runtime, _ := fixture(t)
	s := createFixture(t, a, runtime, "existing")
	ctx := context.Background()
	quote := strings.Repeat("原文", 2883)
	root, err := s.annotate(Annotation{Text: "review this", Target: &AnnotationTarget{Kind: "history", TurnID: "t", ItemID: "item", Quote: quote, EndOffset: len([]rune(quote))}}, "发起者")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err = s.annotate(Annotation{Text: fmt.Sprintf("unrelated-%d", i)}, "发起者"); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(a.Handler(http.NotFoundHandler()))
	defer server.Close()
	backend := mcp.Backend{URL: server.URL, Token: a.Token, Client: server.Client()}
	args := map[string]any{"id": s.record.ID, "kind": "annotations", "annotationId": root.ID}
	raw, err := backend.Invoke(ctx, "read_context", args)
	if err != nil || bytes.Contains(raw, []byte("unrelated-")) || !bytes.Contains(raw, []byte(quote)) || readview.RawWireSize(raw) > readview.DefaultBytes {
		t.Fatal(err, string(raw))
	}
	var selected struct {
		Annotations []Annotation `json:"annotations"`
	}
	if json.Unmarshal(raw, &selected) != nil || len(selected.Annotations) != 1 || selected.Annotations[0].ID != root.ID {
		t.Fatal(string(raw))
	}
	status, err := backend.Invoke(ctx, "get_collaboration", map[string]any{"id": s.record.ID})
	if err != nil || bytes.Contains(status, []byte(quote)) || !bytes.Contains(status, []byte(`"annotationsCount":9`)) {
		t.Fatal(err, string(status))
	}
	reply := map[string]any{"id": s.record.ID, "annotationId": root.ID, "text": "saved answer", "requestId": "same-request"}
	receipt, err := backend.Invoke(ctx, "reply_to_annotation", reply)
	if err != nil || bytes.Contains(receipt, []byte(quote)) || !bytes.Contains(receipt, []byte(`"status":"saved"`)) || !bytes.Contains(receipt, []byte("saved answer")) {
		t.Fatal(err, string(receipt))
	}
	retry, err := backend.Invoke(ctx, "reply_to_annotation", reply)
	if err != nil || string(receipt) != string(retry) {
		t.Fatal("idempotent receipt changed", err, string(retry))
	}
	args["annotationId"] = "missing"
	if _, err = backend.Invoke(ctx, "read_context", args); err == nil {
		t.Fatal("missing annotation silently returned all notes")
	}
	t.Logf("5766-character quote: single read %d bytes; status %d bytes; saved receipt %d bytes", readview.RawWireSize(raw), readview.RawWireSize(status), readview.RawWireSize(receipt))
}

func TestAgentAnnotationReplyPaginationAndIndexBudget(t *testing.T) {
	note := Annotation{ID: "root", Text: "opinion", Target: &AnnotationTarget{Kind: "material", Quote: strings.Repeat("字", 5766)}}
	for i := 0; i < 40; i++ {
		note.Replies = append(note.Replies, AnnotationReply{ID: fmt.Sprint(i), Text: strings.Repeat("reply", 350)})
	}
	opts := HistoryRead{Compact: true, AnnotationID: "root"}
	seen := []string{}
	for page := 0; page < 30; page++ {
		result, err := agentAnnotations("space", []Annotation{note}, opts)
		if err != nil {
			t.Fatal(err)
		}
		if readview.WireSize(result) > readview.DefaultBytes {
			t.Fatal("oversized annotation page")
		}
		out := result.(map[string]any)
		for _, r := range out["annotations"].([]Annotation)[0].Replies {
			seen = append(seen, r.ID)
		}
		next, ok := out["nextRead"].(map[string]any)
		if !ok {
			break
		}
		opts.Offset = next["offset"].(int)
		omit := false
		opts.IncludeQuote = &omit
	}
	if len(seen) != 40 {
		t.Fatal("lost replies", seen)
	}
	for i, id := range seen {
		if id != fmt.Sprint(i) {
			t.Fatal("duplicate reply", seen)
		}
	}
	index, err := agentAnnotations("space", []Annotation{note}, HistoryRead{Compact: true})
	encoded, _ := json.Marshal(index)
	if err != nil || bytes.Contains(encoded, []byte(note.Target.Quote)) || bytes.Contains(encoded, []byte(note.Replies[0].Text)) {
		t.Fatal(err, string(encoded))
	}
}
