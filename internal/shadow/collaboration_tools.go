package shadow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"teamcross/internal/spacecontext"
)

type CollaborationAction struct {
	ID              string                   `json:"id"`
	Tool            string                   `json:"tool"`
	Arguments       json.RawMessage          `json:"arguments"`
	Fingerprint     string                   `json:"fingerprint"`
	RunID           string                   `json:"runId"`
	SourceID        string                   `json:"sourceId,omitempty"`
	EventID         string                   `json:"eventId,omitempty"`
	JobID           string                   `json:"jobId,omitempty"`
	JobVersion      int                      `json:"jobVersion,omitempty"`
	QuestionID      string                   `json:"questionId,omitempty"`
	State           string                   `json:"state"`
	Result          json.RawMessage          `json:"result,omitempty"`
	Error           string                   `json:"error,omitempty"`
	CreatedAt       time.Time                `json:"createdAt"`
	UpdatedAt       time.Time                `json:"updatedAt"`
	SubmittedAt     *time.Time               `json:"submittedAt,omitempty"`
	ResponseSources []spacecontext.SourceRef `json:"responseSources,omitempty"`
}

func (e *Engine) collaborationTool(name string) (bool, bool) {
	for _, t := range e.cfg.CollaborationTools {
		if t["name"] == name {
			annotations, _ := t["annotations"].(map[string]any)
			return annotations["readOnlyHint"] != true, true
		}
	}
	return false, false
}

