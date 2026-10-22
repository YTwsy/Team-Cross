package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/nativecodex"
)

type fakeCall struct {
	Method string
	Params map[string]any
}

func TestMemoryCorrectionsForgetAndStaleConsolidation(t *testing.T) {
	e, f, _ := setup(t)
	e.Start()
	send(t, e, "B", "remember concise responses")
	run := active(t, e, "main")
	memory := cloneMap(tool(t, e, f, run, "shadow_remember", map[string]any{"operation": "upsert", "scope": "member", "text": "concise"}))
	id := memory["id"].(string)
	rev := int(memory["revision"].(float64))
	f.complete(run.ThreadID, "remembered", "completed", true)
	until(t, func() bool { return !e.View().Busy })
	send(t, e, "C", "try to change B preference")
	run = active(t, e, "main")
	args, _ := json.Marshal(map[string]any{"operation": "upsert", "scope": "member", "id": id, "baseRevision": rev, "text": "verbose"})
	e.mu.Lock()
	_, err := e.toolLocked(toolCall{ThreadID: run.ThreadID, TurnID: run.TurnID, CallID: uuid.NewString(), Tool: "shadow_remember", Arguments: args})
	e.mu.Unlock()
	if err == nil {
		t.Fatal("member C changed B preference")
	}
	f.complete(run.ThreadID, "not your preference", "completed", true)
	until(t, func() bool { return !e.View().Busy })
	send(t, e, "B", "correct preference")
	run = active(t, e, "main")
	memory = cloneMap(tool(t, e, f, run, "shadow_remember", map[string]any{"operation": "upsert", "scope": "member", "id": id, "baseRevision": rev, "text": "detailed"}))
	newRev := int(memory["revision"].(float64))
	if newRev <= rev {
		t.Fatal(memory)
	}
	e.mu.Lock()
	e.finishMemoryLocked(Run{State: "completed", MemoryRevision: rev, MemoryWatermark: 2, Output: `{"candidates":[]}`})
	watermark := e.record.MemoryWatermark
	e.mu.Unlock()
	if watermark != 0 {
		t.Fatal("stale consolidation advanced watermark")
	}
	tool(t, e, f, run, "shadow_remember", map[string]any{"operation": "forget", "scope": "member", "id": id, "baseRevision": newRev})
	if m := e.View().Memories[0]; m.State != "forgotten" || m.Text != "" {
		t.Fatal(m)
	}
	f.complete(run.ThreadID, "forgotten", "completed", true)
	until(t, func() bool { return !e.View().Busy })
	msg := send(t, e, "B", "next message")
	if prompt := e.messagePrompt(msg); !strings.Contains(prompt, `"state":"forgotten"`) {
		t.Fatal("no invalidation in next prompt", prompt)
	}
}

func TestBackgroundUpdateAndCancelSuppressObsoleteCompletion(t *testing.T) {
	e, f, _ := setup(t)
	e.Start()
	send(t, e, "B", "background version one")
	run := active(t, e, "main")
	created := cloneMap(tool(t, e, f, run, "shadow_task", map[string]any{"operation": "start", "title": "investigate", "prompt": "version one"}))
	id := created["id"].(string)
	first := active(t, e, "job")
	f.mu.Lock()
	f.holdInterrupt = true
	f.mu.Unlock()
	tool(t, e, f, run, "shadow_task", map[string]any{"operation": "update", "id": id, "baseVersion": 1, "prompt": "version two"})
	if e.View().Jobs[0].State != "stopping" {
		t.Fatal(e.View().Jobs)
	}
	f.complete(first.ThreadID, "obsolete result", "completed", true)
	until(t, func() bool { j := e.View().Jobs[0]; return j.State == "running" && j.RunningVersion == 2 })
	if j := e.View().Jobs[0]; j.Result != "" || j.ResultEventID != "" {
		t.Fatal("old result published", j)
	}
	tool(t, e, f, run, "shadow_task", map[string]any{"operation": "cancel", "id": id, "baseVersion": 2})
	f.complete(first.ThreadID, "late result after cancel", "completed", true)
	until(t, func() bool { return e.View().Jobs[0].State == "cancelled" })
	if j := e.View().Jobs[0]; j.Result != "" {
		t.Fatal("cancelled result published", j)
	}
}

