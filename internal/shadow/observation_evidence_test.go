package shadow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"teamcross/internal/spacecontext"
)

func observationJSON(t *testing.T, run Run, value Observation) string {
	t.Helper()
	if run.ObservationEvidence == nil {
		return jsonText(value)
	}
	out := observationOutput{Summary: value.Summary, Outcome: value.Outcome, NextStep: value.NextStep, EvidenceIDs: []string{}, OpenQuestions: value.OpenQuestions}
	for _, ref := range value.Evidence {
		id := ""
		for candidate, source := range run.ObservationEvidence.Sources {
			if attentionHash(ref) == attentionHash(source) {
				id = candidate
			}
		}
		if id == "" {
			t.Fatal("fixture selected evidence absent from native input", ref)
		}
		out.EvidenceIDs = append(out.EvidenceIDs, id)
	}
	return jsonText(out)
}

func TestObservationEvidenceIDsPreserveExactSourceShapes(t *testing.T) {
	refs := []spacecontext.SourceRef{
		{SpaceID: "space", AnnotationID: "discussion", ReplyID: "reply"},
		{SpaceID: "space", ExecutionID: "execution", AnnotationID: "discussion", ReplyID: "reply"},
		{SpaceID: "space", MaterialID: "material", Version: 2},
		{SpaceID: "space", BriefRevision: 3, BriefItemID: "decision"},
		{SpaceID: "space", ShadowID: "shadow", ShadowResultID: "result-v1"},
	}
	for _, ref := range refs {
		batch := ObservationBatch{Focus: Focus{Evidence: []spacecontext.SourceRef{ref}}, Sources: []SourceSnapshot{{Source: ref, Data: json.RawMessage(`{"requestId":"business-command","turnId":"body-turn","itemIds":["body-item"]}`)}}}
		run := Run{ObservationEvidence: newObservationEvidence(batch)}
		if len(run.ObservationEvidence.Sources) != 1 {
			t.Fatal("duplicate prior/current source got two choices")
		}
		run.Output = `{"summary":"The update is understood.","evidenceIds":["E1"],"openQuestions":[],"outcome":"no_action","nextStep":""}`
		decoded, err := decodeObservation(run)
		if err != nil || !reflect.DeepEqual(decoded.Evidence, []spacecontext.SourceRef{ref}) {
			t.Fatal("exact source was lost or enriched with body fields", decoded, err)
		}
		schema := jsonText(observationSchema(run.ObservationEvidence))
		if strings.Contains(schema, "requestId") || !strings.Contains(schema, `"enum":["E1"]`) {
			t.Fatal("native schema still asks the model to construct references", schema)
		}
		// The native submission itself carries the mapping, independent of the
		// current store state or a later source catalog's numbering.
		var restored Run
		if err := json.Unmarshal([]byte(jsonText(run)), &restored); err != nil {
			t.Fatal(err)
		}
		again, err := decodeObservation(restored)
		if err != nil || !reflect.DeepEqual(decoded, again) {
			t.Fatal("mapping changed across durable recovery", again, err)
		}
	}
}

func TestObservationEvidenceRejectsUnprovidedIDsAndRawReferences(t *testing.T) {
	run := Run{ObservationEvidence: &ObservationEvidence{Sources: map[string]spacecontext.SourceRef{"E1": {SpaceID: "space", MaterialID: "material", Version: 1}}}}
	for _, output := range []string{
		`{"summary":"ok","evidenceIds":["E2"],"outcome":"no_action"}`,
		`{"summary":"ok","evidenceIds":[{"spaceId":"space"}],"outcome":"no_action"}`,
		`{"summary":"ok","evidenceIds":[],"evidence":[{"spaceId":"other-space"}],"outcome":"no_action"}`,
		`{"summary":"ok","outcome":"no_action"}`,
		`{"summary":"ok","evidenceIds":null,"outcome":"no_action"}`,
		`{"summary":"ok","evidenceIds":[],"outcome":"no_action"} {}`,
	} {
		run.Output = output
		if _, err := decodeObservation(run); !errors.Is(err, errInvalidObservation) {
			t.Fatal("unprovided reference was accepted", output, err)
		}
	}
}

