package collab

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/materialstore"
	"teamcross/internal/sharing"
)

const (
	materialUploadLifetime   = 24 * time.Hour
	materialVersionBlobLimit = 32 << 20
	materialUploadLimit      = 32
)

type publicationUploadRecord struct {
	ID           string                 `json:"id"`
	MemberID     string                 `json:"memberId"`
	RequestID    string                 `json:"requestId"`
	RequestHash  string                 `json:"requestHash"`
	MaterialID   string                 `json:"materialId,omitempty"`
	BaseVersion  int                    `json:"baseVersion,omitempty"`
	ManifestHash string                 `json:"manifestHash"`
	Manifest     materialstore.Manifest `json:"manifest"`
	Required     []string               `json:"required"`
	Reusable     []string               `json:"reusable,omitempty"`
	Uploaded     map[string]bool        `json:"uploaded"`
	CreatedAt    time.Time              `json:"createdAt"`
	ExpiresAt    time.Time              `json:"expiresAt"`
	Committed    bool                   `json:"committed,omitempty"`
	Result       map[string]any         `json:"result,omitempty"`
}

func (s *Session) uploadRecordPath(id string) (string, error) {
	if _, err := uuid.Parse(id); err != nil {
		return "", fmt.Errorf("上传标识无效")
	}
	return filepath.Join(s.app.Config.DataDir, "collaborations", s.record.ID, "materials", "staging", id, "upload.json"), nil
}

func (s *Session) uploadPayloadStore(id string) (*materialstore.Store, error) {
	path, err := s.uploadRecordPath(id)
	if err != nil {
		return nil, err
	}
	return materialstore.New(filepath.Join(filepath.Dir(path), "payload")), nil
}

// cleanupAndMeasureUploads removes only expired, well-formed upload
// directories owned by this space. It also reserves bounded staging capacity
// before a new manifest is accepted, so abandoned uploads cannot bypass the
// active-material quota by filling the permanent blob store.
func (s *Session) cleanupAndMeasureUploads(now time.Time, excludeID string) (int, int64, error) {
	root := filepath.Join(s.app.Config.DataDir, "collaborations", s.record.ID, "materials", "staging")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	count := 0
	var bytes int64
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		if _, parseErr := uuid.Parse(id); parseErr != nil {
			continue
		}
		var record publicationUploadRecord
		if readJSON(filepath.Join(root, id, "upload.json"), &record) != nil || record.ID != id {
			// A damaged local record is retained for inspection and consumes one
			// slot rather than being deleted speculatively.
			if id != excludeID {
				count++
			}
			continue
		}
		if !now.Before(record.ExpiresAt) {
			if removeErr := os.RemoveAll(filepath.Join(root, id)); removeErr != nil {
				return 0, 0, removeErr
			}
			continue
		}
		if record.Committed || id == excludeID {
			continue
		}
		count++
		bytes += requiredBlobBytes(record.Manifest, record.Required)
	}
	return count, bytes, nil
}

func requiredBlobBytes(manifest materialstore.Manifest, required []string) int64 {
	wanted := make(map[string]bool, len(required))
	for _, hash := range required {
		wanted[hash] = true
	}
	seen := map[string]bool{}
	var total int64
	for _, turn := range manifest.Turns {
		for _, item := range turn.Items {
			if item.Body.Kind == materialstore.BodyBlob && wanted[item.Body.Hash] && !seen[item.Body.Hash] {
				seen[item.Body.Hash] = true
				total += int64(item.Body.ByteLength)
			}
		}
	}
	return total
}

func (s *Session) saveUploadRecord(record publicationUploadRecord) error {
	path, err := s.uploadRecordPath(record.ID)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return writeJSONFile(path, record)
}

func (s *Session) loadUploadRecord(id string) (publicationUploadRecord, error) {
	var record publicationUploadRecord
	path, err := s.uploadRecordPath(id)
	if err != nil {
		return record, err
	}
	if err = readJSON(path, &record); err != nil || record.ID != id || record.ManifestHash == "" {
		return record, fmt.Errorf("上传会话不存在，请重新协商")
	}
	return record, nil
}

func manifestBlobHashes(manifest materialstore.Manifest) ([]string, int64) {
	seen := map[string]bool{}
	hashes := []string{}
	var bytes int64
	for _, turn := range manifest.Turns {
		for _, item := range turn.Items {
			if item.Body.Kind != materialstore.BodyBlob || seen[item.Body.Hash] {
				continue
			}
			seen[item.Body.Hash] = true
			hashes = append(hashes, item.Body.Hash)
			bytes += int64(item.Body.ByteLength)
		}
	}
	return hashes, bytes
}

