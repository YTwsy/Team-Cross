package nativeclaude

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const MinimumVersion = "2.1.268"
const VerifiedVersion = "2.1.268"

// ErrRejected means the input was conclusively rejected, not an uncertain
// transport outcome. Callers may report failure without suggesting a replay.
var ErrRejected = errors.New("Claude 未接收此输入")

// Home is A's effective personal Claude home, where the explicitly created
// fork is published. RuntimeDir holds ownership metadata; restricted mode also
// uses it as CLAUDE_CONFIG_DIR for the transcript, settings and auth snapshot.
type Config struct {
	Binary, Home, RuntimeDir, Cwd, Log, MCPConfig string
	Trusted                                       bool
	// DefaultHome keeps native ~/.claude.json lookup when CLAUDE_CONFIG_DIR
	// was unset. Explicitly setting it to ~/.claude changes that lookup.
	DefaultHome bool
}
type Job struct {
	ID         string `json:"id"`
	SessionID  string `json:"sessionId"`
	Cwd        string `json:"cwd"`
	PID        int    `json:"pid"`
	State      string `json:"state"`
	Status     string `json:"status"`
	WaitingFor string `json:"waitingFor"`
}

func (j Job) Busy() bool {
	// `state` is the job's semantic progress label, which can remain "working"
	// after a native interrupt. The live terminal status takes precedence.
	switch j.Status {
	case "idle":
		return j.WaitingFor != ""
	case "busy", "waiting":
		return true
	default:
		return j.State == "working" || j.WaitingFor != ""
	}
}

type marker struct {
	Trusted     bool   `json:"trusted,omitempty"`
	DefaultHome bool   `json:"defaultHome,omitempty"`
	Version     int    `json:"version"`
	SourceID    string `json:"sourceId"`
	SessionID   string `json:"sessionId"`
	JobID       string `json:"jobId,omitempty"`
	Cwd         string `json:"cwd"`
}
type Process struct {
	Config    Config
	Job       Job
	Socket    string
	meta      marker
	closed    atomic.Bool
	closeOnce sync.Once
}

func Home() (string, error) {
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		return filepath.Abs(v)
	}
	h, err := os.UserHomeDir()
	return filepath.Join(h, ".claude"), err
}
func Binary() (string, error) {
	if v := os.Getenv("TEAMCROSS_CLAUDE_BIN"); v != "" {
		return exec.LookPath(v)
	}
	if v, err := exec.LookPath("claude"); err == nil {
		return v, nil
	}
	h, _ := os.UserHomeDir()
	for _, p := range []string{filepath.Join(h, ".local/bin/claude"), "/opt/homebrew/bin/claude", "/usr/local/bin/claude"} {
		if s, err := os.Stat(p); err == nil && s.Mode().IsRegular() && s.Mode().Perm()&0111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("未找到 Claude Code CLI，请在设置中指定路径")
}
func Version(ctx context.Context, binary string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, binary, "--version").Output()
	return strings.TrimSpace(string(b)), err
}
func CheckVersion(ctx context.Context, binary string) error {
	v, err := Version(ctx, binary)
	if err != nil {
		return err
	}
	return ValidateVersion(v)
}
func ValidateVersion(v string) error {
	actual, ok := versionTuple(v)
	minimum, _ := versionTuple(MinimumVersion)
	if ok {
		for i := range actual {
			if actual[i] > minimum[i] {
				return nil
			}
			if actual[i] < minimum[i] {
				ok = false
				break
			}
		}
	}
	if !ok {
		return fmt.Errorf("Claude Code 需要 %s 或更高的正式版本，检测到 %s", MinimumVersion, v)
	}
	return nil
}

