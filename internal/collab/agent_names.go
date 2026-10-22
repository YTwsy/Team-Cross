package collab

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

func legacyAgentName(p AgentPairing) bool {
	return p.NameSource == "" && (p.Name == "当前会话" || strings.TrimSpace(p.Name) == "")
}

func customAgentName(p AgentPairing) bool {
	return p.NameSource == "custom" || (p.NameSource == "" && !legacyAgentName(p))
}

func defaultAgentName(provider, sessionID, title string) (string, string) {
	if title = strings.TrimSpace(title); title != "" {
		if len([]rune(title)) > 80 {
			title = shortText(title, 79)
		}
		return title, "native"
	}
	label := map[string]string{"codex": "Codex", "claude": "Claude", "chatgpt": "ChatGPT"}[provider]
	// Use the same opaque identity as the public target, never a private native
	// ID, working directory, or conversation preview as a substitute title.
	return label + " · " + contentHash([]string{provider, sessionID})[:8], "fallback"
}

func (a *App) agentPairingTitle(ctx context.Context, p AgentPairing) (string, bool) {
	if p.Provider == "chatgpt" {
		return "", true // The transport supplies identity but no conversation title.
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var read struct {
		Thread Source `json:"thread"`
	}
	params := map[string]any{"threadId": p.SessionID, "includeTurns": false}
	if p.Transport == "native" {
		s := a.agentSession(p)
		if s == nil {
			return "", false
		}
		raw, err := s.RPC(ctx, "owner", "thread/read", params, "")
		if err != nil || json.Unmarshal(raw, &read) != nil {
			return "", false
		}
	} else if err := a.sourceCall(ctx, p.Provider, "thread/read", params, &read); err != nil {
		return "", false
	}
	return read.Thread.Name, read.Thread.ID == p.SessionID
}

// Repair only the old default. The existing workbench poll mirrors the name to
// each active association; it never re-registers or revives a removed target.
func (a *App) refreshLegacyAgentNames(ctx context.Context, pairs map[string]AgentPairing) {
	// A slow/unavailable source must not delay the regular request poll once per
	// legacy pairing. Share one small budget across the entire repair pass.
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for id, p := range pairs {
		if !legacyAgentName(p) || len(p.Links) == 0 || (p.State != "paired" && p.State != "linked" && p.State != "verifying") || ctx.Err() != nil {
			continue
		}
		title, ok := a.agentPairingTitle(ctx, p)
		if !ok {
			continue
		}
		name, source := defaultAgentName(p.Provider, p.SessionID, title)
		a.agentMu.Lock()
		current, exists := a.agents.Pairings[id]
		if exists && legacyAgentName(current) && current.Provider == p.Provider && current.SessionID == p.SessionID {
			current.Name, current.NameSource = name, source
			next := libraryClone(a.agents)
			next.Pairings[id] = current
			if a.saveAgentsLocked(next) == nil {
				pairs[id] = current
			}
		}
		a.agentMu.Unlock()
	}
}
