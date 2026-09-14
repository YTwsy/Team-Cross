package nativecodex

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	names, err := MCPServerNames(context.Background(), binary, t.TempDir(), t.TempDir())
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
