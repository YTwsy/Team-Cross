package collab

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"teamcross/internal/mcp"
)

func TestConnectChatGPTLinksIdempotentlyWithoutReceivingOrNativeSession(t *testing.T) {
	a, f, s := spaceFixture(t)
	c := agentCaller{Caller: mcp.CallerSource{Provider: "chatgpt", SourceID: strings.Repeat("a", 64)}}
	ctx := context.Background()
	first, err := a.connectCurrentSession(ctx, c, s.record.ID, "当前讨论", "")
	if err != nil {
		t.Fatal(err)
	}
	result := first.(map[string]any)
	if result["linked"] != true || result["receiving"] != false {
		t.Fatal(result)
	}
	pair := result["pairing"].(AgentPairing)
	if pair.State != "linked" || pair.SessionID != c.Caller.SourceID {
		t.Fatal(pair)
	}
	second, err := a.connectCurrentSession(ctx, c, s.record.ID, "", "")
	if err != nil || second.(map[string]any)["pairing"].(AgentPairing).ID != pair.ID {
		t.Fatal(second, err)
	}
	a.agentMu.Lock()
	p := a.agents.Pairings[pair.ID]
	p.ExpiresAt = time.Now().Add(-time.Hour)
	a.agents.Pairings[pair.ID] = p
	a.agentMu.Unlock()
	if _, err := a.createAgentPairing("other"); err != nil {
		t.Fatal(err)
	}
	for _, p := range a.agentPairings() {
		if p.ID == pair.ID && p.State != "linked" {
			t.Fatal("association expired with old code", p)
		}
	}
	a.pumpWorkbench(ctx)
	view := workbenchDecode[WorkbenchView](wb(t, a, s.record.ID, "view", workbenchInput{}))
	if len(view.Targets) != 1 || view.Targets[0].Available {
		t.Fatal(view.Targets)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, call := range f.calls {
		if call == "turn/start" || call == "thread/resume" || call == "thread/start" {
			t.Fatal("connection changed native sessions", f.calls)
		}
	}
}

func TestCurrentConnectionTracksExactSpaceAndRemovalWithoutWrites(t *testing.T) {
	a, f, s := spaceFixture(t)
	c := agentCaller{Caller: mcp.CallerSource{Provider: "codex", SourceID: f.source.ID, TurnID: "turn-fixture"}}
	_, target := connectNamedFixture(t, a, c, s.record.ID, "")
	ctx := context.Background()
	check := func(caller mcp.CallerSource, want bool) {
		t.Helper()
		raw, err := a.currentConnection(ctx, caller, s.record.ID)
		if err != nil {
			t.Fatal(err)
		}
		out := raw.(map[string]any)
		if out["known"] != true || out["linked"] != want || out["receiving"] != false {
			t.Fatal(out)
		}
	}
	c.Caller.TurnID = "" // A panel may be read while the model is idle.
	f.mu.Lock()
	beforeCalls := len(f.calls)
	f.mu.Unlock()
	check(c.Caller, true)
	check(mcp.CallerSource{Provider: "chatgpt", SourceID: strings.Repeat("a", 64)}, false)
	if _, err := a.currentConnection(ctx, c.Caller, "unrelated-space"); err == nil {
		t.Fatal("accepted unavailable space")
	}
	wb(t, a, s.record.ID, "remove", workbenchInput{TargetID: target.ID})
	check(c.Caller, false)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) != beforeCalls {
		t.Fatal("status lookup called the native client", f.calls)
	}
}

func TestConcurrentCurrentConnectionsReusePairAndTarget(t *testing.T) {
	a, _, s := spaceFixture(t)
	c := agentCaller{Caller: mcp.CallerSource{Provider: "chatgpt", SourceID: strings.Repeat("c", 64)}}
	var wg sync.WaitGroup
	results := make(chan any, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := a.connectCurrentSession(context.Background(), c, s.record.ID, "", "")
			if err != nil {
				results <- err
			} else {
				results <- out
			}
		}()
	}
	wg.Wait()
	close(results)
	var id string
	for result := range results {
		out, ok := result.(map[string]any)
		if !ok {
			t.Fatal(result)
		}
		target := workbenchDecode[SpaceTarget](out["target"])
		if id != "" && target.ID != id {
			t.Fatal("duplicate target", result)
		}
		id = target.ID
	}
	a.agentMu.Lock()
	defer a.agentMu.Unlock()
	if len(a.agents.Pairings) != 1 {
		t.Fatal(a.agents.Pairings)
	}
}
