package collab

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestRequestKeepsBriefAndPredecessorSnapshot(t *testing.T) {
	a, _, s := spaceFixture(t)
	_, c, target := spaceChannel(t, a, s.record.ID)
	brief := SpaceBrief{Topic: "First topic", Decisions: []BriefItem{{Text: "Confirmed by member"}}, Questions: []BriefItem{}}
	wb(t, a, s.record.ID, "brief", workbenchInput{Brief: brief, BaseRevision: 0})
	in := workbenchInput{RequestID: uuid.NewString(), TargetID: target.ID, Instruction: "Review", Intent: "analyze", Actor: SpaceActor{Kind: "human"}}
	first := workbenchDecode[SpaceRequest](wb(t, a, s.record.ID, "send", in))
	if first.BriefRevision != 1 || first.Context.Brief.Topic != "First topic" {
		t.Fatal(first)
	}
	a.pumpWorkbench(context.Background())
	if _, err := a.readAgentRequest(context.Background(), c, first.ID, "", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := a.finishAgentRequest(context.Background(), c, first.ID, "", "completed", "Result"); err != nil {
		t.Fatal(err)
	}
	brief.Topic = "Changed topic"
	brief.Decisions = append(brief.Decisions, BriefItem{Text: "Member accepted result", RequestID: first.ID})
	wb(t, a, s.record.ID, "brief", workbenchInput{Brief: brief, BaseRevision: 1})
	duplicate := workbenchDecode[SpaceRequest](wb(t, a, s.record.ID, "send", in))
	if duplicate.Context.Brief.Topic != "First topic" || duplicate.BriefRevision != 1 {
		t.Fatal("retry refreshed context", duplicate)
	}
	follow := in
	follow.RequestID, follow.ParentRequestID = uuid.NewString(), first.ID
	second := workbenchDecode[SpaceRequest](wb(t, a, s.record.ID, "send", follow))
	if second.Context.Brief.Revision != 2 || *second.Context.PreviousBriefRevision != 1 || second.Context.Parent.Summary != "Result" {
		t.Fatal(second)
	}
	view := workbenchDecode[WorkbenchView](wb(t, a, s.record.ID, "view", workbenchInput{}))
	for _, request := range view.Requests {
		if request.Context != nil {
			t.Fatal("list repeated full snapshots")
		}
	}
	if _, err := a.workbenchCall(context.Background(), s.record.ID, "brief", workbenchInput{Brief: brief, BaseRevision: 1}); err == nil {
		t.Fatal("stale draft overwrote confirmed brief")
	}
	brief.Decisions[1].RequestID = second.ID
	if _, err := a.workbenchCall(context.Background(), s.record.ID, "brief", workbenchInput{Brief: brief, BaseRevision: 2}); err == nil {
		t.Fatal("unfinished request became a cited result")
	}
}

func TestOversizedRequestContextRejectedBeforeDelivery(t *testing.T) {
	a, _, s := spaceFixture(t)
	_, c, target := spaceChannel(t, a, s.record.ID)
	in := workbenchInput{RequestID: uuid.NewString(), TargetID: target.ID, Instruction: strings.Repeat("😀", 4000), Intent: "analyze"}
	first := workbenchDecode[SpaceRequest](wb(t, a, s.record.ID, "send", in))
	a.pumpWorkbench(context.Background())
	if _, err := a.readAgentRequest(context.Background(), c, first.ID, "", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := a.finishAgentRequest(context.Background(), c, first.ID, "", "completed", strings.Repeat("中", 2000)); err != nil {
		t.Fatal(err)
	}
	brief := SpaceBrief{Topic: strings.Repeat("中", 2000), Decisions: []BriefItem{{Text: strings.Repeat("中", 2000)}, {Text: strings.Repeat("中", 2000)}}}
	wb(t, a, s.record.ID, "brief", workbenchInput{Brief: brief})
	in.RequestID, in.ParentRequestID = uuid.NewString(), first.ID
	if _, err := a.workbenchCall(context.Background(), s.record.ID, "send", in); err == nil || !strings.Contains(err.Error(), "上下文过长") {
		t.Fatalf("oversized request: %v", err)
	}
	view := workbenchDecode[WorkbenchView](wb(t, a, s.record.ID, "view", workbenchInput{}))
	if view.Total != 1 {
		t.Fatal("unreadable request was persisted", view.Total)
	}
}
