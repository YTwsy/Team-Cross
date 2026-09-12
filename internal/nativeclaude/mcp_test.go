package nativeclaude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestMinimumClaudeVersion(t *testing.T) {
	for _, version := range []string{"2.1.268", "2.1.268 (Claude Code)", "2.1.269", "2.2.0", "3.0.0", "2.10.1", "2.1.268+build"} {
		if err := ValidateVersion(version); err != nil {
			t.Errorf("%s: %v", version, err)
		}
	}
	for _, version := range []string{"", "unknown", "2.1.267", "2.1.99", "1.99.999", "2.1", "2.1.2680wrong", "2.2.0-beta.1", "2.1.268+", "2.1.268+build..id", "2.01.268", "2.-2.300", "999999999999999999999999.1.2"} {
		if ValidateVersion(version) == nil {
			t.Errorf("accepted %q", version)
		}
	}
}

func TestPersonalMCPConfigPathAndEnvironment(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home, _ := os.UserHomeDir()
	path, err := (PersonalMCP{}).ConfigPath()
	if err != nil || path != filepath.Join(home, ".claude.json") {
		t.Fatal(path, err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("CLAUDE_CODE_EFFORT_LEVEL", "high")
	t.Setenv("CLAUDECODE", "nested")
	m := PersonalMCP{Home: t.TempDir()}
	path, err = m.ConfigPath()
	if err != nil || path != filepath.Join(m.Home, ".claude.json") {
		t.Fatal(path, err)
	}
	if !slices.Contains(m.Env(), "CLAUDE_CONFIG_DIR="+m.Home) || !slices.Contains(m.Env(), "CLAUDE_CODE_EFFORT_LEVEL=high") || slices.Contains(m.Env(), "CLAUDECODE=nested") {
		t.Fatal("personal environment changed unexpectedly")
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "relative-config")
	m = PersonalMCP{Cwd: t.TempDir()}
	absolute, _ := filepath.Abs("relative-config")
	if !slices.Contains(m.Env(), "CLAUDE_CONFIG_DIR="+absolute) {
		t.Fatal("child cwd changed the selected personal config directory")
	}
}

func TestPersonalMCPSetupPreservesConfigAndRollsBack(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			m := personalMCPFixture(t)
			original := map[string]any{"theme": "dark", "fixtureFailAdd": fail, "mcpServers": map[string]any{"unrelated": map[string]any{"command": "/personal/server"}, "teamcross": map[string]any{"command": "/old/teamcross", "args": []string{"mcp"}, "env": map[string]string{"CUSTOM": "keep-on-rollback"}}}}
			path, _ := m.ConfigPath()
			writeJSON(path, original)
			err := m.Setup(context.Background())
			if (err != nil) != fail {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(path)
			var actual map[string]any
			json.Unmarshal(b, &actual)
			if actual["theme"] != "dark" || actual["mcpServers"].(map[string]any)["unrelated"].(map[string]any)["command"] != "/personal/server" {
				t.Fatal("lost unrelated config")
			}
			configured, e := m.Inspect()
			if e != nil || configured == fail {
				t.Fatal(configured, e)
			}
			if fail {
				if !strings.Contains(string(b), "keep-on-rollback") || !strings.Contains(string(b), "/old/teamcross") {
					t.Fatal("rollback lost original entry")
				}
			} else {
				log, _ := os.ReadFile(filepath.Join(m.Home, "calls.jsonl"))
				if err := m.Setup(context.Background()); err != nil {
					t.Fatal(err)
				}
				after, _ := os.ReadFile(filepath.Join(m.Home, "calls.jsonl"))
				if string(log) != string(after) {
					t.Fatal("repeated setup mutated config")
				}
			}
		})
	}
}