// reusableBlobsLocked returns only hashes the current author can already prove
// through one of their own active publications. Store-wide existence is never
// exposed by negotiate, including content reachable only from another author
// or a withdrawn material.
func (s *Session) reusableBlobsLocked(memberID string) (map[string]bool, error) {
	reusable := map[string]bool{}
	for _, material := range s.record.Materials {
		if material.AuthorID != memberID || material.WithdrawnAt != nil {
			continue
		}
		for _, version := range material.Versions {
			manifest, err := s.materialStore.LoadManifest(version.Hash)
			if err != nil {
				return nil, err
			}
			hashes, _ := manifestBlobHashes(manifest)
			for _, hash := range hashes {
				if s.materialStore.HasBlob(hash) {
					reusable[hash] = true
				}
			}
		}
	}
	return reusable, nil
}

func uploadID(spaceID, memberID, requestID string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("teamcross/material-upload/v1\x00"+spaceID+"\x00"+memberID+"\x00"+requestID)).String()
}

func (s *Session) negotiatePublication(ctx context.Context, in publicationNegotiate) (publicationNegotiation, error) {
	if _, err := uuid.Parse(in.RequestID); err != nil {
		return publicationNegotiation{}, fmt.Errorf("发布需要 UUID requestId")
	}
	if in.MaterialID != "" {
		if _, err := uuid.Parse(in.MaterialID); err != nil {
			return publicationNegotiation{}, fmt.Errorf("无效的材料 ID")
		}
	} else if in.BaseVersion != 0 {
		return publicationNegotiation{}, fmt.Errorf("新材料不接受 baseVersion")
	}
	manifest, manifestHash, _, err := materialstore.Finalize(in.Manifest)
	if err != nil {
		return publicationNegotiation{}, err
	}
	if _, err = uuid.Parse(manifest.SourceID); err != nil {
		return publicationNegotiation{}, fmt.Errorf("材料需要准确的来源会话身份")
	}
	hashes, blobBytes := manifestBlobHashes(manifest)
	if blobBytes > materialVersionBlobLimit {
		return publicationNegotiation{}, fmt.Errorf("一个材料版本引用的唯一 blob 不能超过 32 MiB")
	}
	upload := publicationUpload{Bundle: materialstore.Bundle{Manifest: manifest}, RequestID: in.RequestID, MaterialID: in.MaterialID, BaseVersion: in.BaseVersion}
	requestHash := publicationRequestHash(manifestHash, upload)

	s.materialUploadMu.Lock()
	defer s.materialUploadMu.Unlock()
	id := uploadID(s.record.ID, sharing.MemberID(ctx), in.RequestID)
	pendingCount, pendingBytes, err := s.cleanupAndMeasureUploads(time.Now(), id)
	if err != nil {
		return publicationNegotiation{}, err
	}
	s.mu.Lock()
	_, _, existingResult, err := s.publicationPreflightLocked(ctx, upload, manifest, requestHash)
	if err != nil {
		s.mu.Unlock()
		return publicationNegotiation{}, err
	}
	if existingResult != nil {
		s.mu.Unlock()
		return publicationNegotiation{State: "published", ManifestHash: manifestHash, Result: existingResult.(map[string]any)}, nil
	}
	memberID := sharing.MemberID(ctx)
	reusable, err := s.reusableBlobsLocked(memberID)
	s.mu.Unlock()
	if err != nil {
		return publicationNegotiation{}, err
	}

	if record, loadErr := s.loadUploadRecord(id); loadErr == nil && time.Now().Before(record.ExpiresAt) {
		if record.MemberID != memberID || record.RequestHash != requestHash || record.ManifestHash != manifestHash {
			return publicationNegotiation{}, fmt.Errorf("requestId 已用于不同发布内容，请使用原请求查询")
		}
		stillReusable := record.Reusable[:0]
		for _, hash := range record.Reusable {
			if reusable[hash] {
				stillReusable = append(stillReusable, hash)
			} else if !slices.Contains(record.Required, hash) {
				record.Required = append(record.Required, hash)
			}
		}
		record.Reusable = stillReusable
		if pendingBytes+requiredBlobBytes(record.Manifest, record.Required) > materialSpaceBlobLimit {
			return publicationNegotiation{}, fmt.Errorf("空间未完成上传暂存不能超过 256 MiB")
		}
		payload, payloadErr := s.uploadPayloadStore(id)
		if payloadErr != nil {
			return publicationNegotiation{}, payloadErr
		}
		missing := []string{}
		for _, hash := range record.Required {
			if !record.Uploaded[hash] || !payload.HasBlob(hash) {
				missing = append(missing, hash)
			}
		}
		if err = s.saveUploadRecord(record); err != nil {
			return publicationNegotiation{}, err
		}
		return publicationNegotiation{State: "uploading", UploadID: id, ManifestHash: manifestHash, Missing: missing, ExpiresAt: record.ExpiresAt, Result: record.Result}, nil
	}

	required, reused := []string{}, []string{}
	for _, hash := range hashes {
		if reusable[hash] {
			reused = append(reused, hash)
		} else {
			required = append(required, hash)
		}
	}
	if pendingCount >= materialUploadLimit {
		return publicationNegotiation{}, fmt.Errorf("空间同时最多保留 %d 个未完成材料上传", materialUploadLimit)
	}
	if pendingBytes+requiredBlobBytes(manifest, required) > materialSpaceBlobLimit {
		return publicationNegotiation{}, fmt.Errorf("空间未完成上传暂存不能超过 256 MiB")
	}
	now := time.Now()
	record := publicationUploadRecord{
		ID: id, MemberID: memberID, RequestID: in.RequestID, RequestHash: requestHash,
		MaterialID: in.MaterialID, BaseVersion: in.BaseVersion, ManifestHash: manifestHash,
		Manifest: manifest, Required: required, Reusable: reused, Uploaded: map[string]bool{},
		CreatedAt: now, ExpiresAt: now.Add(materialUploadLifetime),
	}
	if err = s.saveUploadRecord(record); err != nil {
		return publicationNegotiation{}, err
	}
	return publicationNegotiation{State: "uploading", UploadID: id, ManifestHash: manifestHash, Missing: required, ExpiresAt: record.ExpiresAt}, nil
}

