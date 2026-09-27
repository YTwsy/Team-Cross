package collab

import (
	"fmt"
	"unicode/utf16"

	"teamcross/internal/materialstore"
	"teamcross/internal/readview"
)

type agentItemIndex struct {
	TurnID string `json:"turnId"`
	ItemID string `json:"itemId"`
	Type   string `json:"type"`
	Phase  string `json:"phase,omitempty"`
	Label  string `json:"label,omitempty"`
	Length int    `json:"length"`
	Notice string `json:"notice,omitempty"`
}

func agentOutline(turn materialstore.Turn) materialOutline {
	out := materialOutline{ID: turn.ID, Label: turn.Label, Status: turn.Status, ItemCount: len(turn.Items), AnswerStatus: "unidentified"}
	if turn.Status != "completed" && turn.Status != "failed" && turn.Status != "interrupted" {
		out.AnswerStatus = "pending"
	}
	for _, item := range turn.Items {
		if item.Type == "agentMessage" && item.Phase == "final_answer" {
			out.AnswerStatus = "available"
		}
		if !isConversationItem(item) {
			out.ToolCount++
		}
	}
	return out
}

func agentCursor(cursor materialCursor, options readview.Options, count int, targeted bool) (materialCursor, error) {
	if cursor.Read.Enabled() {
		if options.Enabled() {
			requested, err := options.Normalize()
			if err != nil || requested != cursor.Read {
				return cursor, fmt.Errorf("分页必须沿用原来的读取选项")
			}
		}
	}
	if !cursor.Read.Enabled() || cursor.WindowEnd == 0 {
		var err error
		if cursor.Read.Enabled() {
			options = cursor.Read
		}
		cursor.Read, err = options.Normalize()
		if err != nil {
			return cursor, err
		}
		if targeted || cursor.Scope == "item" {
			cursor.WindowStart, cursor.WindowEnd = cursor.Turn, cursor.Turn+1
		} else {
			cursor.WindowStart, cursor.WindowEnd = max(0, count-cursor.Read.TurnLimit), count
			cursor.Turn, cursor.Older = cursor.WindowStart, true
		}
	}
	if _, err := cursor.Read.Normalize(); err != nil {
		return cursor, err
	}
	if cursor.WindowStart < 0 || cursor.WindowEnd > count || cursor.WindowStart >= cursor.WindowEnd || cursor.Turn < cursor.WindowStart || cursor.Turn >= cursor.WindowEnd || cursor.Item < 0 || cursor.Offset < 0 {
		return cursor, fmt.Errorf("读取分页位置无效")
	}
	return cursor, nil
}

func agentNext(cursor materialCursor) string {
	if cursor.Turn >= cursor.WindowEnd {
		if !cursor.Older || cursor.WindowStart == 0 {
			return ""
		}
		cursor.WindowEnd = cursor.WindowStart
		cursor.WindowStart = max(0, cursor.WindowEnd-cursor.Read.TurnLimit)
		cursor.Turn, cursor.Item, cursor.Offset = cursor.WindowStart, 0, 0
	}
	return encodeMaterialCursor(cursor)
}

