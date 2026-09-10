package collab

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"teamcross/internal/nativecodex"
	"teamcross/internal/sharing"
	"teamcross/internal/workspace"
	"time"
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
	Loopback                          bool
	StartProcess                      func(string, string, string, string) (Runtime, error)
}
type Source struct {
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
	SourceID      string `json:"sourceId"`
	WorkspaceMode string `json:"workspaceMode"`
	Title         string `json:"title"`
	PreviewHash   string `json:"previewHash"`
	RequestID     string `json:"requestId"`
}
type Preview struct {
	TargetDirectory string            `json:"targetDirectory"`
	Source          Source            `json:"source"`
	SourceTurnID    string            `json:"sourceTurnId"`
	Workspace       workspace.Preview `json:"workspace"`
	Hash            string            `json:"previewHash"`
}
type Annotation struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Reference string    `json:"reference,omitempty"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"createdAt"`
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
type Record struct {
	Model           string             `json:"model,omitempty"`
	ModelProvider   string             `json:"modelProvider,omitempty"`
	ReasoningEffort *string            `json:"reasoningEffort,omitempty"`
	ID              string             `json:"id"`
	Title           string             `json:"title"`
	SourceID        string             `json:"sourceId"`
	SourceTurnID    string             `json:"sourceTurnId"`
	SessionID       string             `json:"sessionId"`
	WorkspaceMode   string             `json:"workspaceMode"`
	Repo            string             `json:"repo"`
	ExecutionCwd    string             `json:"executionCwd"`
	WorkspaceRoot   string             `json:"workspaceRoot"`
	WorkspaceOwned  bool               `json:"workspaceOwned"`
	Head            string             `json:"head"`
	Branch          string             `json:"branch"`
	ProviderHome    string             `json:"providerHome"`
	State           string             `json:"state"`
	Error           string             `json:"error,omitempty"`
	CreatedAt       time.Time          `json:"createdAt"`
	UpdatedAt       time.Time          `json:"updatedAt"`
	PreviewHash     string             `json:"previewHash"`
	Annotations     []Annotation       `json:"annotations"`
	Commands        map[string]Command `json:"commands,omitempty"`
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
	mu              sync.Mutex
	record          Record
	app             *App
	process         Runtime
	writer          string
	busy            bool
	online          bool
	epoch           uint64
	share           *sharing.Runtime
	releaseWhenIdle bool
	stopping        chan struct{}
	activeCalls     int
	generation      uint64
	closed          bool
	direct          *direct
	events          []Event
	sequence        uint64
	approvals       map[string]Approval
	listener        net.Listener
	server          *http.Server
	endpoint        string
	starting        bool
	remoteSeen      time.Time
	inputRequested  bool
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
}
type Settings struct {
	Binary     string `json:"binary"`
	DesktopApp string `json:"desktopApp"`
}
type App struct {
	joinMu        sync.Mutex
	retiredShares []*sharing.Runtime
	mu            sync.Mutex
	Config        Config
	Host          string
	URL           string
	Token         string
	settings      Settings
	reader        Runtime
	readerMu      sync.Mutex
	sessions      map[string]*Session
	joined        map[string]*Joined
	closed        bool
	pending       map[string]pendingInvite
	mcpObserved   time.Time
	mcpProbed     bool
}
