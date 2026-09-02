package bridgeclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const ProtocolVersion = 1

type Event struct {
	RunID          string         `json:"runId,omitempty"`
	Provider       string         `json:"provider,omitempty"`
	Type           string         `json:"type"`
	Timestamp      time.Time      `json:"timestamp,omitempty"`
	TurnID         string         `json:"turnId,omitempty"`
	MessageID      string         `json:"messageId,omitempty"`
	ToolID         string         `json:"toolId,omitempty"`
	InputRequestID string         `json:"inputRequestId,omitempty"`
	Data           map[string]any `json:"data,omitempty"`
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type pendingCall struct {
	result chan rpcMessage
}

type storedError struct {
	err error
}

type Client struct {
	command *exec.Cmd
	stdin   io.WriteCloser

	mu      sync.Mutex
	pending map[string]pendingCall
	events  chan Event
	done    chan struct{}
	err     atomic.Value
	nextID  atomic.Uint64
	close   sync.Once
}

type StartOptions struct {
	Command string
	Args    []string
	Dir     string
	Env     []string
}

type Readiness struct {
	OK              bool   `json:"ok"`
	Name            string `json:"name"`
	Version         string `json:"version"`
	ProtocolVersion int    `json:"protocolVersion"`
}

// DefaultCommand finds the Agent Bridge independently of the repository passed
// to teamcross serve. TEAMCROSS_SOURCE_ROOT is an explicit development override;
// installed layouts are resolved relative to the Team Cross executable before
// falling back to a PATH-installed bridge and source checkouts above the cwd.
func DefaultCommand() (StartOptions, error) {
	executable, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	working, _ := os.Getwd()
	pathBridge, _ := exec.LookPath("teamcross-agent-bridge")
	return resolveDefaultCommand(os.Getenv("TEAMCROSS_SOURCE_ROOT"), executable, working, pathBridge)
}

func resolveDefaultCommand(sourceRoot, executable, working, pathBridge string) (StartOptions, error) {
	var candidates []string
	appendSourceCandidate := func(root string) {
		if root == "" {
			return
		}
		candidates = append(candidates,
			filepath.Join(root, "packages", "agent-bridge", "dist", "index.js"),
			filepath.Join(root, "agent-bridge", "dist", "index.js"),
		)
	}
	if sourceRoot != "" {
		appendSourceCandidate(sourceRoot)
	}
	if executable != "" {
		directory := filepath.Dir(executable)
		candidates = append(candidates,
			filepath.Join(directory, "agent-bridge", "dist", "index.js"),
			filepath.Join(directory, "..", "Resources", "agent-bridge", "dist", "index.js"),
			filepath.Join(directory, "..", "lib", "teamcross", "agent-bridge", "dist", "index.js"),
			filepath.Join(directory, "..", "share", "teamcross", "agent-bridge", "dist", "index.js"),
			filepath.Join(directory, "..", "packages", "agent-bridge", "dist", "index.js"),
		)
	}
	for _, candidate := range candidates {
		if bridgeScriptExists(candidate) {
			absolute, _ := filepath.Abs(candidate)
			return StartOptions{Command: "node", Args: []string{absolute}, Dir: filepath.Dir(absolute)}, nil
		}
	}
	if pathBridge != "" {
		return StartOptions{Command: pathBridge}, nil
	}
	for directory := working; directory != ""; directory = filepath.Dir(directory) {
		if isDevelopmentRoot(directory) {
			before := len(candidates)
			appendSourceCandidate(directory)
			for _, candidate := range candidates[before:] {
				if bridgeScriptExists(candidate) {
					absolute, _ := filepath.Abs(candidate)
					return StartOptions{Command: "node", Args: []string{absolute}, Dir: filepath.Dir(absolute)}, nil
				}
			}
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
	}
	return StartOptions{}, fmt.Errorf("Agent Bridge entrypoint not found; build packages/agent-bridge or set TEAMCROSS_SOURCE_ROOT")
}

func isDevelopmentRoot(path string) bool {
	return bridgeScriptExists(filepath.Join(path, "go.mod")) &&
		bridgeScriptExists(filepath.Join(path, "pnpm-workspace.yaml"))
}

func bridgeScriptExists(path string) bool {
	info, err := os.Stat(filepath.Clean(path))
	return err == nil && info.Mode().IsRegular()
}

func Start(ctx context.Context, options StartOptions) (*Client, error) {
	if options.Command == "" {
		return nil, errors.New("bridge command is required")
	}
	command := exec.CommandContext(ctx, options.Command, options.Args...)
	command.Dir = options.Dir
	if options.Env != nil {
		command.Env = append(os.Environ(), options.Env...)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("bridge stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("bridge stdout: %w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("bridge stderr: %w", err)
	}
	client := &Client{
		command: command,
		stdin:   stdin,
		pending: make(map[string]pendingCall),
		events:  make(chan Event, 256),
		done:    make(chan struct{}),
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start bridge: %w", err)
	}
	go client.readLoop(stdout)
	go client.stderrLoop(stderr)
	go func() {
		err := command.Wait()
		if err != nil {
			client.setErr(err)
		}
		client.shutdown()
	}()
	return client, nil
}

func (client *Client) Events() <-chan Event  { return client.events }
func (client *Client) Done() <-chan struct{} { return client.done }

func (client *Client) Err() error {
	value := client.err.Load()
	if value == nil {
		return nil
	}
	return value.(storedError).err
}

func (client *Client) Call(ctx context.Context, method string, params any, result any) error {
	select {
	case <-client.done:
		return client.stoppedError()
	default:
	}
	id := strconv.FormatUint(client.nextID.Add(1), 10)
	request := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	data, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode bridge request: %w", err)
	}
	waiter := pendingCall{result: make(chan rpcMessage, 1)}
	client.mu.Lock()
	client.pending[id] = waiter
	_, writeErr := client.stdin.Write(append(data, '\n'))
	client.mu.Unlock()
	if writeErr != nil {
		client.removePending(id)
		select {
		case <-client.done:
			return client.stoppedError()
		default:
		}
		return fmt.Errorf("write bridge request: %w", writeErr)
	}
	select {
	case <-ctx.Done():
		client.removePending(id)
		return ctx.Err()
	case <-client.done:
		client.removePending(id)
		return client.stoppedError()
	case response := <-waiter.result:
		if response.Error != nil {
			return fmt.Errorf("bridge error %d: %s", response.Error.Code, response.Error.Message)
		}
		if result == nil || len(response.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(response.Result, result); err != nil {
			return fmt.Errorf("decode bridge result: %w", err)
		}
		return nil
	}
}

func (client *Client) stoppedError() error {
	if err := client.Err(); err != nil {
		return fmt.Errorf("bridge stopped: %w", err)
	}
	return errors.New("bridge stopped")
}

// Probe verifies that the process is responsive and speaks the protocol this
// Go Core expects. It is side-effect-free and safe to use from startup/doctor.
func (client *Client) Probe(ctx context.Context) (Readiness, error) {
	var readiness Readiness
	if err := client.Call(ctx, "bridge.ping", map[string]any{}, &readiness); err != nil {
		return Readiness{}, err
	}
	if !readiness.OK || readiness.Name != "teamcross-agent-bridge" {
		return Readiness{}, fmt.Errorf("unexpected bridge readiness response")
	}
	if readiness.ProtocolVersion != ProtocolVersion {
		return Readiness{}, fmt.Errorf("bridge protocol %d is incompatible with Core protocol %d", readiness.ProtocolVersion, ProtocolVersion)
	}
	return readiness, nil
}

func (client *Client) Close() error {
	client.close.Do(func() {
		_ = client.stdin.Close()
		if client.command.Process != nil {
			_ = client.command.Process.Signal(os.Interrupt)
			select {
			case <-client.done:
			case <-time.After(time.Second):
				_ = client.command.Process.Kill()
			}
		}
	})
	return nil
}

func (client *Client) readLoop(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 8*1024*1024)
	for scanner.Scan() {
		var message rpcMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			client.setErr(fmt.Errorf("invalid bridge JSON: %w", err))
			continue
		}
		if len(message.ID) > 0 {
			id := string(message.ID)
			if len(id) >= 2 && id[0] == '"' {
				id = id[1 : len(id)-1]
			}
			client.mu.Lock()
			pending, ok := client.pending[id]
			if ok {
				delete(client.pending, id)
			}
			client.mu.Unlock()
			if ok {
				pending.result <- message
			}
			continue
		}
		if message.Method == "event" {
			var event Event
			if err := json.Unmarshal(message.Params, &event); err == nil {
				select {
				case client.events <- event:
				default:
					client.setErr(errors.New("bridge event buffer overflow"))
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		client.setErr(err)
	}
}

func (client *Client) setErr(err error) {
	if err != nil {
		client.err.Store(storedError{err: err})
	}
}

func (client *Client) stderrLoop(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		// Stderr is intentionally drained. The bridge sends structured run errors on stdout.
	}
}

func (client *Client) removePending(id string) {
	client.mu.Lock()
	delete(client.pending, id)
	client.mu.Unlock()
}

func (client *Client) shutdown() {
	client.close.Do(func() {})
	select {
	case <-client.done:
		return
	default:
		close(client.done)
		close(client.events)
	}
}
