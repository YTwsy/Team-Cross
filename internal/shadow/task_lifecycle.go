package shadow

import (
	"fmt"
	"strings"
	"time"

	"teamcross/internal/spacecontext"
)

type TaskCheckpoint struct {
	Summary       string                   `json:"summary"`
	NextStep      string                   `json:"nextStep"`
	Evidence      []spacecontext.SourceRef `json:"evidence,omitempty"`
	OpenQuestions []string                 `json:"openQuestions,omitempty"`
}

// Cursor acknowledges durable source discovery, not that an answer satisfies
// the task. Only the working turn can resolve a dependency with cited signals.
type TaskDependency struct {
	QuestionID            string                 `json:"questionId,omitempty"`
	QuestionActionID      string                 `json:"questionActionId,omitempty"`
	QuestionTool          string                 `json:"questionTool,omitempty"`
	QuestionSourcePending bool                   `json:"questionSourcePending,omitempty"`
	ID                    string                 `json:"id"`
	Kind                  string                 `json:"kind"` // discussion, request, job, time
	Condition             string                 `json:"condition"`
	Source                spacecontext.SourceRef `json:"source,omitempty"`
	JobID                 string                 `json:"jobId,omitempty"`
	JobVersion            int                    `json:"jobVersion,omitempty"`
	DueAt                 *time.Time             `json:"dueAt,omitempty"`
	Cursor                string                 `json:"cursor,omitempty"`
	State                 string                 `json:"state"` // waiting, satisfied
	Signals               []DependencySignal     `json:"signals,omitempty"`
	Received              map[string]bool        `json:"received,omitempty"`
	AnswerReceived        bool                   `json:"answerReceived,omitempty"`
	Resolution            string                 `json:"resolution,omitempty"`
	EvidenceIDs           []string               `json:"evidenceIds,omitempty"`
	Error                 string                 `json:"error,omitempty"`
}

type DependencySignal struct {
	QuestionSource *spacecontext.SourceRef `json:"questionSource,omitempty"`
	ID             string                  `json:"id"`
	Source         spacecontext.SourceRef  `json:"source,omitempty"`
	Actor          spacecontext.Actor      `json:"actor,omitempty"`
	State          string                  `json:"state,omitempty"`
	Preview        string                  `json:"preview,omitempty"`
	JobID          string                  `json:"jobId,omitempty"`
	JobVersion     int                     `json:"jobVersion,omitempty"`
}

type DependencyUpdate struct {
	QuestionSource *spacecontext.SourceRef `json:"questionSource,omitempty"`
	Cursor         string                  `json:"cursor"`
	Signals        []DependencySignal      `json:"signals"`
}

type DependencyResolution struct {
	ID          string   `json:"id"`
	Explanation string   `json:"explanation"`
	SignalIDs   []string `json:"signalIds"`
}

type taskLifecycleInput struct {
	Prompt              string                 `json:"prompt"`
	CompletionCriteria  []string               `json:"completionCriteria"`
	RetainDependencyIDs *[]string              `json:"retainDependencyIds"`
	RelatedFocuses      *[]TaskContextInput    `json:"relatedFocuses"`
	ContextReviews      []TaskContextReview    `json:"contextReviews"`
	Operation           string                 `json:"operation"`
	ID                  string                 `json:"id"`
	BaseVersion         int                    `json:"baseVersion"`
	BaseRevision        uint64                 `json:"baseRevision"`
	Checkpoint          *TaskCheckpoint        `json:"checkpoint"`
	Dependencies        []TaskDependency       `json:"dependencies"`
	Resolutions         []DependencyResolution `json:"resolutions"`
	Outcome             string                 `json:"outcome"`
	Result              string                 `json:"result"`
	CriteriaChecks      []string               `json:"criteriaChecks"`
}

func taskSourceSchema() map[string]any {
	str := map[string]any{"type": "string"}
	return map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"spaceId": str, "executionId": str, "materialId": str, "annotationId": str, "replyId": str, "requestId": str,
		"turnId": str, "version": map[string]any{"type": "integer", "minimum": 0},
		"briefRevision": map[string]any{"type": "integer", "minimum": 1}, "briefItemId": str,
		"shadowId": str, "shadowMessageId": str, "shadowResultId": str,
		"itemIds": map[string]any{"type": "array", "items": str, "maxItems": 32},
	}}
}

