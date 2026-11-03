package collab

import (
	"context"
	"fmt"

	"teamcross/internal/readview"
	"teamcross/internal/sharing"
)

// These projections are applied after the existing visibility/permission filter.
func agentAnnotations(spaceID string, notes []Annotation, in HistoryRead) (any, error) {
	opts, err := in.Options.Normalize()
	if err != nil || in.Offset < 0 {
		return nil, fmt.Errorf("批注分页参数无效")
	}
	out := map[string]any{"spaceId": spaceID, "annotations": []any{}, "total": len(notes)}
	if (in.ReplyID != "" || in.SourceOnly) && in.AnnotationID == "" {
		return nil, fmt.Errorf("精确讨论来源需要 annotationId")
	}
	if in.AnnotationID == "" {
		if in.Offset > len(notes) {
			return nil, fmt.Errorf("批注分页位置无效")
		}
		items := []any{}
		for i := in.Offset; i < len(notes); i++ {
			note := notes[i]
			text := []rune(note.Text)
			entry := map[string]any{"id": note.ID, "textPreview": string(text[:min(240, len(text))]), "author": note.Author, "replyCount": len(note.Replies), "hasTarget": note.Target != nil}
			candidate := append(items, entry)
			out["annotations"], out["nextOffset"] = candidate, i+1
			if len(items) == 20 || readview.WireSize(out) > opts.MaxBytes {
				out["annotations"], out["nextOffset"] = items, i
				return out, nil
			}
			items = candidate
		}
		delete(out, "nextOffset")
		return out, nil
	}
	if len(notes) != 1 {
		return nil, fmt.Errorf("没有找到当前空间的原批注")
	}
	note := notes[0]
	if in.ReplyID != "" || in.SourceOnly {
		if in.Offset != 0 {
			return nil, fmt.Errorf("精确来源读取不能混用回复分页位置")
		}
		root := note
		root.Replies = nil
		if in.IncludeQuote != nil && !*in.IncludeQuote && root.Target != nil {
			target := *root.Target
			target.Quote = ""
			root.Target = &target
			out["quoteOmitted"] = true
		}
		out["annotations"], out["replyCount"], out["sourceOnly"] = []Annotation{root}, len(note.Replies), true
		if in.ReplyID != "" {
			var reply *AnnotationReply
			for i := range note.Replies {
				if note.Replies[i].ID == in.ReplyID {
					reply = &note.Replies[i]
					break
				}
			}
			if reply == nil {
				return nil, fmt.Errorf("没有找到当前讨论中的准确回复")
			}
			out["reply"] = *reply
			if reply.ReplyTo != nil && reply.ReplyTo.Kind == "reply" {
				for _, parent := range note.Replies {
					if parent.ID == reply.ReplyTo.ID {
						out["parentReply"] = parent
						break
					}
				}
				if out["parentReply"] == nil {
					return nil, fmt.Errorf("回复父项缺失，不能确认讨论上下文")
				}
			}
		}
		if readview.WireSize(out) > opts.MaxBytes {
			return nil, fmt.Errorf("精确讨论来源超过 maxBytes；可增加预算或设 includeQuote=false 后按 target 回读")
		}
		return out, nil
	}
	if in.Offset > len(note.Replies) {
		return nil, fmt.Errorf("回复分页位置无效")
	}
	replies := note.Replies
	note.Replies = nil
	quoteOmitted := in.Offset > 0 || in.IncludeQuote != nil && !*in.IncludeQuote
	if quoteOmitted && note.Target != nil {
		target := *note.Target
		target.Quote = ""
		note.Target = &target
	}
	out["annotations"], out["replyCount"], out["quoteOmitted"] = []Annotation{note}, len(replies), quoteOmitted
	nextRead := func(offset int) map[string]any {
		return map[string]any{"annotationId": in.AnnotationID, "offset": offset, "includeQuote": false, "maxBytes": opts.MaxBytes}
	}
	if in.Offset < len(replies) {
		out["nextRead"] = nextRead(in.Offset)
	}
	if readview.WireSize(out) > opts.MaxBytes {
		return nil, fmt.Errorf("批注超过 maxBytes；可设 maxBytes=65536 或 includeQuote=false 后按 target 定位原文")
	}
	for i := in.Offset; i < len(replies); i++ {
		candidate := note
		candidate.Replies = append(candidate.Replies, replies[i])
		out["annotations"], out["nextRead"] = []Annotation{candidate}, nextRead(i+1)
		if readview.WireSize(out) > opts.MaxBytes {
			out["annotations"], out["nextRead"] = []Annotation{note}, nextRead(i)
			if i == in.Offset && quoteOmitted {
				return nil, fmt.Errorf("回复超过 maxBytes，请增大预算")
			}
			return out, nil
		}
		note = candidate
	}
	delete(out, "nextRead")
	return out, nil
}

func (s *Session) replyAnnotationResult(ctx context.Context, in AnnotationReplyInput, author string, compact bool) (any, error) {
	note, err := s.replyAnnotation(ctx, in, author)
	if err != nil || !compact {
		return note, err
	}
	for _, reply := range note.Replies {
		if reply.RequestID == in.RequestID && reply.Author == author && reply.AuthorID == sharing.MemberID(ctx) {
			return map[string]any{"annotationId": note.ID, "status": "saved", "reply": reply}, nil
		}
	}
	return nil, fmt.Errorf("回复回执不可用，请按 annotationId 查询保存状态")
}
