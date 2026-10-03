package pluginpack

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportUpgradeHasStableRuntimeAndPreservesOtherFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "package with spaces")
	binary := filepath.Join(t.TempDir(), "cli")
	os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0700)
	p, err := Export(root, binary, "/data with spaces", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(root, "user-notes.txt")
	os.WriteFile(other, []byte("keep"), 0600)
	os.WriteFile(binary, []byte("#!/bin/sh\nexit 1\n"), 0700)
	upgraded, err := Export(root, binary, p.DataDir, "1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if p.Binary != upgraded.Binary {
		t.Fatal("runtime path drift")
	}
	saved, _ := os.ReadFile(other)
	if string(saved) != "keep" {
		t.Fatal("removed unrelated file")
	}
	var config struct {
		Servers map[string]struct {
			Command string
			Args    []string
		} `json:"mcpServers"`
	}
	b, _ := os.ReadFile(filepath.Join(root, "plugins/teamcross/.mcp.json"))
	if err := json.Unmarshal(b, &config); err != nil {
		t.Fatal(err)
	}
	server := config.Servers["teamcross-ui"]
	if server.Command != p.Binary || strings.Join(server.Args, "|") != "mcp|--ui|--data-dir|/data with spaces" {
		t.Fatal(server)
	}
	if len(config.Servers) != 1 {
		t.Fatal(config)
	}
	current, _ := os.ReadFile(upgraded.Binary)
	if !strings.Contains(string(current), "exit 1") {
		t.Fatal("binary not upgraded")
	}
	if _, err := owned(root); err != nil {
		t.Fatal(err)
	}
}

func TestExportRejectsForeignRootAndNestedSymlinks(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "cli")
	os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0700)
	foreign := t.TempDir()
	if _, err := Export(foreign, binary, "/data", "1.0.0"); err == nil {
		t.Fatal("overwrote foreign directory")
	}
	root := filepath.Join(t.TempDir(), "package")
	if _, err := Export(root, binary, "/data", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "sentinel")
	os.WriteFile(sentinel, []byte("keep"), 0600)
	// Remove only the owned manifest folder to insert the test redirect.
	if err := os.RemoveAll(filepath.Join(root, "plugins/teamcross/.codex-plugin")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "plugins/teamcross/.codex-plugin")); err != nil {
		t.Fatal(err)
	}
	if _, err := Export(root, binary, "/data", "1.1.0"); err == nil {
		t.Fatal("followed nested symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "plugin.json")); !os.IsNotExist(err) {
		t.Fatal("wrote outside package")
	}
	b, _ := os.ReadFile(sentinel)
	if string(b) != "keep" {
		t.Fatal("foreign file changed")
	}
}

func TestManagerRefusesForeignMarketplaceBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	cli := filepath.Join(dir, "codex")
	log := filepath.Join(dir, "calls")
	// A named marketplace conflict must stop before add/remove is issued.
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + log + "'\nprintf '%s\\n' '{\"marketplaces\":[{\"name\":\"teamcross-local\",\"root\":\"/foreign\",\"marketplaceSource\":{\"source\":\"/foreign\"}}]}'\n"
	os.WriteFile(cli, []byte(script), 0700)
	m := Manager{Codex: cli, Root: dir}
	if _, err := m.Remove(context.Background()); err == nil {
		t.Fatal("foreign source accepted")
	}
	if _, err := m.Status(context.Background()); err == nil {
		t.Fatal("foreign source accepted")
	}
	b, _ := os.ReadFile(log)
	if strings.Contains(string(b), "remove") || strings.Contains(string(b), "add") {
		t.Fatal(string(b))
	}
}

func TestManagerRefusesForeignPluginIdentity(t *testing.T) {
	m := Manager{Root: "/owned"}
	for _, raw := range []string{`{"pluginId":"other@teamcross-local","source":{"path":"/owned/plugins/teamcross"}}`, `{"pluginId":"teamcross@teamcross-local","source":{"path":"/foreign"}}`} {
		if err := m.verifyInstalled([]json.RawMessage{json.RawMessage(raw)}); err == nil {
			t.Fatal(raw)
		}
	}
	if err := m.verifyInstalled([]json.RawMessage{json.RawMessage(`{"pluginId":"teamcross@teamcross-local","source":{"path":"/owned/plugins/teamcross"}}`)}); err != nil {
		t.Fatal(err)
	}
}
