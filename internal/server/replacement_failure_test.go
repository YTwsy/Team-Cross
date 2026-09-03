package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"teamcross/internal/bridgeclient"
	"teamcross/internal/domain"
)

func TestReplacementDurabilityFailureDisablesTargetAndPreservesOwnerCloseRetry(t *testing.T) {
	for _, operation := range []string{"switch", "continue"} {
		for _, phase := range []string{"event", "archive-status", "event-target-close-fails"} {
			for _, outgoing := range []bool{true, false} {
				if !outgoing && phase == "archive-status" {
					continue
				}
				name := operation + "/" + phase + "/without-outgoing"
				if outgoing {
					name = operation + "/" + phase + "/with-outgoing"
				}
				t.Run(name, func(t *testing.T) {
					ctx := context.Background()
					repo := serverTestRepository(t)
					app := newIntegrationApp(t, repo)
					detail := captureForContinuation(t, app)
					originalStatus := string(serverTestGit(t, repo, "status", "--porcelain=v2", "-z"))
					var old *managedRun
					if outgoing {
						old = &managedRun{ID: "outgoing", ThreadID: detail.ID, Provider: "mock", SessionID: "old-native", Status: "idle"}
						if _, err := app.store.CreateAgentRun(ctx, domain.AgentRun{ID: old.ID, ThreadID: old.ThreadID, Provider: old.Provider, SessionID: old.SessionID, Status: old.Status}); err != nil {
							t.Fatal(err)
						}
						app.runs[detail.ID], app.runsByID[old.ID] = old, old
					}
					log := filepath.Join(t.TempDir(), "calls")
					releaseOld := filepath.Join(t.TempDir(), "old-close-allowed")
					script := writeBridgeFixture(t, t.TempDir(), `#!/bin/sh
log=$1
release=$2
target_fail=$3
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$log"
  id=$(printf '%s' "$line" | sed -E 's/.*"id":"([^"]+)".*/\1/')
  run=$(printf '%s' "$line" | sed -E 's/.*"runId":"([^"]+)".*/\1/')
  case "$line" in
    *'"method":"runs.create"'*) printf '{"jsonrpc":"2.0","id":"%s","result":{"sessionId":"new-native-%s","status":"idle"}}\n' "$id" "$id";;
    *'"method":"runs.close"'*)
      if { [ "$run" = outgoing ] && [ ! -f "$release" ]; } || { [ "$run" != outgoing ] && [ "$target_fail" = yes ]; }; then
        printf '{"jsonrpc":"2.0","id":"%s","error":{"code":-32000,"message":"close unconfirmed"}}\n' "$id"
      else
        printf '{"jsonrpc":"2.0","id":"%s","result":{}}\n' "$id"
      fi;;
    *'"method":"runs.send"'*)
      if [ "$run" = outgoing ]; then
        printf '{"jsonrpc":"2.0","id":"%s","result":{}}\n' "$id"
      else
        printf '{"jsonrpc":"2.0","id":"%s","result":{"turnId":"new-turn"}}\n' "$id"
      fi;;
    *) printf '{"jsonrpc":"2.0","id":"%s","result":{}}\n' "$id";;
  esac
done
`)
					targetFails := "no"
					if phase == "event-target-close-fails" {
						targetFails = "yes"
					}
					bridge, err := bridgeclient.Start(ctx, bridgeclient.StartOptions{Command: script, Args: []string{log, releaseOld, targetFails}})
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = bridge.Close() })
					app.bridge = bridge
					eventType := "agent.switched"
					path := "/api/v1/threads/" + detail.ID + "/agent/switch"
					body := map[string]any{"provider": "claude"}
					if operation == "continue" {
						eventType = "agent.continued"
						path = "/api/v1/threads/" + detail.ID + "/continue"
						body = map[string]any{"provider": "claude", "roundId": detail.Rounds[0].ID, "prompt": "Continue safely", "expectedRevision": detail.Revision}
					}
					trigger := "CREATE TRIGGER reject_activation BEFORE INSERT ON events WHEN NEW.event_type='" + eventType + "' BEGIN SELECT RAISE(ABORT,'injected activation failure'); END"
					if phase == "archive-status" {
						trigger = "CREATE TRIGGER reject_activation BEFORE UPDATE ON agent_runs WHEN NEW.id='outgoing' AND NEW.status='archived' BEGIN SELECT RAISE(ABORT,'injected activation failure'); END"
					}
					if _, err := app.store.DB().ExecContext(ctx, trigger); err != nil {
						t.Fatal(err)
					}
					response := requestJSON(t, app.Handler(), http.MethodPost, path, body, nil)
					if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "injected activation failure") {
						t.Fatalf("durable activation failed silently: %d %s", response.Code, response.Body.String())
					}
					app.mu.RLock()
					fenced := app.runs[detail.ID] == old && (old == nil || old.Status == "archived")
					app.mu.RUnlock()
					if !fenced {
						t.Fatal("failed activation left a writable target or revived outgoing Run")
					}
					runs, err := app.store.ListAgentRuns(ctx, detail.ID)
					if err != nil {
						t.Fatal(err)
					}
					disabled := 0
					for _, run := range runs {
						if run.ID == "outgoing" {
							wantStatus := "archived"
							if phase == "archive-status" {
								wantStatus = "idle" // failed persistence must not be reported as durable.
							}
							if run.Status != wantStatus || !run.ClosedAt.IsZero() {
								t.Fatalf("outgoing persistence or close invented: %#v", run)
							}
							continue
						}
						disabled++
						app.mu.RLock()
						addressable := app.runsByID[run.ID] != nil
						app.mu.RUnlock()
						if addressable || run.Status != "error" || (targetFails == "yes" && !run.ClosedAt.IsZero()) {
							t.Fatalf("target was not safely disabled: %#v addressable=%v", run, addressable)
						}
					}
					if disabled != 1 {
						t.Fatalf("disabled targets=%d, want 1", disabled)
					}
					for _, command := range []string{"send", "steer", "input", "interrupt"} {
						response := requestJSON(t, app.Handler(), http.MethodPost, "/api/v1/threads/"+detail.ID+"/agent/"+command, agentCommandRequest{Text: "must not run", InputRequestID: "input"}, nil)
						if response.Code != http.StatusConflict {
							t.Fatalf("%s bypassed failed activation: %d %s", command, response.Code, response.Body.String())
						}
					}
					if _, err := app.store.DB().ExecContext(ctx, "DROP TRIGGER reject_activation"); err != nil {
						t.Fatal(err)
					}
					if outgoing {
						// The compensated archived reference must retry old close
						// BEFORE creating another Run, even after the DB recovers.
						retry := requestJSON(t, app.Handler(), http.MethodPost, "/api/v1/threads/"+detail.ID+"/agent/switch", map[string]any{"provider": "claude"}, nil)
						if retry.Code != http.StatusBadGateway {
							t.Fatalf("failed close retry=%d %s", retry.Code, retry.Body.String())
						}
					}
					calls, err := os.ReadFile(log)
					if err != nil {
						t.Fatal(err)
					}
					if strings.Count(string(calls), `"method":"runs.create"`) != 1 {
						t.Fatalf("failure/retry created another target: %s", calls)
					}
					assertNoTargetPrompts(t, calls)
					if err := os.WriteFile(releaseOld, nil, 0o600); err != nil {
						t.Fatal(err)
					}
					// An explicit retry can recover after real old-close ACK and
					// successful durable activation. No automatic replay is used.
					recovered := requestJSON(t, app.Handler(), http.MethodPost, "/api/v1/threads/"+detail.ID+"/agent/switch", map[string]any{"provider": "claude"}, nil)
					if recovered.Code != http.StatusOK {
						t.Fatalf("explicit recovery=%d %s", recovered.Code, recovered.Body.String())
					}
					if originalStatus != string(serverTestGit(t, repo, "status", "--porcelain=v2", "-z")) {
						t.Fatal("replacement compensation changed original checkout")
					}
				})
			}
		}
	}
}

func assertNoTargetPrompts(t *testing.T, data []byte) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var call struct {
			Method string `json:"method"`
			Params struct {
				RunID string `json:"runId"`
			} `json:"params"`
		}
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			t.Fatal(err)
		}
		if call.Method == "runs.send" && call.Params.RunID != "outgoing" {
			t.Fatalf("target received a prompt before replacement completed: %s", data)
		}
	}
}
