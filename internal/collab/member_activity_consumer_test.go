package collab

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/shadow"
)

func consumerFixture(t *testing.T) (*App, *Session, *App, *Joined, Annotation) {
	t.Helper()
	a, _, s := spaceFixture(t)
	b, _, _ := fixture(t)
	stopWorkbenchPump(b)
	if err := s.Share(context.Background(), "lan"); err != nil {
		t.Fatal(err)
	}
	j, err := b.Join(context.Background(), s.sharedRuntime().Token())
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.annotate(Annotation{Text: "Which client versions should we support?"}, "Owner")
	if err != nil {
		t.Fatal(err)
	}
	return a, s, b, j, n
}

func consumerReply(t *testing.T, j *Joined, n Annotation, text string) {
	t.Helper()
	var result Annotation
	if err := j.request(context.Background(), "POST", "/v2/annotation-replies", AnnotationReplyInput{AnnotationID: n.ID, Text: text, RequestID: uuid.NewString()}, &result); err != nil {
		t.Fatal(err)
	}
}

func consumerCall[T any](t *testing.T, a *App, space, op string, in ActivityConsumerInput) T {
	t.Helper()
	out, err := a.activityConsumerCall(context.Background(), space, op, in)
	if err != nil {
		t.Fatalf("%s: %v", op, err)
	}
	return workbenchDecode[T](out)
}

func consumerRegister(t *testing.T, a *App, space string) shadow.View {
	t.Helper()
	return workbenchDecode[shadow.View](shadowOp(t, a, space, "register", shadowInput{ID: uuid.NewString(), Name: "活动分身"}))
}

func consumerInput(route ActivityAgentRoute) ActivityConsumerInput {
	return ActivityConsumerInput{ShadowID: route.ShadowID, BaseRevision: route.Revision}
}

func TestActivityConsumerInitialPagesSurviveHumanReadAndRestart(t *testing.T) {
	a, s, _, j, n := consumerFixture(t)
	consumerRegister(t, a, s.record.ID)
	for i := range 27 {
		consumerReply(t, j, n, fmt.Sprintf("evidence %d", i))
	}
	route := consumerCall[ActivityAgentRoute](t, a, s.record.ID, "ensure", ActivityConsumerInput{})
	if route.InitialRemaining != 28 { // One private follow baseline and 27 replies.
		t.Fatal(route)
	}
	in := consumerInput(route)
	in.Limit = 7
	first := consumerCall[ActivityConsumerPage](t, a, s.record.ID, "consume", in)
	if !first.Initial || len(first.Items) != 7 || !first.More {
		t.Fatal(first)
	}
	// Opening/clearing the human list between consumer pages must not alter
	// the frozen initial set, nor invalidate the outstanding consumer receipt.
	human := activityTestPage(t, s)
	if len(human.Groups[0].Events) != 20 {
		t.Fatal("fixture must exceed the UI expansion limit")
	}
	if _, err := s.activityOperation(context.Background(), "read", ActivityInput{Snapshot: human.Snapshot}); err != nil {
		t.Fatal(err)
	}
	in.Receipt = first.Receipt
	consumerCall[ActivityAgentRoute](t, a, s.record.ID, "ack", in)
	consumerCall[ActivityAgentRoute](t, a, s.record.ID, "ack", in)
	consumerReply(t, j, n, "arrives while the initial review is still pending")
	beforeRead := activityClone(s.activityLog.Reads)
	// Reload both authoritative files, without using the original in-memory log.
	var disk Record
	if err := readJSON(filepath.Join(s.runtimeDirectory(), "collaboration.json"), &disk); err != nil {
		t.Fatal(err)
	}
	restored := a.newSession(disk)
	seen := map[string]bool{}
	for _, e := range first.Items {
		seen[e.ID] = true
	}
	for attempt := 0; ; attempt++ {
		if attempt > 10 {
			t.Fatal("consumer did not finish")
		}
		out, err := restored.activityConsumerOperation(context.Background(), "consume", in)
		if err != nil {
			t.Fatal(err)
		}
		page := out.(ActivityConsumerPage)
		for _, e := range page.Items {
			if seen[e.ID] {
				t.Fatal("duplicate event after acknowledged page", e.ID)
			}
			seen[e.ID] = true
		}
		in.Receipt = page.Receipt
		if _, err = restored.activityConsumerOperation(context.Background(), "ack", in); err != nil {
			t.Fatal(err)
		}
		if !page.More {
			break
		}
	}
	if len(seen) != 29 || !reflect.DeepEqual(beforeRead, restored.activityLog.Reads) {
		t.Fatalf("events=%d; consumer changed human reads=%v", len(seen), !reflect.DeepEqual(beforeRead, restored.activityLog.Reads))
	}
}