// Native call IDs only deduplicate a single tool invocation. Business action
// IDs survive turn boundaries, result integration and exact-thread recovery.
func (e *Engine) callCollaborationTool(ctx context.Context, call toolCall) (any, error) {
	write, found := e.collaborationTool(call.Tool)
	if !found || e.cfg.CallTool == nil {
		return nil, fmt.Errorf("分身协作工具尚未连接")
	}
	var args map[string]any
	if len(call.Arguments) > 64<<10 || json.Unmarshal(call.Arguments, &args) != nil || args == nil {
		return nil, fmt.Errorf("协作工具参数无效或过大")
	}
	e.mu.Lock()
	var run Run
	for _, candidate := range e.record.Runs {
		if activeRun(candidate) && candidate.ThreadID == call.ThreadID && (candidate.TurnID == call.TurnID || candidate.TurnID == "") {
			run = candidate
			break
		}
	}
	if run.ID == "" || call.CallID == "" {
		e.mu.Unlock()
		return nil, fmt.Errorf("协作工具调用不属于当前活动轮次")
	}
	if write && run.Kind != "main" && run.Kind != "result" && run.Kind != "job" && run.Kind != "continuation" {
		e.mu.Unlock()
		return nil, fmt.Errorf("观察或维护回合只准备上下文，实际协作由接续任务执行")
	}
	if run.JobID != "" {
		job := e.record.Jobs[run.JobID]
		if job.Version != run.JobVersion || job.State == "stopping" || job.State == "cancelled" {
			e.mu.Unlock()
			return nil, fmt.Errorf("任务已修改或取消，停止旧版本行动")
		}
		if write && run.Kind == "job" && job.DecisionRunID == run.ID && job.State != "running" {
			e.mu.Unlock()
			return nil, fmt.Errorf("本轮已保存等待或结果，停止继续提交动作")
		}
		if write {
			if err := e.validateTaskFollowLocked(job); err != nil {
				e.mu.Unlock()
				return nil, err
			}
			if job.Follow != nil && job.Follow.NeedsReplan {
				e.mu.Unlock()
				return nil, fmt.Errorf("关注目标已变化，请先保存任务 replan 再行动")
			}
		}
	}
	if write && run.Kind == "continuation" {
		if e.attention == nil {
			e.mu.Unlock()
			return nil, fmt.Errorf("接续认识记录未连接")
		}
		if _, err := e.attention.CurrentContinuation(run.EventID, e.record.ID); err != nil {
			e.mu.Unlock()
			return nil, err
		}
	}
	actionID, _ := args["requestId"].(string)
	if call.Tool == "send_space_shadow" {
		actionID, _ = args["messageId"].(string)
	}
	canonical, _ := json.Marshal(args)
	fingerprint := attentionHash([]any{call.Tool, args})
	if write {
		if actionID == "" || len(actionID) > 200 {
			e.mu.Unlock()
			return nil, fmt.Errorf("协作写入需要稳定行动 ID，同一行动跨回合保持 requestId/messageId")
		}
		action, exists := e.record.Actions[actionID]
		if exists {
			if action.Fingerprint != fingerprint {
				e.mu.Unlock()
				return nil, fmt.Errorf("行动 ID 已用于不同工具或正文，请核对原行动")
			}
			if action.State == "saved" {
				e.mu.Unlock()
				return clone(action.Result), nil
			}
			if action.QuestionID != "" {
				q, known := e.record.Questions[action.QuestionID]
				job := e.record.Jobs[q.JobID]
				if !known || job.Version != q.JobVersion || !taskAcceptsAnswers(job) || job.State == "paused" || job.State == "stopping" {
					e.mu.Unlock()
					return nil, fmt.Errorf("提问所关联的任务已变化，保留原记录核对")
				}
				if err := e.validateTaskFollowLocked(job); err != nil {
					e.mu.Unlock()
					return nil, err
				}
				if job.Follow != nil && job.Follow.NeedsReplan {
					e.mu.Unlock()
					return nil, fmt.Errorf("请先重核提问所关联的任务目标")
				}
			}
		} else {
			if len(e.record.Actions) >= 10000 {
				e.mu.Unlock()
				return nil, fmt.Errorf("行动记录已达容量边界，尚未提交新行动")
			}
			action = CollaborationAction{ID: actionID, Tool: call.Tool, Arguments: canonical, Fingerprint: fingerprint, RunID: run.ID, SourceID: run.SourceID, EventID: run.EventID, JobID: run.JobID, JobVersion: run.JobVersion, State: "pending", CreatedAt: time.Now(), UpdatedAt: time.Now()}
			if err := e.mutateLocked(func() error { e.record.Actions[actionID] = action; return nil }); err != nil {
				e.mu.Unlock()
				return nil, err
			}
		}
	}
	if write {
		sources := e.responseSourcesLocked(call.Tool, args, run)
		if q, ok := e.record.Questions[e.record.Actions[actionID].QuestionID]; ok && q.RelationsRecorded {
			// The question's relationship was saved with its send intent. A
			// retry in another turn must not rebind it to that turn's sources.
			sources = questionRelationSources(q.RespondsTo)
		}
		err := e.commitQuestionSendLocked(sources, func() error {
			return e.mutateLocked(func() error {
				action := e.record.Actions[actionID]
				now := time.Now()
				action.SubmittedAt, action.ResponseSources, action.Error = &now, sources, ""
				if action.State == "held_for_owner" {
					action.State = "pending"
				}
				e.record.Actions[actionID] = action
				return nil
			})
		})
		if err != nil {
			if errors.Is(err, errQuestionOwned) && e.record.Actions[actionID].SubmittedAt == nil {
				if saveErr := e.mutateLocked(func() error {
					a := e.record.Actions[actionID]
					a.State, a.Error = "held_for_owner", err.Error()
					e.record.Actions[actionID] = a
					return nil
				}); saveErr != nil {
					err = saveErr
				}
			}
			e.mu.Unlock()
			return nil, err
		}
	}
	e.mu.Unlock()
	// No engine lock crosses a network/business call. These operations resolve
	// an existing stable business ID before deciding whether a write is new.
	out, callErr := e.cfg.CallTool(ctx, call.Tool, args)
	encoded, encodeErr := json.Marshal(out)
	if callErr == nil && (encodeErr != nil || len(encoded) > 64<<10) {
		callErr = fmt.Errorf("协作结果超过读取预算；按准确来源分页读取，写入仍使用原行动 ID 核对")
	}
	if write {
		e.mu.Lock()
		// A concurrent successful receipt is authoritative even when this
		// invocation lost its response. Never downgrade saved back to unknown.
		if current := e.record.Actions[actionID]; current.State == "saved" {
			e.mu.Unlock()
			return clone(current.Result), nil
		}
		err := e.mutateLocked(func() error {
			action := e.record.Actions[actionID]
			action.UpdatedAt = time.Now()
			if callErr != nil {
				action.State, action.Error = "unknown", callErr.Error()
			} else {
				action.State, action.Error, action.Result = "saved", "", encoded
				if call.Tool == "update_space_brief" {
					if ref, ok := briefToolSource(out); ok {
						action.ResponseSources = []spacecontext.SourceRef{ref}
					}
				}
			}
			e.record.Actions[actionID] = action
			return nil
		})
		e.mu.Unlock()
		if err != nil {
			return nil, err
		}
	}
	if !write && callErr == nil && run.Kind == "continuation" {
		ref, ok := readToolSource(call.Tool, args)
		if call.Tool == "read_space_brief" {
			ref, ok = briefToolSource(out)
		}
		if call.Tool == "read_space_shadow" {
			var result struct {
				Source      spacecontext.SourceRef `json:"source"`
				Unavailable string                 `json:"unavailable"`
			}
			data, _ := json.Marshal(out)
			ok = json.Unmarshal(data, &result) == nil && result.Unavailable == "" && result.Source.IsShadow() && result.Source.ValidateShadow() == nil
			ref = result.Source
		}
		if ok {
			e.mu.Lock()
			ref.SpaceID = e.record.SpaceID
			current := e.record.Runs[run.ID]
			found := false
			for _, previous := range current.ReadSources {
				found = found || attentionHash(previous) == attentionHash(ref)
			}
			var err error
			if !found {
				if len(current.ReadSources) >= 32 {
					err = fmt.Errorf("本轮读取证据达到预算，请保存进展后按任务继续")
				} else {
					err = e.mutateLocked(func() error {
						current.ReadSources = append(current.ReadSources, ref)
						e.record.Runs[run.ID] = current
						return nil
					})
				}
			}
			e.mu.Unlock()
			if err != nil {
				return nil, err
			}
		}
	}
	return out, callErr
}

func briefToolSource(out any) (spacecontext.SourceRef, bool) {
	var result struct {
		Source      spacecontext.SourceRef `json:"source"`
		Unavailable string                 `json:"unavailable"`
		State       string                 `json:"state"`
	}
	data, _ := json.Marshal(out)
	if json.Unmarshal(data, &result) != nil {
		return result.Source, false
	}
	return result.Source, result.Source.BriefRevision > 0 && result.Unavailable == "" && result.State != "conflict"
}

func readToolSource(tool string, args map[string]any) (spacecontext.SourceRef, bool) {
	var ref spacecontext.SourceRef
	data, _ := json.Marshal(args)
	_ = json.Unmarshal(data, &ref)
	switch tool {
	case "read_annotations":
		return ref, ref.AnnotationID != ""
	case "read_material":
		return ref, ref.MaterialID != "" && ref.Version > 0
	case "get_space_request":
		return ref, ref.RequestID != ""
	case "read_context":
		return ref, args["kind"] == "annotations" && ref.AnnotationID != "" || args["kind"] == "history" && ref.ExecutionID != "" && ref.TurnID != ""
	}
	return ref, false
}
