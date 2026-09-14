// Package nativecodex connects native Codex clients to a dedicated app-server.
// It is separate from the managed Agent Bridge: no prompt is generated here.
package nativecodex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"teamcross/internal/buildinfo"
	"time"

	"github.com/coder/websocket"
)

const Profile = "teamcross-native"

type Message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

type Process struct {
	URL          string
	cmd          *exec.Cmd
	conn         *websocket.Conn
	done         chan struct{}
	seq          atomic.Uint64
	mu           sync.Mutex
	pending      map[string]chan Message
	closeOnce    sync.Once
	handler      func(Message)
	Init         json.RawMessage
	disconnected atomic.Bool
}

func Binary() (string, error) {
	if value := os.Getenv("TEAMCROSS_CODEX_BIN"); value != "" {
		return exec.LookPath(value)
	}
	if value, err := exec.LookPath("codex"); err == nil {
		return value, nil
	}
	for _, value := range []string{"/Applications/ChatGPT.app/Contents/Resources/codex", "/Applications/Codex.app/Contents/Resources/codex"} {
		if stat, err := os.Stat(value); err == nil && stat.Mode().IsRegular() {
			return value, nil
		}
	}
	return "", errors.New("未找到 Codex CLI；安装 Codex 或设置 TEAMCROSS_CODEX_BIN")
}

func Home() (string, error) {
	if value := os.Getenv("CODEX_HOME"); value != "" {
		return filepath.Abs(value)
	}
	home, err := os.UserHomeDir()
	return filepath.Join(home, ".codex"), err
}

func Version(ctx context.Context, binary string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, binary, "--version").Output()
	return strings.TrimSpace(string(data)), err
}

func runtimeConfigArgs(args []string, overrides ...string) []string {
	for _, value := range []string{
		`default_permissions="teamcross-native"`, `approval_policy="on-request"`,
		`permissions.teamcross-native={extends=":workspace",network={enabled=false}}`,
		`web_search="disabled"`, `allow_login_shell=false`, `features.code_mode_host=true`,
		`features.plugins=false`, `features.apps=false`, `features.hooks=false`,
		`features.plugin_hooks=false`, `features.memories=false`, `features.multi_agent=false`,
		`features.multi_agent_v2=false`, `features.goals=false`, `features.browser_use=false`, `features.computer_use=false`,
	} {
		args = append(args, "-c", value)
	}
	for _, value := range overrides {
		args = append(args, "-c", value)
	}
	return args
}

// MCPServerNames reads the effective home/project configuration under the same
// isolation settings used by the worker, without starting any server. Codex
// merges MCP tables across layers, so a worker must disable inherited entries
// individually before adding its scoped tools. Applying the runtime settings
// here also excludes plugin-provided MCP entries whose transports disappear
// when plugins are disabled for the worker.
func MCPServerNames(ctx context.Context, binary, home, cwd string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, runtimeConfigArgs([]string{"mcp", "list", "--json"})...)
	cmd.Dir, cmd.Env = cwd, environment(home)
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("无法读取 Codex MCP 配置，请检查客户端配置")
	}
	var entries []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("Codex 未返回有效的 MCP 配置列表")
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	return names, nil
}

func environment(home string) []string {
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key == "CODEX_HOME" || strings.HasPrefix(key, "CODEX_THREAD") || strings.HasPrefix(key, "CODEX_TURN") || strings.HasPrefix(key, "CODEX_SESSION") || strings.HasPrefix(key, "CODEX_APP_SERVER") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "CODEX_HOME="+home)
}

// WriteConfig only writes a Team Cross-owned directory, never the user's config.
func WriteConfig(home string) error {
	if err := os.MkdirAll(home, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(home, "config.toml"), []byte(`default_permissions = "teamcross-native"
approval_policy = "on-request"
web_search = "disabled"
allow_login_shell = false
[features]
code_mode_host = true
plugins = false
apps = false
hooks = false
plugin_hooks = false
memories = false
multi_agent = false
multi_agent_v2 = false
goals = false
browser_use = false
computer_use = false
[permissions.teamcross-native]
extends = ":workspace"
[permissions.teamcross-native.network]
enabled = false
`), 0600)
}