func taskToolProperties() map[string]any {
	str := map[string]any{"type": "string"}
	num := map[string]any{"type": "integer", "minimum": 0}
	stringsArray := map[string]any{"type": "array", "items": str, "maxItems": 16}
	dependency := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"id", "kind", "condition"}, "properties": map[string]any{
		"id": str, "kind": map[string]any{"type": "string", "enum": []string{"discussion", "request", "job", "time"}},
		"condition": str, "source": taskSourceSchema(), "jobId": str, "jobVersion": num, "questionId": str,
		"dueAt": map[string]any{"type": "string", "description": "An explicitly requested check time in RFC3339, including timezone."},
	}}
	checkpoint := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"summary", "nextStep"}, "properties": map[string]any{
		"summary": str, "nextStep": str, "evidence": map[string]any{"type": "array", "maxItems": 32, "items": taskSourceSchema()}, "openQuestions": stringsArray,
	}}
	resolution := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"id", "explanation", "signalIds"}, "properties": map[string]any{
		"id": str, "explanation": str, "signalIds": stringsArray,
	}}
	understanding := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"id", "baseRevision", "signalIds", "summary", "evidence", "openQuestions"}, "properties": map[string]any{
		"id": str, "baseRevision": num, "signalIds": stringsArray, "summary": str, "evidence": map[string]any{"type": "array", "maxItems": 32, "items": taskSourceSchema()}, "openQuestions": stringsArray,
	}}
	return map[string]any{
		"operation": str, "id": str, "baseVersion": num, "baseRevision": num, "title": str, "prompt": str,
		"source": taskSourceSchema(), "requestId": str, "handler": map[string]any{"type": "string", "enum": []string{"owner", "shadow"}},
		"followId":            map[string]any{"type": "string", "description": "On start, explicitly associate this work with an existing private follow goal. Ordinary observation work and child tasks inherit their goal binding; direct mentions and independent member requests do not."},
		"retainDependencyIds": stringsArray,
		"relatedFocuses":      map[string]any{"type": "array", "maxItems": 8, "description": "On start or checkpoint/wait/finish, the complete desired list of existing focus IDs whose facts this task uses. Each needs a concrete reason. Omit to preserve links; an empty array removes links. This does not bind the task to those follow goals.", "items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"id", "reason"}, "properties": map[string]any{"id": str, "reason": str, "mode": map[string]any{"type": "string", "enum": []string{"live", "snapshot"}, "description": "live (default) keeps this task informed of exact source changes independently of ordinary follows. snapshot freezes the currently linked facts and fixed evidence; later changes do not alter that scope."}}}},
		"contextReviews":      map[string]any{"type": "array", "maxItems": 8, "items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"id", "revision", "explanation"}, "properties": map[string]any{"id": str, "revision": num, "explanation": str}}},
		"understandings":      map[string]any{"type": "array", "maxItems": 8, "items": understanding, "description": "With operation understand, reconcile the pending answer targets returned by task status into their current understanding. Supply id/baseVersion/baseRevision and a stable requestId. Preserve still-valid facts, update answered and remaining questions, and cite actual sources. Each target uses its own current revision. This saves facts and answer receipts atomically, without completing the task."},
		"question": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"tool", "arguments", "condition"}, "properties": map[string]any{
			"respondsTo": map[string]any{"type": "array", "maxItems": 8, "items": taskSourceSchema(), "description": "Exact earlier discussion questions this outgoing question advances. Required when opening a new discussion from work with multiple original questions. An explicit empty array means an independent question. This relation is saved across turns; the actual reply target always remains related."},
			"tool":       map[string]any{"type": "string", "enum": []string{"reply_to_annotation", "add_annotation"}},
			"arguments":  map[string]any{"type": "object", "description": "Exactly the arguments of the named existing collaboration tool, including stable requestId, text, mentions and precise replyTo where needed. The creating Core validates its normal business schema."},
			"condition":  str,
		}},
		"kind": map[string]any{"type": "string", "enum": []string{"work", "coordination"}}, "completionCriteria": stringsArray,
		"checkpoint": checkpoint, "dependencies": map[string]any{"type": "array", "maxItems": 8, "items": dependency},
		"resolutions": map[string]any{"type": "array", "maxItems": 8, "items": resolution},
		"outcome":     map[string]any{"type": "string", "enum": []string{"completed", "needs_input", "needs_followup", "failed"}}, "result": str, "criteriaChecks": stringsArray,
	}
}

