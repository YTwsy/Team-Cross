package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"sync"
	"time"

	"teamcross/internal/bridgeclient"
	"teamcross/internal/domain"
	"teamcross/internal/platform"
	"teamcross/internal/storage"
	"teamcross/internal/webassets"
)

type Config struct {
	Repo          string
	DataDir       string
	Version       string
	DevWeb        string
	BridgeCommand string
	BridgeArgs    []string
	Logger        *slog.Logger
}

type managedRun struct {
	ID               string
	ThreadID         string
	Provider         string
	SessionID        string
	TurnID           string
	Status           string
	NetworkEnabled   bool
	IdentityConflict bool
}

type hostedShare struct {
	ID          string
	ThreadID    string
	Token       string
	Runtime     shareRuntime
	ExpiresAt   time.Time
	Transports  []string
	ExpiryTimer *time.Timer
}

type App struct {
	config    Config
	paths     platform.Paths
	store     *storage.Store
	logger    *slog.Logger
	api       *http.ServeMux
	closeOnce sync.Once
	closeErr  error

	bridge         *bridgeclient.Client
	bridgeCancel   context.CancelFunc
	snapshotReader func(context.Context, string, string) (domain.SessionSnapshot, error)
	nativeOpener   func(context.Context, string) error
	followReader   func(context.Context, domain.SessionFollow) (domain.SessionPoll, error)
	followMu       sync.Mutex
	followCtx      context.Context
	followCancel   context.CancelFunc
	followWorkers  map[string]context.CancelFunc
	followWG       sync.WaitGroup
	followClosed   bool

	handoffTimeout         time.Duration
	handoffTerminalTimeout time.Duration

	mu            sync.RWMutex
	roundMu       sync.Mutex
	runs          map[string]*managedRun
	runsByID      map[string]*managedRun
	switching     map[string]bool
	agentOps      map[string]int
	shares        map[string]*hostedShare
	shareByID     map[string]*hostedShare
	shareStarting map[string]*shareCreation
	sharesClosed  bool
	shareWG       sync.WaitGroup
	shareStarter  shareStartFunc
	handoff       map[string]*handoffWaiter
}

func Open(ctx context.Context, config Config) (*App, error) {
	if config.Version == "" {
		config.Version = "0.1.0-dev"
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	paths, err := platform.DataPaths(config.DataDir)
	if err != nil {
		return nil, err
	}
	store, err := storage.Open(ctx, paths.Root)
	if err != nil {
		return nil, err
	}
	repo := config.Repo
	if repo == "" {
		repo = "."
	}
	if absolute, absoluteErr := filepath.Abs(repo); absoluteErr == nil {
		config.Repo = absolute
	}
	app := &App{
		config:    config,
		paths:     paths,
		store:     store,
		logger:    config.Logger,
		runs:      make(map[string]*managedRun),
		runsByID:  make(map[string]*managedRun),
		switching: make(map[string]bool),
		agentOps:  make(map[string]int),
		shares:    make(map[string]*hostedShare),
		shareByID: make(map[string]*hostedShare),
		handoff:   make(map[string]*handoffWaiter),
	}
	app.api = app.routes()
	app.startBridge(ctx)
	app.startSessionFollows(ctx)
	return app, nil
}

func (app *App) Store() *storage.Store { return app.store }

func (app *App) Close() error {
	app.closeOnce.Do(func() {
		app.closeShares()
		app.stopFollowWorkers()
		if app.bridgeCancel != nil {
			app.bridgeCancel()
		}
		if app.bridge != nil {
			_ = app.bridge.Close()
		}
		app.closeErr = app.store.Close()
	})
	return app.closeErr
}

func (app *App) Handler() http.Handler {
	root := http.NewServeMux()
	root.Handle("/api/", app.withAccess(access{Mode: "host", Role: "owner"}, app.api))
	if app.config.DevWeb != "" {
		target, err := url.Parse(app.config.DevWeb)
		if err == nil {
			proxy := httputil.NewSingleHostReverseProxy(target)
			root.Handle("/", proxy)
			return secureHeaders(root)
		}
	}
	root.Handle("/", spaHandler())
	return secureHeaders(root)
}

func spaHandler() http.Handler {
	dist, err := fs.Sub(webassets.Dist, "dist")
	if err != nil {
		return http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			http.Error(response, "WebGUI is not built", http.StatusServiceUnavailable)
		})
	}
	files := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		path := filepath.ToSlash(filepath.Clean(request.URL.Path))
		if path == "/" {
			files.ServeHTTP(response, request)
			return
		}
		if _, err := fs.Stat(dist, path[1:]); err == nil {
			files.ServeHTTP(response, request)
			return
		}
		index, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = response.Write(index)
	})
}

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(response, request)
	})
}

