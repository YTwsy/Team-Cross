package nativecodex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSourceTurnUsesPersistedBoundaries(t *testing.T) {
	start := "{\"type\":\"session_meta\",\"payload\":{\"id\":\"source\"}}\n{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\",\"turn_id\":\"turn-1\"}}\n"
	complete := "{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\",\"turn_id\":\"turn-1\"}}\n"
	for _, row := range []struct{ name, text, id, status string }{
		{"active", start, "turn-1", "inProgress"},
		{"complete", start + complete, "turn-1", "completed"},
		{"next-turn", start + complete + "{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\",\"turn_id\":\"turn-2\"}}\n", "turn-2", "inProgress"},
		{"aborted", start + "{\"type\":\"event_msg\",\"payload\":{\"type\":\"turn_aborted\",\"turn_id\":\"turn-1\"}}\n", "turn-1", "interrupted"},
		{"inflight-tail", start + complete + `{"type":`, "turn-1", "inProgress"},
		{"other-turn-completion", start + "{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\",\"turn_id\":\"other\"}}\n", "turn-1", "inProgress"},
		{"missing-identity", complete, "", ""},
		{"corrupt-middle", start + "{broken\n" + complete, "", ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rollout.jsonl")
			if err := os.WriteFile(path, []byte(row.text), 0600); err != nil {
				t.Fatal(err)
			}
			id, status, err := SourceTurn(path, "source")
			if row.id == "" {
				if err == nil {
					t.Fatal("accepted invalid history", id, status)
				}
				return
			}
			if err != nil || id != row.id || status != row.status {
				t.Fatal(id, status, err)
			}
			if _, _, err := SourceTurn(path, "wrong-session"); err == nil {
				t.Fatal("accepted wrong session")
			}
		})
	}
}
