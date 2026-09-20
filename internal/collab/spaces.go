package collab

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (s *Session) snapshotLocked() Record {
	r := s.record
	if r.ExecutionRecord != nil {
		execution := *r.ExecutionRecord
		r.ExecutionRecord = &execution
	}
	return r
}

func executionCommands(r Record) map[string]Command {
	if r.ExecutionRecord == nil {
		return nil
	}
	return r.Commands
}

type SpaceInput struct {
	RequestID string `json:"requestId"`
	Title     string `json:"title"`
}

// A space owns publications and discussion. Creating it never inspects a Git
// directory, starts a provider runtime, or resumes/forks the source session.
func (a *App) CreateSpace(ctx context.Context, in SpaceInput) (*Session, error) {
	if _, err := uuid.Parse(in.RequestID); err != nil {
		return nil, fmt.Errorf("空间创建需要唯一 requestId")
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" || len([]rune(in.Title)) > 160 {
		return nil, fmt.Errorf("空间名称需要 1–160 字")
	}
	a.spaceCreateMu.Lock()
	defer a.spaceCreateMu.Unlock()
	a.mu.Lock()
	closed, old := a.closed, a.sessions[in.RequestID]
	a.mu.Unlock()
	if closed || ctx.Err() != nil {
		return nil, fmt.Errorf("Core 已退出或请求取消")
	}
	if old != nil {
		old.mu.Lock()
		matches := old.record.Title == in.Title
		if !matches {
			old.mu.Unlock()
			return nil, fmt.Errorf("requestId 已用于另一个空间")
		}
		err := old.saveLocked()
		old.mu.Unlock()
		return old, err
	}
	r := Record{Schema: 3, ID: in.RequestID, Title: in.Title, State: "ready", CreatedAt: time.Now(), UpdatedAt: time.Now(), Annotations: []Annotation{}}
	if err := os.MkdirAll(filepath.Join(a.Config.DataDir, "collaborations", r.ID), 0700); err != nil {
		return nil, err
	}
	s := a.newSession(r)
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil, fmt.Errorf("Core 已退出")
	}
	if a.sessions[r.ID] != nil {
		a.mu.Unlock()
		return nil, fmt.Errorf("空间标识已使用")
	}
	a.sessions[r.ID] = s
	a.mu.Unlock()
	s.mu.Lock()
	err := s.saveLocked()
	s.mu.Unlock()
	return s, err
}
