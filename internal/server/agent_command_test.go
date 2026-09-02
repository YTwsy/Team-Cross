package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"teamcross/internal/bridgeclient"
	"teamcross/internal/domain"
)

func TestRemoteAgentCommandDurableReplayPrecedesMutableFences(t *testing.T) {
	ctx := context.Background()
	app := newIntegrationApp(t, serverTestRepository(t))
	thread, err := app.store.CreateThread(ctx, domain.Thread{
		Title: "Remote command replay", RepoRoot: app.config.Repo, Branch: "main",
		ReadOnly: true, Revision: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	share, err := app.store.CreateShare(ctx, domain.Share{
		ID: "agent-command-share", ThreadID: thread.ID, SecretHash: "secret", ServerSPKI: "pin",
		Capabilities: []byte(`["view","send","steer","interrupt"]`), ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.UpsertParticipant(ctx, domain.Participant{
		ID: "controller-one", ShareID: share.ID, Name: "Controller", Role: "observer|lan",
	}); err != nil {
		t.Fatal(err)
	}
	lease, err := app.store.AcquireControl(ctx, share.ID, "controller-one", time.Time{}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	counter := filepath.Join(t.TempDir(), "bridge-calls")
	script := writeBridgeFixture(t, t.TempDir(), `#!/bin/sh
counter=$1
while IFS= read -r line; do
  printf 'call\n' >> "$counter"
  id=$(printf '%s' "$line" | sed -E 's/.*"id":"([^"]+)".*/\1/')
  printf '{"jsonrpc":"2.0","id":"%s","result":{"turnId":"turn-one"}}\n' "$id"
done
`)
	bridge, err := bridgeclient.Start(ctx, bridgeclient.StartOptions{Command: script, Args: []string{counter}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bridge.Close() })
	app.bridge = bridge
	run := &managedRun{ID: "managed-run", ThreadID: thread.ID, Provider: "mock", Status: "idle"}
	app.runs[thread.ID] = run
	app.runsByID[run.ID] = run
	app.shares[thread.ID] = &hostedShare{ID: share.ID, ThreadID: thread.ID, Token: "test", ExpiresAt: share.ExpiresAt, Transports: []string{"lan"}}

	remote := app.remoteHandler(thread.ID, share.ID)
	headers := map[string]string{
		"X-TeamCross-Participant-ID":   "controller-one",
		"X-TeamCross-Participant-Name": "Controller",
		"X-TeamCross-Transport":        "lan",
	}
	command := agentCommandRequest{
		Text: "continue the task", CommandID: "durable-agent-command",
		ExpectedRevision: thread.Revision, LeaseEpoch: lease.Epoch,
	}
	path := "/api/v1/threads/" + thread.ID + "/agent/send"
	first := requestJSON(t, remote, http.MethodPost, path, command, headers)
	if first.Code != http.StatusOK {
		t.Fatalf("first command status = %d, body = %s", first.Code, first.Body.String())
	}
	var firstDetail threadDetail
	decodeResponse(t, first, &firstDetail)
	if firstDetail.Revision != thread.Revision+1 {
		t.Fatalf("first command revision = %d, want %d", firstDetail.Revision, thread.Revision+1)
	}
	persisted, err := app.store.GetCommand(ctx, share.ID, command.CommandID)
	if err != nil || persisted.Status != "completed" {
		t.Fatalf("persisted command = %#v, err = %v", persisted, err)
	}

	// Simulate the response being lost, followed by runtime state disappearing.
	// The exact retry must use the durable command record without dispatching.
	app.bridge = nil
	delete(app.runs, thread.ID)
	delete(app.runsByID, run.ID)
	replay := requestJSON(t, remote, http.MethodPost, path, command, headers)
	if replay.Code != http.StatusOK {
		t.Fatalf("replayed command status = %d, body = %s", replay.Code, replay.Body.String())
	}
	var replayDetail threadDetail
	decodeResponse(t, replay, &replayDetail)
	if replayDetail.Revision != firstDetail.Revision {
		t.Fatalf("replayed command revision = %d, want durable revision %d", replayDetail.Revision, firstDetail.Revision)
	}
	assertBridgeCallCount(t, counter, 1)
	assertEventCount(t, app.store, thread.ID, "command.send", 1)

	otherParticipantHeaders := map[string]string{
		"X-TeamCross-Participant-ID":   "controller-two",
		"X-TeamCross-Participant-Name": "Other controller",
		"X-TeamCross-Transport":        "lan",
	}
	otherParticipant := requestJSON(t, remote, http.MethodPost, path, command, otherParticipantHeaders)
	if otherParticipant.Code != http.StatusConflict || !strings.Contains(otherParticipant.Body.String(), "command_conflict") {
		t.Fatalf("cross-participant replay status = %d, body = %s", otherParticipant.Code, otherParticipant.Body.String())
	}
	assertBridgeCallCount(t, counter, 1)

	conflictBody := command
	conflictBody.Text = "different semantic input"
	conflict := requestJSON(t, remote, http.MethodPost, path, conflictBody, headers)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "command_conflict") {
		t.Fatalf("conflicting command status = %d, body = %s", conflict.Code, conflict.Body.String())
	}
	assertBridgeCallCount(t, counter, 1)

	// Restoring the runtime does not let the old controller dispatch a new ID
	// after the Owner has advanced the lease epoch.
	app.bridge = bridge
	app.runs[thread.ID] = run
	app.runsByID[run.ID] = run
	if _, err := app.store.PreemptControl(ctx, share.ID, "", time.Time{}, 0); err != nil {
		t.Fatal(err)
	}
	current, err := app.store.GetThread(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	fenced := agentCommandRequest{
		Text: "must not run", CommandID: "fenced-new-command",
		ExpectedRevision: current.Revision, LeaseEpoch: lease.Epoch,
	}
	fencedResponse := requestJSON(t, remote, http.MethodPost, path, fenced, headers)
	if fencedResponse.Code != http.StatusConflict || !strings.Contains(fencedResponse.Body.String(), "stale_lease") {
		t.Fatalf("fenced command status = %d, body = %s", fencedResponse.Code, fencedResponse.Body.String())
	}
	if _, err := app.store.GetCommand(ctx, share.ID, fenced.CommandID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("fenced command was claimed: %v", err)
	}
	assertBridgeCallCount(t, counter, 1)
}

func assertBridgeCallCount(t *testing.T, path string, want int) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Count(string(content), "call\n")
	if got != want {
		t.Fatalf("bridge call count = %d, want %d", got, want)
	}
}
