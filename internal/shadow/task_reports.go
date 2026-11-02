package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"teamcross/internal/spacecontext"
)

type taskReportInput struct {
	taskLifecycleInput
	State             string `json:"state"`
	PublishedActionID string `json:"publishedActionId"`
}

// The running W supplies identity/version. The caller describes progress, not
// the host's bookkeeping. This runs in the same transaction as the outbox.
func (e *Engine) reportTaskLocked(run Run, call toolCall) (Job, error) {
	var in taskReportInput
	if err := json.Unmarshal(call.Arguments, &in); err != nil {
		return Job{}, err
	}
	if run.Kind != "job" || run.JobID == "" {
		return Job{}, fmt.Errorf("report 由正在执行的 Work 提交；主会话用 start 派发，普通补问直接回复")
	}
	job := e.record.Jobs[run.JobID]
	if in.ID != "" && in.ID != job.ID || in.BaseVersion != 0 && in.BaseVersion != run.JobVersion || job.Version != run.JobVersion {
		return Job{}, fmt.Errorf("工作目标已变化，不能用旧版本回报")
	}
	if in.BaseRevision != 0 && in.BaseRevision != job.Revision {
		return Job{}, fmt.Errorf("工作进展已有新版本，请重新读取")
	}
	in.ID, in.BaseVersion, in.BaseRevision = job.ID, run.JobVersion, job.Revision
	if strings.TrimSpace(in.Result) == "" || len(in.Result) > 16000 {
		return Job{}, fmt.Errorf("report 需要有界的实际进展或结果")
	}
	if in.Checkpoint == nil {
		in.Checkpoint = &TaskCheckpoint{Summary: clip(in.Result, 1800)}
		if job.Checkpoint != nil {
			in.Checkpoint.NextStep = job.Checkpoint.NextStep
			in.Checkpoint.Evidence = clone(job.Checkpoint.Evidence)
			in.Checkpoint.OpenQuestions = clone(job.Checkpoint.OpenQuestions)
		}
	}
	switch in.State {
	case "continue":
		in.Operation = "checkpoint"
	case "wait":
		in.Operation = "wait"
	case "complete":
		in.Operation, in.Outcome = "finish", "completed"
	case "fail":
		in.Operation, in.Outcome = "finish", "failed"
	default:
		return Job{}, fmt.Errorf("report state 只能是 continue、wait、complete 或 fail")
	}
	if _, err := e.taskLifecycleLocked(run, in.taskLifecycleInput); err != nil {
		return Job{}, err
	}
	job = e.record.Jobs[job.ID]
	// A resumed attempt may reach the same wording with new evidence. Dedup
	// within its durable run, without swallowing a later attempt's report.
	reportID := "task-report:" + attentionHash([]any{job.ID, job.Version, run.ID, in.State, in.Result, in.PublishedActionID})
	if in.State == "complete" || in.State == "fail" {
		job.ResultEventID = reportID
	}
	// Explicitly reference an actual public receipt to avoid a second copy.
	if in.PublishedActionID != "" {
		a, exists := e.record.Actions[in.PublishedActionID]
		if !exists || a.State != "saved" || a.JobID != job.ID || a.JobVersion != job.Version || a.Tool != "reply_to_annotation" {
			return Job{}, fmt.Errorf("已发布回执必须属于本 Work 当前版本的实际讨论回复")
		}
		origins := []spacecontext.SourceRef{}
		if job.Trigger != nil {
			origins = uniqueQuestionSources(job.Trigger.Sources)
		}
		if len(origins) == 0 {
			return Job{}, fmt.Errorf("该回执没有绑定本 Work 的原讨论")
		}
		for _, origin := range origins {
			matched := false
			for _, actual := range a.ResponseSources {
				matched = matched || questionSourceID(origin) == questionSourceID(actual)
			}
			if !matched {
				return Job{}, fmt.Errorf("已发布回执不覆盖本 Work 的原讨论")
			}
		}
		job.DeliveredEventID = job.ResultEventID
	} else if err := e.queueTaskReportLocked(run, &job, reportID, in.Result); err != nil {
		return Job{}, err
	}
	e.record.Jobs[job.ID] = job
	return taskForContext(job), nil
}

