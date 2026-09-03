package server

import (
	"teamcross/internal/domain"
	"time"
)

type doctorCheck struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Status   string `json:"status"`
	Detail   string `json:"detail"`
	Optional bool   `json:"optional,omitempty"`
}

type appInfo struct {
	Version           string        `json:"version"`
	Mode              string        `json:"mode"`
	Role              string        `json:"role"`
	Repo              string        `json:"repo,omitempty"`
	SelectedTransport string        `json:"selectedTransport,omitempty"`
	ParticipantID     string        `json:"participantId,omitempty"`
	Doctor            []doctorCheck `json:"doctor"`
}

type gitFile struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Captured bool   `json:"captured"`
	Reason   string `json:"reason,omitempty"`
}

type gitView struct {
	Head          string    `json:"head"`
	Branch        string    `json:"branch"`
	Unborn        bool      `json:"unborn"`
	Status        string    `json:"status"`
	StagedPatch   string    `json:"stagedPatch"`
	UnstagedPatch string    `json:"unstagedPatch"`
	FinalPatch    string    `json:"finalPatch,omitempty"`
	Untracked     []gitFile `json:"untracked"`
}

type roundView struct {
	ID        string    `json:"id"`
	Sequence  int64     `json:"sequence"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"createdAt"`
	Provider  string    `json:"provider,omitempty"`
}

type eventView struct {
	Seq       int64          `json:"seq"`
	Type      string         `json:"type"`
	Actor     string         `json:"actor,omitempty"`
	Payload   map[string]any `json:"payload"`
	CreatedAt time.Time      `json:"createdAt"`
}

type annotationView struct {
	SourceShareID string                   `json:"-"`
	Target        *domain.AnnotationTarget `json:"target,omitempty"`
	ID            string                   `json:"id"`
	Author        string                   `json:"author"`
	Body          string                   `json:"body"`
	File          string                   `json:"file,omitempty"`
	Line          int                      `json:"line,omitempty"`
	CreatedAt     time.Time                `json:"createdAt"`
}

type evidenceView struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	MIMEType  string    `json:"mimeType,omitempty"`
	Source    string    `json:"source,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type agentRunView struct {
	ID             string `json:"id"`
	Provider       string `json:"provider"`
	Status         string `json:"status"`
	SessionID      string `json:"sessionId,omitempty"`
	TurnID         string `json:"turnId,omitempty"`
	NetworkEnabled bool   `json:"networkEnabled"`
}

type participantView struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Role       string    `json:"role"`
	Transport  string    `json:"transport"`
	LastSeenAt time.Time `json:"lastSeenAt"`
}

type shareView struct {
	AllowControl bool               `json:"allowControl"`
	Scope        *domain.ShareScope `json:"scope,omitempty"`
	ID           string             `json:"id"`
	Invite       string             `json:"invite"`
	ExpiresAt    time.Time          `json:"expiresAt"`
	Status       string             `json:"status"`
	Transports   []string           `json:"transports"`
}

type leaseView struct {
	ParticipantID string    `json:"participantId"`
	Epoch         int64     `json:"epoch"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

type threadSummary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Repo      string    `json:"repo"`
	Branch    string    `json:"branch"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updatedAt"`
	Provider  string    `json:"provider,omitempty"`
}

type threadDetail struct {
	threadSummary
	ReadOnly         bool                     `json:"readOnly"`
	SessionSnapshots []domain.SessionSnapshot `json:"sessionSnapshots"`
	SessionFollows   []domain.SessionFollow   `json:"sessionFollows,omitempty"`
	NativeLive       *nativeLiveView          `json:"nativeLive,omitempty"`
	Revision         int64                    `json:"revision"`
	Worktree         string                   `json:"worktree"`
	Goal             string                   `json:"goal"`
	Progress         string                   `json:"progress"`
	Blocker          string                   `json:"blocker"`
	Tried            string                   `json:"tried"`
	Questions        string                   `json:"questions"`
	Git              gitView                  `json:"git"`
	Rounds           []roundView              `json:"rounds"`
	Events           []eventView              `json:"events"`
	Annotations      []annotationView         `json:"annotations"`
	Evidence         []evidenceView           `json:"evidence"`
	AgentRun         *agentRunView            `json:"agentRun,omitempty"`
	Participants     []participantView        `json:"participants"`
	Share            *shareView               `json:"share,omitempty"`
	ControlLease     *leaseView               `json:"controlLease,omitempty"`
}

type handoffManifest struct {
	Goal      string `json:"goal"`
	Progress  string `json:"progress"`
	Blocker   string `json:"blocker"`
	Tried     string `json:"tried"`
	Questions string `json:"questions"`
}

type capturePreview struct {
	Repo      string    `json:"repo"`
	Branch    string    `json:"branch"`
	Head      string    `json:"head"`
	Unborn    bool      `json:"unborn"`
	Status    string    `json:"status"`
	Untracked []gitFile `json:"untracked"`
}

type createThreadRequest struct {
	Repo      string   `json:"repo"`
	Title     string   `json:"title"`
	Goal      string   `json:"goal"`
	Progress  string   `json:"progress"`
	Blocker   string   `json:"blocker"`
	Tried     string   `json:"tried"`
	Questions string   `json:"questions"`
	Untracked []string `json:"untracked"`
}
