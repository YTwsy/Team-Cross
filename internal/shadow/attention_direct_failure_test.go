package shadow

import (
	"fmt"
	"testing"
	"time"
)

func TestAttentionNewDirectInputBypassesFailedObservationWithoutReplayingIt(t *testing.T) {
	s, receipt, base := attentionFixture(t)
	acceptAttentionSignals(t, s, base)
	failed, err := s.ClaimBatch(receipt.Route.ShadowID, time.Now(), 0, 0)
	if err != nil || failed == nil {
		t.Fatal(failed, err)
	}
	// Include an ordinary backlog larger than the batch budget, and a new
	// request received while the old observation is still running.
	for i := 2; i <= 25; i++ {
		signal := clone(base)
		signal.ID, signal.Seq, signal.Source.ReplyID = fmt.Sprintf("ordinary-%d", i), uint64(i), fmt.Sprintf("r-%d", i)
		acceptAttentionSignals(t, s, signal)
	}
	direct := clone(base)
	direct.ID, direct.Seq, direct.Source.ReplyID = "self-mention", 26, "self-reply"
	direct.Actor.MemberID, direct.Reasons = "owner", []string{"mention"}
	acceptAttentionSignals(t, s, direct)
	if err := s.FailBatch(failed.ID, "invalid ordinary evidence", time.Time{}); err != nil {
		t.Fatal(err)
	}
	s, err = OpenAttention(s.directory, "space", "owner")
	if err != nil || !s.HasPending(receipt.Route.ShadowID, time.Now()) {
		t.Fatal("restart or an old observation silenced the new request", err)
	}
	batch, err := s.ClaimBatch(receipt.Route.ShadowID, time.Now(), time.Hour, time.Hour)
	if err != nil || batch == nil || batch.Mode != "direct" || len(batch.EventIDs) != 1 || batch.EventIDs[0] != direct.ID {
		t.Fatal("old failed prefix displaced the explicit input", batch, err)
	}
	prepareObservation(t, s, batch, 1)
	packet, err := s.ClaimContinuation(receipt.Route.ShadowID)
	if err != nil || packet == nil || len(packet.MemberChanges) != 0 || len(packet.SourceBodies) != 1 || attentionHash(packet.Sources[0]) != attentionHash(direct.Source) {
		t.Fatal("self delegation became an already-completed change", packet, err)
	}
	second := clone(direct)
	second.ID, second.Seq, second.Source.ReplyID = "second-mention", 27, "second-reply"
	acceptAttentionSignals(t, s, second)
	secondBatch, err := s.ClaimBatch(receipt.Route.ShadowID, time.Now(), 0, 0)
	if err != nil || secondBatch == nil || len(secondBatch.EventIDs) != 1 || secondBatch.EventIDs[0] != second.ID {
		t.Fatal("a second direct input replayed the old observation", secondBatch, err)
	}
	prepareObservation(t, s, secondBatch, 1)
	if err := s.FinishContinuation(batch.ID, receipt.Route.ShadowID, "completed", "ordinary free text", []string{"public-reply"}, ContinuationWork{}); err != nil {
		t.Fatal(err)
	}
	if packet, err := s.ClaimContinuation(receipt.Route.ShadowID); err != nil || packet == nil || packet.Continuation.ID != secondBatch.ID {
		t.Fatal("old observation error blocked an already-prepared direct input", packet, err)
	}
	if err := s.FinishContinuation(secondBatch.ID, receipt.Route.ShadowID, "completed", "second reply", []string{"second-public-reply"}, ContinuationWork{}); err != nil {
		t.Fatal(err)
	}
	if s.HasPending(receipt.Route.ShadowID, time.Now().Add(time.Hour)) {
		t.Fatal("direct completion automatically replayed an unrelated failed observation")
	}
	r := s.Snapshot()
	if r.Events[direct.ID].State != "processed" || r.Events[base.ID].State != "queued" || r.Focuses[batch.Focus.ID].Error == "" {
		t.Fatal("direct processing pretended to repair the ordinary observation", r.Focuses)
	}
}

func TestAttentionFailedDirectSourceDoesNotBusyLoop(t *testing.T) {
	s, receipt, direct := attentionFixture(t)
	direct.Reasons = []string{"mention"}
	acceptAttentionSignals(t, s, direct)
	batch, err := s.ClaimBatch(receipt.Route.ShadowID, time.Now(), 0, 0)
	if err != nil || batch == nil {
		t.Fatal(batch, err)
	}
	if err := s.FailBatch(batch.ID, "source unavailable", time.Time{}); err != nil {
		t.Fatal(err)
	}
	acceptAttentionSignals(t, s, direct) // Repeated receipt is not a new request.
	if s.HasPending(receipt.Route.ShadowID, time.Now()) {
		t.Fatal("failed direct input automatically retried")
	}
	if got, err := s.ClaimBatch(receipt.Route.ShadowID, time.Now(), 0, 0); err != nil || got != nil {
		t.Fatal(got, err)
	}
}
