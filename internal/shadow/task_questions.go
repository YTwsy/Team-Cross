package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/spacecontext"
)

// A question is an actual outgoing business action with a durable task link.
// Its delivery state is independent of whether its answer satisfies a task.
type TaskQuestion struct {
	RespondsTo        []QuestionRelation     `json:"respondsTo,omitempty"`
	RelationsRecorded bool                   `json:"relationsRecorded,omitempty"`
	ID                string                 `json:"id"`
	ActionID          string                 `json:"actionId"`
	Fingerprint       string                 `json:"fingerprint"`
	Revision          uint64                 `json:"revision"`
	Tool              string                 `json:"tool"`
	Text              string                 `json:"text"`
	Condition         string                 `json:"condition"`
	Recipients        []string               `json:"recipients"`
	Source            spacecontext.SourceRef `json:"source"`
	JobID             string                 `json:"jobId"`
	JobVersion        int                    `json:"jobVersion"`
	RunID             string                 `json:"runId"`
	EventID           string                 `json:"eventId,omitempty"`
	Delivery          string                 `json:"delivery"` // pending, unknown, saved
	Error             string                 `json:"error,omitempty"`
	CreatedAt         time.Time              `json:"createdAt"`
	UpdatedAt         time.Time              `json:"updatedAt"`
}

type askQuestion struct {
	RespondsTo *[]spacecontext.SourceRef `json:"respondsTo,omitempty"`
	Tool       string                    `json:"tool"`
	Arguments  map[string]any            `json:"arguments"`
	Condition  string                    `json:"condition"`
}

type taskAskInput struct {
	Operation          string          `json:"operation"`
	ID                 string          `json:"id,omitempty"`
	BaseVersion        int             `json:"baseVersion,omitempty"`
	BaseRevision       uint64          `json:"baseRevision,omitempty"`
	Title              string          `json:"title,omitempty"`
	Prompt             string          `json:"prompt,omitempty"`
	CompletionCriteria []string        `json:"completionCriteria,omitempty"`
	Kind               string          `json:"kind,omitempty"`
	FollowID           string          `json:"followId,omitempty"`
	Checkpoint         *TaskCheckpoint `json:"checkpoint"`
	Question           askQuestion     `json:"question"`
}

func (e *Engine) questionResultLocked(q TaskQuestion) any {
	q = clone(q)
	q.Fingerprint, q.RunID, q.EventID = "", "", ""
	q.RelationsRecorded = false
	links := e.questionTaskLinksLocked(q.ID)
	return map[string]any{"question": q, "task": taskForContext(e.record.Jobs[q.JobID]), "tasks": links[:min(16, len(links))], "totalTasks": questionTaskCount(links, false), "totalConditions": len(links), "note": "Saved delivery is not an accepted answer. Each linked task retains its own condition and next step. Use status with a task id for full details, wait when blocked, or continue independent work. A source still pending will be reconciled by its existing business action ID without resending."}
}

