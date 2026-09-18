package collab

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/google/uuid"
	"teamcross/internal/sharing"
)

func validateMaterial(c MaterialContent) error {
	if _, err := uuid.Parse(c.SourceID); err != nil {
		return fmt.Errorf("材料需要准确的来源会话身份")
	}
	if c.Provider != "codex" && c.Provider != "claude" {
		return fmt.Errorf("材料来源 Provider 不受支持")
	}
	if !utf8.ValidString(c.Title) || strings.TrimSpace(c.Title) == "" || len([]rune(c.Title)) > 160 {
		return fmt.Errorf("材料名称需要 1–160 字")
	}
	b, _ := json.Marshal(c)
	if len(b) > maxMaterialBytes || len(c.Turns) == 0 || len(c.Turns) > 2048 {
		return fmt.Errorf("材料须包含 1–2048 轮且不超过 4 MiB，请缩小公开范围")
	}
	if c.StartTurnID != c.Turns[0].ID || c.EndTurnID != c.Turns[len(c.Turns)-1].ID {
		return fmt.Errorf("公开范围与正文不一致")
	}
	seen := map[string]bool{}
	for _, t := range c.Turns {
		if t.ID == "" || len(t.ID) > 200 || seen[t.ID] {
			return fmt.Errorf("材料轮次标识无效或重复")
		}
		seen[t.ID] = true
		if t.Status != "completed" && t.Status != "interrupted" && t.Status != "failed" {
			return fmt.Errorf("不能发布仍在进行的轮次")
		}
		if len(t.Items) == 0 || len(t.Items) > 4096 {
			return fmt.Errorf("材料消息数量无效")
		}
		items := map[string]bool{}
		for _, i := range t.Items {
			if i.ID == "" || len(i.ID) > 200 || items[i.ID] || len(i.Type) > 80 || len(i.Notice) > 1000 || !utf8.ValidString(i.Text) {
				return fmt.Errorf("材料消息无效")
			}
			items[i.ID] = true
			if !slices.Contains([]string{"userMessage", "agentMessage", "toolCall", "toolResult", "commandExecution", "fileChange", "mcpToolCall", "dynamicToolCall", "webSearch", "imageView", "imageGeneration", "collabAgentToolCall", "plan", "unavailable"}, i.Type) {
				return fmt.Errorf("材料含不支持的内容类型")
			}
		}
	}
	if c.ReadingStartID != "" && !seen[c.ReadingStartID] {
		return fmt.Errorf("建议阅读起点必须在公开范围内")
	}
	return nil
}

