package collab

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"teamcross/internal/shadow"
)

func registeredToolShadow(t *testing.T, a *App, space string) shadow.View {
	t.Helper()
	v := consumerRegister(t, a, space)
	root := t.TempDir()
	e, err := shadow.New(shadow.Config{Directory: filepath.Join(root, "record")}, v.ID, space, v.Name, root, root, v.Owner, shadow.ModelSettings{Model: "fixture-model", ModelProvider: "fixture-provider", ReasoningEffort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.shadows[v.ID] = e
	a.mu.Unlock()
	t.Cleanup(e.Close)
	return v
}

func TestShadowToolsRemoteAuthorsMentionsRepliesAndActionIdentity(t *testing.T) {
	a, s, b, j, note := consumerFixture(t)
	v := registeredToolShadow(t, b, j.ID)
	ctx := context.Background()
	args := map[string]any{"annotationId": note.ID, "requestId": uuid.NewString(), "text": "@Owner compatibility is supported.", "mentions": []MemberMention{{MemberID: "owner", Label: "Owner", StartOffset: 0, EndOffset: 6}}}
	out, err := b.shadowToolCall(ctx, v.ID, "reply_to_annotation", args)
	if err != nil {
		t.Fatal(err)
	}
	result := workbenchDecode[struct {
		Reply AnnotationReply `json:"reply"`
	}](out)
	reply := result.Reply
	if reply.Actor.Kind != "shadow" || reply.Actor.ShadowID != v.ID || reply.Actor.MemberID != v.Owner.ID || reply.AuthorID != v.Owner.ID || reply.ReplyTo.ID != note.ID || len(reply.Mentions) != 1 {
		t.Fatal("lost actual shadow authorship", reply)
	}
	replay, err := b.shadowToolCall(ctx, v.ID, "reply_to_annotation", args)
	if err != nil || workbenchDecode[struct {
		Reply AnnotationReply `json:"reply"`
	}](replay).Reply.ID != reply.ID {
		t.Fatal("replayed reply duplicated", replay, err)
	}
	s.mu.Lock()
	if len(s.record.Annotations[0].Replies) != 1 {
		t.Error("duplicate reply persisted")
	}
	_, followed := s.activityPreferenceLocked(v.Owner.ID, ActivitySubject{Kind: "discussion", ID: note.ID})
	s.mu.Unlock()
	if followed {
		t.Fatal("temporary consultation permanently followed discussion")
	}
	var own FollowPage
	activityTestRemote(t, j, "follows", ActivityInput{}, &own)
	if len(own.Items) != 0 {
		t.Fatal("shadow write changed member preferences", own)
	}
	page := activityTestPage(t, s)
	if len(page.Groups) != 1 || page.Groups[0].Latest.Actor.ShadowID != v.ID {
		t.Fatal("public activity did not preserve shadow identity", page)
	}
	args["text"] = "changed after acknowledgement"
	if _, err = b.shadowToolCall(ctx, v.ID, "reply_to_annotation", args); err == nil {
		t.Fatal("changed payload reused action ID")
	}
	read, err := b.shadowToolCall(ctx, v.ID, "read_annotations", map[string]any{"annotationId": note.ID, "replyId": reply.ID, "sourceOnly": true})
	if err != nil || workbenchDecode[map[string]any](read)["spaceId"] != j.ID {
		t.Fatal("exact remote source not remapped", read, err)
	}
	members, err := b.shadowToolCall(ctx, v.ID, "list_space_members", map[string]any{})
	if err != nil || len(workbenchDecode[[]map[string]any](members)) != 2 {
		t.Fatal(members, err)
	}
	other := consumerRegister(t, a, s.record.ID)
	var rejected any
	if err = j.request(ctx, "POST", "/v2/shadow-tools", shadowToolInput{ShadowID: other.ID, Name: "list_space_members", Arguments: map[string]any{}}, &rejected); err == nil {
		t.Fatal("member used another member's shadow")
	}
	if _, err = b.shadowToolCall(ctx, v.ID, "read_annotations", map[string]any{"spaceId": s.record.ID}); err == nil {
		t.Fatal("bound tool accepted arbitrary space")
	}
}

func TestShadowToolsConsultSessionsAndOtherShadowsWithoutLosingActor(t *testing.T) {
	a, s, b, j, _ := consumerFixture(t)
	v := registeredToolShadow(t, b, j.ID)
	_, _, target := spaceChannel(t, a, s.record.ID)
	ctx := context.Background()
	args := map[string]any{"requestId": uuid.NewString(), "targetId": target.ID, "instruction": "Find the compatibility limit for the current question.", "intent": "analyze", "references": []any{}}
	out, err := b.shadowToolCall(ctx, v.ID, "send_space_request", args)
	if err != nil {
		t.Fatal(err)
	}
	request := workbenchDecode[SpaceRequest](out)
	if request.Actor.Kind != "shadow" || request.Actor.ShadowID != v.ID || request.Actor.MemberID != v.Owner.ID || request.State != "queued" {
		t.Fatal(request)
	}
	duplicate, err := b.shadowToolCall(ctx, v.ID, "send_space_request", args)
	if err != nil || workbenchDecode[SpaceRequest](duplicate).ID != request.ID {
		t.Fatal(duplicate, err)
	}
	other := consumerRegister(t, a, s.record.ID)
	other.Revision, other.Online = 1, true
	shadowOp(t, a, s.record.ID, "sync", shadowInput{ID: other.ID, Snapshot: &other})
	messageArgs := map[string]any{"shadowId": other.ID, "messageId": uuid.NewString(), "text": "What is the agreed compatibility deadline?"}
	out, err = b.shadowToolCall(ctx, v.ID, "send_space_shadow", messageArgs)
	if err != nil {
		t.Fatal(err)
	}
	delivery := workbenchDecode[ShadowDelivery](out)
	if delivery.State != "sending" || delivery.Message.Actor.Kind != "shadow" || delivery.Message.Actor.ID != v.ID || delivery.Message.Actor.MemberID != v.Owner.ID {
		t.Fatal("consultation impersonated a human", delivery)
	}
	rootArgs := map[string]any{"requestId": uuid.NewString(), "text": "Question about the missing client version."}
	created, err := b.shadowToolCall(ctx, v.ID, "add_annotation", rootArgs)
	if err != nil || workbenchDecode[Annotation](created).Actor.ShadowID != v.ID {
		t.Fatal(created, err)
	}
	second := registeredToolShadow(t, b, j.ID)
	if _, err = b.shadowToolCall(ctx, second.ID, "add_annotation", rootArgs); err == nil {
		t.Fatal("same-member shadow reused a different author's action")
	}
}