func (s *Session) uploadPublicationBlob(ctx context.Context, uploadID, hash, text string) error {
	s.materialUploadMu.Lock()
	defer s.materialUploadMu.Unlock()
	record, err := s.loadUploadRecord(uploadID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	valid := !s.closed && s.callerValidLocked(ctx) && record.MemberID == sharing.MemberID(ctx)
	s.mu.Unlock()
	if !valid {
		return fmt.Errorf("上传会话不属于当前成员或共享已结束")
	}
	if !time.Now().Before(record.ExpiresAt) {
		return fmt.Errorf("上传会话已过期，请重新协商")
	}
	if !slices.Contains(record.Required, hash) {
		return fmt.Errorf("此 blob 不属于协商后的缺失集合")
	}
	payload, err := s.uploadPayloadStore(uploadID)
	if err != nil {
		return err
	}
	if err = payload.PutBlob(hash, text); err != nil {
		return err
	}
	if record.Uploaded == nil {
		record.Uploaded = map[string]bool{}
	}
	record.Uploaded[hash] = true
	return s.saveUploadRecord(record)
}

func (s *Session) commitPublicationUpload(ctx context.Context, id string) (any, error) {
	s.materialUploadMu.Lock()
	defer s.materialUploadMu.Unlock()
	record, err := s.loadUploadRecord(id)
	if err != nil {
		return nil, err
	}
	if record.MemberID != sharing.MemberID(ctx) {
		return nil, fmt.Errorf("上传会话不属于当前成员")
	}
	if !time.Now().Before(record.ExpiresAt) {
		return nil, fmt.Errorf("上传会话已过期，请重新协商")
	}
	if record.Committed && record.Result != nil {
		return record.Result, nil
	}
	payload, err := s.uploadPayloadStore(id)
	if err != nil {
		return nil, err
	}
	for _, hash := range record.Required {
		if !record.Uploaded[hash] || !payload.HasBlob(hash) {
			return nil, fmt.Errorf("仍有协商要求的 blob 未完成上传")
		}
	}
	s.mu.Lock()
	reusable, reuseErr := s.reusableBlobsLocked(record.MemberID)
	s.mu.Unlock()
	if reuseErr != nil {
		return nil, reuseErr
	}
	for _, hash := range record.Reusable {
		if !reusable[hash] || !s.materialStore.HasBlob(hash) {
			return nil, fmt.Errorf("可复用 blob 已不再由当前成员的未撤回版本授权，请重新协商")
		}
	}
	bundle := materialstore.Bundle{Manifest: record.Manifest, Blobs: map[string]string{}}
	required := make(map[string]bool, len(record.Required))
	for _, hash := range record.Required {
		required[hash] = true
	}
	for _, turn := range record.Manifest.Turns {
		for _, item := range turn.Items {
			if !required[item.Body.Hash] {
				continue
			}
			if _, exists := bundle.Blobs[item.Body.Hash]; exists {
				continue
			}
			text, readErr := payload.ReadBody(item)
			if readErr != nil {
				return nil, readErr
			}
			bundle.Blobs[item.Body.Hash] = text
		}
	}
	upload := publicationUpload{Bundle: bundle, RequestID: record.RequestID, MaterialID: record.MaterialID, BaseVersion: record.BaseVersion}
	result, err := s.publishBundle(ctx, upload, false)
	if err != nil {
		return nil, err
	}
	record.Committed = true
	record.Result = result.(map[string]any)
	// The authoritative publication now owns the immutable manifest and blobs.
	// Keep only the small idempotency result until this upload session expires.
	record.Manifest = materialstore.Manifest{}
	record.Required = nil
	record.Reusable = nil
	record.Uploaded = nil
	if err = s.saveUploadRecord(record); err != nil {
		// The publication record is already authoritative. A retry will be
		// recovered by requestId during negotiate even if this cache write fails.
		return result, nil
	}
	path, _ := s.uploadRecordPath(id)
	_ = os.RemoveAll(filepath.Join(filepath.Dir(path), "payload"))
	return result, nil
}
