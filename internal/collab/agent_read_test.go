package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"

	"teamcross/internal/materialstore"
	"teamcross/internal/readview"
)

func TestAgentViewsKeepQuestionsAndExplicitFinalsWithoutLoadingTools(t *testing.T) {
	raw := json.RawMessage(`{"id":"turn","status":"completed","items":[{"id":"q","type":"userMessage","content":[{"type":"text","text":"question"}]},{"id":"c","type":"agentMessage","phase":"commentary","text":"progress"},{"id":"steer","type":"userMessage","content":[{"type":"text","text":"user correction"}]},{"id":"tool","type":"commandExecution","command":"go test ./...","aggregatedOutput":"large log"},{"id":"f","type":"agentMessage","phase":"final_answer","text":"final"},{"id":"r","type":"reasoning","text":"private"}]}`)
	turn, err := exportTurn(raw)
	if err != nil {
		t.Fatal(err)
	}
	manifest := transientHistoryManifest([]MaterialTurn{turn})
	reads := []string{}
	reader := func(item materialstore.Item) (string, error) {
		reads = append(reads, item.ID)
		return item.Body.Text, nil
	}
	for _, tc := range []struct {
		view  string
		want  string
		reads int
	}{{"answers", "q,steer,f", 3}, {"conversation", "q,c,steer,f", 4}, {"outline", "", 0}, {"items", "", 0}} {
		t.Run(tc.view, func(t *testing.T) {
			reads = nil
			page, err := readAgentPage(reader, MaterialVersion{}, manifest, materialCursor{Scope: "stream"}, readview.Options{View: tc.view}, false, nil)
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{}
			for _, s := range page.Segments {
				ids = append(ids, s.ItemID)
				if s.StartOffset != 0 {
					t.Fatal(s)
				}
			}
			if strings.Join(ids, ",") != tc.want || len(reads) != tc.reads || page.Turns[0].AnswerStatus != "available" {
				t.Fatal(page, reads)
			}
			if tc.view == "items" && (len(page.Items) != 5 || page.Items[3].Label != "go test ./..." || page.Items[4].Phase != "final_answer") {
				t.Fatal(page.Items)
			}
		})
	}
	// The phase is part of the frozen manifest and survives a store round trip.
	bundle, _, _, err := buildMaterialBundle(MaterialContent{Title: "test", Provider: "codex", SourceID: "00000000-0000-4000-8000-000000000001", StartTurnID: "turn", EndTurnID: "turn", Turns: []MaterialTurn{turn}})
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Manifest.Turns[0].Items[4].Phase != "final_answer" {
		t.Fatal(bundle.Manifest)
	}
}

func TestAgentPagingBudgetsWholeMCPResponseAndPreservesUTF16(t *testing.T) {
	body := strings.Repeat("中文🙂\"\\\n\x00", 5000)
	manifest := transientHistoryManifest([]MaterialTurn{{ID: "turn", Status: "completed", Items: []MaterialItem{{ID: "tool", Type: "toolResult", Text: body}, {ID: "later", Type: "agentMessage", Phase: "final_answer", Text: "must not leak"}}}})
	reader := func(item materialstore.Item) (string, error) { return item.Body.Text, nil }
	cursor := materialCursor{Scope: "item", MaterialID: "material", Version: 1, Hash: strings.Repeat("a", 64)}
	options := readview.Options{View: "answers", MaxBytes: 8192}
	var got strings.Builder
	offset, pages := 0, 0
	for {
		page, err := readAgentPage(reader, MaterialVersion{Version: 1, Hash: cursor.Hash}, manifest, cursor, options, true, nil)
		if err != nil {
			t.Fatal(err)
		}
		if readview.WireSize(page) > 8192 || len(page.Segments) != 1 || page.Segments[0].ItemID != "tool" {
			t.Fatal("oversized or crossed item", readview.WireSize(page))
		}
		s := page.Segments[0]
		if s.StartOffset != offset || s.EndOffset <= offset || s.EndOffset-offset != len(utf16.Encode([]rune(s.Text))) {
			t.Fatal("invalid coordinates", s)
		}
		offset = s.EndOffset
		got.WriteString(s.Text)
		pages++
		if page.NextCursor == "" {
			if !page.ItemComplete {
				t.Fatal(page)
			}
			break
		}
		if pages > 200 {
			t.Fatal("pagination did not advance")
		}
		cursor, err = decodeMaterialCursor(page.NextCursor)
		if err != nil {
			t.Fatal(err)
		}
		options = readview.Options{}
	}
	if got.String() != body || pages < 2 {
		t.Fatal("lost or repeated source content", pages)
	}
	t.Logf("escaped UTF-16 payload: %d pages, every MCP response <= 8192 bytes", pages)
}

