package collab

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func ownChangeReply(t *testing.T, s *Session, n Annotation) {
	t.Helper()
	_, err := s.replyAnnotation(context.Background(), AnnotationReplyInput{AnnotationID: n.ID, RequestID: uuid.NewString(), Text: "I have confirmed the minimum version and updated the explanation."}, "Owner")
	if err != nil {
		t.Fatal(err)
	}
}

func TestActivityConsumerOwnChangesSurviveOutboxRecoveryWithoutHumanReminder(t *testing.T) {
	a, s, _, _, n := consumerFixture(t)
	consumerRegister(t, a, s.record.ID)
	route := consumerCall[ActivityAgentRoute](t, a, s.record.ID, "ensure", ActivityConsumerInput{})
	route = consumerAckFollowBaseline(t, a, s.record.ID, route)
	before := activityTestPage(t, s)
	logPath := filepath.Join(s.runtimeDirectory(), "member-activity.json")
	if err := os.Rename(logPath, logPath+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(logPath, 0700); err != nil {
		t.Fatal(err)
	}
	ownChangeReply(t, s, n)
	var disk Record
	if err := readJSON(filepath.Join(s.runtimeDirectory(), "collaboration.json"), &disk); err != nil {
		t.Fatal(err)
	}
	if len(disk.Activity.Outbox) != 1 || len(disk.Activity.Outbox[0].AgentRecipients["owner"]) == 0 || len(disk.Activity.Outbox[0].Recipients["owner"]) != 0 {
		t.Fatal("source did not persist the private self-change independently of reminders", disk.Activity.Outbox)
	}
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(logPath+".saved", logPath); err != nil {
		t.Fatal(err)
	}
	restored := a.newSession(disk)
	out, err := restored.activityConsumerOperation(context.Background(), "consume", consumerInput(route))
	if err != nil {
		t.Fatal(err)
	}
	page := out.(ActivityConsumerPage)
	if len(page.Items) != 1 || page.Items[0].Source.ReplyID == "" || !slices.Contains(page.Items[0].Reasons, "own_source_change") || activityDirect(page.Items[0].Reasons) {
		t.Fatal("own change did not arrive as an exact ordinary update", page)
	}
	again, err := restored.activityConsumerOperation(context.Background(), "consume", consumerInput(route))
	if err != nil || !reflect.DeepEqual(page, again) {
		t.Fatal("unacknowledged page was not stable", again, err)
	}
	in := consumerInput(route)
	in.Receipt = page.Receipt
	if _, err := restored.activityConsumerOperation(context.Background(), "ack", in); err != nil {
		t.Fatal(err)
	}
	after := activityTestPage(t, restored)
	if before.Unread != after.Unread || before.ReadRevision != after.ReadRevision || len(after.Groups) != len(before.Groups) {
		t.Fatal("self-change became a human reminder or a read", before, after)
	}
	public, err := restored.activityOperation(context.Background(), "list", ActivityInput{View: "space"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(public)
	if strings.Contains(string(encoded), "own_source_change") || strings.Contains(string(encoded), "agentRecipients") {
		t.Fatal("private reason entered the public activity projection", string(encoded))
	}
}

func TestActivityConsumerOwnChangesBeforeFirstConsumerAreNotSkipped(t *testing.T) {
	a, s, _, _, n := consumerFixture(t)
	ownChangeReply(t, s, n)
	consumerRegister(t, a, s.record.ID)
	route := consumerCall[ActivityAgentRoute](t, a, s.record.ID, "ensure", ActivityConsumerInput{})
	page := consumerCall[ActivityConsumerPage](t, a, s.record.ID, "consume", consumerInput(route))
	if !page.Initial || len(page.Items) != 2 || page.Items[0].Follow == nil || !slices.Contains(page.Items[1].Reasons, "own_source_change") {
		t.Fatal("fixed follow baseline swallowed a later own change", page)
	}
	if human := activityTestPage(t, s); human.Unread != 0 || len(human.Groups) != 0 {
		t.Fatal(human)
	}
}

func TestActivityConsumerOwnChangesRespectCurrentAndSavedFollowModes(t *testing.T) {
	for _, mode := range []string{"direct_only", "muted"} {
		t.Run(mode, func(t *testing.T) {
			a, s, _, _, n := consumerFixture(t)
			consumerRegister(t, a, s.record.ID)
			route := consumerCall[ActivityAgentRoute](t, a, s.record.ID, "ensure", ActivityConsumerInput{})
			route = consumerAckFollowBaseline(t, a, s.record.ID, route)
			ownChangeReply(t, s, n) // Saved while following, cancelled before consumption.
			_, err := s.activityOperation(context.Background(), "follow", ActivityInput{Subject: ActivitySubject{Kind: "discussion", ID: n.ID}, Mode: mode, Revision: 1, RequestID: "change-mode"})
			if err != nil {
				t.Fatal(err)
			}
			ownChangeReply(t, s, n) // The saved preference must not be reopened by participation.
			page := consumerCall[ActivityConsumerPage](t, a, s.record.ID, "consume", consumerInput(route))
			if len(page.Items) != 1 || page.Items[0].Follow == nil || page.Items[0].Follow.Mode != mode {
				t.Fatal("own change bypassed the member's follow preference", page)
			}
			for _, event := range s.activityLog.Events {
				if event.Seq > page.Items[0].Seq && len(event.AgentRecipients["owner"]) != 0 {
					t.Fatal("new own change was matched after follow cancellation", event)
				}
			}
		})
	}
}

func TestActivityConsumerOwnChangesRemoteMemberKeepsIdentityAndLocalSource(t *testing.T) {
	_, s, b, j, n := consumerFixture(t)
	consumerReply(t, j, n, "I will check the compatibility note.")
	v := consumerRegister(t, b, j.ID)
	route := consumerCall[ActivityAgentRoute](t, b, j.ID, "ensure", ActivityConsumerInput{})
	route = consumerAckFollowBaseline(t, b, j.ID, route)
	consumerReply(t, j, n, "The minimum version is v3.")
	page := consumerCall[ActivityConsumerPage](t, b, j.ID, "consume", consumerInput(route))
	if len(page.Items) != 1 || page.Items[0].Source.SpaceID != j.ID || page.Items[0].Actor.MemberID != v.Owner.ID || !slices.Contains(page.Items[0].Reasons, "own_source_change") {
		t.Fatal("remote self-change lost identity or source mapping", page)
	}
	for _, group := range activityTestPage(t, s).Groups {
		for _, event := range group.Events {
			if slices.Contains(event.Reasons, "own_source_change") {
				t.Fatal("member-private self-change reason leaked to another member", event)
			}
		}
	}
}

func TestActivityConsumerOwnMaterialVersionUsesFixedSource(t *testing.T) {
	ctx := context.Background()
	a, f, s := spaceFixture(t)
	d := materialDraft(t, a, f)
	out, err := a.Publish(ctx, s.record.ID, PublishInput{PreviewID: d.ID, PreviewHash: d.Hash, RequestID: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	mid := out.(map[string]any)["materialId"].(string)
	consumerRegister(t, a, s.record.ID)
	route := consumerCall[ActivityAgentRoute](t, a, s.record.ID, "ensure", ActivityConsumerInput{})
	route = consumerAckFollowBaseline(t, a, s.record.ID, route)
	if _, err = a.Publish(ctx, s.record.ID, PublishInput{PreviewID: d.ID, PreviewHash: d.Hash, RequestID: uuid.NewString(), MaterialID: mid, BaseVersion: 1}); err != nil {
		t.Fatal(err)
	}
	page := consumerCall[ActivityConsumerPage](t, a, s.record.ID, "consume", consumerInput(route))
	if len(page.Items) != 1 || page.Items[0].Source.MaterialID != mid || page.Items[0].Source.Version != 2 || !slices.Contains(page.Items[0].Reasons, "own_source_change") {
		t.Fatal("material update did not reach the owner shadow at its exact version", page)
	}
	if activityTestPage(t, s).Unread != 0 {
		t.Fatal("own publication generated an unread reminder")
	}
}

func TestActivityConsumerOwnChangeRetentionWaitsForAgentReceipt(t *testing.T) {
	event := ActivityEnvelope{ID: "own-update", Seq: 3, CreatedAt: time.Now().AddDate(0, 0, -100), AgentRecipients: map[string][]string{"owner": {"own_source_change", "follow_material"}}}
	log := ActivityLog{Seq: 3, Events: []ActivityEnvelope{event}, Seen: map[string]bool{event.ID: true}, Reads: map[string]MemberActivityReadState{"owner": {Through: 3}}, Consumers: map[string]ActivityConsumer{"owner": {Through: 2}}}
	if log.pruneActivity(time.Now().AddDate(0, 0, -90), nil) || len(log.Events) != 1 {
		t.Fatal("human read position deleted unreceived self-change")
	}
	c := log.Consumers["owner"]
	c.Through = 3
	log.Consumers["owner"] = c
	if !log.pruneActivity(time.Now().AddDate(0, 0, -90), nil) || len(log.Events) != 0 {
		t.Fatal("received self-change was retained forever")
	}
}
