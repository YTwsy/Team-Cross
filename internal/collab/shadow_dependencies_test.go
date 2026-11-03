package collab

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/shadow"
	"teamcross/internal/spacecontext"
)

func TestShadowDependencyReadsOwnersAnswerWithoutPersonalReminder(t *testing.T) {
	_, s, b, j, n := consumerFixture(t)
	v := registeredToolShadow(t, b, j.ID)
	out, err := b.shadowToolCall(context.Background(), v.ID, "reply_to_annotation", map[string]any{"annotationId": n.ID, "requestId": uuid.NewString(), "text": "Which exact client version is required?"})
	if err != nil {
		t.Fatal(err)
	}
	question := workbenchDecode[struct {
		Reply AnnotationReply `json:"reply"`
	}](out).Reply
	var answer Annotation
	if err := j.request(context.Background(), "POST", "/v2/annotation-replies", AnnotationReplyInput{AnnotationID: n.ID, ReplyTo: &ReplyTarget{Kind: "reply", ID: question.ID}, Text: "I need old client version 3.", RequestID: uuid.NewString()}, &answer); err != nil {
		t.Fatal(err)
	}
	answerID := answer.Replies[len(answer.Replies)-1].ID
	for i := range 25 {
		consumerReply(t, j, n, fmt.Sprintf("Unrelated root reply %d", i))
	}
	var before ActivityPage
	activityTestRemote(t, j, "list", ActivityInput{}, &before)
	for _, g := range before.Groups {
		for _, event := range g.Events {
			if event.Source.ReplyID == answerID {
				t.Fatal("owner answer unexpectedly reminded the same owner")
			}
		}
	}
	d := shadow.TaskDependency{ID: "versions", Kind: "discussion", Condition: "Version is specified", Source: SpaceSourceRef{SpaceID: j.ID, AnnotationID: n.ID, ReplyID: question.ID}}
	first, err := b.readShadowDependency(context.Background(), v.ID, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Signals) != 1 || first.Signals[0].Source.SpaceID != j.ID || first.Signals[0].Source.ReplyID != answerID || first.Signals[0].Actor.MemberID != v.Owner.ID {
		t.Fatal("exact owner answer was lost or unrelated replies included", first)
	}
	d.Cursor = first.Cursor
	again, err := b.readShadowDependency(context.Background(), v.ID, d)
	if err != nil || len(again.Signals) != 0 || again.Cursor != first.Cursor {
		t.Fatal("unchanged answer rediscovered", again, err)
	}
	var after ActivityPage
	activityTestRemote(t, j, "list", ActivityInput{}, &after)
	if before.ReadRevision != after.ReadRevision || before.Unread != after.Unread {
		t.Fatal("dependency read changed human read state")
	}
	// Another member cannot consume this shadow's dependency route.
	_, err = s.shadowDependencyOperation(context.Background(), shadowDependencyInput{ShadowID: v.ID, Dependency: shadow.TaskDependency{Kind: "discussion", Source: SpaceSourceRef{SpaceID: s.record.ID, AnnotationID: n.ID}}})
	if err == nil {
		t.Fatal("foreign shadow identity accepted")
	}
}

