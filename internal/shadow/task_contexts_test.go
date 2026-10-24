package shadow

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"teamcross/internal/spacecontext"
)

func saveTaskContextFacts(t *testing.T, s *AttentionStore, id, summary string, version int) Focus {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.record)
	f := next.Focuses[id]
	f.ID, f.Subject = id, spacecontext.Subject{Kind: "material", ID: id}
	f.Goal, f.GoalVersion, f.State = "PRIVATE_FOLLOW_GOAL", 1, "active"
	f.Revision++
	f.ProcessedSeq++
	f.Summary = summary
	f.Evidence = []spacecontext.SourceRef{{SpaceID: s.record.SpaceID, MaterialID: id, Version: version}}
	next.Focuses[id] = f
	if err := s.saveLocked(next); err != nil {
		t.Fatal(err)
	}
	return f
}

func reviewTaskContexts(job Job) []TaskContextReview {
	out := []TaskContextReview{}
	for _, c := range job.Contexts {
		if contextPending(c) {
			out = append(out, TaskContextReview{ID: c.ID, Revision: c.Current.Revision, Explanation: "Read the linked evidence and reconciled its impact on the current requirements."})
		}
	}
	return out
}

func TestTaskContextsChangedFactsResumeSameWorkerAfterRestart(t *testing.T) {
	e, native, cfg := setup(t)
	store, _ := attachEngineAttention(t, e)
	a := saveTaskContextFacts(t, store, "interface", "OLD_INTERFACE_FACTS", 1)
	b := saveTaskContextFacts(t, store, "validation", "UNCHANGED_VALIDATION_FACTS", 1)
	reader := func(_ context.Context, d TaskDependency) (DependencyUpdate, error) {
		return DependencyUpdate{Cursor: d.Cursor}, nil
	}
	e.cfg.ReadDependency, cfg.ReadDependency = reader, reader
	e.Start()
	send(t, e, "owner", "Use the interface and validation facts; ask for the missing client version and continue when it arrives.")
	main := active(t, e, "main")
	created := cloneMap(tool(t, e, native, main, "shadow_task", map[string]any{
		"operation": "start", "title": "Compatibility", "prompt": "Reconcile interface and validation facts", "completionCriteria": []string{"Use current interface facts and obtain the client version"},
		"relatedFocuses": []TaskContextInput{{ID: a.ID, Reason: "Interface failure semantics"}, {ID: b.ID, Reason: "Validation outcome"}},
	}))
	worker := jobRun(t, e, created["id"].(string))
	j := currentJob(t, e, worker.JobID)
	if len(j.Contexts) != 2 || j.Follow != nil || !pendingTaskContexts(j) || len(e.View().Jobs[0].Contexts) != 0 {
		t.Fatal("facts changed task ownership or leaked into shared view", j, e.View())
	}
	finish := taskCommandFor(j, "finish")
	finish["outcome"], finish["result"], finish["criteriaChecks"] = "completed", "Done", []string{"Checked"}
	if taskToolError(e, worker, finish) == nil {
		t.Fatal("unreviewed baseline completed the task")
	}
	input := taskCommandFor(j, "wait")
	input["contextReviews"] = reviewTaskContexts(j)
	input["dependencies"] = []TaskDependency{{ID: "version", Kind: "discussion", Condition: "The client version is provided", Source: spacecontext.SourceRef{AnnotationID: "client-question"}}}
	tool(t, e, native, worker, "shadow_task", input)
	native.complete(worker.ThreadID, "Waiting for the version", "completed", true)
	until(t, func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.record.Jobs[j.ID].State == "waiting" && !e.taskRunActiveLocked(j.ID)
	})
	e.Close()
	saveTaskContextFacts(t, store, a.ID, "NEW_INTERFACE_FACTS", 2)
	saveTaskContextFacts(t, store, "unrelated", "UNRELATED_MUST_NOT_ENTER", 1)
	restored, err := OpenAttention(store.directory, "space-test", "owner")
	if err != nil {
		t.Fatal(err)
	}
	e2, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer e2.Close()
	if err = e2.AttachAttention(restored); err != nil {
		t.Fatal(err)
	}
	e2.Start()
	next := nextJobRun(t, e2, worker)
	j = currentJob(t, e2, j.ID)
	if next.ThreadID != worker.ThreadID || j.Version != 1 || !contextPending(j.Contexts[0]) || contextPending(j.Contexts[1]) || j.Contexts[0].Reviewed.Summary != "OLD_INTERFACE_FACTS" {
		t.Fatal("changed evidence lost its previous baseline or started different work", j, next)
	}
	native.mu.Lock()
	turns := native.threads[next.ThreadID]
	prompt := itemText(turns[len(turns)-1].Items[0])
	native.mu.Unlock()
	for _, text := range []string{"NEW_INTERFACE_FACTS", "OLD_INTERFACE_FACTS", "contextReviews"} {
		if !strings.Contains(prompt, text) {
			t.Fatal("missing changed context", text, prompt)
		}
	}
	for _, text := range []string{"UNCHANGED_VALIDATION_FACTS", "UNRELATED_MUST_NOT_ENTER", "PRIVATE_FOLLOW_GOAL", "fingerprint"} {
		if strings.Contains(prompt, text) {
			t.Fatal("unrelated/private bookkeeping entered task packet", text, prompt)
		}
	}
	stale := taskCommandFor(j, "checkpoint")
	stale["contextReviews"] = []TaskContextReview{{ID: a.ID, Revision: a.Revision, Explanation: "Old facts"}}
	if taskToolError(e2, next, stale) == nil {
		t.Fatal("stale evidence review was accepted")
	}
	input = taskCommandFor(j, "wait")
	input["contextReviews"] = reviewTaskContexts(j)
	tool(t, e2, native, next, "shadow_task", input)
	native.complete(next.ThreadID, "Updated the analysis; still need the client version", "completed", true)
	until(t, func() bool { return currentJob(t, e2, j.ID).State == "waiting" })
	if currentJob(t, e2, j.ID).Dependencies[0].State != "waiting" {
		t.Fatal("new context silently satisfied a separate question")
	}
}

