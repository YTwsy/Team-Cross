package collab

import (
	"context"
	"encoding/json"
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

// Read configuration confirmed by Codex rather than displaying a requested
// model that may have been rejected. This does not start or resume a turn.
func (s *Session) refreshModel(ctx context.Context, runtime Runtime) {
	s.mu.Lock()
	id := s.record.SessionID
	s.mu.Unlock()
	if id == "" {
		return
	}
	var read struct {
		Thread Source `json:"thread"`
	}
	if runtime.Call(ctx, "thread/read", map[string]any{"threadId": id, "includeTurns": false}, &read) != nil || read.Thread.ID != id {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.process != runtime {
		return
	}
	s.modelLocked(read.Thread.Model, read.Thread.ModelProvider, read.Thread.ReasoningEffort)
	_ = s.saveLocked()
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