func TestActivityConsumerLiveSnapshotRouteAndReceiptIsolation(t *testing.T) {
	a, s, b, j, n := consumerFixture(t)
	first := consumerRegister(t, a, s.record.ID)
	route := consumerCall[ActivityAgentRoute](t, a, s.record.ID, "ensure", ActivityConsumerInput{})
	route = consumerAckFollowBaseline(t, a, s.record.ID, route)
	second := consumerRegister(t, a, s.record.ID)
	if got := consumerCall[ActivityAgentRoute](t, a, s.record.ID, "ensure", ActivityConsumerInput{ShadowID: second.ID}); got.ShadowID != first.ID {
		t.Fatal("new registration took over", got)
	}
	for i := range 8 {
		consumerReply(t, j, n, fmt.Sprintf("live %d", i))
	}
	in := consumerInput(route)
	in.Limit = 3
	p := consumerCall[ActivityConsumerPage](t, a, s.record.ID, "consume", in)
	if p.Initial || len(p.Items) != 3 || !p.More {
		t.Fatal(p)
	}
	in.ThroughSeq = p.SnapshotSeq
	consumerReply(t, j, n, "after the fixed snapshot")
	count := 0
	for {
		count += len(p.Items)
		in.Receipt = p.Receipt
		consumerCall[ActivityAgentRoute](t, a, s.record.ID, "ack", in)
		if !p.More {
			break
		}
		p = consumerCall[ActivityConsumerPage](t, a, s.record.ID, "consume", in)
	}
	if count != 8 {
		t.Fatal("snapshot lost or included a concurrent event", count)
	}
	in.ThroughSeq = 0
	p = consumerCall[ActivityConsumerPage](t, a, s.record.ID, "consume", in)
	if len(p.Items) != 1 || !strings.Contains(p.Items[0].Preview, "after") {
		t.Fatal(p)
	}
	other := consumerRegister(t, b, j.ID)
	otherRoute := consumerCall[ActivityAgentRoute](t, b, j.ID, "ensure", ActivityConsumerInput{})
	otherIn := consumerInput(otherRoute)
	otherIn.Receipt = p.Receipt
	if _, err := b.activityConsumerCall(context.Background(), j.ID, "ack", otherIn); err == nil {
		t.Fatal("cross-member receipt accepted")
	}
	if _, err := a.activityConsumerCall(context.Background(), s.record.ID, "configure", ActivityConsumerInput{ShadowID: other.ID, BaseRevision: route.Revision, RequestID: "foreign"}); err == nil {
		t.Fatal("foreign shadow selected")
	}
	change := ActivityConsumerInput{ShadowID: second.ID, BaseRevision: route.Revision, RequestID: "switch"}
	switched := consumerCall[ActivityAgentRoute](t, a, s.record.ID, "configure", change)
	if again := consumerCall[ActivityAgentRoute](t, a, s.record.ID, "configure", change); again != switched {
		t.Fatal("configuration retry changed its receipt")
	}
	in.Receipt = p.Receipt
	if _, err := a.activityConsumerCall(context.Background(), s.record.ID, "ack", in); err == nil {
		t.Fatal("old route acknowledged work after transfer")
	}
	change.ShadowID = first.ID
	if _, err := a.activityConsumerCall(context.Background(), s.record.ID, "configure", change); err == nil {
		t.Fatal("idempotency key accepted different configuration")
	}
	remaining := consumerCall[ActivityConsumerPage](t, a, s.record.ID, "consume", consumerInput(switched))
	if len(remaining.Items) != 1 || remaining.Items[0].ID != p.Items[0].ID {
		t.Fatal("route change lost the unacknowledged event", remaining)
	}
}

