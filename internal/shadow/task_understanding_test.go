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

func understandingFixture(t *testing.T) (*Engine, Config, *AttentionStore, Run, spacecontext.Signal) {
	t.Helper()
	e, _, cfg := setup(t)
	store, signal := attachEngineAttention(t, e)
	signal.ID, signal.Seq, signal.Source.ReplyID = "answer-event", 2, "answer"
	signal.Reasons = []string{"direct_reply", "follow_discussion"}
	job := testWaitingJob()
	job.State, job.SourceID, job.ThreadID = "running", "member-request", "worker"
	job.CompletionCriteria = []string{"Confirm supported client version"}
	job.Trigger = &TaskTrigger{Kind: "message", SourceID: job.SourceID, MessageID: job.SourceID}
	run := Run{ID: "working-run", ThreadID: job.ThreadID, TurnID: "turn", Kind: "job", State: "submitted", JobID: job.ID, JobVersion: job.Version}
	e.mu.Lock()
	err := e.mutateLocked(func() error {
		e.record.Messages = append(e.record.Messages, Message{ID: job.SourceID, Role: "user", Actor: Actor{ID: "owner", Kind: "human"}, Text: "Confirm compatibility"})
		e.record.Jobs[job.ID], e.record.Runs[run.ID] = job, run
		return nil
	})
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	e.cfg.ResolveDependency, cfg.ResolveDependency = exactAnswerResolver, exactAnswerResolver
	return e, cfg, store, run, signal
}

func answerUnderstanding(t *testing.T, e *Engine, run Run, summary string) map[string]any {
	t.Helper()
	e.mu.Lock()
	job := e.record.Jobs[run.JobID]
	targets := e.taskUnderstandingTargetsLocked(job)
	e.mu.Unlock()
	updates := []TaskUnderstandingUpdate{}
	for _, target := range targets {
		evidence := []spacecontext.SourceRef{}
		for _, dependency := range job.Dependencies {
			for _, signal := range dependency.Signals {
				for _, id := range target.Answers {
					if id == signal.ID {
						evidence = append(evidence, signal.Source)
					}
				}
			}
		}
		updates = append(updates, TaskUnderstandingUpdate{ID: target.ID, BaseRevision: target.Current.Revision, SignalIDs: target.Answers, Summary: summary, Evidence: evidence})
	}
	return map[string]any{"operation": "understand", "id": job.ID, "baseVersion": job.Version, "baseRevision": job.Revision, "requestId": "understanding-one", "understandings": updates}
}