func TestObservationCatalogExcludesWithdrawnMaterialAndKeepsProvidedSubreferences(t *testing.T) {
	material := spacecontext.SourceRef{SpaceID: "space", MaterialID: "material", Version: 1}
	item := material
	item.TurnID, item.ItemIDs = "turn", []string{"item"}
	batch := ObservationBatch{Focus: Focus{Evidence: []spacecontext.SourceRef{item}}, Sources: []SourceSnapshot{{Source: material, Unavailable: "withdrawn", Data: json.RawMessage(`{}`)}}}
	catalog := newObservationEvidence(batch)
	if len(catalog.Sources) != 0 || !strings.Contains(jsonText(observationSchema(catalog)), `"maxItems":0`) {
		t.Fatal("withdrawn material remained selectable", catalog)
	}
	batch.Sources[0].Unavailable = ""
	batch.Sources[0].References = []spacecontext.SourceRef{item}
	catalog = newObservationEvidence(batch)
	if len(catalog.Sources) != 2 {
		t.Fatal("provided fixed-version and item references were lost", catalog)
	}
	// Commit still fences a different space, even with a corrupted input map.
	store, receipt, signal := attentionFixture(t)
	signal.Reasons = []string{"follow_discussion"}
	acceptAttentionSignals(t, store, signal)
	claimed, err := store.ClaimBatch(receipt.Route.ShadowID, time.Now(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	claimed = prepareObservation(t, store, claimed, 1)
	foreign := signal.Source
	foreign.SpaceID = "other-space"
	if err := store.FinishBatch(claimed.ID, Observation{Summary: "wrong space", Evidence: []spacecontext.SourceRef{foreign}, Outcome: "no_action"}); !errors.Is(err, errObservationSource) {
		t.Fatal("source scope validation weakened", err)
	}
	if store.Snapshot().Events[signal.ID].State != "observing" {
		t.Fatal("invalid evidence advanced processing")
	}
}

func TestLegacyObservationRetryIsExplicitBoundedAndDoesNotReplayDirectWork(t *testing.T) {
	e, native, _, store, signal := directAttention(t)
	signal.Reasons = []string{"follow_discussion"}
	acceptAttentionSignals(t, store, signal)
	e.Start()
	first := active(t, e, "attention")
	// Reproduce the persisted old protocol, including the original failed output.
	e.mu.Lock()
	err := e.mutateLocked(func() error {
		run := e.record.Runs[first.ID]
		run.ObservationEvidence = nil
		e.record.Runs[run.ID] = run
		return nil
	})
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	bad := signal.Source
	bad.RequestID = "copied-business-request"
	native.complete(first.ThreadID, jsonText(Observation{Summary: "body understood", Evidence: []spacecontext.SourceRef{bad}, Outcome: "no_action"}), "completed", true)
	until(t, func() bool { return attentionCommitted(e, first.ID) })
	if store.Snapshot().Focuses[attentionHash(signal.Subject)].Error != errObservationSource.Error() {
		t.Fatal("legacy failure not reproduced")
	}
	if next, err := store.ClaimBatch(e.View().ID, time.Now().Add(time.Hour), 0, 0); err != nil || next != nil {
		t.Fatal("failure retried without explicit reconcile")
	}
	// A successful newer direct input on this object must remain processed.
	direct := clone(signal)
	direct.ID, direct.Seq, direct.Source.ReplyID, direct.Reasons = "direct-later", 2, "direct-reply", []string{"mention"}
	acceptAttentionSignals(t, store, direct)
	e.notify()
	main := active(t, e, "continuation")
	tool(t, e, native, main, "reply_to_annotation", map[string]any{"annotationId": direct.Source.AnnotationID, "requestId": "11111111-1111-4111-8111-111111111111", "text": "Already answered the direct question."})
	native.complete(main.ThreadID, "Already answered the direct question.", "completed", true)
	until(t, func() bool { return attentionCommitted(e, main.ID) })
	if err := e.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	second := active(t, e, "attention")
	if second.EventID == first.EventID || second.ObservationEvidence == nil {
		t.Fatal("replayed the obsolete protocol")
	}
	if store.Snapshot().Events[direct.ID].State != "processed" {
		t.Fatal("direct reply was queued again")
	}
	native.complete(second.ThreadID, observationJSON(t, second, Observation{Summary: "fresh source saved", Evidence: []spacecontext.SourceRef{signal.Source}, Outcome: "no_action"}), "completed", true)
	until(t, func() bool { return attentionCommitted(e, second.ID) })
	before := store.Snapshot()
	if before.Batches[first.EventID].State != "superseded" || before.Batches[first.EventID].Error != errObservationSource.Error() || before.Focuses[attentionHash(signal.Subject)].Error != "" {
		t.Fatal("recovery lost error audit or failed to save new understanding")
	}
	if err := e.retryLegacyObservationEvidence(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, store.Snapshot()) {
		t.Fatal("legacy recovery was not idempotent")
	}
	e.Close()
	reopened, err := OpenAttention(store.directory, "space-test", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, reopened.Snapshot()) {
		t.Fatal("recovered record did not survive restart")
	}
}

func TestLegacyObservationRetryPersistenceFailureKeepsFailure(t *testing.T) {
	store, receipt, signal := attentionFixture(t)
	signal.Reasons = []string{"follow_discussion"}
	acceptAttentionSignals(t, store, signal)
	batch, err := store.ClaimBatch(receipt.Route.ShadowID, time.Now(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	batch = prepareObservation(t, store, batch, 1)
	if err := store.FailBatch(batch.ID, errObservationSource.Error(), time.Time{}); err != nil {
		t.Fatal(err)
	}
	before := store.Snapshot()
	path := filepath.Join(store.directory, "attention.json")
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := store.retryLegacyObservationEvidence(receipt.Route.ShadowID, map[string]bool{batch.ID: true}); err == nil {
		t.Fatal("expected failed save")
	}
	if !reflect.DeepEqual(before, store.Snapshot()) {
		t.Fatal("failed save cleared the error")
	}
}

func TestLegacyObservationRetrySkipsUnrelatedOrIneligibleFailures(t *testing.T) {
	for _, scenario := range []string{"new_protocol", "other_error", "direct", "wrong_shadow", "disabled_route", "paused", "running"} {
		t.Run(scenario, func(t *testing.T) {
			store, receipt, signal := attentionFixture(t)
			signal.Reasons = []string{"follow_discussion"}
			acceptAttentionSignals(t, store, signal)
			batch, err := store.ClaimBatch(receipt.Route.ShadowID, time.Now(), 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			batch = prepareObservation(t, store, batch, 1)
			if err := store.FailBatch(batch.ID, errObservationSource.Error(), time.Time{}); err != nil {
				t.Fatal(err)
			}
			legacy := map[string]bool{batch.ID: true}
			store.mu.Lock()
			r := clone(store.record)
			focus, failed := r.Focuses[batch.Focus.ID], r.Batches[batch.ID]
			switch scenario {
			case "new_protocol":
				delete(legacy, batch.ID)
			case "other_error":
				failed.Error, focus.Error = "source unavailable", "source unavailable"
			case "direct":
				failed.Mode = "direct"
			case "wrong_shadow":
				failed.ShadowID = "another-shadow"
			case "disabled_route":
				r.Route.Enabled = false
			case "paused":
				focus.State = "paused"
			case "running":
				current := clone(failed)
				current.ID, current.State = "current", "running"
				r.Batches[current.ID] = current
			}
			r.Focuses[focus.ID], r.Batches[batch.ID] = focus, failed
			err = store.saveLocked(r)
			store.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			before := store.Snapshot()
			if err := store.retryLegacyObservationEvidence(receipt.Route.ShadowID, legacy); err != nil || !reflect.DeepEqual(before, store.Snapshot()) {
				t.Fatal("unrelated or ineligible failure was retried", err)
			}
		})
	}
}
