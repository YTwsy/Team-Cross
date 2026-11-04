package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/nativecodex"
)

func activityTestPage(t *testing.T, s *Session) ActivityPage {
	t.Helper()
	out, err := s.activityOperation(context.Background(), "list", ActivityInput{})
	if err != nil {
		t.Fatal(err)
	}
	return out.(ActivityPage)
}
func activityTestRemote(t *testing.T, j *Joined, op string, in any, out any) {
	t.Helper()
	if err := j.request(context.Background(), "POST", "/v2/member-activity/"+op, in, out); err != nil {
		t.Fatal(err)
	}
}
func TestMemberActivityMentionsRepliesAndSharedRead(t *testing.T) {
	a, _, s := spaceFixture(t)
	b, _, _ := fixture(t)
	c, _, _ := fixture(t)
	stopWorkbenchPump(b)
	stopWorkbenchPump(c)
	if err := s.Share(context.Background(), "lan"); err != nil {
		t.Fatal(err)
	}
	jb, err := b.Join(context.Background(), s.sharedRuntime().Token())
	if err != nil {
		t.Fatal(err)
	}
	jc, err := c.Join(context.Background(), s.sharedRuntime().Token())
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.annotate(Annotation{Text: "review", RequestID: "root"}, "发起者")
	if err != nil {
		t.Fatal(err)
	}
	var answer Annotation
	if err := jb.request(context.Background(), "POST", "/v2/annotation-replies", AnnotationReplyInput{AnnotationID: root.ID, Text: "@Owner answer", RequestID: "reply", Mentions: []MemberMention{{MemberID: "owner", StartOffset: 0, EndOffset: 6, Label: "Owner"}}}, &answer); err != nil {
		t.Fatal(err)
	}
	page := activityTestPage(t, s)
	if page.Unread != 1 || len(page.Groups) != 1 || len(page.Groups[0].Latest.Reasons) != 3 {
		t.Fatalf("merged reasons: %+v", page)
	}
	var cp ActivityPage
	activityTestRemote(t, jc, "list", ActivityInput{}, &cp)
	if cp.Unread != 0 {
		t.Fatal("broadcast to uninvolved member", cp)
	}
	if _, err = s.activityOperation(context.Background(), "read", ActivityInput{Snapshot: page.Snapshot}); err != nil {
		t.Fatal(err)
	}
	if activityTestPage(t, s).Unread != 0 {
		t.Fatal("read not shared")
	}
	// Reply-to-reply belongs to B, not the root author. Root is still followed by owner.
	var nested Annotation
	if err := jc.request(context.Background(), "POST", "/v2/annotation-replies", AnnotationReplyInput{AnnotationID: root.ID, Text: "nested", RequestID: "nested", ReplyTo: &ReplyTarget{Kind: "reply", ID: answer.Replies[0].ID}}, &nested); err != nil {
		t.Fatal(err)
	}
	var bp ActivityPage
	activityTestRemote(t, jb, "list", ActivityInput{}, &bp)
	if bp.Unread != 1 || !strings.Contains(strings.Join(bp.Groups[0].Latest.Reasons, ","), "direct_reply") {
		t.Fatal(bp)
	}
	if activityTestPage(t, s).Unread != 1 {
		t.Fatal("snapshot cleared a later reply")
	}
	// Stored outbox and receipts are never returned in public views.
	raw, _ := json.Marshal(s.view())
	if strings.Contains(string(raw), "recipients") || strings.Contains(string(raw), "followCommands") || strings.Contains(string(raw), "memberActivity") {
		t.Fatal("private state leaked")
	}
	_ = a
}
func TestMemberActivityPreferenceHierarchyAndIdempotency(t *testing.T) {
	_, _, s := spaceFixture(t)
	b, _, _ := fixture(t)
	stopWorkbenchPump(b)
	ctx := context.Background()
	if err := s.Share(ctx, "lan"); err != nil {
		t.Fatal(err)
	}
	j, err := b.Join(ctx, s.sharedRuntime().Token())
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.annotate(Annotation{Text: "review"}, "Owner")
	if err != nil {
		t.Fatal(err)
	}
	subject := ActivitySubject{Kind: "discussion", ID: root.ID}
	out, err := s.activityOperation(ctx, "follow", ActivityInput{Subject: subject, Mode: "direct_only", Revision: 1, RequestID: "cancel"})
	if err != nil {
		t.Fatal(err)
	}
	pref := out.(MemberFollowPreference)
	if _, err = s.activityOperation(ctx, "follow", ActivityInput{Subject: subject, Mode: "following", Revision: 1, RequestID: "cancel"}); err == nil {
		t.Fatal("changed request accepted")
	}
	if _, err = s.replyAnnotation(ctx, AnnotationReplyInput{AnnotationID: root.ID, Text: "I reply again", RequestID: "self"}, "Owner"); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.activityPreferenceLocked("owner", subject); p.Mode != "direct_only" {
		t.Fatal("automatic follow overrode cancellation")
	}
	var updated Annotation
	if err = j.request(ctx, "POST", "/v2/annotation-replies", AnnotationReplyInput{AnnotationID: root.ID, Text: "direct", RequestID: "direct"}, &updated); err != nil {
		t.Fatal(err)
	}
	page := activityTestPage(t, s)
	if page.Unread != 1 || strings.Contains(strings.Join(page.Groups[0].Latest.Reasons, ","), "follow_") {
		t.Fatal(page)
	}
	if _, err = s.activityOperation(ctx, "follow", ActivityInput{Subject: subject, Mode: "muted", Revision: pref.Revision, RequestID: "mute"}); err != nil {
		t.Fatal(err)
	}
	if err = j.request(ctx, "POST", "/v2/annotation-replies", AnnotationReplyInput{AnnotationID: root.ID, Text: "muted", RequestID: "muted"}, &updated); err != nil {
		t.Fatal(err)
	}
	after := activityTestPage(t, s)
	if len(after.Groups[0].Events) != 1 {
		t.Fatal("muting added or deleted existing reminders", after)
	}
}
func TestMemberActivityOutboxCrashAndConcurrentReads(t *testing.T) {
	_, _, s := spaceFixture(t)
	ctx := context.Background()
	// Block log writes while keeping the source record writable.
	logPath := filepath.Join(s.runtimeDirectory(), "member-activity.json")
	if err := os.Mkdir(logPath, 0700); err != nil {
		t.Fatal(err)
	}
	note, err := s.annotate(Annotation{Text: "durable"}, "Owner")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.record.Activity.Outbox) != 2 || s.record.Activity.Outbox[0].Follow == nil || s.record.Activity.Outbox[1].Kind != "comment" {
		t.Fatal("source committed without both the private follow baseline and public event")
	}
	var disk Record
	if err = readJSON(filepath.Join(s.runtimeDirectory(), "collaboration.json"), &disk); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	recovered := s.app.newSession(disk)
	if _, err = recovered.activityOperation(ctx, "list", ActivityInput{View: "space"}); err != nil {
		t.Fatal(err)
	}
	if len(recovered.activityLog.Events) != 2 || recovered.activityLog.Events[0].Source.AnnotationID != note.ID || recovered.activityLog.Events[1].Source.AnnotationID != note.ID {
		t.Fatal(recovered.activityLog)
	}
	if err = recovered.drainActivityLocked(); err != nil || len(recovered.activityLog.Events) != 2 {
		t.Fatal("replay duplicated", err)
	}
	// Concurrent old-watermark confirmations cannot lower a newer watermark.
	initial := activityTestPage(t, recovered)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := recovered.activityOperation(ctx, "read", ActivityInput{Snapshot: initial.Snapshot})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if recovered.activityLog.Reads["owner"].Through != 2 {
		t.Fatal("read watermark lost")
	}
}
func TestMemberActivityMentionValidationAndReplyParent(t *testing.T) {
	_, _, s := spaceFixture(t)
	ctx := context.Background()
	if _, err := s.annotate(Annotation{Text: "@Missing", Mentions: []MemberMention{{MemberID: "absent", Label: "Missing", StartOffset: 0, EndOffset: 8}}}, "Owner"); err == nil {
		t.Fatal("revoked mention accepted")
	}
	root, err := s.annotate(Annotation{Text: "root"}, "Owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.replyAnnotation(ctx, AnnotationReplyInput{AnnotationID: root.ID, Text: "bad", RequestID: "bad", ReplyTo: &ReplyTarget{Kind: "reply", ID: "other"}}, "Owner"); err == nil {
		t.Fatal("foreign parent accepted")
	}
	in := AnnotationReplyInput{AnnotationID: root.ID, Text: "🙂 @Owner", RequestID: "emoji", Mentions: []MemberMention{{MemberID: "owner", Label: "Owner", StartOffset: 3, EndOffset: 9}}}
	if _, err = s.replyAnnotation(ctx, in, "Owner"); err != nil {
		t.Fatal(err)
	}
	in.Mentions = nil
	if _, err = s.replyAnnotation(ctx, in, "Owner"); err == nil {
		t.Fatal("same request changed mention semantics")
	}
	if activityTestPage(t, s).Unread != 0 {
		t.Fatal("self mention notified")
	}
}
func TestMemberActivityTurnInitiatorSurvivesHandoff(t *testing.T) {
	a, runtime, _ := fixture(t)
	stopWorkbenchPump(a)
	s := createFixture(t, a, runtime, "existing")
	ctx := context.Background()
	// Capture a real accepted input relation, then change current writer before completion.
	s.mu.Lock()
	s.activityStateLocked().Inputs["accepted"] = ActivityTurnInput{MemberID: "owner", Method: "turn/start", State: "pending"}
	if err := s.saveLocked(); err != nil {
		t.Fatal(err)
	}
	s.mu.Unlock()
	encode := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	s.onMessage(nativecodex.Message{Method: "turn/started", Params: encode(map[string]any{"turn": map[string]string{"id": "activity-turn"}})})
	s.mu.Lock()
	s.writer = "some-other-member"
	s.mu.Unlock()
	msg := nativecodex.Message{Method: "turn/completed", Params: encode(map[string]any{"turn": MaterialTurn{ID: "activity-turn", Status: "completed", Items: []MaterialItem{{ID: "answer", Type: "agentMessage", Text: "done"}}}})}
	s.onMessage(msg)
	s.onMessage(msg)
	page := activityTestPage(t, s.activityRoot())
	if page.Unread != 1 || page.Groups[0].Latest.Kind != "turn_completed" || page.Groups[0].Latest.Preview != "done" {
		t.Fatal(page)
	}
	if len(page.Groups[0].Events) != 1 {
		t.Fatal("duplicate completion")
	}
	_ = ctx
}
func TestMemberActivityMaterialVersionsAndParentMute(t *testing.T) {
	a, f, s := spaceFixture(t)
	ctx := context.Background()
	draft := materialDraft(t, a, f)
	out, err := a.Publish(ctx, s.record.ID, PublishInput{PreviewID: draft.ID, PreviewHash: draft.Hash, RequestID: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	id := out.(map[string]any)["materialId"].(string)
	p, ok := s.activityPreferenceLocked("owner", ActivitySubject{Kind: "material", ID: id})
	if !ok || p.Mode != "following" {
		t.Fatal("publisher not followed")
	}
	s.mu.Lock()
	target := AnnotationTarget{Kind: "material", MaterialID: id, Version: 1}
	s.record.Annotations = append(s.record.Annotations, Annotation{ID: "thread", Text: "thread", Target: &target, Actor: SpaceActor{Kind: "human", MemberID: "owner"}, CreatedAt: time.Now()})
	err = s.saveLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	subject := ActivitySubject{Kind: "discussion", ID: "thread"}
	if mode, _ := s.activityEffectiveLocked("owner", subject); mode != "following" {
		t.Fatal(mode)
	}
	if _, err = s.activityOperation(ctx, "follow", ActivityInput{Subject: p.Subject, Mode: "muted", Revision: p.Revision, RequestID: "mute-material"}); err != nil {
		t.Fatal(err)
	}
	if mode, _ := s.activityEffectiveLocked("owner", subject); mode != "muted" {
		t.Fatal("child bypassed parent mute")
	}
}

func TestMemberActivityRetentionCapacityAndCursors(t *testing.T) {
	_, _, s := spaceFixture(t)
	ctx := context.Background()
	for i := range 3 {
		if _, err := s.annotate(Annotation{Text: fmt.Sprint(i)}, "Owner"); err != nil {
			t.Fatal(err)
		}
	}
	result, err := s.activityOperation(ctx, "follows", ActivityInput{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	first := result.(FollowPage)
	if len(first.Items) != 1 || first.Next == "" || first.Total != 3 {
		t.Fatal(first)
	}
	result, err = s.activityOperation(ctx, "follows", ActivityInput{Limit: 1, Cursor: first.Next})
	if err != nil || result.(FollowPage).Items[0].Subject == first.Items[0].Subject {
		t.Fatal(result, err)
	}
	if _, err = s.activityOperation(ctx, "follow", ActivityInput{Subject: first.Items[0].Subject, Mode: "direct_only", Revision: 1, RequestID: "page-change"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.activityOperation(ctx, "follows", ActivityInput{Cursor: first.Next}); err == nil {
		t.Fatal("stale follow page accepted")
	}
	// All retained reminders are readable before paging. A concurrent read invalidates unread offset cursors.
	s.mu.Lock()
	for i := range s.activityLog.Events {
		s.activityLog.Events[i].Recipients = map[string][]string{"owner": {"mention"}}
	}
	s.mu.Unlock()
	result, err = s.activityOperation(ctx, "list", ActivityInput{Limit: 1, Unread: true})
	if err != nil {
		t.Fatal(err)
	}
	page := result.(ActivityPage)
	if _, err = s.activityOperation(ctx, "read", ActivityInput{EventIDs: []string{page.Groups[0].Latest.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.activityOperation(ctx, "list", ActivityInput{Limit: 1, Unread: true, Cursor: page.Next}); err == nil {
		t.Fatal("stale unread cursor accepted")
	}
	log := activityClone(*s.activityLog)
	for i := range log.Events {
		log.Events[i].CreatedAt = time.Now().AddDate(0, 0, -91)
	}
	if !log.pruneActivity(time.Now().AddDate(0, 0, -90), nil) || len(log.Events) != 2 {
		t.Fatal("retention deleted unread or retained read", len(log.Events))
	}
	for member, read := range log.Reads {
		read.Through = log.Seq
		log.Reads[member] = read
	}
	pending := map[string]bool{log.Events[0].ID: true}
	log.pruneActivity(time.Now().AddDate(0, 0, -90), pending)
	if len(log.Events) != 1 || !pending[log.Events[0].ID] {
		t.Fatal("pending replay identity lost")
	}
	s.mu.Lock()
	for i := range maxFollowPreferences {
		s.activityStateLocked().Preferences["owner"][fmt.Sprint(i)] = MemberFollowPreference{}
	}
	before := len(s.record.Annotations)
	s.mu.Unlock()
	if _, err = s.annotate(Annotation{Text: "must not commit"}, "Owner"); err == nil {
		t.Fatal("auto-follow overflow silently accepted")
	}
	if len(s.record.Annotations) != before {
		t.Fatal("rejected business write was retained")
	}
}

func TestMemberActivityOfflineReadAndRevocation(t *testing.T) {
	_, _, s := spaceFixture(t)
	b, _, _ := fixture(t)
	stopWorkbenchPump(b)
	ctx := context.Background()
	if err := s.Share(ctx, "lan"); err != nil {
		t.Fatal(err)
	}
	j, err := b.Join(ctx, s.sharedRuntime().Token())
	if err != nil {
		t.Fatal(err)
	}
	member := firstRemote(s)
	if _, err = s.annotate(Annotation{Text: "@B one", Mentions: []MemberMention{{MemberID: member, Label: "B", EndOffset: 2}}}, "Owner"); err != nil {
		t.Fatal(err)
	}
	value, err := b.remoteMemberActivity(ctx, j, "list", ActivityInput{})
	if err != nil {
		t.Fatal(err)
	}
	page := value.(ActivityPage)
	if page.Unread != 1 {
		t.Fatal(page)
	}
	j.mu.Lock()
	original := j.Client
	j.Client = nil
	j.mu.Unlock()
	value, err = b.remoteMemberActivity(ctx, j, "list", ActivityInput{})
	if err != nil || !value.(ActivityPage).PendingSync || value.(ActivityPage).Unread != 1 {
		t.Fatal(value, err)
	}
	if value.(ActivityPage).Groups[0].Latest.Preview != "" {
		t.Fatal("offline cache retained source text")
	}
	value, err = b.remoteMemberActivity(ctx, j, "read", ActivityInput{Snapshot: page.Snapshot})
	if err != nil || !value.(ActivityPage).PendingSync {
		t.Fatal(value, err)
	}
	// A new event after the offline snapshot must remain unread after retry.
	if _, err = s.annotate(Annotation{Text: "@B two", Mentions: []MemberMention{{MemberID: member, Label: "B", EndOffset: 2}}}, "Owner"); err != nil {
		t.Fatal(err)
	}
	j.mu.Lock()
	j.Client = original
	j.mu.Unlock()
	b.flushActivityReads(ctx)
	value, err = b.remoteMemberActivity(ctx, j, "list", ActivityInput{})
	if err != nil || value.(ActivityPage).Unread != 1 {
		t.Fatal(value, err)
	}
	q, err := b.loadActivityQueueLocked()
	if err != nil || len(q) != 0 {
		t.Fatal(q, err)
	}
	if err = s.RevokeMember(member); err != nil {
		t.Fatal(err)
	}
	if value, err = b.remoteMemberActivity(ctx, j, "list", ActivityInput{}); err == nil {
		t.Fatal("revoked member received cached content", value)
	}
}

func TestMemberActivityPrivateBoundaryAndCorruptLog(t *testing.T) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	ctx := context.WithValue(context.Background(), runtimeAnnotationKey{}, "bound-runtime")
	f.mu.Lock()
	before := len(f.calls)
	f.mu.Unlock()
	if _, err := s.activityRoot().activityOperation(ctx, "follow", ActivityInput{Subject: ActivitySubject{Kind: "execution", ID: s.record.ID}, Mode: "following", RequestID: "private"}); err == nil {
		t.Fatal("runtime reached personal follows")
	}
	f.mu.Lock()
	after := len(f.calls)
	f.mu.Unlock()
	if before != after {
		t.Fatal("private rejection started a native history read")
	}
	root := s.activityRoot()
	root.mu.Lock()
	root.activityLog = nil
	if err := os.WriteFile(filepath.Join(root.runtimeDirectory(), "member-activity.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	root.mu.Unlock()
	if _, err := root.activityOperation(context.Background(), "list", ActivityInput{}); err == nil {
		t.Fatal("corrupt log silently replaced")
	}
}

func TestMemberActivityKnownAgentOwnerDoesNotSuppressReply(t *testing.T) {
	_, _, s := spaceFixture(t)
	root, err := s.annotate(Annotation{Text: "review"}, "Owner")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.workbenchReply(context.Background(), workbenchInput{Reply: AnnotationReplyInput{AnnotationID: root.ID, RequestID: "agent", Text: "agent answer"}, Actor: SpaceActor{Kind: "session", Provider: "codex", Session: strings.Repeat("a", 24)}})
	if err != nil {
		t.Fatal(err)
	}
	page := activityTestPage(t, s)
	if page.Unread != 1 || page.Groups[0].Latest.Actor.Kind != "session" || page.Groups[0].Latest.Actor.MemberID != "owner" {
		t.Fatal("own agent reply was treated as human self input", page)
	}
}

func TestMemberActivitySteerAcceptedAfterCompletion(t *testing.T) {
	encode := func(v any) json.RawMessage { data, _ := json.Marshal(v); return data }
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	s.mu.Lock()
	state := s.activityStateLocked()
	state.Turns["steered"] = ActivityTurn{ID: "steered", State: "inProgress", Members: []string{"owner"}}
	state.Inputs["steer"] = ActivityTurnInput{Method: "turn/steer", MemberID: "owner", TurnID: "steered", State: "pending"}
	s.mu.Unlock()
	s.onMessage(nativecodex.Message{Method: "turn/completed", Params: encode(map[string]any{"turn": MaterialTurn{ID: "steered", Status: "completed", Items: []MaterialItem{{ID: "final", Type: "agentMessage", Phase: "final_answer", Text: "finished"}}}})})
	if activityTestPage(t, s.activityRoot()).Unread != 0 {
		t.Fatal("terminal reminder published before pending accepted input resolved")
	}
	s.mu.Lock()
	s.activityInputResultLocked("steer", encode(map[string]any{"turnId": "steered"}), "completed")
	err := s.saveLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	page := activityTestPage(t, s.activityRoot())
	if page.Unread != 1 || len(page.Groups[0].Events) != 1 {
		t.Fatal(page)
	}
}
