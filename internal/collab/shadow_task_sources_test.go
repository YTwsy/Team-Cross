package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/nativecodex"
	"teamcross/internal/shadow"
)

func TestShadowTaskSourceReadsOwnAndForeignChangesAfterMuteAcrossCores(t *testing.T) {
	_, s, b, j, n := consumerFixture(t)
	v := registeredToolShadow(t, b, j.ID)
	ctx := context.Background()
	query := shadow.TaskSourceQuery{Subject: ActivitySubject{Kind: "discussion", ID: n.ID}}
	initial, err := b.readShadowTaskSource(ctx, v.ID, query)
	if err != nil {
		t.Fatal(err)
	}
	query.Cursor = initial.Cursor
	var preference MemberFollowPreference
	activityTestRemote(t, j, "follow", ActivityInput{Subject: query.Subject, Mode: "muted", RequestID: "mute-task-source"}, &preference)
	for i := range 25 {
		if i%2 == 0 {
			consumerReply(t, j, n, fmt.Sprintf("own fact %d", i))
			continue
		}
		if _, err := s.replyAnnotation(ctx, AnnotationReplyInput{AnnotationID: n.ID, Text: fmt.Sprintf("peer fact %d", i), RequestID: fmt.Sprintf("task-source-peer-%d", i)}, "Owner"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.annotate(Annotation{Text: "UNRELATED_OBJECT"}, "Owner"); err != nil {
		t.Fatal(err)
	}
	var human ActivityPage
	activityTestRemote(t, j, "list", ActivityInput{}, &human)
	for _, g := range human.Groups {
		if g.Latest.Subject == query.Subject && g.Unread > 0 {
			t.Fatal("muted discussion produced ordinary reminders")
		}
	}
	s.mu.Lock()
	readsBefore := contentHash(s.activityLog.Reads)
	followsBefore := contentHash(s.record.Activity.Preferences)
	s.mu.Unlock()
	seen := map[string]bool{}
	own, peer := 0, 0
	for page := 0; page < 3; page++ {
		out, err := b.readShadowTaskSource(ctx, v.ID, query)
		if err != nil {
			t.Fatal(err)
		}
		if page == 0 && len(out.Signals) != 16 || page == 1 && len(out.Signals) != 9 || page == 2 && len(out.Signals) != 0 {
			t.Fatal("source cursor skipped a page or repeated changes", page, out)
		}
		for _, signal := range out.Signals {
			if seen[signal.ID] || signal.Source.SpaceID != j.ID || signal.Source.AnnotationID != n.ID || signal.Source.ReplyID == "" {
				t.Fatal("source identity or scope changed", signal)
			}
			seen[signal.ID] = true
			if signal.Actor.MemberID == v.Owner.ID {
				own++
			} else {
				peer++
			}
			body, err := b.readShadowSource(ctx, v.ID, signal.Source)
			if err != nil || !strings.Contains(string(body.Data), signal.Preview) {
				t.Fatal("source cannot be read at its exact reference", err, string(body.Data))
			}
		}
		query.Cursor = out.Cursor
	}
	if len(seen) != 25 || own != 13 || peer != 12 {
		t.Fatal("own changes were mistaken for absent personal reminders", own, peer)
	}
	s.mu.Lock()
	readsAfter := contentHash(s.activityLog.Reads)
	followsAfter := contentHash(s.record.Activity.Preferences)
	s.mu.Unlock()
	if readsBefore != readsAfter || followsBefore != followsAfter {
		t.Fatal("task source read changed human preferences or read progress")
	}
	if _, err = s.shadowTaskSourceOperation(ctx, shadowTaskSourceInput{ShadowID: v.ID, Query: query}); err == nil {
		t.Fatal("a different member read this shadow's source route")
	}
}

func TestShadowTaskSourceExecutionKeepsExactTurnAndDiscussionScope(t *testing.T) {
	a, reader, space := multiExecutionFixture(t)
	one := createInSpace(t, a, reader, space, "worktree")
	two := createInSpace(t, a, reader, space, "worktree")
	v := registeredToolShadow(t, a, space.record.ID)
	query := shadow.TaskSourceQuery{Subject: ActivitySubject{Kind: "execution", ID: one.record.ID}}
	first, err := a.readShadowTaskSource(context.Background(), v.ID, query)
	if err != nil {
		t.Fatal(err)
	}
	query.Cursor = first.Cursor
	for _, execution := range []*Session{one, two} {
		if _, err := execution.annotate(Annotation{Text: "discussion for " + execution.record.ID}, "Owner"); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]any{"turn": MaterialTurn{ID: "source-turn-" + execution.record.ID, Status: "completed", Items: []MaterialItem{{ID: "answer", Type: "agentMessage", Text: "completed source evidence"}}}})
		execution.onMessage(nativecodex.Message{Method: "turn/completed", Params: body})
	}
	out, err := a.readShadowTaskSource(context.Background(), v.ID, query)
	if err != nil || len(out.Signals) != 2 {
		t.Fatal("execution changes omitted or sibling included", out, err)
	}
	turn, discussion := false, false
	for _, signal := range out.Signals {
		if signal.Source.ExecutionID != one.record.ID {
			t.Fatal("sibling source escaped exact execution scope", signal)
		}
		turn = turn || signal.Source.TurnID == "source-turn-"+one.record.ID
		discussion = discussion || signal.Source.AnnotationID != ""
	}
	if !turn || !discussion {
		t.Fatal("execution change lost its exact turn or discussion", out)
	}
}

