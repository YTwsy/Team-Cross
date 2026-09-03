package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"teamcross/internal/bridgeclient"
	"teamcross/internal/domain"
	"teamcross/internal/share"
)

// Read is reached only after remoteHandler's original publication/capability
// checks. No listener or real Provider is involved in this slow-body fixture.
type delayedCommandBody struct {
	reader  io.Reader
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (body *delayedCommandBody) Read(data []byte) (int, error) {
	body.once.Do(func() { close(body.entered); <-body.release })
	return body.reader.Read(data)
}
func (*delayedCommandBody) Close() error { return nil }

func TestRevocationRejectsRemoteBodyThatWasOnlyHTTPAuthorized(t *testing.T) {
	for _, kind := range []string{"agent.send", "agent.steer", "agent.interrupt", "agent.input", "control.request", "control.renew", "control.release", "annotate"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			app := newIntegrationApp(t, t.TempDir())
			thread, err := app.store.CreateThread(ctx, domain.Thread{Title: "Synthetic admission", ReadOnly: true, Revision: 7})
			if err != nil {
				t.Fatal(err)
			}
			storedShare, err := app.store.CreateShare(ctx, domain.Share{ThreadID: thread.ID, SecretHash: "synthetic", ServerSPKI: "synthetic", Capabilities: []byte(`["view","annotate","send","steer","interrupt"]`), ExpiresAt: time.Now().Add(time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			participant, err := app.store.UpsertParticipant(ctx, domain.Participant{ID: "synthetic-participant", ShareID: storedShare.ID, Name: "Synthetic", Role: "observer"})
			if err != nil {
				t.Fatal(err)
			}
			lease, err := app.store.AcquireControl(ctx, storedShare.ID, participant.ID, time.Time{}, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			lease, err = app.store.GetControlLease(ctx, storedShare.ID)
			if err != nil {
				t.Fatal(err)
			}

			counter := filepath.Join(t.TempDir(), "synthetic-rpc-calls")
			script := writeBridgeFixture(t, t.TempDir(), `#!/bin/sh
counter=$1
while IFS= read -r line; do
  printf 'call\n' >> "$counter"
  id=$(printf '%s' "$line" | sed -E 's/.*"id":"([^"]+)".*/\1/')
  printf '{"jsonrpc":"2.0","id":"%s","result":{"turnId":"synthetic-turn"}}\n' "$id"
done
`)
			bridge, err := bridgeclient.Start(ctx, bridgeclient.StartOptions{Command: script, Args: []string{counter}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = bridge.Close() })
			app.bridge = bridge
			run := &managedRun{ID: "synthetic-run", ThreadID: thread.ID, Provider: "mock", Status: "idle"}
			app.runs[thread.ID], app.runsByID[run.ID] = run, run
			retiring, releaseRetire := make(chan struct{}), make(chan struct{})
			var releaseRetireOnce sync.Once
			finishRetire := func() { releaseRetireOnce.Do(func() { close(releaseRetire) }) }
			t.Cleanup(finishRetire)
			runtime := newFakeShareRuntime(share.RuntimeConfig{ShareID: storedShare.ID, ExpiresAt: storedShare.ExpiresAt})
			runtime.onClose = func() { close(retiring); <-releaseRetire }
			state := &hostedShare{ID: storedShare.ID, ThreadID: thread.ID, Runtime: runtime, ExpiresAt: storedShare.ExpiresAt}
			app.shares[thread.ID], app.shareByID[storedShare.ID] = state, state
			path := "/api/v1/threads/" + thread.ID
			commandID := "late-" + kind
			var input any
			switch kind {
			case "annotate":
				path += "/annotations"
				input = annotationRequest{Body: "must not persist", CommandID: commandID, ExpectedRevision: thread.Revision}
			case "control.request", "control.renew", "control.release":
				path += "/control"
				input = controlRequest{Action: kind[len("control."):], CommandID: commandID, ExpectedRevision: thread.Revision, LeaseEpoch: lease.Epoch}
			default:
				path += "/agent/" + kind[len("agent."):]
				input = agentCommandRequest{Text: "must not run", InputRequestID: "synthetic-input", Response: "must not answer", CommandID: commandID, ExpectedRevision: thread.Revision, LeaseEpoch: lease.Epoch}
			}
			encoded, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			body := &delayedCommandBody{reader: bytes.NewReader(encoded), entered: make(chan struct{}), release: make(chan struct{})}
			var releaseBodyOnce sync.Once
			releaseBody := func() { releaseBodyOnce.Do(func() { close(body.release) }) }
			t.Cleanup(releaseBody)
			request := httptest.NewRequest(http.MethodPost, path, body)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-TeamCross-Participant-ID", participant.ID)
			request.Header.Set("X-TeamCross-Participant-Name", "Synthetic")
			remoteResult := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				response := httptest.NewRecorder()
				app.remoteHandler(thread.ID, storedShare.ID).ServeHTTP(response, request)
				remoteResult <- response
			}()
			waitShareSignal(t, body.entered)
			ownerResult := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				ownerResult <- requestJSON(t, app.Handler(), http.MethodDelete, "/api/v1/threads/"+thread.ID+"/shares/current", nil, nil)
			}()
			waitShareSignal(t, retiring)
			// Runtime shutdown is draining and the revoke event has not advanced
			// revision. The durable revoked_at check must independently reject.
			current, err := app.store.GetThread(ctx, thread.ID)
			if err != nil || current.Revision != thread.Revision {
				t.Fatalf("revoke test lost old revision: %#v %v", current, err)
			}
			releaseBody()
			response := waitShareResult(t, remoteResult)
			if response.Code < 400 {
				t.Fatalf("late body executed: %d %s", response.Code, response.Body.String())
			}
			if _, err := app.store.GetCommand(ctx, storedShare.ID, commandID); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("revoked request acquired a command record: %v", err)
			}
			if data, err := os.ReadFile(counter); (err != nil && !errors.Is(err, os.ErrNotExist)) || len(data) != 0 {
				t.Fatalf("late command reached Bridge: %s %v", data, err)
			}
			annotations, err := app.store.ListAnnotations(ctx, thread.ID)
			if err != nil || len(annotations) != 0 {
				t.Fatalf("late annotation persisted: %#v %v", annotations, err)
			}
			currentLease, err := app.store.GetControlLease(ctx, storedShare.ID)
			if err != nil || currentLease != lease {
				t.Fatalf("late command changed lease: %#v %v", currentLease, err)
			}
			finishRetire()
			if revoked := waitShareResult(t, ownerResult); revoked.Code != 200 {
				t.Fatalf("revoke=%d %s", revoked.Code, revoked.Body.String())
			}
		})
	}
}
