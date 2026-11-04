package shadow

import (
	"fmt"
	"strings"
	"time"
)

// An event source is evidence, not necessarily a continuing delegation. Only
// follow-derived work (or an explicitly associated task) tracks the F goal.
// This binding is private Core state and is omitted from shared projections.
type TaskFollow struct {
	FocusID     string `json:"focusId"`
	GoalVersion uint64 `json:"goalVersion"`
	State       string `json:"state"`
	NeedsReplan bool   `json:"needsReplan,omitempty"`
}

func (s *AttentionStore) taskFocus(id string) (Focus, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.record.Focuses[id]
	return clone(f), ok
}

func (e *Engine) validateTaskFollowLocked(job Job) error {
	if job.Follow == nil {
		return nil
	}
	if e.attention == nil {
		return fmt.Errorf("任务认识尚未连接")
	}
	f, ok := e.attention.taskFocus(job.Follow.FocusID)
	if !ok || f.GoalVersion != job.Follow.GoalVersion || f.State != "active" {
		return fmt.Errorf("关联关注目标已变化或停止，等待当前目标重核")
	}
	return nil
}

func (e *Engine) newTaskFollowLocked(run Run, origin *TaskTrigger, explicitID string) (*TaskFollow, error) {
	id := explicitID
	if explicitID != "" {
		owned := origin.FollowGoal && origin.FocusID == explicitID
		if parent := e.record.Jobs[run.JobID]; parent.Follow != nil && parent.Follow.FocusID == explicitID {
			owned = true
		}
		for _, message := range e.record.Messages {
			if message.ID == origin.MessageID && message.Role == "user" && message.Actor.Kind != "shadow" && message.Actor.ID == e.record.Owner.ID {
				owned = true
			}
		}
		if !owned {
			return nil, fmt.Errorf("其他成员的共享委托使用公开来源，不绑定本人的私有关注目标")
		}
	}
	if id == "" {
		if parent := e.record.Jobs[run.JobID]; parent.Follow != nil {
			id = parent.Follow.FocusID
		} else if origin.FollowGoal {
			id = origin.FocusID
		}
	}
	if id == "" {
		return nil, nil
	}
	if e.attention == nil {
		return nil, fmt.Errorf("任务认识尚未连接")
	}
	f, ok := e.attention.taskFocus(id)
	if !ok || f.State != "active" {
		return nil, fmt.Errorf("关联的关注目标不存在或已经停止")
	}
	return &TaskFollow{FocusID: id, GoalVersion: f.GoalVersion, State: f.State}, nil
}

func (e *Engine) taskControlRunActiveLocked(id string) bool {
	for _, run := range e.record.Runs {
		if run.JobID == id && (run.Kind == "job" || run.Kind == "result") && activeRun(run) {
			return true
		}
	}
	return false
}

func settleTaskFollow(job *Job) {
	job.StopReason, job.Error = "", ""
	switch job.Follow.State {
	case "active":
		job.State, job.Outcome = "queued", ""
	case "completed":
		job.State, job.Outcome = "cancelled", "cancelled"
	default:
		job.State, job.Outcome = "paused", ""
	}
}

// Called inside the engine transaction, including before native terminal
// handling. A late old-goal completion must never outrun scheduler polling.
func (e *Engine) refreshedTaskFollowLocked(job Job) (Job, bool) {
	if job.Follow == nil || job.State == "cancelled" || job.StopReason == "cancel" || e.attention == nil {
		return job, false
	}
	f, ok := e.attention.taskFocus(job.Follow.FocusID)
	if !ok || f.GoalVersion == job.Follow.GoalVersion && f.State == job.Follow.State {
		return job, false
	}
	if job.State == "completed" {
		// Completed business work stays completed at its original version. A
		// queued old-goal announcement cannot become a new-goal completion.
		if job.ResultEventID == "" || job.ResultEventID == job.DeliveredEventID {
			return job, false
		}
		job = clone(job)
		job.ResultEventID, job.DeliveredEventID = "", ""
		job.Revision++
		job.UpdatedAt = time.Now()
		return job, true
	}
	job = clone(job)
	job.Version++
	job.Revision++
	job.UpdatedAt = time.Now()
	job.Follow.GoalVersion, job.Follow.State, job.Follow.NeedsReplan = f.GoalVersion, f.State, true
	job.DecisionRunID, job.Outcome, job.Error = "", "", ""
	job.CompletionChecks = nil
	// The prior result remains evidence with its original ResultVersion. It is
	// no longer an actionable completion notification for the current goal.
	job.ResultEventID, job.DeliveredEventID = "", ""
	if e.taskControlRunActiveLocked(job.ID) {
		job.State, job.StopReason = "stopping", "follow"
	} else {
		settleTaskFollow(&job)
	}
	return job, true
}

