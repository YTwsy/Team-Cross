// Package shadow manages a space-shared conversation over native Codex sessions.
// The durable conversation, memories and jobs outlive individual native segments.
package shadow

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"teamcross/internal/nativecodex"
)

type Runtime interface {
	Call(context.Context, string, any, any) error
	Reply(context.Context, json.RawMessage, any) error
	SetHandler(func(nativecodex.Message))
	Alive() bool
	Close()
}

type Config struct {
	Directory string
	Start     func(context.Context, string, string, string) (Runtime, error)
	// Timing and budget knobs are injectable for deterministic replay; the
	// production adapter leaves model selection to the native configuration.
	CheckRatio       float64
	IdleMemoryDelay  time.Duration
	MaxBackground    int
	AuxiliaryTimeout time.Duration
}

type Actor struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Message struct {
	ID         string     `json:"id"`
	Actor      Actor      `json:"actor"`
	Role       string     `json:"role"`
	Text       string     `json:"text"`
	State      string     `json:"state"` // delivered, submitted, completed, failed, unknown
	ReplyTo    string     `json:"replyTo,omitempty"`
	SegmentID  string     `json:"segmentId,omitempty"`
	TurnID     string     `json:"turnId,omitempty"`
	Error      string     `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	ReceivedAt *time.Time `json:"receivedAt,omitempty"`
}

type Segment struct {
	ID              string     `json:"id"`
	Number          int        `json:"number"`
	CreatedAt       time.Time  `json:"createdAt"`
	ClosedAt        *time.Time `json:"closedAt,omitempty"`
	Checkpoint      string     `json:"checkpoint,omitempty"`
	MemoryRevision  int        `json:"memoryRevision"`
	InputTokens     int64      `json:"inputTokens"`
	UsageValid      bool       `json:"usageValid"`
	ContextWindow   int64      `json:"contextWindow"`
	Compactions     int        `json:"compactions"`
	LastCheckTokens int64      `json:"lastCheckTokens"`
}

type Memory struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Scope     string    `json:"scope"` // member or space
	SubjectID string    `json:"subjectId,omitempty"`
	SourceIDs []string  `json:"sourceIds"`
	Revision  int       `json:"revision"`
	State     string    `json:"state"` // active, candidate, forgotten
	UpdatedAt time.Time `json:"updatedAt"`
}

type Topic struct {
	ID          string   `json:"id"`
	Goal        string   `json:"goal"`
	State       string   `json:"state"`
	Constraints []string `json:"constraints"`
	SourceIDs   []string `json:"sourceIds"`
}

type Job struct {
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	Prompt           string    `json:"prompt"`
	SourceID         string    `json:"sourceId"`
	Version          int       `json:"version"`
	RunningVersion   int       `json:"runningVersion"`
	ThreadID         string    `json:"threadId,omitempty"`
	State            string    `json:"state"` // queued, running, stopping, completed, failed, cancelled, interrupted, unknown
	Result           string    `json:"result,omitempty"`
	ResultVersion    int       `json:"resultVersion,omitempty"`
	ResultEventID    string    `json:"resultEventId,omitempty"`
	DeliveredEventID string    `json:"deliveredEventId,omitempty"`
	Error            string    `json:"error,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
	StopReason       string    `json:"stopReason,omitempty"`
}

// A run is written before the native request. An unacknowledged request is
// reconciled against native history, never retried as a fresh model input.
type Run struct {
	ID              string            `json:"id"`
	Kind            string            `json:"kind"` // main, job, result, steward, handoff, memory, compact
	ThreadID        string            `json:"threadId"`
	TurnID          string            `json:"turnId,omitempty"`
	SourceID        string            `json:"sourceId,omitempty"`
	JobID           string            `json:"jobId,omitempty"`
	JobVersion      int               `json:"jobVersion,omitempty"`
	EventID         string            `json:"eventId,omitempty"`
	State           string            `json:"state"` // prepared, submitted, unknown, completed, failed
	Output          string            `json:"output,omitempty"`
	Items           map[string]string `json:"items,omitempty"`
	MemoryRevision  int               `json:"memoryRevision"`
	MemoryWatermark int               `json:"memoryWatermark,omitempty"`
	SegmentID       string            `json:"segmentId,omitempty"`
	Compactions     int               `json:"compactions,omitempty"`
	MessageCount    int               `json:"messageCount,omitempty"`
	Error           string            `json:"error,omitempty"`
	IgnoreResult    bool              `json:"ignoreResult,omitempty"`
	CreatedAt       time.Time         `json:"createdAt"`
}

