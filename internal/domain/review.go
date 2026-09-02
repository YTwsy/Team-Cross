package domain

import "time"

// SessionRef retains the Provider's conversation identity, never a Team Cross ID.
type SessionRef struct {
	Provider        string            `json:"provider"`
	ProviderVersion string            `json:"providerVersion,omitempty"`
	SessionID       string            `json:"sessionId"`
	IdentityKind    string            `json:"identityKind"`
	Surface         string            `json:"surface"`
	Title           string            `json:"title,omitempty"`
	Cwd             string            `json:"cwd,omitempty"`
	NativeIDs       map[string]string `json:"nativeIds,omitempty"`
}

type SessionCapabilities struct {
	Read        bool   `json:"read"`
	Follow      bool   `json:"follow"`
	Open        bool   `json:"open"`
	Resume      bool   `json:"resume"`
	TakeControl bool   `json:"takeControl"`
	Reason      string `json:"reason,omitempty"`
}

type SessionEntry struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Role     string `json:"role,omitempty"`
	Text     string `json:"text"`
	SourceID string `json:"sourceId,omitempty"`
	TurnID   string `json:"turnId,omitempty"`
}

// SessionSnapshot is an immutable read-only capture, not a Run or live attachment.
type SessionSnapshot struct {
	ID           string              `json:"id"`
	ThreadID     string              `json:"threadId"`
	Source       SessionRef          `json:"source"`
	CapturedAt   time.Time           `json:"capturedAt"`
	Entries      []SessionEntry      `json:"entries"`
	Truncated    bool                `json:"truncated"`
	Warnings     []string            `json:"warnings"`
	Capabilities SessionCapabilities `json:"capabilities"`
}

type AnnotationTarget struct {
	SnapshotID string `json:"snapshotId,omitempty"`
	EntryID    string `json:"entryId,omitempty"`
	EvidenceID string `json:"evidenceId,omitempty"`
	RoundID    string `json:"roundId,omitempty"`
}

// ShareScope is an allowlist. Empty collections disclose no corresponding data.
type ShareScope struct {
	SnapshotID    string   `json:"snapshotId,omitempty"`
	EntryIDs      []string `json:"entryIds"`
	EvidenceIDs   []string `json:"evidenceIds"`
	IncludeCode   bool     `json:"includeCode"`
	IncludeEvents bool     `json:"includeEvents"`
}
