package shadow

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"teamcross/internal/spacecontext"
)

func TestWaitingActualQuestionsMergeAcrossTasksAndPreservePartialAnswers(t *testing.T) {
	e, _, _ := setup(t)
	s, _ := attachEngineAttention(t, e)
	ref := spacecontext.SourceRef{SpaceID: e.record.SpaceID, AnnotationID: "discussion", ReplyID: "question"}
	q := TaskQuestion{ID: "q", Source: ref, Text: "Which version?", Recipients: []string{e.record.Owner.ID}, Delivery: "saved", UpdatedAt: time.Now()}
	e.record.Questions[q.ID] = q
	for i := range 2 {
		id := fmt.Sprint(i)
		d := TaskDependency{ID: "d" + id, Kind: "discussion", QuestionID: q.ID, Source: ref, State: "waiting", Condition: "condition " + id}
		if i == 0 {
			d.Signals = []DependencySignal{{ID: "partial", Source: ref, Preview: "Partial answer", State: "answered"}}
		}
		e.record.Jobs[id] = Job{ID: id, Title: "task " + id, State: "waiting", Dependencies: []TaskDependency{d}, UpdatedAt: time.Now()}
	}
	page, err := s.WaitingPage(e.WaitingItems(), "", "", 30)
	if err != nil || len(page.Items) != 1 || page.Items[0].Direction != "needs_owner" || page.Items[0].TotalTasks != 2 || page.Items[0].State != "waiting" {
		t.Fatal(page, err)
	}
	// Received remains unresolved, but does not ask the owner to answer again.
	j := e.record.Jobs["1"]
	j.Dependencies[0].Signals = []DependencySignal{{ID: "received", Source: ref, State: "answered"}}
	e.record.Jobs[j.ID] = j
	page, err = s.WaitingPage(e.WaitingItems(), "", "", 30)
	if err != nil || page.Items[0].Direction != "processing" || page.Items[0].State != "answer_received" || page.Items[0].TotalTasks != 2 {
		t.Fatal(page, err)
	}
	for id, j := range e.record.Jobs {
		j.Dependencies[0].State = "satisfied"
		e.record.Jobs[id] = j
	}
	page, err = s.WaitingPage(e.WaitingItems(), "", "", 30)
	if err != nil || page.Total != 0 {
		t.Fatal("resolved conditions remained in waiting index", page, err)
	}
}

func TestWaitingUnsentQuestionDoesNotPretendParentNeedsOwnerAndCursorIsStable(t *testing.T) {
	e, _, _ := setup(t)
	s, _ := attachEngineAttention(t, e)
	for i := range 3 {
		id := fmt.Sprint(i)
		ref := spacecontext.SourceRef{SpaceID: e.record.SpaceID, AnnotationID: "parent", ReplyID: "unrelated-parent"}
		q := TaskQuestion{ID: "q" + id, ActionID: "action" + id, Source: ref, Text: "Question " + id, Recipients: []string{e.record.Owner.ID}, Delivery: "unknown", Error: "Receipt not received"}
		e.record.Questions[q.ID] = q
		e.record.Jobs[id] = Job{ID: id, State: "paused", Dependencies: []TaskDependency{{ID: "d", Kind: "discussion", QuestionID: q.ID, Source: ref, QuestionSourcePending: true, State: "waiting"}}}
	}
	page, err := s.WaitingPage(e.WaitingItems(), "processing", "", 1)
	if err != nil || page.Total != 3 || page.NextCursor == "" || page.Items[0].Source.AnnotationID != "" || page.Items[0].Tasks[0].State != "paused" {
		t.Fatal(page, err)
	}
	for range 10 {
		again, err := s.WaitingPage(e.WaitingItems(), "processing", "", 1)
		if err != nil || again.Revision != page.Revision {
			t.Fatal("map order invalidated pagination", again, err)
		}
	}
	next, err := s.WaitingPage(e.WaitingItems(), "processing", page.NextCursor, 1)
	if err != nil || len(next.Items) != 1 || next.Items[0].ID == page.Items[0].ID {
		t.Fatal(next, err)
	}
	q := e.record.Questions["q0"]
	q.Error = "Changed"
	e.record.Questions[q.ID] = q
	if _, err := s.WaitingPage(e.WaitingItems(), "processing", page.NextCursor, 1); err == nil {
		t.Fatal("stale page accepted")
	}
	owner, err := s.WaitingPage(e.WaitingItems(), "needs_owner", "", 30)
	if err != nil || owner.Total != 0 {
		t.Fatal("unsent questions demanded owner replies", owner, err)
	}
	b, _ := json.Marshal(next)
	if strings.Contains(string(b), "questionActionId") || strings.Contains(string(b), "fingerprint") || strings.Contains(string(b), "nativeThread") {
		t.Fatal("transport details in waiting projection", string(b))
	}
}

func TestWaitingOwnershipSurvivesDefaultChangeWithoutNewTask(t *testing.T) {
	s, _, signal := attentionFixture(t)
	h := controlHandling(t, s, signal.Source, "owner", 0)
	if err := s.SetRoute(AttentionRoute{ShadowID: "replacement", Revision: 2, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	page, err := s.WaitingPage(nil, "needs_owner", "", 30)
	if err != nil || page.Total != 1 || page.Items[0].TotalTasks != 0 || page.Items[0].Handling.Revision != 1 {
		t.Fatal(page, err)
	}
	answer := DependencySignal{ID: "owner-answer", Source: spacecontext.SourceRef{SpaceID: "space", AnnotationID: "discussion", ReplyID: "answer"}, State: "answered", Actor: spacecontext.Actor{Kind: "human", MemberID: "owner"}}
	if err := s.applyHandlingUpdate(h, DependencyUpdate{Cursor: "reply:4", Signals: []DependencySignal{answer}}, nil); err != nil {
		t.Fatal(err)
	}
	page, err = s.WaitingPage(nil, "needs_owner", "", 30)
	if err != nil || page.Total != 0 {
		t.Fatal("own answer still appears as needing the owner", page, err)
	}
	if len(s.Snapshot().Continuations) != 0 {
		t.Fatal("opening waiting view started work")
	}
}