type Transition struct {
	From            string `json:"from"`
	To              string `json:"to,omitempty"`
	Stage           string `json:"stage"` // note, create-prepared, ready
	Checkpoint      string `json:"checkpoint,omitempty"`
	Model           string `json:"model,omitempty"`
	ModelProvider   string `json:"modelProvider,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}

type Decision struct {
	Action       string `json:"action"`
	Reason       string `json:"reason"`
	SegmentID    string `json:"segmentId"`
	Compactions  int    `json:"compactions"`
	MessageCount int    `json:"messageCount"`
}

type Record struct {
	Model           string                     `json:"model,omitempty"`
	ModelProvider   string                     `json:"modelProvider,omitempty"`
	ReasoningEffort string                     `json:"reasoningEffort,omitempty"`
	Schema          int                        `json:"schema"`
	ID              string                     `json:"id"`
	SpaceID         string                     `json:"spaceId"`
	Name            string                     `json:"name"`
	Owner           Actor                      `json:"owner"`
	Home            string                     `json:"home"`
	Cwd             string                     `json:"cwd"`
	Enabled         bool                       `json:"enabled"`
	Revision        int                        `json:"revision"`
	Current         string                     `json:"current"`
	Segments        []Segment                  `json:"segments"`
	Messages        []Message                  `json:"messages"`
	Memories        map[string]Memory          `json:"memories"`
	MemoryRevision  int                        `json:"memoryRevision"`
	MemoryWatermark int                        `json:"memoryWatermark"`
	Topics          map[string]Topic           `json:"topics"`
	Jobs            map[string]Job             `json:"jobs"`
	Runs            map[string]Run             `json:"runs"`
	ToolResults     map[string]json.RawMessage `json:"toolResults"`
	Transition      *Transition                `json:"transition,omitempty"`
	Decision        *Decision                  `json:"decision,omitempty"`
	Creating        string                     `json:"creating,omitempty"`
	Error           string                     `json:"error,omitempty"`
	CreatedAt       time.Time                  `json:"createdAt"`
	UpdatedAt       time.Time                  `json:"updatedAt"`
}

// View is safe to replicate to the space host: native configuration paths,
// permissions requests and tool ledgers remain on the executing member's Core.
type View struct {
	Model           string    `json:"model,omitempty"`
	ModelProvider   string    `json:"modelProvider,omitempty"`
	ReasoningEffort string    `json:"reasoningEffort,omitempty"`
	ID              string    `json:"id"`
	SpaceID         string    `json:"spaceId"`
	Name            string    `json:"name"`
	Owner           Actor     `json:"owner"`
	Revision        int       `json:"revision"`
	Enabled         bool      `json:"enabled"`
	Online          bool      `json:"online"`
	Busy            bool      `json:"busy"`
	Current         string    `json:"current"`
	Segments        []Segment `json:"segments"`
	Messages        []Message `json:"messages"`
	Memories        []Memory  `json:"memories"`
	Jobs            []Job     `json:"jobs"`
	Error           string    `json:"error,omitempty"`
}

type Approval struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type Engine struct {
	mu              sync.Mutex
	cfg             Config
	record          Record
	runtime         Runtime
	events          []nativeEvent
	releaseThreads  []string
	generation      int
	approvals       map[string]Approval
	compacting      map[string]bool
	wake            chan struct{}
	done            chan struct{}
	cancel          context.CancelFunc
	started         bool
	closed          bool
	lastActivity    time.Time
	lastMaintenance time.Time
}

type nativeEvent struct {
	generation int
	message    nativecodex.Message
}
