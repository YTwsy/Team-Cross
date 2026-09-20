package materialstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	ManifestSchema      = 1
	InlineItemBytes     = 4 << 10
	InlineManifestBytes = 256 << 10
	MaxBlobBytes        = 8 << 20
	MaxManifestBytes    = 4 << 20
	MaxTurns            = 2048
	MaxItems            = 16384
)

const (
	BodyInline = "inline"
	BodyBlob   = "blob"
)

var supportedItemTypes = []string{
	"userMessage", "agentMessage", "toolCall", "toolResult",
	"commandExecution", "fileChange", "mcpToolCall", "dynamicToolCall",
	"webSearch", "imageView", "imageGeneration", "collabAgentToolCall",
	"plan", "unavailable",
}

// Manifest is the immutable structural description of one published version.
// Large item bodies are addressed by their raw UTF-8 SHA-256 instead of being
// embedded in the collaboration record.
type Manifest struct {
	Schema         int    `json:"schema"`
	Title          string `json:"title"`
	Provider       string `json:"provider"`
	SourceID       string `json:"sourceId"`
	StartTurnID    string `json:"startTurnId"`
	EndTurnID      string `json:"endTurnId"`
	ReadingStartID string `json:"readingStartId,omitempty"`
	Turns          []Turn `json:"turns"`
}

type Turn struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Label  string `json:"label"`
	Hash   string `json:"hash"`
	Items  []Item `json:"items"`
}

type Item struct {
	ID                 string `json:"id"`
	Type               string `json:"type"`
	Notice             string `json:"notice,omitempty"`
	Body               Body   `json:"body"`
	SourceUTF16Length  int    `json:"sourceUtf16Length,omitempty"`
	OmittedUTF16Length int    `json:"omittedUtf16Length,omitempty"`
}

type Body struct {
	Kind        string `json:"kind"`
	Text        string `json:"text,omitempty"`
	Hash        string `json:"hash,omitempty"`
	ByteLength  int    `json:"byteLength"`
	UTF16Length int    `json:"utf16Length"`
	LineCount   int    `json:"lineCount"`
}

type Bundle struct {
	Manifest Manifest          `json:"manifest"`
	Blobs    map[string]string `json:"blobs,omitempty"`
}

// Store owns immutable manifests and blobs below one authorization scope. A
// hash is never a capability: callers must first resolve an authorized
// manifest reference before using any read method.
type Store struct {
	root string
	mu   sync.Mutex
}

func New(root string) *Store { return &Store{root: root} }

func UTF16Length(text string) int { return len(utf16.Encode([]rune(text))) }

func LineCount(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

func HashBlob(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])
}

func InlineBody(text string) Body {
	return Body{Kind: BodyInline, Text: text, ByteLength: len([]byte(text)), UTF16Length: UTF16Length(text), LineCount: LineCount(text)}
}

func BlobBody(text string) Body {
	return Body{Kind: BodyBlob, Hash: HashBlob(text), ByteLength: len([]byte(text)), UTF16Length: UTF16Length(text), LineCount: LineCount(text)}
}

func domainHash(domain string, value any) (string, []byte, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", nil, err
	}
	h := sha256.New()
	_, _ = io.WriteString(h, domain)
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(b)
	return hex.EncodeToString(h.Sum(nil)), b, nil
}

