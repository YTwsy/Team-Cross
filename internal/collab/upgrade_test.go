package collab

import (
	"slices"
	"testing"
	"time"
)

func TestUpgradeProtectsInvitationsReceiversAndDeliveries(t *testing.T) {
	a := &App{}
	if a.UpgradeBusy() {
		t.Fatal("empty app is busy")
	}
	a.pending = map[string]pendingInvite{"test": {Expires: time.Now().Add(time.Minute)}}
	if !a.UpgradeBusy() {
		t.Fatal("pending invitation lost")
	}
	a.pending = nil
	a.agentReceivers = map[string]*agentReceiver{"test": {lastSeen: time.Now()}}
	if !a.UpgradeBusy() {
		t.Fatal("receiver lost")
	}
	a.agentReceivers["test"].lastSeen = time.Now().Add(-time.Minute)
	if a.UpgradeBusy() {
		t.Fatal("expired receiver blocks forever")
	}
	for _, state := range []string{"submitting", "submitted", "received"} {
		a.agents.Requests = map[string]AgentRequest{"test": {State: state}}
		if !a.UpgradeBusy() {
			t.Fatal("lost request", state)
		}
	}
	a.agents.Requests = map[string]AgentRequest{"test": {State: "completed"}}
	if a.UpgradeBusy() {
		t.Fatal("completed work blocks upgrade")
	}
}

func TestUnknownReceiptSurvivesUpgradeWithoutBlockingOrReplaying(t *testing.T) {
	a, _, _ := spaceFixture(t)
	unknown := AgentRequest{ID: "unknown-receipt", State: "unknown", Instruction: "Do not resend", CreatedAt: time.Now().Add(-30 * 24 * time.Hour)}
	a.agentMu.Lock()
	next := libraryClone(a.agents)
	next.Requests[unknown.ID] = unknown
	err := a.saveAgentsLocked(next)
	a.agentMu.Unlock()
	if err != nil || a.UpgradeBusy() {
		t.Fatal("persisted unknown receipt blocks upgrade", err)
	}
	config := a.Config
	a.Close()
	restored, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if restored.UpgradeBusy() || restored.agents.Requests[unknown.ID].State != "unknown" || restored.agents.Requests[unknown.ID].Instruction != unknown.Instruction {
		t.Fatal("upgrade changed or replayed the unknown receipt")
	}
	if len(restored.agentProxies) != 0 || len(restored.agentReceivers) != 0 {
		t.Fatal("restoring receipt started a delivery")
	}
}

func TestUpgradeUsesEffectiveClientSettings(t *testing.T) {
	a := &App{Config: Config{Binary: "/old-cli"}, settings: Settings{Binary: "", ClaudeBinary: "/current-claude", DesktopApp: "/current.app"}}
	if !slices.Equal(a.ClientLaunchArgs(), []string{"--codex-bin", "", "--claude-bin", "/current-claude", "--desktop-app", "/current.app"}) {
		t.Fatal("reintroduced obsolete launch overrides", a.ClientLaunchArgs())
	}
}