func versionTuple(v string) ([3]uint64, bool) {
	var result [3]uint64
	fields := strings.Fields(v)
	if len(fields) == 0 {
		return result, false
	}
	number, build, hasBuild := strings.Cut(fields[0], "+")
	if hasBuild {
		for _, part := range strings.Split(build, ".") {
			if part == "" || strings.Trim(part, "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ-") != "" {
				return result, false
			}
		}
	}
	parts := strings.Split(number, ".")
	if len(parts) != len(result) {
		return result, false
	}
	for i, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" || (len(part) > 1 && part[0] == '0') {
			return result, false
		}
		value, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return result, false
		}
		result[i] = value
	}
	return result, true
}
func Env(home string) []string {
	env := []string{}
	for _, v := range os.Environ() {
		key, _, _ := strings.Cut(v, "=")
		if key == "CLAUDECODE" || strings.HasPrefix(key, "CLAUDE_") {
			continue
		}
		env = append(env, v)
	}
	return append(env, "CLAUDE_CONFIG_DIR="+home, "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "CLAUDE_CODE_DISABLE_AUTO_MEMORY=1", "DISABLE_AUTOUPDATER=1", "DISABLE_TELEMETRY=1")
}
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(path+".tmp", b, 0600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}
func (c Config) configHome() string {
	if c.RuntimeDir != "" && !c.Trusted {
		return c.RuntimeDir
	}
	return c.Home
}
func (c Config) environment() []string {
	if !c.Trusted {
		return Env(c.configHome())
	}
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "CLAUDECODE" && key != "CLAUDE_CONFIG_DIR" {
			env = append(env, entry)
		}
	}
	if !c.DefaultHome {
		env = append(env, "CLAUDE_CONFIG_DIR="+c.Home)
	}
	return env
}
func (c Config) command(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Binary, args...)
	cmd.Env = c.environment()
	cmd.Dir = c.Cwd
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if c.Log != "" && stderr.Len() > 0 {
		if f, e := os.OpenFile(c.Log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); e == nil {
			_, _ = f.Write(stderr.Bytes())
			_ = f.Close()
		}
	}
	if err != nil {
		return out, fmt.Errorf("Claude 命令失败：%w；请检查本机运行时日志", err)
	}
	return out, nil
}
func (c Config) jobs(ctx context.Context) ([]Job, error) {
	b, err := c.command(ctx, "agents", "--json", "--all")
	if err != nil {
		return nil, err
	}
	var jobs []Job
	err = json.Unmarshal(b, &jobs)
	return jobs, err
}

const markerVersion = 2

func runtimeFile(c Config, name string) string {
	return filepath.Join(c.RuntimeDir, name)
}

