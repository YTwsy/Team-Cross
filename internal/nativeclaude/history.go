// Package nativeclaude adapts the versioned Claude Code background-job transport.
// It never starts a second SDK runtime beside a native TUI worker.
package nativeclaude

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"
)

const maxHistory = 64 << 20

type Item struct {
	ID      string              `json:"id"`
	Type    string              `json:"type"`
	Text    string              `json:"text,omitempty"`
	Content []map[string]string `json:"content,omitempty"`
}
type Turn struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Items  []Item `json:"items"`
}
type History struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Preview         string  `json:"preview"`
	Cwd             string  `json:"cwd"`
	Path            string  `json:"path,omitempty"`
	UpdatedAt       int64   `json:"updatedAt"`
	Model           string  `json:"model,omitempty"`
	ReasoningEffort *string `json:"reasoningEffort,omitempty"`
	Fingerprint     string  `json:"-"`
	Turns           []Turn  `json:"turns,omitempty"`
}
type entry struct {
	Type        string  `json:"type"`
	UUID        string  `json:"uuid"`
	Parent      string  `json:"parentUuid"`
	SessionID   string  `json:"sessionId"`
	Cwd         string  `json:"cwd"`
	Sidechain   bool    `json:"isSidechain"`
	Meta        bool    `json:"isMeta"`
	Effort      *string `json:"effort"`
	CustomTitle string  `json:"customTitle"`
	Message     struct {
		Model      string          `json:"model"`
		Content    json.RawMessage `json:"content"`
		StopReason string          `json:"stop_reason"`
	} `json:"message"`
}
type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	ToolUseID string          `json:"tool_use_id"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
}

func content(raw json.RawMessage) []block {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []block{{Type: "text", Text: text}}
	}
	var blocks []block
	_ = json.Unmarshal(raw, &blocks)
	return blocks
}

// ReadFile follows the latest main conversation chain, excluding sidechains and
// obsolete branches. Attachment/system snapshots and their credentials are not
// exposed as collaboration history.
func ReadFile(path string) (History, error) {
	h := History{ID: strings.TrimSuffix(filepath.Base(path), ".jsonl"), Path: path}
	if _, err := uuid.Parse(h.ID); err != nil {
		return h, fmt.Errorf("无效的 Claude 会话 ID")
	}
	f, err := os.Open(path)
	if err != nil {
		return h, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxHistory {
		return h, fmt.Errorf("Claude 历史文件不可读取或超过 64 MiB")
	}
	h.UpdatedAt = st.ModTime().Unix()
	hash := sha256.New()
	scan := bufio.NewScanner(io.TeeReader(io.LimitReader(f, maxHistory+1), hash))
	scan.Buffer(make([]byte, 64<<10), 8<<20)
	entries := map[string]entry{}
	last := ""
	malformed := false
	for scan.Scan() {
		if malformed {
			return h, fmt.Errorf("Claude 历史包含损坏的记录")
		}
		var e entry
		if json.Unmarshal(scan.Bytes(), &e) != nil {
			malformed = true
			continue
		} // A final in-flight line may be incomplete.
		if e.Sidechain {
			continue
		}
		if e.CustomTitle != "" {
			h.Name = e.CustomTitle
		}
		if e.UUID != "" {
			entries[e.UUID] = e
			if e.Type == "user" || e.Type == "assistant" {
				last = e.UUID
			}
		}
	}
	if err := scan.Err(); err != nil {
		return h, err
	}
	h.Fingerprint = hex.EncodeToString(hash.Sum(nil))
	chain := []entry{}
	seen := map[string]bool{}
	for last != "" && !seen[last] {
		seen[last] = true
		e, ok := entries[last]
		if !ok {
			break
		}
		chain = append(chain, e)
		last = e.Parent
	}
	slices.Reverse(chain)
	for _, e := range chain {
		if e.Cwd != "" {
			h.Cwd = e.Cwd
		}
		if e.Type != "user" && e.Type != "assistant" {
			continue
		}
		blocks := content(e.Message.Content)
		if e.Type == "user" && len(blocks) == 1 && blocks[0].Type == "text" {
			text := strings.TrimSpace(blocks[0].Text)
			// The native CLI records local commands as user messages. They do not
			// start a model turn and their XML wrappers are not conversation text.
			if strings.HasPrefix(text, "<command-name>") || strings.HasPrefix(text, "<local-command-stdout>") || strings.HasPrefix(text, "<local-command-stderr>") {
				continue
			}
			if text == "[Request interrupted by user]" || text == "[Request interrupted by user for tool use]" {
				if len(h.Turns) > 0 {
					h.Turns[len(h.Turns)-1].Status = "interrupted"
				}
				continue
			}
		}
		toolResult := false
		for _, b := range blocks {
			if b.Type == "tool_result" {
				toolResult = true
			}
		}
		if e.Type == "user" && !e.Meta && !toolResult {
			text := ""
			for _, b := range blocks {
				if b.Type == "text" {
					text += b.Text
				} else {
					text += "\n[附件未导出]"
				}
			}
			if strings.TrimSpace(text) == "" {
				continue
			}
			h.Preview = text
			h.Turns = append(h.Turns, Turn{ID: e.UUID, Status: "inProgress", Items: []Item{{ID: e.UUID, Type: "userMessage", Content: []map[string]string{{"type": "text", "text": text}}}}})
			continue
		}
		if len(h.Turns) == 0 {
			continue
		}
		t := &h.Turns[len(h.Turns)-1]
		if toolResult {
			for _, b := range blocks {
				if b.Type == "tool_result" {
					text := ""
					for _, part := range content(b.Content) {
						if part.Type == "text" {
							text += part.Text
						} else {
							text += "\n[工具输出附件未导出]"
						}
					}
					t.Items = append(t.Items, Item{ID: e.UUID + ":" + b.ToolUseID, Type: "toolResult", Text: text})
				}
			}
		}
		if e.Type == "assistant" {
			if e.Message.Model != "" && e.Message.Model != "<synthetic>" {
				h.Model = e.Message.Model
				h.ReasoningEffort = e.Effort
			}
			for _, b := range blocks {
				switch b.Type {
				case "text":
					t.Items = append(t.Items, Item{ID: e.UUID, Type: "agentMessage", Text: b.Text})
				case "tool_use":
					t.Items = append(t.Items, Item{ID: b.ID, Type: "toolCall", Text: b.Name + "\n" + string(b.Input)})
				}
			}
			if e.Message.StopReason == "end_turn" || e.Message.StopReason == "stop_sequence" {
				t.Status = "completed"
			}
		}
	}
	if len(h.Turns) == 0 {
		return h, fmt.Errorf("Claude 会话尚无可共享的对话")
	}
	if malformed {
		h.Turns[len(h.Turns)-1].Status = "inProgress"
	}
	return h, nil
}

func historyPath(home, id string) (string, error) {
	if _, err := uuid.Parse(id); err != nil {
		return "", fmt.Errorf("无效的 Claude 会话 ID")
	}
	paths, err := filepath.Glob(filepath.Join(home, "projects", "*", id+".jsonl"))
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("Claude 会话历史不存在：%w", os.ErrNotExist)
	}
	if len(paths) != 1 {
		return "", fmt.Errorf("无法唯一定位 Claude 会话 %s", id)
	}
	return paths[0], nil
}

func Read(home, id string) (History, error) {
	path, err := historyPath(home, id)
	if err != nil {
		return History{}, err
	}
	return ReadFile(path)
}

func List(home, search, cursor string) ([]History, string, error) {
	paths, err := filepath.Glob(filepath.Join(home, "projects", "*", "*.jsonl"))
	if err != nil {
		return nil, "", err
	}
	rows := []History{}
	for _, path := range paths {
		h, err := ReadFile(path)
		if err != nil {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(h.Name+" "+h.Preview+" "+h.Cwd), strings.ToLower(search)) {
			continue
		}
		h.Turns = nil
		rows = append(rows, h)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].UpdatedAt == rows[j].UpdatedAt {
			return rows[i].ID > rows[j].ID
		}
		return rows[i].UpdatedAt > rows[j].UpdatedAt
	})
	if cursor != "" {
		i := slices.IndexFunc(rows, func(h History) bool { return h.ID == cursor })
		if i < 0 {
			return nil, "", fmt.Errorf("来源列表已变化，请重新加载")
		}
		rows = rows[i+1:]
	}
	next := ""
	if len(rows) > 40 {
		next = rows[39].ID
		rows = rows[:40]
	}
	return rows, next, nil
}

func (h History) Page(cursor string, limit int) ([]Turn, string, error) {
	end := len(h.Turns)
	if cursor != "" {
		end = slices.IndexFunc(h.Turns, func(t Turn) bool { return t.ID == cursor })
		if end < 0 {
			return nil, "", fmt.Errorf("对话分页位置已变化，请重新读取")
		}
	}
	if limit < 1 || limit > 100 {
		limit = 8
	}
	start := max(0, end-limit)
	page := append([]Turn{}, h.Turns[start:end]...)
	slices.Reverse(page)
	next := ""
	if start > 0 {
		next = h.Turns[start].ID
	}
	return page, next, nil
}

func (h History) CompletedTurn() string {
	if len(h.Turns) == 0 || h.Turns[len(h.Turns)-1].Status != "completed" {
		return ""
	}
	return h.Turns[len(h.Turns)-1].ID
}
