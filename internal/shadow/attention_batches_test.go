package shadow

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"teamcross/internal/spacecontext"
)

func acceptAttentionSignals(t *testing.T, s *AttentionStore, signals ...spacecontext.Signal) {
	t.Helper()
	r := AttentionReceipt{Token: fmt.Sprintf("page-%d-%s", len(s.Snapshot().Events), signals[0].ID), Route: s.Snapshot().Route}
	if err := s.Accept(r, signals); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmReceipt(r); err != nil {
		t.Fatal(err)
	}
}

func prepareObservation(t *testing.T, s *AttentionStore, batch *ObservationBatch, count int) *ObservationBatch {
	t.Helper()
	signals, err := s.BatchSignals(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	sources := []SourceSnapshot{}
	for _, signal := range signals[:count] {
		sources = append(sources, SourceSnapshot{Source: signal.Source, Data: json.RawMessage(`{"text":"complete source body"}`), ObservedAt: time.Now()})
	}
	prepared, err := s.PrepareBatch(batch.ID, sources)
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func TestAttentionBatchCoalescesOneObjectAndPreservesConcurrentSuffix(t *testing.T) {
	s, receipt, base := attentionFixture(t)
	base.Reasons = []string{"follow_discussion"}
	signals := []spacecontext.Signal{}
	for i := range 25 {
		signal := clone(base)
		signal.ID, signal.Seq, signal.Source.ReplyID = fmt.Sprintf("e-%02d", i), uint64(i+1), fmt.Sprintf("r-%d", i)
		signals = append(signals, signal)
	}
	acceptAttentionSignals(t, s, signals...)
	if batch, err := s.ClaimBatch(receipt.Route.ShadowID, time.Now(), time.Second, 10*time.Second); err != nil || batch != nil {
		t.Fatal("ordinary updates bypassed the merge window", batch, err)
	}
	batch, err := s.ClaimBatch(receipt.Route.ShadowID, time.Now().Add(2*time.Second), time.Second, 10*time.Second)
	if err != nil || batch == nil || len(batch.EventIDs) != 20 {
		t.Fatal(batch, err)
	}
	batch = prepareObservation(t, s, batch, 7)
	if len(batch.EventIDs) != 7 {
		t.Fatal("body budget did not trim the batch prefix")
	}
	newer := clone(base)
	newer.ID, newer.Seq, newer.Source.ReplyID = "arrived-during-analysis", 26, "r-26"
	acceptAttentionSignals(t, s, newer)
	observation := Observation{Summary: "The first seven replies establish the API behavior.", Evidence: []spacecontext.SourceRef{signals[6].Source}, Outcome: "no_action"}
	if err := s.FinishBatch(batch.ID, observation); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishBatch(batch.ID, observation); err != nil {
		t.Fatal("completion replay failed", err)
	}
	r := s.Snapshot()
	focus := r.Focuses[batch.Focus.ID]
	if focus.ProcessedSeq != 7 || focus.Processing != "queued" || len(r.Continuations) != 0 {
		t.Fatal(focus, r.Continuations)
	}
	processed, queued := 0, 0
	for _, event := range r.Events {
		if event.State == "processed" {
			processed++
		}
		if event.State == "queued" {
			queued++
		}
	}
	if processed != 7 || queued != 19 {
		t.Fatalf("processed=%d queued=%d", processed, queued)
	}
	continued, err := s.ClaimBatch(receipt.Route.ShadowID, time.Now().Add(20*time.Second), time.Second, 10*time.Second)
	if err != nil || continued == nil || continued.EventIDs[0] != "e-07" {
		t.Fatal("skipped unprocessed prefix", continued, err)
	}
}

func TestAttentionBatchUrgencyRouteFenceAndDurableContinuation(t *testing.T) {
	s, receipt, base := attentionFixture(t)
	base.Reasons = []string{"mention", "follow_discussion"}
	ordinary := clone(base)
	ordinary.ID, ordinary.Subject.ID, ordinary.Reasons = "ordinary", "other-topic", []string{"follow_discussion"}
	ordinary.Source.AnnotationID = ordinary.Subject.ID
	acceptAttentionSignals(t, s, ordinary, base)
	batch, err := s.ClaimBatch(receipt.Route.ShadowID, time.Now(), time.Hour, 2*time.Hour)
	if err != nil || batch == nil || batch.EventIDs[0] != base.ID {
		t.Fatal("direct question was not prioritized", batch, err)
	}
	// A route change before native intent returns this exact batch to the queue.
	newRoute := receipt.Route
	newRoute.ShadowID, newRoute.Revision = "new-shadow", 2
	if err := s.SetRoute(newRoute); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PrepareBatch(batch.ID, []SourceSnapshot{{Source: base.Source, Data: json.RawMessage(`{}`)}}); err == nil {
		t.Fatal("stale route prepared model input")
	}
	batch, err = s.ClaimBatch(newRoute.ShadowID, time.Now(), time.Hour, 2*time.Hour)
	if err != nil || batch == nil {
		t.Fatal(err)
	}
	batch = prepareObservation(t, s, batch, 1)
	if _, err := s.ClaimContinuation(newRoute.ShadowID); err != nil {
		t.Fatal(err)
	}
	// Already-started work keeps its original owner; another shadow cannot
	// start the same object while that batch is running.
	third := newRoute
	third.ShadowID, third.Revision = "third-shadow", 3
	if err := s.SetRoute(third); err != nil {
		t.Fatal(err)
	}
	if other, err := s.ClaimBatch(third.ShadowID, time.Now(), time.Hour, 2*time.Hour); err != nil || other != nil {
		t.Fatal("new route raced active work", other, err)
	}
	decision := Observation{Summary: "The compatibility update is understood and requires no action.", Outcome: "no_action"}
	if err := s.FinishContinuation(batch.ID, newRoute.ShadowID, "completed", jsonText(decision), nil, ContinuationWork{}); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishContinuation(batch.ID, newRoute.ShadowID, "completed", jsonText(decision), nil, ContinuationWork{}); err != nil {
		t.Fatal(err)
	}
	restored, err := OpenAttention(s.directory, "space", "owner")
	if err != nil {
		t.Fatal(err)
	}
	r := restored.Snapshot()
	if len(r.Continuations) != 1 || r.Continuations[batch.ID].ShadowID != newRoute.ShadowID || r.Events[base.ID].State != "processed" {
		t.Fatal(r.Continuations)
	}
}

func TestAttentionBatchOldGoalCannotCompleteNewGoalAndFailureDoesNotSkip(t *testing.T) {
	s, receipt, base := attentionFixture(t)
	acceptAttentionSignals(t, s, base)
	batch, err := s.ClaimBatch(receipt.Route.ShadowID, time.Now(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	batch = prepareObservation(t, s, batch, 1)
	updated, err := s.UpdateFocus(batch.Focus.ID, batch.Focus.Revision, "Now investigate the new deadline", "active")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishBatch(batch.ID, Observation{Summary: "Old goal result", Outcome: "continue", NextStep: "An outdated next step"}); err != nil {
		t.Fatal(err)
	}
	r := s.Snapshot()
	if r.Focuses[batch.Focus.ID].GoalVersion != updated.GoalVersion || r.Focuses[batch.Focus.ID].ProcessedSeq != 0 || len(r.Continuations) != 0 || r.Events[base.ID].State != "queued" {
		t.Fatal("old result advanced new work", r)
	}
	retry, err := s.ClaimBatch(receipt.Route.ShadowID, time.Now(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FailBatch(retry.ID, "source temporarily unavailable", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ClaimBatch(receipt.Route.ShadowID, time.Now(), 0, 0); err != nil || got != nil {
		t.Fatal("failed source entered a busy loop", got, err)
	}
	if r := s.Snapshot(); r.Focuses[batch.Focus.ID].ProcessedSeq != 0 || r.Events[base.ID].State != "queued" {
		t.Fatal("failure skipped input", r)
	}
}
