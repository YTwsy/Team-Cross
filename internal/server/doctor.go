package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"teamcross/internal/transport"
)

type DoctorResult struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Status   string `json:"status"`
	Detail   string `json:"detail"`
	Optional bool   `json:"optional,omitempty"`
}

func (app *App) Doctor(ctx context.Context, probeTailcat bool) []DoctorResult {
	checks := make([]DoctorResult, 0, 7)
	checks = append(checks, commandCheck(ctx, "git", "Git", false, "git", "--version"))
	checks = append(checks, commandCheck(ctx, "node", "Node.js Agent runtime", false, "node", "--version"))
	if err := app.store.DB().PingContext(ctx); err != nil {
		checks = append(checks, DoctorResult{Key: "storage", Label: "SQLite & object store", Status: "error", Detail: err.Error()})
	} else {
		checks = append(checks, DoctorResult{Key: "storage", Label: "SQLite & object store", Status: "ok", Detail: app.paths.Database})
	}
	checks = append(checks, app.bridgeDoctorCheck(ctx))
	if detail, err := probeCodexInitialize(ctx); err != nil {
		checks = append(checks, DoctorResult{Key: "codex", Label: "Codex app-server", Status: "warning", Detail: err.Error(), Optional: true})
	} else {
		checks = append(checks, DoctorResult{Key: "codex", Label: "Codex app-server", Status: "ok", Detail: detail, Optional: true})
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key == "" && os.Getenv("CLAUDE_CODE_USE_BEDROCK") == "" && os.Getenv("CLAUDE_CODE_USE_VERTEX") == "" {
		checks = append(checks, DoctorResult{Key: "claude", Label: "Claude credentials", Status: "warning", Detail: "ANTHROPIC_API_KEY or a supported cloud provider credential is not configured", Optional: true})
	} else {
		checks = append(checks, DoctorResult{Key: "claude", Label: "Claude credentials", Status: "ok", Detail: "Agent SDK credentials are configured", Optional: true})
	}
	tailnetCtx, cancelTailnet := context.WithTimeout(ctx, 2*time.Second)
	tailnet, err := (transport.LocalAPITailnetStatus{}).Status(tailnetCtx)
	cancelTailnet()
	if err != nil || !tailnet.Running {
		detail := "Tailscale LocalAPI unavailable; shares will fall back normally"
		if err == nil {
			detail = "Tailscale is not in Running state; shares will fall back normally"
		}
		checks = append(checks, DoctorResult{Key: "tailscale", Label: "Tailscale LocalAPI", Status: "warning", Detail: detail, Optional: true})
	} else {
		checks = append(checks, DoctorResult{Key: "tailscale", Label: "Tailscale LocalAPI", Status: "ok", Detail: fmt.Sprintf("Running with %d address(es)", len(tailnet.IPs)), Optional: true})
	}
	tailcatResult := DoctorResult{Key: "tailcat", Label: "Tailcat v0.4.0", Status: "ok", Detail: "Pinned Go adapter is compiled; DERP prewarms when a Share is created", Optional: true}
	if probeTailcat {
		probeCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
		host, probeErr := transport.StartTailcatHost(probeCtx, 443, func(connection net.Conn) { _ = connection.Close() })
		cancel()
		if probeErr != nil {
			tailcatResult.Status = "warning"
			tailcatResult.Detail = "Ephemeral DERP initialization failed: " + probeErr.Error()
		} else {
			tailcatResult.Detail = "Ephemeral Tailcat listener initialized successfully"
			_ = host.Close()
		}
	}
	checks = append(checks, tailcatResult)
	return checks
}

func (app *App) bridgeDoctorCheck(ctx context.Context) DoctorResult {
	result := DoctorResult{Key: "bridge", Label: "Team Cross Agent Bridge", Status: "error"}
	if app.bridge == nil {
		result.Detail = "Agent Bridge is unavailable; build packages/agent-bridge or set TEAMCROSS_SOURCE_ROOT"
		return result
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	readiness, err := app.bridge.Probe(probeCtx)
	if err != nil {
		result.Detail = "JSONL-RPC readiness probe failed: " + err.Error()
		return result
	}
	result.Status = "ok"
	result.Detail = fmt.Sprintf("%s %s, protocol %d", readiness.Name, readiness.Version, readiness.ProtocolVersion)
	return result
}

func commandCheck(ctx context.Context, key, label string, optional bool, command string, args ...string) DoctorResult {
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(probeCtx, command, args...).CombinedOutput()
	if err != nil {
		status := "error"
		if optional {
			status = "warning"
		}
		return DoctorResult{Key: key, Label: label, Status: status, Detail: err.Error(), Optional: optional}
	}
	return DoctorResult{Key: key, Label: label, Status: "ok", Detail: strings.TrimSpace(string(output)), Optional: optional}
}

func probeCodexInitialize(ctx context.Context) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(probeCtx, "codex", "app-server", "--stdio")
	stdin, err := command.StdinPipe()
	if err != nil {
		return "", err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := command.Start(); err != nil {
		return "", err
	}
	defer func() {
		_ = stdin.Close()
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		_ = command.Wait()
	}()
	initialize := map[string]any{"method": "initialize", "id": 0, "params": map[string]any{"clientInfo": map[string]string{"name": "teamcross", "title": "Team Cross Doctor", "version": "0.1.0"}}}
	encoded, _ := json.Marshal(initialize)
	if _, err := stdin.Write(append(encoded, '\n')); err != nil {
		return "", err
	}
	if _, err := stdin.Write([]byte("{\"method\":\"initialized\",\"params\":{}}\n")); err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		var response struct {
			ID     *int           `json:"id"`
			Result map[string]any `json:"result"`
			Error  any            `json:"error"`
		}
		if json.Unmarshal(scanner.Bytes(), &response) != nil || response.ID == nil || *response.ID != 0 {
			continue
		}
		if response.Error != nil {
			return "", fmt.Errorf("initialize failed: %v", response.Error)
		}
		family, _ := response.Result["platformFamily"].(string)
		if family == "" {
			family = "initialized"
		}
		return "app-server " + family, nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("app-server closed before initialize response")
}

func (app *App) handleInfo(response http.ResponseWriter, request *http.Request) {
	identity := accessFrom(request)
	if identity.Mode == "share" {
		writeJSON(response, http.StatusOK, appInfo{
			Version: app.config.Version, Mode: "join", Role: "observer",
			ParticipantID: identity.ParticipantID, SelectedTransport: identity.Transport,
			Doctor: []doctorCheck{{Key: "connection", Label: "Pinned Share connection", Status: "ok", Detail: "Connected through " + identity.Transport}},
		})
		return
	}
	checks := app.Doctor(request.Context(), false)
	viewChecks := make([]doctorCheck, 0, len(checks))
	for _, check := range checks {
		viewChecks = append(viewChecks, doctorCheck{Key: check.Key, Label: check.Label, Status: check.Status, Detail: check.Detail, Optional: check.Optional})
	}
	info := appInfo{Version: app.config.Version, Mode: identity.Mode, Role: identity.Role, ParticipantID: identity.ParticipantID, SelectedTransport: identity.Transport, Doctor: viewChecks}
	if identity.Mode == "host" {
		info.Repo = app.config.Repo
	}
	writeJSON(response, http.StatusOK, info)
}
