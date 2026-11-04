package shadow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"teamcross/internal/spacecontext"
)

// A delivery acknowledges storage in a task, not business completion. It also
// keeps routing to an older default shadow explicit after the default changes.
type TaskDelivery struct {
	ShadowID     string `json:"shadowId"`
	JobID        string `json:"jobId"`
	Version      int    `json:"version"`
	DependencyID string `json:"dependencyId"`
	SignalID     string `json:"signalId"`
}

func receiveDependencySignals(d *TaskDependency, signals []DependencySignal) (bool, error) {
	if d.Received == nil {
		d.Received = map[string]bool{}
	}
	// Upgrade records which predate the durable receipt index.
	for _, signal := range d.Signals {
		d.Received[signal.ID] = true
		d.AnswerReceived = d.AnswerReceived || signal.State == "answered"
	}
	for _, id := range d.EvidenceIDs {
		d.Received[id] = true
	}
	added := false
	for _, signal := range signals {
		if d.Received[signal.ID] {
			continue
		}
		if len(d.Received) >= 2048 || len(d.Signals) >= 16 {
			return false, fmt.Errorf("任务答复已达批次或保留边界，来源位置未推进")
		}
		d.Received[signal.ID] = true
		d.Signals = append(d.Signals, clone(signal))
		d.AnswerReceived = d.AnswerReceived || signal.State == "answered"
		added = true
	}
	return added, nil
}

// A partial answer remains observed after its transient signal is reviewed.
// This does not satisfy the dependency or complete the task.
func dependencyAnswerReceived(d TaskDependency) bool {
	if d.AnswerReceived {
		return true
	}
	for _, signal := range d.Signals {
		if signal.State == "answered" {
			return true
		}
	}
	for id := range d.Received {
		if strings.HasPrefix(id, "reply:") {
			return true
		}
	}
	return false
}

