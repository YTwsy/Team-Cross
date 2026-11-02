package shadow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/nativecodex"
	"teamcross/internal/spacecontext"
)

const maxInputBatch = 16
const maxInputBytes = 96 << 10
const maxTurnInputs = 128

// Input retains the actual message or discussion origin. It is a native input
// receipt, not a business task, waiting condition or answer-completion record.
type Input struct {
	ID           string               `json:"id"`
	Kind         string               `json:"kind"`
	SourceID     string               `json:"sourceId"`
	EventID      string               `json:"eventId,omitempty"`
	Message      *Message             `json:"message,omitempty"`
	Continuation *ContinuationContext `json:"continuation,omitempty"`
	CreatedAt    time.Time            `json:"createdAt"`
	Committed    bool                 `json:"committed,omitempty"`
	TaskIDs      []string             `json:"taskIds,omitempty"`
	Truncated    bool                 `json:"truncated,omitempty"`
	Origins      []InputOrigin        `json:"origins,omitempty"`
}

type InputOrigin struct {
	ID       string                 `json:"id"`
	SourceID string                 `json:"sourceId"`
	Actor    spacecontext.Actor     `json:"actor"`
	Source   spacecontext.SourceRef `json:"source"`
}

func (e *Engine) trackInputTaskLocked(selected Run, jobID string) {
	current := e.record.Runs[selected.ID]
	current.TaskIDs = appendUniqueLinks(current.TaskIDs, []string{jobID})
	for b := range current.InputBatches {
		for i := range current.InputBatches[b].Inputs {
			input := &current.InputBatches[b].Inputs[i]
			matches := input.SourceID == selected.SourceID
			for _, origin := range input.Origins {
				matches = matches || origin.SourceID == selected.SourceID
			}
			if input.Kind == selected.Kind && matches && input.EventID == selected.EventID {
				input.TaskIDs = appendUniqueLinks(input.TaskIDs, []string{jobID})
			}
		}
	}
	e.record.Runs[selected.ID] = current
}

type InputBatch struct {
	ID     string  `json:"id"`
	Method string  `json:"method"`
	TurnID string  `json:"turnId,omitempty"`
	State  string  `json:"state"` // prepared, accepted, rejected, failed, unknown
	Error  string  `json:"error,omitempty"`
	Inputs []Input `json:"inputs"`
}

func runInputs(run Run) []Input {
	var inputs []Input
	for _, batch := range run.InputBatches {
		if batch.State == "accepted" {
			inputs = append(inputs, batch.Inputs...)
		}
	}
	return inputs
}

func uncertainInputs(run Run) bool {
	for _, batch := range run.InputBatches {
		if batch.State == "prepared" || batch.State == "unknown" {
			return true
		}
	}
	return false
}

// A selected copy supplies an action's origin; never replace the persisted
// multi-input run with this projection or pick the first/last author implicitly.
func selectInput(run Run, id string) (Run, error) {
	if len(run.InputBatches) == 0 {
		if id != "" {
			return Run{}, fmt.Errorf("当前回合没有该输入")
		}
		return run, nil
	}
	inputs := runInputs(run)
	if id == "" && len(inputs) == 1 {
		id = inputs[0].ID
	}
	for _, in := range inputs {
		if in.ID == id {
			if len(in.Origins) > 1 {
				return Run{}, fmt.Errorf("讨论包包含多个实际输入，请选择 origins 中准确的 inputId")
			}
			run.Kind, run.SourceID, run.EventID = in.Kind, in.SourceID, in.EventID
			return run, nil
		}
		for _, origin := range in.Origins {
			if origin.ID == id {
				run.Kind, run.SourceID, run.EventID = in.Kind, origin.SourceID, in.EventID
				return run, nil
			}
		}
	}
	return Run{}, fmt.Errorf("请用 inputId 明确选择本轮实际输入；不得默认使用其他成员或讨论的来源")
}

func inputID(arguments json.RawMessage) string {
	var p struct {
		InputID string `json:"inputId"`
	}
	_ = json.Unmarshal(arguments, &p)
	return p.InputID
}

