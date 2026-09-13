package nativeclaude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// PersonalMCP configures the ordinary local client. Home is an explicit
// CLAUDE_CONFIG_DIR override, never a shared worker or attach-client home.
type PersonalMCP struct {
	Binary, Home, Cwd, Command, DataDir string
}

func (m PersonalMCP) ConfigPath() (string, error) {
	home := m.Home
	if home == "" {
		home = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Abs(filepath.Join(home, ".claude.json"))
}

func (m PersonalMCP) Env() []string {
	home := m.Home
	if home == "" {
		home = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if home != "" {
		if absolute, err := filepath.Abs(home); err == nil {
			home = absolute
		}
	}
	env := []string{}
	for _, v := range os.Environ() {
		key, _, _ := strings.Cut(v, "=")
		if key == "CLAUDECODE" || (home != "" && key == "CLAUDE_CONFIG_DIR") {
			continue
		}
		env = append(env, v)
	}
	if home != "" {
		env = append(env, "CLAUDE_CONFIG_DIR="+home)
	}
	return env
}

type mcpProject struct {
	Servers  map[string]json.RawMessage `json:"mcpServers"`
	Disabled []string                   `json:"disabledMcpServers"`
}
type mcpConfig struct {
	mcpProject
	Projects map[string]mcpProject `json:"projects"`
}

func (m PersonalMCP) read() (mcpConfig, error) {
	var config mcpConfig
	path, err := m.ConfigPath()
	if err != nil {
		return config, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return config, fmt.Errorf("无法读取 Claude 个人 MCP 配置：%w", err)
	}
	if json.Unmarshal(b, &config) != nil {
		return config, fmt.Errorf("Claude 个人配置不是有效 JSON，请先在 Claude Code 中修复")
	}
	return config, nil
}

// Inspect never starts configured servers. A saved entry is not proof that a
// running client loaded it; actual tools/call observations are tracked by Core.
func (m PersonalMCP) Inspect() (bool, error) {
	config, err := m.read()
	if err != nil {
		return false, err
	}
	cwd, err := filepath.Abs(m.Cwd)
	if err != nil {
		return false, err
	}
	project := config.Projects[cwd]
	if slices.Contains(config.Disabled, "teamcross") || slices.Contains(project.Disabled, "teamcross") {
		return false, fmt.Errorf("当前 Claude 项目已禁用 teamcross；请在 Claude Code 的 /mcp 中启用后重新检查")
	}
	if _, ok := project.Servers["teamcross"]; ok {
		return false, fmt.Errorf("当前 Claude 项目的 local 配置覆盖了个人 teamcross；请先在 Claude Code 中处理同名配置")
	}
	// Claude project MCP configuration is rooted at the project working directory.
	// Do not run `mcp get` here: it can start arbitrary configured servers to probe.
	if b, e := os.ReadFile(filepath.Join(cwd, ".mcp.json")); e == nil {
		var p mcpProject
		if json.Unmarshal(b, &p) != nil {
			return false, fmt.Errorf("当前目录的 .mcp.json 无法解析，请先修复项目配置")
		}
		if _, ok := p.Servers["teamcross"]; ok {
			return false, fmt.Errorf("当前目录的 .mcp.json 包含同名 teamcross，会优先于个人配置；请先处理该项目配置")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return false, fmt.Errorf("无法检查当前目录的 .mcp.json：%w", e)
	}
	return m.matches(config.Servers["teamcross"]), nil
}

func (m PersonalMCP) matches(raw json.RawMessage) bool {
	var server struct {
		Type    string            `json:"type"`
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
	}
	return json.Unmarshal(raw, &server) == nil && (server.Type == "stdio" || server.Type == "") && server.Command == m.Command && slices.Equal(server.Args, m.args()) && len(server.Env) == 0
}

func (m PersonalMCP) args() []string { return []string{"mcp", "--data-dir", m.DataDir} }

func (m PersonalMCP) run(ctx context.Context, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, m.Binary, args...)
	cmd.Dir, cmd.Env = m.Cwd, m.Env()
	// Native CLI output can contain other configuration values. Keep it out of
	// API errors; report the bounded operation and its exit status instead.
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Claude MCP 配置命令未完成：%w", err)
	}
	return nil
}

// Setup uses Claude's own scoped config writer so unrelated settings and server
// entries remain under its native locking/merge rules. Callers serialize setup.
func (m PersonalMCP) Setup(ctx context.Context) error {
	if err := CheckVersion(ctx, m.Binary); err != nil {
		return err
	}
	configured, err := m.Inspect()
	if err != nil || configured {
		return err
	}
	config, err := m.read()
	if err != nil {
		return err
	}
	previous, exists := config.Servers["teamcross"]
	if exists {
		if err = m.run(ctx, "mcp", "remove", "--scope", "user", "teamcross"); err != nil {
			return err
		}
	}
	args := append([]string{"mcp", "add", "--transport", "stdio", "--scope", "user", "teamcross", "--", m.Command}, m.args()...)
	if err = m.run(ctx, args...); err != nil {
		if exists {
			// A cancelled browser request must not prevent restoring the old entry.
			restore := m.run(context.WithoutCancel(ctx), "mcp", "add-json", "--scope", "user", "teamcross", string(previous))
			if restore != nil {
				return fmt.Errorf("%w；原 teamcross 条目恢复失败，请重新接入或在 Claude Code 中修复", err)
			}
		}
		return err
	}
	configured, err = m.Inspect()
	if err == nil && !configured {
		err = fmt.Errorf("Claude MCP 命令已退出，但未检测到目标配置，请重新检查")
	}
	return err
}
