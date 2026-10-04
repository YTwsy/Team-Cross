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
	for _, state := range []string{"submitting", "submitted", "received", "unknown"} {
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

func TestUpgradeUsesEffectiveClientSettings(t *testing.T) {
	a := &App{Config: Config{Binary: "/old-cli"}, settings: Settings{Binary: "", ClaudeBinary: "/current-claude", DesktopApp: "/current.app"}}
	if !slices.Equal(a.ClientLaunchArgs(), []string{"--codex-bin", "", "--claude-bin", "/current-claude", "--desktop-app", "/current.app"}) {
		t.Fatal("reintroduced obsolete launch overrides", a.ClientLaunchArgs())
	}
}
