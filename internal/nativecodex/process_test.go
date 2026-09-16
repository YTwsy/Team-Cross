package nativecodex

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"teamcross/internal/runtimeconfig"
	"testing"
)

func TestMCPServerNamesUsesRuntimeIsolationConfig(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "codex")
	arguments := filepath.Join(dir, "arguments")
	script := `#!/bin/sh
printf '%s\n' "$@" > "$TEAMCROSS_TEST_ARGUMENTS"
plugins_disabled=false
while [ "$#" -gt 0 ]; do
  if [ "$1" = "features.plugins=false" ]; then
    plugins_disabled=true
  fi
  shift
done
if [ "$plugins_disabled" = true ]; then
  printf '[{"name":"personal"}]'
else
  printf '[{"name":"codex_app"},{"name":"cua_repl"},{"name":"personal"}]'
fi
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEAMCROSS_TEST_ARGUMENTS", arguments)
	names, err := MCPServerNames(context.Background(), binary, t.TempDir(), t.TempDir(), runtimeconfig.Restricted)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"personal"}) {
		t.Fatalf("plugin MCP entries leaked into isolated runtime: %v", names)
	}
	data, err := os.ReadFile(arguments)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(args) < 5 || !reflect.DeepEqual(args[:3], []string{"mcp", "list", "--json"}) {
		t.Fatalf("unexpected MCP list arguments: %q", args)
	}
	if !strings.Contains(string(data), "features.plugins=false\n") || !strings.Contains(string(data), "features.apps=false\n") {
		t.Fatalf("MCP discovery did not use runtime isolation: %q", data)
	}
	names, err = MCPServerNames(context.Background(), binary, t.TempDir(), t.TempDir(), runtimeconfig.Trusted)
	if err != nil || !reflect.DeepEqual(names, []string{"codex_app", "cua_repl", "personal"}) {
		t.Fatalf("trusted discovery lost plugin tools: %v %v", names, err)
	}
	data, _ = os.ReadFile(arguments)
	if string(data) != "mcp\nlist\n--json\n" {
		t.Fatalf("trusted discovery imposed configuration: %s", data)
	}
}

func TestTrustedRuntimeAndClientDoNotImposePermissionsOrFeatures(t *testing.T) {
	args := runtimeConfigArgs([]string{"app-server"}, runtimeconfig.Trusted, "mcp_servers.fixture={command=\"fixture\"}")
	if !reflect.DeepEqual(args, []string{"app-server", "-c", "mcp_servers.fixture={command=\"fixture\"}"}) {
		t.Fatalf("trusted runtime changed native settings: %v", args)
	}
	home := t.TempDir()
	if err := WriteConfigForMode(home, runtimeconfig.Trusted); err != nil {
		t.Fatal(err)
	}
	config, _ := os.ReadFile(filepath.Join(home, "config.toml"))
	if strings.Contains(string(config), "=") {
		t.Fatalf("trusted client imposed settings: %s", config)
	}
	params := SessionOverrides("shared", "/selected", runtimeconfig.Trusted)
	if params["threadId"] != "shared" || params["cwd"] != "/selected" || len(params) != 3 {
		t.Fatalf("trusted binding imposed settings or lost identity: %v", params)
	}
}

func TestClientLaunchLeavesModelChoiceToCodex(t *testing.T) {
	home := t.TempDir()
	if err := WriteConfig(home); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), "model") {
		t.Fatal("client config imposed model settings")
	}
	command := Command("/Applications/Codex Test/codex", home, "thread-id", "ws://127.0.0.1:1")
	if strings.Contains(command, " -m ") || strings.Contains(command, "--model") {
		t.Fatal("TUI launch imposed a model")
	}
	params := Overrides("thread-id", "/workspace")
	if _, ok := params["model"]; ok {
		t.Fatal("workspace binding imposed a model")
	}
}