func TestActivityConsumerRemoteSpaceMappingAndRuntimeBoundary(t *testing.T) {
	a, s, b, j, n := consumerFixture(t)
	v := consumerRegister(t, b, j.ID)
	route := consumerCall[ActivityAgentRoute](t, b, j.ID, "ensure", ActivityConsumerInput{})
	_, err := s.replyAnnotation(context.Background(), AnnotationReplyInput{AnnotationID: n.ID, Text: "@Member please review", RequestID: uuid.NewString(), Mentions: []MemberMention{{MemberID: v.Owner.ID, Label: "Member", StartOffset: 0, EndOffset: 7}}}, "Owner")
	if err != nil {
		t.Fatal(err)
	}
	p := consumerCall[ActivityConsumerPage](t, b, j.ID, "consume", consumerInput(route))
	if len(p.Items) != 1 || p.Items[0].Source.SpaceID != j.ID || !activityDirect(p.Items[0].Reasons) {
		t.Fatal("remote source mapping or recipient identity lost", p)
	}
	ctx := context.WithValue(context.Background(), runtimeAnnotationKey{}, "runtime")
	if _, err = a.activityConsumerCall(ctx, s.record.ID, "status", ActivityConsumerInput{}); err == nil {
		t.Fatal("bound runtime read the member consumer ledger")
	}
}

