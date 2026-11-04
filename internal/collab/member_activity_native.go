package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

func (s *Session) activityInputResultLocked(key string, result json.RawMessage, state string) {
	a := s.activityStateLocked()
	input, ok := a.Inputs[key]
	if !ok {
		return
	}
	input.State = state
	var out struct {
		TurnID string `json:"turnId"`
		Turn   struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	_ = json.Unmarshal(result, &out)
	if out.Turn.ID != "" {
		input.TurnID = out.Turn.ID
	}
	if out.TurnID != "" {
		input.TurnID = out.TurnID
	}
	a.Inputs[key] = input
	if state == "completed" && input.TurnID != "" {
		turn := a.Turns[input.TurnID]
		turn.ID = input.TurnID
		if turn.State == "" {
			turn.State = "inProgress"
		}
		if !slices.Contains(turn.Members, input.MemberID) {
			turn.Members = append(turn.Members, input.MemberID)
		}
		a.Turns[turn.ID] = turn
	}
}
func (s *Session) observeActivityNativeLocked(method string, params json.RawMessage) {
	if s.receiverSpace != "" || s.record.ExecutionRecord == nil {
		return
	}
	if method != "turn/started" && method != "turn/completed" && method != "item/completed" {
		return
	}
	var p struct {
		Turn   MaterialTurn `json:"turn"`
		TurnID string       `json:"turnId"`
		Item   MaterialItem `json:"item"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	id := p.Turn.ID
	if id == "" {
		id = p.TurnID
	}
	if id == "" {
		return
	}
	a := s.activityStateLocked()
	turn := a.Turns[id]
	turn.ID = id
	if turn.State == "" {
		turn.State = "inProgress"
	}
	if method == "turn/started" {
		for key, input := range a.Inputs {
			if input.State == "pending" && input.Method == "turn/start" && input.TurnID == "" {
				input.TurnID = id
				input.State = "accepted"
				a.Inputs[key] = input
			}
			if input.TurnID == id && input.State != "failed" && !slices.Contains(turn.Members, input.MemberID) {
				turn.Members = append(turn.Members, input.MemberID)
			}
		}
	}
	items := p.Turn.Items
	if method == "item/completed" {
		items = []MaterialItem{p.Item}
	}
	for _, item := range items {
		if item.Type == "agentMessage" && item.Phase != "commentary" {
			if item.ID != "" && !slices.Contains(turn.ItemIDs, item.ID) {
				turn.ItemIDs = append(turn.ItemIDs, item.ID)
			}
			if item.Text != "" {
				turn.Preview = activityPreview(item.Text)
			}
		}
	}
	if method == "turn/completed" {
		turn.State = p.Turn.Status
		if turn.State == "" {
			turn.State = "completed"
		}
	}
	turn.UpdatedAt = time.Now()
	a.Turns[id] = turn
	if err := s.saveLocked(); err != nil {
		s.activityRoot().activityError = err.Error()
	}
}

// Read saved turns without creating a writable runtime or sending a model input.
// The first scan is a baseline; only known accepted inputs may produce historical results.
func (s *Session) syncActivityHistory(ctx context.Context) error {
	s.nativeStateMu.Lock()
	defer s.nativeStateMu.Unlock()
	s.mu.Lock()
	if s.closed || s.record.ExecutionRecord == nil || s.receiverSpace != "" {
		s.mu.Unlock()
		return nil
	}
	record := s.snapshotLocked()
	process := s.process
	old := activityClone(s.activityStateLocked())
	s.activityLastSync = time.Now()
	s.mu.Unlock()
	turns := []MaterialTurn{}
	cursor := ""
	var readErr error
	for page := 0; page < 125; page++ {
		_, batch, next, err := s.loadHistoryPage(ctx, record, process, cursor)
		if err != nil {
			readErr = err
			break
		}
		turns = append(turns, batch...)
		reached := false
		for _, turn := range batch {
			if old.NativeKnown[turn.ID] {
				reached = true
			}
		}
		if next == "" || reached || !old.NativeReady {
			break
		}
		cursor = next
		if page == 124 {
			readErr = fmt.Errorf("原生历史超过本次同步范围，结果同步尚未完成")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.record.SessionID != record.SessionID {
		return fmt.Errorf("协作会话已变化")
	}
	a := s.activityStateLocked()
	if readErr != nil {
		a.SyncError = "轮次动态暂未同步：" + readErr.Error()
		_ = s.saveLocked()
		return readErr
	}
	if a.NativeKnown == nil {
		a.NativeKnown = map[string]bool{}
	}
	for _, native := range turns {
		prior, known := a.Turns[native.ID]
		if native.Status == "inProgress" || native.Status == "running" {
			a.NativeKnown[native.ID] = true
			continue
		}
		if native.Status == "" {
			continue
		}
		shouldObserve := known || (old.NativeReady && !old.NativeKnown[native.ID])
		a.NativeKnown[native.ID] = true
		if !shouldObserve {
			continue
		}
		if known && prior.State != "" && prior.State != "inProgress" {
			continue
		}
		prior.ID = native.ID
		prior.State = native.Status
		prior.UpdatedAt = time.Now()
		for _, input := range a.Inputs {
			if input.TurnID == native.ID && input.State != "failed" && !slices.Contains(prior.Members, input.MemberID) {
				prior.Members = append(prior.Members, input.MemberID)
			}
		}
		for _, item := range native.Items {
			if item.Type == "agentMessage" && item.Phase != "commentary" {
				prior.ItemIDs = append(prior.ItemIDs, item.ID)
				prior.Preview = activityPreview(item.Text)
			}
		}
		a.Turns[native.ID] = prior
	}
	a.NativeReady = true
	a.SyncError = ""
	return s.saveLocked()
}
func (a *App) pumpMemberActivity(ctx context.Context) {
	a.mu.Lock()
	sessions := []*Session{}
	for _, s := range a.executions {
		sessions = append(sessions, s)
	}
	a.mu.Unlock()
	for _, s := range sessions {
		s.mu.Lock()
		needed := false
		if state := s.record.Activity; state != nil {
			for _, prefs := range state.Preferences {
				for _, p := range prefs {
					if p.Subject.Kind == "execution" && p.Mode == "following" {
						needed = true
					}
				}
			}
			for _, turn := range state.Turns {
				if turn.State == "inProgress" {
					needed = true
				}
			}
		}
		due := time.Since(s.activityLastSync) > 10*time.Second
		s.mu.Unlock()
		if needed && due {
			call, cancel := context.WithTimeout(ctx, 5*time.Second)
			_ = s.syncActivityHistory(call)
			cancel()
		}
		if ctx.Err() != nil {
			return
		}
	}
}
