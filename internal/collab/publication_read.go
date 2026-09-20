package collab

import (
	"fmt"
	"slices"

	"teamcross/internal/materialstore"
)

type publicationDraftReadResponse struct {
	DraftID                string            `json:"draftId"`
	Hash                   string            `json:"hash"`
	Title                  string            `json:"title"`
	Provider               string            `json:"provider"`
	SourceID               string            `json:"sourceId"`
	StartTurnID            string            `json:"startTurnId"`
	EndTurnID              string            `json:"endTurnId"`
	ReadingStartID         string            `json:"readingStartId,omitempty"`
	NextCursor             string            `json:"nextCursor"`
	Scope                  string            `json:"scope"`
	ItemComplete           bool              `json:"itemComplete,omitempty"`
	PageEndsAtTurnBoundary bool              `json:"pageEndsAtTurnBoundary"`
	Turns                  []materialOutline `json:"turns,omitempty"`
	Segments               []materialSegment `json:"segments"`
}

func publicationDraftSummary(draft PublicationDraft) map[string]any {
	turns := make([]map[string]any, 0, len(draft.Turns))
	for _, turn := range draft.Turns {
		turns = append(turns, map[string]any{"id": turn.ID, "status": turn.Status, "label": materialTurnLabel(turn), "itemCount": len(turn.Items)})
	}
	return map[string]any{
		"id": draft.ID, "title": draft.Title, "provider": draft.Provider,
		"sourceId": draft.SourceID, "startTurnId": draft.StartTurnID,
		"endTurnId": draft.EndTurnID, "readingStartId": draft.ReadingStartID,
		"frozenAt": draft.FrozenAt, "hash": draft.Hash, "turnCount": len(draft.Turns),
		"turns":    turns,
		"readHint": "正文未内联；使用 read_publication_draft 按流读取，长工具输出折叠后可按 turnId + itemId 展开",
	}
}

func draftCursor(manifest materialstore.Manifest, record publicationDraftRecord, in PublicationDraftRead) (materialCursor, error) {
	cursor := materialCursor{Scope: "stream", MaterialID: in.DraftID, Hash: record.Hash}
	if in.Cursor != "" {
		if in.StartOffset != 0 || in.ItemID != "" || in.TurnID != "" {
			return cursor, fmt.Errorf("游标不能与按条定位参数同时使用")
		}
		decoded, err := decodeMaterialCursor(in.Cursor)
		if err != nil || decoded.MaterialID != in.DraftID || decoded.Version != 0 || decoded.Hash != record.Hash || (decoded.Scope != "stream" && decoded.Scope != "item") {
			return cursor, fmt.Errorf("分页位置不属于这份本机预览")
		}
		return decoded, nil
	}
	if in.ItemID != "" {
		if in.TurnID == "" || in.StartOffset < 0 {
			return cursor, fmt.Errorf("按条读取需要 turnId、itemId 和有效的 startOffset")
		}
		cursor.Scope = "item"
		cursor.Turn = slices.IndexFunc(manifest.Turns, func(turn materialstore.Turn) bool { return turn.ID == in.TurnID })
		if cursor.Turn >= 0 {
			cursor.Item = slices.IndexFunc(manifest.Turns[cursor.Turn].Items, func(item materialstore.Item) bool { return item.ID == in.ItemID })
		}
		cursor.Offset = in.StartOffset
		if cursor.Turn < 0 || cursor.Item < 0 {
			return cursor, fmt.Errorf("本机预览中没有该消息")
		}
		return cursor, nil
	}
	if in.TurnID != "" {
		cursor.Turn = slices.IndexFunc(manifest.Turns, func(turn materialstore.Turn) bool { return turn.ID == in.TurnID })
		if cursor.Turn < 0 {
			return cursor, fmt.Errorf("定位不在本机预览范围内")
		}
	} else if in.StartOffset != 0 {
		return cursor, fmt.Errorf("startOffset 只能用于按条读取")
	}
	return cursor, nil
}

func (a *App) ReadPublicationDraft(in PublicationDraftRead) (publicationDraftReadResponse, error) {
	record, manifest, err := a.loadDraftRecord(in.DraftID)
	if err != nil {
		return publicationDraftReadResponse{}, err
	}
	cursor, err := draftCursor(manifest, record, in)
	if err != nil {
		return publicationDraftReadResponse{}, err
	}
	version := versionFromManifest(manifest, record.Hash, "", "", 0, nil)
	version.CreatedAt = record.FrozenAt
	reader := &Session{materialStore: a.draftStore()}
	var page materialReadResponse
	if cursor.Scope == "item" {
		page, err = reader.readMaterialItem(version, manifest, cursor)
	} else {
		page, err = reader.readMaterialStream(version, manifest, cursor, in.IncludeOutline && in.Cursor == "")
	}
	if err != nil {
		return publicationDraftReadResponse{}, err
	}
	return publicationDraftReadResponse{
		DraftID: in.DraftID, Hash: record.Hash, Title: manifest.Title, Provider: manifest.Provider,
		SourceID: manifest.SourceID, StartTurnID: manifest.StartTurnID, EndTurnID: manifest.EndTurnID,
		ReadingStartID: manifest.ReadingStartID, NextCursor: page.NextCursor, Scope: page.Scope,
		ItemComplete: page.ItemComplete, PageEndsAtTurnBoundary: page.PageEndsAtTurnBoundary,
		Turns: page.Turns, Segments: page.Segments,
	}, nil
}