func (e *Engine) questionsForContextLocked(id string) (any, error) {
	if id != "" {
		q, ok := e.record.Questions[id]
		if !ok {
			return nil, fmt.Errorf("提问记录不存在")
		}
		return e.questionResultLocked(q), nil
	}
	items := []TaskQuestion{}
	for _, q := range e.record.Questions {
		items = append(items, q)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	index := []map[string]any{}
	links := e.questionLinksLocked()
	for _, q := range items[:min(32, len(items))] {
		index = append(index, map[string]any{"id": q.ID, "jobId": q.JobID, "delivery": q.Delivery, "source": q.Source, "condition": clip(q.Condition, 300), "text": clip(q.Text, 500), "recipients": q.Recipients, "totalTasks": questionTaskCount(links[q.ID], false), "relatedQuestions": len(q.RespondsTo)})
	}
	return map[string]any{"questions": index, "total": len(items), "detail": "Use questions with an exact question id for the original question and current task state."}, nil
}

func (e *Engine) callTaskAsk(ctx context.Context, call toolCall) (any, error) {
	var in taskAskInput
	if len(call.Arguments) > 64<<10 || json.Unmarshal(call.Arguments, &in) != nil || in.Operation != "ask" {
		return nil, fmt.Errorf("提问参数无效或过大")
	}
	if in.Question.Tool != "reply_to_annotation" && in.Question.Tool != "add_annotation" || e.cfg.CallTool == nil || e.cfg.ReadDependency == nil || e.cfg.ValidateTool == nil {
		return nil, fmt.Errorf("提问需要已连接的讨论写入、业务校验和准确答复读取")
	}
	if write, registered := e.collaborationTool(in.Question.Tool); !registered || !write {
		return nil, fmt.Errorf("讨论写入工具尚未接入当前分身")
	}
	if err := e.cfg.ValidateTool(in.Question.Tool, in.Question.Arguments); err != nil {
		return nil, err
	}
	requestID, _ := in.Question.Arguments["requestId"].(string)
	text, _ := in.Question.Arguments["text"].(string)
	if requestID == "" || len(requestID) > 200 || strings.TrimSpace(text) == "" || strings.TrimSpace(in.Question.Condition) == "" || len(in.Question.Condition) > 2400 {
		return nil, fmt.Errorf("提问需要稳定 requestId、实际正文和明确的答复条件")
	}
	relations, err := e.prepareQuestionRelations(ctx, call, in.Question, requestID)
	if err != nil {
		return nil, err
	}
	fingerprint := attentionHash(in)
	e.mu.Lock()
	questionID := attentionHash([]any{e.record.ID, requestID})
	var run Run
	for _, candidate := range e.record.Runs {
		if activeRun(candidate) && candidate.ThreadID == call.ThreadID && (candidate.TurnID == call.TurnID || candidate.TurnID == "") {
			run = candidate
			break
		}
	}
	if run.ID == "" || call.CallID == "" {
		e.mu.Unlock()
		return nil, fmt.Errorf("提问不属于当前活动轮次")
	}
	run, err = selectInput(run, inputID(call.Arguments))
	if err != nil {
		e.mu.Unlock()
		return nil, err
	}
	if old, exists := e.record.Questions[questionID]; exists {
		if old.Fingerprint != fingerprint {
			e.mu.Unlock()
			return nil, fmt.Errorf("同一提问 ID 的内容或任务关系发生变化")
		}
		job := e.record.Jobs[old.JobID]
		if old.Delivery == "saved" || job.Version != old.JobVersion || job.State == "cancelled" || job.State == "completed" || e.validateTaskFollowLocked(job) != nil {
			result := e.questionResultLocked(old)
			e.mu.Unlock()
			return result, nil
		}
	} else {
		origin, err := e.taskOriginLocked(run)
		if err != nil {
			e.mu.Unlock()
			return nil, err
		}
		if len(e.record.Questions) >= 10000 || len(e.record.Actions) >= 10000 {
			e.mu.Unlock()
			return nil, fmt.Errorf("提问或行动记录已达保留边界")
		}
		if _, exists := e.record.Actions[requestID]; exists {
			e.mu.Unlock()
			return nil, fmt.Errorf("提问 ID 已用于其他行动，请核对原记录")
		}
		if err := validateCheckpoint(in.Checkpoint, e.record.SpaceID); err != nil {
			e.mu.Unlock()
			return nil, err
		}
		if strings.TrimSpace(in.Checkpoint.NextStep) == "" {
			e.mu.Unlock()
			return nil, fmt.Errorf("提问需要记录答复到达后要继续的工作")
		}
		id := in.ID
		if id == "" && run.JobID != "" {
			id = run.JobID
		}
		job, exists := e.record.Jobs[id]
		if exists {
			if job.DecisionRunID == run.ID && job.State != "running" {
				e.mu.Unlock()
				return nil, fmt.Errorf("本轮已提交等待或结果，请结束当前回合")
			}
			if job.Version != in.BaseVersion || job.Revision != in.BaseRevision || job.State == "completed" || job.State == "cancelled" || job.State == "stopping" || job.State == "paused" || e.taskRunActiveLocked(job.ID) && (run.Kind != "job" || run.JobID != job.ID) {
				e.mu.Unlock()
				return nil, fmt.Errorf("任务目标或进展已变化，或正在其他工作轮执行")
			}
			if err := e.validateTaskFollowLocked(job); err != nil {
				e.mu.Unlock()
				return nil, err
			}
			if job.Follow != nil && job.Follow.NeedsReplan {
				e.mu.Unlock()
				return nil, fmt.Errorf("先重核当前任务目标，再提出新问题")
			}
			job = clone(job)
		} else {
			if id != "" || strings.TrimSpace(in.Title) == "" || len(in.Title) > 320 || strings.TrimSpace(in.Prompt) == "" || len(in.Prompt) > 24<<10 || len(in.CompletionCriteria) == 0 || len(in.CompletionCriteria) > 16 {
				e.mu.Unlock()
				return nil, fmt.Errorf("新提问需要后续任务的 title、prompt 和 completionCriteria")
			}
			for _, c := range in.CompletionCriteria {
				if strings.TrimSpace(c) == "" || len(c) > 2400 {
					e.mu.Unlock()
					return nil, fmt.Errorf("完成条件无效")
				}
			}
			open := 0
			for _, j := range e.record.Jobs {
				if j.State != "completed" && j.State != "cancelled" && j.State != "failed" && j.State != "interrupted" {
					open++
				}
			}
			if open >= 32 {
				e.mu.Unlock()
				return nil, fmt.Errorf("未结束任务已达上限")
			}
			kind := in.Kind
			if kind != "work" {
				e.mu.Unlock()
				return nil, fmt.Errorf("普通补问直接回复，不创建协调任务；请先 start 明确的 Work")
			}
			follow, err := e.newTaskFollowLocked(run, origin, in.FollowID)
			if err != nil {
				e.mu.Unlock()
				return nil, err
			}
			job = Job{ID: uuid.NewString(), Title: in.Title, Prompt: in.Prompt, CompletionCriteria: clone(in.CompletionCriteria), Kind: kind, SourceID: origin.SourceID, Trigger: clone(origin), Follow: follow, ParentID: run.JobID, Version: 1, CreatedAt: time.Now(), State: "waiting", Outcome: "needs_input"}
		}
		if len(job.Dependencies) >= 8 {
			e.mu.Unlock()
			return nil, fmt.Errorf("任务等待项已达上限")
		}
		source := spacecontext.SourceRef{SpaceID: e.record.SpaceID}
		source.ExecutionID, _ = in.Question.Arguments["executionId"].(string)
		source.AnnotationID, _ = in.Question.Arguments["annotationId"].(string)
		var fields struct {
			Mentions []struct {
				MemberID string `json:"memberId"`
			} `json:"mentions"`
		}
		canonical, _ := json.Marshal(in.Question.Arguments)
		_ = json.Unmarshal(canonical, &fields)
		recipients := []string{}
		for _, mention := range fields.Mentions {
			recipients = append(recipients, mention.MemberID)
		}
		q := TaskQuestion{RespondsTo: clone(relations), RelationsRecorded: true, ID: questionID, ActionID: requestID, Fingerprint: fingerprint, Revision: 1, Tool: in.Question.Tool, Text: text, Condition: in.Question.Condition, Recipients: recipients, Source: source, JobID: job.ID, JobVersion: job.Version, RunID: run.ID, EventID: run.EventID, Delivery: "pending", CreatedAt: time.Now(), UpdatedAt: time.Now()}
		job.Dependencies = append(job.Dependencies, TaskDependency{ID: "question:" + questionID, Kind: "discussion", QuestionID: questionID, QuestionActionID: requestID, QuestionTool: q.Tool, QuestionSourcePending: true, Condition: q.Condition, Source: source, State: "waiting"})
		job.Checkpoint, job.UpdatedAt = clone(in.Checkpoint), time.Now()
		job.Revision++
		if !e.taskRunActiveLocked(job.ID) {
			job.State, job.Outcome, job.ResultEventID, job.DeliveredEventID = "waiting", "needs_input", "", ""
		}
		action := CollaborationAction{ID: requestID, Tool: q.Tool, Arguments: canonical, Fingerprint: attentionHash([]any{q.Tool, in.Question.Arguments}), QuestionID: q.ID, JobID: job.ID, JobVersion: job.Version, RunID: run.ID, SourceID: run.SourceID, EventID: run.EventID, ResponseSources: questionRelationSources(q.RespondsTo), State: "pending", CreatedAt: time.Now(), UpdatedAt: time.Now()}
		err = e.commitQuestionSendLocked(action.ResponseSources, func() error {
			return e.mutateLocked(func() error {
				e.record.Questions[q.ID], e.record.Jobs[job.ID], e.record.Actions[action.ID] = q, job, action
				e.trackInputTaskLocked(run, job.ID)
				return nil
			})
		})
		if err != nil {
			e.mu.Unlock()
			return nil, err
		}
	}
	e.mu.Unlock()
	arguments := clone(in.Question.Arguments)
	if id := inputID(call.Arguments); id != "" {
		arguments["inputId"] = id
	}
	args, _ := json.Marshal(arguments)
	result, sendErr := e.callCollaborationTool(ctx, toolCall{ThreadID: call.ThreadID, TurnID: call.TurnID, CallID: call.CallID, Tool: in.Question.Tool, Arguments: args})
	e.mu.Lock()
	defer e.mu.Unlock()
	err = e.mutateLocked(func() error {
		q := e.record.Questions[questionID]
		if q.Delivery == "saved" {
			return nil
		}
		if sendErr == nil {
			ref, err := questionReceiptSource(q, result, e.record.ID, e.record.Owner.ID)
			if err == nil {
				return e.bindQuestionSourceLocked(q.ID, ref)
			}
			sendErr = err
		}
		q.Delivery, q.Error, q.UpdatedAt = "unknown", sendErr.Error(), time.Now()
		e.record.Questions[q.ID] = q
		return nil
	})
	if err != nil {
		return nil, err
	}
	e.notify()
	return e.questionResultLocked(e.record.Questions[questionID]), nil
}

func questionReceiptSource(q TaskQuestion, result any, shadowID, ownerID string) (spacecontext.SourceRef, error) {
	type posted struct {
		ID        string             `json:"id"`
		RequestID string             `json:"requestId"`
		Actor     spacecontext.Actor `json:"actor"`
	}
	var receipt struct {
		ID        string             `json:"id"`
		RequestID string             `json:"requestId"`
		Actor     spacecontext.Actor `json:"actor"`
		Reply     posted             `json:"reply"`
	}
	b, _ := json.Marshal(result)
	if json.Unmarshal(b, &receipt) != nil {
		return q.Source, fmt.Errorf("提问回执尚未确认")
	}
	value := posted{receipt.ID, receipt.RequestID, receipt.Actor}
	if q.Tool == "reply_to_annotation" {
		value = receipt.Reply
	}
	if value.ID == "" || value.RequestID != q.ActionID || value.Actor.Kind != "shadow" || value.Actor.ShadowID != shadowID || value.Actor.MemberID != ownerID {
		return q.Source, fmt.Errorf("提问的准确来源或实际作者尚未确认，保留原行动核对")
	}
	ref := q.Source
	if q.Tool == "reply_to_annotation" {
		ref.ReplyID = value.ID
	} else {
		ref.AnnotationID = value.ID
	}
	return ref, nil
}

// Called only inside an engine transaction. Read-only business reconciliation
// can bind the source even when the original write acknowledgement was lost.
func (e *Engine) validateQuestionSourceLocked(id string, ref spacecontext.SourceRef) error {
	q, ok := e.record.Questions[id]
	if !ok || ref.SpaceID != e.record.SpaceID || ref.ExecutionID != q.Source.ExecutionID || ref.AnnotationID == "" || ref.RequestID != "" || q.Source.AnnotationID != "" && ref.AnnotationID != q.Source.AnnotationID || q.Tool == "reply_to_annotation" && ref.ReplyID == "" || q.Tool == "add_annotation" && ref.ReplyID != "" {
		return fmt.Errorf("提问来源与原行动不匹配")
	}
	if q.Delivery == "saved" && attentionHash(q.Source) != attentionHash(ref) {
		return fmt.Errorf("同一提问返回了不同来源")
	}
	return nil
}

func bindQuestionDependencies(job *Job, id string, ref spacecontext.SourceRef) bool {
	changed := false
	for i := range job.Dependencies {
		d := &job.Dependencies[i]
		if d.QuestionID == id && d.QuestionSourcePending {
			d.Source, d.QuestionSourcePending = clone(ref), false
			changed = true
		}
	}
	return changed
}

func (e *Engine) bindQuestionSourceLocked(id string, ref spacecontext.SourceRef) error {
	if err := e.validateQuestionSourceLocked(id, ref); err != nil {
		return err
	}
	q := e.record.Questions[id]
	if q.Delivery == "saved" {
		return nil
	}
	q.Revision++
	q.Source, q.Delivery, q.Error, q.UpdatedAt = clone(ref), "saved", "", time.Now()
	e.record.Questions[id] = q
	for jobID, current := range e.record.Jobs {
		job := clone(current)
		changed := bindQuestionDependencies(&job, id, ref)
		if changed {
			job.Revision++
			job.UpdatedAt = time.Now()
			e.record.Jobs[jobID] = job
		}
	}
	return nil
}
