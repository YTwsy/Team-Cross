package collab

import (
	"context"
	"encoding/json"
	"path/filepath"
)

// Inherit the native thread's confirmed settings, falling back to Codex's own
// configuration only when the source has no persisted value.
func inheritModel(params map[string]any, source Source) {
	if source.Model != "" {
		params["model"] = source.Model
	}
	if source.ModelProvider != "" {
		params["modelProvider"] = source.ModelProvider
	}
	if source.ReasoningEffort != nil {
		params["config"] = map[string]any{"model_reasoning_effort": *source.ReasoningEffort}
	}
}

func (s *Session) modelLocked(model, provider string, effort *string) {
	if model == "" {
		return
	}
	s.record.Model = model
	s.record.ModelProvider = provider
	s.record.ReasoningEffort = effort
}

// Read native-confirmed state without starting or resuming a turn. Completion
// refreshes only the workspace, so it cannot overwrite a newer model notice.
func (s *Session) refreshNativeState(ctx context.Context, runtime Runtime, refreshModel bool) {
	s.nativeStateMu.Lock()
	defer s.nativeStateMu.Unlock()
	s.mu.Lock()
	if s.closed || s.record.ExecutionRecord == nil || s.process != runtime || s.record.SessionID == "" {
		s.mu.Unlock()
		return
	}
	id, generation := s.record.SessionID, s.generation
	s.mu.Unlock()
	var read struct {
		Thread Source `json:"thread"`
	}
	if runtime.Call(ctx, "thread/read", map[string]any{"threadId": id, "includeTurns": false}, &read) != nil || read.Thread.ID != id {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.process != runtime || s.generation != generation || s.record.SessionID != id {
		return
	}
	if refreshModel {
		s.modelLocked(read.Thread.Model, read.Thread.ModelProvider, read.Thread.ReasoningEffort)
	}
	s.nativeWorkspaceLocked(read.Thread)
	_ = s.saveLocked()
}

func (s *Session) nativeWorkspaceLocked(thread Source) {
	// Claude's workspace belongs to its native job contract. A transcript cwd
	// must not silently rebind that job. Empty/relative paths are not confirmed
	// host directories; preserve the last known directory in those cases.
	if s.record.Provider != "claude" && thread.ID == s.record.SessionID && filepath.IsAbs(thread.Cwd) {
		if cwd := filepath.Clean(thread.Cwd); cwd != s.record.ExecutionCwd {
			s.record.ExecutionCwd = cwd
			// The existing context views reload on sequence changes. A native
			// read can finish after the completion event has already rendered.
			s.sequence++
		}
	}
}

func (s *Session) observeModelLocked(method string, params json.RawMessage) {
	if method != "thread/settings/updated" {
		return
	}
	var update struct {
		ThreadID string `json:"threadId"`
		Settings struct {
			Model    string  `json:"model"`
			Provider string  `json:"modelProvider"`
			Effort   *string `json:"effort"`
		} `json:"threadSettings"`
	}
	if json.Unmarshal(params, &update) != nil || update.ThreadID != s.record.SessionID {
		return
	}
	s.modelLocked(update.Settings.Model, update.Settings.Provider, update.Settings.Effort)
	_ = s.saveLocked()
}
