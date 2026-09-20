package collab

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/materialstore"
	"teamcross/internal/nativecodex"
)

func contentHash(value any) string {
	b, _ := json.Marshal(value)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// FreezeSource is local-only. Neither its endpoint nor a draft ID is exposed by
// the shared-space router or by the runtime's tools. No thread/resume is used.
func (a *App) FreezeSource(ctx context.Context, provider, sourceID string) (PublicationDraft, error) {
	var draft PublicationDraft
	provider, err := providerName(provider)
	if err != nil {
		return draft, err
	}
	if _, err := uuid.Parse(sourceID); err != nil {
		return draft, fmt.Errorf("请选择准确的本机来源会话")
	}
	var read struct {
		Thread Source `json:"thread"`
	}
	if err := a.sourceCall(ctx, provider, "thread/read", map[string]any{"threadId": sourceID, "includeTurns": false}, &read); err != nil {
		return draft, err
	}
	if read.Thread.ID != sourceID {
		return draft, fmt.Errorf("来源会话身份不一致")
	}
	var turns []MaterialTurn
	cursor, size := "", 0
	seen := map[string]bool{}
	for {
		var page struct {
			Data       []json.RawMessage `json:"data"`
			NextCursor string            `json:"nextCursor"`
		}
		params := map[string]any{"threadId": sourceID, "itemsView": "full", "sortDirection": "desc", "limit": 100}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := a.sourceCall(ctx, provider, "thread/turns/list", params, &page); err != nil {
			return draft, err
		}
		for _, raw := range page.Data {
			size += len(raw)
			if size > 32<<20 || len(turns) >= materialstore.MaxTurns {
				return draft, fmt.Errorf("来源超过 %d 轮或 32 MiB，请选择较小的调查会话", materialstore.MaxTurns)
			}
			turn, err := exportTurn(raw)
			if err != nil {
				return draft, err
			}
			if seen[turn.ID] {
				return draft, fmt.Errorf("来源分页正在变化，请重新读取")
			}
			seen[turn.ID] = true
			turns = append(turns, turn)
		}
		if page.NextCursor == "" {
			break
		}
		if page.NextCursor == cursor {
			return draft, fmt.Errorf("来源分页没有推进")
		}
		cursor = page.NextCursor
	}
	// The native app-server can report an active persisted turn as interrupted.
	// Consult the saved boundary without resuming it before choosing public turns.
	if provider == "codex" && read.Thread.Path != "" && len(turns) > 0 {
		id, status, err := nativecodex.SourceTurn(read.Thread.Path, sourceID)
		if err != nil {
			return draft, err
		}
		if id != turns[0].ID {
			return draft, fmt.Errorf("来源末轮正在变化，请重新读取")
		}
		turns[0].Status = status
	}
	slices.Reverse(turns)
	// Only a terminal prefix is eligible, never jump across an unfinished turn.
	end := 0
	for _, t := range turns {
		if t.Status != "completed" && t.Status != "interrupted" && t.Status != "failed" {
			break
		}
		end++
	}
	turns = turns[:end]
	if len(turns) == 0 {
		return draft, fmt.Errorf("会话还没有已结束的对话，请等待当前轮结束")
	}
	title := strings.TrimSpace(read.Thread.Name)
	if title == "" {
		title = shortText(read.Thread.Preview, 72)
	}
	if title == "" {
		title = "会话材料"
	}
	draft = PublicationDraft{ID: uuid.NewString(), FrozenAt: time.Now(), MaterialContent: MaterialContent{Title: shortText(title, 159), Provider: provider, SourceID: sourceID, StartTurnID: turns[0].ID, EndTurnID: turns[len(turns)-1].ID, Turns: turns}}
	return a.saveDraft(draft)
}

func exportTurn(raw json.RawMessage) (MaterialTurn, error) {
	var wire struct {
		ID, Status string
		Items      []map[string]json.RawMessage
	}
	if json.Unmarshal(raw, &wire) != nil || wire.ID == "" || len(wire.ID) > 200 {
		return MaterialTurn{}, fmt.Errorf("来源包含无法解析的轮次")
	}
	t := MaterialTurn{ID: wire.ID, Status: wire.Status, Items: []MaterialItem{}}
	ids := map[string]bool{}
	for index, rawItem := range wire.Items {
		str := func(key string) string { var s string; _ = json.Unmarshal(rawItem[key], &s); return s }
		typ, id := str("type"), str("id")
		if typ == "reasoning" {
			continue
		} // Internal reasoning is not published.
		if id == "" || ids[id] {
			id = fmt.Sprintf("item-%d", index)
		}
		ids[id] = true
		i := MaterialItem{ID: id, Type: typ}
		switch typ {
		case "userMessage":
			var blocks []map[string]json.RawMessage
			if json.Unmarshal(rawItem["content"], &blocks) != nil {
				i.Notice = "用户消息内容无法解析"
			}
			for _, b := range blocks {
				var kind, text string
				_ = json.Unmarshal(b["type"], &kind)
				_ = json.Unmarshal(b["text"], &text)
				if kind == "text" {
					i.Text += text
				} else {
					i.Notice = "附件未导出；没有读取消息中的文件路径或外部地址"
					i.Text += "\n[附件未导出]"
				}
			}
		case "agentMessage", "toolCall", "toolResult":
			i.Text = str("text")
		case "commandExecution", "fileChange", "mcpToolCall", "dynamicToolCall", "webSearch", "imageView", "imageGeneration", "collabAgentToolCall", "plan":
			// A schema allowlist keeps configuration/credential envelopes out. Tool
			// arguments and saved output remain visible and must be reviewed.
			// A struct fixes the review order so command identity is visible before
			// potentially very long output.
			visible := struct {
				Command          json.RawMessage `json:"command,omitempty"`
				Cwd              json.RawMessage `json:"cwd,omitempty"`
				Status           json.RawMessage `json:"status,omitempty"`
				ExitCode         json.RawMessage `json:"exitCode,omitempty"`
				Server           json.RawMessage `json:"server,omitempty"`
				Tool             json.RawMessage `json:"tool,omitempty"`
				Query            json.RawMessage `json:"query,omitempty"`
				Action           json.RawMessage `json:"action,omitempty"`
				Arguments        json.RawMessage `json:"arguments,omitempty"`
				Changes          json.RawMessage `json:"changes,omitempty"`
				Text             json.RawMessage `json:"text,omitempty"`
				AggregatedOutput json.RawMessage `json:"aggregatedOutput,omitempty"`
				Result           json.RawMessage `json:"result,omitempty"`
				Error            json.RawMessage `json:"error,omitempty"`
				ContentItems     json.RawMessage `json:"contentItems,omitempty"`
			}{Command: rawItem["command"], Cwd: rawItem["cwd"], Status: rawItem["status"], ExitCode: rawItem["exitCode"], Server: rawItem["server"], Tool: rawItem["tool"], Query: rawItem["query"], Action: rawItem["action"], Arguments: rawItem["arguments"], Changes: rawItem["changes"], Text: rawItem["text"], AggregatedOutput: rawItem["aggregatedOutput"], Result: rawItem["result"], Error: rawItem["error"], ContentItems: rawItem["contentItems"]}
			b, _ := json.MarshalIndent(visible, "", "  ")
			i.Text = string(b)
			if typ == "imageView" || typ == "imageGeneration" {
				i.Notice = "图片文件未导出，没有从原目录收集附件"
			}
		default:
			i.Type = "unavailable"
			i.Notice = "此内容类型尚不能导出，原始数据未包含在材料中"
		}
		t.Items = append(t.Items, i)
	}
	if len(t.Items) == 0 {
		t.Items = []MaterialItem{{ID: "unavailable", Type: "unavailable", Text: "", Notice: "没有保存可导出的可见消息"}}
	}
	return t, nil
}

type publicationDraftRecord struct {
	ID       string    `json:"id"`
	FrozenAt time.Time `json:"frozenAt"`
	Hash     string    `json:"hash"`
}

func (a *App) draftStore() *materialstore.Store {
	return materialstore.New(filepath.Join(a.Config.DataDir, "publication-drafts", "materials"))
}

func (a *App) draftRecordPath(id string) string {
	return filepath.Join(a.Config.DataDir, "publication-drafts", "records", id+".json")
}

func (a *App) saveDraft(d PublicationDraft) (PublicationDraft, error) {
	if _, err := uuid.Parse(d.ID); err != nil {
		return PublicationDraft{}, fmt.Errorf("本机预览标识无效")
	}
	bundle, normalized, hash, err := buildMaterialBundle(d.MaterialContent)
	if err != nil {
		return PublicationDraft{}, err
	}
	store := a.draftStore()
	if storedHash, putErr := store.PutBundle(bundle); putErr != nil {
		return PublicationDraft{}, putErr
	} else if storedHash != hash {
		return PublicationDraft{}, fmt.Errorf("本机预览哈希不一致")
	}
	d.MaterialContent = normalized
	d.Hash = hash
	directory := filepath.Dir(a.draftRecordPath(d.ID))
	if err = os.MkdirAll(directory, 0700); err != nil {
		return PublicationDraft{}, err
	}
	err = writeJSONFile(a.draftRecordPath(d.ID), publicationDraftRecord{ID: d.ID, FrozenAt: d.FrozenAt, Hash: hash})
	return d, err
}

func (a *App) loadDraftRecord(id string) (publicationDraftRecord, materialstore.Manifest, error) {
	var record publicationDraftRecord
	if _, err := uuid.Parse(id); err != nil {
		return record, materialstore.Manifest{}, fmt.Errorf("无效的本机预览")
	}
	if err := readJSON(a.draftRecordPath(id), &record); err != nil {
		return record, materialstore.Manifest{}, fmt.Errorf("本机预览不存在，请重新读取来源")
	}
	if record.ID != id || record.Hash == "" {
		return record, materialstore.Manifest{}, fmt.Errorf("本机预览内容已变化")
	}
	manifest, err := a.draftStore().LoadManifest(record.Hash)
	if err != nil {
		return record, materialstore.Manifest{}, fmt.Errorf("本机预览内容已变化: %w", err)
	}
	return record, manifest, nil
}

func (a *App) loadDraft(id string) (PublicationDraft, error) {
	var d PublicationDraft
	record, manifest, err := a.loadDraftRecord(id)
	if err != nil {
		return d, err
	}
	content, err := materializeManifest(a.draftStore(), manifest)
	if err != nil {
		return d, fmt.Errorf("本机预览内容已变化: %w", err)
	}
	return PublicationDraft{ID: record.ID, FrozenAt: record.FrozenAt, Hash: record.Hash, MaterialContent: content}, nil
}

func (a *App) loadDraftBundle(id string) (publicationDraftRecord, materialstore.Bundle, error) {
	record, _, err := a.loadDraftRecord(id)
	if err != nil {
		return record, materialstore.Bundle{}, err
	}
	bundle, err := a.draftStore().LoadBundle(record.Hash)
	return record, bundle, err
}
func (a *App) PreviewPublication(in PublicationSelection) (PublicationDraft, error) {
	d, err := a.loadDraft(in.DraftID)
	if err != nil {
		return d, err
	}
	start := slices.IndexFunc(d.Turns, func(t MaterialTurn) bool { return t.ID == in.StartTurnID })
	end := slices.IndexFunc(d.Turns, func(t MaterialTurn) bool { return t.ID == in.EndTurnID })
	if start < 0 || end < start {
		return PublicationDraft{}, fmt.Errorf("请选择连续且已完成的公开范围")
	}
	d.ID = uuid.NewString()
	d.Title, d.StartTurnID, d.EndTurnID, d.ReadingStartID = strings.TrimSpace(in.Title), in.StartTurnID, in.EndTurnID, in.ReadingStartID
	d.Turns = d.Turns[start : end+1]
	if err := validateMaterial(d.MaterialContent); err != nil {
		return PublicationDraft{}, err
	}
	return a.saveDraft(d)
}

func (a *App) Publish(ctx context.Context, id string, in PublishInput) (any, error) {
	record, bundle, err := a.loadDraftBundle(in.PreviewID)
	if err != nil {
		return nil, err
	}
	if in.PreviewHash != record.Hash {
		return nil, fmt.Errorf("发布内容与预览不一致")
	}
	upload := publicationUpload{Bundle: bundle, RequestID: in.RequestID, MaterialID: in.MaterialID, BaseVersion: in.BaseVersion}
	a.mu.Lock()
	s, j := a.sessions[id], a.joined[id]
	a.mu.Unlock()
	if j != nil {
		var negotiation publicationNegotiation
		err = j.request(ctx, "POST", "/v2/material-negotiate", publicationNegotiate{Manifest: bundle.Manifest, RequestID: in.RequestID, MaterialID: in.MaterialID, BaseVersion: in.BaseVersion}, &negotiation)
		if err != nil {
			return nil, err
		}
		if negotiation.State == "published" && negotiation.Result != nil {
			return negotiation.Result, nil
		}
		if negotiation.State != "uploading" || negotiation.UploadID == "" || negotiation.ManifestHash != record.Hash {
			return nil, fmt.Errorf("主机返回的材料上传协商无效")
		}
		for _, hash := range negotiation.Missing {
			text, ok := bundle.Blobs[hash]
			if !ok {
				return nil, fmt.Errorf("本机预览缺少主机要求的 blob")
			}
			path := fmt.Sprintf("/v2/material-uploads/%s/blobs/%s", negotiation.UploadID, hash)
			if err = j.requestBlob(ctx, path, text); err != nil {
				return nil, err
			}
		}
		var result any
		err = j.request(ctx, "POST", fmt.Sprintf("/v2/material-uploads/%s/commit", negotiation.UploadID), nil, &result)
		return result, err
	}
	if s == nil {
		return nil, fmt.Errorf("空间不存在")
	}
	return s.publish(ctx, upload)
}