func versionInfo(v MaterialVersion) map[string]any {
	notices := 0
	for _, t := range v.Turns {
		for _, i := range t.Items {
			if i.Notice != "" {
				notices++
			}
		}
	}
	return map[string]any{"version": v.Version, "title": v.Title, "provider": v.Provider, "sourceId": v.SourceID, "startTurnId": v.StartTurnID, "endTurnId": v.EndTurnID, "readingStartId": v.ReadingStartID, "turnCount": len(v.Turns), "hash": v.Hash, "createdAt": v.CreatedAt, "noticeCount": notices}
}
func (s *Session) materialDirectoryLocked() []map[string]any {
	out := []map[string]any{}
	for _, m := range s.record.Materials {
		versions := []map[string]any{}
		for n, v := range m.Versions {
			info := versionInfo(v)
			if n > 0 {
				before := map[string]string{}
				for _, t := range m.Versions[n-1].Turns {
					before[t.ID] = contentHash(t)
				}
				added, changed := 0, 0
				for _, t := range v.Turns {
					if hash, ok := before[t.ID]; !ok {
						added++
					} else {
						if hash != contentHash(t) {
							changed++
						}
						delete(before, t.ID)
					}
				}
				info["changes"] = map[string]int{"added": added, "changed": changed, "removed": len(before)}
			}
			versions = append(versions, info)
		}
		out = append(out, map[string]any{"id": m.ID, "authorId": m.AuthorID, "author": m.Author, "withdrawnAt": m.WithdrawnAt, "versions": versions})
	}
	return out
}
func publicationResult(m Material, v MaterialVersion) map[string]any {
	return map[string]any{"materialId": m.ID, "version": v.Version, "hash": v.Hash, "requestId": v.RequestID, "withdrawnAt": m.WithdrawnAt, "state": "published"}
}
func (s *Session) publicationStatus(ctx context.Context, requestID string) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.callerValidLocked(ctx) {
		return nil, fmt.Errorf("共享已结束")
	}
	for _, m := range s.record.Materials {
		if m.AuthorID == sharing.MemberID(ctx) {
			for _, v := range m.Versions {
				if v.RequestID == requestID {
					return publicationResult(m, v), nil
				}
			}
		}
	}
	return map[string]any{"requestId": requestID, "state": "not_found"}, nil
}
func (s *Session) publish(ctx context.Context, in publicationUpload) (any, error) {
	if _, err := uuid.Parse(in.RequestID); err != nil {
		return nil, fmt.Errorf("发布需要 UUID requestId")
	}
	if err := validateMaterial(in.Content); err != nil {
		return nil, err
	}
	if in.MaterialID != "" {
		if _, err := uuid.Parse(in.MaterialID); err != nil {
			return nil, fmt.Errorf("无效的材料 ID")
		}
	} else if in.BaseVersion != 0 {
		return nil, fmt.Errorf("新材料不接受 baseVersion")
	}
	hash := contentHash(in)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || !s.callerValidLocked(ctx) {
		return nil, fmt.Errorf("共享已结束或成员已移除")
	}
	authorID := sharing.MemberID(ctx)
	index := -1
	for n, m := range s.record.Materials {
		if m.ID == in.MaterialID {
			index = n
		}
		if m.AuthorID != authorID {
			continue
		}
		for _, v := range m.Versions {
			if v.RequestID == in.RequestID {
				if v.RequestHash != hash {
					return nil, fmt.Errorf("requestId 已用于不同发布内容，请先查询发布结果")
				}
				return publicationResult(m, v), nil
			}
		}
	}
	m := Material{ID: uuid.NewString(), AuthorID: authorID, Author: sharing.MemberName(ctx)}
	if in.MaterialID != "" {
		if index < 0 {
			return nil, fmt.Errorf("材料不存在")
		}
		m = s.record.Materials[index]
		if m.AuthorID != authorID {
			return nil, fmt.Errorf("只能更新自己发布的材料")
		}
		if m.WithdrawnAt != nil {
			return nil, fmt.Errorf("材料已撤回，请明确发布为新材料")
		}
		if in.BaseVersion != len(m.Versions) {
			return nil, fmt.Errorf("材料版本已变化，请先查看最新版本")
		}
		last := m.Versions[len(m.Versions)-1]
		if last.Provider != in.Content.Provider || last.SourceID != in.Content.SourceID {
			return nil, fmt.Errorf("同一材料的新版本必须来自同一原生会话")
		}
	}
	v := MaterialVersion{MaterialContent: in.Content, Version: len(m.Versions) + 1, Hash: contentHash(in.Content), RequestID: in.RequestID, RequestHash: hash, CreatedAt: time.Now()}
	m.Versions = append(append([]MaterialVersion(nil), m.Versions...), v)
	previous, updated := s.record.Materials, s.record.UpdatedAt
	s.record.Materials = append([]Material(nil), previous...)
	if index < 0 {
		s.record.Materials = append(s.record.Materials, m)
	} else {
		s.record.Materials[index] = m
	}
	s.record.UpdatedAt = time.Now()
	b, _ := json.Marshal(s.record.Materials)
	var err error
	if len(b) > 64<<20 {
		err = fmt.Errorf("空间材料已达到 64 MiB 上限")
	} else {
		err = s.saveLocked()
	}
	if err != nil {
		s.record.Materials, s.record.UpdatedAt = previous, updated
		return nil, err
	}
	return publicationResult(m, v), nil
}
func (s *Session) withdrawMaterial(ctx context.Context, id string) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || !s.callerValidLocked(ctx) {
		return nil, fmt.Errorf("共享已结束")
	}
	index := slices.IndexFunc(s.record.Materials, func(m Material) bool { return m.ID == id })
	if index < 0 {
		return nil, fmt.Errorf("材料不存在")
	}
	m := s.record.Materials[index]
	if m.AuthorID != sharing.MemberID(ctx) {
		return nil, fmt.Errorf("只能撤回自己发布的材料")
	}
	if m.WithdrawnAt != nil {
		return map[string]bool{"withdrawn": true}, nil
	}
	previous, updated := s.record.Materials, s.record.UpdatedAt
	now := time.Now()
	m.WithdrawnAt = &now
	s.record.Materials = append([]Material(nil), previous...)
	s.record.Materials[index] = m
	s.record.UpdatedAt = now
	if err := s.saveLocked(); err != nil {
		s.record.Materials, s.record.UpdatedAt = previous, updated
		return nil, err
	}
	return map[string]bool{"withdrawn": true}, nil
}
func (s *Session) materialVersionLocked(ref MaterialReference) (MaterialVersion, error) {
	for _, m := range s.record.Materials {
		if m.ID == ref.MaterialID {
			if m.WithdrawnAt != nil {
				return MaterialVersion{}, fmt.Errorf("材料已撤回")
			}
			if ref.Version < 1 || ref.Version > len(m.Versions) {
				return MaterialVersion{}, fmt.Errorf("材料版本不存在，请指定已读的固定版本")
			}
			v := m.Versions[ref.Version-1]
			if ref.TurnID != "" && !slices.ContainsFunc(v.Turns, func(t MaterialTurn) bool { return t.ID == ref.TurnID }) {
				return MaterialVersion{}, fmt.Errorf("定位不在已公开范围内")
			}
			return v, nil
		}
	}
	return MaterialVersion{}, fmt.Errorf("当前空间没有这份材料")
}
func (s *Session) validateReferencesLocked(refs []MaterialReference) error {
	if len(refs) > 16 {
		return fmt.Errorf("一次最多引用 16 份材料")
	}
	for _, ref := range refs {
		if _, err := s.materialVersionLocked(ref); err != nil {
			return err
		}
	}
	return nil
}

