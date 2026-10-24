package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"teamcross/internal/spacecontext"
)

type WatchInput struct {
	Operation       string               `json:"operation"`
	ID              string               `json:"id,omitempty"`
	Subject         spacecontext.Subject `json:"subject,omitempty"`
	Goal            string               `json:"goal,omitempty"`
	BaseGoalVersion uint64               `json:"baseGoalVersion,omitempty"`
	RequestID       string               `json:"requestId,omitempty"`
	Cursor          string               `json:"cursor,omitempty"`
	Limit           int                  `json:"limit,omitempty"`
}

type WatchCommand struct {
	Fingerprint string     `json:"fingerprint"`
	Input       WatchInput `json:"input"`
	FocusID     string     `json:"focusId"`
	SourceID    string     `json:"sourceId"`
	State       string     `json:"state"`
	Error       string     `json:"error,omitempty"`
	Result      *Focus     `json:"result,omitempty"`
}

func watchTool() any {
	str := map[string]any{"type": "string"}
	return map[string]any{"type": "function", "name": "shadow_watch", "description": "Read and maintain the owner's private understanding of followed objects in an owner-originated request. read uses id or exact subject; list is paged. start follows the existing space object and stores an optional goal; update changes that goal; pause/resume controls analysis without changing the member follow; complete ends the stated goal while ordinary future updates can still refresh understanding. Writes need a stable requestId and baseGoalVersion from read (0 if absent). Native turn completion does not complete a goal. These are direct operations, not approval requests. Other members' shared requests use public sources and their actual tasks, not the owner's private follow list.", "inputSchema": map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"operation"}, "properties": map[string]any{
			"operation": map[string]any{"type": "string", "enum": []string{"read", "list", "start", "update", "pause", "resume", "complete"}},
			"id":        str, "goal": str, "requestId": str, "cursor": str, "baseGoalVersion": map[string]any{"type": "integer", "minimum": 0}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 50},
			"subject": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"kind", "id"}, "properties": map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"material", "discussion", "execution"}}, "id": str, "executionId": str}},
		},
	}}
}

func (s *AttentionStore) ReadWatch(in WatchInput) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.Operation == "read" {
		id := in.ID
		if in.Subject.ID != "" {
			if id != "" && id != attentionHash(in.Subject) {
				return nil, fmt.Errorf("认识 ID 与对象不符")
			}
			id = attentionHash(in.Subject)
		}
		if id == "" {
			return nil, fmt.Errorf("读取认识需要 id 或准确对象")
		}
		f, exists := s.record.Focuses[id]
		if !exists {
			return map[string]any{"id": id, "subject": in.Subject, "exists": false, "goalVersion": 0}, nil
		}
		return clone(f), nil
	}
	if in.Operation != "list" {
		return nil, fmt.Errorf("认识读取操作无效")
	}
	items := []Focus{}
	for _, f := range s.record.Focuses {
		items = append(items, clone(f))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	fingerprint := attentionHash(items)
	offset := 0
	if in.Cursor != "" {
		parts := strings.Split(in.Cursor, ":")
		var err error
		if len(parts) == 2 {
			offset, err = strconv.Atoi(parts[1])
		}
		if len(parts) != 2 || err != nil || parts[0] != fingerprint || offset < 0 || offset > len(items) {
			return nil, fmt.Errorf("认识清单已变化，请重新读取")
		}
	}
	if in.Limit == 0 {
		in.Limit = 20
	}
	if in.Limit < 1 || in.Limit > 50 {
		return nil, fmt.Errorf("认识分页大小需要 1–50")
	}
	end := min(offset+in.Limit, len(items))
	index := []map[string]any{}
	for _, f := range items[offset:end] {
		index = append(index, map[string]any{"id": f.ID, "subject": f.Subject, "goal": clip(f.Goal, 200), "goalVersion": f.GoalVersion, "revision": f.Revision, "state": f.State, "processing": f.Processing, "summary": clip(f.Summary, 400), "updatedAt": f.UpdatedAt})
	}
	next := ""
	if end < len(items) {
		next = fingerprint + ":" + strconv.Itoa(end)
	}
	return map[string]any{"items": index, "total": len(items), "nextCursor": next}, nil
}

