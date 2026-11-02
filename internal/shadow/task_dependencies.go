package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

func (e *Engine) taskRunActiveLocked(jobID string) bool {
	for _, run := range e.record.Runs {
		if run.Kind == "job" && run.JobID == jobID && activeRun(run) {
			return true
		}
	}
	return false
}

// Waiting is a durable business state. Poll only the explicitly recorded
// objects; this uses no model turn and is independent of human activity reads.
// Bounded concurrent reads never hold up foreground messages or native events.
func (e *Engine) refreshTaskDependencies(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || !e.record.Enabled {
		return
	}
	if e.dependencyReading == nil {
		e.dependencyReading = map[string]bool{}
		e.dependencyNext = map[string]time.Time{}
	}
	interval := e.cfg.DependencyPollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	now := time.Now()
	for _, job := range sortedJobs(e.record.Jobs) {
		if job.State != "waiting" || e.taskRunActiveLocked(job.ID) {
			continue
		}
		for _, dependency := range job.Dependencies {
			key := job.ID + ":" + dependency.ID
			if dependency.State == "satisfied" || e.dependencyReading[key] || now.Before(e.dependencyNext[key]) {
				continue
			}
			if len(e.dependencyReading) >= 4 {
				return
			}
			if dependency.Kind == "time" && dependency.DueAt != nil && now.Before(*dependency.DueAt) {
				continue
			}
			var local *DependencyUpdate
			if dependency.Kind == "job" {
				update := DependencyUpdate{Cursor: dependency.Cursor}
				child, ok := e.record.Jobs[dependency.JobID]
				if ok && (child.Version != dependency.JobVersion || child.ResultEventID != "") {
					key := fmt.Sprintf("job:%s:%d:%s", child.ID, child.Version, child.ResultEventID)
					if key != dependency.Cursor {
						state := child.State
						if child.Version != dependency.JobVersion {
							state = "superseded"
						}
						update = DependencyUpdate{Cursor: key, Signals: []DependencySignal{{ID: key, JobID: child.ID, JobVersion: child.Version, State: state, Preview: clip(child.Result, 1000)}}}
					}
				}
				local = &update
			} else if dependency.Kind == "time" {
				key := "time:" + dependency.DueAt.UTC().Format(time.RFC3339Nano)
				update := DependencyUpdate{Cursor: dependency.Cursor}
				if key != dependency.Cursor {
					update = DependencyUpdate{Cursor: key, Signals: []DependencySignal{{ID: key, State: "due", Preview: dependency.Condition}}}
				}
				local = &update
			} else if e.cfg.ReadDependency == nil {
				continue
			}
			e.dependencyReading[key], e.dependencyNext[key] = true, now.Add(interval)
			e.dependencyWait.Add(1)
			go func(job Job, dependency TaskDependency, key string, local *DependencyUpdate) {
				defer e.dependencyWait.Done()
				var update DependencyUpdate
				var err error
				if local != nil {
					update = *local
				} else {
					readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
					update, err = e.cfg.ReadDependency(readCtx, dependency)
					cancel()
				}
				e.mu.Lock()
				defer e.mu.Unlock()
				delete(e.dependencyReading, key)
				if ctx.Err() != nil || e.closed {
					return
				}
				if saveErr := e.applyDependencyUpdateLocked(job.ID, job.Version, dependency, update, err); saveErr != nil {
					// Persistence failures do not consume the source cursor. Report
					// them locally and retry the same exact object on a later check.
					e.record.Error = "保存任务答复失败：" + saveErr.Error()
				}
				e.notify()
			}(clone(job), clone(dependency), key, local)
		}
	}
}

