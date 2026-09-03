package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"teamcross/internal/bridgeclient"
	"teamcross/internal/domain"
)

func managedIdentityFixture(t *testing.T) (*App, *managedRun) {
	t.Helper()
	app := newIntegrationApp(t, serverTestRepository(t))
	thread, err := app.store.CreateThread(context.Background(), domain.Thread{Title: "identity", RepoRoot: app.config.Repo, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	run := &managedRun{ID: "managed-identity", ThreadID: thread.ID, Provider: "claude", Status: "starting"}
	if _, err := app.store.CreateAgentRun(context.Background(), domain.AgentRun{ID: run.ID, ThreadID: thread.ID, Provider: run.Provider, Status: run.Status}); err != nil {
		t.Fatal(err)
	}
	app.runsByID[run.ID] = run
	app.runs[thread.ID] = run
	if err := app.store.SaveRunBinding(context.Background(), run.ID, []byte(`{"mode":"managed","writer":"teamcross","fromRoundId":"selected-round","originThreadId":"source-thread","capabilities":{"send":true},"sessionRef":{"surface":"managed"}}`)); err != nil {
		t.Fatal(err)
	}
	return app, run
}

func assertManagedIdentity(t *testing.T, app *App, run *managedRun, want string) {
	t.Helper()
	app.mu.RLock()
	got := run.SessionID
	app.mu.RUnlock()
	if got != want {
		t.Fatalf("memory Session ID=%q; want %q", got, want)
	}
	stored, err := app.store.GetAgentRun(context.Background(), run.ID)
	if err != nil || stored.SessionID != want {
		t.Fatalf("stored identity=%#v, err=%v", stored, err)
	}
	payload, err := app.store.GetRunBinding(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var binding struct {
		SessionRef domain.SessionRef `json:"sessionRef"`
	}
	if err := json.Unmarshal(payload, &binding); err != nil || binding.SessionRef.SessionID != want || binding.SessionRef.Provider != "claude" {
		t.Fatalf("binding=%s, err=%v", payload, err)
	}
}

func identityEvent(run *managedRun, candidate string) bridgeclient.Event {
	return bridgeclient.Event{Type: "run.status", RunID: run.ID, Provider: run.Provider, Data: map[string]any{"sessionId": candidate, "status": "ready"}}
}

func TestBridgeDelayedIdentityAndDuplicateEventsPreserveProvenance(t *testing.T) {
	app, run := managedIdentityFixture(t)
	for _, candidate := range []string{"", "claude-pending-" + run.ID} {
		app.consumeBridgeEvent(identityEvent(run, candidate))
		assertManagedIdentity(t, app, run, "")
	}
	app.consumeBridgeEvent(identityEvent(run, "native-conversation"))
	for _, candidate := range []string{"native-conversation", "", "claude-pending-" + run.ID} {
		app.consumeBridgeEvent(identityEvent(run, candidate))
		assertManagedIdentity(t, app, run, "native-conversation")
	}
	if err := app.store.SaveRunBinding(context.Background(), run.ID, []byte(`{"host":"local","sessionRef":{"sessionId":""}}`)); err != nil {
		t.Fatal(err)
	}
	assertManagedIdentity(t, app, run, "native-conversation")
	payload, _ := app.store.GetRunBinding(context.Background(), run.ID)
	var binding map[string]any
	_ = json.Unmarshal(payload, &binding)
	if binding["fromRoundId"] != "selected-round" || binding["originThreadId"] != "source-thread" || binding["capabilities"] == nil || binding["writer"] != "teamcross" {
		t.Fatalf("source binding lost: %s", payload)
	}
	// A wrong Provider or unknown Run cannot supply identity for this Run.
	wrong := identityEvent(run, "wrong")
	wrong.Provider = "codex"
	app.consumeBridgeEvent(wrong)
	wrong.Provider = "claude"
	wrong.RunID = "other-run"
	app.consumeBridgeEvent(wrong)
	assertManagedIdentity(t, app, run, "native-conversation")
	app.mu.RLock()
	conflict := run.IdentityConflict
	app.mu.RUnlock()
	if conflict {
		t.Fatal("unrelated event poisoned the existing Run")
	}
	// Published event identity is canonical too, not the late placeholder.
	events, err := app.store.EventsAfter(context.Background(), run.ThreadID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		var value map[string]any
		_ = json.Unmarshal(event.Payload, &value)
		if value["sessionId"] == "claude-pending-"+run.ID {
			t.Fatal("placeholder was published as native identity")
		}
	}
}

func TestBridgeIdentityConflictRejectsRebindAndNewCommands(t *testing.T) {
	app, run := managedIdentityFixture(t)
	app.consumeBridgeEvent(identityEvent(run, "established"))
	app.consumeBridgeEvent(identityEvent(run, "different"))
	assertManagedIdentity(t, app, run, "established")
	if got, reason := app.reserveAgentCommand(run.ThreadID); got != nil || reason != "identity_conflict" {
		t.Fatalf("conflicted Run still accepts commands: %#v, %s", got, reason)
	}
	for _, command := range []string{"send", "steer", "input", "interrupt"} {
		result := requestJSON(t, app.Handler(), http.MethodPost, "/api/v1/threads/"+run.ThreadID+"/agent/"+command, agentCommandRequest{Text: "must not dispatch", InputRequestID: "input"}, nil)
		if result.Code != http.StatusConflict {
			t.Fatalf("%s was not fenced: %d %s", command, result.Code, result.Body.String())
		}
	}
	if err := app.sendManagedPrompt(context.Background(), run, "must not dispatch"); !errors.Is(err, domain.ErrSessionIdentityConflict) {
		t.Fatalf("first prompt did not fail closed: %v", err)
	}
	if summary := app.requestAgentHandoff(context.Background(), run); summary != "" {
		t.Fatal("conflicted Run dispatched handoff")
	}
	app.consumeBridgeEvent(identityEvent(run, "established"))
	app.consumeBridgeEvent(identityEvent(run, "different"))
	assertEventCount(t, app.store, run.ThreadID, "run.identity_conflict", 1)
	stored, _ := app.store.GetAgentRun(context.Background(), run.ID)
	if stored.Status != "identity_conflict" {
		t.Fatalf("conflict not durable: %#v", stored)
	}
	app.mu.RLock()
	status := run.Status
	app.mu.RUnlock()
	if status != "identity_conflict" {
		t.Fatalf("later status cleared conflict: %s", status)
	}
}

func TestLateBridgeIdentityDoesNotResurrectHistoricalRun(t *testing.T) {
	for _, status := range []string{"archived", "closed"} {
		t.Run(status, func(t *testing.T) {
			app, run := managedIdentityFixture(t)
			app.mu.Lock()
			run.Status = status
			current := &managedRun{ID: "current", ThreadID: run.ThreadID, Provider: "mock", Status: "idle"}
			app.runs[run.ThreadID] = current
			app.mu.Unlock()
			if err := app.store.UpdateAgentRun(context.Background(), run.ID, status, time.Time{}); err != nil {
				t.Fatal(err)
			}
			app.consumeBridgeEvent(identityEvent(run, "late-established"))
			app.consumeBridgeEvent(identityEvent(run, "must-not-rebind"))
			app.consumeBridgeEvent(identityEvent(run, ""))
			assertManagedIdentity(t, app, run, "late-established")
			app.mu.RLock()
			unchanged := run.Status == status && app.runs[run.ThreadID] == current
			app.mu.RUnlock()
			if !unchanged {
				t.Fatal("historical event changed current Writer or reactivated old Run")
			}
			stored, _ := app.store.GetAgentRun(context.Background(), run.ID)
			if stored.Status != status {
				t.Fatalf("historical state changed: %#v", stored)
			}
		})
	}
}

func TestManagedCreationLateDescriptorCannotOverwriteEarlyIdentityEvent(t *testing.T) {
	for _, descriptorKind := range []string{"empty", "legacy"} {
		t.Run(descriptorKind, func(t *testing.T) {
			app := newIntegrationApp(t, serverTestRepository(t))
			detail := captureForContinuation(t, app)
			thread, err := app.store.GetThread(context.Background(), detail.ID)
			if err != nil {
				t.Fatal(err)
			}
			release := filepath.Join(t.TempDir(), "release")
			script := writeBridgeFixture(t, t.TempDir(), `#!/bin/sh
release=$1
kind=$2
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -E 's/.*"id":"([^"]+)".*/\1/')
  case "$line" in
    *'"method":"runs.create"'*)
      run=$(printf '%s' "$line" | sed -E 's/.*"runId":"([^"]+)".*/\1/')
      printf '{"jsonrpc":"2.0","method":"event","params":{"type":"run.status","runId":"%s","provider":"claude","data":{"status":"ready","sessionId":"early-native-id"}}}\n' "$run"
      while [ ! -f "$release" ]; do sleep 0.01; done
      session=''
      if [ "$kind" = legacy ]; then session="claude-pending-$run"; fi
      printf '{"jsonrpc":"2.0","id":"%s","result":{"sessionId":"%s","status":"idle"}}\n' "$id" "$session";;
    *) printf '{"jsonrpc":"2.0","id":"%s","result":{}}\n' "$id";;
  esac
done
`)
			bridge, err := bridgeclient.Start(context.Background(), bridgeclient.StartOptions{Command: script, Args: []string{release, descriptorKind}})
			if err != nil {
				t.Fatal(err)
			}
			app.bridge = bridge
			consumed := make(chan struct{})
			go func() { app.consumeBridgeEvents(bridge); close(consumed) }()
			t.Cleanup(func() { _ = bridge.Close(); <-consumed })
			type creation struct {
				run *managedRun
				err error
			}
			created := make(chan creation, 1)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			go func() { run, err := app.createManagedRun(ctx, thread, "claude", false); created <- creation{run, err} }()
			// The event must be fully persisted before releasing the RPC reply,
			// which makes the ordering deterministic without timing assumptions.
			waitForEventType(t, app, thread.ID, "run.status")
			if err := os.WriteFile(release, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-created:
				if result.err != nil {
					t.Fatal(result.err)
				}
				assertManagedIdentity(t, app, result.run, "early-native-id")
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}

func TestManagedIdentityConcurrentReadsAndAdoption(t *testing.T) {
	app, run := managedIdentityFixture(t)
	start := make(chan struct{})
	errorsSeen := make(chan error, 40)
	var workers sync.WaitGroup
	for i := 0; i < 40; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			<-start
			if i%3 == 0 {
				app.attachEphemeralState(context.Background(), &threadDetail{ID: run.ThreadID}, access{Mode: "host"})
				return
			}
			candidate := "native"
			if i%3 == 1 {
				candidate = "claude-pending-" + run.ID
			}
			if err := app.adoptManagedRunIdentity(context.Background(), run, candidate); err != nil {
				errorsSeen <- err
			}
		}(i)
	}
	close(start)
	workers.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Error(err)
	}
	assertManagedIdentity(t, app, run, "native")
}

func TestManagedCreationUnknownIdentityResolvesAfterDescriptor(t *testing.T) {
	app := newIntegrationApp(t, serverTestRepository(t))
	detail := captureForContinuation(t, app)
	thread, err := app.store.GetThread(context.Background(), detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	script := writeBridgeFixture(t, t.TempDir(), `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -E 's/.*"id":"([^"]+)".*/\1/')
  printf '{"jsonrpc":"2.0","id":"%s","result":{"sessionId":"","status":"idle"}}\n' "$id"
done
`)
	bridge, err := bridgeclient.Start(context.Background(), bridgeclient.StartOptions{Command: script})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bridge.Close() })
	app.bridge = bridge
	run, err := app.createManagedRun(context.Background(), thread, "claude", false)
	if err != nil {
		t.Fatal(err)
	}
	assertManagedIdentity(t, app, run, "")
	if err := app.store.SaveRunBinding(context.Background(), run.ID, []byte(`{"fromRoundId":"continued-round","originThreadId":"fork-origin"}`)); err != nil {
		t.Fatal(err)
	}
	app.consumeBridgeEvent(identityEvent(run, "delayed-native-id"))
	assertManagedIdentity(t, app, run, "delayed-native-id")
}

func TestHistoricalTurnCompletionCannotSealCurrentWorktree(t *testing.T) {
	for _, state := range []string{"archived", "closed", "not-current", "identity-conflict", "no-current"} {
		t.Run(state, func(t *testing.T) {
			app := newIntegrationApp(t, serverTestRepository(t))
			detail := captureForContinuation(t, app)
			ctx := context.Background()
			stored, err := app.store.CreateAgentRun(ctx, domain.AgentRun{ThreadID: detail.ID, Provider: "mock", Status: "idle"})
			if err != nil {
				t.Fatal(err)
			}
			run := &managedRun{ID: stored.ID, ThreadID: detail.ID, Provider: "mock", Status: "idle"}
			app.runsByID[run.ID] = run
			app.runs[detail.ID] = run
			before, err := app.store.ListRounds(ctx, detail.ID)
			if err != nil {
				t.Fatal(err)
			}
			// Place unmistakable newer work in the shared worktree. The old
			// Run's delayed completion must not capture or attribute it.
			if err := os.WriteFile(filepath.Join(detail.Worktree, "new-writer.txt"), []byte("new Writer owns this\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			app.mu.Lock()
			switch state {
			case "archived", "closed":
				run.Status = state
			case "not-current":
				app.runs[detail.ID] = &managedRun{ID: "new-writer", ThreadID: detail.ID, Provider: "mock", Status: "idle"}
			case "identity-conflict":
				run.IdentityConflict = true
			case "no-current":
				delete(app.runs, detail.ID)
			}
			if app.canSealRunLocked(run) {
				t.Error("historical Run passed event-time seal gate")
			}
			app.mu.Unlock()
			app.consumeBridgeEvent(bridgeclient.Event{RunID: run.ID, Provider: run.Provider, Type: "turn.completed", TurnID: "late-turn", Data: map[string]any{"status": "completed"}})
			assertEventCount(t, app.store, detail.ID, "turn.completed", 1)
			events, err := app.store.EventsAfter(ctx, detail.ID, 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			completed := events[len(events)-1]
			// Exercise the inner gate directly as well: callers cannot bypass
			// the event-time check by handing it an old copied Run descriptor.
			app.sealCompletedTurn(run, "late-turn", completed, map[string]any{"status": "completed"})
			after, err := app.store.ListRounds(ctx, detail.ID)
			if err != nil || len(after) != len(before) {
				t.Fatalf("late completion sealed newer work: before=%d after=%d err=%v", len(before), len(after), err)
			}
			if _, err := os.Stat(app.contextManifestPath(detail.ID)); !os.IsNotExist(err) {
				t.Fatalf("historical completion wrote current manifest: %v", err)
			}
		})
	}
}

func TestQueuedTurnCompletionRechecksWriterAfterRoundLock(t *testing.T) {
	app := newIntegrationApp(t, serverTestRepository(t))
	detail := captureForContinuation(t, app)
	ctx := context.Background()
	stored, err := app.store.CreateAgentRun(ctx, domain.AgentRun{ThreadID: detail.ID, Provider: "mock", Status: "idle"})
	if err != nil {
		t.Fatal(err)
	}
	run := &managedRun{ID: stored.ID, ThreadID: detail.ID, Provider: "mock", Status: "idle"}
	app.runs[detail.ID] = run
	app.runsByID[run.ID] = run
	completed, err := app.store.AppendEvent(ctx, detail.ID, "turn.completed", []byte(`{"status":"completed"}`))
	if err != nil {
		t.Fatal(err)
	}
	before, err := app.store.ListRounds(ctx, detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := *run
	app.roundMu.Lock()
	done := make(chan struct{})
	go func() {
		app.sealCompletedTurn(&snapshot, "old-turn", completed, map[string]any{"status": "completed"})
		close(done)
	}()
	app.mu.Lock()
	app.runs[detail.ID] = &managedRun{ID: "new-writer", ThreadID: detail.ID, Provider: "mock", Status: "idle"}
	run.Status = "archived"
	app.mu.Unlock()
	app.roundMu.Unlock()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("queued completion did not finish")
	}
	after, err := app.store.ListRounds(ctx, detail.ID)
	if err != nil || len(after) != len(before) {
		t.Fatalf("queued historical completion sealed current worktree: %d -> %d, %v", len(before), len(after), err)
	}
}

func TestConflictedWriterMustCloseBeforeReplacement(t *testing.T) {
	for _, operation := range []string{"switch", "continue"} {
		for _, closeFails := range []bool{true, false} {
			t.Run(operation+map[bool]string{true: "/close-fails", false: "/close-succeeds"}[closeFails], func(t *testing.T) {
				app := newIntegrationApp(t, serverTestRepository(t))
				detail := captureForContinuation(t, app)
				ctx := context.Background()
				stored, err := app.store.CreateAgentRun(ctx, domain.AgentRun{ThreadID: detail.ID, Provider: "mock", SessionID: "old-native", Status: "identity_conflict"})
				if err != nil {
					t.Fatal(err)
				}
				old := &managedRun{ID: stored.ID, ThreadID: detail.ID, Provider: "mock", SessionID: "old-native", Status: "identity_conflict", IdentityConflict: true}
				app.runs[detail.ID] = old
				app.runsByID[old.ID] = old
				log := filepath.Join(t.TempDir(), "calls")
				script := writeBridgeFixture(t, t.TempDir(), `#!/bin/sh
log=$1
fail=$2
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$log"
  id=$(printf '%s' "$line" | sed -E 's/.*"id":"([^"]+)".*/\1/')
  case "$line" in
    *'"method":"runs.close"'*)
      if [ "$fail" = yes ]; then
        printf '{"jsonrpc":"2.0","id":"%s","error":{"code":-32000,"message":"old Writer still running"}}\n' "$id"
      else
        printf '{"jsonrpc":"2.0","id":"%s","result":{}}\n' "$id"
      fi;;
    *'"method":"runs.create"'*) printf '{"jsonrpc":"2.0","id":"%s","result":{"sessionId":"new-native","status":"idle"}}\n' "$id";;
    *'"method":"runs.send"'*) printf '{"jsonrpc":"2.0","id":"%s","result":{"turnId":"new-turn"}}\n' "$id";;
    *) printf '{"jsonrpc":"2.0","id":"%s","result":{}}\n' "$id";;
  esac
done
`)
				fail := "no"
				if closeFails {
					fail = "yes"
				}
				bridge, err := bridgeclient.Start(ctx, bridgeclient.StartOptions{Command: script, Args: []string{log, fail}})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = bridge.Close() })
				app.bridge = bridge
				path := "/api/v1/threads/" + detail.ID + "/agent/switch"
				body := map[string]any{"provider": "mock"}
				if operation == "continue" {
					path = "/api/v1/threads/" + detail.ID + "/continue"
					body = map[string]any{"provider": "mock", "roundId": detail.Rounds[0].ID, "prompt": "Continue safely", "expectedRevision": detail.Revision}
				}
				response := requestJSON(t, app.Handler(), http.MethodPost, path, body, nil)
				calls, err := os.ReadFile(log)
				if err != nil {
					t.Fatal(err)
				}
				lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
				if len(lines) == 0 || !strings.Contains(lines[0], `"method":"runs.close"`) {
					t.Fatalf("replacement started before close proof: %s", calls)
				}
				if closeFails {
					if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "agent_close_required") {
						t.Fatalf("failed close response=%d %s", response.Code, response.Body.String())
					}
					if len(lines) != 1 {
						t.Fatalf("RPCs executed after failed close: %s", calls)
					}
					runs, err := app.store.ListAgentRuns(ctx, detail.ID)
					if err != nil || len(runs) != 1 {
						t.Fatalf("replacement Run was created: %#v %v", runs, err)
					}
					app.mu.RLock()
					fenced := app.runs[detail.ID] == old && old.IdentityConflict
					app.mu.RUnlock()
					if !fenced {
						t.Fatal("failed close lost the old Writer fence")
					}
				} else {
					want := http.StatusOK
					if operation == "continue" {
						want = http.StatusCreated
					}
					if response.Code != want {
						t.Fatalf("successful close recovery=%d %s", response.Code, response.Body.String())
					}
					if !strings.Contains(string(calls), `"method":"runs.create"`) || !strings.Contains(string(calls), `"method":"runs.send"`) {
						t.Fatalf("replacement did not start after close: %s", calls)
					}
				}
			})
		}
	}
}

func TestConflictDiscoveredDuringHandoffStillRequiresClose(t *testing.T) {
	app := newIntegrationApp(t, serverTestRepository(t))
	detail := captureForContinuation(t, app)
	ctx := context.Background()
	stored, err := app.store.CreateAgentRun(ctx, domain.AgentRun{ID: "outgoing", ThreadID: detail.ID, Provider: "mock", SessionID: "established", Status: "idle"})
	if err != nil {
		t.Fatal(err)
	}
	old := &managedRun{ID: stored.ID, ThreadID: detail.ID, Provider: "mock", SessionID: "established", Status: "idle"}
	app.runs[detail.ID] = old
	app.runsByID[old.ID] = old
	log := filepath.Join(t.TempDir(), "calls")
	script := writeBridgeFixture(t, t.TempDir(), `#!/bin/sh
log=$1
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$log"
  id=$(printf '%s' "$line" | sed -E 's/.*"id":"([^"]+)".*/\1/')
  case "$line" in
    *'"method":"runs.send"'*)
      printf '{"jsonrpc":"2.0","method":"event","params":{"type":"run.status","runId":"outgoing","provider":"mock","data":{"status":"running","sessionId":"different"}}}\n'
      printf '{"jsonrpc":"2.0","method":"event","params":{"type":"message.completed","runId":"outgoing","provider":"mock","turnId":"handoff","data":{"text":"handoff"}}}\n'
      printf '{"jsonrpc":"2.0","method":"event","params":{"type":"turn.completed","runId":"outgoing","provider":"mock","turnId":"handoff","data":{"status":"completed"}}}\n'
      printf '{"jsonrpc":"2.0","id":"%s","result":{"turnId":"handoff"}}\n' "$id";;
    *'"method":"runs.close"'*) printf '{"jsonrpc":"2.0","id":"%s","error":{"code":-32000,"message":"old Writer still running"}}\n' "$id";;
    *) printf '{"jsonrpc":"2.0","id":"%s","result":{}}\n' "$id";;
  esac
done
`)
	bridge, err := bridgeclient.Start(ctx, bridgeclient.StartOptions{Command: script, Args: []string{log}})
	if err != nil {
		t.Fatal(err)
	}
	app.bridge = bridge
	consumed := make(chan struct{})
	go func() { app.consumeBridgeEvents(bridge); close(consumed) }()
	t.Cleanup(func() { _ = bridge.Close(); <-consumed })
	response := requestJSON(t, app.Handler(), http.MethodPost, "/api/v1/threads/"+detail.ID+"/agent/switch", map[string]any{"provider": "codex"}, nil)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "agent_close_required") {
		t.Fatalf("handoff conflict bypassed close gate: %d %s", response.Code, response.Body.String())
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(calls), `"method":"runs.create"`) || strings.Count(string(calls), `"method":"runs.send"`) != 1 || strings.Count(string(calls), `"method":"runs.close"`) != 1 {
		t.Fatalf("new Writer after handoff conflict: %s", calls)
	}
	runs, err := app.store.ListAgentRuns(ctx, detail.ID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("unexpected replacement Runs: %#v %v", runs, err)
	}
}

func TestReplacementCloseFailureNeverStartsTargetAndRemainsFenced(t *testing.T) {
	for _, operation := range []string{"switch", "continue"} {
		for _, phase := range []string{"ordinary-idle", "target-initialization", "outgoing-close"} {
			t.Run(operation+"/"+phase, func(t *testing.T) {
				app := newIntegrationApp(t, serverTestRepository(t))
				detail := captureForContinuation(t, app)
				ctx := context.Background()
				stored, err := app.store.CreateAgentRun(ctx, domain.AgentRun{ID: "outgoing", ThreadID: detail.ID, Provider: "mock", SessionID: "old-native", Status: "idle"})
				if err != nil {
					t.Fatal(err)
				}
				old := &managedRun{ID: stored.ID, ThreadID: detail.ID, Provider: "mock", SessionID: "old-native", Status: "idle"}
				app.runs[detail.ID] = old
				app.runsByID[old.ID] = old
				log := filepath.Join(t.TempDir(), "calls")
				release := filepath.Join(t.TempDir(), "release")
				script := writeBridgeFixture(t, t.TempDir(), `#!/bin/sh
log=$1
phase=$2
release=$3
emit_conflict() {
  printf '{"jsonrpc":"2.0","method":"event","params":{"type":"run.status","runId":"outgoing","provider":"mock","data":{"status":"running","sessionId":"different"}}}\n'
  while [ ! -f "$release" ]; do sleep 0.01; done
}
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$log"
  id=$(printf '%s' "$line" | sed -E 's/.*"id":"([^"]+)".*/\1/')
  run=$(printf '%s' "$line" | sed -E 's/.*"runId":"([^"]+)".*/\1/')
  case "$line" in
    *'"method":"runs.create"'*)
      if [ "$phase" = target-initialization ]; then emit_conflict; fi
      printf '{"jsonrpc":"2.0","id":"%s","result":{"sessionId":"new-native","status":"idle"}}\n' "$id";;
    *'"method":"runs.close"'*)
      if [ "$run" = outgoing ]; then
        if [ "$phase" = outgoing-close ]; then emit_conflict; fi
        printf '{"jsonrpc":"2.0","id":"%s","error":{"code":-32000,"message":"old Writer still running"}}\n' "$id"
      elif [ "$phase" = outgoing-close ]; then
        printf '{"jsonrpc":"2.0","id":"%s","error":{"code":-32000,"message":"target close unconfirmed"}}\n' "$id"
      else
        printf '{"jsonrpc":"2.0","id":"%s","result":{}}\n' "$id"
      fi;;
    *) printf '{"jsonrpc":"2.0","id":"%s","result":{}}\n' "$id";;
  esac
done
`)
				bridge, err := bridgeclient.Start(ctx, bridgeclient.StartOptions{Command: script, Args: []string{log, phase, release}})
				if err != nil {
					t.Fatal(err)
				}
				app.bridge = bridge
				consumed := make(chan struct{})
				go func() { app.consumeBridgeEvents(bridge); close(consumed) }()
				t.Cleanup(func() { _ = bridge.Close(); <-consumed })
				path := "/api/v1/threads/" + detail.ID + "/agent/switch"
				body := map[string]any{"provider": "claude"}
				if operation == "continue" {
					path = "/api/v1/threads/" + detail.ID + "/continue"
					body = map[string]any{"provider": "claude", "roundId": detail.Rounds[0].ID, "prompt": "safe continuation", "expectedRevision": detail.Revision}
				}
				done := make(chan struct{})
				var status int
				var responseBody string
				go func() {
					response := requestJSON(t, app.Handler(), http.MethodPost, path, body, nil)
					status = response.Code
					responseBody = response.Body.String()
					close(done)
				}()
				if phase != "ordinary-idle" {
					waitForEventType(t, app, detail.ID, "run.identity_conflict")
					if err := os.WriteFile(release, nil, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				select {
				case <-done:
				case <-time.After(10 * time.Second):
					t.Fatal("replacement request did not finish")
				}
				if status != http.StatusBadGateway || !strings.Contains(responseBody, "agent_close_required") {
					t.Fatalf("unconfirmed close accepted: %d %s", status, responseBody)
				}
				app.mu.RLock()
				fenced := app.runs[detail.ID] == old && old.Status == "archived"
				app.mu.RUnlock()
				if !fenced {
					t.Fatal("old reference not retained as archived close-retry fence")
				}
				runs, err := app.store.ListAgentRuns(ctx, detail.ID)
				if err != nil || len(runs) != 2 {
					t.Fatalf("Runs=%#v err=%v", runs, err)
				}
				for _, run := range runs {
					if run.ID == old.ID {
						continue
					}
					if run.Status != "error" {
						t.Fatalf("unstarted target not disabled: %#v", run)
					}
					if phase == "outgoing-close" && !run.ClosedAt.IsZero() {
						t.Fatalf("failed target close recorded as confirmed: %#v", run)
					}
					app.mu.RLock()
					addressable := app.runsByID[run.ID] != nil
					app.mu.RUnlock()
					if addressable {
						t.Fatal("disabled target remains addressable")
					}
				}
				for _, command := range []string{"send", "steer", "input", "interrupt"} {
					response := requestJSON(t, app.Handler(), http.MethodPost, "/api/v1/threads/"+detail.ID+"/agent/"+command, agentCommandRequest{Text: "must not run", InputRequestID: "input"}, nil)
					if response.Code != http.StatusConflict {
						t.Fatalf("%s bypassed close fence: %d %s", command, response.Code, response.Body.String())
					}
				}
				// An explicit transition may retry close, but another close failure
				// still cannot create a second target or send to either old/new Run.
				retry := requestJSON(t, app.Handler(), http.MethodPost, "/api/v1/threads/"+detail.ID+"/agent/switch", map[string]any{"provider": "claude"}, nil)
				if retry.Code != http.StatusBadGateway {
					t.Fatalf("close retry=%d %s", retry.Code, retry.Body.String())
				}
				calls, err := os.ReadFile(log)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Count(string(calls), `"method":"runs.create"`) != 1 {
					t.Fatalf("retry created another target: %s", calls)
				}
				for _, line := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
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
						t.Fatalf("target prompt executed after close failure: %s", calls)
					}
				}
			})
		}
	}
}

func TestActivationPreservesConfirmedOutgoingClose(t *testing.T) {
	app, old := managedIdentityFixture(t)
	app.mu.Lock()
	old.Status = "closed"
	app.mu.Unlock()
	if err := app.store.UpdateAgentRun(context.Background(), old.ID, "closed", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	target := &managedRun{ID: "new", ThreadID: old.ThreadID, Provider: "mock", Status: "idle"}
	if err := app.activatePreparedRun(context.Background(), old.ThreadID, old, target); err != nil {
		t.Fatal(err)
	}
	app.mu.RLock()
	status := old.Status
	app.mu.RUnlock()
	if status != "closed" {
		t.Fatalf("activation demoted confirmed close: %s", status)
	}
	// No Bridge is installed: re-closing an already acknowledged Run must be
	// a no-op, including when identity metadata arrives late.
	if err := app.closeOutgoingRun(context.Background(), old.ThreadID, old); err != nil {
		t.Fatal(err)
	}
}
