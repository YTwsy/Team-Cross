package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"teamcross/internal/nativecodex"
)

type nativeTurn struct {
	ID     string            `json:"id"`
	Status string            `json:"status"`
	Items  []json.RawMessage `json:"items"`
	Error  json.RawMessage   `json:"error"`
}

func itemText(raw json.RawMessage) string {
	var x struct {
		Text    string `json:"text"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	_ = json.Unmarshal(raw, &x)
	if x.Text != "" {
		return x.Text
	}
	parts := []string{}
	for _, c := range x.Content {
		if c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func (e *Engine) restore(ctx context.Context) error {
	e.mu.Lock()
	p, r := e.runtime, clone(e.record)
	e.mu.Unlock()
	ids := map[string]bool{}
	for _, id := range r.AttentionThreads {
		ids[id] = true
	}
	if r.Current != "" {
		ids[r.Current] = true
	}
	if r.Transition != nil && r.Transition.To != "" {
		ids[r.Transition.To] = true
	}
	for _, run := range r.Runs {
		if activeRun(run) {
			ids[run.ThreadID] = true
		}
	}
	for _, j := range r.Jobs {
		if j.ThreadID != "" && j.Kind != "coordination" && (j.State == "running" || j.State == "stopping" || j.State == "queued" || j.State == "waiting" || j.State == "paused" || j.State == "needs_followup") {
			ids[j.ThreadID] = true
		}
	}
	for id := range ids {
		var resumed struct {
			Model           string `json:"model"`
			ModelProvider   string `json:"modelProvider"`
			ReasoningEffort string `json:"reasoningEffort"`
			Thread          struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		if err := p.Call(ctx, "thread/resume", r.Settings.threadParams(map[string]any{"threadId": id}), &resumed); err != nil {
			return fmt.Errorf("恢复准确原生会话失败，未创建替代会话: %w", err)
		}
		if resumed.Thread.ID != id {
			return fmt.Errorf("原生恢复返回了其他会话")
		}
		if id == r.Current {
			e.mu.Lock()
			err := e.mutateLocked(func() error {
				e.record.Model, e.record.ModelProvider, e.record.ReasoningEffort = resumed.Model, resumed.ModelProvider, resumed.ReasoningEffort
				return nil
			})
			e.mu.Unlock()
			if err != nil {
				return err
			}
		}
		if err := r.Settings.confirm(resumed.Model, resumed.ModelProvider, resumed.ReasoningEffort); err != nil {
			return err
		}
	}
	return e.reconcileRuns(ctx)
}

func (e *Engine) reconcileRuns(ctx context.Context) error {
	e.mu.Lock()
	p, r := e.runtime, clone(e.record)
	e.mu.Unlock()
	for _, run := range r.Runs {
		if !activeRun(run) && !uncertainInputs(run) {
			continue
		}
		cursor := ""
		found := false
		for page := 0; page < 100; page++ {
			params := map[string]any{"threadId": run.ThreadID, "limit": 50, "itemsView": "full", "sortDirection": "desc"}
			if cursor != "" {
				params["cursor"] = cursor
			}
			var history struct {
				Data []nativeTurn `json:"data"`
				Next string       `json:"nextCursor"`
			}
			if err := p.Call(ctx, "thread/turns/list", params, &history); err != nil {
				return err
			}
			for _, turn := range history.Data {
				matches := run.TurnID != "" && run.TurnID == turn.ID
				if run.TurnID == "" {
					for _, item := range turn.Items {
						var v struct {
							Type string `json:"type"`
						}
						_ = json.Unmarshal(item, &v)
						if v.Type == "userMessage" && strings.Contains(itemText(item), "[teamcross-shadow-run:"+run.ID+"]") {
							matches = true
						}
					}
				}
				if !matches {
					continue
				}
				found = true
				e.mu.Lock()
				err := e.mutateLocked(func() error {
					current := e.record.Runs[run.ID]
					if !activeRun(current) && !uncertainInputs(current) {
						return nil
					}
					current.TurnID, current.State, current.Output, current.Items = turn.ID, "submitted", "", map[string]string{}
					e.acceptInputHistoryLocked(&current, turn)
					for _, raw := range turn.Items {
						if err := e.archiveLocked(run.ThreadID, turn.ID, raw); err != nil {
							return err
						}
						addOutput(&current, raw)
					}
					e.record.Runs[run.ID] = current
					if turn.Status != "inProgress" {
						e.finishRunLocked(run.ID, turn.Status, string(turn.Error))
					}
					return nil
				})
				e.mu.Unlock()
				if err != nil {
					return err
				}
				break
			}
			if found || history.Next == "" || history.Next == cursor {
				break
			}
			cursor = history.Next
		}
		if !found {
			e.mu.Lock()
			err := e.mutateLocked(func() error {
				current := e.record.Runs[run.ID]
				current.State, current.Error = "unknown", "原生记录未确认该输入；保留原 ID，不自动重发"
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

func addOutput(run *Run, item json.RawMessage) {
	var x struct{ ID, Type, Phase string }
	_ = json.Unmarshal(item, &x)
	if x.Type != "agentMessage" || x.Phase == "commentary" {
		return
	}
	if run.Items == nil {
		run.Items = map[string]string{}
	}
	text := itemText(item)
	if old, exists := run.Items[x.ID]; exists && old == text {
		return
	}
	run.Items[x.ID] = text
	if run.Output != "" {
		run.Output += "\n"
	}
	run.Output += text
}

func (e *Engine) drainEvents(ctx context.Context) {
	e.mu.Lock()
	pending := e.events
	e.events = nil
	e.mu.Unlock()
	for _, event := range pending {
		e.mu.Lock()
		current := event.generation == e.generation
		e.mu.Unlock()
		if !current {
			continue
		}
		if event.message.Method == "item/tool/call" && len(event.message.ID) > 0 {
			e.dispatchTool(ctx, event)
			continue
		}
		if err := e.handleEvent(ctx, event.message); err != nil {
			e.mu.Lock()
			_ = e.mutateLocked(func() error { e.record.Error = err.Error(); e.record.Enabled = false; return nil })
			e.mu.Unlock()
		}
	}
}

func (e *Engine) handleEvent(ctx context.Context, m nativecodex.Message) error {
	if m.Method == "teamcross/reconcile" {
		return e.reconcileRuns(ctx)
	}
	if m.Method == "item/tool/call" && len(m.ID) > 0 {
		return e.handleTool(ctx, m)
	}
	e.mu.Lock()
	if m.Method == "teamcross/runtimeDisconnected" {
		p := e.runtime
		e.runtime = nil
		e.generation++
		err := e.mutateLocked(func() error {
			for id, run := range e.record.Runs {
				if activeRun(run) {
					run.State = "unknown"
					e.record.Runs[id] = run
				}
			}
			e.record.Error = "原生连接已断开，恢复时核对原会话与已提交输入"
			return nil
		})
		e.mu.Unlock()
		if p != nil {
			p.Close()
		}
		return err
	}
	defer e.mu.Unlock()
	if len(m.ID) > 0 {
		e.approvals[string(m.ID)] = Approval{ID: m.ID, Method: m.Method, Params: m.Params}
		return nil
	}
	var p struct {
		ThreadID   string          `json:"threadId"`
		TurnID     string          `json:"turnId"`
		Turn       nativeTurn      `json:"turn"`
		Item       json.RawMessage `json:"item"`
		RequestID  json.RawMessage `json:"requestId"`
		TokenUsage struct {
			Last struct {
				Input  int64 `json:"inputTokens"`
				Output int64 `json:"outputTokens"`
			} `json:"last"`
			Window int64 `json:"modelContextWindow"`
		} `json:"tokenUsage"`
	}
	if err := json.Unmarshal(m.Params, &p); err != nil {
		return nil
	}
	if m.Method == "serverRequest/resolved" {
		delete(e.approvals, string(p.RequestID))
		return nil
	}
	if p.ThreadID == "" {
		return nil
	}
	if m.Method == "item/completed" {
		if err := e.archiveLocked(p.ThreadID, p.TurnID, p.Item); err != nil {
			return err
		}
	}
	switch m.Method {
	case "thread/tokenUsage/updated":
		if p.ThreadID != e.record.Current {
			return nil
		}
		return e.mutateLocked(func() error {
			if s := e.currentSegmentLocked(); s != nil {
				s.InputTokens = p.TokenUsage.Last.Input + p.TokenUsage.Last.Output
				s.UsageValid = true
				if p.TokenUsage.Window > 0 {
					s.ContextWindow = p.TokenUsage.Window
				}
			}
			return nil
		})
	case "item/started", "item/completed":
		var item struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(p.Item, &item)
		if item.Type == "contextCompaction" {
			e.compacting[p.ThreadID] = m.Method == "item/started"
			if m.Method == "item/completed" && p.ThreadID == e.record.Current {
				return e.mutateLocked(func() error {
					if s := e.currentSegmentLocked(); s != nil {
						s.Compactions++
						s.LastCheckTokens = 0
						// Pre-compaction usage is not the size of the new summary.
						// Wait for fresh native usage instead of compacting in a loop.
						s.InputTokens = 0
						s.UsageValid = false
					}
					return nil
				})
			}
		}
		if m.Method == "item/started" {
			return nil
		}
	case "turn/started", "turn/completed":
		if p.Turn.ID != "" {
			p.TurnID = p.Turn.ID
		}
	default:
		return nil
	}
	for id, run := range e.record.Runs {
		if run.ThreadID != p.ThreadID || !activeRun(run) || (run.TurnID != "" && run.TurnID != p.TurnID) {
			continue
		}
		return e.mutateLocked(func() error {
			run.TurnID = p.TurnID
			if m.Method == "item/completed" {
				addOutput(&run, p.Item)
			}
			if m.Method == "turn/started" {
				run.State = "submitted"
			}
			e.record.Runs[id] = run
			if m.Method == "turn/completed" {
				for _, item := range p.Turn.Items {
					addOutput(&run, item)
				}
				e.record.Runs[id] = run
				e.finishRunLocked(id, p.Turn.Status, string(p.Turn.Error))
			}
			return nil
		})
	}
	return nil
}

func (e *Engine) finishRunLocked(id, status, failure string) {
	run := e.record.Runs[id]
	if !activeRun(run) {
		return
	}
	if run.JobID != "" {
		if job, changed := e.refreshedTaskFollowLocked(e.record.Jobs[run.JobID]); changed {
			e.record.Jobs[job.ID] = job
		}
	}
	run.State = "completed"
	if status != "completed" {
		run.State, run.Error = "failed", failure
		if run.Error == "" || run.Error == "null" {
			run.Error = status
		}
	}
	e.record.Runs[id] = run
	if run.Kind == "steward" || run.Kind == "memory" || run.Kind == "attention" {
		e.releaseThreads = append(e.releaseThreads, run.ThreadID)
	}
	e.lastActivity = time.Now()
	switch run.Kind {
	case "main", "continuation", "result":
		if len(run.InputBatches) > 0 {
			e.setInputMessageStateLocked(runInputs(run), run.State, run.ThreadID, run.TurnID, run.Error)
		} else if run.Kind == "main" {
			for i := range e.record.Messages {
				if e.record.Messages[i].ID == run.SourceID {
					e.record.Messages[i].State, e.record.Messages[i].TurnID, e.record.Messages[i].Error = run.State, run.TurnID, run.Error
				}
			}
		}
		job := e.record.Jobs[run.JobID]
		publish := run.Kind == "main" || job.Follow == nil && (job.Trigger == nil || job.Trigger.Kind != "activity") && job.Version == run.JobVersion && job.State != "stopping" && job.State != "cancelled"
		if len(run.InputBatches) > 0 {
			inputs := runInputs(run)
			publish = len(inputs) == 1 && inputs[0].Kind == "main" && !run.ExplicitReply && !uncertainInputs(run)
			if publish {
				run.SourceID = inputs[0].SourceID
			}
		} else if run.Kind == "continuation" {
			publish = false
		}
		if run.Output != "" && publish {
			message := Message{ID: "reply-" + run.ID, Actor: Actor{ID: e.record.ID, Name: e.record.Name}, Role: "assistant", Text: run.Output, State: run.State, ReplyTo: run.SourceID, SegmentID: run.ThreadID, TurnID: run.TurnID, CreatedAt: time.Now()}
			if run.Kind == "result" {
				message.TaskResultID = run.EventID
			} else {
				if e.record.ReplyOutbox == nil {
					e.record.ReplyOutbox = map[string]Message{}
				}
				e.record.ReplyOutbox[message.ID] = message
			}
			e.record.Messages = append(e.record.Messages, message)
		}
		if s := e.currentSegmentLocked(); s != nil && s.ID == run.ThreadID {
			s.MemoryRevision = run.MemoryRevision
		}
		if run.Kind == "result" {
			j := e.record.Jobs[run.JobID]
			if j.State == "stopping" && j.StopReason == "follow" && j.Follow != nil && !e.taskControlRunActiveLocked(j.ID) {
				settleTaskFollow(&j)
				j.Revision++
				j.UpdatedAt = time.Now()
				e.record.Jobs[j.ID] = j
			}
			// Consume a failed notification too: never repeat it automatically.
			if j.ResultEventID == run.EventID {
				j.DeliveredEventID = run.EventID
				e.record.Jobs[j.ID] = j
			}
		}
	case "job":
		e.finishTaskRunLocked(run, status)
	case "steward":
		e.finishStewardLocked(run)
	case "handoff":
		if e.record.Transition == nil || e.record.Transition.From != run.ThreadID {
			break
		}
		if run.State != "completed" {
			e.record.Error = "交接笔记生成失败，保留原段和未完成事项"
			if e.record.Transition.Reason == "native-tools-updated" {
				e.record.Enabled = false
			}
			e.record.Transition = nil
			break
		}
		e.record.Transition.Checkpoint, e.record.Transition.Stage = run.Output, "create-prepared"
	case "memory":
		e.finishMemoryLocked(run)
	case "bootstrap":
		if e.record.Transition != nil && e.record.Transition.To == run.ThreadID {
			if run.State == "completed" && strings.TrimSpace(run.Output) == "HANDOFF_READY" {
				e.record.Transition.Stage = "commit"
			} else {
				e.record.Error = "新段接手失败，保留原段和已接收消息"
				e.record.Enabled = false
			}
		}
	}
}

func (e *Engine) archiveLocked(threadID, turnID string, item json.RawMessage) error {
	if !validID(threadID) {
		return nil
	}
	dir := filepath.Join(e.cfg.Directory, "history")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, threadID+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(map[string]any{"threadId": threadID, "turnId": turnID, "item": item, "at": time.Now()})
	if err != nil {
		return err
	}
	if _, err = f.Write(append(line, '\n')); err != nil {
		return err
	}
	return f.Sync()
}
