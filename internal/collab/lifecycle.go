package collab

import (
	"context"
	"teamcross/internal/sharing"
	"time"
)

// Closing this Session's dedicated app-server is the native writer-lock release
// boundary. Unsubscribing alone can leave the thread loaded in Codex.
func (s *Session) releaseIfIdleLocked() {
	if !s.releaseWhenIdle || s.closed || s.record.State == "preparing" || s.share != nil || s.direct != nil || s.starting || s.stopping != nil || s.activeCalls != 0 || s.process == nil {
		return
	}
	if s.process.Alive() && (s.busy || len(s.approvals) != 0) {
		return
	}
	if s.record.Provider == "claude" && time.Since(s.nativeLastWrite) < 3*time.Second {
		return
	}
	p := s.process
	done := make(chan struct{})
	s.stopping, s.process, s.online = done, nil, false
	s.generation++ // A late disconnect/turn notification cannot affect a restart.
	go func() {
		p.Close() // Wait for process exit without holding Session.mu.
		s.mu.Lock()
		s.stopping = nil
		close(done)
		s.mu.Unlock()
	}()
}

func (s *Session) finishCall() {
	s.mu.Lock()
	s.activeCalls--
	s.releaseIfIdleLocked()
	s.mu.Unlock()
}

type directKey struct{}

func (s *Session) callerValidLocked(ctx context.Context) bool {
	if !sharing.Authorized(ctx, s.share) {
		return false
	}
	if d, ok := ctx.Value(directKey{}).(*direct); ok {
		if s.direct != d {
			return false
		}
		select {
		case <-d.done:
			return false
		default:
		}
	}
	return true
}
