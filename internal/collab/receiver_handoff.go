package collab

import (
	"context"
	"encoding/json"
	"fmt"

	"teamcross/internal/problem"
)

// Check the persisted store, not just the worker's in-memory thread: an empty
// native thread may not yet have a rollout that another process can resume.
func (a *App) receiverHistory(ctx context.Context, id string) error {
	var turns struct {
		Data []json.RawMessage `json:"data"`
	}
	if id == "" {
		return fmt.Errorf("原生会话尚未创建")
	}
	if err := a.readerCall(ctx, "thread/turns/list", map[string]any{"threadId": id, "limit": 1, "itemsView": "notLoaded"}, &turns); err != nil || len(turns.Data) == 0 {
		return fmt.Errorf("会话尚无可读取的已保存历史，请完成首轮后再释放或打开")
	}
	return nil
}

func (a *App) pauseReceiver(ctx context.Context, s *Session) error {
	s.receiverActionMu.Lock()
	defer s.receiverActionMu.Unlock()
	s.mu.Lock()
	if s.closed || s.starting || s.record.State != "ready" {
		s.mu.Unlock()
		return fmt.Errorf("会话尚未就绪，请稍后重试")
	}
	if s.receiverPaused {
		s.releaseIfIdleLocked()
		s.mu.Unlock()
		return nil
	}
	id := s.record.SessionID
	s.mu.Unlock()
	if err := a.receiverHistory(ctx, id); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("Core 已退出")
	}
	s.receiverPaused, s.releaseWhenIdle = true, true
	if err := s.saveLocked(); err != nil {
		s.receiverPaused, s.releaseWhenIdle = false, false
		return err
	}
	// The saved pause blocks delivery immediately. Existing turns, approvals
	// and accepted RPC calls finish before the dedicated process is closed.
	s.releaseIfIdleLocked()
	return nil
}

func (a *App) resumeReceiver(ctx context.Context, s *Session) error {
	s.receiverActionMu.Lock()
	defer s.receiverActionMu.Unlock()
	s.mu.Lock()
	if s.receiverPaused && (s.starting || s.stopping != nil || (s.process != nil && s.process.Alive())) {
		s.mu.Unlock()
		return fmt.Errorf("会话仍在释放，请等待当前执行和原生交互结束")
	}
	s.mu.Unlock()
	if err := s.start(ctx, true); err != nil {
		return problem.New("receiver_resume_failed", "恢复接收失败；若会话仍在其他应用中打开，请先在那里释放后重试", err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	paused := s.receiverPaused
	s.receiverPaused = false
	if err := s.saveLocked(); err != nil {
		s.receiverPaused, s.releaseWhenIdle = paused, paused
		s.releaseIfIdleLocked()
		return err
	}
	return nil
}

func (a *App) openReceiverDesktop(ctx context.Context, s *Session) (any, error) {
	s.receiverActionMu.Lock()
	defer s.receiverActionMu.Unlock()
	s.mu.Lock()
	id := s.record.SessionID
	released := s.receiverPaused && !s.online && !s.starting && s.stopping == nil && (s.process == nil || !s.process.Alive())
	s.mu.Unlock()
	if !released {
		return nil, fmt.Errorf("请先暂停接收并等待会话释放，再在 Codex Desktop 中继续")
	}
	if err := a.receiverHistory(ctx, id); err != nil {
		return nil, err
	}
	return a.openPersonalCodex(ctx, id, true)
}
