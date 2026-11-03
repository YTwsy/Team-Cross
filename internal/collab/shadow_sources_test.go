package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"teamcross/internal/readview"
	"teamcross/internal/shadow"
)

func TestShadowSourceReadsExactOldReplyAndParentAcrossCores(t *testing.T) {
	_, s, b, j, n := consumerFixture(t)
	b.shadowCancel()
	b.shadowWG.Wait()
	consumerReply(t, j, n, "parent question from member B")
	n = s.record.Annotations[0]
	parentID := n.Replies[0].ID
	body := strings.Repeat("完整依据，", 150) + "END-OF-EVIDENCE"
	updated, err := s.replyAnnotation(context.Background(), AnnotationReplyInput{AnnotationID: n.ID, Text: body, RequestID: uuid.NewString(), ReplyTo: &ReplyTarget{Kind: "reply", ID: parentID}}, "Owner")
	if err != nil {
		t.Fatal(err)
	}
	replyID := updated.Replies[1].ID
	for i := range 25 {
		consumerReply(t, j, n, fmt.Sprintf("UNRELATED-%d", i))
	}
	v := consumerRegister(t, b, j.ID)
	root := t.TempDir()
	e, err := shadow.New(shadow.Config{Directory: t.TempDir()}, v.ID, j.ID, v.Name, root, root, v.Owner, shadow.ModelSettings{Model: "fixture-model", ModelProvider: "fixture-provider", ReasoningEffort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	b.mu.Lock()
	b.shadows[v.ID] = e
	b.mu.Unlock()
	var before ActivityPage
	activityTestRemote(t, j, "list", ActivityInput{}, &before)
	ref := SpaceSourceRef{SpaceID: j.ID, AnnotationID: n.ID, ReplyID: replyID}
	snapshot, err := b.readShadowSource(context.Background(), v.ID, ref)
	if err != nil {
		t.Fatal(err)
	}
	var content struct {
		SpaceID     string          `json:"spaceId"`
		Annotations []Annotation    `json:"annotations"`
		Reply       AnnotationReply `json:"reply"`
		Parent      AnnotationReply `json:"parentReply"`
		ReplyCount  int             `json:"replyCount"`
	}
	if err := json.Unmarshal(snapshot.Data, &content); err != nil {
		t.Fatal(err)
	}
	if content.SpaceID != j.ID || content.Reply.Text != body || content.Parent.ID != parentID || len(content.Annotations) != 1 || len(content.Annotations[0].Replies) != 0 || content.ReplyCount != 27 || snapshot.Partial {
		t.Fatal("exact reply body, root or parent lost", string(snapshot.Data))
	}
	if strings.Contains(string(snapshot.Data), "UNRELATED-") {
		t.Fatal("loaded the whole discussion into an exact-source read")
	}
	var after ActivityPage
	activityTestRemote(t, j, "list", ActivityInput{}, &after)
	if before.Unread != after.Unread || before.ReadRevision != after.ReadRevision {
		t.Fatal("source read altered human read state")
	}
	ref.SpaceID = s.record.ID
	if _, err = b.readShadowSource(context.Background(), v.ID, ref); err == nil {
		t.Fatal("shadow read another space alias")
	}
}

func TestExactAnnotationSourceValidatesIdentityAndBudget(t *testing.T) {
	note := Annotation{ID: "root", Text: "root", Replies: []AnnotationReply{{ID: "a", Text: "parent"}, {ID: "b", Text: "answer", ReplyTo: &ReplyTarget{Kind: "reply", ID: "a"}}}}
	for _, in := range []HistoryRead{
		{ReplyID: "b"},
		{AnnotationID: "root", ReplyID: "missing"},
		{AnnotationID: "root", ReplyID: "b", Offset: 1},
	} {
		if _, err := agentAnnotations("space", []Annotation{note}, in); err == nil {
			t.Fatal("invalid source accepted", in)
		}
	}
	note.Replies[1].Text = strings.Repeat("x", 20000)
	if _, err := agentAnnotations("space", []Annotation{note}, HistoryRead{AnnotationID: "root", ReplyID: "b", Options: readview.Options{MaxBytes: 8192}}); err == nil {
		t.Fatal("silently truncated or exceeded the source budget")
	}
	rootOnly, err := agentAnnotations("space", []Annotation{note}, HistoryRead{AnnotationID: "root", SourceOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(rootOnly)
	if strings.Contains(string(data), strings.Repeat("x", 100)) {
		t.Fatal("root-only source loaded unrelated replies")
	}
}

func TestShadowSourceMaterialUsesPublishedVersionAndWithdrawal(t *testing.T) {
	a, f, s := spaceFixture(t)
	draft := materialDraft(t, a, f)
	out, err := a.Publish(context.Background(), s.record.ID, PublishInput{PreviewID: draft.ID, PreviewHash: draft.Hash, RequestID: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	id := out.(map[string]any)["materialId"].(string)
	ref := SpaceSourceRef{SpaceID: s.record.ID, MaterialID: id, Version: 1}
	snapshot, err := a.readAttentionSource(context.Background(), ref)
	if err != nil || !strings.Contains(string(snapshot.Data), "已选的验证结果") || strings.Contains(string(snapshot.Data), "未公开") {
		t.Fatal("published source read failed", err, string(snapshot.Data))
	}
	if _, err := s.withdrawMaterial(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	old, err := a.readAttentionSource(context.Background(), ref)
	if err != nil || old.Unavailable != "material_withdrawn" || strings.Contains(string(old.Data), "已选的验证结果") || old.Source.Version != 1 {
		t.Fatal("old publication must yield a tombstone without its body", old, err)
	}
	ref.Version = 0
	withdrawal, err := a.readAttentionSource(context.Background(), ref)
	if err != nil || !withdrawal.Latest || withdrawal.Unavailable != "material_withdrawn" {
		t.Fatal("withdrawal metadata not available", err)
	}
}