// Keep the default model contract focused on explicit Work. Advanced fact
// maintenance has its own store and is not a prerequisite or a common action.
func publicTaskProperties() map[string]any {
	all := taskToolProperties()
	keep := []string{"operation", "id", "baseVersion", "title", "prompt", "completionCriteria", "checkpoint", "dependencies", "resolutions", "criteriaChecks", "result", "source", "requestId", "handler", "followId", "retainDependencyIds"}
	out := map[string]any{}
	for _, key := range keep {
		out[key] = all[key]
	}
	out["operation"] = map[string]any{"type": "string", "enum": []string{"start", "report", "status", "update", "cancel", "resume", "handling", "handoff", "replan"}}
	out["baseRevision"] = map[string]any{"type": "integer", "minimum": 0, "description": "Only for explicit handling/replan concurrency. W report binds its current revision automatically."}
	out["state"] = map[string]any{"type": "string", "enum": []string{"continue", "wait", "complete", "fail"}, "description": "Required for report."}
	out["publishedActionId"] = map[string]any{"type": "string", "description": "For report only: saved discussion reply action from this Work/version covering the original question; reuse it instead of sending the answer again."}
	return out
}

func validateCheckpoint(cp *TaskCheckpoint, spaceID string) error {
	if cp == nil || strings.TrimSpace(cp.Summary) == "" || len(cp.Summary) > 8000 || len(cp.NextStep) > 4000 || len(cp.OpenQuestions) > 16 || len(cp.Evidence) > 32 {
		return fmt.Errorf("需要有界的进展、下一步和证据检查点")
	}
	for _, q := range cp.OpenQuestions {
		if len(q) > 2000 {
			return fmt.Errorf("未决问题过长")
		}
	}
	for i := range cp.Evidence {
		if cp.Evidence[i].SpaceID == "" {
			cp.Evidence[i].SpaceID = spaceID
		}
		if cp.Evidence[i].SpaceID != spaceID {
			return fmt.Errorf("检查点证据不属于本空间")
		}
	}
	return nil
}

func dependencyIdentity(d TaskDependency) string {
	if d.QuestionID != "" {
		return attentionHash([]any{d.Kind, d.QuestionID})
	}
	return attentionHash([]any{d.Kind, d.Source, d.JobID, d.JobVersion, d.DueAt})
}

func (e *Engine) validateDependencyLocked(job Job, d *TaskDependency) error {
	if d.ID == "" || len(d.ID) > 160 || strings.TrimSpace(d.Condition) == "" || len(d.Condition) > 2400 {
		return fmt.Errorf("等待项需要稳定 ID 和明确答复条件")
	}
	if d.QuestionID != "" {
		q, ok := e.record.Questions[d.QuestionID]
		if !ok || d.Kind != "discussion" {
			return fmt.Errorf("等待项引用的提问不存在")
		}
		d.Source, d.QuestionTool, d.QuestionActionID, d.QuestionSourcePending = clone(q.Source), q.Tool, q.ActionID, q.Delivery != "saved"
	} else {
		d.QuestionTool, d.QuestionActionID, d.QuestionSourcePending = "", "", false
	}
	switch d.Kind {
	case "discussion", "request":
		if d.Source.SpaceID == "" {
			d.Source.SpaceID = e.record.SpaceID
		}
		if d.Source.SpaceID != e.record.SpaceID || e.cfg.ReadDependency == nil {
			return fmt.Errorf("等待来源不属于本空间或尚未连接")
		}
		if d.Kind == "discussion" && (d.Source.AnnotationID == "" && !d.QuestionSourcePending || d.Source.RequestID != "") || d.Kind == "request" && (d.Source.RequestID == "" || d.Source.AnnotationID != "") {
			return fmt.Errorf("等待项缺少准确原问题或请求身份")
		}
	case "job":
		other, ok := e.record.Jobs[d.JobID]
		if !ok || d.JobID == job.ID || d.JobVersion != other.Version {
			return fmt.Errorf("等待任务不存在、版本已变化或引用自己")
		}
		seen := map[string]bool{}
		var reaches func(string) bool
		reaches = func(id string) bool {
			if id == job.ID {
				return true
			}
			if seen[id] {
				return false
			}
			seen[id] = true
			for _, next := range e.record.Jobs[id].Dependencies {
				if next.Kind == "job" && next.State != "satisfied" && reaches(next.JobID) {
					return true
				}
			}
			return false
		}
		if reaches(d.JobID) {
			return fmt.Errorf("任务等待形成环，请先拆解实际缺口")
		}
	case "time":
		if d.DueAt == nil || d.DueAt.IsZero() {
			return fmt.Errorf("定时检查需要准确时间")
		}
	default:
		return fmt.Errorf("等待来源类型无效")
	}
	return nil
}

