package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"teamcross/internal/buildinfo"
	"teamcross/internal/cliinstall"
	"teamcross/internal/nativeclaude"
	"teamcross/internal/nativecodex"
	"teamcross/internal/problem"
	"teamcross/internal/runtimeconfig"
	"teamcross/internal/service"
	"time"
)

func (a *App) desktop() string {
	a.mu.Lock()
	v := a.settings.DesktopApp
	a.mu.Unlock()
	if v != "" {
		return v
	}
	h, _ := os.UserHomeDir()
	for _, p := range []string{"/Applications/ChatGPT.app", "/Applications/Codex.app", filepath.Join(h, "Applications", "Codex.app"), filepath.Join(h, "Applications", "ChatGPT.app")} {
		if _, e := os.Stat(p); e == nil {
			return p
		}
	}
	return ""
}
func (a *App) ClientPlan(ctx context.Context, id, client string, launch bool) (map[string]any, error) {
	if client != "tui" && client != "desktop" {
		return nil, fmt.Errorf("请选择 TUI 或 Desktop")
	}
	view, e := a.View(ctx, id)
	if e != nil {
		return nil, e
	}
	if view["online"] != true {
		return nil, fmt.Errorf("请先恢复协作运行时")
	}
	role, _ := view["role"].(string)
	if view["writer"] != role {
		return nil, fmt.Errorf("请先完成输入交接")
	}
	if view["provider"] == "claude" {
		return a.claudeClientPlan(ctx, id, client, launch, view)
	}
	endpoint, e := a.endpoint(id)
	if e != nil {
		return nil, e
	}
	home := filepath.Join(a.Config.DataDir, "clients", id, client, "codex-home")
	mode := runtimeconfig.Restricted
	if view["runtimeMode"] != nil {
		mode, e = runtimeconfig.Parse(runtimeconfig.Mode(fmt.Sprint(view["runtimeMode"])))
		if e != nil {
			return nil, e
		}
	}
	if e = nativecodex.WriteConfigForMode(home, mode); e != nil {
		return nil, e
	}
	binary, e := a.binary()
	if e != nil {
		return nil, e
	}
	sessionID, _ := view["sessionId"].(string)
	command := nativecodex.Command(binary, home, sessionID, endpoint)
	var cmd *exec.Cmd
	if client == "desktop" {
		app := a.desktop()
		if stat, e := os.Stat(app); e != nil || !stat.IsDir() {
			return nil, problem.New("client_missing", "未找到 Codex Desktop", "请在设置中选择已安装的应用，或选择 TUI")
		}
		data := filepath.Join(a.Config.DataDir, "clients", id, "desktop", "app-data")
		if e = os.MkdirAll(data, 0700); e != nil {
			return nil, e
		}
		args := []string{"-n", "--env", "CODEX_HOME=" + home, "--env", "CODEX_ELECTRON_USER_DATA_PATH=" + data, "--env", "CODEX_APP_SERVER_WS_URL=" + endpoint, "--env", "CODEX_APP_SERVER_FORCE_CLI=0", app, "--args", "--user-data-dir=" + data}
		command = "open"
		for _, arg := range args {
			command += " " + nativecodex.Quote(arg)
		}
		cmd = exec.Command("open", args...)
	} else {
		script := "tell application \"Terminal\"\nactivate\ndo script " + strconv.Quote(command) + "\nend tell"
		cmd = exec.Command("osascript", "-e", script)
	}
	result := map[string]any{"client": client, "command": command, "endpoint": endpoint, "sessionId": sessionID, "launched": false, "note": ""}
	if launch {
		if out, e := cmd.CombinedOutput(); e != nil {
			return nil, fmt.Errorf("无法打开客户端：%v %s；可使用复制命令入口", e, string(out))
		}
		result["launched"] = true
	}
	if client == "desktop" {
		result["note"] = "专用 Desktop 已绑定此协作；在会话列表中打开该协作即可。原有 Desktop 保持独立。"
	}
	return result, nil
}
func (a *App) Info(ctx context.Context) map[string]any {
	binary, e := a.binary()
	version := ""
	problem := ""
	if e != nil {
		problem = e.Error()
	} else {
		version, e = nativecodex.Version(ctx, binary)
		if e != nil {
			problem = e.Error()
		}
	}
	executable, _ := os.Executable()
	executable = service.StableExecutable(executable)
	mcpCommand := "codex mcp add teamcross -- " + nativecodex.Quote(executable) + " mcp --data-dir " + nativecodex.Quote(a.Config.DataDir)
	a.mu.Lock()
	observed, claudeObserved, probed := a.mcpObserved["codex"], a.mcpObserved["claude"], a.mcpProbed
	a.mu.Unlock()
	claudeBinary, claudeVersion, claudeError := a.claudeInfo(ctx)
	claude := a.personalClaudeMCP(claudeBinary, executable)
	claudeConfigured, claudeConfigError := claude.Inspect()
	claudeCommand := a.claudePersonalCommand(claudeBinary) + " mcp add --transport stdio --scope user teamcross -- " + nativecodex.Quote(executable) + " mcp --data-dir " + nativecodex.Quote(a.Config.DataDir)
	codexConfigured := a.mcpConfigured(ctx, binary)
	clients := map[string]MCPClientStatus{
		"codex":  {Configured: codexConfigured, Command: mcpCommand, ObservedAt: observed},
		"claude": {Configured: claudeConfigured, Command: claudeCommand, ObservedAt: claudeObserved},
	}
	if claudeConfigError != nil {
		status := clients["claude"]
		status.ConfigError = claudeConfigError.Error()
		clients["claude"] = status
	}
	return map[string]any{"mcpClients": clients, "claudeBinary": claudeBinary, "claudeVersion": claudeVersion, "claudeError": claudeError, "mcpObservedAt": observed, "mcpProbed": probed, "name": "Team Cross", "version": buildinfo.Version, "installedVersion": service.InstalledVersion(ctx, executable), "cli": cliinstall.Inspect(executable, cliinstall.DefaultDir, os.Getenv("PATH")), "commit": buildinfo.Commit, "host": a.Host, "binary": binary, "codexVersion": version, "codexError": problem, "desktopApp": a.desktop(), "dataDir": a.Config.DataDir, "mcpCommand": mcpCommand, "mcpConfigured": codexConfigured, "time": time.Now()}
}
func (a *App) SetupMCP(ctx context.Context, provider string) error {
	provider, e := providerName(provider)
	if e != nil {
		return e
	}
	a.mcpSetupMu.Lock()
	defer a.mcpSetupMu.Unlock()
	if provider == "claude" {
		binary, e := a.claudeBinary()
		if e != nil {
			return e
		}
		executable, e := os.Executable()
		if e != nil {
			return e
		}
		return a.personalClaudeMCP(binary, service.StableExecutable(executable)).Setup(ctx)
	}
	binary, e := a.binary()
	if e != nil {
		return e
	}
	executable, e := os.Executable()
	executable = service.StableExecutable(executable)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "mcp", "add", "teamcross", "--", executable, "mcp", "--data-dir", a.Config.DataDir)
	if out, e := cmd.CombinedOutput(); e != nil {
		return fmt.Errorf("MCP 配置未完成：%s (%w)", out, e)
	}
	return nil
}

