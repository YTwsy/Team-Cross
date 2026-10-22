package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestInFlowHookConfigurationAndQuietInvalidInput(t *testing.T) {
	for _, provider := range []string{"codex", "claude"} {
		var out bytes.Buffer
		if err := runInFlowHook([]string{"--provider", provider, "--data-dir", "/a path/it's data", "--print-config"}, strings.NewReader(""), &out); err != nil {
			t.Fatal(err)
		}
		var config map[string]any
		if err := json.Unmarshal(out.Bytes(), &config); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "UserPromptSubmit") || !strings.Contains(out.String(), "--provider "+provider) {
			t.Fatal(out.String())
		}
		for _, input := range []string{`{}`, `{"session_id":"not-an-id","hook_event_name":"UserPromptSubmit"}`, `{"session_id":"e2855f66-9ccc-4775-bb8b-28b3bc357337","hook_event_name":"Stop"}`, `{"session_id":"e2855f66-9ccc-4775-bb8b-28b3bc357337","hook_event_name":"UserPromptSubmit"}`} {
			out.Reset()
			if err := runInFlowHook([]string{"--provider", provider, "--data-dir", t.TempDir()}, strings.NewReader(input), &out); err != nil {
				t.Fatal(err)
			}
			if out.Len() != 0 {
				t.Fatal(out.String())
			}
		}
	}
}
