package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"teamcross/internal/nativeclaude"
	"teamcross/internal/nativecodex"
	"teamcross/internal/problem"
)

func providerName(v string) (string, error) {
	if v == "" || v == "codex" {
		return "codex", nil
	}
	if v == "claude" {
		return v, nil
	}
	return "", fmt.Errorf("请选择 Codex 或 Claude Code")
}
func (a *App) providerHome(provider string) (string, error) {
	if provider == "claude" {
		if a.Config.ClaudeHome != "" {
			return filepath.Abs(a.Config.ClaudeHome)
		}
		return nativeclaude.Home()
	}
	return nativecodex.Home()
}
func (a *App) claudeBinary() (string, error) {
	a.mu.Lock()
	path := a.settings.ClaudeBinary
	a.mu.Unlock()
	if path != "" {
		return exec.LookPath(path)
	}
	return nativeclaude.Binary()
}
func (a *App) claudeInfo(ctx context.Context) (string, string, string) {
	b, err := a.claudeBinary()
	if err != nil {
		return "", "", err.Error()
	}
	v, err := nativeclaude.Version(ctx, b)
	if err != nil {
		return b, v, err.Error()
	}
	if err = nativeclaude.ValidateVersion(v); err != nil {
		return b, v, err.Error()
	}
	return b, v, ""
}
func (a *App) claudeSource(id string) (nativeclaude.History, error) {
	home, err := a.providerHome("claude")
	if err != nil {
		return nativeclaude.History{}, err
	}
	return nativeclaude.Read(home, id)
}
func marshalInto(v, out any) error {
	if out == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
func historyCall(h nativeclaude.History, method string, params map[string]any, out any) error {
	switch method {
	case "thread/read":
		h.Turns = nil
		return marshalInto(map[string]any{"thread": h}, out)
	case "thread/turns/list":
		limit := 8
		switch v := params["limit"].(type) {
		case int:
			limit = v
		case float64:
			limit = int(v)
		}
		cursor, _ := params["cursor"].(string)
		page, next, err := h.Page(cursor, limit)
		if err != nil {
			return err
		}
		return marshalInto(map[string]any{"data": page, "nextCursor": emptyCursor(next)}, out)
	default:
		return fmt.Errorf("Claude 读取入口不支持 %s", method)
	}
}
func emptyCursor(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func (a *App) sourceCall(ctx context.Context, provider, method string, params map[string]any, out any) error {
	if provider != "claude" {
		return a.readerCall(ctx, method, params, out)
	}
	id, _ := params["threadId"].(string)
	h, err := a.claudeSource(id)
	if err != nil {
		return err
	}
	return historyCall(h, method, params, out)
}
func (a *App) SourcesFor(ctx context.Context, provider, search, cursor string) (map[string]any, error) {
	provider, err := providerName(provider)
	if err != nil {
		return nil, err
	}
	if provider == "codex" {
		return a.Sources(ctx, search, cursor)
	}
	home, err := a.providerHome(provider)
	if err != nil {
		return nil, err
	}
	rows, next, err := nativeclaude.List(home, search, cursor)
	if err != nil {
		return nil, err
	}
	return map[string]any{"data": rows, "nextCursor": emptyCursor(next)}, nil
}
func (s *Session) createClaude(ctx context.Context, preview Preview, title string) error {
	s.mu.Lock()
	r := s.record
	s.mu.Unlock()
	source, err := s.app.claudeSource(r.SourceID)
	if err != nil {
		return err
	}
	if source.Fingerprint != preview.SourceFingerprint {
		return fmt.Errorf("Claude 来源已变化，请重新查看起点")
	}
	binary, err := s.app.claudeBinary()
	if err != nil {
		return err
	}
	launch, err := s.annotationLaunch()
	if err != nil {
		return err
	}
	dir := filepath.Join(s.app.Config.DataDir, "collaborations", r.ID)
	driver, err := nativeclaude.Fork(ctx, nativeclaude.Config{Binary: binary, Home: r.ProviderHome, RuntimeDir: filepath.Join(dir, "claude-runtime"), Cwd: r.ExecutionCwd, Log: filepath.Join(dir, "runtime.log"), MCPConfig: launch.claudeConfig()}, source, title)
	if err != nil {
		return err
	}
	p := newClaudeRuntime(driver)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		p.Close()
		return fmt.Errorf("Core 已退出")
	}
	s.record.SessionID = driver.Job.SessionID
	s.record.NativeJobID = driver.Job.ID
	s.record.Model = source.Model
	s.record.ReasoningEffort = source.ReasoningEffort
	s.record.State = "ready"
	s.record.UpdatedAt = time.Now()
	s.process = p
	s.annotationAccess = true
	s.online = true
	s.generation++
	generation := s.generation
	err = s.saveLocked()
	p.SetHandler(func(m nativecodex.Message) { s.onRuntimeMessage(generation, m) })
	s.mu.Unlock()
	if err != nil {
		p.Close()
	}
	return err
}
func (a *App) restoreClaude(ctx context.Context, r Record) (*claudeRuntime, error) {
	binary, err := a.claudeBinary()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(a.Config.DataDir, "collaborations", r.ID)
	p, err := nativeclaude.Restore(ctx, nativeclaude.Config{Binary: binary, Home: r.ProviderHome, RuntimeDir: filepath.Join(dir, "claude-runtime"), Cwd: r.ExecutionCwd, Log: filepath.Join(dir, "runtime.log")}, r.SessionID)
	if err != nil {
		return nil, err
	}
	return newClaudeRuntime(p), nil
}
func claudeContext(r Record, dataDir string, cursors []string) (any, error) {
	runtimeDir := filepath.Join(dataDir, "collaborations", r.ID, "claude-runtime")
	h, err := nativeclaude.SavedHistory(r.ProviderHome, runtimeDir, r.SessionID, r.ExecutionCwd)
	if err != nil {
		return nil, err
	}
	cursor := ""
	if len(cursors) > 0 {
		cursor = cursors[0]
	}
	page, next, err := h.Page(cursor, 8)
	if err != nil {
		return nil, err
	}
	slices.Reverse(page)
	h.Turns = page
	h.Path = ""
	return map[string]any{"thread": h, "nextCursor": emptyCursor(next)}, nil
}
func claudeMethod(method string, params map[string]any) error {
	switch method {
	case "thread/read", "thread/turns/list", "thread/list", "thread/loaded/list", "thread/unsubscribe":
		return nil
	case "turn/start":
		for _, key := range []string{"model", "modelProvider", "effort", "config", "collaborationMode"} {
			if params[key] != nil {
				return problem.New("native_client_required", "请在 Claude 原生 TUI 中选择模型与推理强度", "")
			}
		}
		_, err := claudeText(params)
		return err
	default:
		return problem.New("native_client_required", "Claude 当前请在原生 TUI 中进行补充、中断、审批或设置操作", "")
	}
}
func claudeText(params map[string]any) (string, error) {
	b, _ := json.Marshal(params["input"])
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(b, &parts) != nil || len(parts) != 1 || parts[0].Type != "text" || strings.TrimSpace(parts[0].Text) == "" || len(parts[0].Text) > 256<<10 {
		return "", fmt.Errorf("Claude 辅助输入当前支持一段不超过 256 KiB 的文本")
	}
	return parts[0].Text, nil
}

type claudeRuntime struct {
	driver  *nativeclaude.Process
	mu      sync.Mutex
	handler func(nativecodex.Message)
	cancel  context.CancelFunc
	done    chan struct{}
	alive   atomic.Bool
	once    sync.Once
}

func newClaudeRuntime(p *nativeclaude.Process) *claudeRuntime {
	r := &claudeRuntime{driver: p, done: make(chan struct{})}
	r.alive.Store(true)
	return r
}
func (p *claudeRuntime) Initialization() json.RawMessage {
	return json.RawMessage(`{"provider":"claude"}`)
}
func (p *claudeRuntime) Alive() bool { return p.alive.Load() && p.driver.Alive() }
func (p *claudeRuntime) Reply(context.Context, json.RawMessage, any) error {
	return problem.New("native_client_required", "请在 Claude 原生 TUI 中回应审批", "")
}
func (p *claudeRuntime) SetHandler(h func(nativecodex.Message)) {
	p.mu.Lock()
	p.handler = h
	if p.cancel != nil {
		p.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.mu.Unlock()
	go p.poll(ctx)
}
func (p *claudeRuntime) emit(method string, value any) {
	p.mu.Lock()
	h := p.handler
	p.mu.Unlock()
	if h != nil {
		b, _ := json.Marshal(value)
		h(nativecodex.Message{Method: method, Params: b})
	}
}
func (p *claudeRuntime) poll(ctx context.Context) {
	defer close(p.done)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	lastModel := ""
	lastHistory := ""
	failures := 0
	for {
		job, err := p.driver.Status(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			failures++
		} else {
			failures = 0
		}
		if failures >= 3 || (err == nil && (job.State == "failed" || job.State == "stopped")) {
			p.alive.Store(false)
			p.emit("teamcross/runtimeDisconnected", nil)
			return
		}
		if err == nil {
			state := map[string]any{"busy": job.Busy(), "waitingFor": job.WaitingFor, "state": job.State, "status": job.Status}
			p.emit("teamcross/claudeState", state)
			if h, e := p.driver.History(); e == nil && h.Model != "" {
				if h.Fingerprint != lastHistory {
					lastHistory = h.Fingerprint
					p.emit("teamcross/claudeHistory", map[string]any{"threadId": p.driver.Job.SessionID})
				}
				modelKey, _ := json.Marshal([]any{h.Model, h.ReasoningEffort})
				if string(modelKey) != lastModel {
					lastModel = string(modelKey)
					p.emit("thread/settings/updated", map[string]any{"threadId": p.driver.Job.SessionID, "threadSettings": map[string]any{"model": h.Model, "effort": h.ReasoningEffort}})
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
func (p *claudeRuntime) Close() {
	p.once.Do(func() {
		p.alive.Store(false)
		p.mu.Lock()
		cancel := p.cancel
		p.mu.Unlock()
		if cancel != nil {
			cancel()
			<-p.done
		}
		p.driver.Close()
	})
}
func (p *claudeRuntime) Call(ctx context.Context, method string, params, out any) error {
	var in map[string]any
	b, _ := json.Marshal(params)
	_ = json.Unmarshal(b, &in)
	if method == "turn/start" {
		text, err := claudeText(in)
		if err != nil {
			return err
		}
		if err = p.driver.Send(ctx, text); err != nil {
			return err
		}
		return marshalInto(map[string]any{"accepted": true, "provider": "claude"}, out)
	}
	h, err := p.driver.History()
	if err != nil {
		return err
	}
	h.Path = ""
	return historyCall(h, method, in, out)
}
func (p *claudeRuntime) Attach(ctx context.Context, r nativeclaude.TerminalRequest) (net.Conn, json.RawMessage, error) {
	return p.driver.Attach(ctx, r)
}
func (p *claudeRuntime) Resize(ctx context.Context, r nativeclaude.TerminalRequest) error {
	return p.driver.Resize(ctx, r)
}

func (a *App) claudeClientPlan(ctx context.Context, id, client string, launch bool, view map[string]any) (map[string]any, error) {
	if client != "tui" {
		return nil, problem.New("client_unsupported", "Claude 协作当前支持原生 TUI", "")
	}
	binary, err := a.claudeBinary()
	if err != nil {
		return nil, err
	}
	job, _ := view["nativeJobId"].(string)
	if job == "" {
		return nil, fmt.Errorf("主机未确认 Claude 后台会话")
	}
	a.mu.Lock()
	existing := a.claudeClients[id]
	closed := a.closed
	a.mu.Unlock()
	if closed {
		return nil, fmt.Errorf("Core 已退出")
	}
	if existing == nil {
		home := filepath.Join(a.Config.DataDir, "clients", id, "tui", "claude-home")
		cwd := filepath.Join(a.Config.DataDir, "clients", id, "tui", "workspace")
		bridge, err := nativeclaude.StartClient(ctx, binary, home, cwd, job, func(ctx context.Context) (*websocket.Conn, error) { return a.dialClaude(ctx, id) })
		if err != nil {
			return nil, err
		}
		a.mu.Lock()
		if a.closed {
			a.mu.Unlock()
			bridge.Close()
			return nil, fmt.Errorf("Core 已退出")
		}
		if a.claudeClients == nil {
			a.claudeClients = map[string]*nativeclaude.Client{}
		}
		if a.claudeClients[id] != nil {
			a.mu.Unlock()
			bridge.Close()
			return nil, fmt.Errorf("客户端正在准备，请重试")
		}
		a.claudeClients[id] = bridge
		existing = bridge
		a.mu.Unlock()
	}
	command := "cd " + nativecodex.Quote(existing.Cwd) + " && env CLAUDE_CONFIG_DIR=" + nativecodex.Quote(existing.Home) + " " + nativecodex.Quote(binary) + " attach " + nativecodex.Quote(job)
	if launch {
		cmd := exec.Command("osascript", "-e", "tell application \"Terminal\"\nactivate\ndo script "+strconv.Quote(command)+"\nend tell")
		if b, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("无法打开 Claude TUI：%v %s；可复制启动命令", err, b)
		}
	}
	return map[string]any{"client": "tui", "provider": "claude", "command": command, "launched": launch, "sessionId": view["sessionId"], "note": "使用 Claude 原生 TUI 输入、审批和中断；执行保留在发起者主机。"}, nil
}
func (a *App) dialClaude(ctx context.Context, id string) (*websocket.Conn, error) {
	a.mu.Lock()
	s, j := a.sessions[id], a.joined[id]
	closed := a.closed
	a.mu.Unlock()
	if closed {
		return nil, fmt.Errorf("Core 已退出")
	}
	if s != nil {
		url, err := a.endpoint(id)
		if err != nil {
			return nil, err
		}
		conn, _, err := websocket.Dial(ctx, url, nil)
		return conn, err
	}
	if j != nil {
		j.mu.Lock()
		if j.left || j.ended || j.closed || !j.confirmed {
			j.mu.Unlock()
			return nil, fmt.Errorf("协作访问已结束")
		}
		url, client, credential := j.URL, j.Client, j.Credential
		j.mu.Unlock()
		conn, _, err := websocket.Dial(ctx, strings.Replace(url, "https:", "wss:", 1)+"/v2/connect", &websocket.DialOptions{HTTPClient: client, HTTPHeader: http.Header{"Authorization": []string{"Bearer " + credential}}})
		return conn, err
	}
	return nil, fmt.Errorf("没有找到协作")
}