func TestActivityConsumerPauseMuteAndWriteFailure(t *testing.T) {
	a, s, _, j, n := consumerFixture(t)
	consumerRegister(t, a, s.record.ID)
	route := consumerCall[ActivityAgentRoute](t, a, s.record.ID, "ensure", ActivityConsumerInput{})
	route = consumerAckFollowBaseline(t, a, s.record.ID, route)
	consumerReply(t, j, n, "pending")
	paused := false
	route = consumerCall[ActivityAgentRoute](t, a, s.record.ID, "configure", ActivityConsumerInput{Enabled: &paused, BaseRevision: route.Revision, RequestID: "pause"})
	if p := consumerCall[ActivityConsumerPage](t, a, s.record.ID, "consume", consumerInput(route)); len(p.Items) != 0 || p.Receipt != "" {
		t.Fatal("paused consumer advanced", p)
	}
	enabled := true
	route = consumerCall[ActivityAgentRoute](t, a, s.record.ID, "configure", ActivityConsumerInput{Enabled: &enabled, BaseRevision: route.Revision, RequestID: "resume"})
	in := consumerInput(route)
	p := consumerCall[ActivityConsumerPage](t, a, s.record.ID, "consume", in)
	if len(p.Items) != 1 {
		t.Fatal("pause discarded pending input", p)
	}
	path := filepath.Join(s.runtimeDirectory(), "member-activity.json")
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	in.Receipt = p.Receipt
	if _, err := a.activityConsumerCall(context.Background(), s.record.ID, "ack", in); err == nil {
		t.Fatal("acknowledged without durable write")
	}
	if s.activityLog.Consumers["owner"].Through != route.Through {
		t.Fatal("failed save advanced the in-memory position")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	// A queued direct signal still respects the member's later explicit mute.
	if _, err := s.activityOperation(context.Background(), "follow", ActivityInput{Subject: ActivitySubject{Kind: "discussion", ID: n.ID}, Mode: "muted", Revision: 1, RequestID: "mute"}); err != nil {
		t.Fatal(err)
	}
	muted := consumerCall[ActivityConsumerPage](t, a, s.record.ID, "consume", consumerInput(route))
	if len(muted.Items) != 1 || muted.Items[0].Follow == nil || muted.Items[0].Follow.Mode != "muted" || muted.ThroughSeq != p.ThroughSeq+1 {
		t.Fatal(muted)
	}
}

func consumerAckFollowBaseline(t *testing.T, a *App, space string, route ActivityAgentRoute) ActivityAgentRoute {
	t.Helper()
	page := consumerCall[ActivityConsumerPage](t, a, space, "consume", consumerInput(route))
	if !page.Initial || len(page.Items) != 1 || page.Items[0].Follow == nil {
		t.Fatal("expected the initial private follow baseline", page)
	}
	in := consumerInput(route)
	in.Receipt = page.Receipt
	return consumerCall[ActivityAgentRoute](t, a, space, "ack", in)
}

func TestActivityConsumerRetentionAndCorruptPosition(t *testing.T) {
	old := time.Now().AddDate(0, 0, -100)
	log := ActivityLog{Seq: 4, Reads: map[string]MemberActivityReadState{"owner": {Through: 4}}, Seen: map[string]bool{}, Consumers: map[string]ActivityConsumer{"owner": {ShadowID: "s", Revision: 1, Through: 2, InitialSeq: 2, Initial: []string{"initial"}}}}
	for i, id := range []string{"initial", "read", "not-received", "unrelated"} {
		e := ActivityEnvelope{ID: id, Seq: uint64(i + 1), CreatedAt: old, Recipients: map[string][]string{}}
		if id != "unrelated" {
			e.Recipients["owner"] = []string{"mention"}
		}
		log.Events = append(log.Events, e)
		log.Seen[id] = true
	}
	log.pruneActivity(time.Now().AddDate(0, 0, -90), nil)
	if len(log.Events) != 2 || log.Events[0].ID != "initial" || log.Events[1].ID != "not-received" {
		t.Fatal(log.Events)
	}
	c := log.Consumers["owner"]
	c.InitialDone, c.Through = 1, 4
	log.Consumers["owner"] = c
	log.pruneActivity(time.Now().AddDate(0, 0, -90), nil)
	if len(log.Events) != 0 {
		t.Fatal("acknowledged read events retained forever")
	}
	_, _, s := spaceFixture(t)
	log.Secret = "test-secret"
	c.InitialDone = -1
	log.Consumers["owner"] = c
	if err := writeJSONFile(filepath.Join(s.runtimeDirectory(), "member-activity.json"), log); err != nil {
		t.Fatal(err)
	}
	s.activityLog = nil
	if err := s.loadActivityLogLocked(); err == nil {
		t.Fatal("corrupt consumer position loaded")
	}
}

func TestActivityConsumerLegacyCreationOrderAndOwnShadowFeedback(t *testing.T) {
	a, _, s := spaceFixture(t)
	first := consumerRegister(t, a, s.record.ID)
	second := consumerRegister(t, a, s.record.ID)
	// Old shared projections have no creation time. Even a paused, unchanged
	// projection must recover it from the owner's durable shadow record.
	s.mu.Lock()
	for _, id := range []string{first.ID, second.ID} {
		entry := s.record.Shadows[id]
		entry.View.CreatedAt = time.Time{}
		s.record.Shadows[id] = entry
	}
	s.mu.Unlock()
	first.CreatedAt = time.Now().Add(-time.Hour)
	second.CreatedAt = first.CreatedAt.Add(-time.Hour)
	shadowOp(t, a, s.record.ID, "sync", shadowInput{ID: first.ID, Snapshot: &first})
	shadowOp(t, a, s.record.ID, "sync", shadowInput{ID: second.ID, Snapshot: &second})
	route := consumerCall[ActivityAgentRoute](t, a, s.record.ID, "ensure", ActivityConsumerInput{})
	if route.ShadowID != second.ID {
		t.Fatal("legacy default ignored the real creation order", route)
	}
	ctx := context.WithValue(context.Background(), activityActorKey{}, SpaceActor{Kind: "shadow", MemberID: "owner", ShadowID: route.ShadowID, Name: "Shadow"})
	n, err := s.annotate(Annotation{Text: "temporary question"}, "Shadow", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := s.activityPreferenceLocked("owner", ActivitySubject{Kind: "discussion", ID: n.ID}); exists {
		t.Fatal("temporary shadow participation created an owner's permanent follow")
	}
	if _, err = s.replyAnnotation(ctx, AnnotationReplyInput{AnnotationID: n.ID, Text: "@Owner resolved", RequestID: uuid.NewString(), Mentions: []MemberMention{{MemberID: "owner", Label: "Owner", StartOffset: 0, EndOffset: 6}}}, "Shadow"); err != nil {
		t.Fatal(err)
	}
	p := consumerCall[ActivityConsumerPage](t, a, s.record.ID, "consume", consumerInput(route))
	if len(p.Items) != 0 || p.Receipt == "" {
		t.Fatal("own shadow fed its output back into a new observation", p)
	}
}