func containsPath(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// prepare verifies the selected personal-history snapshot. Restricted mode
// copies only that source into an isolated home; trusted mode leaves native
// configuration and history in the personal home.
func prepare(c Config, source History) error {
	if c.Home == "" || c.RuntimeDir == "" {
		return fmt.Errorf("Claude 协作需要个人历史目录和独立运行目录")
	}
	home, err := filepath.Abs(c.Home)
	if err != nil {
		return err
	}
	path, err := filepath.Abs(source.Path)
	if err != nil {
		return err
	}
	runtimeDir, err := filepath.Abs(c.RuntimeDir)
	if err != nil {
		return err
	}
	if containsPath(home, runtimeDir) || containsPath(runtimeDir, home) {
		return fmt.Errorf("Claude 个人历史目录和协作运行目录不能重叠")
	}
	projects := filepath.Join(home, "projects")
	if filepath.Dir(filepath.Dir(path)) != projects || filepath.Base(path) != source.ID+".jsonl" {
		return fmt.Errorf("Claude 来源不属于个人历史目录")
	}
	b, err := os.ReadFile(source.Path)
	if err != nil {
		return err
	}
	if len(b) > maxHistory {
		return fmt.Errorf("Claude 来源历史过大")
	}
	sum := sha256.Sum256(b)
	if source.Fingerprint == "" || hex.EncodeToString(sum[:]) != source.Fingerprint {
		return fmt.Errorf("Claude 来源已变化，请重新查看起点")
	}
	if c.Trusted {
		// Native Claude uses the owner's settings, MCP, plugins, hooks, memory,
		// credentials and permissions in place. This directory holds ownership
		// only; no configuration or personal history is copied or rewritten.
		if entries, err := os.ReadDir(c.RuntimeDir); err == nil && len(entries) != 0 {
			return fmt.Errorf("Claude 运行目录已存在，请检查上次创建结果")
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return os.MkdirAll(c.RuntimeDir, 0700)
	}

	var user map[string]json.RawMessage
	if b, err := os.ReadFile(filepath.Join(c.Home, "settings.json")); err == nil {
		if err = json.Unmarshal(b, &user); err != nil {
			return fmt.Errorf("Claude 用户设置格式无效")
		}
	}
	settings := map[string]any{"disableAllHooks": true, "enabledPlugins": map[string]any{}, "permissions": map[string]any{"defaultMode": "manual", "ask": []string{"Bash", "Write", "Edit"}}}
	// Route/auth env stays on A. This file is never included in remote context.
	if user["env"] != nil {
		var env map[string]string
		if json.Unmarshal(user["env"], &env) != nil {
			return fmt.Errorf("Claude 路由环境设置格式无效")
		}
		route := map[string]string{}
		for key, value := range env {
			if strings.HasPrefix(key, "ANTHROPIC_") || key == "CLAUDE_CODE_AVAILABLE_MODELS" || key == "HTTP_PROXY" || key == "HTTPS_PROXY" || key == "ALL_PROXY" || key == "NO_PROXY" {
				route[key] = value
			}
		}
		settings["env"] = route
	}
	if entries, readErr := os.ReadDir(c.RuntimeDir); readErr == nil && len(entries) != 0 {
		return fmt.Errorf("Claude 运行目录已存在，请检查上次创建结果")
	} else if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if err = os.MkdirAll(c.RuntimeDir, 0700); err != nil {
		return err
	}
	snapshotDir := filepath.Join(runtimeFile(c, "projects"), filepath.Base(filepath.Dir(path)))
	if err = os.MkdirAll(snapshotDir, 0700); err != nil {
		return err
	}
	snapshot := filepath.Join(snapshotDir, filepath.Base(path))
	f, err := os.OpenFile(snapshot, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	// Authentication is copied into the private runtime when the native CLI
	// stores it as a file. The personal credential file is never a write target.
	if credentials, readErr := os.ReadFile(filepath.Join(home, ".credentials.json")); readErr == nil {
		if err = os.WriteFile(runtimeFile(c, ".credentials.json"), credentials, 0600); err != nil {
			return err
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("无法读取 Claude 认证快照：%w", readErr)
	}
	if err = Bootstrap(c.RuntimeDir, c.Cwd); err != nil {
		return err
	}
	return writeJSON(runtimeFile(c, "settings.json"), settings)
}

// Bootstrap applies only to a dedicated config and a directory the user chose
// in Team Cross's create flow; it does not accept trust in their personal CLI.
func Bootstrap(home, cwd string) error {
	if err := os.MkdirAll(home, 0700); err != nil {
		return err
	}
	path := filepath.Join(home, ".claude.json")
	x := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err = json.Unmarshal(b, &x); err != nil {
			return err
		}
	}
	x["hasCompletedOnboarding"] = true
	x["lastOnboardingVersion"] = VerifiedVersion
	x["autoUpdates"] = false
	if x["theme"] == nil {
		x["theme"] = "light"
	}
	x["hasSeenTasksHint"] = true
	x["projects"] = map[string]any{cwd: map[string]any{"hasTrustDialogAccepted": true, "hasCompletedProjectOnboarding": true, "projectOnboardingSeenCount": 1, "allowedTools": []string{}}}
	return writeJSON(path, x)
}

var regexpJob = regexp.MustCompile(`^[0-9a-f]{8}$`)
var backgroundID = regexp.MustCompile(`backgrounded · ([0-9a-f]{8})`)
var socketDir = regexp.MustCompile(`(/(?:private/)?tmp/cc-daemon-[^\s]+)`)

func SocketPath(ctx context.Context, c Config) (string, error) {
	// daemon status does not start a supervisor, including for B's empty config.
	b, err := c.command(ctx, "daemon", "status")
	m := socketDir.FindSubmatch(b)
	if len(m) != 2 {
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("Claude 未报告本机连接目录")
	}
	return filepath.Join(string(m[1]), "control.sock"), nil
}

func Fork(ctx context.Context, c Config, source History, title string) (*Process, error) {
	if err := CheckVersion(ctx, c.Binary); err != nil {
		return nil, err
	}
	if err := prepare(c, source); err != nil {
		return nil, err
	}
	p := &Process{Config: c, meta: marker{Version: markerVersion, SourceID: source.ID, Cwd: c.Cwd, Trusted: c.Trusted, DefaultHome: c.DefaultHome}}
	if err := writeJSON(runtimeFile(c, "teamcross-runtime.json"), p.meta); err != nil {
		return nil, err
	}
	mcpConfig := c.MCPConfig
	if mcpConfig == "" {
		mcpConfig = `{"mcpServers":{}}`
	}
	args := []string{"--resume", source.ID, "--fork-session", "--bg", "--name", title, "--settings", runtimeFile(c, "settings.json"), "--setting-sources", "", "--strict-mcp-config", "--mcp-config", mcpConfig, "--permission-mode", "manual", "--tools", "Bash,Read,Write,Edit,Glob,Grep,AskUserQuestion", "--no-chrome"}
	if c.Trusted {
		args = []string{"--resume", source.ID, "--fork-session", "--bg", "--name", title, "--mcp-config", mcpConfig}
	}
	if source.Model != "" {
		args = append(args, "--model", source.Model)
	}
	if source.ReasoningEffort != nil {
		args = append(args, "--effort", *source.ReasoningEffort)
	}
	if err := p.launch(ctx, args); err != nil {
		p.Close()
		return nil, err
	}
	if p.Job.SessionID == "" || p.Job.SessionID == source.ID {
		p.Close()
		return nil, fmt.Errorf("Claude 未创建新的 fork")
	}
	p.meta.SessionID = p.Job.SessionID
	p.meta.JobID = p.Job.ID
	if err := writeJSON(runtimeFile(c, "teamcross-runtime.json"), p.meta); err != nil {
		p.Close()
		return nil, err
	}
	if _, err := p.publishHistory(); err != nil && !errors.Is(err, os.ErrNotExist) {
		p.Close()
		return nil, err
	}
	return p, nil
}
func Restore(ctx context.Context, c Config, id string) (*Process, error) {
	if err := CheckVersion(ctx, c.Binary); err != nil {
		return nil, err
	}
	var m marker
	b, err := os.ReadFile(runtimeFile(c, "teamcross-runtime.json"))
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(b, &m) != nil || m.Version != markerVersion || m.SessionID != id || !regexpJob.MatchString(m.JobID) || m.Cwd != c.Cwd || m.Trusted != c.Trusted || m.DefaultHome != c.DefaultHome {
		return nil, fmt.Errorf("Claude 协作运行时记录不匹配")
	}
	if !c.Trusted {
		personal := c
		personal.RuntimeDir = ""
		personalJobs, err := personal.jobs(ctx)
		if err != nil {
			return nil, fmt.Errorf("无法确认个人 Claude 会话占用：%w", err)
		}
		for _, j := range personalJobs {
			if j.SessionID == id && j.Cwd == c.Cwd && j.PID > 0 && j.State != "stopped" && j.State != "failed" {
				return nil, fmt.Errorf("Claude 会话已在 Team Cross 之外运行，请先结束该运行时")
			}
		}
	}
	p := &Process{Config: c, meta: m}
	// A Core crash can leave its native worker alive. Reconnect that worker;
	// --resume on an already-running session explicitly creates another copy.
	jobs, err := c.jobs(ctx)
	if err != nil {
		return nil, err
	}
	var owned *Job
	for i := range jobs {
		j := &jobs[i]
		if j.SessionID != id || j.Cwd != c.Cwd || j.PID <= 0 || j.State == "stopped" || j.State == "failed" {
			continue
		}
		if j.ID != m.JobID {
			return nil, fmt.Errorf("Claude 会话已在 Team Cross 之外运行，请先结束该运行时")
		}
		owned = j
	}
	if owned != nil {
		p.Job = *owned
		p.Socket, err = SocketPath(ctx, c)
		if err != nil {
			return nil, err
		}
		conn, ack, err := p.connect(ctx, map[string]any{"op": "has"})
		if err != nil {
			return nil, err
		}
		_ = conn.Close()
		var state struct {
			Alive bool `json:"alive"`
		}
		if json.Unmarshal(ack, &state) != nil {
			return nil, fmt.Errorf("Claude 未确认现有 worker 状态")
		}
		if state.Alive {
			return p, nil
		}
	}
	// Adding launch/model/settings flags here makes Claude create a copy.
	if err = p.launch(ctx, []string{"--resume", id, "--bg"}); err != nil {
		p.stopJob()
		p.closed.Store(true)
		return nil, err
	}
	if p.Job.SessionID != id {
		p.stopJob()
		p.closed.Store(true)
		return nil, fmt.Errorf("Claude 恢复了不同会话，已停止该运行时")
	}
	p.meta.JobID = p.Job.ID
	if err = writeJSON(runtimeFile(c, "teamcross-runtime.json"), p.meta); err != nil {
		p.stopJob()
		p.closed.Store(true)
		return nil, err
	}
	return p, nil
}
func (p *Process) launch(ctx context.Context, args []string) error {
	b, err := p.Config.command(ctx, args...)
	if err != nil {
		return err
	}
	m := backgroundID.FindSubmatch(b)
	if len(m) != 2 {
		return fmt.Errorf("Claude 未确认后台会话 ID")
	}
	p.Job.ID = string(m[1])
	jobs, err := p.Config.jobs(ctx)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.ID == p.Job.ID {
			p.Job = j
			break
		}
	}
	if p.Job.SessionID == "" || filepath.Clean(p.Job.Cwd) != filepath.Clean(p.Config.Cwd) {
		return fmt.Errorf("Claude 未确认会话与执行目录")
	}
	p.Socket, err = SocketPath(ctx, p.Config)
	if err != nil {
		return err
	}
	return nil
}
func (p *Process) History() (History, error) {
	path, err := p.publishHistory()
	if err != nil {
		return History{}, err
	}
	return ReadFile(path)
}

// publishHistory exposes only the newly created collaboration transcript in
// A's personal history. A hard link keeps both native CLIs on the same file
// without exposing the rest of the personal projects tree to the collaboration
// worker. A file symlink is the cross-volume fallback.
func (p *Process) publishHistory() (string, error) {
	if p.Config.Home == "" || p.Config.RuntimeDir == "" || p.Job.SessionID == "" {
		return "", fmt.Errorf("Claude 协作历史位置不完整")
	}
	if p.Config.Trusted {
		return historyPath(p.Config.Home, p.Job.SessionID)
	}
	runtimePath, runtimeErr := historyPath(p.Config.RuntimeDir, p.Job.SessionID)
	personalPath, personalErr := historyPath(p.Config.Home, p.Job.SessionID)
	if personalErr == nil {
		if runtimeErr != nil {
			if errors.Is(runtimeErr, os.ErrNotExist) {
				return personalPath, nil
			}
			return "", runtimeErr
		}
		runtimeInfo, err := os.Stat(runtimePath)
		if err != nil {
			return "", err
		}
		personalInfo, err := os.Stat(personalPath)
		if err != nil {
			return "", err
		}
		if !os.SameFile(runtimeInfo, personalInfo) {
			return "", fmt.Errorf("个人 Claude 历史已有不同的同 ID 会话")
		}
		return personalPath, nil
	}
	if !errors.Is(personalErr, os.ErrNotExist) {
		return "", personalErr
	}
	if runtimeErr != nil {
		return "", runtimeErr
	}
	info, err := os.Lstat(runtimePath)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("Claude 协作历史不是普通文件")
	}
	rel, err := filepath.Rel(runtimeFile(p.Config, "projects"), runtimePath)
	if err != nil || filepath.Dir(rel) == "." || filepath.Dir(filepath.Dir(rel)) != "." {
		return "", fmt.Errorf("Claude 协作历史位置无效")
	}
	destination := filepath.Join(p.Config.Home, "projects", rel)
	if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return "", err
	}
	if err = os.Link(runtimePath, destination); err != nil {
		if errors.Is(err, os.ErrExist) {
			return p.publishHistory()
		}
		if !errors.Is(err, syscall.EXDEV) {
			return "", fmt.Errorf("无法发布 Claude 个人历史：%w", err)
		}
		if err = os.Symlink(runtimePath, destination); err != nil {
			if errors.Is(err, os.ErrExist) {
				return p.publishHistory()
			}
			return "", fmt.Errorf("无法跨磁盘发布 Claude 个人历史：%w", err)
		}
	}
	return destination, nil
}

