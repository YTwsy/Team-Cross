package collab

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"teamcross/internal/shadow"
	"teamcross/internal/spacecontext"
)

func TestShadowAttentionRemoteDurableDeliveryRestartAndRouteChange(t *testing.T) {
	_, s, b, j, n := consumerFixture(t)
	v := consumerRegister(t, b, j.ID)
	store, err := b.memberAttention(j.ID, v.Owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = b.pumpShadowAttention(ctx, j.ID, store); err != nil {
		t.Fatal(err)
	}
	post := func() {
		t.Helper()
		_, err := s.replyAnnotation(ctx, AnnotationReplyInput{AnnotationID: n.ID, Text: "@Member review the changed API", RequestID: uuid.NewString(), Mentions: []MemberMention{{MemberID: v.Owner.ID, Label: "Member", StartOffset: 0, EndOffset: 7}}}, "Owner")
		if err != nil {
			t.Fatal(err)
		}
	}
	post()
	route := consumerCall[ActivityAgentRoute](t, b, j.ID, "status", ActivityConsumerInput{})
	page := consumerCall[ActivityConsumerPage](t, b, j.ID, "consume", consumerInput(route))
	if len(page.Items) != 1 {
		t.Fatal(page)
	}
	item := page.Items[0]
	r := shadow.AttentionReceipt{Token: page.Receipt, Route: store.Snapshot().Route, Through: page.ThroughSeq, Initial: page.Initial}
	if err = store.Accept(r, []spacecontext.Signal{{ID: item.ID, Seq: item.Seq, Kind: item.Kind, Source: item.Source, Subject: item.Subject, Actor: item.Actor, CreatedAt: item.CreatedAt, Preview: item.Preview, Reasons: item.Reasons}}); err != nil {
		t.Fatal(err)
	}
	// Crash after durable local persistence but before the host receives ack.
	b.attentionMu.Lock()
	b.shadowAttention = nil
	b.attentionMu.Unlock()
	store, err = b.memberAttention(j.ID, v.Owner.ID)
	if err != nil || store.PendingReceipt() == nil {
		t.Fatal("restart lost the pending acknowledgement", err)
	}
	if err = b.pumpShadowAttention(ctx, j.ID, store); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot(); len(got.Events) != 1 || got.Pending != nil {
		t.Fatal(got)
	}
	afterAck := consumerCall[ActivityAgentRoute](t, b, j.ID, "status", ActivityConsumerInput{})
	if afterAck.Through != page.ThroughSeq {
		t.Fatal(afterAck)
	}
	var human ActivityPage
	activityTestRemote(t, j, "list", ActivityInput{}, &human)
	if human.Unread != 1 {
		t.Fatal("receiving agent activity marked the human list read", human)
	}
	// Failure to persist the next batch must leave the host's cursor unchanged.
	path := filepath.Join(b.Config.DataDir, "shadow-activity", contentHash([]string{j.ID, v.Owner.ID}), "attention.json")
	if err = os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	post()
	if err = b.pumpShadowAttention(ctx, j.ID, store); err == nil {
		t.Fatal("failed local persistence was acknowledged")
	}
	if got := consumerCall[ActivityAgentRoute](t, b, j.ID, "status", ActivityConsumerInput{}); got.Through != afterAck.Through {
		t.Fatal("host advanced before durable member receipt", got)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	if err = b.pumpShadowAttention(ctx, j.ID, store); err != nil {
		t.Fatal(err)
	}
	before := store.Snapshot()
	second := consumerRegister(t, b, j.ID)
	consumerCall[ActivityAgentRoute](t, b, j.ID, "configure", ActivityConsumerInput{ShadowID: second.ID, BaseRevision: route.Revision, RequestID: "new-default"})
	if err = b.pumpShadowAttention(ctx, j.ID, store); err != nil {
		t.Fatal(err)
	}
	after := store.Snapshot()
	if after.Route.ShadowID != second.ID || !reflect.DeepEqual(before.Events, after.Events) || !reflect.DeepEqual(before.Focuses, after.Focuses) {
		t.Fatal("route switch lost received context", after)
	}
	// No private follow reasons or internal receipt is published in C1.
	shared := workbenchDecode[shadowSpaceView](shadowOp(t, b, j.ID, "view", shadowInput{ID: v.ID}))
	if len(shared.Shadows[0].Messages) != 0 {
		t.Fatal("activity was appended to shared chat", shared)
	}
	raw, _ := json.Marshal(shared)
	var public map[string]any
	_ = json.Unmarshal(raw, &public)
	if _, exists := public["pending"]; exists {
		t.Fatal("private consumer receipt leaked")
	}
}