func TestAgentOutlineVisitsRecentWindowsAndUnknownFinals(t *testing.T) {
	turns := []MaterialTurn{}
	for i := 0; i < 8; i++ {
		turns = append(turns, MaterialTurn{ID: fmt.Sprint(i), Status: "completed", Items: []MaterialItem{{ID: "unmarked", Type: "agentMessage", Text: "not automatically a final"}}})
	}
	turns[7].Status = "inProgress"
	manifest := transientHistoryManifest(turns)
	reader := func(materialstore.Item) (string, error) { t.Fatal("outline loaded a body"); return "", nil }
	cursor, options := materialCursor{Scope: "stream"}, readview.Options{View: "outline"}
	ids := []string{}
	for n := 0; n < 4; n++ {
		page, err := readAgentPage(reader, MaterialVersion{}, manifest, cursor, options, false, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, turn := range page.Turns {
			ids = append(ids, turn.ID)
			if turn.ID == "7" {
				if turn.Status != "inProgress" || turn.AnswerStatus != "pending" {
					t.Fatal(turn)
				}
			} else if turn.AnswerStatus != "unidentified" {
				t.Fatal(turn)
			}
		}
		if page.NextCursor == "" {
			break
		}
		cursor, err = decodeMaterialCursor(page.NextCursor)
		if err != nil {
			t.Fatal(err)
		}
		options = readview.Options{}
	}
	if strings.Join(ids, ",") != "5,6,7,2,3,4,0,1" {
		t.Fatal(ids)
	}
	page, err := readAgentPage(reader, MaterialVersion{}, manifest, materialCursor{Scope: "stream", Turn: 3}, readview.Options{View: "outline"}, true, nil)
	if err != nil || len(page.Turns) != 1 || page.Turns[0].ID != "3" || page.NextCursor != "" {
		t.Fatal(page, err)
	}
}

func TestAgentHistorySupportsFinalViewAndExactItemExpansion(t *testing.T) {
	a, runtime, _ := fixture(t)
	s := createFixture(t, a, runtime, "existing")
	runtime.mu.Lock()
	runtime.history = []MaterialTurn{{ID: "t", Status: "inProgress", Items: []MaterialItem{{ID: "comment", Type: "agentMessage", Phase: "commentary", Text: "working"}, {ID: "tool", Type: "toolResult", Text: strings.Repeat("tool", 8000)}}}}
	runtime.mu.Unlock()
	value, err := s.ContextHistory(context.Background(), "history", "", 0, HistoryRead{Options: readview.Options{View: "answers", MaxBytes: 8192}})
	if err != nil {
		t.Fatal(err)
	}
	page := value.(historyReadResponse)
	if len(page.Segments) != 0 || page.Turns[0].AnswerStatus != "pending" || page.PageCursor == "" {
		t.Fatal(page)
	}
	value, err = s.ContextHistory(context.Background(), "history", "", 0, HistoryRead{Cursor: page.PageCursor, TurnID: "t", ItemID: "tool", Options: readview.Options{View: "answers", MaxBytes: 8192}})
	if err != nil {
		t.Fatal(err)
	}
	item := value.(historyReadResponse)
	if len(item.Segments) != 1 || item.Segments[0].ItemID != "tool" || readview.WireSize(item) > 8192 || item.NextCursor == "" {
		t.Fatal(item)
	}
	value, err = s.ContextHistory(context.Background(), "history", "", 0, HistoryRead{Cursor: item.NextCursor})
	if err != nil || value.(historyReadResponse).Segments[0].StartOffset != item.Segments[0].EndOffset {
		t.Fatal(value, err)
	}
	runtime.mu.Lock()
	runtime.history = []MaterialTurn{}
	runtime.mu.Unlock()
	value, err = s.ContextHistory(context.Background(), "history", "", 0, HistoryRead{Options: readview.Options{View: "answers", MaxBytes: 8192}})
	if err != nil {
		t.Fatal(err)
	}
	empty := value.(historyReadResponse)
	if empty.View != "answers" || len(empty.Thread) != 1 || !empty.SourcePageComplete || len(empty.Segments) != 0 || readview.WireSize(empty) > 8192 {
		t.Fatal(empty)
	}
}
