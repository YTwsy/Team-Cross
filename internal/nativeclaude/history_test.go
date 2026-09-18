package nativeclaude

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func historyFixture(t *testing.T, home, id string) string {
	t.Helper()
	path := filepath.Join(home, "projects", "fixture", id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	records := []map[string]any{
		{"type": "user", "uuid": "u1", "cwd": "/fixture", "message": map[string]any{"content": "source question"}},
		{"type": "assistant", "uuid": "a1", "parentUuid": "u1", "effort": "high", "message": map[string]any{"model": "fixture-model", "content": []map[string]any{{"type": "text", "text": "answer"}}, "stop_reason": "end_turn"}},
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, r := range records {
		if err := json.NewEncoder(f).Encode(r); err != nil {
			t.Fatal(err)
		}
	}
	return path
}
func appendRecord(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = f.WriteString(line + "\n"); err != nil {
		t.Fatal(err)
	}
}
func TestHistoryFollowsMainBranchAndStableCursor(t *testing.T) {
	path := historyFixture(t, t.TempDir(), uuid.NewString())
	appendRecord(t, path, `{"type":"user","uuid":"discarded","parentUuid":"a1","message":{"content":"old branch"}}`)
	appendRecord(t, path, `{"type":"user","uuid":"u2","parentUuid":"a1","message":{"content":"current branch"}}`)
	appendRecord(t, path, `{"type":"assistant","uuid":"tool","parentUuid":"u2","message":{"content":[{"type":"tool_use","id":"tool-1","name":"Bash","input":{"command":"pwd"}}],"stop_reason":"tool_use"}}`)
	appendRecord(t, path, `{"type":"user","uuid":"result","parentUuid":"tool","message":{"content":[{"type":"tool_result","tool_use_id":"tool-1","content":"/fixture"}]}}`)
	appendRecord(t, path, `{"type":"assistant","uuid":"a2","parentUuid":"result","message":{"model":"fixture-new-model","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}}`)
	appendRecord(t, path, `{"type":"user","uuid":"side","isSidechain":true,"message":{"content":"PRIVATE_SIDECHAIN"}}`)
	appendRecord(t, path, `{"type":"attachment","attachment":{"private":"CREDENTIAL_SENTINEL"}}`)
	h, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Turns) != 2 || h.CompletedTurn() != "u2" || h.Model != "fixture-new-model" || h.ReasoningEffort != nil {
		t.Fatalf("unexpected history: %+v", h)
	}
	b, _ := json.Marshal(h)
	for _, forbidden := range []string{"old branch", "PRIVATE_SIDECHAIN", "CREDENTIAL_SENTINEL"} {
		if strings.Contains(string(b), forbidden) {
			t.Fatal("leaked excluded history", forbidden)
		}
	}
	page, next, err := h.Page("", 1)
	if err != nil || len(page) != 1 || page[0].ID != "u2" || next != "u2" {
		t.Fatal(page, next, err)
	}
	page, next, err = h.Page(next, 1)
	if err != nil || len(page) != 1 || page[0].ID != "u1" || next != "" {
		t.Fatal(page, next, err)
	}
	if _, _, err = h.Page("missing", 1); err == nil {
		t.Fatal("accepted stale cursor")
	}
}
func TestHistoryIncompleteAndCorruptRecords(t *testing.T) {
	path := historyFixture(t, t.TempDir(), uuid.NewString())
	appendRecord(t, path, `{"type":`)
	h, err := ReadFile(path)
	if err != nil || h.CompletedTurn() != "" {
		t.Fatal("inflight tail was presented as complete", err)
	}
	appendRecord(t, path, `{"type":"last-prompt"}`)
	if _, err = ReadFile(path); err == nil {
		t.Fatal("silently ignored corrupt history")
	}
	if _, err = Read(filepath.Dir(path), uuid.NewString()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if _, err = Read(t.TempDir(), "../../outside"); err == nil {
		t.Fatal("accepted path traversal")
	}
}

func TestNativeLocalCommandAndInterruptAreNotNewModelTurns(t *testing.T) {
	path := historyFixture(t, t.TempDir(), uuid.NewString())
	appendRecord(t, path, `{"type":"user","uuid":"cmd","parentUuid":"a1","message":{"content":"<command-name>/effort</command-name>"}}`)
	appendRecord(t, path, `{"type":"user","uuid":"stdout","parentUuid":"cmd","message":{"content":"<local-command-stdout>effort set</local-command-stdout>"}}`)
	h, err := ReadFile(path)
	if err != nil || len(h.Turns) != 1 || h.Turns[0].Status != "completed" {
		t.Fatal("local command invented an unfinished model turn", h, err)
	}
	appendRecord(t, path, `{"type":"user","uuid":"u2","parentUuid":"stdout","message":{"content":"long task"}}`)
	appendRecord(t, path, `{"type":"user","uuid":"interrupt","parentUuid":"u2","message":{"content":[{"type":"text","text":"[Request interrupted by user]"}]}}`)
	h, err = ReadFile(path)
	if err != nil || len(h.Turns) != 2 || h.Turns[1].Status != "interrupted" {
		t.Fatal("interrupt invented an unfinished model turn", h, err)
	}
}

func TestMissingForkNeverFallsBackToSource(t *testing.T) {
	home := t.TempDir()
	runtimeDir := t.TempDir()
	source, id := uuid.NewString(), uuid.NewString()
	historyFixture(t, home, source)
	p := Process{Config: Config{Home: home, RuntimeDir: runtimeDir, Cwd: "/fork"}, Job: Job{SessionID: id}, meta: marker{SourceID: source}}
	_, err := p.History()
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing fork silently reverted to source", err)
	}
	path := historyFixture(t, home, id)
	if _, err = p.History(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err = p.History(); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("lost fork silently reverted to source", err)
	}
}

func TestVisibleToolResultsArePreservedForPublication(t *testing.T) {
	path := historyFixture(t, t.TempDir(), uuid.NewString())
	appendRecord(t, path, `{"type":"user","uuid":"u2","parentUuid":"a1","message":{"content":"check logs"}}`)
	appendRecord(t, path, `{"type":"assistant","uuid":"a2","parentUuid":"u2","message":{"content":[{"type":"tool_use","id":"tool-1","name":"Bash","input":{"command":"cat log"}}],"stop_reason":"tool_use"}}`)
	appendRecord(t, path, `{"type":"user","uuid":"r2","parentUuid":"a2","message":{"content":[{"type":"tool_result","tool_use_id":"tool-1","content":[{"type":"text","text":"evidence-output"},{"type":"image","source":{"data":"private-image"}}]}]}}`)
	appendRecord(t, path, `{"type":"assistant","uuid":"a3","parentUuid":"r2","message":{"content":[{"type":"text","text":"finished"}],"stop_reason":"end_turn"}}`)
	h, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(h.Turns)
	if !strings.Contains(string(raw), "evidence-output") || !strings.Contains(string(raw), "工具输出附件未导出") || strings.Contains(string(raw), "private-image") {
		t.Fatal(string(raw))
	}
}
