package collab

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"teamcross/internal/nativecodex"
)

func settingsCLI(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	// Version and MCP status are enough to exercise the real Info route without
	// opening a native session or reading the user's personal configuration.
	script := "#!/bin/sh\nif test \"$1\" = --version; then printf 'codex-cli 0.160.0\\n'; else exit 1; fi\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestClientInfoPreservesSavedPathAndRecoversSameApp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("CODEX_HOME", filepath.Join(dir, "native-home"))
	t.Setenv("TEAMCROSS_CODEX_BIN", "")
	a, _, _ := fixture(t)
	app := filepath.Join(dir, "ChatGPT.app")
	old := filepath.Join(app, "Contents/Resources/codex")
	cli := settingsCLI(t, filepath.Join(app, "Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex"))
	saved := Settings{Binary: old, DesktopApp: app, ClaudeBinary: filepath.Join(dir, "missing-claude"), UILanguage: "en"}
	a.mu.Lock()
	a.settings = saved
	a.mu.Unlock()
	settingsFile := filepath.Join(a.Config.DataDir, "settings.json")
	if err := writeJSONFile(settingsFile, saved); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(settingsFile)
	info := a.Info(context.Background())
	if info["binary"] != cli || info["codexVersion"] != "codex-cli 0.160.0" || info["codexError"] != "" || info["settings"] != saved {
		t.Fatal(info)
	}
	installation := info["codexInstallation"].(nativecodex.Installation)
	if installation.RecoveredFrom != old || installation.Source != "app" {
		t.Fatal(installation)
	}
	if binary, err := a.binary(); err != nil || binary != cli {
		t.Fatal("execution did not share the settings resolver", binary, err)
	}
	w := httptest.NewRecorder()
	a.Handler(nil).ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1/api/info?refreshClients=1", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), old) || !strings.Contains(w.Body.String(), cli) {
		t.Fatal(w.Code, w.Body.String())
	}
	after, _ := os.ReadFile(settingsFile)
	if string(before) != string(after) || a.settings != saved {
		t.Fatal("discovery changed saved settings")
	}
	// An arbitrary invalid override must stay visible and actionable even with
	// a valid application available. Clearing it is a separate explicit write.
	missing := filepath.Join(dir, "my-custom-codex")
	a.mu.Lock()
	a.settings.Binary = missing
	a.mu.Unlock()
	info = a.Info(context.Background())
	if info["binary"] != "" || info["codexError"] != "Codex CLI 文件不存在" || info["codexRecovery"] == "" || info["settings"].(Settings).Binary != missing {
		t.Fatal(info)
	}
	w = httptest.NewRecorder()
	body, _ := json.Marshal(Settings{DesktopApp: app, ClaudeBinary: saved.ClaudeBinary})
	a.Handler(nil).ServeHTTP(w, httptest.NewRequest("POST", "http://127.0.0.1/api/settings", strings.NewReader(string(body))))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var persisted Settings
	data, err := os.ReadFile(settingsFile)
	if err != nil || json.Unmarshal(data, &persisted) != nil || persisted.Binary != "" || persisted.DesktopApp != app || persisted.UILanguage != "en" {
		t.Fatal(string(data), err)
	}
	info = a.Info(context.Background())
	if info["binary"] != cli || info["settings"].(Settings).Binary != "" {
		t.Fatal("effective path was pinned when automatic discovery was saved", info)
	}
}

func TestClientInfoReportsVersionFailureAsConfigurationError(t *testing.T) {
	a, _, _ := fixture(t)
	a.mu.Lock()
	a.settings.ClaudeBinary = filepath.Join(t.TempDir(), "missing-claude")
	a.mu.Unlock()
	info := a.Info(context.Background()) // /usr/bin/true is executable but not Codex.
	if info["codexError"] != "无法读取 Codex CLI 版本" || info["codexRecovery"] == "" {
		t.Fatal(info)
	}
}