func (s *AttentionStore) BeginWatch(in WatchInput, sourceID string) (WatchCommand, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.RequestID == "" || len(in.RequestID) > 200 || sourceID == "" || len(in.Goal) > 8000 {
		return WatchCommand{}, fmt.Errorf("关注目标需要稳定 requestId、真实来源和有界内容")
	}
	switch in.Operation {
	case "start", "update", "pause", "resume", "complete":
	default:
		return WatchCommand{}, fmt.Errorf("关注目标操作无效")
	}
	fingerprint := attentionHash(in)
	if previous, exists := s.record.WatchCommands[in.RequestID]; exists {
		if previous.Fingerprint != fingerprint {
			return WatchCommand{}, fmt.Errorf("requestId 已用于不同关注目标")
		}
		return clone(previous), nil
	}
	id := in.ID
	if in.Subject.ID != "" {
		if (in.Subject.Kind != "material" && in.Subject.Kind != "discussion" && in.Subject.Kind != "execution") || (in.Subject.Kind != "discussion" && in.Subject.ExecutionID != "") {
			return WatchCommand{}, fmt.Errorf("关注对象身份无效")
		}
		if id != "" && id != attentionHash(in.Subject) {
			return WatchCommand{}, fmt.Errorf("认识 ID 与关注对象不符")
		}
		id = attentionHash(in.Subject)
	}
	f, exists := s.record.Focuses[id]
	if !exists && (in.Operation != "start" || in.Subject.ID == "") {
		return WatchCommand{}, fmt.Errorf("认识不存在；start 需要准确已有空间对象")
	}
	if f.GoalVersion != in.BaseGoalVersion {
		return WatchCommand{}, fmt.Errorf("关注目标已变化，请读取当前 goalVersion")
	}
	if len(s.record.WatchCommands) >= 10000 {
		return WatchCommand{}, fmt.Errorf("关注操作回执达到存储上限")
	}
	next := clone(s.record)
	if !exists {
		// Reserve the identity before remote follow. Its incoming baseline can
		// arrive while the response is in flight, but cannot run without the goal.
		f = Focus{ID: id, Subject: in.Subject, State: "paused", Revision: 1, Evidence: []spacecontext.SourceRef{}, OpenQuestions: []string{}, ActionIDs: []string{}, JobIDs: []string{}, UpdatedAt: time.Now()}
		next.Focuses[id] = f
	}
	command := WatchCommand{Fingerprint: fingerprint, Input: in, FocusID: id, SourceID: sourceID, State: "pending"}
	if next.WatchCommands == nil {
		next.WatchCommands = map[string]WatchCommand{}
	}
	next.WatchCommands[in.RequestID] = command
	if err := s.saveLocked(next); err != nil {
		return WatchCommand{}, err
	}
	return clone(command), nil
}

func (s *AttentionStore) FinishWatch(requestID string) (Focus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	command, exists := s.record.WatchCommands[requestID]
	if !exists {
		return Focus{}, fmt.Errorf("关注目标意图不存在")
	}
	if command.Result != nil {
		return clone(*command.Result), nil
	}
	f := s.record.Focuses[command.FocusID]
	if f.GoalVersion != command.Input.BaseGoalVersion {
		return Focus{}, fmt.Errorf("关注目标已变化，先读取当前认识再接续")
	}
	switch command.Input.Operation {
	case "start":
		f.State, f.Goal = "active", command.Input.Goal
	case "update":
		f.Goal = command.Input.Goal
		if f.State == "completed" {
			f.State = "active"
		}
	case "pause":
		f.State = "paused"
	case "resume":
		f.State = "active"
	case "complete":
		f.State = "completed"
	}
	f.GoalVersion++
	f.Revision++
	f.GoalSourceID, f.UpdatedAt, f.Error, f.RetryAfter = command.SourceID, time.Now(), "", time.Time{}
	next := clone(s.record)
	next.Focuses[f.ID] = f
	for _, batch := range next.Batches {
		if batch.Focus.ID == f.ID && batch.State == "preparing" {
			resetBatch(&next, batch, "superseded", "", time.Time{})
		}
	}
	f = next.Focuses[f.ID]
	command.State, command.Result, command.Error = "saved", &f, ""
	next.WatchCommands[requestID] = command
	if err := s.saveLocked(next); err != nil {
		return Focus{}, err
	}
	return clone(f), nil
}

