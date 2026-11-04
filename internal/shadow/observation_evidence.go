package shadow

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"teamcross/internal/spacecontext"
)

// Save the exact choices with the native submission, so completion can be
// decoded after restart or after FinishBatch discards the source bodies.
// A nil catalog identifies a turn submitted with the earlier SourceRef schema.
type ObservationEvidence struct {
	Sources map[string]spacecontext.SourceRef `json:"sources"`
}

type observationOutput struct {
	Summary       string   `json:"summary"`
	EvidenceIDs   []string `json:"evidenceIds"`
	OpenQuestions []string `json:"openQuestions"`
	Outcome       string   `json:"outcome"`
	NextStep      string   `json:"nextStep"`
}

// Shared by the input catalog and commit validation. An unavailable material
// invalidates earlier evidence for that material, including more specific refs.
func observationSourceSet(batch ObservationBatch) map[string]spacecontext.SourceRef {
	known := map[string]spacecontext.SourceRef{}
	withdrawn := map[string]bool{}
	for _, source := range batch.Sources {
		if source.Unavailable != "" && source.Source.MaterialID != "" {
			withdrawn[source.Source.MaterialID] = true
		}
	}
	for _, ref := range batch.Focus.Evidence {
		known[attentionHash(ref)] = ref
	}
	for _, source := range batch.Sources {
		for _, ref := range append([]spacecontext.SourceRef{source.Source}, source.References...) {
			key := attentionHash(ref)
			if source.Unavailable == "" {
				known[key] = ref
			} else {
				delete(known, key)
			}
		}
	}
	for key, ref := range known {
		if withdrawn[ref.MaterialID] {
			delete(known, key)
		}
	}
	return known
}

func newObservationEvidence(batch ObservationBatch) *ObservationEvidence {
	refs := observationSourceSet(batch)
	keys := make([]string, 0, len(refs))
	for key := range refs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	catalog := &ObservationEvidence{Sources: map[string]spacecontext.SourceRef{}}
	for i, key := range keys {
		catalog.Sources[fmt.Sprintf("E%d", i+1)] = clone(refs[key])
	}
	return catalog
}

func decodeObservation(run Run) (Observation, error) {
	var observation Observation
	if run.ObservationEvidence == nil {
		// Already-submitted turns retain exact commit-time source validation.
		if err := json.Unmarshal([]byte(run.Output), &observation); err != nil {
			return observation, fmt.Errorf("%w: 不是有效 JSON", errInvalidObservation)
		}
		return observation, nil
	}
	var output observationOutput
	d := json.NewDecoder(strings.NewReader(run.Output))
	d.DisallowUnknownFields()
	if err := d.Decode(&output); err != nil {
		return observation, fmt.Errorf("%w: 不是有效的编号引用结构", errInvalidObservation)
	}
	if d.Decode(new(any)) != io.EOF || output.EvidenceIDs == nil || len(output.EvidenceIDs) > 32 {
		return observation, fmt.Errorf("%w: 证据编号缺失或超出预算", errInvalidObservation)
	}
	observation = Observation{Summary: output.Summary, Evidence: []spacecontext.SourceRef{}, OpenQuestions: output.OpenQuestions, Outcome: output.Outcome, NextStep: output.NextStep}
	seen := map[string]bool{}
	for _, id := range output.EvidenceIDs {
		ref, exists := run.ObservationEvidence.Sources[id]
		if !exists {
			return Observation{}, fmt.Errorf("%w: 证据编号未在本轮提供", errInvalidObservation)
		}
		if !seen[id] {
			observation.Evidence = append(observation.Evidence, clone(ref))
			seen[id] = true
		}
	}
	return observation, nil
}

// Explicit local reconciliation retries only old read-only observation failures
// fixed by this schema. Ordinary scheduling never clears errors automatically.
func (e *Engine) retryLegacyObservationEvidence() error {
	e.mu.Lock()
	store, shadowID := e.attention, e.record.ID
	legacy := map[string]bool{}
	for _, run := range e.record.Runs {
		if run.Kind == "attention" && run.ObservationEvidence == nil && run.State == "completed" && run.AttentionCommitted {
			legacy[run.EventID] = true
		}
	}
	e.mu.Unlock()
	if store == nil || len(legacy) == 0 {
		return nil
	}
	return store.retryLegacyObservationEvidence(shadowID, legacy)
}

func (s *AttentionStore) retryLegacyObservationEvidence(shadowID string, legacy map[string]bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.Route.ShadowID != shadowID || !s.record.Route.Enabled {
		return nil
	}
	active := map[string]bool{}
	for _, batch := range s.record.Batches {
		if batch.State == "preparing" || batch.State == "running" || batch.State == "continuing" {
			active[batch.Focus.ID] = true
		}
	}
	next := clone(s.record)
	changed := false
	for id, batch := range s.record.Batches {
		focus := s.record.Focuses[batch.Focus.ID]
		if !legacy[id] || batch.ShadowID != shadowID || batch.Mode != "observe" || batch.State != "failed" || batch.Error != errObservationSource.Error() || focus.Error != batch.Error || focus.State == "paused" || active[focus.ID] {
			continue
		}
		// Preserve failed native output and errors for audit. Events already
		// processed by a direct turn are never placed back in the queue.
		batch.State = "superseded"
		next.Batches[id] = batch
		focus.Error, focus.Processing = "", "queued"
		focus.RetryAfter, focus.UpdatedAt = time.Time{}, time.Now()
		next.Focuses[focus.ID] = focus
		changed = true
	}
	if !changed {
		return nil
	}
	return s.saveLocked(next)
}
