package mcp

import (
	"context"
	"testing"
)

func TestManagementRejectsAmbiguousInputsBeforeCallingCore(t *testing.T) {
	for _, row := range []struct {
		name string
		args map[string]any
	}{
		{"list_source_sessions", map[string]any{}},
		{"list_source_sessions", map[string]any{"provider": "any"}},
		{"create_collaboration", map[string]any{"provider": "codex", "sourceId": "latest", "workspaceMode": "existing", "requestId": "new", "previewHash": "none"}},
		{"create_invitation", map[string]any{"id": "fixture"}},
		{"create_invitation", map[string]any{"id": "fixture", "transport": "auto"}},
		{"open_client", map[string]any{"id": "fixture", "client": "tui", "launch": "false"}},
		{"end_sharing", map[string]any{"id": "../other"}},
		{"join_collaboration", map[string]any{"invitation": "fixture", "provider": "codex"}},
	} {
		if _, err := (Backend{}).Invoke(context.Background(), row.name, row.args); err == nil {
			t.Fatal(row)
		}
	}
}