func (e *Engine) taskLifecycleLocked(run Run, in taskLifecycleInput) (Job, error) {
	job, exists := e.record.Jobs[in.ID]
	if !exists || job.Version != in.BaseVersion || job.Revision != in.BaseRevision {
		return Job{}, fmt.Errorf("任务目标或进展已变化，请先读取当前版本")
	}
	job = clone(job)
	if job.State == "stopping" || job.State == "cancelled" || job.State == "completed" || job.State == "paused" {
		return Job{}, fmt.Errorf("任务正在停止或已经结束")
	}
	if err := e.validateTaskFollowLocked(job); err != nil {
		return Job{}, err
	}
	if e.taskRunActiveLocked(job.ID) && (run.Kind != "job" || run.JobID != job.ID || run.JobVersion != job.Version) {
		return Job{}, fmt.Errorf("工作轮仍在执行；调整目标请使用 update，不覆盖在途检查点")
	}
	if job.DecisionRunID == run.ID && (job.State == "waiting" || job.State == "failed") {
		return Job{}, fmt.Errorf("本轮已提交等待或结束，请结束当前回合")
	}
	if err := validateCheckpoint(in.Checkpoint, e.record.SpaceID); err != nil {
		return Job{}, err
	}
	if in.Operation == "replan" {
		return e.replanTaskLocked(run, job, in)
	}
	if job.Follow != nil && job.Follow.NeedsReplan {
		return Job{}, fmt.Errorf("关注目标已变化，请先保存 replan 和当前完成条件")
	}
	if err := e.reviewTaskContextsLocked(&job, in); err != nil {
		return Job{}, err
	}
	seenResolution := map[string]bool{}
	for _, resolution := range in.Resolutions {
		if seenResolution[resolution.ID] || strings.TrimSpace(resolution.Explanation) == "" || len(resolution.Explanation) > 2400 || len(resolution.SignalIDs) == 0 || len(resolution.SignalIDs) > 16 {
			return Job{}, fmt.Errorf("解除等待需要说明和实际答复信号")
		}
		seenResolution[resolution.ID] = true
		found := false
		for i := range job.Dependencies {
			d := &job.Dependencies[i]
			if d.ID != resolution.ID {
				continue
			}
			available := map[string]bool{}
			for _, signal := range d.Signals {
				available[signal.ID] = true
			}
			for _, id := range d.EvidenceIDs {
				available[id] = true
			}
			for _, id := range resolution.SignalIDs {
				if !available[id] {
					return Job{}, fmt.Errorf("答复依据不属于该等待项")
				}
			}
			d.State, d.Resolution, d.EvidenceIDs = "satisfied", resolution.Explanation, clone(resolution.SignalIDs)
			found = true
		}
		if !found {
			return Job{}, fmt.Errorf("待解除的问题不存在")
		}
	}
	if len(in.Dependencies) > 8 {
		return Job{}, fmt.Errorf("单任务等待项超过上限")
	}
	ids, identities, inputIDs := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, old := range job.Dependencies {
		ids[old.ID], identities[dependencyIdentity(old)] = true, true
	}
	for _, supplied := range in.Dependencies {
		if inputIDs[supplied.ID] {
			return Job{}, fmt.Errorf("等待项 ID 重复")
		}
		inputIDs[supplied.ID] = true
		if err := e.validateDependencyLocked(job, &supplied); err != nil {
			return Job{}, err
		}
		found := false
		for i := range job.Dependencies {
			d := &job.Dependencies[i]
			if d.ID != supplied.ID {
				continue
			}
			if dependencyIdentity(*d) != dependencyIdentity(supplied) {
				return Job{}, fmt.Errorf("同一等待项 ID 不能改指向另一个问题")
			}
			d.Condition = supplied.Condition
			found = true
		}
		if !found {
			if ids[supplied.ID] || identities[dependencyIdentity(supplied)] {
				return Job{}, fmt.Errorf("等待来源重复")
			}
			// Caller cannot manufacture discovery cursors, answers or satisfaction.
			supplied.Cursor, supplied.State, supplied.Resolution, supplied.Error = "", "waiting", "", ""
			supplied.Signals, supplied.EvidenceIDs = nil, nil
			supplied.Received = nil
			supplied.AnswerReceived = false
			job.Dependencies = append(job.Dependencies, supplied)
			ids[supplied.ID], identities[dependencyIdentity(supplied)] = true, true
		}
	}
	if len(job.Dependencies) > 8 {
		return Job{}, fmt.Errorf("单任务等待项超过上限")
	}
	job.Checkpoint, job.UpdatedAt = clone(in.Checkpoint), time.Now()
	job.Revision++
	waiting := 0
	for _, d := range job.Dependencies {
		if d.State != "satisfied" {
			waiting++
		}
	}
	switch in.Operation {
	case "checkpoint":
		// Progress does not change the business outcome or native run ownership.
	case "wait":
		if waiting == 0 && len(job.Contexts) == 0 || strings.TrimSpace(in.Checkpoint.NextStep) == "" {
			return Job{}, fmt.Errorf("等待需要未满足的依赖或关联认识，以及恢复后的下一步")
		}
		job.State, job.Outcome, job.DecisionRunID = "waiting", "needs_input", run.ID
	case "finish":
		if strings.TrimSpace(in.Result) == "" || len(in.Result) > 16000 {
			return Job{}, fmt.Errorf("结束任务需要实际结果")
		}
		switch in.Outcome {
		case "completed":
			if waiting != 0 || len(in.Checkpoint.OpenQuestions) != 0 {
				return Job{}, fmt.Errorf("仍有未满足的依赖或未决问题，任务尚未完成")
			}
			if len(in.CriteriaChecks) != len(job.CompletionCriteria) || len(in.CriteriaChecks) == 0 {
				return Job{}, fmt.Errorf("请逐项核对任务完成条件")
			}
			for _, check := range in.CriteriaChecks {
				if strings.TrimSpace(check) == "" || len(check) > 2400 {
					return Job{}, fmt.Errorf("完成条件核对为空或过长")
				}
			}
			job.CompletionChecks = clone(in.CriteriaChecks)
			job.State = "completed"
		case "needs_input":
			if waiting == 0 && len(job.Contexts) == 0 {
				return Job{}, fmt.Errorf("需要输入时应登记准确等待项或关联认识")
			}
			job.State = "waiting"
		case "needs_followup":
			job.State = "needs_followup"
		case "failed":
			job.State = "failed"
		default:
			return Job{}, fmt.Errorf("任务结果类型无效")
		}
		job.Outcome, job.Result, job.ResultVersion, job.DecisionRunID = in.Outcome, in.Result, job.Version, run.ID
	}
	if job.State == "waiting" {
		// These signals were available in this working turn. A further answer
		// advances the exact source cursor and wakes the task again.
		for i := range job.Dependencies {
			if job.Dependencies[i].State != "satisfied" {
				job.Dependencies[i].Signals = nil
			}
		}
		job.ResultEventID, job.DeliveredEventID = "", ""
	} else if in.Operation == "finish" && run.Kind != "job" {
		job.ResultEventID, job.DeliveredEventID = "task-result:"+run.ID+":"+job.ID, ""
	}
	e.record.Jobs[job.ID] = job
	return taskForContext(job), nil
}
