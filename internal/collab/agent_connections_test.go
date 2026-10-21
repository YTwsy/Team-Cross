package collab

import (
	"context"
	"strings"
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