func inputMarker(id string) string { return "[teamcross-shadow-input:" + id + "]" }

func (e *Engine) pendingInputs(ctx context.Context) ([]Input, error) {
	e.mu.Lock()
	var inputs []Input
	excluded := map[string]bool{}
	for _, run := range e.record.Runs {
		if run.Kind == "continuation" && len(run.InputBatches) == 0 {
			excluded[run.EventID] = true
		}
		for _, batch := range run.InputBatches {
			if batch.State == "rejected" {
				continue
			}
			for _, in := range batch.Inputs {
				if in.EventID != "" {
					excluded[in.EventID] = true
				}
			}
		}
	}
	for _, message := range e.record.Messages {
		if message.Role == "user" && message.State == "delivered" {
			m := clone(message)
			at := m.CreatedAt
			if m.ReceivedAt != nil {
				at = *m.ReceivedAt
			}
			inputs = append(inputs, Input{ID: "message:" + m.ID, Kind: "main", SourceID: m.ID, Message: &m, CreatedAt: at})
		}
	}
	store, id, tools := e.attention, e.record.ID, e.cfg.CallTool != nil
	e.mu.Unlock()
	if store != nil && tools {
		for n := 0; n < maxInputBatch; n++ {
			packet, err := store.claimContinuationExcept(id, excluded)
			if err != nil {
				return nil, err
			}
			if packet == nil {
				break
			}
			c := packet.Continuation
			excluded[c.ID] = true
			if c.Mode == "direct" {
				if !e.checkDirectRoute(ctx, store, c.BatchID) {
					continue
				}
				current, err := store.CurrentContinuation(c.ID, id)
				if err != nil {
					continue
				}
				packet = &current
			}
			source := c.ID
			if len(c.EventIDs) > 0 {
				source = c.EventIDs[0]
			}
			input := Input{ID: "activity:" + c.ID, Kind: "continuation", SourceID: source, EventID: c.ID, Continuation: packet, CreatedAt: c.CreatedAt}
			if c.Mode == "direct" && c.HandlingID == "" {
				signals, err := store.BatchSignals(c.BatchID)
				if err != nil {
					return nil, err
				}
				for _, signal := range signals {
					input.Origins = append(input.Origins, InputOrigin{ID: input.ID + ":" + signal.ID, SourceID: signal.ID, Actor: signal.Actor, Source: signal.Source})
				}
				if len(input.Origins) > 1 {
					input.SourceID = ""
				}
			}
			inputs = append(inputs, input)
		}
	}
	for i := range inputs {
		inputs[i] = boundedInput(inputs[i])
	}
	sort.SliceStable(inputs, func(i, j int) bool { return inputs[i].CreatedAt.Before(inputs[j].CreatedAt) })
	size, end := 0, 0
	for _, in := range inputs {
		n := len(jsonText(in))
		if end == maxInputBatch || size+n > maxInputBytes {
			break
		}
		size += n
		end++
	}
	if end == 0 && len(inputs) > 0 {
		return nil, fmt.Errorf("单条分身输入超过有界原生输入预算，保留原来源待核对")
	}
	return inputs[:end], nil
}

// Originals remain in Messages / AttentionStore. Large bodies are references
// plus explicit previews so ordinary long bodies fit the native input budget.
func boundedInput(in Input) Input {
	if len(jsonText(in)) <= maxInputBytes {
		return in
	}
	in = clone(in)
	in.Truncated = true
	if in.Message != nil {
		in.Message.Text = clip(in.Message.Text, 8000)
	}
	if p := in.Continuation; p != nil {
		for i := range p.SourceBodies {
			body := &p.SourceBodies[i]
			body.Data, _ = json.Marshal(map[string]any{"preview": clip(string(body.Data), 600), "readExactSource": body.Source})
			body.Partial = true
		}
		p.Focus.Summary, p.Focus.Goal = clip(p.Focus.Summary, 1000), clip(p.Focus.Goal, 1000)
	}
	return in
}