func SavedHistory(home, runtimeDir, id, cwd string) (History, error) {
	var m marker
	b, err := os.ReadFile(filepath.Join(runtimeDir, "teamcross-runtime.json"))
	if err != nil {
		return History{}, err
	}
	if json.Unmarshal(b, &m) != nil || m.Version != markerVersion || m.SessionID != id || !regexpJob.MatchString(m.JobID) || m.Cwd != cwd {
		return History{}, fmt.Errorf("Claude 会话记录不匹配")
	}
	p := Process{Config: Config{Home: home, RuntimeDir: runtimeDir, Cwd: cwd, Trusted: m.Trusted, DefaultHome: m.DefaultHome}, Job: Job{SessionID: id}, meta: m}
	return p.History()
}
func (p *Process) Status(ctx context.Context) (Job, error) {
	if p.closed.Load() {
		return Job{}, fmt.Errorf("Claude 运行时已关闭")
	}
	jobs, err := p.Config.jobs(ctx)
	if err != nil {
		return Job{}, err
	}
	for _, j := range jobs {
		if j.ID == p.Job.ID && j.SessionID == p.Job.SessionID && j.Cwd == p.Config.Cwd && j.PID > 0 {
			return j, nil
		}
	}
	return Job{}, fmt.Errorf("Claude 后台 worker 不存在")
}
func (p *Process) Alive() bool { return !p.closed.Load() }
func (p *Process) stopJob() {
	if p.Job.ID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if p.Config.Trusted {
		// A trusted worker shares the personal daemon's job namespace. Verify
		// the current identity before stopping; a stale/reused id is not ours.
		if p.Job.SessionID == "" || p.Job.SessionID == p.meta.SourceID || p.Job.Cwd != p.Config.Cwd {
			return
		}
		jobs, err := p.Config.jobs(ctx)
		if err != nil {
			return
		}
		owned := false
		for _, job := range jobs {
			if job.ID == p.Job.ID && job.SessionID == p.Job.SessionID && job.Cwd == p.Config.Cwd {
				owned = true
			}
		}
		if !owned {
			return
		}
	}
	_, stopErr := p.Config.command(ctx, "stop", p.Job.ID)
	if stopErr != nil || !p.Config.Trusted || p.Socket == "" {
		return
	}
	// Native stop acknowledges before the worker has necessarily exited. A
	// resume during that interval creates a copy, so keep release pending until
	// the personal daemon confirms this exact job is gone. Close has already
	// disabled public calls; this private probe only reads worker liveness.
	probe := Process{Job: p.Job, Socket: p.Socket}
	for ctx.Err() == nil {
		conn, ack, err := probe.connect(ctx, map[string]any{"op": "has"})
		if err != nil {
			return
		}
		_ = conn.Close()
		var state struct {
			Alive   bool `json:"alive"`
			Present bool `json:"present"`
		}
		if json.Unmarshal(ack, &state) != nil || (!state.Alive && !state.Present) {
			return
		}
		select {
		case <-ctx.Done():
		case <-time.After(100 * time.Millisecond):
		}
	}
}
func (p *Process) Close() {
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		// The marker identifies the collaboration-owned job. Only restricted
		// mode owns the supervisor; trusted mode shares the personal daemon.
		var m marker
		b, err := os.ReadFile(runtimeFile(p.Config, "teamcross-runtime.json"))
		if err != nil || json.Unmarshal(b, &m) != nil || m.Version != markerVersion || m.Trusted != p.Config.Trusted || m.DefaultHome != p.Config.DefaultHome || m.SourceID != p.meta.SourceID || m.Cwd != p.Config.Cwd || (m.SessionID != "" && m.SessionID != p.Job.SessionID) || (m.JobID != "" && m.JobID != p.Job.ID) {
			return
		}
		p.stopJob()
		_, _ = p.publishHistory()
		if p.Config.Trusted {
			// The owner may have other jobs on this personal daemon.
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = p.Config.command(ctx, "daemon", "stop", "--any")
	})
}

