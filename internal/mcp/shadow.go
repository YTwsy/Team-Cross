package mcp

import "fmt"

// ShadowTools reuses the public business tool schemas, bound to the creating
// member's space. Delivery and action bookkeeping is supplied by Core.
func ShadowTools() []map[string]any {
	selected := map[string]bool{
		"read_annotations": true, "reply_to_annotation": true, "list_materials": true, "read_material": true,
		"list_space_targets": true, "read_space_brief": true, "list_space_requests": true, "get_space_request": true,
		"list_space_shadows": true, "read_space_shadow": true, "send_space_request": true, "send_space_shadow": true,
		"send_space_note": true, "get_space_note": true, "update_space_brief": true,
	}
	out := []map[string]any{}
	for _, t := range RuntimeTools() {
		name := t["name"].(string)
		if !selected[name] {
			continue
		}
		schema := t["inputSchema"].(map[string]any)
		props := schema["properties"].(map[string]any)
		if name == "read_annotations" || name == "reply_to_annotation" {
			props["executionId"] = str("讨论所属协作会话 ID；空间或材料讨论省略")
		}
		switch name {
		case "send_space_request":
			t["description"] = "向本空间相关参与会话咨询或安排当前任务需要的工作。自行组织具体要求，保留准确材料引用、父请求和唯一 requestId。先查已有结果；同一行动重试沿用原 ID，不重复创建任务。"
			props["instruction"] = str("本次具体处理要求，1–4000 字")
		case "send_space_shadow":
			t["description"] = "向本空间相关成员的分身咨询或协作；消息与答复对空间可见。发送需要当前工作的具体问题和唯一 messageId，同一行动重试保持 ID 与正文。送达与完成分别读取。"
			props["shadowId"], props["text"] = str("本空间目标分身 ID"), str("本次协作的具体问题与必要上下文，最多 64 KiB")
		case "send_space_note":
			t["description"] = "给本空间已开启顺流感知的相关会话留下简短工作补充，下次正常交互附带，不启动模型。保留唯一 requestId，同一行动重试保持原文。"
		}
		out = append(out, t)
	}
	for _, t := range Tools() {
		name := t["name"].(string)
		if name != "add_annotation" && name != "read_context" {
			continue
		}
		schema := t["inputSchema"].(map[string]any)
		props := schema["properties"].(map[string]any)
		delete(props, "id")
		required := []string{}
		for _, key := range schema["required"].([]string) {
			if key != "id" {
				required = append(required, key)
			}
		}
		if name == "add_annotation" {
			t["description"] = "以真实分身身份在本空间发起讨论或提出问题；可 @ 有效成员并引用已发布材料。有原讨论时优先 reply_to_annotation。同一行动保持 requestId。"
			props["executionId"] = str("协作会话讨论所属 ID；空间或材料讨论省略")
			required = append(required, "requestId")
		}
		schema["required"] = required
		out = append(out, t)
	}
	out = append(out, tool("list_space_members", "列出本空间有效成员及可用于 @ 的成员 ID 和显示名，不读取任何成员的私人动态或关注清单。", map[string]any{}, []string{}, true))
	return out
}

func ValidateShadowCall(name string, args map[string]any) error {
	for _, schema := range ShadowTools() {
		if schema["name"] == name {
			return validateToolArgs(schema, args)
		}
	}
	return fmt.Errorf("未知分身协作工具")
}