func validHash(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func validateBody(body Body) error {
	if body.ByteLength < 0 || body.UTF16Length < 0 || body.LineCount < 0 {
		return fmt.Errorf("正文长度无效")
	}
	switch body.Kind {
	case BodyInline:
		if body.Hash != "" || !utf8.ValidString(body.Text) || len([]byte(body.Text)) > InlineItemBytes {
			return fmt.Errorf("内联正文无效")
		}
		if body.ByteLength != len([]byte(body.Text)) || body.UTF16Length != UTF16Length(body.Text) || body.LineCount != LineCount(body.Text) {
			return fmt.Errorf("内联正文长度不一致")
		}
	case BodyBlob:
		if body.Text != "" || !validHash(body.Hash) || body.ByteLength > MaxBlobBytes {
			return fmt.Errorf("blob 正文无效")
		}
	default:
		return fmt.Errorf("正文存储类型不受支持")
	}
	return nil
}

func finalize(manifest Manifest) (Manifest, string, []byte, error) {
	if manifest.Schema != ManifestSchema {
		return Manifest{}, "", nil, fmt.Errorf("材料清单 schema 不受支持")
	}
	if strings.TrimSpace(manifest.Title) == "" || len([]rune(manifest.Title)) > 160 || !utf8.ValidString(manifest.Title) {
		return Manifest{}, "", nil, fmt.Errorf("材料名称需要 1–160 字")
	}
	if manifest.Provider != "codex" && manifest.Provider != "claude" {
		return Manifest{}, "", nil, fmt.Errorf("材料来源 Provider 不受支持")
	}
	if manifest.SourceID == "" || len(manifest.Turns) == 0 || len(manifest.Turns) > MaxTurns {
		return Manifest{}, "", nil, fmt.Errorf("材料清单轮次无效")
	}

	result := manifest
	result.Turns = make([]Turn, len(manifest.Turns))
	seenTurns := make(map[string]bool, len(manifest.Turns))
	blobBodies := map[string]Body{}
	inlineBytes, itemCount := 0, 0
	for turnIndex, sourceTurn := range manifest.Turns {
		turn := sourceTurn
		turn.Items = append([]Item(nil), sourceTurn.Items...)
		if turn.ID == "" || len(turn.ID) > 200 || seenTurns[turn.ID] {
			return Manifest{}, "", nil, fmt.Errorf("材料轮次标识无效或重复")
		}
		seenTurns[turn.ID] = true
		if turn.Status != "completed" && turn.Status != "interrupted" && turn.Status != "failed" {
			return Manifest{}, "", nil, fmt.Errorf("不能发布仍在进行的轮次")
		}
		if !utf8.ValidString(turn.Label) || len([]rune(turn.Label)) > 100 {
			return Manifest{}, "", nil, fmt.Errorf("材料轮次标题无效")
		}
		if len(turn.Items) == 0 {
			return Manifest{}, "", nil, fmt.Errorf("材料轮次不能没有可见内容")
		}
		itemCount += len(turn.Items)
		if itemCount > MaxItems {
			return Manifest{}, "", nil, fmt.Errorf("材料消息总数超过 %d", MaxItems)
		}
		seenItems := make(map[string]bool, len(turn.Items))
		for itemIndex := range turn.Items {
			item := &turn.Items[itemIndex]
			if item.ID == "" || len(item.ID) > 200 || seenItems[item.ID] || len(item.Type) > 80 || len([]rune(item.Notice)) > 1000 || !utf8.ValidString(item.Notice) {
				return Manifest{}, "", nil, fmt.Errorf("材料消息无效")
			}
			seenItems[item.ID] = true
			if !slices.Contains(supportedItemTypes, item.Type) {
				return Manifest{}, "", nil, fmt.Errorf("材料含不支持的内容类型")
			}
			if item.SourceUTF16Length < 0 || item.OmittedUTF16Length < 0 {
				return Manifest{}, "", nil, fmt.Errorf("材料省略信息无效")
			}
			if err := validateBody(item.Body); err != nil {
				return Manifest{}, "", nil, err
			}
			if item.Body.Kind == BodyInline {
				inlineBytes += item.Body.ByteLength
				if inlineBytes > InlineManifestBytes {
					return Manifest{}, "", nil, fmt.Errorf("材料内联正文超过 %d KiB", InlineManifestBytes>>10)
				}
			} else if previous, exists := blobBodies[item.Body.Hash]; exists && previous != item.Body {
				return Manifest{}, "", nil, fmt.Errorf("同一 blob 的长度元数据不一致")
			} else {
				blobBodies[item.Body.Hash] = item.Body
			}
		}
		turnForHash := struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Label  string `json:"label"`
			Items  []Item `json:"items"`
		}{ID: turn.ID, Status: turn.Status, Label: turn.Label, Items: turn.Items}
		hash, _, err := domainHash("teamcross/material-turn/v1", turnForHash)
		if err != nil {
			return Manifest{}, "", nil, err
		}
		if turn.Hash != "" && turn.Hash != hash {
			return Manifest{}, "", nil, fmt.Errorf("材料轮次哈希不一致")
		}
		turn.Hash = hash
		result.Turns[turnIndex] = turn
	}
	if result.StartTurnID != result.Turns[0].ID || result.EndTurnID != result.Turns[len(result.Turns)-1].ID {
		return Manifest{}, "", nil, fmt.Errorf("公开范围与清单不一致")
	}
	if result.ReadingStartID != "" && !seenTurns[result.ReadingStartID] {
		return Manifest{}, "", nil, fmt.Errorf("建议阅读起点必须在公开范围内")
	}
	hash, encoded, err := domainHash("teamcross/material-manifest/v1", result)
	if err != nil {
		return Manifest{}, "", nil, err
	}
	if len(encoded) > MaxManifestBytes {
		return Manifest{}, "", nil, fmt.Errorf("材料清单超过 %d MiB", MaxManifestBytes>>20)
	}
	return result, hash, encoded, nil
}

// Finalize validates a manifest, fills deterministic turn hashes and returns
// the content-addressed manifest hash plus canonical JSON bytes.
func Finalize(manifest Manifest) (Manifest, string, []byte, error) {
	return finalize(manifest)
}

func (s *Store) manifestPath(hash string) (string, error) {
	if !validHash(hash) {
		return "", fmt.Errorf("材料清单哈希无效")
	}
	return filepath.Join(s.root, "manifests", hash[:2], hash+".json"), nil
}