func TestReceiveDoesNotAcknowledgeFailedPersistence(t *testing.T) {
	e, _, cfg := setup(t)
	if err := os.Rename(cfg.Directory, cfg.Directory+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.Directory, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	m := Message{ID: uuid.NewString(), Actor: Actor{ID: "B"}, Text: "must persist"}
	if _, err := e.Receive(m); err == nil {
		t.Fatal("acknowledged failed write")
	}
	if len(e.View().Messages) != 0 {
		t.Fatal("failed write remained in memory")
	}
	if err := os.Remove(cfg.Directory); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(cfg.Directory+"-saved", cfg.Directory); err != nil {
		t.Fatal(err)
	}
	if got, err := e.Receive(m); err != nil || got.ReceivedAt == nil {
		t.Fatal(got, err)
	}
}

type fakeNative struct {
	mu            sync.Mutex
	alive         bool
	handler       func(nativecodex.Message)
	threads       map[string][]nativeTurn
	calls         []fakeCall
	replies       map[string]any
	loseTurnReply bool
	holdInterrupt bool
	wrongSettings bool
}

func newFake() *fakeNative {
	return &fakeNative{alive: true, threads: map[string][]nativeTurn{}, replies: map[string]any{}}
}
func (f *fakeNative) Alive() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.alive }
func (f *fakeNative) Close()      { f.mu.Lock(); f.alive = false; f.handler = nil; f.mu.Unlock() }
func (f *fakeNative) SetHandler(h func(nativecodex.Message)) {
	f.mu.Lock()
	f.handler = h
	f.mu.Unlock()
}
func (f *fakeNative) emit(method string, params any) {
	b, _ := json.Marshal(params)
	f.mu.Lock()
	h := f.handler
	f.mu.Unlock()
	if h != nil {
		h(nativecodex.Message{Method: method, Params: b})
	}
}
func (f *fakeNative) Reply(_ context.Context, id json.RawMessage, result any) error {
	f.mu.Lock()
	f.replies[string(id)] = clone(result)
	f.mu.Unlock()
	return nil
}
func (f *fakeNative) Call(_ context.Context, method string, params, out any) error {
	p := cloneMap(params)
	f.mu.Lock()
	f.calls = append(f.calls, fakeCall{method, p})
	var result any
	id, _ := p["threadId"].(string)
	switch method {
	case "thread/start":
		id = uuid.NewString()
		f.threads[id] = []nativeTurn{}
		result = map[string]any{"thread": map[string]any{"id": id}, "model": p["model"], "modelProvider": p["modelProvider"], "reasoningEffort": p["config"].(map[string]any)["model_reasoning_effort"]}
		if f.wrongSettings {
			result.(map[string]any)["model"] = "unexpected-default"
		}
	case "thread/resume":
		if _, ok := f.threads[id]; !ok {
			f.mu.Unlock()
			return fmt.Errorf("missing thread")
		}
		result = map[string]any{"thread": map[string]any{"id": id}, "model": p["model"], "modelProvider": p["modelProvider"], "reasoningEffort": p["config"].(map[string]any)["model_reasoning_effort"]}
		if f.wrongSettings {
			result.(map[string]any)["model"] = "unexpected-default"
		}
	case "turn/start":
		if _, ok := f.threads[id]; !ok {
			f.mu.Unlock()
			return fmt.Errorf("missing thread")
		}
		text := p["input"].([]any)[0].(map[string]any)["text"].(string)
		item, _ := json.Marshal(map[string]any{"id": uuid.NewString(), "type": "userMessage", "content": []any{map[string]any{"type": "text", "text": text}}})
		turn := nativeTurn{ID: uuid.NewString(), Status: "inProgress", Items: []json.RawMessage{item}}
		f.threads[id] = append(f.threads[id], turn)
		result = map[string]any{"turn": turn}
		lose := f.loseTurnReply
		f.loseTurnReply = false
		f.mu.Unlock()
		f.emit("turn/started", map[string]any{"threadId": id, "turn": turn})
		if strings.Contains(text, "Prepare a handoff checkpoint") {
			go f.complete(id, "Checkpoint: retain all original constraints, topics and jobs.", "completed", true)
		}
		if strings.Contains(text, "reply only HANDOFF_READY") {
			go f.complete(id, "HANDOFF_READY", "completed", true)
		}
		if lose {
			return fmt.Errorf("lost acknowledgement")
		}
		return assign(result, out)
	case "thread/turns/list":
		turns := clone(f.threads[id])
		for i, j := 0, len(turns)-1; i < j; i, j = i+1, j-1 {
			turns[i], turns[j] = turns[j], turns[i]
		}
		result = map[string]any{"data": turns}
	case "turn/interrupt":
		hold := f.holdInterrupt
		f.mu.Unlock()
		if !hold {
			go f.complete(id, "", "interrupted", true)
		}
		return nil
	case "thread/unsubscribe":
		result = map[string]any{}
	default:
		f.mu.Unlock()
		return fmt.Errorf("unexpected method %s", method)
	}
	f.mu.Unlock()
	return assign(result, out)
}
func cloneMap(v any) map[string]any { var out map[string]any; _ = assign(v, &out); return out }
func assign(v, out any) error {
	if out == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return json.Unmarshal(b, out)
}
func (f *fakeNative) complete(id, text, status string, emit bool) {
	f.mu.Lock()
	turns := f.threads[id]
	if len(turns) == 0 {
		f.mu.Unlock()
		return
	}
	turn := turns[len(turns)-1]
	turn.Status = status
	var item json.RawMessage
	if text != "" {
		item, _ = json.Marshal(map[string]any{"id": uuid.NewString(), "type": "agentMessage", "phase": "final_answer", "text": text})
		turn.Items = append(turn.Items, item)
	}
	turns[len(turns)-1] = turn
	f.threads[id] = turns
	f.mu.Unlock()
	if emit {
		if item != nil {
			f.emit("item/completed", map[string]any{"threadId": id, "turnId": turn.ID, "item": item})
		}
		f.emit("turn/completed", map[string]any{"threadId": id, "turn": turn})
	}
}