func TestShadowDependencyResolvesLostQuestionReceiptByActualActorAndAction(t *testing.T) {
	for _, toolName := range []string{"reply_to_annotation", "add_annotation"} {
		t.Run(toolName, func(t *testing.T) {
			_, _, b, j, note := consumerFixture(t)
			v := registeredToolShadow(t, b, j.ID)
			other := registeredToolShadow(t, b, j.ID)
			ctx := context.Background()
			actionID := uuid.NewString()
			d := shadow.TaskDependency{ID: "question", Kind: "discussion", QuestionID: "private-question-id", QuestionActionID: actionID, QuestionTool: toolName, QuestionSourcePending: true, Source: SpaceSourceRef{SpaceID: j.ID}}
			args := map[string]any{"requestId": actionID, "text": "Which client version must we support?"}
			if toolName == "reply_to_annotation" {
				d.Source.AnnotationID, args["annotationId"] = note.ID, note.ID
			}
			missing, err := b.readShadowDependency(ctx, v.ID, d)
			if err != nil || missing.QuestionSource != nil || len(missing.Signals) != 0 {
				t.Fatal("not-yet-posted question fabricated a source", missing, err)
			}
			if _, err := b.shadowToolCall(ctx, v.ID, toolName, args); err != nil {
				t.Fatal(err)
			}
			found, err := b.readShadowDependency(ctx, v.ID, d)
			if err != nil || found.QuestionSource == nil || found.QuestionSource.SpaceID != j.ID || found.QuestionSource.AnnotationID == "" || len(found.Signals) != 0 {
				t.Fatal("saved question source not recovered across Core aliases", found, err)
			}
			ref := *found.QuestionSource
			foreign, err := b.readShadowDependency(ctx, other.ID, d)
			if err != nil || foreign.QuestionSource != nil {
				t.Fatal("another shadow adopted the command", foreign, err)
			}
			parent := &ReplyTarget{Kind: "annotation", ID: ref.AnnotationID}
			if toolName == "reply_to_annotation" {
				if ref.ReplyID == "" {
					t.Fatal("reply question lost exact source")
				}
				parent = &ReplyTarget{Kind: "reply", ID: ref.ReplyID}
			}
			var updated Annotation
			if err := j.request(ctx, "POST", "/v2/annotation-replies", AnnotationReplyInput{AnnotationID: ref.AnnotationID, ReplyTo: parent, Text: "Client v3, including the legacy parser.", RequestID: uuid.NewString()}, &updated); err != nil {
				t.Fatal(err)
			}
			answer := updated.Replies[len(updated.Replies)-1]
			candidate := spacecontext.Signal{Kind: "reply", Source: SpaceSourceRef{SpaceID: j.ID, AnnotationID: ref.AnnotationID, ReplyID: answer.ID}}
			routed, err := b.resolveShadowDependency(ctx, v.ID, d, candidate)
			if err != nil || routed == nil || routed.QuestionSource == nil || routed.QuestionSource.ReplyID != ref.ReplyID || routed.Source.ReplyID != answer.ID || routed.Actor.MemberID != v.Owner.ID {
				t.Fatal("early owner answer did not carry exact question source", routed, err)
			}
			candidate.Source.AnnotationID = "unrelated-root"
			unrelated, err := b.resolveShadowDependency(ctx, v.ID, d, candidate)
			if err != nil || unrelated != nil {
				t.Fatal("pending question accepted unrelated reply", unrelated, err)
			}
			page, err := b.readShadowDependency(ctx, v.ID, d)
			if err != nil || page.QuestionSource == nil || len(page.Signals) != 1 || page.Signals[0].Source.ReplyID != answer.ID {
				t.Fatal("read-only recovery lost actual answer", page, err)
			}
			d.Source, d.QuestionSourcePending, d.Cursor = *page.QuestionSource, false, page.Cursor
			again, err := b.readShadowDependency(ctx, v.ID, d)
			if err != nil || len(again.Signals) != 0 {
				t.Fatal("recovered question replayed answers", again, err)
			}
		})
	}
}

