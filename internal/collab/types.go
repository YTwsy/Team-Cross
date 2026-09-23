package collab

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"

	"teamcross/internal/materialstore"
	"teamcross/internal/nativeclaude"
	"teamcross/internal/nativecodex"
	"teamcross/internal/runtimeconfig"
	"teamcross/internal/sharing"
	"teamcross/internal/workspace"
)

type Runtime interface {
	Call(context.Context, string, any, any) error
	Reply(context.Context, json.RawMessage, any) error
	SetHandler(func(nativecodex.Message))
	Initialization() json.RawMessage
	Alive() bool
	Close()
}

type Config struct {
	DataDir, Repo, Binary, DesktopApp string
	Executable                        string
	ClaudeBinary, ClaudeHome          string
	Loopback                          bool
	StartProcess                      func(string, string, string, string) (Runtime, error)
}
type Source struct {
	Provider        string  `json:"provider,omitempty"`
	Model           string  `json:"model,omitempty"`
	ModelProvider   string  `json:"modelProvider,omitempty"`
	ReasoningEffort *string `json:"reasoningEffort,omitempty"`
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Preview         string  `json:"preview"`
	Cwd             string  `json:"cwd"`
	Path            string  `json:"path,omitempty"`
	UpdatedAt       int64   `json:"updatedAt"`
	Status          struct {
		Type string `json:"type"`
	} `json:"status"`
}
type CreateInput struct {
	SpaceID       string             `json:"spaceId,omitempty"`
	RuntimeMode   runtimeconfig.Mode `json:"runtimeMode"`
	Provider      string             `json:"provider,omitempty"`
	SourceID      string             `json:"sourceId"`
	WorkspaceMode string             `json:"workspaceMode"`
	Title         string             `json:"title"`
	PreviewHash   string             `json:"previewHash"`
	RequestID     string             `json:"requestId"`
}
type Preview struct {
	RuntimeMode       runtimeconfig.Mode `json:"runtimeMode"`
	SourceFingerprint string             `json:"-"`
	TargetDirectory   string             `json:"targetDirectory"`
	Source            Source             `json:"source"`
	SourceTurnID      string             `json:"sourceTurnId"`
	SourceTurnStatus  string             `json:"sourceTurnStatus"`
	Workspace         workspace.Preview  `json:"workspace"`
	Hash              string             `json:"previewHash"`
}
type Annotation struct {
	Materials []MaterialReference `json:"materials,omitempty"`
	ID        string              `json:"id"`
	Text      string              `json:"text"`
	Reference string              `json:"reference,omitempty"`
	Target    *AnnotationTarget   `json:"target,omitempty"`
	Author    string              `json:"author"`
	AuthorID  string              `json:"authorId"`
	CreatedAt time.Time           `json:"createdAt"`
	Replies   []AnnotationReply   `json:"replies,omitempty"`
}

// Replies belong to a root annotation, never to another reply or source range.
type AnnotationReply struct {
	Materials []MaterialReference `json:"materials,omitempty"`
	ID        string              `json:"id"`
	RequestID string              `json:"requestId"`
	Text      string              `json:"text"`
	Author    string              `json:"author"`
	AuthorID  string              `json:"authorId"`
	CreatedAt time.Time           `json:"createdAt"`
}

type AnnotationReplyInput struct {
	Materials    []MaterialReference `json:"materials,omitempty"`
	AnnotationID string              `json:"annotationId"`
	Text         string              `json:"text"`
	RequestID    string              `json:"requestId"`
}

