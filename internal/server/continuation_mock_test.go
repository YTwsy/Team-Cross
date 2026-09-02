package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"teamcross/internal/bridgeclient"
)

// This exercises the real local Mock Adapter, not Codex/Claude or credentials.
// Source-only Go consumers may lack the optional built Node Bridge artifact.
func TestContinueRoundRealMockAdapterSealsNewRound(t *testing.T) {
	options, err := bridgeclient.DefaultCommand()
	if err != nil {
		t.Skipf("built local Bridge unavailable: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bridge, err := bridgeclient.Start(ctx, options)
	if err != nil {
		t.Skipf("Node Bridge unavailable: %v", err)
	}
	defer bridge.Close()
	probeCtx, probeCancel := context.WithTimeout(ctx, 3*time.Second)
	_, err = bridge.Probe(probeCtx)
	probeCancel()
	if err != nil {
		t.Fatalf("built Bridge readiness failed: %v", err)
	}
	repo := serverTestRepository(t)
	app := newIntegrationApp(t, repo)
	app.bridge = bridge
	go app.consumeBridgeEvents(bridge)
	detail := captureForContinuation(t, app)
	response := requestJSON(t, app.Handler(), http.MethodPost, "/api/v1/threads/"+detail.ID+"/continue", map[string]any{"roundId": detail.Rounds[0].ID, "provider": "mock", "prompt": "write a review result", "expectedRevision": detail.Revision}, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("Mock continuation=%d %s", response.Code, response.Body.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		rounds, err := app.store.ListRounds(ctx, detail.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(rounds) >= 2 {
			if rounds[1].Kind != "agent_turn" {
				t.Fatalf("unexpected sealed Round: %#v", rounds[1])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Mock Turn did not seal an immutable Round")
		}
		time.Sleep(20 * time.Millisecond)
	}
	data, err := os.ReadFile(filepath.Join(detail.Worktree, ".teamcross-mock-output.txt"))
	if err != nil || !strings.Contains(string(data), "write a review result") {
		t.Fatalf("Mock output=%q err=%v", data, err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".teamcross-mock-output.txt")); !os.IsNotExist(err) {
		t.Fatal("Mock wrote to source checkout")
	}
}
