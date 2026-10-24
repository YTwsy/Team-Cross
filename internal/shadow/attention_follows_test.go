package shadow

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"teamcross/internal/spacecontext"
)

func TestAttentionFollowControlInvalidatesQueuedWorkAndSurvivesRestart(t *testing.T) {
	s, receipt, event := attentionFixture(t)
	event.Source.MaterialID, event.Source.Version = "material", 1
	accept := func(signals ...spacecontext.Signal) {
		t.Helper()
		receipt.Token = fmt.Sprint("receipt-", len(s.Snapshot().Events))
		if err := s.Accept(receipt, signals); err != nil {
			t.Fatal(err)
		}
		if err := s.ConfirmReceipt(receipt); err != nil {
			t.Fatal(err)
		}
	}
	accept(event)
	batch, err := s.ClaimBatch(receipt.Route.ShadowID, time.Now(), 0, 0)
	if err != nil || batch == nil {
		t.Fatal(err)
	}
	control := spacecontext.Signal{ID: "parent-muted", Seq: 2, Kind: "follow_changed", Source: spacecontext.SourceRef{SpaceID: "space", MaterialID: "material", Version: 1}, Subject: spacecontext.Subject{Kind: "material", ID: "material"}, Follow: &spacecontext.FollowChange{Mode: "muted", Revision: 1}}
	accept(control)
	if s.Snapshot().Batches[batch.ID].State != "superseded" || s.HasPending(receipt.Route.ShadowID, time.Now()) {
		t.Fatal("a later parent mute did not fence pending ordinary analysis")
	}
	before := s.Snapshot()
	reopened, err := OpenAttention(s.directory, "space", "owner")
	if err != nil || !reflect.DeepEqual(before, reopened.Snapshot()) {
		t.Fatal("saved follow controls did not survive restart", err)
	}
	s = reopened
	control.ID, control.Seq = "parent-unmuted", 3
	control.Follow = &spacecontext.FollowChange{Mode: "following", Revision: 2, Baseline: []spacecontext.SourceRef{control.Source}}
	accept(control)
	if !s.HasPending(receipt.Route.ShadowID, time.Now()) {
		t.Fatal("unmute discarded already received work")
	}
	if f := s.Snapshot().Focuses[attentionHash(event.Subject)]; f.ProcessedSeq != 0 || f.Summary != "" {
		t.Fatal("preference control falsely claimed understanding", f)
	}
}

func TestAttentionFollowDirectOnlyAndStaleInitialization(t *testing.T) {
	s, receipt, event := attentionFixture(t)
	change := event
	change.ID, change.Seq, change.Kind, change.Reasons = "follow-start", 2, "follow_changed", []string{"follow_preference"}
	change.Follow = &spacecontext.FollowChange{Mode: "following", Revision: 1, Baseline: []spacecontext.SourceRef{event.Source}}
	stop := change
	stop.ID, stop.Seq, stop.Follow = "follow-stop", 3, &spacecontext.FollowChange{Mode: "direct_only", Revision: 2}
	direct := event
	direct.ID, direct.Seq, direct.Reasons = "direct-question", 4, []string{"mention"}
	if err := s.Accept(receipt, []spacecontext.Signal{event, change, stop, direct}); err != nil {
		t.Fatal(err)
	}
	batch, err := s.ClaimBatch(receipt.Route.ShadowID, time.Now(), 0, 0)
	if err != nil || batch == nil || len(batch.EventIDs) != 1 || batch.EventIDs[0] != direct.ID {
		t.Fatal("direct_only lost the question or analyzed obsolete follow history", batch, err)
	}
	wrong := stop
	wrong.ID, wrong.Follow = "same-revision-different-mode", &spacecontext.FollowChange{Mode: "muted", Revision: 2}
	before := s.Snapshot()
	if err := s.Accept(receipt, []spacecontext.Signal{wrong}); err == nil || !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("conflicting follow revision was not rejected atomically")
	}
}

func TestAttentionFollowBaselineEvidenceIsTheActualBoundedRead(t *testing.T) {
	s, receipt, event := attentionFixture(t)
	event.ID, event.Kind, event.Source.ReplyID = "follow", "follow_changed", ""
	reply := event.Source
	reply.ReplyID = "recent-reply"
	event.Follow = &spacecontext.FollowChange{Mode: "following", Revision: 1, Baseline: []spacecontext.SourceRef{event.Source, reply}}
	if err := s.Accept(receipt, []spacecontext.Signal{event}); err != nil {
		t.Fatal(err)
	}
	batch, err := s.ClaimBatch(receipt.Route.ShadowID, time.Now(), 0, 0)
	if err != nil || batch == nil {
		t.Fatal(err)
	}
	body, _ := observationReader(context.Background(), event.Source)
	body.References, body.Partial = []spacecontext.SourceRef{reply}, true
	if _, err := s.PrepareBatch(batch.ID, []SourceSnapshot{body}); err != nil {
		t.Fatal(err)
	}
	observation := Observation{Summary: "The recent answer establishes one fact; earlier replies remain unread.", Evidence: []spacecontext.SourceRef{reply}, Outcome: "no_action"}
	if err := s.FinishBatch(batch.ID, observation); err != nil {
		t.Fatal("actual baseline evidence rejected", err)
	}
	if got := s.Snapshot().Focuses[attentionHash(event.Subject)]; got.ProcessedSeq != event.Seq || len(got.Evidence) != 1 || got.Evidence[0].ReplyID != reply.ReplyID {
		t.Fatal(got)
	}
}
