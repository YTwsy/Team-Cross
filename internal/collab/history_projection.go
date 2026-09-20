package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"

	"teamcross/internal/materialstore"
	"teamcross/internal/nativeclaude"
)

// HistoryRead keeps the provider's page cursor separate from the segment
// cursor used inside that page. A pageCursor returned to clients identifies
// the exact eight-turn source page needed for later item reads.
type HistoryRead struct {
	Cursor      string
	TurnID      string
	ItemID      string
	StartOffset int
}

type historyReadResponse struct {
	Thread                 map[string]any    `json:"thread"`
	ContentHash            string            `json:"contentHash"`
	PageCursor             string            `json:"pageCursor"`
	NextCursor             string            `json:"nextCursor"`
	Scope                  string            `json:"scope"`
	ItemComplete           bool              `json:"itemComplete,omitempty"`
	SourcePageComplete     bool              `json:"sourcePageComplete,omitempty"`
	PageEndsAtTurnBoundary bool              `json:"pageEndsAtTurnBoundary"`
	Turns                  []materialOutline `json:"turns,omitempty"`
	Segments               []materialSegment `json:"segments"`
}

func historyThreadMetadata(thread map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"id", "name", "preview", "cwd", "updatedAt", "model", "modelProvider", "reasoningEffort", "status"} {
		if value, ok := thread[key]; ok {
			out[key] = value
		}
	}
	return out
}

func exportedHistoryTurns(raw []json.RawMessage) ([]MaterialTurn, error) {
	turns := make([]MaterialTurn, 0, len(raw))
	for _, encoded := range raw {
		turn, err := exportTurn(encoded)
		if err != nil {
			return nil, err
		}
		turns = append(turns, turn)
	}
	return turns, nil
}

func (s *Session) loadHistoryPage(ctx context.Context, record Record, process Runtime, sourceCursor string) (map[string]any, []MaterialTurn, string, error) {
	if record.Provider == "claude" {
		runtimeDir := filepath.Join(s.app.Config.DataDir, "collaborations", record.ID, "claude-runtime")
		history, err := nativeclaude.SavedHistory(record.ProviderHome, runtimeDir, record.SessionID, record.ExecutionCwd)
		if err != nil {
			return nil, nil, "", err
		}
		page, next, err := history.Page(sourceCursor, 8)
		if err != nil {
			return nil, nil, "", err
		}
		slices.Reverse(page)
		raw := make([]json.RawMessage, 0, len(page))
		for _, turn := range page {
			encoded, marshalErr := json.Marshal(turn)
			if marshalErr != nil {
				return nil, nil, "", marshalErr
			}
			raw = append(raw, encoded)
		}
		metadata := map[string]any{
			"id": history.ID, "name": history.Name, "preview": history.Preview,
			"cwd": history.Cwd, "updatedAt": history.UpdatedAt, "model": history.Model,
			"reasoningEffort": history.ReasoningEffort,
		}
		turns, err := exportedHistoryTurns(raw)
		return historyThreadMetadata(metadata), turns, next, err
	}

	call := s.app.readerCall
	if process != nil {
		call = process.Call
	}
	var read struct {
		Thread map[string]any `json:"thread"`
	}
	if err := call(ctx, "thread/read", map[string]any{"threadId": record.SessionID, "includeTurns": false}, &read); err != nil {
		return nil, nil, "", err
	}
	params := map[string]any{"threadId": record.SessionID, "limit": 8, "itemsView": "full", "sortDirection": "desc"}
	if sourceCursor != "" {
		params["cursor"] = sourceCursor
	}
	var page struct {
		Data       []json.RawMessage `json:"data"`
		NextCursor *string           `json:"nextCursor"`
	}
	if err := call(ctx, "thread/turns/list", params, &page); err != nil {
		return nil, nil, "", err
	}
	slices.Reverse(page.Data)
	turns, err := exportedHistoryTurns(page.Data)
	next := ""
	if page.NextCursor != nil {
		next = *page.NextCursor
	}
	return historyThreadMetadata(read.Thread), turns, next, err
}

func transientHistoryManifest(turns []MaterialTurn) materialstore.Manifest {
	manifest := materialstore.Manifest{Schema: materialstore.ManifestSchema}
	for _, sourceTurn := range turns {
		turn := materialstore.Turn{ID: sourceTurn.ID, Status: sourceTurn.Status, Label: materialTurnLabel(sourceTurn)}
		for _, sourceItem := range sourceTurn.Items {
			text, sourceLength, omitted := truncateMaterialText(sourceItem.Text)
			notice := sourceItem.Notice
			if omitted > 0 {
				notice = appendMaterialNotice(notice, fmt.Sprintf("原始内容过长，读取时省略 %d 个 UTF-16 字符；保留头尾", omitted))
			}
			turn.Items = append(turn.Items, materialstore.Item{
				ID: sourceItem.ID, Type: sourceItem.Type, Notice: notice,
				Body: materialstore.InlineBody(text), SourceUTF16Length: sourceLength, OmittedUTF16Length: omitted,
			})
		}
		manifest.Turns = append(manifest.Turns, turn)
	}
	return manifest
}

