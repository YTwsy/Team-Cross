package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"teamcross/internal/nativecodex"
	"teamcross/internal/pluginpack"
	"teamcross/internal/service"
	"time"
)

// The installed helper owns plugin updates. A long-running Core may still run
// the previous build; it must not copy itself over the updated App's plugin.
func (a *App) pluginConnection(ctx context.Context, action string) (json.RawMessage, error) {
	executable := a.Config.Executable
	if executable == "" {
		executable, _ = os.Executable()
	}
	home, err := pluginpack.NativeHome()
	if err != nil {
		return nil, err
	}
	executable, err = pluginpack.ManagementBinary(home, service.StableExecutable(executable))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	args := []string{"plugin", action, "--data-dir", a.Config.DataDir}
	// A configured Desktop is independent of the collaboration provider CLI.
	a.mu.Lock()
	desktop := a.settings.DesktopApp
	a.mu.Unlock()
	if path := nativecodex.BundledBinary(desktop); desktop != "" && path != "" {
		args = append(args, "--codex-bin", path)
	}
	cmd := exec.CommandContext(ctx, executable, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("ChatGPT plugin: %s", message)
	}
	if !json.Valid(b) {
		return nil, fmt.Errorf("Invalid plugin connection response")
	}
	return json.RawMessage(b), nil
}

func (a *App) httpPluginConnection(w http.ResponseWriter, r *http.Request, path string) bool {
	if path != "plugin/connection" {
		return false
	}
	action := "connection-status"
	if r.Method == http.MethodPost {
		var input struct {
			Action string `json:"action"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		d.DisallowUnknownFields()
		if err := d.Decode(&input); err != nil || (input.Action != "connect" && input.Action != "disconnect") {
			respond(w, nil, fmt.Errorf("Invalid plugin action"))
			return true
		}
		var trailing any
		if d.Decode(&trailing) != io.EOF {
			respond(w, nil, fmt.Errorf("Invalid plugin action"))
			return true
		}
		action = input.Action
	} else if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return true
	}
	v, err := a.pluginConnection(r.Context(), action)
	respond(w, v, err)
	return true
}
