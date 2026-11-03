package collab

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"teamcross/internal/spacecontext"
)

func TestFollowInitializationRemotePrivateBoundedAndIdempotent(t *testing.T) {
	_, s, b, j, n := consumerFixture(t)
	ctx := context.Background()
	for i := range 12 {
		if _, err := s.replyAnnotation(ctx, AnnotationReplyInput{AnnotationID: n.ID, Text: fmt.Sprintf("fixed evidence %d", i), RequestID: fmt.Sprintf("before-follow-%d", i)}, "Owner"); err != nil {
			t.Fatal(err)
		}
	}
	in := ActivityInput{Subject: ActivitySubject{Kind: "discussion", ID: n.ID}, Mode: "following", RequestID: "follow-from-member"}
	var preference MemberFollowPreference
	activityTestRemote(t, j, "follow", in, &preference)
	seq := s.activityLog.Seq
	activityTestRemote(t, j, "follow", in, &preference)
	if s.activityLog.Seq != seq || preference.Revision != 1 {
		t.Fatal("follow retry duplicated its initialization")
	}
	v := consumerRegister(t, b, j.ID)
	route := consumerCall[ActivityAgentRoute](t, b, j.ID, "ensure", ActivityConsumerInput{})
	p := consumerCall[ActivityConsumerPage](t, b, j.ID, "consume", consumerInput(route))
	if !p.Initial || len(p.Items) != 1 || p.Items[0].Follow == nil {
		t.Fatal("existing discussion was not initialized independently of new replies", p)
	}
	item := p.Items[0]
	if len(item.Follow.Baseline) != 9 || !item.Follow.Partial || item.Follow.Baseline[0].SpaceID != j.ID || item.Unread {
		t.Fatal("unbounded or incorrectly mapped follow baseline", item)
	}
	// New replies do not silently move the fixed initialization boundary.
	if _, err := s.replyAnnotation(ctx, AnnotationReplyInput{AnnotationID: n.ID, Text: "after boundary", RequestID: "after-follow"}, "Owner"); err != nil {
		t.Fatal(err)
	}
	signal := spacecontext.Signal{ID: item.ID, Seq: item.Seq, Kind: item.Kind, Subject: item.Subject, Source: item.Source, CreatedAt: item.CreatedAt, Reasons: item.Reasons, Follow: item.Follow}
	source, err := b.readFollowSource(ctx, signal)
	if err != nil || len(source.References) != 9 || !source.Partial || strings.Contains(string(source.Data), "after boundary") || len(source.Data) > 65536 {
		t.Fatal("initialization did not read its fixed bounded sources", err, string(source.Data))
	}
	store, err := b.memberAttention(j.ID, v.Owner.ID)
	if err != nil || b.pumpShadowAttention(ctx, j.ID, store) != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot(); len(got.Events) != 1 || got.Pending != nil || len(got.Follows) != 1 {
		t.Fatal("baseline was not durably delivered", got)
	}
	for _, view := range []string{"mine", "space"} {
		var human ActivityPage
		activityTestRemote(t, j, "list", ActivityInput{View: view}, &human)
		for _, group := range human.Groups {
			for _, event := range group.Events {
				if event.Follow != nil || event.Kind == "follow_changed" {
					t.Fatal("private control event leaked into human activity", view)
				}
			}
		}
	}
	var ignored any
	if err := j.request(ctx, "POST", "/v2/member-activity/read", ActivityInput{EventIDs: []string{item.ID}}, &ignored); err == nil {
		t.Fatal("private follow control accepted as human read")
	}
}