func (e *Engine) syncTaskFollows() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	updates := map[string]Job{}
	for id, job := range e.record.Jobs {
		next, followChanged := e.refreshedTaskFollowLocked(job)
		next, contextChanged := e.refreshedTaskContextsLocked(next)
		if followChanged || contextChanged {
			updates[id] = next
		}
	}
	if len(updates) == 0 {
		return nil
	}
	return e.mutateLocked(func() error {
		for id, job := range updates {
			e.record.Jobs[id] = job
		}
		return nil
	})
}

func (e *Engine) taskFollowPromptLocked(job Job) string {
	if job.Follow == nil || e.attention == nil {
		return ""
	}
	f, ok := e.attention.taskFocus(job.Follow.FocusID)
	if !ok {
		return ""
	}
	packet := map[string]any{"id": f.ID, "goal": f.Goal, "goalVersion": f.GoalVersion, "goalSourceId": f.GoalSourceID, "state": f.State, "summary": clip(f.Summary, 6000), "evidence": f.Evidence, "openQuestions": f.OpenQuestions}
	text := "\nPrivate follow context for this task; do not publish the owner's follow reasons or this internal packet:\n" + jsonText(packet)
	if job.Follow.NeedsReplan {
		var previous Run
		for _, run := range e.record.Runs {
			if run.JobID == job.ID && run.Kind == "job" && run.JobVersion < job.Version && !activeRun(run) && run.CreatedAt.After(previous.CreatedAt) {
				previous = run
			}
		}
		if previous.Output != "" {
			text += "\nPrevious native output is evidence at its original goal version, not completion of this goal:\n" + jsonText(map[string]any{"jobVersion": previous.JobVersion, "state": previous.State, "output": clip(previous.Output, 4000)})
		}
		text += "\nThe follow goal changed and the previous native turn has stopped. Before doing further work, read the task status and call shadow_task replan with its current version/revision, revised prompt, explicit completionCriteria, checkpoint, and retainDependencyIds (empty if none still apply). Review the saved evidence, previous result and completed actions; keep dependencies that remain relevant. This revises the same task and keeps this exact worker thread. Do not finish against the previous criteria or repeat completed actions. If the goal no longer calls for this task, cancel it. No routine user approval is needed."
	}
	return text
}

func (e *Engine) replanTaskLocked(run Run, job Job, in taskLifecycleInput) (Job, error) {
	if job.Follow == nil || !job.Follow.NeedsReplan || run.Kind != "job" || run.JobID != job.ID || run.JobVersion != job.Version {
		return Job{}, fmt.Errorf("只有当前任务工作轮能重核变化的关注目标")
	}
	if strings.TrimSpace(in.Prompt) == "" || len(in.Prompt) > 24<<10 || len(in.CompletionCriteria) == 0 || len(in.CompletionCriteria) > 16 || in.RetainDependencyIDs == nil || len(*in.RetainDependencyIDs) > 8 {
		return Job{}, fmt.Errorf("目标重核需要有界的新要求、完成条件和保留依赖 ID 列表")
	}
	for _, criterion := range in.CompletionCriteria {
		if strings.TrimSpace(criterion) == "" || len(criterion) > 2400 {
			return Job{}, fmt.Errorf("完成条件为空或过长")
		}
	}
	retained := map[string]bool{}
	for _, id := range *in.RetainDependencyIDs {
		if retained[id] {
			return Job{}, fmt.Errorf("保留依赖 ID 重复")
		}
		retained[id] = true
	}
	dependencies := []TaskDependency{}
	for _, d := range job.Dependencies {
		if retained[d.ID] {
			dependencies = append(dependencies, clone(d))
			delete(retained, d.ID)
		}
	}
	if len(retained) != 0 {
		return Job{}, fmt.Errorf("保留依赖 ID 不属于当前任务")
	}
	job = clone(job)
	job.Prompt, job.CompletionCriteria = in.Prompt, clone(in.CompletionCriteria)
	job.Checkpoint, job.Dependencies, job.CompletionChecks = clone(in.Checkpoint), dependencies, nil
	job.Follow.NeedsReplan = false
	job.DecisionRunID, job.Outcome = "", ""
	job.Revision++
	job.UpdatedAt = time.Now()
	e.record.Jobs[job.ID] = job
	return taskForContext(job), nil
}
