package collab

import (
	"context"
	"strings"
	"testing"
)

func TestHistoryProjectionFoldsToolsStripsReasoningAndReadsOneItem(t *testing.T) {
	a, runtime, _ := fixture(t)
	s := createFixture(t, a, runtime, "existing")
	runtime.mu.Lock()
	runtime.history = []MaterialTurn{{
		ID: "history-turn", Status: "completed",
		Items: []MaterialItem{
			{ID: "answer", Type: "agentMessage", Text: "可见回答"},
			{ID: "reasoning", Type: "reasoning", Text: "private-chain-of-thought"},
			{ID: "tool", Type: "toolResult", Text: strings.Repeat("tool-output-", 3000)},
			{ID: "after", Type: "agentMessage", Text: "工具后的回答"},
		},
	}}
	runtime.mu.Unlock()

	value, err := s.Context(context.Background(), "history", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	page, ok := value.(historyReadResponse)
	if !ok {
		t.Fatalf("unexpected history response %T", value)
	}
	if page.PageCursor == "" || page.Scope != "stream" || len(page.Turns) != 1 || len(page.Segments) != 3 {
		t.Fatal("history did not use the structured projection", page)
	}
	if page.Segments[0].Text != "可见回答" || page.Segments[1].ItemID != "tool" || !page.Segments[1].Collapsed || page.Segments[1].Length <= materialPreviewLimit {
		t.Fatal("history conversation/tool projection is incorrect", page.Segments)
	}
	for _, segment := range page.Segments {
		if strings.Contains(segment.Text, "private-chain-of-thought") || segment.ItemID == "reasoning" {
			t.Fatal("reasoning escaped through live history", segment)
		}
	}

	value, err = s.ContextHistory(context.Background(), "history", "", 0, HistoryRead{
		Cursor: page.PageCursor, TurnID: "history-turn", ItemID: "tool", StartOffset: page.Segments[1].EndOffset,
	})
	if err != nil {
		t.Fatal(err)
	}
	item := value.(historyReadResponse)
	if item.Scope != "item" || len(item.Segments) != 1 || item.Segments[0].ItemID != "tool" || item.Segments[0].StartOffset != page.Segments[1].EndOffset || item.NextCursor == "" {
		t.Fatal("history item reader did not stay on the selected tool output", item)
	}
	value, err = s.ContextHistory(context.Background(), "history", "", 0, HistoryRead{Cursor: item.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	continued := value.(historyReadResponse)
	if continued.Scope != "item" || len(continued.Segments) != 1 || continued.Segments[0].ItemID != "tool" {
		t.Fatal("history item cursor crossed into another message", continued)
	}
}