func TestFollowInitializationFailureRollsBackPreferenceAndOutbox(t *testing.T) {
	_, _, s := spaceFixture(t)
	n, err := s.annotate(Annotation{Text: "existing object"}, "Owner")
	if err != nil {
		t.Fatal(err)
	}
	before := activityClone(s.record.Activity)
	beforeLog := activityClone(s.activityLog)
	path := filepath.Join(s.runtimeDirectory(), "collaboration.json")
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	in := ActivityInput{Subject: ActivitySubject{Kind: "discussion", ID: n.ID}, Mode: "muted", Revision: 1, RequestID: "durable-mute"}
	if _, err := s.activityOperation(context.Background(), "follow", in); err == nil {
		t.Fatal("preference succeeded without the source record")
	}
	if !reflect.DeepEqual(before, activityClone(s.record.Activity)) || !reflect.DeepEqual(beforeLog, activityClone(s.activityLog)) {
		t.Fatal("failed preference save changed live preference or delivery state")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.activityOperation(context.Background(), "follow", in); err != nil {
		t.Fatal(err)
	}
	last := s.activityLog.Events[len(s.activityLog.Events)-1]
	if last.Follow == nil || last.Follow.Mode != "muted" || last.Follow.Revision != 2 || activityUnread(s.activityLog, last, "owner") {
		t.Fatal(last)
	}
}

func TestShadowWatchEnsureFollowKeepsOneCommandAcrossHostRetryAndUnfollow(t *testing.T) {
	_, s, b, j, n := consumerFixture(t)
	v := registeredToolShadow(t, b, j.ID)
	subject := ActivitySubject{Kind: "discussion", ID: n.ID}
	ctx := context.Background()
	first, err := b.ensureShadowFollow(ctx, v.ID, subject, "watch-follow")
	if err != nil || first.Mode != "following" || first.Revision != 1 {
		t.Fatal(first, err)
	}
	seq := s.activityLog.Seq
	var preference MemberFollowPreference
	activityTestRemote(t, j, "follow", ActivityInput{Subject: subject, Mode: "direct_only", Revision: 1, RequestID: "human-unfollow"}, &preference)
	again, err := b.ensureShadowFollow(ctx, v.ID, subject, "watch-follow")
	if err != nil || again != first || s.activityLog.Seq != seq+1 {
		t.Fatal("retry of a saved watch request re-followed after the member cancelled", again, err)
	}
	var views []FollowView
	activityTestRemote(t, j, "follows", ActivityInput{Subject: subject}, &views)
	if len(views) != 1 || views[0].Mode != "direct_only" {
		t.Fatal(views)
	}
	// A new explicit start intent takes a fresh bounded snapshot on the same
	// member preference; it does not create another subscription mechanism.
	newer, err := b.ensureShadowFollow(ctx, v.ID, subject, "new-watch-follow")
	if err != nil || newer.Revision != 3 || newer.Mode != "following" {
		t.Fatal(newer, err)
	}
}

func TestFollowInitializationEmptyExecutionIsAnExplicitSnapshot(t *testing.T) {
	a, _, _ := spaceFixture(t)
	signal := spacecontext.Signal{Kind: "follow_changed", Subject: ActivitySubject{Kind: "execution", ID: "empty"}, Source: SpaceSourceRef{SpaceID: "space", ExecutionID: "empty"}, Follow: &spacecontext.FollowChange{Mode: "following", Revision: 1}}
	out, err := a.readFollowSource(context.Background(), signal)
	if err != nil || !strings.Contains(string(out.Data), `"noCompletedTurnAtFollow":true`) || out.Partial || len(out.References) != 0 {
		t.Fatal("empty history was guessed or left in a failing retry", out, err)
	}
}

func TestFollowInitializationOldPageDoesNotStartAfterCancellation(t *testing.T) {
	a, s, _, _, n := consumerFixture(t)
	consumerRegister(t, a, s.record.ID)
	ctx := context.Background()
	if _, err := s.activityOperation(ctx, "follow", ActivityInput{Subject: ActivitySubject{Kind: "discussion", ID: n.ID}, Mode: "muted", Revision: 1, RequestID: "mute-before-shadow-start"}); err != nil {
		t.Fatal(err)
	}
	route := consumerCall[ActivityAgentRoute](t, a, s.record.ID, "ensure", ActivityConsumerInput{})
	in := consumerInput(route)
	in.Limit = 1
	page := consumerCall[ActivityConsumerPage](t, a, s.record.ID, "consume", in)
	if len(page.Items) != 1 || page.Items[0].Follow == nil || page.Items[0].Follow.Mode != "muted" || page.More {
		t.Fatal("a superseded baseline reached a model before cancellation on a later page", page)
	}
}