type TerminalRequest struct {
	Op       string          `json:"op"`
	Cols     int             `json:"cols"`
	Rows     int             `json:"rows"`
	AttachID string          `json:"attachId"`
	Caps     json.RawMessage `json:"caps,omitempty"`
}

func (r TerminalRequest) Validate() error {
	if r.Op != "attach" && r.Op != "resize" {
		return fmt.Errorf("不支持的 Claude 终端操作")
	}
	if r.Cols < 1 || r.Cols > 1000 || r.Rows < 1 || r.Rows > 1000 || len(r.AttachID) > 128 || r.AttachID == "" {
		return fmt.Errorf("无效的终端尺寸或连接 ID")
	}
	if r.Op == "attach" && (len(r.Caps) > 8192 || !json.Valid(r.Caps)) {
		return fmt.Errorf("无效的终端能力")
	}
	return nil
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) { return c.reader.Read(b) }
func readAck(c net.Conn) (net.Conn, json.RawMessage, error) {
	r := bufio.NewReaderSize(c, 32<<10)
	line, err := r.ReadSlice('\n')
	if err != nil {
		return nil, nil, err
	}
	var ack struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if json.Unmarshal(line, &ack) != nil {
		return nil, nil, fmt.Errorf("Claude 返回无效连接响应")
	}
	if !ack.OK {
		return nil, nil, fmt.Errorf("%w：%s", ErrRejected, ack.Error)
	}
	return &bufferedConn{Conn: c, reader: r}, append(json.RawMessage(nil), bytes.TrimSpace(line)...), nil
}
func (p *Process) connect(ctx context.Context, request map[string]any) (net.Conn, json.RawMessage, error) {
	if !p.Alive() {
		return nil, nil, fmt.Errorf("Claude 运行时已关闭")
	}
	if request["op"] == "reply" || request["op"] == "resize" {
		key, err := p.controlKey()
		if err != nil {
			return nil, nil, err
		}
		request["auth"] = key
	}
	c, err := (&net.Dialer{}).DialContext(ctx, "unix", p.Socket)
	if err != nil {
		return nil, nil, err
	}
	_ = c.SetDeadline(time.Now().Add(12 * time.Second))
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	request["proto"] = 1
	request["short"] = p.Job.ID
	// Attach uses A's Unix peer; control writes also use A's own daemon key.
	// B's auth never crosses this leg and A's key is never returned to clients.
	b, err := json.Marshal(request)
	if err == nil {
		_, err = c.Write(append(b, '\n'))
	}
	var buffered net.Conn
	var ack json.RawMessage
	if err == nil {
		buffered, ack, err = readAck(c)
	}
	if err != nil {
		_ = c.Close()
		return nil, nil, err
	}
	_ = c.SetDeadline(time.Time{})
	return buffered, ack, nil
}