func setup(t *testing.T) (*Engine, *fakeNative, Config) {
	t.Helper()
	f := newFake()
	root := t.TempDir()
	cfg := Config{Directory: filepath.Join(root, "shadow"), IdleMemoryDelay: time.Hour, Start: func(context.Context, string, string, string) (Runtime, error) {
		f.mu.Lock()
		f.alive = true
		f.mu.Unlock()
		return f, nil
	}}
	e, err := New(cfg, uuid.NewString(), "space-test", "Shadow", root, root, Actor{ID: "owner", Name: "Owner"}, ModelSettings{Model: "fixture-model", ModelProvider: "fixture-provider", ReasoningEffort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	return e, f, cfg
}
func until(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition timed out")
}
func send(t *testing.T, e *Engine, actor, text string) Message {
	t.Helper()
	m, err := e.Receive(Message{ID: uuid.NewString(), Actor: Actor{ID: actor, Name: actor}, Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func active(t *testing.T, e *Engine, kind string) Run {
	t.Helper()
	var out Run
	until(t, func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		for _, r := range e.record.Runs {
			if r.Kind == kind && r.State == "submitted" && r.TurnID != "" {
				out = r
				return true
			}
		}
		return false
	})
	return out
}
func tool(t *testing.T, e *Engine, f *fakeNative, run Run, name string, args any) any {
	t.Helper()
	call := toolCall{ThreadID: run.ThreadID, TurnID: run.TurnID, CallID: uuid.NewString(), Tool: name}
	call.Arguments, _ = json.Marshal(args)
	b, _ := json.Marshal(call)
	id, _ := json.Marshal(uuid.NewString())
	f.mu.Lock()
	h := f.handler
	f.mu.Unlock()
	h(nativecodex.Message{ID: id, Method: "item/tool/call", Params: b})
	var reply map[string]any
	until(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		v, ok := f.replies[string(id)]
		if ok {
			reply = cloneMap(v)
		}
		return ok
	})
	if reply["success"] != true {
		t.Fatal(reply)
	}
	text := reply["contentItems"].([]any)[0].(map[string]any)["text"].(string)
	var out any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDeliveryPersistsAndRetryDoesNotDuplicateOrChangeAuthor(t *testing.T) {
	e, _, cfg := setup(t)
	m := send(t, e, "B", "first shared message")
	if _, err := e.Receive(m); err != nil {
		t.Fatal(err)
	}
	changed := m
	changed.Actor.ID = "C"
	if _, err := e.Receive(changed); err == nil {
		t.Fatal("changed author accepted for same message ID")
	}
	e.Close()
	restored, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if v := restored.View(); len(v.Messages) != 1 || v.Messages[0].State != "delivered" {
		t.Fatal(v)
	}
}

func TestSharedConversationMemoryAndBackgroundTaskSurviveRollover(t *testing.T) {
	e, f, _ := setup(t)
	e.Start()
	m := send(t, e, "B", "Remember my release notes use Chinese. Investigate recovery without changing files.")
	main := active(t, e, "main")
	s1 := main.ThreadID
	tool(t, e, f, main, "shadow_remember", map[string]any{"operation": "upsert", "scope": "member", "text": "Release notes default to Chinese."})
	tool(t, e, f, main, "shadow_topics", map[string]any{"id": "recovery", "goal": "Investigate recovery", "state": "active", "constraints": []string{"Do not modify files"}, "sourceIds": []string{m.ID}})
	job := tool(t, e, f, main, "shadow_task", map[string]any{"operation": "start", "title": "Recovery investigation", "prompt": "Investigate recovery, do not modify files."}).(map[string]any)
	worker := active(t, e, "job")
	if worker.ThreadID == s1 {
		t.Fatal("worker shares main thread")
	}
	f.complete(s1, "Investigation started.", "completed", true)
	until(t, func() bool { return !e.View().Busy })
	send(t, e, "C", "Explain the release checklist while the investigation runs.")
	second := active(t, e, "main")
	if second.ThreadID != s1 {
		t.Fatal("topic/member change created another main session")
	}
	f.complete(s1, "Here is the checklist.", "completed", true)
	until(t, func() bool { return !e.View().Busy })
	if err := e.Rollover(); err != nil {
		t.Fatal(err)
	}
	until(t, func() bool { return len(e.View().Segments) == 2 })
	v := e.View()
	s2 := v.Current
	if s1 == s2 || len(v.Memories) != 1 || v.Memories[0].SubjectID != "B" || v.Jobs[0].ID != job["id"] || v.Jobs[0].ThreadID != worker.ThreadID {
		t.Fatal(v)
	}
	f.mu.Lock()
	bootstrap := ""
	for _, call := range f.calls {
		if call.Method == "turn/start" && call.Params["threadId"] == s2 {
			bootstrap = jsonText(call.Params["input"])
		}
	}
	f.mu.Unlock()
	if !strings.Contains(bootstrap, "Do not modify files") || !strings.Contains(bootstrap, "Chinese") || !strings.Contains(bootstrap, worker.JobID) {
		t.Fatal("handoff lost constraints/memory/job", bootstrap)
	}
	f.complete(worker.ThreadID, "Recovery evidence: cancellation must preserve the epoch.", "completed", true)
	result := active(t, e, "result")
	if result.ThreadID != s2 {
		t.Fatal("result sent to obsolete main session")
	}
	f.complete(s2, "Recovery result with evidence.", "completed", true)
	until(t, func() bool {
		j := e.View().Jobs[0]
		return j.ResultEventID != "" && j.ResultEventID == j.DeliveredEventID
	})
	// A repeated terminal event may be delivered after a transport reconnect.
	f.mu.Lock()
	done := clone(f.threads[worker.ThreadID][0])
	f.mu.Unlock()
	f.emit("turn/completed", map[string]any{"threadId": worker.ThreadID, "turn": done})
	time.Sleep(50 * time.Millisecond)
	e.mu.Lock()
	count := 0
	for _, r := range e.record.Runs {
		if r.Kind == "result" {
			count++
		}
	}
	e.mu.Unlock()
	if count != 1 {
		t.Fatal("duplicate completion dispatched another result", count)
	}
}

func TestRecoverUnknownNativeAcknowledgementWithoutReplaying(t *testing.T) {
	e, f, cfg := setup(t)
	f.loseTurnReply = true
	e.Start()
	m := send(t, e, "B", "one operation only")
	until(t, func() bool { return e.View().Messages[0].State == "unknown" })
	sid := e.View().Current
	e.Close()
	f.complete(sid, "It completed before the connection was lost.", "completed", false)
	restored, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	restored.Start()
	until(t, func() bool { v := restored.View(); return len(v.Messages) == 2 && v.Messages[0].State == "completed" })
	if _, err = restored.Receive(m); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	turns := 0
	for _, c := range f.calls {
		if c.Method == "turn/start" {
			turns++
		}
	}
	f.mu.Unlock()
	if turns != 1 || restored.View().Current != sid {
		t.Fatal("recovery replayed or replaced session", turns)
	}
}

func TestAutomaticCompactionDiscardsStaleStewardDecision(t *testing.T) {
	e, f, _ := setup(t)
	e.Start()
	send(t, e, "B", "check this")
	main := active(t, e, "main")
	f.complete(main.ThreadID, "Checked.", "completed", true)
	until(t, func() bool { return !e.View().Busy })
	f.emit("thread/tokenUsage/updated", map[string]any{"threadId": main.ThreadID, "tokenUsage": map[string]any{"last": map[string]any{"inputTokens": 800}, "modelContextWindow": 1000}})
	cm := active(t, e, "steward")
	f.emit("item/completed", map[string]any{"threadId": main.ThreadID, "item": map[string]any{"id": "native-compact", "type": "contextCompaction"}})
	until(t, func() bool {
		s := e.View().Segments[0]
		return s.Compactions == 1 && !s.UsageValid && s.InputTokens == 0
	})
	f.emit("thread/tokenUsage/updated", map[string]any{"threadId": main.ThreadID, "tokenUsage": map[string]any{"last": map[string]any{"inputTokens": 100}, "modelContextWindow": 1000}})
	f.complete(cm.ThreadID, `{"action":"switch","reason":"boundary"}`, "completed", true)
	until(t, func() bool { e.mu.Lock(); defer e.mu.Unlock(); return e.record.Runs[cm.ID].State == "completed" })
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.record.Decision != nil || e.record.Transition != nil || e.record.Current != main.ThreadID {
		t.Fatal("stale CM decision applied after native compaction")
	}
	if s := e.currentSegmentLocked(); !s.UsageValid || s.InputTokens != 100 {
		t.Fatal("fresh native measurement not restored", s)
	}
}

func TestSuspendStopsPausedRuntimeAndPreservesUnknownWork(t *testing.T) {
	e, f, _ := setup(t)
	e.Start()
	send(t, e, "B", "an accepted request")
	run := active(t, e, "main")
	if err := e.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if !f.Alive() {
		t.Fatal("pause unexpectedly stopped accepted work")
	}
	if err := e.Suspend(); err != nil {
		t.Fatal(err)
	}
	until(t, func() bool { return !f.Alive() })
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.record.Enabled || e.record.Runs[run.ID].State != "unknown" {
		t.Fatal("ended space lost the uncertain native result", e.record.Runs[run.ID])
	}
}

func TestStewardTimeoutIgnoresLateDecision(t *testing.T) {
	e, f, _ := setup(t)
	e.Start()
	send(t, e, "B", "check this")
	main := active(t, e, "main")
	f.complete(main.ThreadID, "Checked.", "completed", true)
	until(t, func() bool { return !e.View().Busy })
	f.emit("thread/tokenUsage/updated", map[string]any{"threadId": main.ThreadID, "tokenUsage": map[string]any{"last": map[string]any{"inputTokens": 800}, "modelContextWindow": 1000}})
	cm := active(t, e, "steward")
	e.mu.Lock()
	expired := e.record.Runs[cm.ID]
	expired.CreatedAt = time.Now().Add(-time.Hour)
	e.record.Runs[cm.ID] = expired
	e.mu.Unlock()
	e.notify()
	until(t, func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.record.Runs[cm.ID].IgnoreResult
	})
	f.complete(cm.ThreadID, `{"action":"switch","reason":"too late"}`, "completed", true)
	send(t, e, "B", "continue chatting")
	next := active(t, e, "main")
	if next.ThreadID != main.ThreadID || e.View().Current != main.ThreadID {
		t.Fatal("timed out decision switched the main session")
	}
}

func TestUpgradeProtectsAcceptedAndPausedBackgroundWork(t *testing.T) {
	e, f, _ := setup(t)
	if e.UpgradeBusy() {
		t.Fatal("idle shadow blocks upgrade")
	}
	send(t, e, "B", "background work")
	if !e.UpgradeBusy() {
		t.Fatal("accepted queue was not protected")
	}
	e.Start()
	main := active(t, e, "main")
	tool(t, e, f, main, "shadow_task", map[string]any{"operation": "start", "title": "investigate", "prompt": "background work"})
	job := active(t, e, "job")
	f.complete(main.ThreadID, "working", "completed", true)
	until(t, func() bool { return !e.View().Busy })
	if err := e.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if !e.UpgradeBusy() {
		t.Fatal("paused reception hid live background work")
	}
	f.complete(job.ThreadID, "done", "completed", true)
	until(t, func() bool { return e.View().Jobs[0].State == "completed" })
	until(t, func() bool { return !e.UpgradeBusy() })
	if !e.View().Online {
		t.Fatal("test must exercise an idle live runtime")
	}
}

func TestUpgradeDistinguishesLiveWorkFromOfflineUnknownReceipts(t *testing.T) {
	e, f, _ := setup(t)
	e.record.Runs["unknown"] = Run{ThreadID: "s1", State: "unknown"}
	if e.UpgradeBusy() {
		t.Fatal("offline unknown receipt blocks upgrade forever")
	}
	e.runtime = f
	if !e.UpgradeBusy() {
		t.Fatal("unknown live turn must be reconciled before upgrade")
	}
	delete(e.record.Runs, "unknown")
	for _, kind := range []string{"main", "job", "steward", "memory", "handoff", "compact"} {
		e.record.Runs[kind] = Run{Kind: kind, State: "submitted"}
		if !e.UpgradeBusy() {
			t.Fatal("live native work lost", kind)
		}
		delete(e.record.Runs, kind)
	}
	e.approvals["request"] = Approval{}
	if !e.UpgradeBusy() {
		t.Fatal("pending native approval lost")
	}
	delete(e.approvals, "request")
	e.compacting["s1"] = true
	if !e.UpgradeBusy() {
		t.Fatal("native compaction lost")
	}
	e.compacting["s1"] = false
	if e.UpgradeBusy() {
		t.Fatal("idle runtime blocks upgrade")
	}
	e.mu.Lock()
	busy := e.UpgradeBusy()
	e.mu.Unlock()
	if !busy {
		t.Fatal("unknown activity under contention treated as idle")
	}
}