func TestShadowDependencyRoutesExactReplyBranchesAfterCursorAdvanced(t *testing.T) {
	_, _, b, j, note := consumerFixture(t)
	v := registeredToolShadow(t, b, j.ID)
	question := func(text string) AnnotationReply {
		t.Helper()
		out, err := b.shadowToolCall(context.Background(), v.ID, "reply_to_annotation", map[string]any{"annotationId": note.ID, "requestId": uuid.NewString(), "text": text})
		if err != nil {
			t.Fatal(err)
		}
		return workbenchDecode[struct {
			Reply AnnotationReply `json:"reply"`
		}](out).Reply
	}
	one, two := question("Which version?"), question("A separate deadline question")
	answer := func(parent, text string) AnnotationReply {
		t.Helper()
		var updated Annotation
		if err := j.request(context.Background(), "POST", "/v2/annotation-replies", AnnotationReplyInput{AnnotationID: note.ID, ReplyTo: &ReplyTarget{Kind: "reply", ID: parent}, RequestID: uuid.NewString(), Text: text}, &updated); err != nil {
			t.Fatal(err)
		}
		return updated.Replies[len(updated.Replies)-1]
	}
	first := answer(one.ID, "Version 3")
	descendant := answer(first.ID, "Including the old parser variant")
	other := answer(two.ID, "Friday")
	d := shadow.TaskDependency{ID: "version", Kind: "discussion", Source: SpaceSourceRef{SpaceID: j.ID, AnnotationID: note.ID, ReplyID: one.ID}}
	page, err := b.readShadowDependency(context.Background(), v.ID, d)
	if err != nil {
		t.Fatal(err)
	}
	d.Cursor = page.Cursor
	for _, test := range []struct {
		reply AnnotationReply
		match bool
	}{{first, true}, {descendant, true}, {other, false}, {one, false}} {
		candidate := spacecontext.Signal{ID: uuid.NewString(), Kind: "reply", Source: SpaceSourceRef{SpaceID: j.ID, AnnotationID: note.ID, ReplyID: test.reply.ID}}
		got, err := b.resolveShadowDependency(context.Background(), v.ID, d, candidate)
		if err != nil || (got != nil) != test.match {
			t.Fatal("wrong dependency branch", test.reply.ID, got, err)
		}
		if got != nil && (got.Source.ReplyID != test.reply.ID || got.Actor.MemberID != v.Owner.ID || got.Source.SpaceID != j.ID) {
			t.Fatal("remote routing lost source or author", got)
		}
	}
	forged := spacecontext.Signal{Kind: "reply", Source: SpaceSourceRef{SpaceID: j.ID, AnnotationID: "other-root", ReplyID: first.ID}}
	if _, err := b.resolveShadowDependency(context.Background(), v.ID, d, forged); err == nil {
		t.Fatal("different root accepted")
	}
}

func TestShadowDependencyRoutingRetainsTaskOnPreviousDefault(t *testing.T) {
	_, _, b, j, note := consumerFixture(t)
	old, next := registeredToolShadow(t, b, j.ID), registeredToolShadow(t, b, j.ID)
	out, err := b.shadowToolCall(context.Background(), old.ID, "reply_to_annotation", map[string]any{"annotationId": note.ID, "requestId": uuid.NewString(), "text": "Which client version?"})
	if err != nil {
		t.Fatal(err)
	}
	question := workbenchDecode[struct {
		Reply AnnotationReply `json:"reply"`
	}](out).Reply
	var updated Annotation
	if err := j.request(context.Background(), "POST", "/v2/annotation-replies", AnnotationReplyInput{AnnotationID: note.ID, ReplyTo: &ReplyTarget{Kind: "reply", ID: question.ID}, RequestID: uuid.NewString(), Text: "Client v3"}, &updated); err != nil {
		t.Fatal(err)
	}
	answer := updated.Replies[len(updated.Replies)-1]
	b.mu.Lock()
	previous := b.shadows[old.ID]
	b.mu.Unlock()
	_, _, root, _ := previous.Identity()
	previous.Close()
	path := filepath.Join(root, "record", "shadow.json")
	var record shadow.Record
	if err := readJSON(path, &record); err != nil {
		t.Fatal(err)
	}
	record.Jobs["kept-task"] = shadow.Job{ID: "kept-task", Version: 1, Revision: 1, State: "waiting", Dependencies: []shadow.TaskDependency{{ID: "question", Kind: "discussion", State: "waiting", Source: SpaceSourceRef{SpaceID: j.ID, AnnotationID: note.ID, ReplyID: question.ID}}}}
	if err := writeJSONFile(path, record); err != nil {
		t.Fatal(err)
	}
	cfg := b.shadowConfig(old.ID)
	cfg.Directory = filepath.Dir(path)
	restored, err := shadow.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Close)
	b.mu.Lock()
	b.shadows[old.ID] = restored
	b.mu.Unlock()
	signal := spacecontext.Signal{ID: uuid.NewString(), Kind: "reply", Source: SpaceSourceRef{SpaceID: j.ID, AnnotationID: note.ID, ReplyID: answer.ID}}
	stamp := b.shadowActivityRouteStamp(next.ID)
	deliveries, err := b.routeShadowActivity(context.Background(), next.ID, signal)
	if err != nil || len(deliveries) != 1 || deliveries[0].ShadowID != old.ID || deliveries[0].JobID != "kept-task" {
		t.Fatal("default change stole or duplicated existing task", deliveries, err)
	}
	if stamp == b.shadowActivityRouteStamp(next.ID) {
		t.Fatal("task update did not invalidate prepared route checks")
	}
	jobs := restored.View().Jobs
	if len(jobs) != 1 || jobs[0].State != "queued" || len(jobs[0].Dependencies[0].Signals) != 1 {
		t.Fatal("exact task did not receive answer", jobs)
	}
	if _, err := b.routeShadowActivity(context.Background(), next.ID, signal); err != nil {
		t.Fatal(err)
	}
	if len(restored.View().Jobs[0].Dependencies[0].Signals) != 1 {
		t.Fatal("repeated route duplicated answer")
	}
}