func (app *App) startBridge(parent context.Context) {
	var (
		options bridgeclient.StartOptions
		err     error
	)
	if app.config.BridgeCommand != "" {
		options.Command = app.config.BridgeCommand
		options.Args = app.config.BridgeArgs
	} else {
		options, err = bridgeclient.DefaultCommand()
		if err != nil {
			app.logger.Warn("Agent Bridge unavailable", "error", err)
			return
		}
	}
	for attempt := 1; attempt <= 2; attempt++ {
		ctx, cancel := context.WithCancel(parent)
		client, startErr := bridgeclient.Start(ctx, options)
		if startErr == nil {
			probeCtx, probeCancel := context.WithTimeout(parent, 3*time.Second)
			_, startErr = client.Probe(probeCtx)
			probeCancel()
		}
		if startErr == nil {
			app.bridge = client
			app.bridgeCancel = cancel
			go app.consumeBridgeEvents(client)
			return
		}
		if client != nil {
			_ = client.Close()
		}
		cancel()
		if attempt == 1 && parent.Err() == nil {
			app.logger.Warn("Agent Bridge readiness failed; restarting once", "error", startErr)
			continue
		}
		app.logger.Warn("Agent Bridge unavailable", "error", startErr)
		return
	}
}

func (app *App) consumeBridgeEvents(client *bridgeclient.Client) {
	for event := range client.Events() {
		app.consumeBridgeEvent(event)
	}
}

func (app *App) consumeBridgeEvent(event bridgeclient.Event) {
	app.mu.RLock()
	known := app.runsByID[event.RunID]
	matchesProvider := known != nil && (event.Provider == "" || event.Provider == known.Provider)
	app.mu.RUnlock()
	if !matchesProvider {
		return
	}
	if event.Type == "run.started" || event.Type == "run.status" {
		if identity, ok := event.Data["sessionId"].(string); ok {
			if err := app.adoptManagedRunIdentity(context.Background(), known, identity); err != nil {
				app.logger.Warn("reject bridge Session identity", "run_id", event.RunID, "error", err)
				return
			}
		}
	}
	app.mu.Lock()
	run := app.runsByID[event.RunID]
	var runSnapshot *managedRun
	waiter := app.handoff[event.RunID]
	isHandoff := waiter != nil && event.TurnID != "" && (waiter.turnID == "" || waiter.turnID == event.TurnID)
	if run != nil {
		// Archived/closed Runs stay historical even if their old process sends
		// delayed events. Identity completion does not grant Writer ownership.
		if run.Status != "archived" && run.Status != "closed" && !run.IdentityConflict {
			if value, ok := event.Data["status"].(string); ok {
				run.Status = value
			}
			switch event.Type {
			case "input.requested":
				run.Status = "waiting"
			case "input.resolved":
				run.Status = "running"
			case "turn.completed":
				run.Status = "idle"
			case "run.closed":
				run.Status = "closed"
			case "run.error":
				run.Status = "error"
			}
			if event.TurnID != "" {
				run.TurnID = event.TurnID
			}
		}
		snapshot := *run
		runSnapshot = &snapshot
	}
	if event.Type == "message.completed" && isHandoff {
		if text, ok := event.Data["text"].(string); ok {
			waiter.summaries[event.TurnID] = text
		}
	}
	shouldSeal := event.Type == "turn.completed" && !isHandoff && app.canSealRunLocked(run)
	app.mu.Unlock()
	if runSnapshot == nil {
		return
	}
	payload := cloneMap(event.Data)
	if _, supplied := payload["sessionId"]; supplied {
		// Never publish the legacy placeholder or an unadopted alternate ID
		// as the Run's native identity in the public event projection.
		payload["sessionId"] = runSnapshot.SessionID
	}
	payload["provider"] = runSnapshot.Provider
	payload["runId"] = runSnapshot.ID
	if event.TurnID != "" {
		payload["turnId"] = event.TurnID
	}
	if event.MessageID != "" {
		payload["messageId"] = event.MessageID
	}
	if event.ToolID != "" {
		payload["toolId"] = event.ToolID
	}
	if event.InputRequestID != "" {
		payload["inputRequestId"] = event.InputRequestID
	}
	encoded, _ := json.Marshal(payload)
	persisted, err := app.store.AppendEvent(context.Background(), runSnapshot.ThreadID, event.Type, encoded)
	if err != nil {
		app.logger.Warn("persist bridge event", "error", err, "type", event.Type)
		return
	}
	if event.Type == "turn.completed" && isHandoff {
		app.mu.Lock()
		if app.handoff[event.RunID] == waiter {
			waiter.completed[event.TurnID] = true
			app.signalHandoffIfCompleteLocked(waiter)
		}
		app.mu.Unlock()
	}
	if shouldSeal {
		go app.sealCompletedTurn(runSnapshot, event.TurnID, persisted, cloneMap(event.Data))
	}
}

func cloneMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source)+2)
	for key, value := range source {
		result[key] = value
	}
	return result
}

func hashSecret(secret []byte) string {
	sum := sha256.Sum256(secret)
	return hex.EncodeToString(sum[:])
}

func joinErrors(values ...error) error {
	var filtered []error
	for _, value := range values {
		if value != nil {
			filtered = append(filtered, value)
		}
	}
	return errors.Join(filtered...)
}

func jsonBytes(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

func (app *App) contextManifestPath(threadID string) string {
	return filepath.Join(app.paths.Contexts, fmt.Sprintf("%s.json", threadID))
}