func (e *Engine) queueTaskReportLocked(run Run, job *Job, id, body string) error {
	messageID := e.taskMessageIDLocked(*job)
	if messageID != "" {
		for _, m := range e.record.Messages {
			if m.ID == id {
				return nil
			}
		}
		m := Message{ID: id, Actor: Actor{ID: e.record.ID, Name: e.record.Name}, Role: "assistant", Text: body, State: "completed", ReplyTo: messageID, SegmentID: run.ThreadID, TurnID: run.TurnID, TaskResultID: job.ResultEventID, CreatedAt: time.Now()}
		e.record.Messages = append(e.record.Messages, m)
		if e.record.ReplyOutbox == nil {
			e.record.ReplyOutbox = map[string]Message{}
		}
		e.record.ReplyOutbox[id] = m
		job.DeliveredEventID = job.ResultEventID
		return nil
	}
	if job.Trigger == nil {
		return fmt.Errorf("Work 缺少准确回报来源")
	}
	for _, ref := range uniqueQuestionSources(job.Trigger.Sources) {
		if ref.SpaceID != e.record.SpaceID {
			return fmt.Errorf("Work 回报来源不属于本空间")
		}
		actionID := id + ":" + questionSourceID(ref)
		if _, exists := e.record.Actions[actionID]; exists {
			continue
		}
		if len(e.record.Actions) >= 10000 {
			return fmt.Errorf("行动记录已达容量边界，回报未提交")
		}
		target := map[string]any{"kind": "annotation", "id": ref.AnnotationID}
		if ref.ReplyID != "" {
			target = map[string]any{"kind": "reply", "id": ref.ReplyID}
		}
		args := map[string]any{"annotationId": ref.AnnotationID, "requestId": actionID, "text": body, "replyTo": target}
		if ref.ExecutionID != "" {
			args["executionId"] = ref.ExecutionID
		}
		if e.cfg.ValidateTool != nil {
			if err := e.cfg.ValidateTool("reply_to_annotation", args); err != nil {
				return err
			}
		}
		encoded, _ := json.Marshal(args)
		e.record.Actions[actionID] = CollaborationAction{ID: actionID, Tool: "reply_to_annotation", Arguments: encoded, Fingerprint: attentionHash([]any{"reply_to_annotation", args}), RunID: run.ID, SourceID: job.SourceID, JobID: job.ID, JobVersion: job.Version, EventID: job.ResultEventID, ResponseSources: []spacecontext.SourceRef{ref}, AutomaticReport: true, State: "pending", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	}
	// Non-discussion activity has no implicit public destination. The fixed
	// task result remains available through the existing result export.
	return nil
}

// Reuse the durable action ledger for discussion outbox delivery. The slow
// host call is separate from the scheduler and native tool reply.
func (e *Engine) scheduleReportDelivery(ctx context.Context) {
	e.mu.Lock()
	if e.reportSending || e.cfg.CallTool == nil || time.Now().Before(e.reportRetryAt) || e.closed {
		e.mu.Unlock()
		return
	}
	var next CollaborationAction
	for _, a := range e.record.Actions {
		if a.AutomaticReport && (a.State == "pending" || a.State == "unknown") && (next.ID == "" || a.CreatedAt.Before(next.CreatedAt)) {
			next = a
		}
	}
	if next.ID == "" {
		e.mu.Unlock()
		return
	}
	job := e.record.Jobs[next.JobID]
	if job.Version != next.JobVersion || job.State == "stopping" || job.State == "cancelled" {
		_ = e.mutateLocked(func() error { next.State = "superseded"; e.record.Actions[next.ID] = next; return nil })
		e.mu.Unlock()
		return
	}
	err := e.commitQuestionSendLocked(next.ResponseSources, func() error {
		return e.mutateLocked(func() error {
			now := time.Now()
			next.SubmittedAt = &now
			e.record.Actions[next.ID] = next
			return nil
		})
	})
	if err != nil {
		e.reportRetryAt = time.Now().Add(2 * time.Second)
		e.mu.Unlock()
		return
	}
	e.reportSending = true
	e.reportWait.Add(1)
	e.mu.Unlock()
	go func() {
		defer e.reportWait.Done()
		var args map[string]any
		_ = json.Unmarshal(next.Arguments, &args)
		sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		out, sendErr := e.cfg.CallTool(sendCtx, next.Tool, args)
		cancel()
		encoded, encodeErr := json.Marshal(out)
		if sendErr == nil && (encodeErr != nil || len(encoded) > 64<<10) {
			sendErr = fmt.Errorf("回报回执无效，保留原行动身份核对")
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		e.reportSending = false
		e.reportRetryAt = time.Now().Add(2 * time.Second)
		_ = e.mutateLocked(func() error {
			a := e.record.Actions[next.ID]
			if a.State == "saved" {
				return nil
			}
			a.UpdatedAt = time.Now()
			if sendErr != nil {
				a.State, a.Error = "unknown", sendErr.Error()
			} else {
				a.State, a.Result, a.Error = "saved", encoded, ""
			}
			e.record.Actions[a.ID] = a
			j := e.record.Jobs[a.JobID]
			if sendErr == nil && j.Version == a.JobVersion && j.ResultEventID != "" && j.ResultEventID == a.EventID {
				all := true
				for _, other := range e.record.Actions {
					if other.AutomaticReport && other.JobID == j.ID && other.EventID == a.EventID && other.State != "saved" {
						all = false
					}
				}
				if all {
					j.DeliveredEventID = a.EventID
					e.record.Jobs[j.ID] = j
				}
			}
			return nil
		})
		e.notify()
	}()
}
