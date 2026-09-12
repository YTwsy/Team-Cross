package nativeclaude

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestForkMaterializesNativeHistoryWithNewLineage(t *testing.T) {
	home := t.TempDir()
	source, id := uuid.NewString(), uuid.NewString()
	path := historyFixture(t, home, source)
	appendRecord(t, path, `{"type":"progress","uuid":"p1","parentUuid":"a1"}`)
	appendRecord(t, path, `{"type":"user","uuid":"u2","parentUuid":"p1","logicalParentUuid":"a1","teamName":"old","slug":"old","message":{"content":"next question"}}`)
	appendRecord(t, path, `{"type":"assistant","uuid":"a2","parentUuid":"u2","message":{"model":"fixture-model","content":[{"type":"text","text":"next answer"}],"stop_reason":"end_turn"}}`)
	appendRecord(t, path, `{"type":"user","uuid":"side","isSidechain":true,"message":{"content":"EXCLUDED_SIDECHAIN"}}`)
	appendRecord(t, path, `{"type":"attachment","uuid":"attach","parentUuid":"a2","attachment":{"type":"deferred_tools_record","nameOnlyAnnouncements":["a1","side",42]}}`)
	appendRecord(t, path, `{"type":"system","subtype":"model_refusal_fallback","uuid":"sys","parentUuid":"attach"}`)
	appendRecord(t, path, `{"type":"custom-title","uuid":"meta","customTitle":"Source title"}`)
	appendRecord(t, path, `{"type":"history-suppression","cause":"source"}`)
	appendRecord(t, path, `{"type":"atis-latch","sessionId":"`+source+`","atis":"valid"}`)
	appendRecord(t, path, `{"type":"content-replacement","sessionId":"`+source+`","replacements":[{"value":9007199254740993}]}`)
	h, err := ReadFile(path)
	if err != nil || len(h.Turns) != 2 {
		t.Fatal("metadata changed conversation leaf", h, err)
	}
	before, _ := os.ReadFile(path)
	c := Config{Home: home, Cwd: "/chosen/worktree"}
	if err = materializeFork(c, h, id, "Fork title"); err != nil {
		t.Fatal(err)
	}
	fork, err := Read(home, id)
	if err != nil || len(fork.Turns) != 2 || fork.Name != "Fork title" || fork.Cwd != c.Cwd || fork.Turns[0].ID == "u1" {
		t.Fatal(fork, err)
	}
	data, _ := os.ReadFile(fork.Path)
	if bytes.Contains(data, []byte("EXCLUDED_SIDECHAIN")) || bytes.Contains(data, []byte(`"type":"progress"`)) || !bytes.Contains(data, []byte("9007199254740993")) {
		t.Fatal("incorrect exclusion or lost numeric precision")
	}
	bySource := map[string]map[string]any{}
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var r map[string]any
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatal(err)
		}
		if lineage, ok := r["forkedFrom"].(map[string]any); ok {
			if lineage["sessionId"] != source || r["sessionId"] != id || r["cwd"] != c.Cwd {
				t.Fatal("lost fork lineage or selected cwd", r)
			}
			bySource[recordString(lineage, "messageUuid")] = r
		}
	}
	if bySource["u2"]["parentUuid"] != bySource["a1"]["uuid"] || bySource["u2"]["logicalParentUuid"] != bySource["a1"]["uuid"] || bySource["u2"]["teamName"] != nil || bySource["sys"]["neutralizedByFork"] != true {
		t.Fatal("parent/progress/metadata remapping failed", bySource)
	}
	names := bySource["attach"]["attachment"].(map[string]any)["nameOnlyAnnouncements"].([]any)
	if len(names) != 1 || names[0] != bySource["a1"]["uuid"] {
		t.Fatal("attachment references not remapped", names)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("source snapshot modified")
	}
	if err = materializeFork(c, h, id, "overwrite"); err == nil {
		t.Fatal("overwrote existing native transcript")
	}
	if _, err = forkTranscript([]byte(`{"type":`), source, id, c.Cwd, "title"); err == nil {
		t.Fatal("forked incomplete source")
	}
}

func TestNativeProjectKey(t *testing.T) {
	if got := projectKey("/Users/a/中文/😀"); got != "-Users-a------" {
		t.Fatal(got)
	}
	// Hash and truncation independently computed with the native JS algorithm.
	if got := projectKey("/" + strings.Repeat("x", 220)); got != "-"+strings.Repeat("x", 199)+"-7q0khb" {
		t.Fatal(got)
	}
}
