package collab

import (
	"context"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/google/uuid"
	"teamcross/internal/sharing"
)

func annotationVisible(a Annotation, execution bool) bool {
	return execution || a.Target == nil || a.Target.Kind == "material"
}
func visibleAnnotations(annotations []Annotation, execution bool) []Annotation {
	if execution {
		return annotations
	}
	out := []Annotation{}
	for _, a := range annotations {
		if annotationVisible(a, false) {
			out = append(out, a)
		}
	}
	return out
}

func (s *Session) replyAnnotation(ctx context.Context, in AnnotationReplyInput, author string) (Annotation, error) {
	in.Text = strings.TrimSpace(in.Text)
	if !utf8.ValidString(in.Text) || in.Text == "" || utf8.RuneCountInString(in.Text) > 4000 {
		return Annotation{}, fmt.Errorf("请输入 1–4000 字的回复")
	}
	if in.AnnotationID == "" || len(in.AnnotationID) > 200 || strings.TrimSpace(in.RequestID) == "" || len(in.RequestID) > 200 {
		return Annotation{}, fmt.Errorf("回复需要原批注 ID 和有效的 requestId")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || !s.callerValidLocked(ctx) {
		return Annotation{}, fmt.Errorf("共享已结束或连接已变化")
	}
	authorID := sharing.MemberID(ctx)
	index := -1
	for i, annotation := range s.record.Annotations {
		if !annotationVisible(annotation, sharing.ExecutionAuthorized(ctx, s.share)) {
			continue
		}
		if annotation.ID == in.AnnotationID {
			index = i
		}
		for _, reply := range annotation.Replies {
			if reply.RequestID == in.RequestID && reply.AuthorID == authorID && reply.Author == author {
				if annotation.ID != in.AnnotationID || reply.Text != in.Text || contentHash(reply.Materials) != contentHash(in.Materials) {
					return Annotation{}, fmt.Errorf("此 requestId 已用于其他回复，请先核对已保存的结果")
				}
				return annotation, nil
			}
		}
	}
	if index < 0 {
		return Annotation{}, fmt.Errorf("没有找到原批注；回复只能属于当前协作的一条原批注")
	}
	if err := s.validateReferencesLocked(in.Materials); err != nil {
		return Annotation{}, err
	}
	previous, updated := s.record.Annotations, s.record.UpdatedAt
	annotation := previous[index]
	annotation.Replies = append(append([]AnnotationReply(nil), annotation.Replies...), AnnotationReply{
		Materials: in.Materials, ID: uuid.NewString(), RequestID: in.RequestID, Text: in.Text, Author: author, AuthorID: authorID, CreatedAt: time.Now(),
	})
	// Context and view readers can still hold the previous snapshot outside mu.
	s.record.Annotations = append([]Annotation(nil), previous...)
	s.record.Annotations[index] = annotation
	s.record.UpdatedAt = time.Now()
	if err := s.saveLocked(); err != nil {
		s.record.Annotations, s.record.UpdatedAt = previous, updated
		return Annotation{}, err
	}
	return annotation, nil
}

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
	if t.Kind != "material" && (t.MaterialID != "" || t.Version != 0) {
		return fmt.Errorf("原生上下文不能混用材料身份")
	}
	switch t.Kind {
	case "history", "material":
		if t.TurnID == "" || t.ItemID == "" || t.StartOffset < 0 || t.EndOffset < t.StartOffset || t.EndOffset-t.StartOffset != len(utf16.Encode([]rune(t.Quote))) {
			return fmt.Errorf("对话批注需要消息 ID 和准确的原文范围")
		}
		if t.Kind == "material" && (t.MaterialID == "" || t.Version < 1 || t.SessionID != "" || t.Cursor != "") {
			return fmt.Errorf("材料批注必须固定材料及版本，不能携带原生读取入口")
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
		return fmt.Errorf("批注目标必须为 history、material、file 或 changes")
	}
	return nil
}
