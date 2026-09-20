package collab

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"time"
	"unicode/utf16"

	"github.com/google/uuid"
	"teamcross/internal/materialstore"
	"teamcross/internal/sharing"
)

const (
	materialStreamSoftLimit = 16000
	materialStreamHardLimit = 24000
	materialItemPageLimit   = 16000
	materialPreviewLimit    = 1500
	materialSegmentLimit    = 64
	materialSpaceBlobLimit  = 256 << 20
)

func versionInfo(v MaterialVersion) map[string]any {
	info := map[string]any{
		"version": v.Version, "title": v.Title, "provider": v.Provider,
		"sourceId": v.SourceID, "startTurnId": v.StartTurnID,
		"endTurnId": v.EndTurnID, "readingStartId": v.ReadingStartID,
		"turnCount": v.TurnCount, "hash": v.Hash, "createdAt": v.CreatedAt,
		"noticeCount": v.NoticeCount,
	}
	if v.Changes != nil {
		info["changes"] = v.Changes
	}
	return info
}

func (s *Session) materialDirectoryLocked() []map[string]any {
	out := []map[string]any{}
	for _, material := range s.record.Materials {
		versions := make([]map[string]any, 0, len(material.Versions))
		for _, version := range material.Versions {
			versions = append(versions, versionInfo(version))
		}
		out = append(out, map[string]any{"id": material.ID, "authorId": material.AuthorID, "author": material.Author, "withdrawnAt": material.WithdrawnAt, "versions": versions})
	}
	return out
}

func publicationResult(material Material, version MaterialVersion) map[string]any {
	return map[string]any{"materialId": material.ID, "version": version.Version, "hash": version.Hash, "requestId": version.RequestID, "withdrawnAt": material.WithdrawnAt, "state": "published"}
}

func (s *Session) publicationStatus(ctx context.Context, requestID string) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.callerValidLocked(ctx) {
		return nil, fmt.Errorf("共享已结束")
	}
	for _, material := range s.record.Materials {
		if material.AuthorID == sharing.MemberID(ctx) {
			for _, version := range material.Versions {
				if version.RequestID == requestID {
					return publicationResult(material, version), nil
				}
			}
		}
	}
	return map[string]any{"requestId": requestID, "state": "not_found"}, nil
}

func publicationRequestHash(manifestHash string, in publicationUpload) string {
	return contentHash(struct {
		Domain      string `json:"domain"`
		Manifest    string `json:"manifest"`
		MaterialID  string `json:"materialId,omitempty"`
		BaseVersion int    `json:"baseVersion,omitempty"`
	}{Domain: "teamcross/publication-request/v1", Manifest: manifestHash, MaterialID: in.MaterialID, BaseVersion: in.BaseVersion})
}

func requireUploadedBlobs(bundle materialstore.Bundle) error {
	for _, turn := range bundle.Manifest.Turns {
		for _, item := range turn.Items {
			if item.Body.Kind == materialstore.BodyBlob {
				if _, ok := bundle.Blobs[item.Body.Hash]; !ok {
					return fmt.Errorf("发布请求必须携带每个 blob；省略上传只能通过已授权协商")
				}
			}
		}
	}
	return nil
}