func TestTaskContextsPersistenceRollbackAndMetadataDoNotWake(t *testing.T) {
	e, _, cfg := setup(t)
	store, _ := attachEngineAttention(t, e)
	focus := saveTaskContextFacts(t, store, "interface", "Reviewed facts", 1)
	e.mu.Lock()
	contexts, err := e.taskContextsLocked(nil, []TaskContextInput{{ID: focus.ID, Reason: "Failure semantics"}})
	if err == nil {
		contexts[0].Reviewed = clone(contexts[0].Current)
		err = e.mutateLocked(func() error {
			e.record.Jobs["job"] = Job{ID: "job", Version: 1, Revision: 1, State: "waiting", Contexts: contexts}
			return nil
		})
	}
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	// A newer observation with identical facts or a changed follow goal is not
	// itself another reason to invoke the worker.
	saveTaskContextFacts(t, store, focus.ID, "Reviewed facts", 1)
	if err = e.syncTaskFollows(); err != nil || currentJob(t, e, "job").Revision != 1 {
		t.Fatal("unchanged facts woke the task", err)
	}
	saveTaskContextFacts(t, store, focus.ID, "Revised facts", 2)
	if err = os.Rename(cfg.Directory, cfg.Directory+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(cfg.Directory, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = e.syncTaskFollows(); err == nil || currentJob(t, e, "job").State != "waiting" || currentJob(t, e, "job").Contexts[0].Current.Summary != "Reviewed facts" {
		t.Fatal("failed save consumed revised facts", err)
	}
	if err = os.Remove(cfg.Directory); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(cfg.Directory+"-saved", cfg.Directory); err != nil {
		t.Fatal(err)
	}
	if err = e.syncTaskFollows(); err != nil {
		t.Fatal(err)
	}
	j := currentJob(t, e, "job")
	if j.State != "queued" || j.Contexts[0].Reviewed.Summary != "Reviewed facts" || j.Contexts[0].Current.Summary != "Revised facts" {
		t.Fatal(j)
	}
}

func TestTaskContextsLateChangeCannotBeIgnoredByWaitOrCompletion(t *testing.T) {
	e, native, _ := setup(t)
	store, _ := attachEngineAttention(t, e)
	focus := saveTaskContextFacts(t, store, "interface", "Initial facts", 1)
	e.Start()
	send(t, e, "owner", "Use the interface evidence")
	main := active(t, e, "main")
	created := cloneMap(tool(t, e, native, main, "shadow_task", map[string]any{"operation": "start", "title": "Use facts", "prompt": "Use current facts", "relatedFocuses": []TaskContextInput{{ID: focus.ID, Reason: "Interface semantics"}}}))
	worker := jobRun(t, e, created["id"].(string))
	j := currentJob(t, e, worker.JobID)
	input := taskCommandFor(j, "wait")
	input["contextReviews"] = reviewTaskContexts(j)
	due := time.Now().Add(time.Hour)
	input["dependencies"] = []TaskDependency{{ID: "check", Kind: "time", Condition: "Check at the requested time", DueAt: &due}}
	tool(t, e, native, worker, "shadow_task", input)
	saveTaskContextFacts(t, store, focus.ID, "Facts arrived after wait was saved", 2)
	native.complete(worker.ThreadID, "Waiting", "completed", true)
	next := nextJobRun(t, e, worker)
	j = currentJob(t, e, j.ID)
	if next.ThreadID != worker.ThreadID || !pendingTaskContexts(j) {
		t.Fatal("late knowledge update remained asleep", j)
	}
	input = taskCommandFor(j, "wait")
	if taskToolError(e, next, input) == nil {
		t.Fatal("wait silently discarded newly arrived context")
	}
}

func TestTaskContextsBoundedProjectionAndExplicitReplacement(t *testing.T) {
	e, _, _ := setup(t)
	store, _ := attachEngineAttention(t, e)
	focus := saveTaskContextFacts(t, store, "material", strings.Repeat("事实", 2000), 1)
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, err := e.taskContextsLocked(nil, []TaskContextInput{{ID: "missing", Reason: "Needed"}}); err == nil {
		t.Fatal("unknown context accepted")
	}
	if _, err := e.taskContextsLocked(nil, []TaskContextInput{{ID: focus.ID, Reason: ""}}); err == nil {
		t.Fatal("link without purpose accepted")
	}
	contexts, err := e.taskContextsLocked(nil, []TaskContextInput{{ID: focus.ID, Reason: "Specific API requirement"}})
	if err != nil || !contexts[0].Current.Partial || len(jsonText(contexts[0].Current)) > 3<<10 {
		t.Fatal("context is not bounded", contexts, err)
	}
	j := Job{Contexts: contexts}
	projection, _ := json.Marshal(taskForContext(j))
	if strings.Contains(string(projection), "fingerprint") || !strings.Contains(string(projection), "needsReview") || len(taskIndex([]Job{j})[0].Contexts) != 0 {
		t.Fatal("model or index projection exposed transport bookkeeping", string(projection))
	}
	empty := []TaskContextInput{}
	if err = e.reviewTaskContextsLocked(&j, taskLifecycleInput{RelatedFocuses: &empty}); err != nil || len(j.Contexts) != 0 {
		t.Fatal("explicit replacement did not remove obsolete source", j, err)
	}
}

func TestTaskContextsIgnoredPacketDoesNotAutomaticallyRepeat(t *testing.T) {
	e, native, _ := setup(t)
	store, _ := attachEngineAttention(t, e)
	focus := saveTaskContextFacts(t, store, "interface", "Actual facts", 1)
	e.Start()
	send(t, e, "owner", "Use the interface facts")
	main := active(t, e, "main")
	created := cloneMap(tool(t, e, native, main, "shadow_task", map[string]any{"operation": "start", "title": "Facts", "prompt": "Check actual facts", "relatedFocuses": []TaskContextInput{{ID: focus.ID, Reason: "Failure semantics"}}}))
	worker := jobRun(t, e, created["id"].(string))
	native.complete(worker.ThreadID, "I did not save a review or a completion decision", "completed", true)
	until(t, func() bool { return currentJob(t, e, worker.JobID).State == "needs_followup" })
	if err := e.syncTaskFollows(); err != nil {
		t.Fatal(err)
	}
	if j := currentJob(t, e, worker.JobID); j.State != "needs_followup" || !pendingTaskContexts(j) {
		t.Fatal("an already presented packet caused an automatic retry loop", j)
	}
	e.mu.Lock()
	input := e.record.Runs[worker.ID].ContextInputs[focus.ID]
	e.mu.Unlock()
	if input == "" {
		t.Fatal("the actual native input was not recorded")
	}
}

func TestTaskContextsOwnChangeObservationFeedsExistingWork(t *testing.T) {
	e, native, _ := setup(t)
	store, signal := attachEngineAttention(t, e)
	e.cfg.ReadSource = observationReader
	acceptAttentionSignals(t, store, signal)
	e.Start()
	first := active(t, e, "attention")
	native.complete(first.ThreadID, jsonText(Observation{Summary: "The supported version is still unknown", Evidence: []spacecontext.SourceRef{signal.Source}, OpenQuestions: []string{"Which client version?"}, Outcome: "no_action"}), "completed", true)
	until(t, func() bool { return attentionCommitted(e, first.ID) })
	record := store.Snapshot()
	focus := record.Focuses[record.Events[signal.ID].FocusID]
	send(t, e, "owner", "Use the compatibility facts and check again at the requested time")
	main := active(t, e, "main")
	created := cloneMap(tool(t, e, native, main, "shadow_task", map[string]any{"operation": "start", "title": "Compatibility", "prompt": "Use the supported version in the migration plan", "relatedFocuses": []TaskContextInput{{ID: focus.ID, Reason: "The client version affects the migration"}}}))
	worker := jobRun(t, e, created["id"].(string))
	j := currentJob(t, e, worker.JobID)
	input := taskCommandFor(j, "wait")
	due := time.Now().Add(time.Hour)
	input["contextReviews"] = reviewTaskContexts(j)
	input["dependencies"] = []TaskDependency{{ID: "later", Kind: "time", Condition: "Requested future check", DueAt: &due}}
	tool(t, e, native, worker, "shadow_task", input)
	native.complete(worker.ThreadID, "Waiting for the requested check", "completed", true)
	until(t, func() bool { return currentJob(t, e, j.ID).LastRunState == "completed" })
	change := clone(signal)
	change.ID, change.Seq, change.Source.ReplyID = "owner-change", 2, "owner-answer"
	change.Actor = spacecontext.Actor{Kind: "human", MemberID: "owner", Name: "Owner"}
	change.Reasons = []string{"own_source_change", "follow_discussion"}
	acceptAttentionSignals(t, store, change)
	observation := active(t, e, "attention")
	native.mu.Lock()
	prompt := itemText(native.threads[observation.ThreadID][0].Items[0])
	native.mu.Unlock()
	if !strings.Contains(prompt, j.ID) || !strings.Contains(prompt, "The client version affects the migration") || !strings.Contains(prompt, "memberChanges") {
		t.Fatal("observer did not know existing work or the member's change", prompt)
	}
	native.complete(observation.ThreadID, jsonText(Observation{Summary: "The member supplied version v3", Evidence: []spacecontext.SourceRef{change.Source}, Outcome: "no_action"}), "completed", true)
	next := nextJobRun(t, e, worker)
	j = currentJob(t, e, j.ID)
	if next.ThreadID != worker.ThreadID || len(store.Snapshot().Continuations) != 0 || len(e.View().Jobs) != 1 || j.Contexts[0].Current.Summary != "The member supplied version v3" || !pendingTaskContexts(j) {
		t.Fatal("understanding failed to reach existing work or duplicated coordination", j, store.Snapshot().Continuations)
	}
}
