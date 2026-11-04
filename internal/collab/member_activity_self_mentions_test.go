package collab

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestActivityConsumerSelfMentionIsDirectWithoutOwnReminder(t *testing.T) {
	a, s, _, _, _ := consumerFixture(t)
	mention := []MemberMention{{MemberID: "owner", Label: "Owner", EndOffset: 6}}
	n, err := s.annotate(Annotation{Text: "@Owner check this proposal", Mentions: mention}, "Owner")
	if err != nil {
		t.Fatal(err)
	}
	in := AnnotationReplyInput{AnnotationID: n.ID, RequestID: uuid.NewString(), Text: "@Owner include the latest change", Mentions: mention}
	for range 2 { // Same business retry must not delegate twice.
		if _, err := s.replyAnnotation(context.Background(), in, "Owner"); err != nil {
			t.Fatal(err)
		}
	}
	consumerRegister(t, a, s.record.ID)
	route := consumerCall[ActivityAgentRoute](t, a, s.record.ID, "ensure", ActivityConsumerInput{})
	page := consumerCall[ActivityConsumerPage](t, a, s.record.ID, "consume", consumerInput(route))
	root, reply := 0, 0
	for _, item := range page.Items {
		if !slices.Contains(item.Reasons, "mention") {
			continue
		}
		if item.Actor.Kind != "human" || item.Actor.MemberID != "owner" || item.Source.SpaceID != s.record.ID || item.Source.AnnotationID != n.ID || slices.Contains(item.Reasons, "own_source_change") {
			t.Fatal("delegation lost its author/source or became a completed owner change", item)
		}
		if item.Source.ReplyID == "" {
			root++
		} else {
			reply++
		}
	}
	if root != 1 || reply != 1 {
		t.Fatal("initial consumer skipped or duplicated self mentions", page)
	}
	if human := activityTestPage(t, s); human.Unread != 0 || len(human.Groups) != 0 {
		t.Fatal("self delegation notified its own author", human)
	}
}

func TestActivityConsumerSelfMentionRespectsFollowModes(t *testing.T) {
	for _, mode := range []string{"following", "direct_only", "muted"} {
		t.Run(mode, func(t *testing.T) {
			a, s, _, _, n := consumerFixture(t)
			consumerRegister(t, a, s.record.ID)
			route := consumerCall[ActivityAgentRoute](t, a, s.record.ID, "ensure", ActivityConsumerInput{})
			route = consumerAckFollowBaseline(t, a, s.record.ID, route)
			in := AnnotationReplyInput{AnnotationID: n.ID, RequestID: uuid.NewString(), Text: "@Owner check this", Mentions: []MemberMention{{MemberID: "owner", Label: "Owner", EndOffset: 6}}}
			if _, err := s.replyAnnotation(context.Background(), in, "Owner"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.activityOperation(context.Background(), "follow", ActivityInput{Subject: ActivitySubject{Kind: "discussion", ID: n.ID}, Mode: mode, Revision: 1, RequestID: uuid.NewString()}); err != nil {
				t.Fatal(err)
			}
			in.RequestID = uuid.NewString()
			if _, err := s.replyAnnotation(context.Background(), in, "Owner"); err != nil {
				t.Fatal(err)
			}
			page := consumerCall[ActivityConsumerPage](t, a, s.record.ID, "consume", consumerInput(route))
			mentions := 0
			for _, item := range page.Items {
				if slices.Contains(item.Reasons, "mention") {
					mentions++
				}
			}
			want := 2
			if mode == "muted" {
				want = 0
			}
			if mentions != want {
				t.Fatal("saved/current preference was bypassed", page)
			}
		})
	}
}

func TestActivityConsumerRemoteSelfMentionKeepsMemberAndLocalSource(t *testing.T) {
	_, s, b, j, n := consumerFixture(t)
	v := consumerRegister(t, b, j.ID)
	var note Annotation
	in := AnnotationReplyInput{AnnotationID: n.ID, RequestID: uuid.NewString(), Text: "@Member please check this", Mentions: []MemberMention{{MemberID: v.Owner.ID, Label: "Member", EndOffset: 7}}}
	if err := j.request(context.Background(), "POST", "/v2/annotation-replies", in, &note); err != nil {
		t.Fatal(err)
	}
	route := consumerCall[ActivityAgentRoute](t, b, j.ID, "ensure", ActivityConsumerInput{})
	page := consumerCall[ActivityConsumerPage](t, b, j.ID, "consume", consumerInput(route))
	mentions := 0
	for _, item := range page.Items {
		if slices.Contains(item.Reasons, "mention") {
			mentions++
			if item.Actor.MemberID != v.Owner.ID || item.Source.SpaceID != j.ID || item.Source.AnnotationID != n.ID || item.Source.ReplyID == "" || slices.Contains(item.Reasons, "own_source_change") {
				t.Fatal("remote self mention was attributed to host or lost its exact source", item)
			}
		}
	}
	if mentions != 1 {
		t.Fatal(page)
	}
	for _, group := range activityTestPage(t, s).Groups {
		for _, item := range group.Events {
			if slices.Contains(item.Reasons, "mention") {
				t.Fatal("private self mention leaked to another member", item)
			}
		}
	}
}