func historyPageHash(sessionID, sourceCursor string, turns []MaterialTurn) string {
	return contentHash(struct {
		Domain       string         `json:"domain"`
		SessionID    string         `json:"sessionId"`
		SourceCursor string         `json:"sourceCursor"`
		Turns        []MaterialTurn `json:"turns"`
	}{Domain: "teamcross/history-page/v1", SessionID: sessionID, SourceCursor: sourceCursor, Turns: turns})
}

func (s *Session) readHistory(ctx context.Context, record Record, process Runtime, in HistoryRead) (historyReadResponse, error) {
	cursor := materialCursor{Scope: "stream", MaterialID: record.SessionID}
	if in.Cursor != "" {
		decoded, err := decodeMaterialCursor(in.Cursor)
		if err != nil || decoded.MaterialID != record.SessionID || decoded.Version != 0 || (decoded.Scope != "stream" && decoded.Scope != "item") {
			return historyReadResponse{}, fmt.Errorf("历史分页位置不属于当前协作会话")
		}
		cursor = decoded
	}
	thread, turns, olderCursor, err := s.loadHistoryPage(ctx, record, process, cursor.SourceCursor)
	if err != nil {
		return historyReadResponse{}, err
	}
	pageHash := historyPageHash(record.SessionID, cursor.SourceCursor, turns)
	if cursor.Hash != "" && cursor.Hash != pageHash {
		return historyReadResponse{}, fmt.Errorf("历史页面已变化，请从最近对话重新读取")
	}
	cursor.Hash = pageHash
	manifest := transientHistoryManifest(turns)
	base := materialCursor{Scope: "stream", MaterialID: record.SessionID, Hash: pageHash, SourceCursor: cursor.SourceCursor}
	pageCursor := encodeMaterialCursor(base)
	response := historyReadResponse{Thread: thread, ContentHash: pageHash, PageCursor: pageCursor, Scope: cursor.Scope, SourcePageComplete: len(manifest.Turns) == 0, PageEndsAtTurnBoundary: true}
	if len(manifest.Turns) == 0 {
		if olderCursor != "" {
			base.SourceCursor, base.Hash = olderCursor, ""
			response.NextCursor = encodeMaterialCursor(base)
		}
		return response, nil
	}

	if in.ItemID != "" {
		if in.TurnID == "" || in.StartOffset < 0 || cursor.Scope == "item" {
			return historyReadResponse{}, fmt.Errorf("按条读取需要 pageCursor、turnId、itemId 和有效的 startOffset")
		}
		cursor.Scope = "item"
		cursor.Turn = slices.IndexFunc(manifest.Turns, func(turn materialstore.Turn) bool { return turn.ID == in.TurnID })
		if cursor.Turn >= 0 {
			cursor.Item = slices.IndexFunc(manifest.Turns[cursor.Turn].Items, func(item materialstore.Item) bool { return item.ID == in.ItemID })
		}
		cursor.Offset = in.StartOffset
		if cursor.Turn < 0 || cursor.Item < 0 {
			return historyReadResponse{}, fmt.Errorf("当前历史页面中没有该消息")
		}
	} else if in.TurnID != "" {
		if in.StartOffset != 0 || cursor.Scope == "item" {
			return historyReadResponse{}, fmt.Errorf("历史流定位参数无效")
		}
		cursor.Turn = slices.IndexFunc(manifest.Turns, func(turn materialstore.Turn) bool { return turn.ID == in.TurnID })
		cursor.Item, cursor.Offset = 0, 0
		if cursor.Turn < 0 {
			return historyReadResponse{}, fmt.Errorf("定位不在当前历史页面")
		}
	} else if in.StartOffset != 0 {
		return historyReadResponse{}, fmt.Errorf("startOffset 只能用于按条读取")
	}

	readBody := materialBodyReader(func(item materialstore.Item) (string, error) { return item.Body.Text, nil })
	version := MaterialVersion{Version: 0, Hash: pageHash, Title: "协作上下文", Provider: record.Provider, SourceID: record.SessionID, TurnCount: len(manifest.Turns)}
	var projected materialReadResponse
	if cursor.Scope == "item" {
		projected, err = readMaterialItemWith(readBody, version, manifest, cursor)
	} else {
		projected, err = readMaterialStreamWith(readBody, version, manifest, cursor, true)
	}
	if err != nil {
		return historyReadResponse{}, err
	}
	for index := range projected.Segments {
		if projected.Segments[index].Collapsed {
			projected.Segments[index].ReadHint = "传入本响应的 pageCursor、turnId、itemId，并从 startOffset=0 按条分页读取"
		}
	}
	response.Scope = projected.Scope
	response.ItemComplete = projected.ItemComplete
	response.PageEndsAtTurnBoundary = projected.PageEndsAtTurnBoundary
	response.SourcePageComplete = projected.Scope == "stream" && projected.NextCursor == ""
	response.Turns = projected.Turns
	response.Segments = projected.Segments
	response.NextCursor = projected.NextCursor
	if projected.Scope == "stream" && response.NextCursor == "" && olderCursor != "" {
		next := materialCursor{Scope: "stream", MaterialID: record.SessionID, SourceCursor: olderCursor}
		response.NextCursor = encodeMaterialCursor(next)
	}
	return response, nil
}