func (e *Engine) applyDependencyUpdateLocked(jobID string, version int, before TaskDependency, update DependencyUpdate, readErr error) error {
	job, exists := e.record.Jobs[jobID]
	if !exists || job.Version != version || job.State != "waiting" || e.taskRunActiveLocked(job.ID) {
		return nil
	}
	job = clone(job)
	index := -1
	for i, d := range job.Dependencies {
		if d.ID == before.ID && d.State != "satisfied" && d.Cursor == before.Cursor && dependencyIdentity(d) == dependencyIdentity(before) {
			index = i
			break
		}
	}
	if index < 0 {
		return nil
	}
	d := &job.Dependencies[index]
	bindSource := false
	if readErr == nil {
		encoded, err := json.Marshal(update)
		if err != nil || len(encoded) > 32<<10 || len(update.Signals) > 16 || len(update.Cursor) > 2000 || len(update.Signals) > 0 && (update.Cursor == "" || update.Cursor == before.Cursor) {
			readErr = fmt.Errorf("等待来源返回了无效或过大的增量")
		}
		seen := map[string]bool{}
		for _, signal := range update.Signals {
			if signal.ID == "" || len(signal.ID) > 512 || seen[signal.ID] || signal.Source.SpaceID != "" && signal.Source.SpaceID != e.record.SpaceID {
				readErr = fmt.Errorf("等待来源信号身份无效")
			}
			seen[signal.ID] = true
		}
		if update.QuestionSource != nil {
			if err := e.validateQuestionSourceLocked(d.QuestionID, *update.QuestionSource); err != nil {
				readErr = err
			} else {
				bindSource = true
			}
		}
	}
	if readErr != nil {
		if d.Error == readErr.Error() {
			return nil
		}
		d.Error = readErr.Error()
	} else {
		if d.Cursor == update.Cursor && d.Error == "" && len(update.Signals) == 0 && !bindSource {
			return nil
		}
		if bindSource {
			bindQuestionDependencies(&job, d.QuestionID, *update.QuestionSource)
		}
		added, err := receiveDependencySignals(d, update.Signals)
		if err != nil {
			return err
		}
		d.Cursor, d.Error = update.Cursor, ""
		if added {
			job.State, job.Outcome, job.Error, job.ResultEventID, job.DeliveredEventID = "queued", "", "", "", ""
		}
	}
	job.Revision++
	job.UpdatedAt = time.Now()
	return e.mutateLocked(func() error {
		if readErr == nil && bindSource {
			if err := e.bindQuestionSourceLocked(d.QuestionID, *update.QuestionSource); err != nil {
				return err
			}
		}
		e.record.Jobs[job.ID] = job
		return nil
	})
}

func (e *Engine) finishTaskRunLocked(run Run, status string) {
	job := e.record.Jobs[run.JobID]
	if job.ThreadID != run.ThreadID {
		return
	}
	job, _ = e.refreshedTaskContextsLocked(job)
	job.LastRunState, job.LastRunError = run.State, run.Error
	if job.State == "stopping" {
		if job.StopReason == "follow" && job.Follow != nil {
			if e.taskControlRunActiveLocked(job.ID) {
				return
			}
			settleTaskFollow(&job)
		} else if job.StopReason == "update" {
			job.State = "queued"
		} else {
			job.State = "cancelled"
			job.Outcome = "cancelled"
		}
	} else if job.Version != run.JobVersion {
		return
	} else if job.DecisionRunID != run.ID {
		// A final response is progress, not proof of business completion. The
		// coordinator can resume it or finish with explicit criteria checks.
		job.State, job.Outcome = "needs_followup", "needs_followup"
		job.Result, job.ResultVersion, job.Error = run.Output, run.JobVersion, run.Error
		if run.State != "completed" {
			job.State, job.Outcome = "failed", "failed"
		}
		if status == "interrupted" {
			job.State = "interrupted"
		}
	}
	// An answer can arrive after wait was saved but before the worker emits
	// its native terminal event. It belongs to the next turn of this same job.
	if unseenTaskContext(job, run) && (job.State == "waiting" || job.State == "needs_followup" && run.State == "completed") {
		job.State, job.Outcome = "queued", ""
		job.ResultEventID, job.DeliveredEventID = "", ""
	}
	if job.State == "waiting" {
		for _, d := range job.Dependencies {
			if d.State != "satisfied" && len(d.Signals) > 0 {
				job.State, job.Outcome = "queued", ""
				break
			}
		}
	}
	if job.ResultEventID == "" && job.State != "queued" && job.State != "waiting" && job.State != "paused" && !(job.State == "cancelled" && job.Follow != nil && job.Follow.State == "completed") {
		job.ResultEventID = "task-result:" + run.ID
	}
	job.Revision++
	job.UpdatedAt = time.Now()
	e.record.Jobs[job.ID] = job
}