func (e *Engine) inputPrompt(inputs []Input, initial bool) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	prefix := ""
	if initial {
		prefix = rolePrompt + "\nCurrent bounded context (service data):\n" + e.contextLocked(false)
		var deltas []Memory
		segment := e.currentSegmentLocked()
		for _, memory := range sortedMemories(e.record.Memories) {
			if memory.State != "candidate" && (segment == nil || memory.Revision > segment.MemoryRevision) {
				deltas = append(deltas, memory)
			}
		}
		if len(deltas) > 0 {
			prefix += "\nMemory changes since this segment last saw them; forgotten means stop using the old value:\n" + jsonText(deltas)
		}
	}
	return prefix + "\nNew ordered inputs. Each input has an id, author, exact source and reply destination. A discussion bundle with multiple origins contains independent messages: use the selected origins[].id as inputId, never default to its first source. Otherwise use the input id. Keep different requests distinct. Use inputId on actions, memories and task starts. For shared-chat answers use shadow_reply with inputId and text, including short acknowledgements and ordinary follow-up questions. For discussion inputs use the original discussion tools with inputId and the exact supplied target. Final text is internal when multiple inputs are present; it is not a broadcast answer. Do not create a job merely to ask an ordinary clarification. Do not copy private receipt reasons or unrelated discussion bodies into shared chat. truncated/partial inputs are previews: read exact discussion sources, or shadow_history with the message ID as anchor and nextOffset for further pages. A substantial background task can be dispatched, then finish this foreground turn without waiting.\n" + jsonText(inputs)
}

func (e *Engine) scheduleInputs(ctx context.Context) (bool, error) {
	_, toolsDigest, err := e.nativeTools()
	if err != nil {
		return false, err
	}
	e.mu.Lock()
	thread := e.record.Current
	var running Run
	blocked := thread == "" || e.record.Transition != nil || e.compacting[thread]
	for _, approval := range e.approvals {
		var p struct {
			ThreadID string `json:"threadId"`
		}
		_ = json.Unmarshal(approval.Params, &p)
		blocked = blocked || p.ThreadID == thread
	}
	for _, run := range e.record.Runs {
		if run.ThreadID != thread {
			continue
		}
		if uncertainInputs(run) {
			blocked = true
		}
		if activeRun(run) {
			if s := e.currentSegmentLocked(); s != nil && s.ToolsetDigest != toolsDigest {
				blocked = true // New input must wait for the registered tools' safe upgrade boundary.
			}
			for _, batch := range run.InputBatches {
				if batch.Method == "turn/steer" && batch.State == "rejected" && batch.TurnID == run.TurnID {
					blocked = true
				}
			}
			if (run.Kind != "main" && run.Kind != "continuation") || run.State != "submitted" || run.TurnID == "" || len(run.InputBatches) == 0 || len(runInputs(run)) >= maxTurnInputs {
				blocked = true
			}
			running = clone(run)
		}
	}
	e.mu.Unlock()
	if blocked {
		return false, nil
	}
	inputs, err := e.pendingInputs(ctx)
	if err != nil || len(inputs) == 0 {
		return false, err
	}
	batch := InputBatch{ID: uuid.NewString(), Method: "turn/start", State: "prepared", Inputs: inputs}
	if running.ID != "" {
		if remaining := maxTurnInputs - len(runInputs(running)); len(batch.Inputs) > remaining {
			batch.Inputs = batch.Inputs[:remaining]
		}
		batch.Method, batch.TurnID = "turn/steer", running.TurnID
		return true, e.steerInputs(ctx, running, batch)
	}
	run := Run{Kind: "main", ThreadID: thread, InputBatches: []InputBatch{batch}}
	if len(inputs) == 1 {
		run.Kind, run.SourceID, run.EventID = inputs[0].Kind, inputs[0].SourceID, inputs[0].EventID
	}
	return true, e.submit(ctx, run, inputMarker(batch.ID)+"\n"+e.inputPrompt(inputs, true), nil)
}