func (p *Process) controlKey() (string, error) {
	f, err := os.Open(filepath.Join(p.Config.configHome(), "daemon", "control.key"))
	if err != nil {
		return "", fmt.Errorf("%w：无法读取 Claude 本机控制凭据", ErrRejected)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || st.Size() > 4096 {
		return "", fmt.Errorf("%w：Claude 本机控制凭据格式或权限异常", ErrRejected)
	}
	b, err := io.ReadAll(io.LimitReader(f, 4097))
	key := strings.TrimSpace(string(b))
	if err != nil || !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(key) {
		return "", fmt.Errorf("%w：Claude 本机控制凭据格式异常", ErrRejected)
	}
	return key, nil
}
func (p *Process) Attach(ctx context.Context, r TerminalRequest) (net.Conn, json.RawMessage, error) {
	if err := r.Validate(); err != nil {
		return nil, nil, err
	}
	if r.Op != "attach" {
		return nil, nil, fmt.Errorf("请使用 attach")
	}
	return p.connect(ctx, map[string]any{"op": "attach", "cols": r.Cols, "rows": r.Rows, "attachId": r.AttachID, "caps": renderCaps(r.Caps)})
}

// Only terminal rendering capabilities cross to A. Editor/browser commands,
// local multiplexers, socket paths and unknown future capabilities never do.
func renderCaps(raw json.RawMessage) map[string]any {
	var input map[string]json.RawMessage
	_ = json.Unmarshal(raw, &input)
	caps := map[string]any{"imark": true, "terminal": "xterm-256color", "mux": nil, "ssh": false, "wheelFlood": false, "hyperlinks": false, "progressReporting": false, "wtSession": false, "isVscodeTerm": false, "browser": nil, "colorLevel": 0, "syncOutput": false, "editor": nil}
	for _, key := range []string{"wheelFlood", "hyperlinks", "progressReporting", "wtSession", "isVscodeTerm", "syncOutput"} {
		var value bool
		if json.Unmarshal(input[key], &value) == nil {
			caps[key] = value
		}
	}
	var color int
	if json.Unmarshal(input["colorLevel"], &color) == nil && color >= 0 && color <= 3 {
		caps["colorLevel"] = color
	}
	var theme string
	if json.Unmarshal(input["systemTheme"], &theme) == nil && (theme == "light" || theme == "dark") {
		caps["systemTheme"] = theme
	}
	return caps
}
func (p *Process) Resize(ctx context.Context, r TerminalRequest) error {
	if err := r.Validate(); err != nil {
		return err
	}
	c, _, err := p.connect(ctx, map[string]any{"op": "resize", "cols": r.Cols, "rows": r.Rows, "attachId": r.AttachID})
	if c != nil {
		_ = c.Close()
	}
	return err
}
func (p *Process) Send(ctx context.Context, text string) error {
	if strings.TrimSpace(text) == "" || len(text) > 256<<10 {
		return fmt.Errorf("%w：请输入不超过 256 KiB 的文本", ErrRejected)
	}
	j, err := p.Status(ctx)
	if err != nil {
		return fmt.Errorf("%w：%v", ErrRejected, err)
	}
	if j.Busy() {
		return fmt.Errorf("%w：正在运行或等待交互，请在原生 TUI 中补充或回应", ErrRejected)
	}
	c, _, err := p.connect(ctx, map[string]any{"op": "reply", "text": text})
	if c != nil {
		_ = c.Close()
	}
	return err // One attempt; an uncertain reply is never replayed.
}
