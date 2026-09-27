package mcp

import "encoding/json"

func readProperties() map[string]any {
	return map[string]any{
		"view":        map[string]any{"type": "string", "enum": []string{"answers", "conversation", "outline", "items"}, "description": "默认 answers：用户问题/补充及有 phase=final_answer 的答复；conversation 包含过程发言；outline 只列轮次；items 只列记录索引。未标记最终答复时返回 unidentified，不猜测。"},
		"toolOutputs": map[string]any{"type": "string", "enum": []string{"none", "preview", "full"}, "description": "默认 none 不读取工具正文；preview 展示截取；full 分页全文。指定 itemId 则直接读取该条全文，不受此过滤。"},
		"turnLimit":   map[string]any{"type": "integer", "minimum": 1, "maximum": 50, "description": "默认最近 3 轮，页内按时间正序，nextCursor 继续更早轮次。指定 turnId 只读该轮。"},
		"maxBytes":    map[string]any{"type": "integer", "minimum": 8192, "maximum": 65536, "description": "默认 24576；包括 JSON 转义与 MCP 封装的响应预算。游标继承读取选项。"},
	}
}

func annotationProperties() map[string]any {
	return map[string]any{
		"annotationId": str("已知原批注 ID 时直接读取；省略只列批注摘要，不附带引用全文和全部回复"),
		"includeQuote": map[string]any{"type": "boolean", "description": "按 ID 首次读取默认 true；只核对回复状态或继续回复分页时设 false"},
		"offset":       map[string]any{"type": "integer", "minimum": 0, "description": "无 ID 时是批注目录位置；有 ID 时是回复位置。按返回的 nextRead/nextOffset 继续。"},
		"maxBytes":     readProperties()["maxBytes"],
	}
}

func contextProperties(id map[string]any) map[string]any {
	out := readProperties()
	for k, v := range annotationProperties() {
		out[k] = v
	}
	out["id"], out["kind"] = id, map[string]any{"type": "string", "enum": []string{"history", "changes", "file", "annotations", "events"}}
	out["cursor"] = str("history 的 nextCursor；从该页定位其他记录时使用 pageCursor + turnId + itemId")
	out["turnId"], out["itemId"] = str("只读取指定轮次"), str("与 turnId 一起精确展开一条记录")
	out["startOffset"] = map[string]any{"type": "integer", "minimum": 0, "description": "按条读取的原始 UTF-16 偏移"}
	out["path"] = str("kind=file 时的相对路径")
	out["after"] = map[string]any{"type": "integer", "minimum": 0}
	return out
}

// Status queries do not carry discussion bodies, material versions, invitation
// secrets or UI launch data. These have their own targeted reader tools.
func compactStatus(raw json.RawMessage) json.RawMessage {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return raw
	}
	project := func(in map[string]any) map[string]any {
		out := map[string]any{}
		for _, key := range []string{"id", "title", "state", "error", "host", "role", "selfId", "hasExecution", "executionAvailable", "reachable", "provider", "runtimeMode", "runtimeState", "nativeWaiting", "capabilities", "executionCwd", "sessionId", "writer", "busy", "online", "epoch", "sequence", "approvals", "members", "sharing", "releasePending", "inputRequested", "clientState"} {
			if v, ok := in[key]; ok {
				out[key] = v
			}
		}
		for _, key := range []string{"annotations", "materials"} {
			out[key+"Count"] = 0
			if rows, ok := in[key].([]any); ok {
				out[key+"Count"] = len(rows)
			}
		}
		return out
	}
	switch v := value.(type) {
	case map[string]any:
		value = project(v)
	case []any:
		for i, row := range v {
			if m, ok := row.(map[string]any); ok {
				v[i] = project(m)
			}
		}
	}
	out, _ := json.Marshal(value)
	return out
}
