package domain

import (
	"errors"
	"time"
)

var ErrFollowFence = errors.New("Follow was stopped or its checkpoint changed")
var ErrFollowUnsupported = errors.New("read-only Follow has not been verified for this Session source")

// SessionFollow is a read-only reader, never an Agent Run or a native Writer.
// Cursor is machine-local opaque data and must not leave the persistence/RPC path.
type SessionFollow struct {
	ID                string     `json:"id"`
	ThreadID          string     `json:"threadId"`
	Source            SessionRef `json:"source"`
	SourceSnapshotID  string     `json:"sourceSnapshotId"`
	CurrentSnapshotID string     `json:"currentSnapshotId"`
	State             string     `json:"state"`
	Epoch             int64      `json:"epoch"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	LastPolledAt      *time.Time `json:"lastPolledAt,omitempty"`
	Reason            string     `json:"reason,omitempty"`
	Gaps              []string   `json:"gaps"`
	Cursor            string     `json:"-"`
}

// SessionPoll is a provider-bound incremental read. Entries are stable-ID
// upserts; Reset replaces only the current view, never an immutable capture.
type SessionPoll struct {
	Source     SessionRef     `json:"source"`
	CapturedAt time.Time      `json:"capturedAt"`
	Entries    []SessionEntry `json:"entries"`
	Cursor     string         `json:"cursor"`
	Reset      bool           `json:"reset"`
	Gaps       []string       `json:"gaps"`
	Truncated  bool           `json:"truncated"`
	Warnings   []string       `json:"warnings"`
}
