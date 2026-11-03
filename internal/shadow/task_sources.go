package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"teamcross/internal/spacecontext"
)

// Source discovery and task understanding have separate durable positions.
// Only this small query goes to the host; private goals and facts stay local.
type TaskSourceCursor struct {
	Seq         uint64 `json:"seq"`
	Retention   uint64 `json:"retention"`
	Initialized bool   `json:"initialized"`
}

type TaskSourceQuery struct {
	Subject spacecontext.Subject `json:"subject"`
	Cursor  TaskSourceCursor     `json:"cursor"`
}

type TaskSourceUpdate struct {
	Cursor  TaskSourceCursor   `json:"cursor"`
	Signals []DependencySignal `json:"signals"`
	Resync  bool               `json:"resync,omitempty"`
}

type TaskSourceState struct {
	Cursor     TaskSourceCursor   `json:"cursor"`
	Signals    []DependencySignal `json:"signals,omitempty"`
	Error      string             `json:"error,omitempty"`
	HistoryGap *TaskSourceGap     `json:"historyGap,omitempty"`
}

// A retention resync does not prove that the skipped interval was reviewed.
// Keep that qualification after its pending source page has been reconciled.
type TaskSourceGap struct {
	AfterSeq   uint64 `json:"afterSeq"`
	ThroughSeq uint64 `json:"throughSeq"`
	Retention  uint64 `json:"retention"`
}

func sourceQuery(c TaskContext) TaskSourceQuery {
	q := TaskSourceQuery{Subject: c.Subject, Cursor: TaskSourceCursor{Seq: c.Current.ProcessedSeq}}
	if c.Source != nil {
		q.Cursor = c.Source.Cursor
	}
	return q
}

func sourceSignalMatches(subject spacecontext.Subject, source spacecontext.SourceRef) bool {
	switch subject.Kind {
	case "material":
		return source.MaterialID == subject.ID
	case "discussion":
		return source.AnnotationID == subject.ID && source.ExecutionID == subject.ExecutionID
	case "execution":
		return source.ExecutionID == subject.ID
	}
	return false
}

func contextSourceInput(c TaskContext) string {
	if c.Source == nil || len(c.Source.Signals) == 0 {
		return ""
	}
	return attentionHash(c.Source.Signals)
}

func (e *Engine) pendingSourceSignalsLocked(c TaskContext) []DependencySignal {
	if c.Source == nil || e.attention == nil {
		return nil
	}
	e.attention.mu.Lock()
	defer e.attention.mu.Unlock()
	out := []DependencySignal{}
	for _, signal := range c.Source.Signals {
		if !understoodAnswer(e.attention.record, c.ID, signal) {
			out = append(out, signal)
		}
	}
	return out
}

// Poll exact registered sources without a model. One pending page per context
// bounds storage; reading resumes after its facts have been reconciled.
func (e *Engine) refreshTaskSources(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || !e.record.Enabled || e.attention == nil || e.cfg.ReadTaskSource == nil {
		return
	}
	if e.sourceReading == nil {
		e.sourceReading, e.sourceNext = map[string]bool{}, map[string]time.Time{}
	}
	interval := e.cfg.DependencyPollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	type candidate struct {
		job     Job
		context TaskContext
		key     string
	}
	candidates := []candidate{}
	liveSources := map[string]bool{}
	now := time.Now()
	for _, job := range e.record.Jobs {
		if !taskAcceptsAnswers(job) || job.State == "paused" || job.State == "stopping" {
			continue
		}
		for _, c := range job.Contexts {
			key := job.ID + ":" + c.ID
			if c.Mode != "snapshot" {
				liveSources[key] = true
			}
			if c.Mode == "snapshot" || e.sourceReading[key] || now.Before(e.sourceNext[key]) || len(e.pendingSourceSignalsLocked(c)) > 0 {
				continue
			}
			candidates = append(candidates, candidate{job, c, key})
		}
	}
	for key := range e.sourceNext {
		if !liveSources[key] && !e.sourceReading[key] {
			delete(e.sourceNext, key)
		}
	}
	// Completed reads get a new deadline. Old/unvisited sources precede recent
	// ones, including when a slow source takes longer than the polling interval.
	sort.Slice(candidates, func(i, j int) bool {
		a, b := e.sourceNext[candidates[i].key], e.sourceNext[candidates[j].key]
		if a.Equal(b) {
			return candidates[i].key < candidates[j].key
		}
		return a.Before(b)
	})
	for _, candidate := range candidates {
		if len(e.sourceReading) >= 4 {
			break
		}
		job, c, key := candidate.job, candidate.context, candidate.key
		e.sourceReading[key] = true
		e.dependencyWait.Add(1)
		go func() {
			defer e.dependencyWait.Done()
			readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			update, err := e.cfg.ReadTaskSource(readCtx, sourceQuery(c))
			cancel()
			e.mu.Lock()
			defer e.mu.Unlock()
			delete(e.sourceReading, key)
			e.sourceNext[key] = time.Now().Add(interval)
			if ctx.Err() != nil || e.closed {
				return
			}
			if saveErr := e.applyTaskSourceLocked(job.ID, job.Version, c, update, err); saveErr != nil {
				e.record.Error = "保存任务来源变化失败：" + saveErr.Error()
			}
			e.notify()
		}()
	}
}