func (e *Engine) setInputMessageStateLocked(inputs []Input, state, thread, turn, failure string) {
	for _, in := range inputs {
		if in.Kind != "main" {
			continue
		}
		for i := range e.record.Messages {
			m := &e.record.Messages[i]
			if m.ID == in.SourceID {
				m.State, m.SegmentID, m.TurnID, m.Error = state, thread, turn, failure
			}
		}
	}
}

func (e *Engine) steerInputs(ctx context.Context, run Run, batch InputBatch) error {
	e.mu.Lock()
	p := e.runtime
	current := e.record.Runs[run.ID]
	if !activeRun(current) || current.TurnID != batch.TurnID || uncertainInputs(current) {
		e.mu.Unlock()
		return nil
	}
	err := e.mutateLocked(func() error {
		current.InputBatches = append(current.InputBatches, batch)
		e.record.Runs[run.ID] = current
		e.setInputMessageStateLocked(batch.Inputs, "submitted", run.ThreadID, run.TurnID, "")
		return nil
	})
	e.mu.Unlock()
	if err != nil {
		return err
	}
	var response struct {
		TurnID string `json:"turnId"`
	}
	err = p.Call(ctx, "turn/steer", map[string]any{"threadId": run.ThreadID, "expectedTurnId": batch.TurnID, "input": []any{map[string]any{"type": "text", "text": inputMarker(batch.ID) + "\n" + e.inputPrompt(batch.Inputs, false)}}}, &response)
	var rejected *nativecodex.RPCError
	explicitRejection := errors.As(err, &rejected) && rejected.Code == -32600 &&
		(strings.Contains(strings.ToLower(rejected.Message), "no active turn") ||
			strings.Contains(strings.ToLower(rejected.Message), "expected") && strings.Contains(strings.ToLower(rejected.Message), "turn") && strings.Contains(strings.ToLower(rejected.Message), "match"))
	e.mu.Lock()
	saveErr := e.mutateLocked(func() error {
		current := e.record.Runs[run.ID]
		for i := range current.InputBatches {
			b := &current.InputBatches[i]
			if b.ID != batch.ID {
				continue
			}
			switch {
			case err == nil && response.TurnID == batch.TurnID:
				b.State = "accepted"
			case explicitRejection:
				b.State, b.Error = "rejected", err.Error()
				e.setInputMessageStateLocked(b.Inputs, "delivered", "", "", "")
			case rejected != nil:
				b.State, b.Error = "failed", err.Error()
				e.setInputMessageStateLocked(b.Inputs, "failed", run.ThreadID, run.TurnID, b.Error)
			default:
				b.State, b.Error = "unknown", "追加输入的原生回执不明，先核对原轮次，不自动重发"
				e.setInputMessageStateLocked(b.Inputs, "unknown", run.ThreadID, run.TurnID, b.Error)
			}
		}
		if len(runInputs(current)) > 1 {
			current.SourceID, current.EventID = "", ""
		}
		e.record.Runs[run.ID] = current
		return nil
	})
	e.mu.Unlock()
	if saveErr != nil {
		return saveErr
	}
	if explicitRejection {
		return e.reconcileRuns(ctx)
	}
	return nil
}

func (e *Engine) acceptInputHistoryLocked(run *Run, turn nativeTurn) {
	for i := range run.InputBatches {
		batch := &run.InputBatches[i]
		if batch.State == "rejected" || batch.State == "failed" {
			continue
		}
		found := batch.State == "accepted"
		for _, item := range turn.Items {
			var header struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(item, &header)
			found = found || header.Type == "userMessage" && strings.Contains(itemText(item), inputMarker(batch.ID))
		}
		if found {
			batch.State, batch.TurnID, batch.Error = "accepted", turn.ID, ""
			e.setInputMessageStateLocked(batch.Inputs, "submitted", run.ThreadID, turn.ID, "")
		} else {
			batch.State, batch.Error = "unknown", "原生历史尚未确认这批输入；保留原身份，不重放"
		}
	}
	if len(runInputs(*run)) > 1 {
		run.SourceID, run.EventID = "", ""
	}
}
