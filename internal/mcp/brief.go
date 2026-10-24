package mcp

func briefSourceSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"spaceId"}, "properties": map[string]any{
		"spaceId": str("本空间 ID"), "materialId": str("已发布材料 ID"), "version": map[string]any{"type": "integer", "minimum": 1},
		"annotationId": str("根讨论 ID"), "replyId": str("准确回复 ID"), "executionId": str("讨论或轮次所属协作会话 ID"), "turnId": str("已完成的原生轮次 ID"),
		"requestId": str("有结果的空间请求 ID"), "briefRevision": map[string]any{"type": "integer", "minimum": 1}, "briefItemId": str("该简报版本内的稳定条目 ID"),
		"shadowId": str("空间分身 ID"), "shadowMessageId": str("准确分身消息 ID，与 shadowResultId 二选一"), "shadowResultId": str("固定任务结果 ID，不能用当前任务状态替代"),
	}}
}

func briefReadTool(space map[string]any) map[string]any {
	return tool("read_space_brief", "读取当前空间简报，或按 revision/itemId 读取准确旧版本及条目。来源与条目保留实际作者、状态和依据；建议不等于团队已确认。更新结果不明时用原 requestId 核对保存版本，不能用当前正文代替旧版本；unavailable 明确表示旧来源不可读。", map[string]any{
		"spaceId": space, "revision": map[string]any{"type": "integer", "minimum": 1}, "itemId": str("可选，稳定简报条目 ID"), "requestId": str("可选，核对一次已有简报更新的 requestId"),
	}, []string{"spaceId"}, true)
}

func briefUpdateTool(space map[string]any) map[string]any {
	item := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"itemId", "text", "status", "sources"}, "properties": map[string]any{
		"itemId": str("已有稳定条目 ID，新增时生成唯一 ID；不按数组位置修改"), "text": str("1–2000 字，保留适用条件和实际确认状态"),
		"status":  choice("proposed", "confirmed", "disputed", "open", "resolved", "in_progress", "completed", "blocked"),
		"sources": map[string]any{"type": "array", "maxItems": 32, "items": briefSourceSchema()},
	}}
	change := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"operation", "section"}, "properties": map[string]any{
		"operation": choice("upsert", "remove"), "section": choice("decisions", "questions", "progress"), "item": item, "itemId": str("remove 使用已有条目 ID；upsert 只传完整 item"),
	}}
	return tool("update_space_brief", "按当前工作需要直接维护空间简报。先读版本，以 requestId/baseRevision 提交 topic 和/或 changes，不需要逐次确认。decisions 使用 proposed/confirmed/disputed，questions 使用 open/resolved，progress 使用 in_progress/completed/blocked；只有已有明确依据才写 confirmed。只修改相关条目，保留他人内容和准确公开来源。state=conflict 表示尚未写入；根据返回的最新 brief/conflicts 合并后使用新的 requestId。保存回执的 requestId 属于简报更新，按 readBack 调用 read_space_brief(requestId) 核对保存版本。结果不明也使用该读取入口，或以完全相同参数重试，不重复创建条目。", map[string]any{
		"spaceId": space, "requestId": str("本次唯一 UUID，同一行动重试保持 ID 与参数"), "baseRevision": map[string]any{"type": "integer", "minimum": 0}, "topic": str("可选，修改当前议题，最多 2000 字"),
		"changes": map[string]any{"type": "array", "maxItems": 32, "items": change},
	}, []string{"spaceId", "requestId", "baseRevision"}, false)
}