// AnnotationTarget describes the content as it was displayed when selected.
// ContentHash is a SHA-256 of the complete file or displayed diff, not Git HEAD.
type AnnotationTarget struct {
	MaterialID   string `json:"materialId,omitempty"`
	Version      int    `json:"version,omitempty"`
	Kind         string `json:"kind"`
	SessionID    string `json:"sessionId,omitempty"`
	Path         string `json:"path,omitempty"`
	StartLine    int    `json:"startLine,omitempty"`
	EndLine      int    `json:"endLine,omitempty"`
	Side         string `json:"side,omitempty"`
	TurnID       string `json:"turnId,omitempty"`
	ItemID       string `json:"itemId,omitempty"`
	StartOffset  int    `json:"startOffset,omitempty"`
	EndOffset    int    `json:"endOffset,omitempty"`
	Cursor       string `json:"cursor,omitempty"`
	Quote        string `json:"quote"`
	ContentHash  string `json:"contentHash,omitempty"`
	BaseRevision string `json:"baseRevision,omitempty"`
}
type Event struct {
	Sequence uint64          `json:"sequence"`
	Method   string          `json:"method"`
	Params   json.RawMessage `json:"params,omitempty"`
	Time     time.Time       `json:"time"`
}
type Approval struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}
type Command struct {
	ID     string          `json:"id"`
	Hash   string          `json:"hash"`
	State  string          `json:"state"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}
type ExecutionRecord struct {
	RequestID           string             `json:"requestId"`
	RuntimeMode         runtimeconfig.Mode `json:"runtimeMode"`
	ProviderDefaultHome bool               `json:"providerDefaultHome,omitempty"`
	AnnotationToken     string             `json:"annotationToken,omitempty"`
	Provider            string             `json:"provider,omitempty"`
	NativeJobID         string             `json:"nativeJobId,omitempty"`
	Model               string             `json:"model,omitempty"`
	ModelProvider       string             `json:"modelProvider,omitempty"`
	ReasoningEffort     *string            `json:"reasoningEffort,omitempty"`
	SourceID            string             `json:"sourceId"`
	SourceTurnID        string             `json:"sourceTurnId"`
	SessionID           string             `json:"sessionId"`
	WorkspaceMode       string             `json:"workspaceMode"`
	Repo                string             `json:"repo"`
	ExecutionCwd        string             `json:"executionCwd"`
	WorkspaceRoot       string             `json:"workspaceRoot"`
	WorkspaceOwned      bool               `json:"workspaceOwned"`
	Head                string             `json:"head"`
	Branch              string             `json:"branch"`
	ProviderHome        string             `json:"providerHome"`
	PreviewHash         string             `json:"previewHash"`
	Commands            map[string]Command `json:"commands,omitempty"`
}
type Record struct {
	Materials        []Material `json:"materials"`
	Schema           int        `json:"schema"`
	*ExecutionRecord `json:"execution,omitempty"`
	ID               string       `json:"id"`
	Title            string       `json:"title"`
	State            string       `json:"state"`
	Error            string       `json:"error,omitempty"`
	CreatedAt        time.Time    `json:"createdAt"`
	UpdatedAt        time.Time    `json:"updatedAt"`
	Annotations      []Annotation `json:"annotations"`
}

type direct struct {
	subscribed    bool
	ready         bool
	send          chan nativecodex.Message
	done          chan struct{}
	heartbeatOnce sync.Once
	once          sync.Once
	role          string
	kind          string
}

func (d *direct) close() { d.once.Do(func() { close(d.done) }) }

type Session struct {
	mu               sync.Mutex
	materialUploadMu sync.Mutex
	record           Record
	app              *App
	materialStore    *materialstore.Store
	process          Runtime
	writer           string
	busy             bool
	nativeWaiting    string
	nativeLastWrite  time.Time
	online           bool
	epoch            uint64
	share            *sharing.Runtime
	sharePreparing   bool
	shareTransport   sharing.Transport
	shareGeneration  uint64
	shareCancel      context.CancelFunc
	releaseWhenIdle  bool
	annotationAccess bool
	stopping         chan struct{}
	activeCalls      int
	generation       uint64
	closed           bool
	direct           *direct
	events           []Event
	sequence         uint64
	approvals        map[string]Approval
	listener         net.Listener
	server           *http.Server
	endpoint         string
	starting         bool
	presence         map[string]memberPresence
}
type Joined struct {
	app           *App
	ended         bool
	mu            sync.Mutex
	ID            string
	Invitation    sharing.Invitation
	Credential    string
	confirmed     bool
	URL           string
	Client        *http.Client
	Connection    *sharing.Connection
	Last          map[string]any
	Error         string
	listener      net.Listener
	server        *http.Server
	endpoint      string
	left          bool
	closed        bool
	closeOnce     sync.Once
	done          chan struct{}
	heartbeatOnce sync.Once
	statusChecked bool
	refreshing    bool
}
type Settings struct {
	ClaudeBinary string `json:"claudeBinary"`
	Binary       string `json:"binary"`
	DesktopApp   string `json:"desktopApp"`
	UILanguage   string `json:"uiLanguage,omitempty"`
}
type App struct {
	libraryMu     sync.Mutex
	library       libraryState
	spaceCreateMu sync.Mutex
	shareRequests map[string]*shareRequest
	shareWorkers  sync.WaitGroup
	joinMu        sync.Mutex
	mcpSetupMu    sync.Mutex
	retiredShares []*sharing.Runtime
	mu            sync.Mutex
	Config        Config
	Host          string
	URL           string
	Token         string
	settings      Settings
	claudeClients map[string]*nativeclaude.Client
	reader        Runtime
	readerMu      sync.Mutex
	sessions      map[string]*Session
	joined        map[string]*Joined
	closed        bool
	pending       map[string]pendingInvite
	mcpObserved   map[string]time.Time
	mcpProbed     bool
}