func TestShadowDependencyPagesAllAnswersAndIncludesReplyDescendants(t *testing.T) {
	_, _, b, j, n := consumerFixture(t)
	v := registeredToolShadow(t, b, j.ID)
	out, err := b.shadowToolCall(context.Background(), v.ID, "reply_to_annotation", map[string]any{"annotationId": n.ID, "requestId": uuid.NewString(), "text": "Provide the requested evidence."})
	if err != nil {
		t.Fatal(err)
	}
	q := workbenchDecode[struct {
		Reply AnnotationReply `json:"reply"`
	}](out).Reply
	parent := q.ID
	expected := map[string]bool{}
	for i := range 40 {
		var note Annotation
		if err := j.request(context.Background(), "POST", "/v2/annotation-replies", AnnotationReplyInput{AnnotationID: n.ID, ReplyTo: &ReplyTarget{Kind: "reply", ID: parent}, RequestID: uuid.NewString(), Text: fmt.Sprintf("Evidence item %d", i)}, &note); err != nil {
			t.Fatal(err)
		}
		id := note.Replies[len(note.Replies)-1].ID
		expected[id] = true
		if i == 0 {
			parent = id
		}
	}
	d := shadow.TaskDependency{ID: "evidence", Kind: "discussion", Condition: "Evidence received", Source: SpaceSourceRef{SpaceID: j.ID, AnnotationID: n.ID, ReplyID: q.ID}}
	for page := 0; page < 4; page++ {
		update, err := b.readShadowDependency(context.Background(), v.ID, d)
		if err != nil {
			t.Fatal(err)
		}
		if len(update.Signals) > 16 {
			t.Fatal("unbounded packet")
		}
		for _, signal := range update.Signals {
			if !expected[signal.Source.ReplyID] {
				t.Fatal("duplicate or wrong source", signal)
			}
			delete(expected, signal.Source.ReplyID)
		}
		d.Cursor = update.Cursor
	}
	if len(expected) != 0 {
		t.Fatal("answers beyond the recent activity page were lost", len(expected))
	}
}

func TestShadowRequestDependencyWakesOnlyOnChangedResult(t *testing.T) {
	_, s, b, j, _ := consumerFixture(t)
	v := registeredToolShadow(t, b, j.ID)
	id := uuid.NewString()
	s.mu.Lock()
	if s.record.Workbench == nil {
		s.record.Workbench = newWorkbench()
	}
	s.record.Workbench.Requests[id] = SpaceRequest{ID: id, State: "received", UpdatedAt: time.Now()}
	err := s.saveLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	d := shadow.TaskDependency{ID: "request", Kind: "request", Condition: "The actual request result is available", Source: SpaceSourceRef{SpaceID: j.ID, RequestID: id}}
	update, err := b.readShadowDependency(context.Background(), v.ID, d)
	if err != nil || len(update.Signals) != 0 {
		t.Fatal(update, err)
	}
	s.mu.Lock()
	r := s.record.Workbench.Requests[id]
	r.State, r.Summary, r.UpdatedAt = "completed", "Verified result", time.Now()
	s.record.Workbench.Requests[id] = r
	err = s.saveLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	update, err = b.readShadowDependency(context.Background(), v.ID, d)
	if err != nil || len(update.Signals) != 1 || update.Signals[0].State != "completed" || update.Signals[0].Source.SpaceID != j.ID {
		t.Fatal(update, err)
	}
	d.Cursor = update.Cursor
	update, err = b.readShadowDependency(context.Background(), v.ID, d)
	if err != nil || len(update.Signals) != 0 {
		t.Fatal(update, err)
	}
}