func (e *Engine) applyTaskSourceLocked(jobID string, version int, before TaskContext, update TaskSourceUpdate, readErr error) error {
	job, exists := e.record.Jobs[jobID]
	if !exists || job.Version != version || !taskAcceptsAnswers(job) || job.State == "paused" || job.State == "stopping" {
		return nil
	}
	job = clone(job)
	index := -1
	for i, c := range job.Contexts {
		if c.ID == before.ID && c.Mode != "snapshot" && sourceQuery(c) == sourceQuery(before) {
			index = i
			break
		}
	}
	if index < 0 {
		return nil
	}
	c := &job.Contexts[index]
	if c.Source == nil {
		c.Source = &TaskSourceState{Cursor: sourceQuery(before).Cursor}
	}
	if readErr == nil {
		encoded, err := json.Marshal(update)
		if err != nil || len(encoded) > 32<<10 || len(update.Signals) > 16 || !update.Cursor.Initialized || update.Cursor.Seq < c.Source.Cursor.Seq || len(update.Signals) > 0 && update.Cursor == c.Source.Cursor {
			readErr = fmt.Errorf("任务来源返回无效或过大的增量")
		}
		seen := map[string]bool{}
		for _, signal := range update.Signals {
			if signal.ID == "" || len(signal.ID) > 512 || seen[signal.ID] || signal.Source.SpaceID != e.record.SpaceID || !sourceSignalMatches(c.Subject, signal.Source) {
				readErr = fmt.Errorf("任务来源变化不属于登记对象")
			}
			seen[signal.ID] = true
		}
	}
	if readErr != nil {
		message := clip(readErr.Error(), 1000)
		if c.Source.Error == message {
			return nil
		}
		c.Source.Error = message
	} else {
		if update.Resync {
			c.Source.HistoryGap = &TaskSourceGap{AfterSeq: c.Source.Cursor.Seq, ThroughSeq: update.Cursor.Seq, Retention: update.Cursor.Retention}
		}
		c.Source.Cursor, c.Source.Error = update.Cursor, ""
		c.Source.Signals = clone(update.Signals)
		c.Source.Signals = e.pendingSourceSignalsLocked(*c)
		if len(c.Source.Signals) > 0 && !e.taskRunActiveLocked(job.ID) && (job.State == "waiting" || job.State == "needs_followup") {
			job.State, job.Outcome, job.ResultEventID, job.DeliveredEventID = "queued", "", "", ""
		}
	}
	if jsonText(job.Contexts[index]) == jsonText(e.record.Jobs[jobID].Contexts[index]) {
		return nil
	}
	job.Revision++
	job.UpdatedAt = time.Now()
	return e.mutateLocked(func() error { e.record.Jobs[job.ID] = job; return nil })
}
