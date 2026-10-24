package shadow

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTaskResultSnapshotsPreserveBusinessOutcomeAndSurviveNewGoal(t *testing.T) {
	e, f, cfg := setup(t)
	e.Start()
	message := send(t, e, "owner", "Investigate and finish with evidence")
	main := active(t, e, "main")
	worker := startWorkingTask(t, e, f, main)
	f.complete(worker.ThreadID, "Native final is only a progress report.", "completed", true)
	until(t, func() bool { return len(e.View().TaskResults) == 1 })
	first := e.View().TaskResults[0]
	if first.State != "needs_followup" || first.Outcome != "needs_followup" || first.MessageID != message.ID || first.TaskVersion != 1 || len(first.CompletionChecks) != 0 {
		t.Fatal("native final became a completed business result or lost origin", first)
	}
	j := currentJob(t, e, worker.JobID)
	args := taskCommandFor(j, "finish")
	args["outcome"], args["result"] = "completed", "Checked compatibility using the actual evidence."
	args["criteriaChecks"] = []string{"The requested compatibility condition is verified."}
	tool(t, e, f, main, "shadow_task", args)
	until(t, func() bool { return len(e.View().TaskResults) == 2 })
	second := e.View().TaskResults[1]
	if second.State != "completed" || second.Text == first.Text || len(second.CompletionChecks) != 1 {
		t.Fatal("checked completion not captured", second)
	}
	e.Close()
	e.mu.Lock()
	err := e.mutateLocked(func() error {
		job := e.record.Jobs[worker.JobID]
		job.Version++
		job.State, job.Result, job.ResultEventID = "queued", "", ""
		job.Prompt, job.CompletionCriteria = "A new requirement", []string{"New evidence required"}
		e.record.Jobs[job.ID] = job
		return nil
	})
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	results := reopened.View().TaskResults
	if len(results) != 2 || results[0].Text != first.Text || results[1].Text != second.Text || results[1].TaskVersion != 1 || results[1].CompletionCriteria[0] == "New evidence required" {
		t.Fatal("new goal or restart replaced old result", results)
	}
	results[1].CompletionChecks[0] = "caller mutation"
	if reopened.View().TaskResults[1].CompletionChecks[0] == "caller mutation" {
		t.Fatal("result projection aliases durable state")
	}
}

func TestTaskResultSnapshotAtomicFailureAndBoundedRetention(t *testing.T) {
	e, _, cfg := setup(t)
	base := time.Now()
	e.mu.Lock()
	err := e.mutateLocked(func() error {
		for i := range TaskResultHistoryLimit + 3 {
			id := fmt.Sprintf("job-%03d", i)
			e.record.Jobs[id] = Job{ID: id, Version: 1, ResultVersion: 1, State: "completed", Outcome: "completed", Result: "stable-result", ResultEventID: "result-" + id, ThreadID: "private-native-thread", ToolsetDigest: "private-tool-digest", UpdatedAt: base.Add(time.Duration(i) * time.Second)}
		}
		return nil
	})
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if got := e.View().TaskResults; len(got) != TaskResultHistoryLimit || got[0].ID != "result-job-003" {
		t.Fatal("retention boundary", len(got))
	}
	e.mu.Lock()
	e.cfg.Directory = filepath.Join(cfg.Directory, "missing")
	err = e.mutateLocked(func() error {
		e.record.Jobs["new"] = Job{ID: "new", Version: 1, ResultVersion: 1, State: "completed", Outcome: "completed", Result: "not-persisted", ResultEventID: "new-result", UpdatedAt: base.Add(time.Hour)}
		return nil
	})
	e.cfg.Directory = cfg.Directory
	e.mu.Unlock()
	if err == nil {
		t.Fatal("failed save succeeded")
	}
	for _, result := range e.View().TaskResults {
		if result.ID == "new-result" {
			t.Fatal("failed save exposed a result")
		}
	}
	e.mu.Lock()
	err = e.saveLocked()
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if got := e.View().TaskResults; len(got) != TaskResultHistoryLimit || got[0].ID != "result-job-003" {
		t.Fatal("old jobs resurrected expired results")
	}
	encoded, _ := json.Marshal(e.View().TaskResults)
	if strings.Contains(string(encoded), "private-") {
		t.Fatal("result snapshots copied private native metadata")
	}
}
