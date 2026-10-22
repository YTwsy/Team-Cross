package collab

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/mcp"
)

func connectNamedFixture(t *testing.T, a *App, c agentCaller, spaceID, name string) (AgentPairing, SpaceTarget) {
	t.Helper()
	raw, err := a.connectCurrentSession(context.Background(), c, spaceID, name, "")
	if err != nil {
		t.Fatal(err)
	}
	out := raw.(map[string]any)
	return out["pairing"].(AgentPairing), workbenchDecode[SpaceTarget](out["target"])
}

func TestConnectSessionUsesNativeTitleAndPreservesExplicitName(t *testing.T) {
	a, f, s := spaceFixture(t)
	c := agentCaller{Caller: mcp.CallerSource{Provider: "codex", SourceID: f.source.ID, TurnID: "turn-fixture"}}
	f.source.Name = "顺流感知设计"
	p, target := connectNamedFixture(t, a, c, s.record.ID, "")
	if p.Name != f.source.Name || p.NameSource != "native" || target.Name != p.Name {
		t.Fatal(p, target)
	}
	wb(t, a, s.record.ID, "inflow-config", workbenchInput{TargetID: target.ID, Enabled: true})
	note := workbenchDecode[SpaceNote](wb(t, a, s.record.ID, "inflow-note", workbenchInput{RequestID: uuid.NewString(), TargetID: target.ID, Instruction: "保留这条留言"}))
	f.source.Name = "更新后的原生标题"
	updated, next := connectNamedFixture(t, a, c, s.record.ID, "")
	if updated.Name != f.source.Name || updated.ID != p.ID || next.ID != target.ID {
		t.Fatal(updated, next)
	}
	// Even the former placeholder is a legitimate explicitly chosen name.
	custom, _ := connectNamedFixture(t, a, c, s.record.ID, "当前会话")
	f.source.Name = "不覆盖自定义名称"
	again, _ := connectNamedFixture(t, a, c, s.record.ID, "")
	a.pumpWorkbench(context.Background())
	view := workbenchDecode[WorkbenchView](wb(t, a, s.record.ID, "view", workbenchInput{}))
	if custom.NameSource != "custom" || again.Name != custom.Name || len(view.Targets) != 1 || view.Targets[0].Name != custom.Name || !view.Targets[0].InFlow {
		t.Fatal(custom, again, view)
	}
	if got := workbenchDecode[SpaceNote](wb(t, a, s.record.ID, "inflow-note-read", workbenchInput{RequestID: note.ID})); got.TargetID != target.ID || got.DeliveredAt != nil {
		t.Fatal("renaming changed the pending note", got)
	}
}

func TestConnectSessionTitleLimitsAndPrivateFallback(t *testing.T) {
	for _, title := range []string{strings.Repeat("中", 80), strings.Repeat("中", 100), ""} {
		t.Run(title, func(t *testing.T) {
			a, f, s := spaceFixture(t)
			f.source.Name, f.source.Preview = title, "private message content"
			c := agentCaller{Caller: mcp.CallerSource{Provider: "codex", SourceID: f.source.ID, TurnID: "turn-fixture"}}
			p, target := connectNamedFixture(t, a, c, s.record.ID, "")
			if len([]rune(p.Name)) > 80 || target.Name != p.Name || strings.Contains(p.Name, f.source.Preview) || strings.Contains(p.Name, f.source.ID) {
				t.Fatal(p, target)
			}
			if title == "" && (!strings.HasPrefix(p.Name, "Codex · ") || p.NameSource != "fallback") {
				t.Fatal(p)
			}
			if title != "" && (p.NameSource != "native" || len([]rune(p.Name)) != 80) {
				t.Fatal(p)
			}
		})
	}
	a, _, s := spaceFixture(t)
	var names []string
	for _, id := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64)} {
		p, _ := connectNamedFixture(t, a, agentCaller{Caller: mcp.CallerSource{Provider: "chatgpt", SourceID: id}}, s.record.ID, "")
		if !strings.HasPrefix(p.Name, "ChatGPT · ") || strings.Contains(p.Name, id) {
			t.Fatal(p)
		}
		names = append(names, p.Name)
	}
	if names[0] == names[1] {
		t.Fatal("unnamed conversations remain indistinguishable", names)
	}
}

func TestConnectSessionUsesBoundRuntimeTitle(t *testing.T) {
	a, f, _ := fixture(t)
	stopWorkbenchPump(a)
	s := createFixture(t, a, f, "existing")
	f.mu.Lock()
	thread := f.threads[s.record.SessionID]
	thread.Name = "协作执行标题"
	f.threads[thread.ID] = thread
	f.mu.Unlock()
	c := agentCaller{Caller: mcp.CallerSource{Provider: "codex", SourceID: thread.ID}}
	raw, err := a.connectCurrentSession(context.Background(), c, s.spaceID(), "", s.record.ID)
	if err != nil {
		t.Fatal(err)
	}
	out := raw.(map[string]any)
	p := out["pairing"].(AgentPairing)
	target := workbenchDecode[SpaceTarget](out["target"])
	if p.Name != thread.Name || p.NameSource != "native" || target.ExecutionID != s.record.ID || target.Name != thread.Name {
		t.Fatal(p, target)
	}
}

