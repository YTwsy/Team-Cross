package nativeclaude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/google/uuid"
)

// projectKey matches the pinned native CLI's UTF-16 path encoding, including
// its signed 32-bit hash for paths longer than 200 code units.
func projectKey(path string) string {
	var key strings.Builder
	var hash int32
	for _, c := range utf16.Encode([]rune(path)) {
		hash = hash*31 + int32(c)
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			key.WriteByte(byte(c))
		} else {
			key.WriteByte('-')
		}
	}
	s := key.String()
	if len(s) <= 200 {
		return s
	}
	n := int64(hash)
	if n < 0 {
		n = -n
	}
	return s[:200] + "-" + strconv.FormatInt(n, 36)
}

func recordString(r map[string]any, key string) string {
	s, _ := r[key].(string)
	return s
}

// forkTranscript follows Agent SDK 0.3.268's offline forkSession rules: new
// message IDs, remapped parent links, no sidechains/progress, and fork lineage.
// Materialize before starting the worker: a lazy --fork-session job can lose its
// source reference when stopped/resumed before its first input. No SDK process,
// model request or live transcript writer is involved in this operation.
func forkTranscript(data []byte, sourceID, id, cwd, title string) ([]byte, error) {
	var records []map[string]any
	var replacements []any
	var atis *string
	historySuppressed := false
	scan := bufio.NewScanner(bytes.NewReader(data))
	scan.Buffer(make([]byte, 64<<10), 8<<20)
	for scan.Scan() {
		if len(bytes.TrimSpace(scan.Bytes())) == 0 {
			continue
		}
		var r map[string]any
		d := json.NewDecoder(bytes.NewReader(scan.Bytes()))
		d.UseNumber()
		if err := d.Decode(&r); err != nil || r == nil {
			return nil, fmt.Errorf("Claude 来源包含未完成或损坏的记录")
		}
		switch recordString(r, "type") {
		case "user", "assistant", "attachment", "system", "progress":
			if recordString(r, "uuid") != "" && r["isSidechain"] != true {
				records = append(records, r)
			}
		case "history-suppression":
			historySuppressed = true
		case "content-replacement":
			if r["sessionId"] == sourceID {
				if values, ok := r["replacements"].([]any); ok {
					replacements = append(replacements, values...)
				}
			}
		case "atis-latch":
			if s, ok := r["atis"].(string); ok && r["sessionId"] == sourceID && strings.IndexFunc(s, func(c rune) bool { return c < 0x21 || c > 0x7e }) < 0 {
				atis = &s
			}
		}
	}
	if err := scan.Err(); err != nil {
		return nil, err
	}
	ids := map[string]string{}
	byID := map[string]map[string]any{}
	var kept []map[string]any
	for _, r := range records {
		old := recordString(r, "uuid")
		ids[old], byID[old] = uuid.NewString(), r
		if r["type"] != "progress" {
			kept = append(kept, r)
		}
	}
	if len(kept) == 0 {
		return nil, fmt.Errorf("Claude 来源没有可 fork 的消息")
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	emit := func(v any) error { return enc.Encode(v) }
	if historySuppressed {
		if err := emit(map[string]any{"type": "history-suppression", "sessionId": id, "cause": "fork_inherit", "ts": now}); err != nil {
			return nil, err
		}
	}
	for i, r := range kept {
		old := recordString(r, "uuid")
		var parent any
		seen := map[string]bool{}
		for p := recordString(r, "parentUuid"); p != "" && !seen[p]; {
			seen[p] = true
			ancestor := byID[p]
			if ancestor == nil {
				break
			}
			if ancestor["type"] != "progress" {
				parent = ids[p]
				break
			}
			p = recordString(ancestor, "parentUuid")
		}
		// Keep originals intact until every parent link has been resolved.
		copy := make(map[string]any, len(r)+3)
		for k, v := range r {
			copy[k] = v
		}
		copy["uuid"], copy["parentUuid"], copy["sessionId"] = ids[old], parent, id
		copy["cwd"], copy["isSidechain"] = cwd, false
		copy["forkedFrom"] = map[string]any{"sessionId": sourceID, "messageUuid": old}
		if logical, exists := r["logicalParentUuid"]; exists && logical != nil {
			copy["logicalParentUuid"] = nil
			if mapped := ids[recordString(r, "logicalParentUuid")]; mapped != "" {
				copy["logicalParentUuid"] = mapped
			}
		}
		for _, key := range []string{"teamName", "agentName", "sessionKind", "slug", "sourceToolAssistantUUID"} {
			delete(copy, key)
		}
		if r["type"] == "system" && r["subtype"] == "model_refusal_fallback" {
			copy["neutralizedByFork"] = true
		}
		if r["type"] == "attachment" {
			if a, ok := r["attachment"].(map[string]any); ok && a["type"] == "deferred_tools_record" {
				if names, ok := a["nameOnlyAnnouncements"].([]any); ok {
					mapped := []string{}
					for _, name := range names {
						if name, ok := name.(string); ok && ids[name] != "" {
							mapped = append(mapped, ids[name])
						}
					}
					a["nameOnlyAnnouncements"] = mapped
				}
			}
		}
		if i == len(kept)-1 {
			copy["timestamp"] = now
		}
		if err := emit(copy); err != nil {
			return nil, err
		}
	}
	if len(replacements) > 0 {
		if err := emit(map[string]any{"type": "content-replacement", "sessionId": id, "replacements": replacements, "uuid": uuid.NewString(), "timestamp": now}); err != nil {
			return nil, err
		}
	}
	if atis != nil {
		if err := emit(map[string]any{"type": "atis-latch", "sessionId": id, "atis": *atis}); err != nil {
			return nil, err
		}
	}
	// Execution directory is explicitly chosen by Team Cross's create flow.
	if err := emit(map[string]any{"type": "relocated", "sessionId": id, "relocatedCwd": cwd}); err != nil {
		return nil, err
	}
	if err := emit(map[string]any{"type": "custom-title", "sessionId": id, "customTitle": title, "uuid": uuid.NewString(), "timestamp": now}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func materializeFork(c Config, source History, id, title string) error {
	snapshot := filepath.Join(c.Home, "projects", filepath.Base(filepath.Dir(source.Path)), filepath.Base(source.Path))
	b, err := os.ReadFile(snapshot)
	if err != nil {
		return err
	}
	b, err = forkTranscript(b, source.ID, id, c.Cwd, title)
	if err != nil {
		return err
	}
	path := filepath.Join(c.Home, "projects", projectKey(c.Cwd), id+".jsonl")
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = f.Write(b); err != nil {
		return err
	}
	return f.Sync()
}