func TestPersonalMCPRejectsDisabledOverriddenOrBrokenConfig(t *testing.T) {
	for _, scenario := range []string{"disabled", "local", "project", "broken", "wrong-args", "wrong-env"} {
		t.Run(scenario, func(t *testing.T) {
			m := personalMCPFixture(t)
			if err := m.Setup(context.Background()); err != nil {
				t.Fatal(err)
			}
			path, _ := m.ConfigPath()
			b, _ := os.ReadFile(path)
			var config map[string]any
			json.Unmarshal(b, &config)
			switch scenario {
			case "disabled":
				config["projects"] = map[string]any{m.Cwd: map[string]any{"disabledMcpServers": []string{"teamcross"}}}
			case "local":
				config["projects"] = map[string]any{m.Cwd: map[string]any{"mcpServers": map[string]any{"teamcross": map[string]any{"command": "/other"}}}}
			case "project":
				os.WriteFile(filepath.Join(m.Cwd, ".mcp.json"), []byte(`{"mcpServers":{"teamcross":{"command":"/other"}}}`), 0600)
			case "wrong-args":
				config["mcpServers"].(map[string]any)["teamcross"].(map[string]any)["args"] = []string{"mcp", "--data-dir", "/another-core"}
			case "wrong-env":
				config["mcpServers"].(map[string]any)["teamcross"].(map[string]any)["env"] = map[string]string{"BASH_ENV": "/unexpected"}
			}
			writeJSON(path, config)
			if scenario == "broken" {
				os.WriteFile(path, []byte("{"), 0600)
			}
			before, _ := os.ReadFile(path)
			configured, err := m.Inspect()
			if configured {
				t.Fatal("reported overridden/broken config as ready")
			}
			if scenario != "wrong-args" && scenario != "wrong-env" {
				if err == nil || m.Setup(context.Background()) == nil {
					t.Fatal("overrode explicit user choice")
				}
			}
			after, _ := os.ReadFile(path)
			if string(after) != string(before) {
				t.Fatal("inspection/blocked setup changed config")
			}
		})
	}
}

func personalMCPFixture(t *testing.T) PersonalMCP {
	t.Helper()
	m := PersonalMCP{Home: t.TempDir(), Cwd: t.TempDir(), Command: "/fixture path/teamcross", DataDir: t.TempDir()}
	executable, _ := os.Executable()
	m.Binary = filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\nTEAMCROSS_MCP_HELPER=1 exec '" + strings.ReplaceAll(executable, "'", "'\\''") + "' -test.run=TestPersonalMCPCLIHelper -- \"$@\"\n"
	if err := os.WriteFile(m.Binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestPersonalMCPCLIHelper(t *testing.T) {
	if os.Getenv("TEAMCROSS_MCP_HELPER") != "1" {
		return
	}
	args := os.Args[slices.Index(os.Args, "--")+1:]
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println("2.2.0 (Claude Code)")
		os.Exit(0)
	}
	if len(args) < 5 || args[0] != "mcp" || !slices.Contains(args, "user") || !slices.Contains(args, "teamcross") {
		os.Exit(2)
	}
	path := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), ".claude.json")
	config := map[string]any{}
	if b, e := os.ReadFile(path); e == nil {
		if json.Unmarshal(b, &config) != nil {
			os.Exit(3)
		}
	}
	servers, ok := config["mcpServers"].(map[string]any)
	if !ok {
		servers = map[string]any{}
		config["mcpServers"] = servers
	}
	log, _ := os.OpenFile(filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "calls.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	json.NewEncoder(log).Encode(args)
	log.Close()
	switch args[1] {
	case "remove":
		delete(servers, "teamcross")
	case "add":
		if config["fixtureFailAdd"] == true {
			os.Exit(4)
		}
		i := slices.Index(args, "--")
		if i < 0 {
			os.Exit(5)
		}
		servers["teamcross"] = map[string]any{"type": "stdio", "command": args[i+1], "args": args[i+2:], "env": map[string]string{}}
	case "add-json":
		var server any
		if json.Unmarshal([]byte(args[len(args)-1]), &server) != nil {
			os.Exit(6)
		}
		servers["teamcross"] = server
	default:
		os.Exit(7)
	}
	if writeJSON(path, config) != nil {
		os.Exit(8)
	}
	os.Exit(0)
}