func TestConnectSessionUsesClaudeTitle(t *testing.T) {
	a, _, s := spaceFixture(t)
	a.Config.ClaudeHome = t.TempDir()
	c := agentCaller{Caller: mcp.CallerSource{Provider: "claude", SourceID: uuid.NewString(), ToolUseID: "verify"}}
	writeAgentClaudeTranscript(t, a, c)
	path := filepath.Join(a.Config.ClaudeHome, "projects", "fixture", c.Caller.SourceID+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("{\"customTitle\":\"Claude 设计讨论\"}\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	p, target := connectNamedFixture(t, a, c, s.record.ID, "")
	if p.Name != "Claude 设计讨论" || p.NameSource != "native" || target.Name != p.Name {
		t.Fatal(p, target)
	}
}

func TestLegacySessionNamesRepairExactSourcesWithoutReconnecting(t *testing.T) {
	a, f, s := spaceFixture(t)
	ctx := context.Background()
	ids := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	f.threads = map[string]Source{
		ids[0]: {ID: ids[0], Name: "设计讨论"},
		ids[1]: {ID: ids[1], Name: "实现与验证"},
		ids[2]: {ID: uuid.NewString(), Name: "错误来源的名称"},
	}
	targets := make([]SpaceTarget, len(ids))
	pairIDs := make([]string, len(ids))
	for i, id := range ids {
		p := AgentPairing{ID: uuid.NewString(), Name: "当前会话", Provider: "codex", SessionID: id, State: "linked", CreatedAt: time.Now()}
		a.agentMu.Lock()
		a.agents.Pairings[p.ID] = p
		a.agentMu.Unlock()
		raw, err := a.registerSpacePairing(ctx, s.record.ID, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		targets[i], pairIDs[i] = workbenchDecode[SpaceTarget](raw), p.ID
	}
	wb(t, a, s.record.ID, "inflow-config", workbenchInput{TargetID: targets[0].ID, Enabled: true})
	wb(t, a, s.record.ID, "remove", workbenchInput{TargetID: targets[1].ID})
	a.pumpWorkbench(ctx)
	var persisted agentState
	if err := readJSON(filepath.Join(a.Config.DataDir, "agent-pairings.json"), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Pairings[pairIDs[0]].Name != "设计讨论" || persisted.Pairings[pairIDs[1]].Name != "实现与验证" || persisted.Pairings[pairIDs[2]].Name != "当前会话" {
		t.Fatal(persisted.Pairings)
	}
	s.mu.Lock()
	first, removed := s.record.Workbench.Targets[targets[0].ID], s.record.Workbench.Targets[targets[1].ID]
	pref := s.record.Workbench.InFlow[inFlowRecipient(targets[0].ID, "")]
	s.mu.Unlock()
	if first.Name != "设计讨论" || !pref.Enabled || !removed.Removed || removed.Name != "当前会话" {
		t.Fatal(first, removed, pref)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, method := range f.calls {
		if method == "thread/start" || method == "thread/resume" || method == "turn/start" || method == "thread/fork" {
			t.Fatal("name repair changed the native conversation", f.calls)
		}
	}
}

func TestSpaceTargetNameSyncIsMemberScopedAndKeepsIdentity(t *testing.T) {
	a, _, s := spaceFixture(t)
	ctx := context.Background()
	if err := s.Share(ctx, "lan"); err != nil {
		t.Fatal(err)
	}
	b, _, _ := fixture(t)
	stopWorkbenchPump(b)
	j, err := b.Join(ctx, s.sharedRuntime().Token())
	if err != nil {
		t.Fatal(err)
	}
	_, _, own := spaceChannel(t, a, s.record.ID)
	p, _, member := spaceChannel(t, b, j.ID)
	b.agentMu.Lock()
	stored := b.agents.Pairings[p.ID]
	stored.Name = "成员修改的标题"
	b.agents.Pairings[p.ID] = stored
	b.agentMu.Unlock()
	b.pumpWorkbench(ctx)
	wb(t, b, j.ID, "poll", workbenchInput{Presence: map[string]string{own.ID: ""}, TargetNames: map[string]string{own.ID: "不能替其他成员改名"}})
	view := workbenchDecode[WorkbenchView](wb(t, a, s.record.ID, "view", workbenchInput{}))
	for _, target := range view.Targets {
		if target.ID == own.ID && target.Name != own.Name || target.ID == member.ID && target.Name != stored.Name {
			t.Fatal(view.Targets)
		}
	}
	// Register may rename, but a changed underlying session cannot reuse the ID.
	_, err = b.workbenchCall(ctx, j.ID, "register", workbenchInput{TargetID: member.ID, Name: "spoof", Provider: member.Provider, Session: strings.Repeat("f", 24)})
	if err == nil {
		t.Fatal("rename accepted a different session identity")
	}
	wb(t, a, s.record.ID, "remove", workbenchInput{TargetID: member.ID})
	b.pumpWorkbench(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.record.Workbench.Targets[member.ID].Removed {
		t.Fatal("name sync revived removed association")
	}
}
