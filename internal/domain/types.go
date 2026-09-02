package domain

import "time"

// Thread is the durable unit of collaboration. Revision is a monotonically
// increasing optimistic-concurrency token for all writes scoped to the thread.
type Thread struct {
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	RepoRoot       string    `json:"repoRoot"`
	BaselineCommit string    `json:"baselineCommit,omitempty"`
	Branch         string    `json:"branch,omitempty"`
	WorktreePath   string    `json:"worktreePath,omitempty"`
	ReadOnly       bool      `json:"readOnly"`
	Revision       int64     `json:"revision"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// Round is immutable after insertion. A round seals a snapshot of the thread
// together with the event interval and optional managed agent run that made it.
type Round struct {
	ID             string    `json:"id"`
	ThreadID       string    `json:"threadId"`
	Number         int64     `json:"number"`
	ParentRoundID  string    `json:"parentRoundId,omitempty"`
	Kind           string    `json:"kind"`
	Summary        string    `json:"summary,omitempty"`
	SnapshotID     string    `json:"snapshotId,omitempty"`
	AgentRunID     string    `json:"agentRunId,omitempty"`
	EventFromSeq   int64     `json:"eventFromSeq,omitempty"`
	EventToSeq     int64     `json:"eventToSeq,omitempty"`
	ManifestObject string    `json:"manifestObject,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

// GitSnapshot stores object references rather than large inline blobs.
type GitSnapshot struct {
	ID                      string    `json:"id"`
	ThreadID                string    `json:"threadId"`
	Head                    string    `json:"head,omitempty"`
	Branch                  string    `json:"branch,omitempty"`
	Unborn                  bool      `json:"unborn"`
	StatusObject            string    `json:"statusObject"`
	StagedPatchObject       string    `json:"stagedPatchObject"`
	UnstagedPatchObject     string    `json:"unstagedPatchObject"`
	UntrackedManifestObject string    `json:"untrackedManifestObject"`
	CreatedAt               time.Time `json:"createdAt"`
}

type Evidence struct {
	ID         string    `json:"id"`
	ThreadID   string    `json:"threadId"`
	RoundID    string    `json:"roundId,omitempty"`
	Kind       string    `json:"kind"`
	Title      string    `json:"title"`
	Source     string    `json:"source,omitempty"`
	ObjectHash string    `json:"objectHash,omitempty"`
	Metadata   []byte    `json:"metadata,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

type Annotation struct {
	Target        *AnnotationTarget `json:"target,omitempty"`
	ID            string            `json:"id"`
	ThreadID      string            `json:"threadId"`
	RoundID       string            `json:"roundId,omitempty"`
	ParticipantID string            `json:"participantId,omitempty"`
	Path          string            `json:"path,omitempty"`
	StartLine     int               `json:"startLine,omitempty"`
	EndLine       int               `json:"endLine,omitempty"`
	Body          string            `json:"body"`
	CreatedAt     time.Time         `json:"createdAt"`
}

type AgentRun struct {
	ID        string    `json:"id"`
	ThreadID  string    `json:"threadId"`
	Provider  string    `json:"provider"`
	SessionID string    `json:"sessionId,omitempty"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"startedAt"`
	ClosedAt  time.Time `json:"closedAt,omitempty"`
}

type Share struct {
	ID           string    `json:"id"`
	ThreadID     string    `json:"threadId"`
	SecretHash   string    `json:"-"`
	ServerSPKI   string    `json:"serverSpki"`
	Capabilities []byte    `json:"capabilities"`
	ExpiresAt    time.Time `json:"expiresAt"`
	RevokedAt    time.Time `json:"revokedAt,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Participant struct {
	ID       string    `json:"id"`
	ShareID  string    `json:"shareId"`
	Name     string    `json:"name"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joinedAt"`
	LastSeen time.Time `json:"lastSeen"`
}

type ControlLease struct {
	ShareID       string    `json:"shareId"`
	ParticipantID string    `json:"participantId,omitempty"`
	Epoch         int64     `json:"epoch"`
	ExpiresAt     time.Time `json:"expiresAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type Command struct {
	ShareID          string    `json:"shareId"`
	ID               string    `json:"id"`
	Kind             string    `json:"kind"`
	ExpectedRevision int64     `json:"expectedRevision"`
	LeaseEpoch       int64     `json:"leaseEpoch"`
	RequestObject    string    `json:"requestObject,omitempty"`
	Status           string    `json:"status"`
	ResultObject     string    `json:"resultObject,omitempty"`
	ErrorText        string    `json:"error,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	CompletedAt      time.Time `json:"completedAt,omitempty"`
}

type Event struct {
	Seq       int64     `json:"seq"`
	ThreadID  string    `json:"threadId"`
	Type      string    `json:"type"`
	Payload   []byte    `json:"payload"`
	CreatedAt time.Time `json:"createdAt"`
}

type Object struct {
	Hash      string    `json:"hash"`
	Size      int64     `json:"size"`
	MIME      string    `json:"mime"`
	CreatedAt time.Time `json:"createdAt"`
}