func seedDiscussionUnderstanding(t *testing.T, store *AttentionStore, signal spacecontext.Signal) Focus {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	next := clone(store.record)
	f := Focus{ID: attentionHash(signal.Subject), Subject: signal.Subject, Goal: "PRIVATE_GOAL", GoalVersion: 1, Revision: 3, State: "active", Processing: "idle", Summary: "Retained interface semantics; waiting for client version", OpenQuestions: []string{"Which client version?"}, Evidence: []spacecontext.SourceRef{{SpaceID: signal.Source.SpaceID, AnnotationID: signal.Source.AnnotationID, ReplyID: "question"}}}
	next.Focuses[f.ID] = f
	if err := store.saveLocked(next); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestTaskUnderstandingRoutedAnswerUpdatesFactsAndLinkedWork(t *testing.T) {
	e, _, store, run, signal := understandingFixture(t)
	focus := seedDiscussionUnderstanding(t, store, signal)
	e.mu.Lock()
	contexts, err := e.taskContextsLocked(nil, []TaskContextInput{{ID: focus.ID, Reason: "Compatibility facts"}})
	if err == nil {
		contexts[0].Reviewed = clone(contexts[0].Current)
		err = e.mutateLocked(func() error {
			e.record.Jobs["linked"] = Job{ID: "linked", State: "waiting", Version: 1, Revision: 1, Contexts: contexts}
			return nil
		})
	}
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	acceptAttentionSignals(t, store, signal)
	batch, err := store.ClaimBatch(e.View().ID, time.Now(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	batch, err = e.routeBatch(context.Background(), store, batch.ID)
	if err != nil || batch.State != "task" || store.Snapshot().Focuses[focus.ID].Summary != focus.Summary {
		t.Fatal("receipt prematurely claimed understanding", batch, err)
	}
	job := currentJob(t, e, run.JobID)
	update := answerUnderstanding(t, e, run, "Retained interface semantics; client v3 confirmed")
	if err = taskToolError(e, run, update); err != nil {
		t.Fatal(err)
	}
	after := store.Snapshot()
	f := after.Focuses[focus.ID]
	if f.Revision != focus.Revision+1 || f.Summary != "Retained interface semantics; client v3 confirmed" || len(f.OpenQuestions) != 0 || len(after.UnderstoodAnswers) != 1 || len(after.Continuations) != 0 {
		t.Fatal("answer did not become durable facts without another coordinator", f, after.Continuations)
	}
	if err = e.syncTaskFollows(); err != nil {
		t.Fatal(err)
	}
	linked := currentJob(t, e, "linked")
	if linked.State != "queued" || !pendingTaskContexts(linked) || linked.Contexts[0].Reviewed.Summary != focus.Summary {
		t.Fatal("other work did not receive the changed facts with its prior baseline", linked)
	}
	if currentJob(t, e, job.ID).Dependencies[0].State != "waiting" {
		t.Fatal("understanding implicitly satisfied a task condition")
	}
	finish := taskCommandFor(currentJob(t, e, job.ID), "finish")
	finish["resolutions"] = []DependencyResolution{{ID: "version", Explanation: "The answer explicitly confirms v3", SignalIDs: []string{"reply:answer"}}}
	finish["outcome"], finish["result"], finish["criteriaChecks"] = "completed", "Client v3 confirmed", []string{"Verified actual v3 answer"}
	if err = taskToolError(e, run, finish); err != nil || currentJob(t, e, job.ID).State != "completed" {
		t.Fatal("original task did not complete after both understanding and criteria", err)
	}
	for _, shared := range e.View().Jobs {
		if len(shared.Understandings) != 0 || len(shared.Contexts) != 0 {
			t.Fatal("private facts entered shared task projection", shared)
		}
	}
}

func TestTaskUnderstandingPollingBeforeActivityAndRestartDoesNotReplay(t *testing.T) {
	e, cfg, store, run, signal := understandingFixture(t)
	// The task knows its exact answer before the member's dynamic stream has
	// created any focus. Understanding must still be durable and discoverable.
	if _, err := e.RouteDependencyActivity(context.Background(), signal); err != nil {
		t.Fatal(err)
	}
	args := answerUnderstanding(t, e, run, "The answer confirms v3")
	if err := taskToolError(e, run, args); err != nil {
		t.Fatal(err)
	}
	before := store.Snapshot()
	e.Close()
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
	if err = taskToolError(e2, run, args); err != nil {
		t.Fatal("same committed command did not recover", err)
	}
	acceptAttentionSignals(t, restored, signal)
	batch, err := restored.ClaimBatch(e2.View().ID, time.Now(), 0, 0)
	if err == nil {
		batch, err = e2.routeBatch(context.Background(), restored, batch.ID)
	}
	id := attentionHash(signal.Subject)
	if err != nil || batch.State != "task" || restored.Snapshot().Focuses[id].Revision != before.Focuses[id].Revision {
		t.Fatal("late event reprocessed an already understood answer", err, batch)
	}
	e2.mu.Lock()
	pending := e2.taskUnderstandingTargetsLocked(e2.record.Jobs[run.JobID])
	e2.mu.Unlock()
	if len(pending) != 0 {
		t.Fatal("restart forgot understood source receipts", pending)
	}
	args["baseVersion"] = 2
	if taskToolError(e2, run, args) == nil {
		t.Fatal("same command ID accepted different input")
	}
}

func TestTaskUnderstandingRejectsStaleAndFabricatedUpdatesAtomically(t *testing.T) {
	e, _, store, run, signal := understandingFixture(t)
	focus := seedDiscussionUnderstanding(t, store, signal)
	if _, err := e.RouteDependencyActivity(context.Background(), signal); err != nil {
		t.Fatal(err)
	}
	args := answerUnderstanding(t, e, run, "v3")
	updates := args["understandings"].([]TaskUnderstandingUpdate)
	before := attentionHash(store.Snapshot())
	for _, variant := range []string{"stale", "foreign focus", "fabricated source", "fabricated signal", "no answer evidence"} {
		t.Run(variant, func(t *testing.T) {
			bad := clone(updates)
			switch variant {
			case "stale":
				bad[0].BaseRevision--
			case "foreign focus":
				bad[0].ID = "unrelated"
			case "fabricated source":
				bad[0].Evidence = append(bad[0].Evidence, spacecontext.SourceRef{SpaceID: "other-space", AnnotationID: "secret"})
			case "fabricated signal":
				bad[0].SignalIDs = []string{"not-received"}
			case "no answer evidence":
				bad[0].Evidence = focus.Evidence
			}
			args["understandings"] = bad
			if err := taskToolError(e, run, args); err == nil || attentionHash(store.Snapshot()) != before {
				t.Fatal("invalid update changed facts or receipts", err)
			}
		})
	}
}

func TestTaskUnderstandingDiskFailureDoesNotConsumeAnswers(t *testing.T) {
	e, _, store, run, signal := understandingFixture(t)
	if _, err := e.RouteDependencyActivity(context.Background(), signal); err != nil {
		t.Fatal(err)
	}
	args := answerUnderstanding(t, e, run, "v3")
	before := attentionHash(store.Snapshot())
	backup := store.directory + "-saved"
	if err := os.Rename(store.directory, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.directory, []byte("block writes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := taskToolError(e, run, args); err == nil || attentionHash(store.Snapshot()) != before {
		t.Fatal("failed save advanced facts or answer receipts", err)
	}
	if err := os.Remove(store.directory); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, store.directory); err != nil {
		t.Fatal(err)
	}
	if err := taskToolError(e, run, args); err != nil || len(store.Snapshot().UnderstoodAnswers) != 1 {
		t.Fatal("same failed command could not recover", err)
	}
}

func TestTaskUnderstandingLateObservationPreservesNewFactsAndActions(t *testing.T) {
	for _, direct := range []bool{false, true} {
		t.Run(map[bool]string{false: "observer", true: "direct already acted"}[direct], func(t *testing.T) {
			e, _, store, run, signal := understandingFixture(t)
			focus := seedDiscussionUnderstanding(t, store, signal)
			older := clone(signal)
			older.ID, older.Seq, older.Source.ReplyID, older.Reasons = "older-update", 1, "older", []string{"follow_discussion"}
			if direct {
				older.Reasons = []string{"mention"}
			}
			acceptAttentionSignals(t, store, older)
			batch, err := store.ClaimBatch(e.View().ID, time.Now(), 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			batch = prepareObservation(t, store, batch, 1)
			if _, err = e.RouteDependencyActivity(context.Background(), signal); err != nil {
				t.Fatal(err)
			}
			if err = taskToolError(e, run, answerUnderstanding(t, e, run, "NEW_FACTS_v3")); err != nil {
				t.Fatal(err)
			}
			old := Observation{Summary: "OLD_FACTS_waiting", Outcome: "no_action"}
			if direct {
				err = store.FinishContinuation(batch.ID, e.View().ID, "completed", jsonText(old), []string{"already-sent"}, ContinuationWork{})
			} else {
				err = store.FinishBatch(batch.ID, old)
			}
			state := store.Snapshot()
			if direct {
				if err != nil || state.Focuses[focus.ID].Summary != "NEW_FACTS_v3" || state.Batches[batch.ID].State != "completed" || len(state.Continuations[batch.ID].ActionIDs) != 1 {
					t.Fatal("direct completion overwrote facts or lost actual action", err, state)
				}
				return
			}
			if err != nil || state.Focuses[focus.ID].Summary != "NEW_FACTS_v3" || state.Batches[batch.ID].State != "superseded" || !state.Events[older.ID].ObserveOnly {
				t.Fatal("late observation overwrote new facts", err, state)
			}
			if direct && len(state.Continuations[batch.ID].ActionIDs) != 1 {
				t.Fatal("already committed action was lost")
			}
			next, err := store.ClaimBatch(e.View().ID, time.Now(), 0, 0)
			if err != nil || next == nil || next.Mode == "direct" || next.Focus.Summary != "NEW_FACTS_v3" {
				t.Fatal("stale input replayed direct actions or missed current facts", next, err)
			}
		})
	}
}

func TestTaskUnderstandingProjectionIsBoundedAndPrivate(t *testing.T) {
	e, _, _, run, signal := understandingFixture(t)
	e.mu.Lock()
	job := e.record.Jobs[run.JobID]
	for i := 0; i < 20; i++ {
		ref := signal.Source
		ref.AnnotationID = string(rune('a' + i))
		job.Dependencies[0].Signals = append(job.Dependencies[0].Signals, DependencySignal{ID: "reply:" + ref.AnnotationID, Source: ref})
	}
	detail := e.taskDetailLocked(job)
	e.mu.Unlock()
	encoded, _ := json.Marshal(detail.Understandings)
	if len(detail.Understandings) != 8 || strings.Contains(string(encoded), "fingerprint") || strings.Contains(string(encoded), "PRIVATE_GOAL") {
		t.Fatal("understanding projection is unbounded or exposes private bookkeeping", string(encoded))
	}
	if len(taskIndex([]Job{detail})[0].Understandings) != 0 {
		t.Fatal("main task index carries all answer facts")
	}
}

func TestTaskUnderstandingQuestionRelationsCommitTogether(t *testing.T) {
	e, _, store, run, signal := understandingFixture(t)
	focus := seedDiscussionUnderstanding(t, store, signal)
	original := spacecontext.SourceRef{SpaceID: signal.Source.SpaceID, AnnotationID: "original-question", ReplyID: "original-reply"}
	e.mu.Lock()
	err := e.mutateLocked(func() error {
		job := e.record.Jobs[run.JobID]
		job.Dependencies[0].QuestionID = "registered-question"
		e.record.Questions["registered-question"] = TaskQuestion{ID: "registered-question", Source: job.Dependencies[0].Source, RespondsTo: []QuestionRelation{{Source: original}}}
		e.record.Jobs[job.ID] = job
		return nil
	})
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.RouteDependencyActivity(context.Background(), signal); err != nil {
		t.Fatal(err)
	}
	args := answerUnderstanding(t, e, run, "v3 answers the original compatibility question")
	updates := args["understandings"].([]TaskUnderstandingUpdate)
	if len(updates) != 2 {
		t.Fatal("registered original question was not included", updates)
	}
	bad := clone(updates)
	bad[len(bad)-1].BaseRevision++
	args["understandings"] = bad
	before := attentionHash(store.Snapshot())
	if err = taskToolError(e, run, args); err == nil || attentionHash(store.Snapshot()) != before {
		t.Fatal("one stale target partially committed a multi-object understanding", err)
	}
	args["understandings"] = updates
	if err = taskToolError(e, run, args); err != nil {
		t.Fatal(err)
	}
	state := store.Snapshot()
	subject, _ := understandingSubject(original)
	if state.Focuses[focus.ID].Revision != focus.Revision+1 || state.Focuses[attentionHash(subject)].Summary == "" || len(state.UnderstoodAnswers) != 2 {
		t.Fatal("actual and original question did not share the reconciled answer", state)
	}
}

func TestTaskUnderstandingOnlyConsumesExplicitAnswerPage(t *testing.T) {
	e, _, store, run, signal := understandingFixture(t)
	e.mu.Lock()
	err := e.mutateLocked(func() error {
		job := e.record.Jobs[run.JobID]
		second := clone(job.Dependencies[0])
		second.ID, second.Source.ReplyID = "second-question", "second-question"
		job.Dependencies = append(job.Dependencies, second)
		for i := 0; i < 17; i++ {
			ref := signal.Source
			ref.ReplyID = "answer-" + string(rune('a'+i))
			dependency := &job.Dependencies[i/16]
			dependency.Signals = append(dependency.Signals, DependencySignal{ID: "reply:" + ref.ReplyID, Source: ref})
		}
		e.record.Jobs[job.ID] = job
		return nil
	})
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	args := answerUnderstanding(t, e, run, "First sixteen answers reconciled; one remains")
	updates := args["understandings"].([]TaskUnderstandingUpdate)
	if len(updates) != 1 || len(updates[0].SignalIDs) != 16 {
		t.Fatal("answer page was not bounded", updates)
	}
	if err = taskToolError(e, run, args); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	pending := e.taskUnderstandingTargetsLocked(e.record.Jobs[run.JobID])
	e.mu.Unlock()
	if len(pending) != 1 || pending[0].AnswerCount != 1 || len(store.Snapshot().UnderstoodAnswers) != 16 {
		t.Fatal("truncated answer page silently consumed unreviewed facts", pending)
	}
	if err := taskToolError(e, run, taskCommandFor(currentJob(t, e, run.JobID), "wait")); err != nil || len(store.Snapshot().UnderstoodAnswers) != 16 {
		t.Fatal("wait must not require or fabricate remaining summary receipts", err)
	}
}
