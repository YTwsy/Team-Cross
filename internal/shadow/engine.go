package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/nativecodex"
)

// UpgradeBusy protects accepted and live native work without treating an idle
// runtime or an offline unknown receipt as a permanently running task.
func (e *Engine) UpgradeBusy() bool {
	if !e.mu.TryLock() {
		return true
	}
	defer e.mu.Unlock()
	if e.runtime != nil && e.runtime.Alive() {
		if len(e.approvals) > 0 {
			return true
		}
		for _, busy := range e.compacting {
			if busy {
				return true
			}
		}
		for _, run := range e.record.Runs {
			if activeRun(run) {
				return true
			}
		}
	}
	if e.record.Enabled && !e.closed {
		if e.attention != nil && e.attention.HasPending(e.record.ID, time.Now()) {
			return true
		}
		if e.record.Creating != "" || e.record.Transition != nil {
			return true
		}
		for _, m := range e.record.Messages {
			if m.Role == "user" && m.State == "delivered" {
				return true
			}
		}
		for _, job := range e.record.Jobs {
			if job.State == "queued" || job.State == "running" || job.State == "stopping" {
				return true
			}
		}
	}
	return false
}

func (e *Engine) Start() {
	e.mu.Lock()
	if e.started || e.closed {
		e.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel, e.started = cancel, true
	e.mu.Unlock()
	go e.loop(ctx)
}

func (e *Engine) Close() {
	e.mu.Lock()
	if e.closed {
		done, started := e.done, e.started
		e.mu.Unlock()
		if started {
			<-done
		}
		return
	}
	e.closed = true
	started, cancel := e.started, e.cancel
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if started {
		<-e.done
	}
}

func (e *Engine) loop(ctx context.Context) {
	defer close(e.done)
	defer e.dependencyWait.Wait()
	defer e.attentionWait.Wait()
	defer e.toolWait.Wait()
	defer e.reportWait.Wait()
	defer func() {
		e.mu.Lock()
		p := e.runtime
		e.runtime = nil
		e.generation++
		_ = e.mutateLocked(func() error {
			for id, r := range e.record.Runs {
				if activeRun(r) {
					r.State = "unknown"
					e.record.Runs[id] = r
				}
			}
			return nil
		})
		e.mu.Unlock()
		if p != nil {
			p.Close()
		}
	}()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-e.wake:
		}
		e.drainEvents(ctx)
		e.mu.Lock()
		release, p := e.releaseThreads, e.runtime
		e.releaseThreads = nil
		e.mu.Unlock()
		if p != nil {
			for _, id := range release {
				_ = p.Call(ctx, "thread/unsubscribe", map[string]any{"threadId": id}, nil)
			}
		}
		if ctx.Err() != nil {
			return
		}
		if err := e.step(ctx); err != nil {
			e.mu.Lock()
			_ = e.mutateLocked(func() error { e.record.Error = err.Error(); e.record.Enabled = false; return nil })
			e.mu.Unlock()
		}
	}
}

func (e *Engine) startRuntime(ctx context.Context) error {
	e.mu.Lock()
	if e.runtime != nil && e.runtime.Alive() {
		e.mu.Unlock()
		return nil
	}
	old := e.runtime
	e.runtime = nil
	e.generation++
	generation := e.generation
	home, cwd := e.record.Home, e.record.Cwd
	e.mu.Unlock()
	if old != nil {
		old.Close()
	}
	if e.cfg.Start == nil {
		return fmt.Errorf("Codex 运行入口尚未配置")
	}
	p, err := e.cfg.Start(ctx, home, cwd, filepath.Join(e.cfg.Directory, "native.log"))
	if err != nil {
		return err
	}
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		p.Close()
		return ctx.Err()
	}
	e.runtime = p
	e.approvals = map[string]Approval{}
	e.mu.Unlock()
	p.SetHandler(func(m nativecodex.Message) {
		e.mu.Lock()
		if !e.closed && e.generation == generation {
			e.events = append(e.events, nativeEvent{generation, m})
			e.notify()
		}
		e.mu.Unlock()
	})
	if err := e.restore(ctx); err != nil {
		// A retry must resume and confirm settings again, never reuse a partially
		// restored process merely because it is still alive.
		p.Close()
		return err
	}
	return nil
}