type materialCursor struct {
	MaterialID         string
	Version            int
	Hash               string
	Turn, Item, Offset int
}

func (s *Session) validateMaterialTargetLocked(t *AnnotationTarget) error {
	v, err := s.materialVersionLocked(MaterialReference{MaterialID: t.MaterialID, Version: t.Version, TurnID: t.TurnID})
	if err != nil {
		return err
	}
	for _, turn := range v.Turns {
		if turn.ID == t.TurnID {
			for _, item := range turn.Items {
				if item.ID == t.ItemID {
					text := utf16.Encode([]rune(item.Text))
					if t.StartOffset < 0 || t.EndOffset > len(text) || t.StartOffset > t.EndOffset || string(utf16.Decode(text[t.StartOffset:t.EndOffset])) != t.Quote {
						return fmt.Errorf("引用原文与已发布版本不一致")
					}
					return nil
				}
			}
		}
	}
	return fmt.Errorf("材料中没有该消息")
}
func (s *Session) readMaterial(ctx context.Context, in MaterialRead) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || !s.callerValidLocked(ctx) {
		return nil, fmt.Errorf("共享已结束")
	}
	v, err := s.materialVersionLocked(MaterialReference{MaterialID: in.MaterialID, Version: in.Version, TurnID: in.TurnID})
	if err != nil {
		return nil, err
	}
	c := materialCursor{MaterialID: in.MaterialID, Version: in.Version, Hash: v.Hash}
	if in.TurnID != "" {
		c.Turn = slices.IndexFunc(v.Turns, func(t MaterialTurn) bool { return t.ID == in.TurnID })
	}
	if in.Cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(in.Cursor)
		if e != nil || len(b) > 1024 || json.Unmarshal(b, &c) != nil || c.MaterialID != in.MaterialID || c.Version != in.Version || c.Hash != v.Hash {
			return nil, fmt.Errorf("分页位置不属于这份材料的固定版本")
		}
	}
	if c.Turn < 0 || c.Turn >= len(v.Turns) || c.Item < 0 || c.Item >= len(v.Turns[c.Turn].Items) || c.Offset < 0 {
		return nil, fmt.Errorf("分页位置无效")
	}
	segments := []map[string]any{}
	budget := 16000 // at most 64 KiB of UTF-8 text, including large saved tool output
	for c.Turn < len(v.Turns) && budget > 0 {
		t := v.Turns[c.Turn]
		i := t.Items[c.Item]
		text := []rune(i.Text)
		if c.Offset > len(text) {
			return nil, fmt.Errorf("分页位置无效")
		}
		end := min(len(text), c.Offset+budget)
		segments = append(segments, map[string]any{"turnId": t.ID, "status": t.Status, "itemId": i.ID, "type": i.Type, "text": string(text[c.Offset:end]), "startOffset": len(utf16.Encode(text[:c.Offset])), "endOffset": len(utf16.Encode(text[:end])), "notice": i.Notice})
		budget -= max(1, end-c.Offset)
		c.Offset = end
		if c.Offset == len(text) {
			c.Offset = 0
			c.Item++
			if c.Item == len(t.Items) {
				c.Item = 0
				c.Turn++
			}
		}
		if len(segments) >= 64 {
			break
		}
	}
	next := ""
	if c.Turn < len(v.Turns) {
		b, _ := json.Marshal(c)
		next = base64.RawURLEncoding.EncodeToString(b)
	}
	outline := []map[string]any{}
	for _, t := range v.Turns {
		label := t.ID
		for _, i := range t.Items {
			if strings.TrimSpace(i.Text) != "" {
				label = shortText(i.Text, 100)
				break
			}
		}
		for _, i := range t.Items {
			if i.Type == "userMessage" && strings.TrimSpace(i.Text) != "" {
				label = shortText(i.Text, 100)
				break
			}
		}
		outline = append(outline, map[string]any{"id": t.ID, "label": label})
	}
	return map[string]any{"materialId": in.MaterialID, "version": versionInfo(v), "turns": outline, "segments": segments, "nextCursor": next}, nil
}
