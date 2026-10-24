package mcp

func WorkbenchReadTool(name string) bool {
	return name == "list_space_targets" || name == "read_space_brief" || name == "list_space_requests" || name == "get_space_request" || name == "get_space_note" || name == "list_space_shadows" || name == "read_space_shadow"
}

func workbenchTools() []map[string]any {
	space := str("当前有权参与的空间 ID，来自 list_collaborations；不是原生会话 ID")
	reference := map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"spaceId": space, "kind": choice("material", "annotation"), "materialId": str("材料 ID"), "version": map[string]any{"type": "integer", "minimum": 1}, "annotationId": str("原批注 ID")}, "required": []string{"spaceId", "kind"}}
	return []map[string]any{
		tool("configure_space_inflow", "按用户的接收偏好设置当前会话的顺流感知。普通空间动态需要已有会话关联；分身委托的任务返回默认生效，没有关联时也可静音或恢复这些返回。只在后续正常交互附带信息，不唤醒模型；不建立材料或讨论订阅。", map[string]any{"spaceId": space, "enabled": map[string]any{"type": "boolean"}}, []string{"spaceId", "enabled"}, false),
		tool("send_space_note", "按当前工作的需要，向已开启顺流感知的相关目标保存一条空间可见留言，下次正常交互顺带投递；不启动模型、不要求回复或完成。保留 requestId，结果不明时按 ID 核对。", map[string]any{"spaceId": space, "requestId": str("唯一 UUID"), "targetId": str("list_space_targets 返回的目标 ID"), "instruction": str("留言正文，1–300 字")}, []string{"spaceId", "requestId", "targetId", "instruction"}, false),
		tool("get_space_note", "读取空间留言及其已投递状态。已投递只表示提醒已经写出，不代表已读、理解或执行。", map[string]any{"spaceId": space, "requestId": str("留言 ID")}, []string{"spaceId", "requestId"}, true),
		tool("list_space_shadows", "列出当前空间的本机数字分身及状态。每个分身是一段全空间共享对话；离线时不能投递。", map[string]any{"spaceId": space}, []string{"spaceId"}, true),
		tool("read_space_shadow", "读取指定分身的共享聊天、记忆与后台任务。offset 从最新一页向前翻；准确来源使用 messageId 或 resultId 二选一，不与分页混用。resultId 是不可变任务结果，保留当时目标版本、完成条件及证据；unavailable 表示来源已不可读，不能替换成当前结果。送达不等于任务完成。", map[string]any{"spaceId": space, "shadowId": str("list_space_shadows 返回的分身 ID"), "messageId": str("可选，准确消息 ID"), "resultId": str("可选，taskResults 中的准确结果 ID"), "offset": map[string]any{"type": "integer", "minimum": 0}}, []string{"spaceId", "shadowId"}, true),
		tool("send_space_shadow", "向当前工作相关的空间分身提问或协作。消息和答复对整个空间可见。自行组织具体问题和必要上下文；同一行动保持 messageId 与正文，结果不明先查询。sending 尚未送达，delivered 表示分身 Core 已保存，工作完成另看结果。普通答复和准确任务结果默认返回本次发起会话，在下一次正常交互附带，无需另开订阅；明确静音仍生效。", map[string]any{"spaceId": space, "shadowId": str("list_space_shadows 返回的相关分身 ID"), "messageId": str("本条消息唯一 UUID，重发保持不变"), "text": str("本次协作问题与必要上下文，最多 64 KiB")}, []string{"spaceId", "shadowId", "messageId", "text"}, false),
		tool("list_space_targets", "列出成员明确关联到空间的接收会话和当前接收状态。只报告可观察状态，不列私人配对列表。", map[string]any{"spaceId": space}, []string{"spaceId"}, true),
		briefReadTool(space),
		briefUpdateTool(space),
		tool("list_space_requests", "分页读取空间协作进展、发起者、关联关系及结果。queued/submitted/received/completed 是不同状态；unknown 需核对，不能自动重发。", map[string]any{"spaceId": space, "offset": map[string]any{"type": "integer", "minimum": 0}}, []string{"spaceId"}, true),
		tool("get_space_request", "按 ID 查询已有空间请求和处理结果；请求标识来自发送回执或空间请求列表。结果不明时查询同一 ID，不自动创建第二个请求。", map[string]any{"spaceId": space, "requestId": str("空间请求 ID")}, []string{"spaceId", "requestId"}, true),
		tool("send_space_request", "按当前任务需要向空间内相关的已关联会话咨询或安排工作，自行组织具体要求与引用。当前调用身份由传输核对，请求和结果摘要对空间成员可见。保留父请求及 requestId，断线后先核对原请求，不重复创建相同工作。", map[string]any{"spaceId": space, "requestId": str("本次唯一 UUID，结果不明时保持同一标识查询"), "targetId": str("list_space_targets 返回的目标 ID"), "parentRequestId": str("可选，同空间关联请求 ID"), "instruction": str("本次具体处理要求，1–4000 字"), "intent": choice("analyze", "analyze_reply"), "references": map[string]any{"type": "array", "maxItems": 32, "items": reference}}, []string{"spaceId", "requestId", "targetId", "instruction", "intent", "references"}, false),
	}
}

func isWorkbenchTool(name string) bool {
	for _, t := range workbenchTools() {
		if t["name"] == name {
			return true
		}
	}
	return false
}