func (e *Engine) step(ctx context.Context) error {
	e.scheduleReportDelivery(ctx)
	e.refreshQuestionHandling(ctx)
	if err := e.syncTaskFollows(); err != nil {
		return err
	}
	if err := e.flushAttentionResults(); err != nil {
		return err
	}
	e.mu.Lock()
	enabled, settings := e.record.Enabled, e.record.Settings
	e.mu.Unlock()
	if !enabled {
		return e.scheduleJobStops(ctx)
	}
	e.refreshTaskDependencies(ctx)
	e.refreshTaskSources(ctx)
	if err := settings.Validate(); err != nil {
		return err
	}
	if err := e.startRuntime(ctx); err != nil {
		return err
	}
	if err := e.scheduleJobStops(ctx); err != nil {
		return err
	}
	e.mu.Lock()
	r := clone(e.record)
	e.mu.Unlock()
	if r.Creating != "" {
		return fmt.Errorf("原生会话创建结果仍需核对")
	}
	if r.Current == "" {
		e.mu.Lock()
		store := e.attention
		e.mu.Unlock()
		pending := store != nil && e.cfg.CallTool != nil && store.HasContinuation(r.ID)
		if len(r.Messages) == 0 && !pending {
			return e.scheduleAttention(ctx)
		}
		_, err := e.createThread(ctx, "main", "")
		return err
	}
	// Background state transitions never block the main thread waiting on a
	// model result. Native RPC acknowledgement itself is bounded by the driver.
	if err := e.scheduleJobs(ctx); err != nil {
		return err
	}
	e.mu.Lock()
	r = clone(e.record)
	busy := e.threadBusyLocked(r.Current)
	e.mu.Unlock()
	if busy {
		if sent, err := e.scheduleInputs(ctx); sent || err != nil {
			return err
		}
		return e.scheduleAttention(ctx)
	}
	if r.Transition != nil {
		return e.advanceTransition(ctx)
	}
	if started, err := e.beginToolsetUpgrade(); started || err != nil {
		return err
	}
	// Budget checks run at a turn boundary even when more member messages are
	// queued. Near the reserved limit, hold new input until maintenance settles.
	if err := e.maintenance(ctx); err != nil {
		return err
	}
	e.mu.Lock()
	r = clone(e.record)
	hold := r.Transition != nil || e.threadBusyLocked(r.Current)
	if s := e.currentSegmentLocked(); s != nil && s.ContextWindow > 0 && s.InputTokens >= s.ContextWindow*7/8 {
		hold = true
	}
	e.mu.Unlock()
	if hold {
		return e.scheduleAttention(ctx)
	}
	if sent, err := e.scheduleInputs(ctx); sent || err != nil {
		return err
	}
	return e.scheduleAttention(ctx)
}

func (e *Engine) threadBusyLocked(threadID string) bool {
	if e.compacting[threadID] {
		return true
	}
	for _, r := range e.record.Runs {
		if r.ThreadID == threadID && (activeRun(r) || uncertainInputs(r)) {
			return true
		}
	}
	for _, approval := range e.approvals {
		var params struct {
			ThreadID string `json:"threadId"`
		}
		_ = json.Unmarshal(approval.Params, &params)
		if params.ThreadID == threadID {
			return true
		}
	}
	return false
}

func (e *Engine) currentSegmentLocked() *Segment {
	for i := range e.record.Segments {
		if e.record.Segments[i].ID == e.record.Current {
			return &e.record.Segments[i]
		}
	}
	return nil
}

