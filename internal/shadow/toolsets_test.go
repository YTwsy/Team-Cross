package shadow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/spacecontext"
)

func changeToolsetRecord(t *testing.T, cfg Config, change func(*Record)) {
	t.Helper()
	path := filepath.Join(cfg.Directory, "shadow.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record Record
	if err = json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	change(&record)
	if err = writeAtomic(path, record); err != nil {
		t.Fatal(err)
	}
}

func extraFixtureTool() map[string]any {
	return map[string]any{"name": "read_upgrade_evidence", "description": "Read the dedicated upgrade fixture evidence.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}, "annotations": map[string]any{"readOnlyHint": true}}
}

func TestToolsetLegacyMainUpgradesOnceAndRetainsPendingWork(t *testing.T) {
	e, f, cfg := setup(t)
	e.Start()
	send(t, e, "owner", "Keep this original constraint: support client v3.")
	first := active(t, e, "main")
	f.complete(first.ThreadID, "Client v3 remains required.", "completed", true)
	until(t, func() bool { return !e.View().Busy })
	e.Close()
	changeToolsetRecord(t, cfg, func(r *Record) {
		for i := range r.Segments {
			r.Segments[i].ToolsetDigest = ""
		}
	})
	cfg.CollaborationTools = []map[string]any{extraFixtureTool()}
	e2, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e2.Close)
	if started, err := e2.beginToolsetUpgrade(); started || err != nil {
		t.Fatal("idle legacy shadow started an unnecessary model handoff", started, err)
	}
	pending := send(t, e2, "owner", "Continue with the new evidence tool; preserve v3.")
	e2.Start()
	next := active(t, e2, "main")
	if next.ThreadID == first.ThreadID || next.SourceID != pending.ID || len(e2.View().Segments) != 2 {
		t.Fatal("pending work did not reach an upgraded segment", next, e2.View())
	}
	e2.mu.Lock()
	current := clone(e2.record)
	e2.mu.Unlock()
	if current.Segments[0].ClosedAt == nil || current.Segments[0].Checkpoint == "" || current.Segments[1].ToolsetDigest == "" {
		t.Fatal("upgrade lacks a durable checkpoint or exact tool registration", current.Segments)
	}
	f.mu.Lock()
	newTools, handoffs := false, 0
	for _, c := range f.calls {
		if c.Method == "thread/resume" && c.Params["dynamicTools"] != nil {
			t.Error("sent unsupported tools on resume")
		}
		if c.Method == "thread/start" && strings.Contains(jsonText(c.Params["dynamicTools"]), "read_upgrade_evidence") {
			newTools = true
		}
		if c.Method == "turn/start" && strings.Contains(jsonText(c.Params["input"]), "Prepare a handoff checkpoint") {
			handoffs++
		}
	}
	f.mu.Unlock()
	if !newTools || handoffs != 1 || len(e2.View().Messages) != 3 {
		t.Fatal("tool upgrade was skipped, repeated, or published internal handoff", newTools, handoffs, e2.View().Messages)
	}
	f.complete(next.ThreadID, "Resumed once with the current tools.", "completed", true)
	until(t, func() bool { return !e2.View().Busy })
	e2.Close()
	e3, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e3.Close)
	send(t, e3, "owner", "Another ordinary message")
	e3.Start()
	last := active(t, e3, "main")
	if last.ThreadID != next.ThreadID || len(e3.View().Segments) != 2 {
		t.Fatal("unchanged toolset upgraded again after restart", last, e3.View().Segments)
	}
}

func TestToolsetWaitsForNativeRunAndExactApproval(t *testing.T) {
	e, f, _ := setup(t)
	e.Start()
	send(t, e, "owner", "An already accepted message")
	run := active(t, e, "main")
	e.mu.Lock()
	e.record.Segments[0].ToolsetDigest = "legacy"
	e.mu.Unlock()
	send(t, e, "owner", "Queued work must wait for the current native result")
	if started, err := e.beginToolsetUpgrade(); started || err != nil {
		t.Fatal("upgraded a live native run", started, err)
	}
	e.mu.Lock()
	e.approvals["fixture-approval"] = Approval{Params: json.RawMessage(`{"threadId":"` + run.ThreadID + `"}`)}
	e.mu.Unlock()
	f.complete(run.ThreadID, "Original work completed once.", "completed", true)
	until(t, func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.record.Runs[run.ID].State == "completed"
	})
	if started, err := e.beginToolsetUpgrade(); started || err != nil {
		t.Fatal("upgraded while the exact native approval remained unresolved", started, err)
	}
	e.mu.Lock()
	delete(e.approvals, "fixture-approval")
	e.notify()
	e.mu.Unlock()
	next := active(t, e, "main")
	if next.ThreadID == run.ThreadID {
		t.Fatal("did not upgrade after the boundary became available")
	}
}

