package nativecodex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