func (e *Engine) createThread(ctx context.Context, kind, jobID string) (string, error) {
	nativeTools, toolsetDigest, err := e.nativeTools()
	if err != nil {
		return "", err
	}
	e.mu.Lock()
	if e.record.Creating != "" {
		e.mu.Unlock()
		return "", fmt.Errorf("已有原生创建等待核对")
	}
	p, cwd, settings := e.runtime, e.record.Cwd, e.record.Settings
	previous := ""
	if kind == "job-upgrade" {
		job := e.record.Jobs[jobID]
		if job.State != "queued" || job.ThreadID == "" || e.threadBusyLocked(job.ThreadID) {
			e.mu.Unlock()
			return "", fmt.Errorf("任务尚未到达可升级的原生轮次边界")
		}
		previous = job.ThreadID
	}
	err = e.mutateLocked(func() error { e.record.Creating = kind + ":" + jobID; return nil })
	e.mu.Unlock()
	if err != nil {
		return "", err
	}
	params := settings.threadParams(map[string]any{"cwd": cwd, "serviceName": "teamcross_shadow", "dynamicTools": nativeTools})
	var result struct {
		Model           string `json:"model"`
		ModelProvider   string `json:"modelProvider"`
		ReasoningEffort string `json:"reasoningEffort"`
		Thread          struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err = p.Call(ctx, "thread/start", params, &result); err != nil {
		return "", fmt.Errorf("原生会话创建需核对: %w", err)
	}
	if !validID(result.Thread.ID) {
		return "", fmt.Errorf("Codex 未返回有效的原生会话 ID")
	}
	id := result.Thread.ID
	confirmation := settings.confirm(result.Model, result.ModelProvider, result.ReasoningEffort)
	e.mu.Lock()
	err = e.mutateLocked(func() error {
		e.record.Creating = ""
		if confirmation != nil {
			// Keep the exact created ID even for auxiliary threads and prevent a new
			// creation or input after a native refusal/normalization of the selection.
			e.record.Creating = kind + ":" + jobID + ":" + id
		}
		switch kind {
		case "attention":
			e.record.AttentionThreads[jobID] = id
			e.record.Model, e.record.ModelProvider, e.record.ReasoningEffort = result.Model, result.ModelProvider, result.ReasoningEffort
		case "main":
			e.record.Model, e.record.ModelProvider, e.record.ReasoningEffort = result.Model, result.ModelProvider, result.ReasoningEffort
			e.record.Current = id
			e.record.Segments = append(e.record.Segments, Segment{ID: id, ToolsetDigest: toolsetDigest, Number: len(e.record.Segments) + 1, CreatedAt: time.Now()})
		case "segment":
			if e.record.Transition == nil {
				return fmt.Errorf("交接记录丢失")
			}
			e.record.Transition.To, e.record.Transition.Stage = id, "ready"
			e.record.Transition.ToolsetDigest = toolsetDigest
			e.record.Transition.Model, e.record.Transition.ModelProvider, e.record.Transition.ReasoningEffort = result.Model, result.ModelProvider, result.ReasoningEffort
		case "job", "job-upgrade":
			j := e.record.Jobs[jobID]
			if previous != "" {
				j.PreviousThreads = append(j.PreviousThreads, previous)
			}
			j.ThreadID, j.ToolsetDigest = id, toolsetDigest
			e.record.Jobs[jobID] = j
		}
		return nil
	})
	if err == nil && confirmation == nil && previous != "" {
		e.releaseThreads = append(e.releaseThreads, previous)
	}
	e.mu.Unlock()
	if err != nil {
		return id, err
	}
	return id, confirmation
}

func (e *Engine) submit(ctx context.Context, run Run, prompt string, schema any) error {
	run.ID, run.State, run.CreatedAt, run.Items = uuid.NewString(), "prepared", time.Now(), map[string]string{}
	e.mu.Lock()
	p, settings := e.runtime, e.record.Settings
	if e.threadBusyLocked(run.ThreadID) {
		e.mu.Unlock()
		return nil
	}
	if run.Kind == "job" {
		job := e.record.Jobs[run.JobID]
		if job.Version != run.JobVersion || job.State != "queued" || e.validateTaskFollowLocked(job) != nil {
			// A concurrent owner goal edit is normal scheduling contention. Let
			// the next durable follow sync choose its state instead of disabling
			// the whole shadow or submitting the obsolete prompt.
			e.mu.Unlock()
			e.notify()
			return nil
		}
		// Freeze the actual facts included in this native input. A later fact
		// change can trigger another turn; an ignored old packet cannot loop.
		prompt = e.workerPromptLocked(job)
		run.ContextInputs = map[string]string{}
		for _, linked := range job.Contexts {
			run.ContextInputs[linked.ID] = linked.Current.Fingerprint
			run.ContextInputs["source:"+linked.ID] = contextSourceInput(linked)
		}
	}
	if run.Kind == "result" {
		job := e.record.Jobs[run.JobID]
		if job.Version != run.JobVersion || job.ResultEventID != run.EventID || job.ResultEventID == job.DeliveredEventID || e.validateTaskFollowLocked(job) != nil {
			e.mu.Unlock()
			e.notify()
			return nil
		}
	}
	run.MemoryRevision = e.record.MemoryRevision
	err := e.mutateLocked(func() error {
		e.record.Runs[run.ID] = run
		if run.Kind == "job" {
			j := e.record.Jobs[run.JobID]
			if j.Version != run.JobVersion || j.State != "queued" {
				return fmt.Errorf("任务版本或状态已变化")
			}
			j.State, j.RunningVersion, j.StopReason = "running", run.JobVersion, ""
			j.ThreadID, j.DecisionRunID = run.ThreadID, ""
			j.Revision++
			e.record.Jobs[j.ID] = j
		}
		if len(run.InputBatches) > 0 {
			for _, batch := range run.InputBatches {
				e.setInputMessageStateLocked(batch.Inputs, "submitted", run.ThreadID, "", "")
			}
		} else if run.Kind == "main" {
			for i := range e.record.Messages {
				if e.record.Messages[i].ID == run.SourceID {
					e.record.Messages[i].State, e.record.Messages[i].SegmentID = "submitted", run.ThreadID
				}
			}
		}
		return nil
	})
	e.mu.Unlock()
	if err != nil {
		return err
	}
	text := "[teamcross-shadow-run:" + run.ID + "]\n" + prompt
	params := map[string]any{"model": settings.Model, "effort": settings.ReasoningEffort, "threadId": run.ThreadID, "input": []any{map[string]any{"type": "text", "text": text}}}
	if schema != nil {
		params["outputSchema"] = schema
	}
	var response struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	err = p.Call(ctx, "turn/start", params, &response)
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.mutateLocked(func() error {
		stored := e.record.Runs[run.ID]
		if err != nil || response.Turn.ID == "" {
			stored.State, stored.Error = "unknown", "原生输入结果需核对，不自动重发"
			if len(stored.InputBatches) > 0 {
				for i := range stored.InputBatches {
					b := &stored.InputBatches[i]
					b.State, b.Error = "unknown", stored.Error
					e.setInputMessageStateLocked(b.Inputs, "unknown", run.ThreadID, "", stored.Error)
				}
			} else if run.Kind == "main" {
				for i := range e.record.Messages {
					if e.record.Messages[i].ID == run.SourceID {
						e.record.Messages[i].State, e.record.Messages[i].Error = "unknown", stored.Error
					}
				}
			}
		} else {
			stored.State, stored.TurnID = "submitted", response.Turn.ID
			for i := range stored.InputBatches {
				b := &stored.InputBatches[i]
				b.State, b.TurnID = "accepted", response.Turn.ID
				e.setInputMessageStateLocked(b.Inputs, "submitted", run.ThreadID, response.Turn.ID, "")
			}
		}
		e.record.Runs[run.ID] = stored
		return nil
	})
}

func (e *Engine) Approvals() []Approval {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []Approval{}
	for _, a := range e.approvals {
		out = append(out, clone(a))
	}
	return out
}

func (e *Engine) Respond(ctx context.Context, id json.RawMessage, result any) error {
	e.mu.Lock()
	_, ok := e.approvals[string(id)]
	p := e.runtime
	e.mu.Unlock()
	if !ok || p == nil {
		return fmt.Errorf("该原生请求已结束或不属于当前分身")
	}
	return p.Reply(ctx, id, result)
}

func (e *Engine) Reconcile(ctx context.Context) error {
	e.mu.Lock()
	online := e.runtime != nil && e.runtime.Alive()
	e.mu.Unlock()
	if !online {
		return fmt.Errorf("请先恢复分身运行时")
	}
	// Queue on the engine loop so recovery cannot race a new turn or rollover.
	e.mu.Lock()
	e.events = append(e.events, nativeEvent{e.generation, nativecodex.Message{Method: "teamcross/reconcile"}})
	e.mu.Unlock()
	e.notify()
	return nil
}

// Suspend stops this shadow's owned process after membership or the space ends.
// Submitted work stays unknown until native history confirms its outcome.
func (e *Engine) Suspend() error {
	if err := e.SetEnabled(false); err != nil {
		return err
	}
	e.mu.Lock()
	e.events = append(e.events, nativeEvent{e.generation, nativecodex.Message{Method: "teamcross/runtimeDisconnected"}})
	e.mu.Unlock()
	e.notify()
	return nil
}