func activityDependencyID(signal spacecontext.Signal) string {
	if signal.Kind == "reply" && signal.Source.ReplyID != "" {
		return "reply:" + signal.Source.ReplyID
	}
	if signal.Source.RequestID != "" && strings.HasPrefix(signal.Kind, "request_") {
		return "request:" + signal.Source.RequestID + ":" + strings.TrimPrefix(signal.Kind, "request_") + ":" + signal.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	return ""
}

func dependencyObjectMatches(d TaskDependency, signal spacecontext.Signal) bool {
	a, b := d.Source, signal.Source
	if a.SpaceID != b.SpaceID || a.ExecutionID != b.ExecutionID {
		return false
	}
	if d.Kind == "discussion" && d.QuestionSourcePending && d.QuestionID != "" && signal.Kind == "reply" {
		return b.AnnotationID != "" && (a.AnnotationID == "" || a.AnnotationID == b.AnnotationID)
	}
	return d.Kind == "discussion" && signal.Kind == "reply" && a.AnnotationID != "" && a.AnnotationID == b.AnnotationID || d.Kind == "request" && a.RequestID != "" && a.RequestID == b.RequestID
}

func taskAcceptsAnswers(job Job) bool {
	switch job.State {
	case "waiting", "queued", "running", "needs_followup", "paused":
		return true
	case "stopping":
		return job.StopReason == "follow" && job.Follow != nil && job.Follow.State != "completed"
	}
	return false
}

// RouteDependencyActivity is called before observation/direct dispatch. The
// resolver checks exact reply ancestry in the business object, without a model.
// The job write happens first; a lost attention receipt can safely retry it.
func (e *Engine) RouteDependencyActivity(ctx context.Context, signal spacecontext.Signal) ([]TaskDelivery, error) {
	e.mu.Lock()
	jobs, id, space, resolver := clone(sortedJobs(e.record.Jobs)), e.record.ID, e.record.SpaceID, e.cfg.ResolveDependency
	closed := e.closed
	e.mu.Unlock()
	if closed || signal.Source.SpaceID != space {
		return nil, nil
	}
	out := []TaskDelivery{}
	for _, job := range jobs {
		for _, d := range job.Dependencies {
			if !dependencyObjectMatches(d, signal) {
				continue
			}
			knownID := activityDependencyID(signal)
			known := knownID != "" && d.Received[knownID]
			for _, old := range d.Signals {
				known = known || old.ID == knownID
			}
			for _, old := range d.EvidenceIDs {
				known = known || old == knownID
			}
			if known {
				e.mu.Lock()
				current := e.record.Jobs[job.ID]
				if !e.closed && current.Version == job.Version {
					out = append(out, TaskDelivery{id, job.ID, job.Version, d.ID, knownID})
				}
				e.mu.Unlock()
				continue
			}
			if !taskAcceptsAnswers(job) || d.State == "satisfied" || resolver == nil {
				continue
			}
			answer, err := resolver(ctx, d, signal)
			if err != nil {
				return nil, err
			}
			if answer == nil {
				continue
			}
			if answer.ID == "" || len(answer.ID) > 512 || answer.Source.SpaceID != space || len(answer.Preview) > 4000 {
				return nil, fmt.Errorf("答复路由返回了无效来源")
			}
			e.mu.Lock()
			current, exists := e.record.Jobs[job.ID]
			accepted := false
			if !e.closed && exists && current.Version == job.Version && taskAcceptsAnswers(current) {
				current = clone(current)
				for i := range current.Dependencies {
					now := &current.Dependencies[i]
					if now.ID != d.ID || dependencyIdentity(*now) != dependencyIdentity(d) || now.State == "satisfied" {
						continue
					}
					var added bool
					if answer.QuestionSource != nil {
						if err = e.validateQuestionSourceLocked(now.QuestionID, *answer.QuestionSource); err != nil {
							break
						}
						bindQuestionDependencies(&current, now.QuestionID, *answer.QuestionSource)
					}
					added, err = receiveDependencySignals(now, []DependencySignal{*answer})
					if err != nil {
						break
					}
					accepted = true
					if !added {
						break
					}
					if current.State == "waiting" && !e.taskRunActiveLocked(current.ID) {
						current.State, current.Outcome, current.Error = "queued", "", ""
						current.ResultEventID, current.DeliveredEventID = "", ""
					}
					current.Revision++
					current.UpdatedAt = time.Now()
					err = e.mutateLocked(func() error {
						if answer.QuestionSource != nil {
							if err := e.bindQuestionSourceLocked(now.QuestionID, *answer.QuestionSource); err != nil {
								return err
							}
						}
						e.record.Jobs[current.ID] = current
						return nil
					})
					break
				}
			}
			e.mu.Unlock()
			if err != nil {
				return nil, err
			}
			if accepted {
				e.notify()
				out = append(out, TaskDelivery{id, job.ID, job.Version, d.ID, answer.ID})
			}
		}
	}
	return out, nil
}

func (e *Engine) routeActivity(ctx context.Context, signal spacecontext.Signal) ([]TaskDelivery, error) {
	if e.cfg.RouteActivity != nil {
		return e.cfg.RouteActivity(ctx, signal)
	}
	return e.RouteDependencyActivity(ctx, signal)
}

// DivertBatch removes only signals durably accepted by a task. A partial write
// failure leaves the same batch available; task receipts prevent double wakeup.
func (s *AttentionStore) DivertBatch(id string, deliveries map[string][]TaskDelivery) (*ObservationBatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	batch, ok := s.record.Batches[id]
	if !ok || batch.State != "preparing" && batch.State != "continuing" {
		return nil, errObsoleteBatch
	}
	if len(deliveries) == 0 {
		return ptrBatch(batch), nil
	}
	next := clone(s.record)
	remaining, bodies := []string{}, []SourceSnapshot{}
	for i, eventID := range batch.EventIDs {
		if tasks := deliveries[eventID]; len(tasks) > 0 {
			event := next.Events[eventID]
			event.State, event.TaskDeliveries = "task", clone(tasks)
			next.Events[eventID] = event
			f := next.Focuses[event.FocusID]
			for _, task := range tasks {
				found := false
				for _, jobID := range f.JobIDs {
					found = found || jobID == task.JobID
				}
				if !found {
					f.JobIDs = append(f.JobIDs, task.JobID)
				}
			}
			next.Focuses[f.ID] = f
		} else {
			remaining = append(remaining, eventID)
			if i < len(batch.Sources) {
				bodies = append(bodies, batch.Sources[i])
			}
		}
	}
	batch.EventIDs, batch.Sources = remaining, bodies
	if len(remaining) == 0 {
		batch.State = "task"
		f := next.Focuses[batch.Focus.ID]
		f.Processing, f.Error = "task", ""
		next.Focuses[f.ID] = f
	}
	next.Batches[id] = batch
	if c, ok := next.Continuations[id]; ok && c.Mode == "direct" {
		c.EventIDs = clone(remaining)
		if len(remaining) == 0 {
			c.State = "task"
		}
		next.Continuations[id] = c
	}
	if err := s.saveLocked(next); err != nil {
		return nil, err
	}
	return ptrBatch(batch), nil
}

func (e *Engine) routeBatch(ctx context.Context, store *AttentionStore, batchID string) (*ObservationBatch, error) {
	signals, err := store.BatchSignals(batchID)
	if err != nil {
		return nil, err
	}
	deliveries := map[string][]TaskDelivery{}
	for _, signal := range signals {
		readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		tasks, err := e.routeActivity(readCtx, signal)
		cancel()
		if err != nil {
			return nil, err
		}
		if len(tasks) > 0 {
			deliveries[signal.ID] = tasks
		}
	}
	return store.DivertBatch(batchID, deliveries)
}
