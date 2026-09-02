package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"teamcross/internal/bridgeclient"
	"teamcross/internal/domain"
)

func TestCommittedAgentActivationSurvivesRequestCancellationAndPersistsBeforeClose(t *testing.T) {
	app := newIntegrationApp(t, serverTestRepository(t))
	thread, err := app.store.CreateThread(context.Background(), domain.Thread{
		Title: "activation order", RepoRoot: app.config.Repo, Branch: "main", ReadOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	current := &managedRun{ID: "outgoing", ThreadID: thread.ID, Provider: "mock", SessionID: "old-session", Status: "idle"}
	target := &managedRun{ID: "incoming", ThreadID: thread.ID, Provider: "codex", SessionID: "new-session", Status: "idle"}
	for _, run := range []*managedRun{current, target} {
		if _, err := app.store.CreateAgentRun(context.Background(), domain.AgentRun{
			ID: run.ID, ThreadID: thread.ID, Provider: run.Provider, SessionID: run.SessionID, Status: run.Status,
		}); err != nil {
			t.Fatal(err)
		}
		app.runsByID[run.ID] = run
	}
	app.runs[thread.ID] = current

	directory := t.TempDir()
	closeStarted := filepath.Join(directory, "close-started")
	closeGate := filepath.Join(directory, "close-gate")
	promptSent := filepath.Join(directory, "prompt-sent")
	script := writeBridgeFixture(t, directory, `#!/bin/sh
started=$1
gate=$2
prompt=$3
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -E 's/.*"id":"([^"]+)".*/\1/')
  case "$line" in
    *'"method":"runs.close"'*)
      printf 'started\n' > "$started"
      while [ ! -f "$gate" ]; do sleep 0.01; done
      ;;
    *'"method":"runs.send"'*)
      printf 'sent\n' > "$prompt"
      printf '{"jsonrpc":"2.0","id":"%s","result":{"turnId":"incoming-turn"}}\n' "$id"
      continue
      ;;
  esac
  printf '{"jsonrpc":"2.0","id":"%s","result":{}}\n' "$id"
done
`)
	bridge, err := bridgeclient.Start(context.Background(), bridgeclient.StartOptions{Command: script, Args: []string{closeStarted, closeGate, promptSent}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bridge.Close() })
	app.bridge = bridge

	requestCtx, requestCancel := context.WithCancel(context.Background())
	requestCancel()
	transitionCtx, transitionCancel := committedAgentTransitionContext(requestCtx)
	defer transitionCancel()
	if err := transitionCtx.Err(); err != nil {
		t.Fatalf("committed transition inherited request cancellation: %v", err)
	}
	deadline, bounded := transitionCtx.Deadline()
	if remaining := time.Until(deadline); !bounded || remaining <= 0 || remaining > committedAgentTransitionTimeout {
		t.Fatalf("committed transition deadline = %v, bounded %v", remaining, bounded)
	}

	app.activatePreparedRun(transitionCtx, thread.ID, current, target)
	if app.runs[thread.ID] != target || current.Status != "archived" {
		t.Fatalf("activation state: current=%#v outgoing=%#v", app.runs[thread.ID], current)
	}
	if _, err := os.Stat(closeStarted); !os.IsNotExist(err) {
		t.Fatalf("activation unexpectedly closed outgoing Run: %v", err)
	}
	if _, err := app.store.AppendEvent(transitionCtx, thread.ID, "agent.switched", jsonBytes(map[string]any{
		"actor": "Owner", "from": current.Provider, "to": target.Provider,
	})); err != nil {
		t.Fatal(err)
	}

	closed := make(chan struct{})
	go func() {
		app.closeOutgoingRun(transitionCtx, thread.ID, current)
		close(closed)
	}()
	waitForFile(t, closeStarted)
	assertEventCount(t, app.store, thread.ID, "agent.switched", 1)
	select {
	case <-closed:
		t.Fatal("outgoing close did not wait for the fixture gate")
	default:
	}
	if err := os.WriteFile(closeGate, []byte("continue\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("outgoing close did not finish")
	}
	if err := app.sendManagedPrompt(transitionCtx, target, "handoff prompt"); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, promptSent)
	if target.Status != "running" || target.TurnID != "incoming-turn" {
		t.Fatalf("target prompt state = status %q, turn %q", target.Status, target.TurnID)
	}
}

func TestAgentTransitionFenceBlocksActiveRunsAndConcurrentWork(t *testing.T) {
	app := newIntegrationApp(t, serverTestRepository(t))
	threadID := "transition-thread"

	for _, status := range []string{"starting", "running", "waiting"} {
		run := &managedRun{ID: "run-" + status, ThreadID: threadID, Provider: "mock", Status: status}
		app.runs[threadID] = run
		if _, _, conflict := app.beginAgentTransition(threadID, "codex", false, true); conflict != "turn" {
			t.Fatalf("status %q conflict = %q, want turn", status, conflict)
		}
		if app.switching[threadID] {
			t.Fatalf("status %q claimed switching fence", status)
		}
	}

	idle := &managedRun{ID: "run-idle", ThreadID: threadID, Provider: "mock", Status: "idle"}
	app.runs[threadID] = idle
	if _, reservation := app.reserveAgentCommand(threadID); reservation != "" {
		t.Fatalf("reserve command = %q", reservation)
	}
	if _, _, conflict := app.beginAgentTransition(threadID, "codex", false, true); conflict != "command" {
		t.Fatalf("transition during command conflict = %q, want command", conflict)
	}
	app.releaseAgentCommand(threadID)

	current, noOp, conflict := app.beginAgentTransition(threadID, "codex", false, true)
	if conflict != "" || noOp || current != idle {
		t.Fatalf("begin transition = current %#v, noOp %v, conflict %q", current, noOp, conflict)
	}
	if _, reservation := app.reserveAgentCommand(threadID); reservation != "transition" {
		t.Fatalf("command during transition reservation = %q", reservation)
	}
	if _, _, conflict := app.beginAgentTransition(threadID, "claude", false, true); conflict != "transition" {
		t.Fatalf("second transition conflict = %q", conflict)
	}
	app.endAgentTransition(threadID)
}

func TestAgentHandoffWaitsForPersistedTurnCompletion(t *testing.T) {
	app := newIntegrationApp(t, serverTestRepository(t))
	thread, err := app.store.CreateThread(context.Background(), domain.Thread{
		Title: "handoff waiter", RepoRoot: app.config.Repo, Branch: "main", ReadOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	gate := filepath.Join(t.TempDir(), "complete")
	script := writeBridgeFixture(t, t.TempDir(), `#!/bin/sh
gate=$1
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -E 's/.*"id":"([^"]+)".*/\1/')
  case "$line" in
    *'"method":"runs.send"'*)
      printf '{"jsonrpc":"2.0","id":"%s","result":{"turnId":"handoff-turn"}}\n' "$id"
      printf '{"jsonrpc":"2.0","method":"event","params":{"runId":"outgoing","provider":"mock","type":"message.completed","turnId":"handoff-turn","data":{"text":"structured summary"}}}\n'
      while [ ! -f "$gate" ]; do sleep 0.01; done
      printf '{"jsonrpc":"2.0","method":"event","params":{"runId":"outgoing","provider":"mock","type":"turn.completed","turnId":"handoff-turn","data":{"status":"completed"}}}\n'
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":"%s","result":{}}\n' "$id"
      ;;
  esac
done
`)
	bridge, err := bridgeclient.Start(context.Background(), bridgeclient.StartOptions{Command: script, Args: []string{gate}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bridge.Close() })
	app.bridge = bridge
	run := &managedRun{ID: "outgoing", ThreadID: thread.ID, Provider: "mock", Status: "idle"}
	app.runs[thread.ID] = run
	app.runsByID[run.ID] = run
	go app.consumeBridgeEvents(bridge)

	result := make(chan string, 1)
	go func() { result <- app.requestAgentHandoff(context.Background(), run) }()
	waitForEventType(t, app, thread.ID, "message.completed")
	select {
	case summary := <-result:
		t.Fatalf("handoff returned before turn.completed: %q", summary)
	default:
	}
	if err := os.WriteFile(gate, []byte("complete\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case summary := <-result:
		if summary != "structured summary" {
			t.Fatalf("handoff summary = %q", summary)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handoff did not finish after turn.completed")
	}
	waitForEventType(t, app, thread.ID, "turn.completed")
}

func TestPrepareAgentSwitchInterruptsTimeoutAndUsesLatestFallback(t *testing.T) {
	repo := serverTestRepository(t)
	app := newIntegrationApp(t, repo)
	baseline := strings.TrimSpace(string(serverTestGit(t, repo, "rev-parse", "HEAD")))
	thread, err := app.store.CreateThread(context.Background(), domain.Thread{
		Title: "fallback", RepoRoot: repo, WorktreePath: repo, Branch: "main",
		BaselineCommit: baseline, ReadOnly: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.CreateRound(context.Background(), domain.Round{ThreadID: thread.ID, Number: 0, Kind: "capture"}); err != nil {
		t.Fatal(err)
	}
	script := writeBridgeFixture(t, t.TempDir(), `#!/bin/sh
worktree=$1
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -E 's/.*"id":"([^"]+)".*/\1/')
  case "$line" in
    *'"method":"runs.send"'*)
      printf 'changed during handoff\n' > "$worktree/tracked.txt"
      printf '{"jsonrpc":"2.0","id":"%s","result":{"turnId":"timeout-turn"}}\n' "$id"
      printf '{"jsonrpc":"2.0","method":"event","params":{"runId":"outgoing","provider":"mock","type":"message.completed","turnId":"timeout-turn","data":{"text":"   "}}}\n'
      ;;
    *'"method":"runs.interrupt"'*)
      printf '{"jsonrpc":"2.0","id":"%s","result":{"interrupted":true}}\n' "$id"
      printf '{"jsonrpc":"2.0","method":"event","params":{"runId":"outgoing","provider":"mock","type":"turn.completed","turnId":"timeout-turn","data":{"status":"interrupted"}}}\n'
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":"%s","result":{}}\n' "$id"
      ;;
  esac
done
`)
	bridge, err := bridgeclient.Start(context.Background(), bridgeclient.StartOptions{Command: script, Args: []string{repo}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bridge.Close() })
	app.bridge = bridge
	app.handoffTimeout = 30 * time.Millisecond
	app.handoffTerminalTimeout = time.Second
	run := &managedRun{ID: "outgoing", ThreadID: thread.ID, Provider: "mock", Status: "idle"}
	app.runs[thread.ID] = run
	app.runsByID[run.ID] = run
	go app.consumeBridgeEvents(bridge)

	prepared, err := app.prepareAgentSwitch(context.Background(), thread, run, "codex")
	if err != nil {
		t.Fatal(err)
	}
	defer app.discardPreparedAgentSwitch(prepared)
	if !strings.Contains(prepared.summary, "tracked.txt") {
		t.Fatalf("fallback did not reflect latest Git status: %q", prepared.summary)
	}
	events, err := app.store.EventsAfter(context.Background(), thread.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 || events[len(events)-1].Type != "turn.completed" {
		t.Fatalf("handoff terminal event was not persisted before prepare returned: %#v", events)
	}
}

func TestPreparedManifestDoesNotReplaceFixedManifestBeforeCommit(t *testing.T) {
	repo := serverTestRepository(t)
	app := newIntegrationApp(t, repo)
	baseline := strings.TrimSpace(string(serverTestGit(t, repo, "rev-parse", "HEAD")))
	thread, err := app.store.CreateThread(context.Background(), domain.Thread{
		Title: "manifest", RepoRoot: repo, WorktreePath: repo, Branch: "main",
		BaselineCommit: baseline, ReadOnly: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixed := app.contextManifestPath(thread.ID)
	if err := os.WriteFile(fixed, []byte("previous manifest\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := app.prepareAgentSwitch(context.Background(), thread, nil, "mock")
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(fixed)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "previous manifest\n" {
		t.Fatalf("prepare replaced fixed manifest: %q", content)
	}
	if err := app.commitPreparedAgentSwitch(context.Background(), prepared); err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(fixed)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) == "previous manifest\n" || !strings.Contains(string(content), `"toProvider": "mock"`) {
		t.Fatalf("commit did not publish prepared manifest: %q", content)
	}
}

func TestPreparedManifestIsRemovedWhenRoundCommitFails(t *testing.T) {
	repo := serverTestRepository(t)
	app := newIntegrationApp(t, repo)
	thread, err := app.store.CreateThread(context.Background(), domain.Thread{
		Title: "failed manifest commit", RepoRoot: repo, WorktreePath: repo,
		Branch: "main", ReadOnly: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.CreateRound(context.Background(), domain.Round{ThreadID: thread.ID, Number: 0, Kind: "capture"}); err != nil {
		t.Fatal(err)
	}
	fixed := app.contextManifestPath(thread.ID)
	if err := os.WriteFile(fixed, []byte("stable alias\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"version":1}`)
	unique, err := app.stageContextManifest(thread.ID, manifest)
	if err != nil {
		t.Fatal(err)
	}
	prepared := &preparedAgentSwitch{
		manifestPath: unique, fixedPath: fixed, manifest: manifest, pendingPath: unique,
		round: &domain.Round{ThreadID: thread.ID, Number: 2, Kind: "agent_switch"},
	}
	if err := app.commitPreparedAgentSwitch(context.Background(), prepared); err == nil {
		t.Fatal("invalid Round sequence unexpectedly committed")
	}
	if _, err := os.Stat(unique); !os.IsNotExist(err) {
		t.Fatalf("uncommitted unique manifest still exists: %v", err)
	}
	content, err := os.ReadFile(fixed)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "stable alias\n" {
		t.Fatalf("failed Round commit changed fixed alias: %q", content)
	}
}

func waitForEventType(t *testing.T, app *App, threadID, eventType string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		events, err := app.store.EventsAfter(context.Background(), threadID, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range events {
			if event.Type == eventType {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("event %q was not persisted", eventType)
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("file %q was not created", path)
}
