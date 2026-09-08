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
	"teamcross/internal/nativecodex"
	"time"
)

func (a *App) desktop() string {
	a.mu.Lock()
	v := a.settings.DesktopApp
	a.mu.Unlock()
	if v != "" {
		return v
	}
	for _, p := range []string{"/Applications/ChatGPT.app", "/Applications/Codex.app"} {
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
	endpoint, e := a.endpoint(id)
	if e != nil {
		return nil, e
	}
	home := filepath.Join(a.Config.DataDir, "clients", id, client, "codex-home")
	if e = nativecodex.WriteConfig(home); e != nil {
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
			return nil, fmt.Errorf("未找到 Codex Desktop，请在设置中选择已安装的应用")
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
	mcpCommand := "codex mcp add teamcross -- " + nativecodex.Quote(executable) + " mcp --data-dir " + nativecodex.Quote(a.Config.DataDir)
	return map[string]any{"name": "Team Cross", "version": "0.2.0-experimental", "host": a.Host, "binary": binary, "codexVersion": version, "codexError": problem, "desktopApp": a.desktop(), "dataDir": a.Config.DataDir, "model": nativecodex.Model, "mcpCommand": mcpCommand, "mcpConfigured": a.mcpConfigured(ctx, binary), "time": time.Now()}
}
func (a *App) SetupMCP(ctx context.Context) error {
	binary, e := a.binary()
	if e != nil {
		return e
	}
	executable, e := os.Executable()
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
	return v.Enabled && v.Transport.Command == executable && slices.Equal(v.Transport.Args, []string{"mcp", "--data-dir", a.Config.DataDir})
}
func (a *App) AssistPlan(ctx context.Context, id, client string, launch bool) (map[string]any, error) {
	if _, e := a.View(ctx, id); e != nil {
		return nil, e
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
		command = "cd " + nativecodex.Quote(a.Config.Repo) + " && " + nativecodex.Quote(binary) + " -m " + nativecodex.Model
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
			return nil, fmt.Errorf("请先完成一次性的 MCP 接入")
		}
		if out, e := cmd.CombinedOutput(); e != nil {
			return nil, fmt.Errorf("打开失败：%v %s", e, out)
		}
	}
	return map[string]any{"command": command, "launched": launch, "note": "在你自己的 Codex 中使用 Team Cross 工具选择这次协作。"}, nil
}
