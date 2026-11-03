package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"teamcross/internal/spacecontext"
)

func attachEngineAttention(t *testing.T, e *Engine) (*AttentionStore, spacecontext.Signal) {
	t.Helper()
	id, space, _, _ := e.Identity()
	s, err := OpenAttention(t.TempDir(), space, e.View().Owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetRoute(AttentionRoute{ShadowID: id, Revision: 1, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := e.AttachAttention(s); err != nil {
		t.Fatal(err)
	}
	signal := spacecontext.Signal{ID: "follow-1", Seq: 1, Kind: "reply", Subject: spacecontext.Subject{Kind: "discussion", ID: "discussion"}, Source: spacecontext.SourceRef{SpaceID: space, AnnotationID: "discussion", ReplyID: "question"}, Reasons: []string{"follow_discussion"}, Preview: "short preview", CreatedAt: time.Now()}
	return s, signal
}

func observationReader(_ context.Context, ref spacecontext.SourceRef) (SourceSnapshot, error) {
	return SourceSnapshot{Source: ref, Data: json.RawMessage(`{"text":"Exact body includes the compatibility question omitted from the preview."}`), ObservedAt: time.Now()}, nil
}

func attentionCommitted(e *Engine, runID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.record.Runs[runID].AttentionCommitted
}

func TestAttentionNativeObservationIsPrivateAndQueuesOnlyNeededContinuation(t *testing.T) {
	for _, outcome := range []string{"no_action", "continue", "needs_owner"} {
		t.Run(outcome, func(t *testing.T) {
			e, f, _ := setup(t)
			e.cfg.ReadSource = observationReader
			s, signal := attachEngineAttention(t, e)
			acceptAttentionSignals(t, s, signal)
			e.Start()
			run := active(t, e, "attention")
			if e.View().Current != "" || len(e.View().Messages) != 0 {
				t.Fatal("observer created a public conversation")
			}
			f.mu.Lock()
			prompt := itemText(f.threads[run.ThreadID][0].Items[0])
			f.mu.Unlock()
			if !strings.Contains(prompt, "Exact body includes") || strings.Contains(prompt, `"role":"user"`) {
				t.Fatal("exact source missing or synthetic human message", prompt)
			}
			observation := Observation{Summary: "The compatibility question is understood.", Evidence: []spacecontext.SourceRef{signal.Source}, Outcome: outcome}
			if outcome != "no_action" {
				observation.NextStep = "Find the compatibility limit and answer the original question."
			}
			f.complete(run.ThreadID, jsonText(observation), "completed", true)
			until(t, func() bool { return attentionCommitted(e, run.ID) })
			r := s.Snapshot()
			if r.Events[signal.ID].State != "processed" || r.Focuses[r.Events[signal.ID].FocusID].ProcessedSeq != 1 {
				t.Fatal("valid observation not committed", r)
			}
			want := 1
			if outcome == "no_action" {
				want = 0
			}
			if len(r.Continuations) != want || len(e.View().Messages) != 0 {
				t.Fatal("observation leaked or continuation was lost", r.Continuations, e.View())
			}
		})
	}
}

func TestAttentionInvalidNativeOutputKeepsInputAndDoesNotDisableChat(t *testing.T) {
	for _, output := range []string{`not json`, `{"summary":"ok","outcome":"continue"}`, `{"summary":"ok","outcome":"no_action","evidence":[{"spaceId":"space-test","annotationId":"invented"}]}`} {
		t.Run(fmt.Sprintf("case-%d", len(output)), func(t *testing.T) {
			e, f, _ := setup(t)
			e.cfg.ReadSource = observationReader
			s, signal := attachEngineAttention(t, e)
			acceptAttentionSignals(t, s, signal)
			e.Start()
			run := active(t, e, "attention")
			f.complete(run.ThreadID, output, "completed", true)
			until(t, func() bool { return attentionCommitted(e, run.ID) })
			r := s.Snapshot()
			focus := r.Focuses[r.Events[signal.ID].FocusID]
			if focus.Error == "" || focus.ProcessedSeq != 0 || !focus.RetryAfter.IsZero() || r.Events[signal.ID].State != "queued" || !e.View().Enabled {
				t.Fatal("invalid result lost input or stopped chat", focus, e.View())
			}
			if batch, err := s.ClaimBatch(e.View().ID, time.Now().Add(time.Hour), 0, 0); err != nil || batch != nil {
				t.Fatal("invalid output caused an automatic retry loop", batch, err)
			}
			send(t, e, "owner", "Continue our unrelated chat")
			main := active(t, e, "main")
			f.complete(main.ThreadID, "The chat still works.", "completed", true)
			until(t, func() bool { return len(e.View().Messages) == 2 })
		})
	}
}

func TestAttentionNativeRecoveryUsesSameThreadAndTurn(t *testing.T) {
	e, f, cfg := setup(t)
	e.cfg.ReadSource, cfg.ReadSource = observationReader, observationReader
	s, signal := attachEngineAttention(t, e)
	acceptAttentionSignals(t, s, signal)
	e.Start()
	run := active(t, e, "attention")
	e.Close()
	f.complete(run.ThreadID, jsonText(Observation{Summary: "Recovered exact native completion.", Outcome: "continue", NextStep: "Answer using the recorded evidence."}), "completed", false)
	restored, err := OpenAttention(s.directory, "space-test", "owner")
	if err != nil {
		t.Fatal(err)
	}
	e2, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e2.Close)
	if err := e2.AttachAttention(restored); err != nil {
		t.Fatal(err)
	}
	e2.Start()
	until(t, func() bool { return attentionCommitted(e2, run.ID) })
	f.mu.Lock()
	starts, turns, resumed := 0, 0, false
	for _, c := range f.calls {
		if c.Method == "thread/start" {
			starts++
		}
		if c.Method == "turn/start" {
			turns++
		}
		if c.Method == "thread/resume" && c.Params["threadId"] == run.ThreadID {
			resumed = true
		}
	}
	f.mu.Unlock()
	if starts != 1 || turns != 1 || !resumed || len(restored.Snapshot().Continuations) != 1 || len(e2.View().Messages) != 0 {
		t.Fatal("recovery replaced native work or leaked output", starts, turns, resumed)
	}
}

func TestAttentionCompletionPersistenceFailureReusesNativeResult(t *testing.T) {
	e, f, _ := setup(t)
	e.cfg.ReadSource = observationReader
	s, signal := attachEngineAttention(t, e)
	acceptAttentionSignals(t, s, signal)
	e.Start()
	run := active(t, e, "attention")
	file := filepath.Join(s.directory, "attention.json")
	if err := os.Rename(file, file+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0700); err != nil {
		t.Fatal(err)
	}
	f.complete(run.ThreadID, jsonText(Observation{Summary: "Persist this completed result after repair.", Outcome: "no_action"}), "completed", true)
	until(t, func() bool { return e.View().Error != "" })
	if attentionCommitted(e, run.ID) || s.Snapshot().Events[signal.ID].State != "observing" {
		t.Fatal("failed write acknowledged processing")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(file+"-saved", file); err != nil {
		t.Fatal(err)
	}
	until(t, func() bool { return attentionCommitted(e, run.ID) })
	f.mu.Lock()
	turns := len(f.threads[run.ThreadID])
	f.mu.Unlock()
	if turns != 1 || s.Snapshot().Events[signal.ID].State != "processed" {
		t.Fatal("storage repair reran the model or lost result")
	}
}

func TestAttentionRouteChangeDuringReadDoesNotDisableOldShadow(t *testing.T) {
	e, f, _ := setup(t)
	s, signal := attachEngineAttention(t, e)
	e.cfg.ReadSource = func(ctx context.Context, ref spacecontext.SourceRef) (SourceSnapshot, error) {
		if err := s.SetRoute(AttentionRoute{ShadowID: "replacement", Revision: 2, Enabled: true}); err != nil {
			return SourceSnapshot{}, err
		}
		return observationReader(ctx, ref)
	}
	acceptAttentionSignals(t, s, signal)
	e.Start()
	until(t, func() bool { return s.Snapshot().Route.Revision == 2 })
	send(t, e, "owner", "I can still talk to the previous shadow")
	run := active(t, e, "main")
	if !e.View().Enabled || s.Snapshot().Events[signal.ID].State != "queued" {
		t.Fatal("route handoff disabled chat or dropped source")
	}
	f.complete(run.ThreadID, "Ready.", "completed", true)
}