func (s *AttentionStore) failWatch(id, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	command, exists := s.record.WatchCommands[id]
	if !exists || command.Result != nil {
		return nil
	}
	next := clone(s.record)
	command.Error = clip(reason, 2000)
	next.WatchCommands[id] = command
	f := next.Focuses[command.FocusID]
	if f.GoalVersion == command.Input.BaseGoalVersion {
		f.Error = command.Error
		next.Focuses[f.ID] = f
	}
	return s.saveLocked(next)
}

func (e *Engine) watchOriginLocked(call toolCall) (string, error) {
	for _, run := range e.record.Runs {
		if !activeRun(run) || call.CallID == "" || run.ThreadID != call.ThreadID || run.TurnID != "" && run.TurnID != call.TurnID {
			continue
		}
		origin, err := e.taskOriginLocked(run)
		if err != nil {
			return "", err
		}
		if origin.Kind == "message" {
			for _, message := range e.record.Messages {
				if message.ID == origin.MessageID && message.Role == "user" && message.Actor.Kind != "shadow" && message.Actor.ID == e.record.Owner.ID {
					return message.ID, nil
				}
			}
		}
		break
	}
	return "", fmt.Errorf("个人关注由所属成员管理；当前共享请求请使用公开来源和实际任务")
}

func (e *Engine) callWatch(ctx context.Context, call toolCall) (any, error) {
	var in WatchInput
	if len(call.Arguments) > 16<<10 || json.Unmarshal(call.Arguments, &in) != nil {
		return nil, fmt.Errorf("关注工具参数无效或过大")
	}
	e.watchMu.Lock()
	defer e.watchMu.Unlock()
	e.mu.Lock()
	sourceID, err := e.watchOriginLocked(call)
	store := e.attention
	e.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, fmt.Errorf("本机成员认识尚未连接")
	}
	if in.Operation == "read" || in.Operation == "list" {
		return store.ReadWatch(in)
	}
	if in.Operation == "start" && e.cfg.EnsureFollow == nil {
		return nil, fmt.Errorf("空间关注入口尚未连接")
	}
	command, err := store.BeginWatch(in, sourceID)
	if err != nil {
		return nil, err
	}
	if command.Result != nil {
		return *command.Result, nil
	}
	if in.Operation == "start" {
		f := store.Snapshot().Focuses[command.FocusID]
		if f.GoalVersion != in.BaseGoalVersion {
			return nil, fmt.Errorf("关注目标已变化，请读取当前认识")
		}
		if _, err := e.cfg.EnsureFollow(ctx, f.Subject, "shadow-watch-"+attentionHash(in.RequestID)); err != nil {
			if saveErr := store.failWatch(in.RequestID, err.Error()); saveErr != nil {
				return nil, fmt.Errorf("关注回执及本机诊断保存尚未确认：%v；%w", err, saveErr)
			}
			return nil, fmt.Errorf("关注保存尚未确认，沿用同一 requestId 核对：%w", err)
		}
	}
	e.mu.Lock()
	_, err = e.watchOriginLocked(call)
	e.mu.Unlock()
	if err != nil {
		return nil, err
	}
	result, err := store.FinishWatch(in.RequestID)
	if err == nil {
		e.notify()
	}
	return result, err
}
