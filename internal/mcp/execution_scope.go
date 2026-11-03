package mcp

import (
	"fmt"
	"net/url"
	"strings"
)

var executionTools = map[string]bool{
	"send_input": true, "interrupt_turn": true, "respond_to_request": true,
	"request_input": true, "cancel_input_request": true, "handoff_input": true,
	"reclaim_input": true, "return_input": true,
	"open_client": true, "resume_collaboration": true, "pause_execution": true,
	"end_execution_sharing": true,
}

func executionToolSchemas(items []map[string]any) []map[string]any {
	for _, item := range items {
		name, _ := item["name"].(string)
		required := executionTools[name]
		if !required && name != "get_collaboration" && name != "read_context" && name != "add_annotation" && name != "reply_to_annotation" {
			continue
		}
		schema := item["inputSchema"].(map[string]any)
		properties := map[string]any{}
		for key, value := range schema["properties"].(map[string]any) {
			properties[key] = value
		}
		properties["id"] = str("本机空间 ID；协作会话必须另传 executionId")
		properties["executionId"] = str("空间 sessions 列表中的协作会话 ID；不得使用原生 sessionId。公共空间讨论省略。")
		schema["properties"] = properties
		if required {
			keys := append([]string(nil), schema["required"].([]string)...)
			found := false
			for _, key := range keys {
				found = found || key == "executionId"
			}
			if !found {
				keys = append(keys, "executionId")
			}
			schema["required"] = keys
		}
	}
	return items
}

// There is no ambient selected session in MCP. The caller names both the
// space and the execution, even when the space currently contains only one.
func scopedToolPath(name string, args map[string]any) (string, error) {
	id, _ := args["id"].(string)
	executionID, _ := args["executionId"].(string)
	if id == "" && name != "list_collaborations" || strings.ContainsAny(id, "/?#\\") {
		return "", fmt.Errorf("请选择有效的空间 ID")
	}
	required := executionTools[name] || name == "read_context" && args["kind"] != "annotations"
	if required && executionID == "" {
		return "", fmt.Errorf("请明确 executionId，不会自动选择空间中的会话")
	}
	base := "collaborations/" + url.PathEscape(id)
	if executionID != "" {
		if strings.ContainsAny(executionID, "/?#\\") || len(executionID) > 200 {
			return "", fmt.Errorf("协作会话标识无效")
		}
		base += "/sessions/" + url.PathEscape(executionID)
	}
	return base, nil
}
