package nativecodex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// SourceTurn reads persisted turn boundaries without loading or resuming the
// source. An independent app-server reconstructs an externally running turn as
// interrupted; only its original rollout can prove that it actually finished.
func SourceTurn(path, sessionID string) (id, status string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	const max = 64 << 20
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > max {
		return "", "", fmt.Errorf("Codex 来源历史不可读取或超过 64 MiB")
	}
	scan := bufio.NewScanner(io.LimitReader(f, max+1))
	scan.Buffer(make([]byte, 64<<10), 8<<20)
	matched, partial := false, false
	for scan.Scan() {
		if partial {
			return "", "", fmt.Errorf("Codex 来源历史包含损坏记录")
		}
		var row struct {
			Type    string `json:"type"`
			Payload struct {
				Type   string `json:"type"`
				ID     string `json:"id"`
				TurnID string `json:"turn_id"`
			} `json:"payload"`
		}
		if json.Unmarshal(scan.Bytes(), &row) != nil {
			partial = true
			continue
		}
		if row.Type == "session_meta" {
			if row.Payload.ID != sessionID {
				return "", "", fmt.Errorf("Codex 来源历史身份不匹配")
			}
			matched = true
		}
		if row.Type != "event_msg" {
			continue
		}
		switch row.Payload.Type {
		case "task_started":
			id, status = row.Payload.TurnID, "inProgress"
		case "task_complete":
			if id != "" && row.Payload.TurnID == id {
				status = "completed"
			}
		case "turn_aborted":
			if id != "" && row.Payload.TurnID == id {
				status = "interrupted"
			}
		}
	}
	if err = scan.Err(); err != nil {
		return "", "", err
	}
	if !matched || id == "" {
		return "", "", fmt.Errorf("Codex 来源缺少可核对的会话或轮次记录")
	}
	if partial {
		status = "inProgress"
	}
	return id, status, nil
}