func TestShadowTaskSourceRetentionResyncIsScopedAndExplicit(t *testing.T) {
	_, s, b, j, n := consumerFixture(t)
	v := registeredToolShadow(t, b, j.ID)
	ctx := context.Background()
	query := shadow.TaskSourceQuery{Subject: ActivitySubject{Kind: "discussion", ID: n.ID}}
	first, err := b.readShadowTaskSource(ctx, v.ID, query)
	if err != nil {
		t.Fatal(err)
	}
	query.Cursor = first.Cursor
	for i := range 22 {
		consumerReply(t, j, n, fmt.Sprintf("current source %d", i))
	}
	s.mu.Lock()
	log := activityClone(*s.activityLog)
	for i := range log.Events {
		log.Events[i].CreatedAt = time.Unix(1, 0)
		for member := range log.Events[i].Recipients {
			log.Reads[member] = MemberActivityReadState{Through: log.Seq}
		}
	}
	if !log.pruneActivity(time.Now(), nil) {
		s.mu.Unlock()
		t.Fatal("fixture did not remove old event metadata")
	}
	if err := writeJSONFile(filepath.Join(s.runtimeDirectory(), "member-activity.json"), log); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.activityLog = nil // Query must recover the actual durable log and retention generation.
	s.mu.Unlock()
	out, err := b.readShadowTaskSource(ctx, v.ID, query)
	if err != nil || !out.Resync || len(out.Signals) != 9 || out.Cursor.Retention == query.Cursor.Retention {
		t.Fatal("missing history was hidden or resync was unbounded", out, err)
	}
	for _, signal := range out.Signals {
		if signal.Source.AnnotationID != n.ID || signal.Source.SpaceID != j.ID || signal.State != "source_resync_partial" || !strings.Contains(signal.Preview, "incomplete") {
			t.Fatal("resync silently claimed full history", signal)
		}
	}
	query.Cursor = out.Cursor
	again, err := b.readShadowTaskSource(ctx, v.ID, query)
	if err != nil || again.Resync || len(again.Signals) != 0 {
		t.Fatal("accepted resync repeated", again, err)
	}
}

func TestShadowTaskSourceMaterialIncludesChildrenAndWithdrawal(t *testing.T) {
	a, native, s := spaceFixture(t)
	v := registeredToolShadow(t, a, s.record.ID)
	ctx := context.Background()
	draft := materialDraft(t, a, native)
	published, err := a.Publish(ctx, s.record.ID, PublishInput{PreviewID: draft.ID, PreviewHash: draft.Hash, RequestID: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	id := published.(map[string]any)["materialId"].(string)
	query := shadow.TaskSourceQuery{Subject: ActivitySubject{Kind: "material", ID: id}}
	first, err := a.readShadowTaskSource(ctx, v.ID, query)
	if err != nil || len(first.Signals) != 1 || first.Signals[0].Source.Version != 1 {
		t.Fatal("publication lost its fixed version", first, err)
	}
	query.Cursor = first.Cursor
	s.mu.Lock()
	s.record.Annotations = append(s.record.Annotations, Annotation{ID: "material-discussion", Text: "version-one discussion", Target: &AnnotationTarget{Kind: "material", MaterialID: id, Version: 1}, Actor: SpaceActor{Kind: "human", MemberID: "owner"}, CreatedAt: time.Now()})
	err = s.saveLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	child, err := a.readShadowTaskSource(ctx, v.ID, query)
	if err != nil || len(child.Signals) != 1 || child.Signals[0].Source.AnnotationID != "material-discussion" {
		t.Fatal("material relationship omitted its child discussion", child, err)
	}
	query.Cursor = child.Cursor
	if _, err := s.withdrawMaterial(ctx, id); err != nil {
		t.Fatal(err)
	}
	withdrawn, err := a.readShadowTaskSource(ctx, v.ID, query)
	if err != nil || len(withdrawn.Signals) != 1 || withdrawn.Signals[0].State != "material_withdrawn" {
		t.Fatal("withdrawal was silently skipped", withdrawn, err)
	}
	body, err := a.readShadowSource(ctx, v.ID, withdrawn.Signals[0].Source)
	if err != nil || body.Unavailable != "material_withdrawn" {
		t.Fatal("withdrawal still exposed an old source body", body, err)
	}
}