func TestToolsetWorkerUpgradePreservesWaitEvidenceAndActions(t *testing.T) {
	e, f, cfg := setup(t)
	reader := func(context.Context, TaskDependency) (DependencyUpdate, error) { return DependencyUpdate{}, nil }
	e.cfg.ReadDependency, cfg.ReadDependency = reader, reader
	e.Start()
	send(t, e, "owner", "Investigate, preserve the original task and continue when answered")
	main := active(t, e, "main")
	worker := startWorkingTask(t, e, f, main)
	job := currentJob(t, e, worker.JobID)
	args := taskCommandFor(job, "wait")
	args["dependencies"] = []TaskDependency{{ID: "answer", Kind: "discussion", Condition: "Exact supported client version", Source: spacecontext.SourceRef{SpaceID: "space-test", AnnotationID: "discussion", ReplyID: "question"}}}
	tool(t, e, f, worker, "shadow_task", args)
	f.complete(worker.ThreadID, "WAIT_CHECKPOINT_MARKER", "completed", true)
	f.complete(main.ThreadID, "Waiting for the exact answer.", "completed", true)
	until(t, func() bool { return !e.View().Busy && currentJob(t, e, worker.JobID).State == "waiting" })
	e.Close()
	actionID := uuid.NewString()
	changeToolsetRecord(t, cfg, func(r *Record) {
		j := r.Jobs[worker.JobID]
		j.ToolsetDigest = ""
		j.State = "queued"
		j.Dependencies[0].Signals = []DependencySignal{{ID: "answer-v3", Preview: "Support v3", Source: spacecontext.SourceRef{SpaceID: "space-test", AnnotationID: "discussion", ReplyID: "answer"}}}
		r.Jobs[j.ID] = j
		if r.Actions == nil {
			r.Actions = map[string]CollaborationAction{}
		}
		r.Actions[actionID] = CollaborationAction{ID: actionID, JobID: j.ID, JobVersion: j.Version, State: "saved", Tool: "reply_to_annotation", CreatedAt: time.Now()}
	})
	e2, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e2.Close)
	e2.Start()
	next := nextJobRun(t, e2, worker)
	got := currentJob(t, e2, job.ID)
	if next.ThreadID == worker.ThreadID || got.Version != job.Version || len(got.PreviousThreads) != 1 || got.PreviousThreads[0] != worker.ThreadID || got.ToolsetDigest == "" || len(got.Dependencies[0].Signals) != 1 || got.Checkpoint == nil {
		t.Fatal("worker migration lost task identity, evidence or native lineage", got)
	}
	f.mu.Lock()
	prompt := itemText(f.threads[next.ThreadID][0].Items[0])
	oldTurns := len(f.threads[worker.ThreadID])
	f.mu.Unlock()
	for _, want := range []string{"WAIT_CHECKPOINT_MARKER", "answer-v3", actionID, job.Prompt, worker.ThreadID} {
		if !strings.Contains(prompt, want) {
			t.Error("upgraded worker lacks checkpoint context", want)
		}
	}
	if oldTurns != 1 {
		t.Fatal("upgrade replayed the original native worker")
	}
	e2.Close()
	e3, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e3.Close)
	e3.Start()
	restored := active(t, e3, "job")
	if restored.ThreadID != next.ThreadID || restored.TurnID != next.TurnID {
		t.Fatal("lost upgraded native identity on restart", restored, next)
	}
}

func TestToolsetRegistrationOrderDoesNotCauseUpgrade(t *testing.T) {
	e, _, _ := setup(t)
	a, b := extraFixtureTool(), extraFixtureTool()
	b["name"] = "second_fixture_tool"
	e.cfg.CollaborationTools = []map[string]any{a, b}
	_, first, err := e.nativeTools()
	if err != nil {
		t.Fatal(err)
	}
	e.cfg.CollaborationTools = []map[string]any{b, a}
	_, same, err := e.nativeTools()
	if err != nil || first != same {
		t.Fatal("registration order changed native capability identity")
	}
	b["description"] = "Updated actual tool behavior."
	_, changed, err := e.nativeTools()
	if err != nil || first == changed {
		t.Fatal("changed tool registration was not detected")
	}
}

func TestToolsetFailedHandoffDoesNotRepeat(t *testing.T) {
	e, _, _ := setup(t)
	e.mu.Lock()
	id, native := uuid.NewString(), uuid.NewString()
	e.record.Current = native
	e.record.Enabled = true
	e.record.Transition = &Transition{From: native, Stage: "note", Reason: "native-tools-updated"}
	e.record.Runs[id] = Run{ID: id, ThreadID: native, Kind: "handoff", State: "submitted"}
	e.finishRunLocked(id, "failed", "handoff unavailable")
	paused := !e.record.Enabled && e.record.Current == native && e.record.Transition == nil && e.record.Error != ""
	e.mu.Unlock()
	if !paused {
		t.Fatal("failed tool upgrade did not preserve the original session and stop retries")
	}
	if err := e.step(context.Background()); err != nil {
		t.Fatal(err)
	}
}
