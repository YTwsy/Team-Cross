package collab

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

func (t *AnnotationTarget) validate() error {
	if t == nil {
		return nil
	}
	if !utf8.ValidString(t.Quote) || t.Quote == "" || utf8.RuneCountInString(t.Quote) > 8000 {
		return fmt.Errorf("请选择 1–8000 字的原文")
	}
	if len(t.SessionID) > 200 || len(t.TurnID) > 200 || len(t.ItemID) > 200 || len(t.Cursor) > 4000 {
		return fmt.Errorf("批注定位过长")
	}
	switch t.Kind {
	case "history":
		if t.TurnID == "" || t.ItemID == "" || t.StartOffset < 0 || t.EndOffset < t.StartOffset || t.EndOffset-t.StartOffset != len(utf16.Encode([]rune(t.Quote))) {
			return fmt.Errorf("对话批注需要消息 ID 和准确的原文范围")
		}
		if t.Path != "" || t.Side != "" || t.StartLine != 0 || t.EndLine != 0 || t.ContentHash != "" || t.BaseRevision != "" {
			return fmt.Errorf("对话批注不能包含文件定位")
		}
	case "file", "changes":
		clean := filepath.Clean(t.Path)
		if t.Path == "" || len(t.Path) > 4096 || strings.ContainsRune(t.Path, 0) || filepath.IsAbs(t.Path) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("批注文件必须使用协作目录内的相对路径")
		}
		t.Path = filepath.ToSlash(clean)
		if t.StartLine < 1 || t.EndLine < t.StartLine || t.EndLine-t.StartLine+1 != strings.Count(t.Quote, "\n")+1 {
			return fmt.Errorf("批注行号与原文范围不一致")
		}
		hash, err := hex.DecodeString(t.ContentHash)
		if err != nil || len(hash) != 32 {
			return fmt.Errorf("请携带读取上下文时返回的 contentHash")
		}
		if t.Kind == "changes" {
			revision, err := hex.DecodeString(t.BaseRevision)
			if (t.Side != "old" && t.Side != "new") || err != nil || (len(revision) != 20 && len(revision) != 32) {
				return fmt.Errorf("改动批注需要 old/new 一侧和 baseRevision")
			}
		} else if t.Side != "" || t.BaseRevision != "" {
			return fmt.Errorf("文件批注不能包含改动一侧或基准提交")
		}
		if t.TurnID != "" || t.ItemID != "" || t.Cursor != "" || t.StartOffset != 0 || t.EndOffset != 0 {
			return fmt.Errorf("文件批注不能包含对话定位")
		}
	default:
		return fmt.Errorf("批注目标必须为 history、file 或 changes")
	}
	return nil
}
