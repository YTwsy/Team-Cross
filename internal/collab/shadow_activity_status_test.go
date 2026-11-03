package collab

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"teamcross/internal/shadow"
)

func TestShadowRouteLocalUIRevisionRetryAndForeignOwner(t *testing.T) {
	a, s, b, j, _ := consumerFixture(t)
	first, second := consumerRegister(t, b, j.ID), consumerRegister(t, b, j.ID)
	foreign := consumerRegister(t, a, s.record.ID)
	path := "collaborations/" + j.ID + "/attention/route"
	w := handlingHTTP(t, b, path, nil)
	var initial shadowRouteView
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &initial) != nil || initial.Route.Revision != 0 {
		t.Fatal(w.Code, w.Body.String())
	}
	// Reading the selection UI does not elect a route or start a model.
	in := ActivityConsumerInput{ShadowID: first.ID, RequestID: uuid.NewString()}
	w = handlingHTTP(t, b, path, in)
	var v shadowRouteView
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Route.ShadowID != first.ID || v.Route.Revision != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	newer := ActivityConsumerInput{ShadowID: second.ID, BaseRevision: v.Route.Revision, RequestID: uuid.NewString()}
	if w = handlingHTTP(t, b, path, newer); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	// Replaying the first successful request must not show or apply its old route.
	w = handlingHTTP(t, b, path, in)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Route.ShadowID != second.ID || v.Route.Revision != 2 {
		t.Fatal(w.Code, w.Body.String())
	}
	store, err := b.memberAttention(j.ID, first.Owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if store.Snapshot().Route.ShadowID != second.ID {
		t.Fatal("old receipt rolled back local default")
	}
	for _, bad := range []ActivityConsumerInput{{ShadowID: foreign.ID, BaseRevision: 2, RequestID: uuid.NewString()}, {ShadowID: first.ID, BaseRevision: 1, RequestID: uuid.NewString()}} {
		if w = handlingHTTP(t, b, path, bad); w.Code == 200 {
			t.Fatal("foreign or stale route accepted", w.Body.String())
		}
	}
}

func TestActivityProcessingStaysLocalAndDoesNotConsumeHumanRead(t *testing.T) {
	a, s, _, j, n := consumerFixture(t)
	v := consumerRegister(t, a, s.record.ID)
	consumerReply(t, j, n, "A new answer for the owner")
	store, err := a.memberAttention(s.record.ID, v.Owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.pumpShadowAttention(context.Background(), s.record.ID, store); err != nil {
		t.Fatal(err)
	}
	raw, err := a.memberActivityCall(context.Background(), s.record.ID, "list", ActivityInput{})
	if err != nil {
		t.Fatal(err)
	}
	page := workbenchDecode[ActivityPage](raw)
	if len(page.Groups) == 0 || page.Unread == 0 {
		t.Fatal(page)
	}
	enriched := a.activityProcessingPage(page, "mine")
	if enriched.Groups[0].Latest.Processing == nil || enriched.Groups[0].Latest.Processing.State != "received" {
		t.Fatal(enriched)
	}
	if page.Groups[0].Latest.Processing != nil || enriched.Unread != page.Unread || enriched.ReadRevision != page.ReadRevision {
		t.Fatal("enrichment modified host/read state")
	}
	detail := handlingHTTP(t, a, "collaborations/"+page.SpaceID+"/attention/event?eventId="+page.Groups[0].Latest.ID, nil)
	if detail.Code != 200 || !strings.Contains(detail.Body.String(), `"state":"received"`) || len(detail.Body.Bytes()) > 32<<10 {
		t.Fatal(detail.Code, detail.Body.String())
	}
	if w := handlingHTTP(t, a, "collaborations/"+page.SpaceID+"/attention/event", map[string]any{"eventId": page.Groups[0].Latest.ID}); w.Code == 200 {
		t.Fatal("detail accepted a write")
	}
	for _, untouched := range []ActivityPage{page, a.activityProcessingPage(page, "space")} {
		data, _ := json.Marshal(untouched)
		if strings.Contains(string(data), `"processing"`) {
			t.Fatal("private state escaped local personal view", string(data))
		}
	}
	other := activityClone(page)
	other.SelfID = "different-member"
	if got := a.activityProcessingPage(other, "mine"); got.Groups[0].Latest.Processing.State != "no_shadow" {
		t.Fatal("another identity inherited processing records")
	}
	// Own shadow replies are visible to the person but excluded by the
	// automatic consumer. They must not appear stuck waiting for themselves.
	root := t.TempDir()
	e, err := shadow.New(shadow.Config{Directory: filepath.Join(root, "record")}, v.ID, s.record.ID, v.Name, root, root, v.Owner, shadow.ModelSettings{Model: "fixture-model", ModelProvider: "fixture-provider", ReasoningEffort: "low"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	if err := e.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.shadows[v.ID] = e
	a.mu.Unlock()
	if _, err := a.shadowToolCall(context.Background(), v.ID, "reply_to_annotation", map[string]any{"annotationId": n.ID, "requestId": uuid.NewString(), "text": "A reply from the owner's shadow"}); err != nil {
		t.Fatal(err)
	}
	raw, err = a.memberActivityCall(context.Background(), s.record.ID, "list", ActivityInput{})
	if err != nil {
		t.Fatal(err)
	}
	projected := a.activityProcessingPage(workbenchDecode[ActivityPage](raw), "mine")
	found := false
	for _, group := range projected.Groups {
		for _, item := range append(group.Events, group.Latest) {
			if item.Actor.Kind == "shadow" && item.Actor.MemberID == v.Owner.ID {
				found = true
				if item.Processing != nil {
					t.Fatal("own output became pending input", item)
				}
			}
		}
	}
	if !found {
		t.Fatal("fixture did not expose the actual shadow reply")
	}
}