// publicationPreflightLocked authorizes a publish without touching blob
// storage. It is deliberately called again after immutable files are written,
// because a concurrent version may have changed while the session lock was
// released.
func (s *Session) publicationPreflightLocked(ctx context.Context, in publicationUpload, manifest materialstore.Manifest, requestHash string) (Material, int, any, error) {
	if s.closed || !s.callerValidLocked(ctx) {
		return Material{}, -1, nil, fmt.Errorf("共享已结束或成员已移除")
	}
	authorID := sharing.MemberID(ctx)
	index := -1
	for materialIndex, material := range s.record.Materials {
		if material.ID == in.MaterialID {
			index = materialIndex
		}
		if material.AuthorID != authorID {
			continue
		}
		for _, version := range material.Versions {
			if version.RequestID == in.RequestID {
				if version.RequestHash != requestHash {
					return Material{}, -1, nil, fmt.Errorf("requestId 已用于不同发布内容，请先查询发布结果")
				}
				return material, materialIndex, publicationResult(material, version), nil
			}
		}
	}
	material := Material{ID: uuid.NewString(), AuthorID: authorID, Author: sharing.MemberName(ctx)}
	if in.MaterialID == "" {
		return material, -1, nil, nil
	}
	if index < 0 {
		return Material{}, -1, nil, fmt.Errorf("材料不存在")
	}
	material = s.record.Materials[index]
	if material.AuthorID != authorID {
		return Material{}, -1, nil, fmt.Errorf("只能更新自己发布的材料")
	}
	if material.WithdrawnAt != nil {
		return Material{}, -1, nil, fmt.Errorf("材料已撤回，请明确发布为新材料")
	}
	if in.BaseVersion != len(material.Versions) {
		return Material{}, -1, nil, fmt.Errorf("材料版本已变化，请先查看最新版本")
	}
	last := material.Versions[len(material.Versions)-1]
	if last.Provider != manifest.Provider || last.SourceID != manifest.SourceID {
		return Material{}, -1, nil, fmt.Errorf("同一材料的新版本必须来自同一原生会话")
	}
	return material, index, nil, nil
}

func (s *Session) activeBlobBytesLocked(add materialstore.Manifest) (int64, error) {
	seen := map[string]bool{}
	var total int64
	count := func(manifest materialstore.Manifest) {
		for _, turn := range manifest.Turns {
			for _, item := range turn.Items {
				if item.Body.Kind == materialstore.BodyBlob && !seen[item.Body.Hash] {
					seen[item.Body.Hash] = true
					total += int64(item.Body.ByteLength)
				}
			}
		}
	}
	for _, material := range s.record.Materials {
		if material.WithdrawnAt != nil {
			continue
		}
		for _, version := range material.Versions {
			manifest, err := s.materialStore.LoadManifest(version.Hash)
			if err != nil {
				return 0, err
			}
			count(manifest)
		}
	}
	count(add)
	return total, nil
}

func versionFromManifest(manifest materialstore.Manifest, hash, requestID, requestHash string, version int, changes *MaterialChanges) MaterialVersion {
	return MaterialVersion{
		Version: version, Hash: hash, Title: manifest.Title, Provider: manifest.Provider,
		SourceID: manifest.SourceID, StartTurnID: manifest.StartTurnID,
		EndTurnID: manifest.EndTurnID, ReadingStartID: manifest.ReadingStartID,
		TurnCount: len(manifest.Turns), NoticeCount: manifestNoticeCount(manifest),
		Changes: changes, RequestID: requestID, RequestHash: requestHash, CreatedAt: time.Now(),
	}
}

func (s *Session) publish(ctx context.Context, in publicationUpload) (any, error) {
	s.materialUploadMu.Lock()
	defer s.materialUploadMu.Unlock()
	return s.publishBundle(ctx, in, true)
}