func Start(ctx context.Context, binary, home, cwd, logPath string, overrides ...string) (*Process, error) {
	// Reserve a loopback port. The readiness handshake detects a lost bind race.
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	address := l.Addr().String()
	_ = l.Close()
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	p := &Process{URL: "ws://" + address, done: make(chan struct{}), pending: make(map[string]chan Message)}
	// Native fork must use A's native history database. Apply configuration to
	// this process only instead of modifying A's personal config.toml.
	args := runtimeConfigArgs([]string{"app-server", "--listen", p.URL}, overrides...)
	p.cmd = exec.Command(binary, args...)
	p.cmd.Dir = cwd
	p.cmd.Env = environment(home)
	p.cmd.Stdout, p.cmd.Stderr = log, log
	if err = p.cmd.Start(); err != nil {
		_ = log.Close()
		return nil, err
	}
	go func() { _ = p.cmd.Wait(); _ = log.Close(); close(p.done) }()
	ready, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for {
		p.conn, _, err = websocket.Dial(ready, p.URL, nil)
		if err == nil {
			break
		}
		select {
		case <-ready.Done():
			p.Close()
			return nil, fmt.Errorf("Codex 启动超时，检查 %s", logPath)
		case <-p.done:
			return nil, fmt.Errorf("Codex 已退出，检查 %s", logPath)
		case <-time.After(80 * time.Millisecond):
		}
	}
	p.conn.SetReadLimit(32 << 20)
	go p.read()
	var initialized json.RawMessage
	err = p.Call(ready, "initialize", map[string]any{"clientInfo": map[string]string{"name": "teamcross_native", "version": buildinfo.Version}, "capabilities": map[string]bool{"experimentalApi": true}}, &initialized)
	p.Init = initialized
	if err == nil {
		err = p.conn.Write(ready, websocket.MessageText, []byte(`{"method":"initialized"}`))
	}
	if err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}

func (p *Process) Alive() bool {
	if p.disconnected.Load() {
		return false
	}
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *Process) Initialization() json.RawMessage { return p.Init }

func (p *Process) Endpoint() string { return p.URL }

func (p *Process) read() {
	defer func() {
		p.disconnected.Store(true)
		p.mu.Lock()
		h := p.handler
		p.mu.Unlock()
		if h != nil {
			h(Message{Method: "teamcross/runtimeDisconnected"})
		}
	}()
	defer func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		for id, ch := range p.pending {
			close(ch)
			delete(p.pending, id)
		}
	}()
	for {
		_, data, err := p.conn.Read(context.Background())
		if err != nil {
			return
		}
		var m Message
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		if m.Method != "" {
			p.mu.Lock()
			handler := p.handler
			p.mu.Unlock()
			if handler != nil {
				handler(m)
			} else if len(m.ID) > 0 {
				_ = p.Reply(context.Background(), m.ID, map[string]any{"decision": "decline"})
			}
			continue
		}
		if len(m.ID) == 0 {
			continue
		}

		p.mu.Lock()
		ch := p.pending[string(m.ID)]
		if ch != nil {
			ch <- m
			delete(p.pending, string(m.ID))
		}
		p.mu.Unlock()
	}
}

func (p *Process) Call(ctx context.Context, method string, params, out any) error {
	if !p.Alive() {
		return errors.New("Codex 运行时未连接")
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	id := p.seq.Add(1)
	key := fmt.Sprint(id)
	ch := make(chan Message, 1)
	p.mu.Lock()
	p.pending[key] = ch
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.pending, key); p.mu.Unlock() }()
	data, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	if err = p.conn.Write(ctx, websocket.MessageText, data); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("%s: %w (结果不明时请先刷新协作列表)", method, ctx.Err())
	case m, ok := <-ch:
		if !ok {
			return errors.New("Codex 控制连接已断开")
		}
		if len(m.Error) > 0 {
			return fmt.Errorf("%s: %s", method, m.Error)
		}
		if out != nil {
			return json.Unmarshal(m.Result, out)
		}
		return nil
	}
}

func (p *Process) Close() {
	p.closeOnce.Do(func() {
		if p.conn != nil {
			_ = p.conn.CloseNow()
		}
		if p.cmd != nil && p.cmd.Process != nil {
			_ = p.cmd.Process.Signal(os.Interrupt)
			select {
			case <-p.done:
			case <-time.After(2 * time.Second):
				_ = p.cmd.Process.Kill()
				<-p.done
			}
		}
	})
}

func Overrides(threadID, cwd string) map[string]any {
	return map[string]any{"threadId": threadID, "cwd": cwd, "permissions": Profile, "runtimeWorkspaceRoots": []string{cwd}, "approvalPolicy": "on-request", "approvalsReviewer": "user"}
}

func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func Command(binary, home, id, endpoint string) string {
	return "env CODEX_HOME=" + Quote(home) + " " + Quote(binary) + " resume " + Quote(id) + " --remote " + Quote(endpoint)
}

func (p *Process) SetHandler(h func(Message)) { p.mu.Lock(); p.handler = h; p.mu.Unlock() }
func (p *Process) Reply(ctx context.Context, id json.RawMessage, result any) error {
	data, err := json.Marshal(map[string]any{"id": id, "result": result})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return p.conn.Write(ctx, websocket.MessageText, data)
}
