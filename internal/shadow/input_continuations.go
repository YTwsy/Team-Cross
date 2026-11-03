package shadow

import (
	"fmt"
	"time"
)

// Ordinary discussion inputs only acknowledge their native processing and
// actual public actions. A free-form final never acts as a task verdict or a
// required understanding JSON document.
func (e *Engine) flushInputContinuations() error {
	e.mu.Lock()
	store, shadowID := e.attention, e.record.ID
	var runs []Run
	if store != nil {
		for _, run := range e.record.Runs {
			if len(run.InputBatches) > 0 && !activeRun(run) {
				runs = append(runs, clone(run))
			}
		}
	}
	e.mu.Unlock()
	for _, run := range runs {
		for _, input := range runInputs(run) {
			if input.Kind != "continuation" || input.Committed {
				continue
			}
			e.mu.Lock()
			var actions, questions []string
			jobs := clone(input.TaskIDs)
			for id, action := range e.record.Actions {
				if action.EventID == input.EventID {
					actions = append(actions, id)
				}
			}
			for id, job := range e.record.Jobs {
				if job.Trigger != nil && job.Trigger.ContinuationID == input.EventID {
					jobs = appendUniqueLinks(jobs, []string{id})
				}
			}
			for id, q := range e.record.Questions {
				if q.EventID == input.EventID {
					questions = append(questions, id)
				}
			}
			e.mu.Unlock()
			if err := store.finishMessageInput(input.EventID, shadowID, run.State, actions, jobs, questions); err != nil {
				return err
			}
			e.mu.Lock()
			err := e.mutateLocked(func() error {
				current := e.record.Runs[run.ID]
				for b := range current.InputBatches {
					for i := range current.InputBatches[b].Inputs {
						if current.InputBatches[b].Inputs[i].ID == input.ID {
							current.InputBatches[b].Inputs[i].Committed = true
						}
					}
				}
				current.AttentionCommitted = true
				for _, in := range runInputs(current) {
					if in.Kind == "continuation" && !in.Committed {
						current.AttentionCommitted = false
					}
				}
				e.record.Runs[run.ID] = current
				return nil
			})
			e.mu.Unlock()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *AttentionStore) finishMessageInput(id, shadowID, state string, actions, jobs, questions []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.record.Continuations[id]
	if !ok || c.ShadowID != shadowID {
		return fmt.Errorf("讨论输入的接续归属不匹配")
	}
	if c.ResultHash != "" {
		return nil
	}
	next := clone(s.record)
	c.State, c.Error = "processed", ""
	if state != "completed" {
		c.State, c.Error = "failed", "原生输入处理未正常结束；保留已发送动作，不自动重放"
	}
	if !continuationFollowEligible(next, c) {
		c.State = "superseded"
	}
	c.Result = "消息处理轮次已结束；实际公开回复见行动回执，明确工作另有状态。"
	c.ResultHash, c.ActionIDs, c.UpdatedAt = attentionHash([]any{id, state}), clone(actions), time.Now()
	next.Continuations[id] = c
	batch := next.Batches[c.BatchID]
	batch.State, batch.ResultHash, batch.Error, batch.Sources = "completed", c.ResultHash, c.Error, nil
	if c.State == "failed" {
		batch.State = "failed"
	}
	next.Batches[batch.ID] = batch
	focus := next.Focuses[c.FocusID]
	previousError, previousRetry := focus.Error, focus.RetryAfter
	if previousError == "" {
		previousError, previousRetry = batch.Focus.Error, batch.Focus.RetryAfter
	}
	focus.Processing, focus.Error, focus.RetryAfter = "idle", "", time.Time{}
	focus.ActionIDs = appendUniqueLinks(focus.ActionIDs, actions)
	focus.JobIDs = appendUniqueLinks(focus.JobIDs, jobs)
	focus.QuestionIDs = appendUniqueLinks(focus.QuestionIDs, questions)
	for _, id := range c.EventIDs {
		event := next.Events[id]
		event.State = "processed"
		if next.Handling[questionSourceID(event.Signal.Source)].Handler == "owner" {
			event.State = "owner"
		}
		next.Events[id] = event
		focus.ProcessedSeq = max(focus.ProcessedSeq, event.Signal.Seq)
	}
	for _, event := range next.Events {
		if event.FocusID == focus.ID && event.State == "queued" {
			focus.Processing = "queued"
			// Handling a new request does not repair or replay an older
			// failed observation. Keep its error until explicitly retried.
			if previousError != "" {
				focus.Processing, focus.Error, focus.RetryAfter = "error", previousError, previousRetry
			}
		}
	}
	focus.UpdatedAt = time.Now()
	next.Focuses[focus.ID] = focus
	return s.saveLocked(next)
}