func (s *Session) publishBundle(ctx context.Context, in publicationUpload, requireBodies bool) (any, error) {
	if _, err := uuid.Parse(in.RequestID); err != nil {
		return nil, fmt.Errorf("发布需要 UUID requestId")
	}
	if in.MaterialID != "" {
		if _, err := uuid.Parse(in.MaterialID); err != nil {
			return nil, fmt.Errorf("无效的材料 ID")
		}
	} else if in.BaseVersion != 0 {
		return nil, fmt.Errorf("新材料不接受 baseVersion")
	}
	manifest, manifestHash, _, err := materialstore.Finalize(in.Bundle.Manifest)
	if err != nil {
		return nil, err
	}
	if _, err = uuid.Parse(manifest.SourceID); err != nil {
		return nil, fmt.Errorf("材料需要准确的来源会话身份")
	}
	if _, blobBytes := manifestBlobHashes(manifest); blobBytes > materialVersionBlobLimit {
		return nil, fmt.Errorf("一个材料版本引用的唯一 blob 不能超过 32 MiB")
	}
	in.Bundle.Manifest = manifest
	if requireBodies {
		if err = requireUploadedBlobs(in.Bundle); err != nil {
			return nil, err
		}
	}
	requestHash := publicationRequestHash(manifestHash, in)

	s.mu.Lock()
	material, _, priorResult, err := s.publicationPreflightLocked(ctx, in, manifest, requestHash)
	s.mu.Unlock()
	if err != nil || priorResult != nil {
		return priorResult, err
	}
	if storedHash, putErr := s.materialStore.PutBundle(in.Bundle); putErr != nil {
		return nil, putErr
	} else if storedHash != manifestHash {
		return nil, fmt.Errorf("材料清单哈希不一致")
	}

	var previousManifest materialstore.Manifest
	if len(material.Versions) > 0 {
		previousManifest, err = s.materialStore.LoadManifest(material.Versions[len(material.Versions)-1].Hash)
		if err != nil {
			return nil, err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	material, index, priorResult, err := s.publicationPreflightLocked(ctx, in, manifest, requestHash)
	if err != nil || priorResult != nil {
		return priorResult, err
	}
	if total, quotaErr := s.activeBlobBytesLocked(manifest); quotaErr != nil {
		return nil, quotaErr
	} else if total > materialSpaceBlobLimit {
		return nil, fmt.Errorf("空间仍在使用的唯一 blob 已超过 256 MiB")
	}
	var changes *MaterialChanges
	if len(material.Versions) > 0 {
		if previousManifest.Schema == 0 {
			previousManifest, err = s.materialStore.LoadManifest(material.Versions[len(material.Versions)-1].Hash)
			if err != nil {
				return nil, err
			}
		}
		changes = compareManifests(previousManifest, manifest)
	}
	version := versionFromManifest(manifest, manifestHash, in.RequestID, requestHash, len(material.Versions)+1, changes)
	material.Versions = append(append([]MaterialVersion(nil), material.Versions...), version)
	previousMaterials, previousUpdated := s.record.Materials, s.record.UpdatedAt
	s.record.Materials = append([]Material(nil), previousMaterials...)
	if index < 0 {
		s.record.Materials = append(s.record.Materials, material)
	} else {
		s.record.Materials[index] = material
	}
	s.record.UpdatedAt = time.Now()
	if err = s.saveLocked(); err != nil {
		s.record.Materials, s.record.UpdatedAt = previousMaterials, previousUpdated
		return nil, err
	}
	return publicationResult(material, version), nil
}

func (s *Session) withdrawMaterial(ctx context.Context, id string) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || !s.callerValidLocked(ctx) {
		return nil, fmt.Errorf("共享已结束")
	}
	index := slices.IndexFunc(s.record.Materials, func(material Material) bool { return material.ID == id })
	if index < 0 {
		return nil, fmt.Errorf("材料不存在")
	}
	material := s.record.Materials[index]
	if material.AuthorID != sharing.MemberID(ctx) {
		return nil, fmt.Errorf("只能撤回自己发布的材料")
	}
	if material.WithdrawnAt != nil {
		return map[string]bool{"withdrawn": true}, nil
	}
	previous, updated := s.record.Materials, s.record.UpdatedAt
	now := time.Now()
	material.WithdrawnAt = &now
	s.record.Materials = append([]Material(nil), previous...)
	s.record.Materials[index] = material
	s.record.UpdatedAt = now
	if err := s.saveLocked(); err != nil {
		s.record.Materials, s.record.UpdatedAt = previous, updated
		return nil, err
	}
	return map[string]bool{"withdrawn": true}, nil
}

func (s *Session) materialVersionLocked(ref MaterialReference) (MaterialVersion, error) {
	for _, material := range s.record.Materials {
		if material.ID != ref.MaterialID {
			continue
		}
		if material.WithdrawnAt != nil {
			return MaterialVersion{}, fmt.Errorf("材料已撤回")
		}
		if ref.Version < 1 || ref.Version > len(material.Versions) {
			return MaterialVersion{}, fmt.Errorf("材料版本不存在，请指定已读的固定版本")
		}
		return material.Versions[ref.Version-1], nil
	}
	return MaterialVersion{}, fmt.Errorf("当前空间没有这份材料")
}

func manifestHasTurn(manifest materialstore.Manifest, turnID string) bool {
	return slices.ContainsFunc(manifest.Turns, func(turn materialstore.Turn) bool { return turn.ID == turnID })
}

func (s *Session) validateReferencesLocked(refs []MaterialReference) error {
	if len(refs) > 16 {
		return fmt.Errorf("一次最多引用 16 份材料")
	}
	for _, ref := range refs {
		version, err := s.materialVersionLocked(ref)
		if err != nil {
			return err
		}
		if ref.TurnID != "" {
			manifest, loadErr := s.materialStore.LoadManifest(version.Hash)
			if loadErr != nil {
				return loadErr
			}
			if !manifestHasTurn(manifest, ref.TurnID) {
				return fmt.Errorf("定位不在已公开范围内")
			}
		}
	}
	return nil
}

type materialCursor struct {
	Scope        string `json:"scope"`
	MaterialID   string `json:"materialId"`
	Version      int    `json:"version"`
	Hash         string `json:"hash"`
	SourceCursor string `json:"sourceCursor,omitempty"`
	Turn         int    `json:"turn"`
	Item         int    `json:"item"`
	Offset       int    `json:"offset"`
}

type materialSegment struct {
	TurnID        string `json:"turnId"`
	Status        string `json:"status"`
	ItemID        string `json:"itemId"`
	Type          string `json:"type"`
	Text          string `json:"text"`
	StartOffset   int    `json:"startOffset"`
	EndOffset     int    `json:"endOffset"`
	Length        int    `json:"length"`
	SourceLength  int    `json:"sourceLength,omitempty"`
	OmittedLength int    `json:"omittedLength,omitempty"`
	Notice        string `json:"notice,omitempty"`
	Collapsed     bool   `json:"collapsed,omitempty"`
	ReadHint      string `json:"readHint,omitempty"`
}

type materialOutline struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type materialReadResponse struct {
	MaterialID             string            `json:"materialId"`
	Version                map[string]any    `json:"version"`
	NextCursor             string            `json:"nextCursor"`
	Scope                  string            `json:"scope"`
	ItemComplete           bool              `json:"itemComplete,omitempty"`
	PageEndsAtTurnBoundary bool              `json:"pageEndsAtTurnBoundary"`
	Turns                  []materialOutline `json:"turns,omitempty"`
	Segments               []materialSegment `json:"segments"`
}

func encodeMaterialCursor(cursor materialCursor) string {
	b, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeMaterialCursor(value string) (materialCursor, error) {
	var cursor materialCursor
	b, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(b) > 1024 || json.Unmarshal(b, &cursor) != nil {
		return cursor, fmt.Errorf("分页位置无效")
	}
	return cursor, nil
}

func validUTF16Boundary(units []uint16, offset int) bool {
	if offset < 0 || offset > len(units) {
		return false
	}
	if offset == 0 || offset == len(units) {
		return true
	}
	return !(units[offset-1] >= 0xD800 && units[offset-1] <= 0xDBFF && units[offset] >= 0xDC00 && units[offset] <= 0xDFFF)
}

func utf16Page(text string, start, limit int) (string, int, int, error) {
	units := utf16.Encode([]rune(text))
	if !validUTF16Boundary(units, start) {
		return "", 0, len(units), fmt.Errorf("UTF-16 偏移不在字符边界")
	}
	end := min(len(units), start+limit)
	if !validUTF16Boundary(units, end) {
		end--
	}
	return string(utf16.Decode(units[start:end])), end, len(units), nil
}

func previewEnd(text string) int {
	units := utf16.Encode([]rune(text))
	if len(units) <= materialPreviewLimit {
		return len(units)
	}
	end := materialPreviewLimit
	if !validUTF16Boundary(units, end) {
		end--
	}
	for index := end - 1; index > 0; index-- {
		if units[index] == '\n' {
			return index + 1
		}
	}
	return end
}

func isConversationItem(item materialstore.Item) bool {
	return item.Type == "userMessage" || item.Type == "agentMessage"
}

type loadedMaterialItem struct {
	item      materialstore.Item
	text      string
	length    int
	projected int
}

type materialBodyReader func(materialstore.Item) (string, error)

func loadMaterialTurn(readBody materialBodyReader, turn materialstore.Turn) ([]loadedMaterialItem, int, error) {
	items := make([]loadedMaterialItem, 0, len(turn.Items))
	cost := 0
	for _, item := range turn.Items {
		text, err := readBody(item)
		if err != nil {
			return nil, 0, err
		}
		length := materialstore.UTF16Length(text)
		projected := length
		if !isConversationItem(item) && length > materialPreviewLimit {
			projected = previewEnd(text)
		}
		cost += max(1, projected)
		items = append(items, loadedMaterialItem{item: item, text: text, length: length, projected: projected})
	}
	return items, cost, nil
}

func segmentNotice(item materialstore.Item, collapsed bool) string {
	if !collapsed {
		return item.Notice
	}
	return appendMaterialNotice(item.Notice, fmt.Sprintf("已折叠，共 %d 个 UTF-16 字符；用 turnId + itemId 读取全文", item.Body.UTF16Length))
}

func newMaterialSegment(turn materialstore.Turn, loaded loadedMaterialItem, text string, start, end int, collapsed bool) materialSegment {
	segment := materialSegment{
		TurnID: turn.ID, Status: turn.Status, ItemID: loaded.item.ID, Type: loaded.item.Type,
		Text: text, StartOffset: start, EndOffset: end, Length: loaded.length,
		SourceLength: loaded.item.SourceUTF16Length, OmittedLength: loaded.item.OmittedUTF16Length,
		Notice: segmentNotice(loaded.item, collapsed), Collapsed: collapsed,
	}
	if collapsed {
		segment.ReadHint = "传入同一 materialId/version、turnId、itemId，并从 startOffset=0 按条分页读取"
	}
	return segment
}

func (s *Session) validateMaterialTargetLocked(target *AnnotationTarget) error {
	version, err := s.materialVersionLocked(MaterialReference{MaterialID: target.MaterialID, Version: target.Version})
	if err != nil {
		return err
	}
	manifest, err := s.materialStore.LoadManifest(version.Hash)
	if err != nil {
		return err
	}
	for _, turn := range manifest.Turns {
		if turn.ID != target.TurnID {
			continue
		}
		for _, item := range turn.Items {
			if item.ID != target.ItemID {
				continue
			}
			text, readErr := s.materialStore.ReadBody(item)
			if readErr != nil {
				return readErr
			}
			units := utf16.Encode([]rune(text))
			if !validUTF16Boundary(units, target.StartOffset) || !validUTF16Boundary(units, target.EndOffset) || target.StartOffset > target.EndOffset || string(utf16.Decode(units[target.StartOffset:target.EndOffset])) != target.Quote {
				return fmt.Errorf("引用原文与已发布版本不一致")
			}
			return nil
		}
		return fmt.Errorf("材料中没有该消息")
	}
	return fmt.Errorf("定位不在已公开范围内")
}

func readMaterialItemWith(readBody materialBodyReader, version MaterialVersion, manifest materialstore.Manifest, cursor materialCursor) (materialReadResponse, error) {
	if cursor.Turn < 0 || cursor.Turn >= len(manifest.Turns) {
		return materialReadResponse{}, fmt.Errorf("分页位置无效")
	}
	turn := manifest.Turns[cursor.Turn]
	if cursor.Item < 0 || cursor.Item >= len(turn.Items) {
		return materialReadResponse{}, fmt.Errorf("分页位置无效")
	}
	item := turn.Items[cursor.Item]
	text, err := readBody(item)
	if err != nil {
		return materialReadResponse{}, err
	}
	part, end, length, err := utf16Page(text, cursor.Offset, materialItemPageLimit)
	if err != nil {
		return materialReadResponse{}, err
	}
	segment := newMaterialSegment(turn, loadedMaterialItem{item: item, text: text, length: length}, part, cursor.Offset, end, false)
	next := ""
	if end < length {
		cursor.Offset = end
		next = encodeMaterialCursor(cursor)
	}
	return materialReadResponse{MaterialID: cursor.MaterialID, Version: versionInfo(version), NextCursor: next, Scope: "item", ItemComplete: end == length, Segments: []materialSegment{segment}}, nil
}

func readMaterialStreamWith(readBody materialBodyReader, version MaterialVersion, manifest materialstore.Manifest, cursor materialCursor, includeOutline bool) (materialReadResponse, error) {
	if cursor.Turn < 0 || cursor.Turn >= len(manifest.Turns) || cursor.Item < 0 || cursor.Item >= len(manifest.Turns[cursor.Turn].Items) || cursor.Offset < 0 {
		return materialReadResponse{}, fmt.Errorf("分页位置无效")
	}
	segments := []materialSegment{}
	used := 0
	for cursor.Turn < len(manifest.Turns) && len(segments) < materialSegmentLimit {
		turn := manifest.Turns[cursor.Turn]
		loadedItems, turnCost, err := loadMaterialTurn(readBody, turn)
		if err != nil {
			return materialReadResponse{}, err
		}
		if cursor.Item == 0 && cursor.Offset == 0 && used > 0 && used+turnCost > materialStreamHardLimit {
			break
		}
		for cursor.Item < len(loadedItems) && len(segments) < materialSegmentLimit {
			loaded := loadedItems[cursor.Item]
			if cursor.Offset > loaded.length {
				return materialReadResponse{}, fmt.Errorf("分页位置无效")
			}
			remaining := materialStreamHardLimit - used
			if remaining <= 0 {
				break
			}
			collapsed := cursor.Offset == 0 && !isConversationItem(loaded.item) && loaded.length > materialPreviewLimit
			if collapsed {
				part, end, _, pageErr := utf16Page(loaded.text, 0, min(loaded.projected, remaining))
				if pageErr != nil {
					return materialReadResponse{}, pageErr
				}
				if end == 0 && loaded.length > 0 {
					break
				}
				segments = append(segments, newMaterialSegment(turn, loaded, part, 0, end, true))
				used += max(1, end)
				cursor.Item++
				cursor.Offset = 0
				continue
			}
			part, end, _, pageErr := utf16Page(loaded.text, cursor.Offset, remaining)
			if pageErr != nil {
				return materialReadResponse{}, pageErr
			}
			if end == cursor.Offset && cursor.Offset < loaded.length {
				break
			}
			segments = append(segments, newMaterialSegment(turn, loaded, part, cursor.Offset, end, false))
			used += max(1, end-cursor.Offset)
			cursor.Offset = end
			if cursor.Offset == loaded.length {
				cursor.Item++
				cursor.Offset = 0
			} else {
				break
			}
		}
		if cursor.Item == len(turn.Items) {
			cursor.Turn++
			cursor.Item = 0
			cursor.Offset = 0
			if used >= materialStreamSoftLimit {
				break
			}
			continue
		}
		break
	}
	next := ""
	if cursor.Turn < len(manifest.Turns) {
		next = encodeMaterialCursor(cursor)
	}
	response := materialReadResponse{
		MaterialID: cursor.MaterialID, Version: versionInfo(version), NextCursor: next,
		Scope:                  "stream",
		PageEndsAtTurnBoundary: cursor.Turn >= len(manifest.Turns) || cursor.Item == 0 && cursor.Offset == 0,
		Segments:               segments,
	}
	if includeOutline {
		for _, turn := range manifest.Turns {
			response.Turns = append(response.Turns, materialOutline{ID: turn.ID, Label: turn.Label})
		}
	}
	return response, nil
}

func (s *Session) readMaterialItem(version MaterialVersion, manifest materialstore.Manifest, cursor materialCursor) (materialReadResponse, error) {
	return readMaterialItemWith(s.materialStore.ReadBody, version, manifest, cursor)
}

func (s *Session) readMaterialStream(version MaterialVersion, manifest materialstore.Manifest, cursor materialCursor, includeOutline bool) (materialReadResponse, error) {
	return readMaterialStreamWith(s.materialStore.ReadBody, version, manifest, cursor, includeOutline)
}

func (s *Session) readMaterial(ctx context.Context, in MaterialRead) (any, error) {
	s.mu.Lock()
	if s.closed || !s.callerValidLocked(ctx) {
		s.mu.Unlock()
		return nil, fmt.Errorf("共享已结束")
	}
	version, err := s.materialVersionLocked(MaterialReference{MaterialID: in.MaterialID, Version: in.Version})
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	manifest, err := s.materialStore.LoadManifest(version.Hash)
	if err != nil {
		return nil, err
	}
	cursor := materialCursor{Scope: "stream", MaterialID: in.MaterialID, Version: in.Version, Hash: version.Hash}
	if in.Cursor != "" {
		if in.StartOffset != 0 || in.ItemID != "" || in.TurnID != "" {
			return nil, fmt.Errorf("游标不能与按条定位参数同时使用")
		}
		cursor, err = decodeMaterialCursor(in.Cursor)
		if err != nil || cursor.MaterialID != in.MaterialID || cursor.Version != in.Version || cursor.Hash != version.Hash || (cursor.Scope != "stream" && cursor.Scope != "item") {
			return nil, fmt.Errorf("分页位置不属于这份材料的固定版本")
		}
	} else if in.ItemID != "" {
		if in.TurnID == "" || in.StartOffset < 0 {
			return nil, fmt.Errorf("按条读取需要 turnId、itemId 和有效的 startOffset")
		}
		cursor.Scope = "item"
		cursor.Turn = slices.IndexFunc(manifest.Turns, func(turn materialstore.Turn) bool { return turn.ID == in.TurnID })
		if cursor.Turn >= 0 {
			cursor.Item = slices.IndexFunc(manifest.Turns[cursor.Turn].Items, func(item materialstore.Item) bool { return item.ID == in.ItemID })
		}
		cursor.Offset = in.StartOffset
		if cursor.Turn < 0 || cursor.Item < 0 {
			return nil, fmt.Errorf("材料中没有该消息")
		}
	} else if in.TurnID != "" {
		cursor.Turn = slices.IndexFunc(manifest.Turns, func(turn materialstore.Turn) bool { return turn.ID == in.TurnID })
		if cursor.Turn < 0 {
			return nil, fmt.Errorf("定位不在已公开范围内")
		}
	} else if in.StartOffset != 0 {
		return nil, fmt.Errorf("startOffset 只能用于按条读取")
	}
	if cursor.Scope == "item" {
		return s.readMaterialItem(version, manifest, cursor)
	}
	return s.readMaterialStream(version, manifest, cursor, in.IncludeOutline && in.Cursor == "")
}