func (a *App) mcpConfigured(ctx context.Context, binary string) bool {
	if binary == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, e := exec.CommandContext(ctx, binary, "mcp", "get", "teamcross", "--json").Output()
	if e != nil {
		return false
	}
	var v struct {
		Enabled   bool `json:"enabled"`
		Transport struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"transport"`
	}
	if json.Unmarshal(out, &v) != nil {
		return false
	}
	executable, _ := os.Executable()
	executable = service.StableExecutable(executable)
	return v.Enabled && v.Transport.Command == executable && slices.Equal(v.Transport.Args, []string{"mcp", "--data-dir", a.Config.DataDir})
}
func (a *App) AssistPlan(ctx context.Context, id, provider, client string, launch bool) (map[string]any, error) {
	provider, e := providerName(provider)
	if e != nil {
		return nil, e
	}
	if _, e := a.View(ctx, id); e != nil {
		return nil, e
	}
	if provider == "claude" {
		return a.claudeAssistPlan(ctx, client, launch)
	}
	binary, e := a.binary()
	if e != nil {
		return nil, e
	}
	var cmd *exec.Cmd
	command := ""
	switch client {
	case "tui":
		// The ordinary client uses its own context and local repository, with the
		// user's normal Codex home. It is never attached to the shared runtime.
		command = "cd " + nativecodex.Quote(a.Config.Repo) + " && " + nativecodex.Quote(binary)
		cmd = exec.Command("osascript", "-e", "tell application \"Terminal\"\nactivate\ndo script "+strconv.Quote(command)+"\nend tell")
	case "desktop":
		app := a.desktop()
		if app == "" {
			return nil, fmt.Errorf("未找到 Codex Desktop，请检查设置")
		}
		command = "open -a " + nativecodex.Quote(app)
		cmd = exec.Command("open", "-a", app)
	default:
		return nil, fmt.Errorf("请选择 TUI 或 Desktop")
	}
	if launch {
		if !a.mcpConfigured(ctx, binary) {
			return nil, problem.New("mcp_not_configured", "尚未接入本机 Codex", "请点击接入，再重新加载已有客户端的工具")
		}
		if out, e := cmd.CombinedOutput(); e != nil {
			return nil, fmt.Errorf("打开失败：%v %s", e, out)
		}
	}
	return map[string]any{"provider": provider, "command": command, "launched": launch, "note": "在你自己的 Codex 中使用 Team Cross 工具选择这次协作。"}, nil
}

type MCPClientStatus struct {
	Configured  bool      `json:"configured"`
	Command     string    `json:"command"`
	ConfigError string    `json:"configError,omitempty"`
	ObservedAt  time.Time `json:"observedAt"`
}

func (a *App) personalClaudeMCP(binary, executable string) nativeclaude.PersonalMCP {
	return nativeclaude.PersonalMCP{Binary: binary, Home: a.Config.ClaudeHome, Cwd: a.Config.Repo, Command: executable, DataDir: a.Config.DataDir}
}

func (a *App) claudePersonalCommand(binary string) string {
	if binary == "" {
		binary = "claude"
	}
	command := "env -u CLAUDECODE"
	home := a.Config.ClaudeHome
	if home == "" {
		home = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if home != "" {
		if absolute, err := filepath.Abs(home); err == nil {
			home = absolute
		}
		command += " CLAUDE_CONFIG_DIR=" + nativecodex.Quote(home)
	}
	return command + " " + nativecodex.Quote(binary)
}

func (a *App) claudeAssistPlan(ctx context.Context, client string, launch bool) (map[string]any, error) {
	if client != "tui" {
		return nil, fmt.Errorf("Claude Code 辅助模式目前支持 TUI")
	}
	binary, e := a.claudeBinary()
	if e != nil {
		return nil, e
	}
	if e = nativeclaude.CheckVersion(ctx, binary); e != nil {
		return nil, e
	}
	command := "cd " + nativecodex.Quote(a.Config.Repo) + " && " + a.claudePersonalCommand(binary)
	if launch {
		executable, e := os.Executable()
		if e != nil {
			return nil, e
		}
		configured, e := a.personalClaudeMCP(binary, service.StableExecutable(executable)).Inspect()
		if e != nil {
			return nil, e
		}
		if !configured {
			return nil, problem.New("mcp_not_configured", "尚未接入本机 Claude Code", "请先接入，再打开个人客户端加载工具")
		}
		cmd := exec.CommandContext(ctx, "osascript", "-e", "tell application \"Terminal\"\nactivate\ndo script "+strconv.Quote(command)+"\nend tell")
		if out, e := cmd.CombinedOutput(); e != nil {
			return nil, fmt.Errorf("打开失败：%v %s", e, out)
		}
	}
	return map[string]any{"provider": "claude", "command": command, "launched": launch, "note": "在你自己的 Claude Code 中使用 Team Cross 工具选择这次协作。个人对话使用本机模型设置；发送到协作的任务仍在发起者的主机执行。"}, nil
}