// readAgentPage never loads tool bodies for answers/none, outline or item
// indices. wrap lets every caller budget its complete response, not just text.
func readAgentPage(readBody materialBodyReader, version MaterialVersion, manifest materialstore.Manifest, cursor materialCursor, options readview.Options, targeted bool, wrap func(materialReadResponse) any) (materialReadResponse, error) {
	if len(manifest.Turns) == 0 {
		return materialReadResponse{View: options.View, Scope: "stream", Segments: []materialSegment{}}, nil
	}
	var err error
	cursor, err = agentCursor(cursor, options, len(manifest.Turns), targeted)
	if err != nil {
		return materialReadResponse{}, err
	}
	page := materialReadResponse{MaterialID: cursor.MaterialID, Version: map[string]any{"version": version.Version, "hash": version.Hash, "provider": version.Provider, "sourceId": version.SourceID}, View: cursor.Read.View, Scope: cursor.Scope, Segments: []materialSegment{}}
	if wrap == nil {
		wrap = func(p materialReadResponse) any { return p }
	}
	finish := func(p materialReadResponse, c materialCursor) materialReadResponse {
		p.NextCursor = agentNext(c)
		p.PageEndsAtTurnBoundary = c.Turn >= c.WindowEnd || c.Item == 0 && c.Offset == 0
		p.ItemComplete = c.Scope == "item" && c.Turn >= c.WindowEnd
		return p
	}
	fits := func(p materialReadResponse, c materialCursor) bool {
		return readview.WireSize(wrap(finish(p, c))) <= cursor.Read.MaxBytes
	}
	progress := false
	for cursor.Turn < cursor.WindowEnd {
		turnIndex := cursor.Turn
		turn := manifest.Turns[cursor.Turn]
		if cursor.Item >= len(turn.Items) && len(turn.Items) > 0 {
			return page, fmt.Errorf("读取记录位置无效")
		}
		beforeTurn := page
		page.Turns = append(page.Turns, agentOutline(turn))
		if !fits(page, cursor) {
			page = beforeTurn
			break
		}
		if cursor.Read.View == "outline" && cursor.Scope != "item" {
			cursor.Turn++
			cursor.Item, cursor.Offset = 0, 0
			progress = true
			continue
		}
		for cursor.Item < len(turn.Items) {
			item := turn.Items[cursor.Item]
			next := cursor
			next.Item++
			next.Offset = 0
			if next.Item == len(turn.Items) || cursor.Scope == "item" {
				next.Turn++
				next.Item = 0
			}
			if cursor.Read.View == "items" && cursor.Scope != "item" {
				candidate := page
				candidate.Items = append(candidate.Items, agentItemIndex{TurnID: turn.ID, ItemID: item.ID, Type: item.Type, Phase: item.Phase, Label: item.Label, Length: item.Body.UTF16Length, Notice: item.Notice})
				if !fits(candidate, next) {
					if !progress {
						return page, fmt.Errorf("记录索引超过 maxBytes，请增大预算")
					}
					return finish(page, cursor), nil
				}
				page, cursor, progress = candidate, next, true
			} else {
				include := cursor.Scope == "item" || item.Type == "userMessage" || item.Type == "agentMessage" && (cursor.Read.View == "conversation" || item.Phase == "final_answer") || !isConversationItem(item) && cursor.Read.ToolOutputs != "none"
				if !include {
					cursor, progress = next, true
				} else {
					body, readErr := readBody(item)
					if readErr != nil {
						return page, readErr
					}
					units := utf16.Encode([]rune(body))
					if !validUTF16Boundary(units, cursor.Offset) {
						return page, fmt.Errorf("UTF-16 偏移不在字符边界")
					}
					endLimit := len(units)
					preview := cursor.Scope != "item" && !isConversationItem(item) && cursor.Read.ToolOutputs == "preview"
					if preview {
						endLimit = min(endLimit, materialPreviewLimit)
						if !validUTF16Boundary(units, endLimit) {
							endLimit--
						}
					}
					if cursor.Offset > endLimit {
						return page, fmt.Errorf("预览偏移无效")
					}
					candidateAt := func(end int) (materialReadResponse, materialCursor) {
						c := next
						if end < endLimit {
							c = cursor
							c.Offset = end
						}
						candidate := page
						segment := newMaterialSegment(turn, loadedMaterialItem{item: item, length: len(units)}, string(utf16.Decode(units[cursor.Offset:end])), cursor.Offset, end, preview && end < len(units))
						// One shared protocol description replaces repeated per-item prose.
						segment.ReadHint = ""
						segment.Notice = item.Notice
						candidate.Segments = append(candidate.Segments, segment)
						return candidate, c
					}
					// Bound the candidate before encoding; binary search counts escaping,
					// metadata, turn indices and continuation cursors in the same budget.
					lo, hi, best := cursor.Offset, min(endLimit, cursor.Offset+cursor.Read.MaxBytes), -1
					for lo <= hi {
						mid := lo + (hi-lo)/2
						end := mid
						if !validUTF16Boundary(units, end) {
							end--
						}
						candidate, c := candidateAt(end)
						if fits(candidate, c) {
							best = end
							lo = mid + 1
						} else {
							hi = mid - 1
						}
					}
					if best < 0 || best == cursor.Offset && best < endLimit {
						if !progress {
							return page, fmt.Errorf("读取元数据超过 maxBytes，请增大预算")
						}
						return finish(page, cursor), nil
					}
					page, cursor = candidateAt(best)
					progress = true
					if best < endLimit {
						return finish(page, cursor), nil
					}
				}
			}
			if cursor.Turn != next.Turn || cursor.Item != next.Item {
				return page, fmt.Errorf("读取位置没有前进")
			}
			if cursor.Turn != turnIndex {
				break
			}
		}
		// Empty turns still retain status and an explicit missing-answer marker.
		if len(turn.Items) == 0 {
			cursor.Turn++
			cursor.Item, cursor.Offset = 0, 0
			progress = true
		}
		if len(page.Segments)+len(page.Items) >= materialSegmentLimit {
			break
		}
	}
	if !progress {
		return page, fmt.Errorf("读取元数据超过 maxBytes，请增大预算")
	}
	return finish(page, cursor), nil
}