func (s *Store) blobPath(hash string) (string, error) {
	if !validHash(hash) {
		return "", fmt.Errorf("材料 blob 哈希无效")
	}
	return filepath.Join(s.root, "blobs", hash[:2], hash), nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func writeImmutable(path string, data []byte) error {
	if current, err := os.ReadFile(path); err == nil {
		if !bytes.Equal(current, data) {
			return fmt.Errorf("内容寻址文件与现有内容冲突")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".teamcross-material-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err = temporary.Chmod(0600); err == nil {
		_, err = temporary.Write(data)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(temporaryPath, path); err != nil {
		return err
	}
	committed = true
	return syncDirectory(directory)
}

func validateBlob(body Body, text string) error {
	if !utf8.ValidString(text) || len([]byte(text)) > MaxBlobBytes || HashBlob(text) != body.Hash {
		return fmt.Errorf("blob 内容与清单不一致")
	}
	if body.ByteLength != len([]byte(text)) || body.UTF16Length != UTF16Length(text) || body.LineCount != LineCount(text) {
		return fmt.Errorf("blob 长度与清单不一致")
	}
	return nil
}

// PutBundle verifies every referenced body, writes blobs first and commits the
// immutable manifest last. The collaboration record must be written only after
// this method succeeds.
func (s *Store) PutBundle(bundle Bundle) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	manifest, hash, encoded, err := finalize(bundle.Manifest)
	if err != nil {
		return "", err
	}
	referenced := map[string]Body{}
	for _, turn := range manifest.Turns {
		for _, item := range turn.Items {
			if item.Body.Kind == BodyBlob {
				referenced[item.Body.Hash] = item.Body
			}
		}
	}
	for supplied := range bundle.Blobs {
		if _, ok := referenced[supplied]; !ok {
			return "", fmt.Errorf("上传包含清单未引用的 blob")
		}
	}
	for blobHash, body := range referenced {
		blobPath, pathErr := s.blobPath(blobHash)
		if pathErr != nil {
			return "", pathErr
		}
		text, supplied := bundle.Blobs[blobHash]
		if !supplied {
			data, readErr := os.ReadFile(blobPath)
			if readErr != nil {
				if os.IsNotExist(readErr) {
					return "", fmt.Errorf("清单引用的 blob 尚未上传")
				}
				return "", readErr
			}
			text = string(data)
		}
		if err = validateBlob(body, text); err != nil {
			return "", err
		}
		if supplied {
			if err = writeImmutable(blobPath, []byte(text)); err != nil {
				return "", err
			}
		}
	}
	manifestPath, err := s.manifestPath(hash)
	if err != nil {
		return "", err
	}
	if err = writeImmutable(manifestPath, encoded); err != nil {
		return "", err
	}
	return hash, nil
}

// PutBlob accepts one resumable upload unit. Authorization and whether the
// caller was allowed to omit this hash are intentionally enforced by the
// collaboration layer, not by the content-addressed store.
func (s *Store) PutBlob(hash, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validHash(hash) || !utf8.ValidString(text) || len([]byte(text)) > MaxBlobBytes || HashBlob(text) != hash {
		return fmt.Errorf("blob 内容与地址不一致")
	}
	path, err := s.blobPath(hash)
	if err != nil {
		return err
	}
	return writeImmutable(path, []byte(text))
}

func (s *Store) HasBlob(hash string) bool {
	path, err := s.blobPath(hash)
	if err != nil {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() <= MaxBlobBytes
}

func (s *Store) LoadManifest(hash string) (Manifest, error) {
	path, err := s.manifestPath(hash)
	if err != nil {
		return Manifest{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxManifestBytes+1))
	if err != nil {
		return Manifest{}, err
	}
	if len(data) > MaxManifestBytes {
		return Manifest{}, fmt.Errorf("材料清单超过读取上限")
	}
	var manifest Manifest
	if err = json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("材料清单无法解析: %w", err)
	}
	manifest, actualHash, canonical, err := finalize(manifest)
	if err != nil {
		return Manifest{}, err
	}
	if actualHash != hash || !bytes.Equal(data, canonical) {
		return Manifest{}, fmt.Errorf("材料清单哈希校验失败")
	}
	return manifest, nil
}

func (s *Store) ReadBody(item Item) (string, error) {
	if err := validateBody(item.Body); err != nil {
		return "", err
	}
	if item.Body.Kind == BodyInline {
		return item.Body.Text, nil
	}
	path, err := s.blobPath(item.Body.Hash)
	if err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxBlobBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > MaxBlobBytes {
		return "", fmt.Errorf("材料 blob 超过读取上限")
	}
	text := string(data)
	if err = validateBlob(item.Body, text); err != nil {
		return "", err
	}
	return text, nil
}

func (s *Store) LoadBundle(hash string) (Bundle, error) {
	manifest, err := s.LoadManifest(hash)
	if err != nil {
		return Bundle{}, err
	}
	bundle := Bundle{Manifest: manifest, Blobs: map[string]string{}}
	for _, turn := range manifest.Turns {
		for _, item := range turn.Items {
			if item.Body.Kind != BodyBlob {
				continue
			}
			if _, ok := bundle.Blobs[item.Body.Hash]; ok {
				continue
			}
			text, readErr := s.ReadBody(item)
			if readErr != nil {
				return Bundle{}, readErr
			}
			bundle.Blobs[item.Body.Hash] = text
		}
	}
	return bundle, nil
}
