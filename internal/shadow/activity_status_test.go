package shadow

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestActivityStatusReceiptUnderstandingAndBusinessCompletion(t *testing.T) {
	e, _, _ := setup(t)
	s, signal := attachEngineAttention(t, e)
	acceptAttentionSignals(t, s, signal)
	before := s.Snapshot()
	p := s.ActivityStatuses([]string{signal.ID}, []*Engine{e})[signal.ID]
	if p.State != "received" || p.ReceivedAt == nil || len(p.Jobs) > 0 {
		t.Fatal(p)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("status read changed receipt or understanding")
	}
	// Observation completed; no business task was created. A model final is
	// evidence of processing, not proof of completing a separate objective.
	s.mu.Lock()
	ev := s.record.Events[signal.ID]
	ev.State = "processed"
	s.record.Events[signal.ID] = ev
	s.record.Continuations["c"] = AttentionContinuation{ID: "c", ShadowID: e.record.ID, EventIDs: []string{signal.ID}, State: "processed", CreatedAt: time.Now()}
	s.mu.Unlock()
	e.mu.Lock()
	e.record.Jobs["j"] = Job{ID: "j", Title: "Finish migration", Version: 1, State: "needs_followup", Trigger: &TaskTrigger{ContinuationID: "c"}, Prompt: "private prompt", ThreadID: "private-native-id"}
	e.record.Actions["a"] = CollaborationAction{ID: "a", Tool: "send_space_request", State: "saved", EventID: "c", Arguments: json.RawMessage(`{"private":"tool argument"}`), Result: json.RawMessage(`{"state":"accepted"}`)}
	e.mu.Unlock()
	p = s.ActivityStatuses([]string{signal.ID}, []*Engine{e})[signal.ID]
	if p.State != "received" || p.TotalJobs != 1 || p.Actions[0].State != "saved" {
		t.Fatal(p)
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "private") || strings.Contains(string(b), "arguments") || strings.Contains(string(b), `"result"`) {
		t.Fatal("raw execution data leaked", string(b))
	}
	e.mu.Lock()
	j := e.record.Jobs["j"]
	j.State = "completed"
	e.record.Jobs[j.ID] = j
	e.mu.Unlock()
	p = s.ActivityStatuses([]string{signal.ID}, []*Engine{e})[signal.ID]
	if p.State != "completed" {
		t.Fatal(p)
	}
	e.mu.Lock()
	failedReceipt := e.record.Actions["a"]
	failedReceipt.Result = json.RawMessage(`{"state":"sendFailed"}`)
	e.record.Actions["a"] = failedReceipt
	e.mu.Unlock()
	p = s.ActivityStatuses([]string{signal.ID}, []*Engine{e})[signal.ID]
	if p.State != "error" || p.Actions[0].ReceiptState != "sendFailed" {
		t.Fatal("saved failed receipt masked failed delivery", p)
	}
	e.mu.Lock()
	a := e.record.Actions["a"]
	a.State = "unknown"
	e.record.Actions[a.ID] = a
	e.mu.Unlock()
	p = s.ActivityStatuses([]string{signal.ID}, []*Engine{e})[signal.ID]
	if p.State == "completed" || p.Hold != "delivery_unknown" {
		t.Fatal("unknown send masked by task completion", p)
	}
}

func TestActivityStatusExactEventAndOriginalTaskOwner(t *testing.T) {
	e, _, _ := setup(t)
	s, first := attachEngineAttention(t, e)
	second := first
	second.ID = "later-event"
	second.Seq = 2
	second.Source.ReplyID = "unrelated-question"
	acceptAttentionSignals(t, s, first, second)
	s.mu.Lock()
	ev := s.record.Events[first.ID]
	ev.State = "task"
	ev.TaskDeliveries = []TaskDelivery{{ShadowID: e.record.ID, JobID: "j"}}
	s.record.Events[first.ID] = ev
	f := s.record.Focuses[ev.FocusID]
	f.JobIDs = []string{"j"}
	s.record.Focuses[f.ID] = f
	s.mu.Unlock()
	e.mu.Lock()
	e.record.Questions["q"] = TaskQuestion{ID: "q", Delivery: "saved", Recipients: []string{e.record.Owner.ID}}
	e.record.Jobs["j"] = Job{ID: "j", Title: "Original task", State: "waiting", Version: 1, Dependencies: []TaskDependency{{ID: "d", QuestionID: "q", State: "waiting"}}}
	e.mu.Unlock()
	if err := s.SetRoute(AttentionRoute{ShadowID: "replacement", Revision: 2, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	states := s.ActivityStatuses([]string{first.ID, second.ID}, []*Engine{e})
	if states[first.ID].State != "needs_owner" || states[first.ID].ShadowID != e.record.ID {
		t.Fatal(states[first.ID])
	}
	if states[second.ID].TotalJobs != 0 || states[second.ID].State != "received" || states[second.ID].ShadowID != "replacement" {
		t.Fatal("unrelated event borrowed object-wide job state", states[second.ID])
	}
	e.mu.Lock()
	j := e.record.Jobs["j"]
	j.Dependencies[0].Signals = []DependencySignal{{ID: "answer"}}
	j.State = "paused"
	e.record.Jobs[j.ID] = j
	e.mu.Unlock()
	if got := s.ActivityStatuses([]string{first.ID}, []*Engine{e})[first.ID]; got.State != "paused" || got.Jobs[0].Waiting != "answer_received" {
		t.Fatal(got)
	}
	e.mu.Lock()
	j = e.record.Jobs["j"]
	j.State = "completed"
	j.Dependencies[0].State = "satisfied"
	e.record.Jobs[j.ID] = j
	e.mu.Unlock()
	if got := s.ActivityStatuses([]string{first.ID}, []*Engine{e})[first.ID]; got.State != "completed" {
		t.Fatal(got)
	}
}

func TestActivityStatusQueuedContinuationIsNotAlreadyProcessed(t *testing.T) {
	e, _, _ := setup(t)
	s, signal := attachEngineAttention(t, e)
	acceptAttentionSignals(t, s, signal)
	s.mu.Lock()
	ev := s.record.Events[signal.ID]
	ev.State = "processed"
	s.record.Events[signal.ID] = ev
	s.record.Continuations["c"] = AttentionContinuation{ID: "c", ShadowID: e.record.ID, EventIDs: []string{signal.ID}, State: "queued"}
	s.mu.Unlock()
	if err := e.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	p := s.ActivityStatuses([]string{signal.ID}, []*Engine{e})[signal.ID]
	if p.State != "received" || p.Hold != "shadow_paused" {
		t.Fatal(p)
	}
	// A different member's runtime cannot supply a name, state or private job.
	e.mu.Lock()
	e.record.Owner = Actor{ID: "other-member"}
	e.mu.Unlock()
	p = s.ActivityStatuses([]string{signal.ID}, []*Engine{e})[signal.ID]
	if p.ShadowName != "" || p.Hold != "shadow_offline" {
		t.Fatal(p)
	}
}
