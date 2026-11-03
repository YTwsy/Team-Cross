package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

func memberMentionsSchema() map[string]any {
	return map[string]any{"type": "array", "maxItems": 20, "description": "只使用空间有效成员 ID；offset 为正文 UTF-16 范围，须精确覆盖 @label。普通引用不自动提及。", "items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"memberId", "startOffset", "endOffset", "label"}, "properties": map[string]any{"memberId": str("成员 ID"), "label": str("显示名"), "startOffset": map[string]any{"type": "integer", "minimum": 0}, "endOffset": map[string]any{"type": "integer", "minimum": 1}}}}
}
func replyTargetSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"kind", "id"}, "properties": map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"annotation", "reply"}}, "id": str("同一根讨论中被回复的批注或回复 ID；省略 replyTo 时回复根批注")}}
}

func memberActivityTools() []map[string]any {
	id := str("本机空间 ID；只访问调用成员自己的动态与偏好")
	subject := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"kind", "id"}, "properties": map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"material", "discussion", "execution"}}, "id": str("材料、根讨论或协作会话 ID"), "executionId": str("讨论所属协作会话 ID；材料讨论省略")}}
	return []map[string]any{
		tool("list_member_activity", "读取本成员的空间动态、关注清单或可提及成员。读取不会推进人的已读状态，不启动模型。动态分页使用 nextCursor，关注清单使用返回的 revision。", map[string]any{"id": id, "mode": map[string]any{"type": "string", "enum": []string{"activity", "follows", "members"}}, "category": map[string]any{"type": "string", "enum": []string{"all", "mentions", "replies", "following"}}, "unread": map[string]any{"type": "boolean"}, "cursor": str("上页返回的游标"), "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}, []string{"id"}, true),
		tool("set_member_follow", "按用户要求关注、取消关注或静音材料、讨论或协作会话。只修改本人成员偏好；不启动分身持续关注。先读取当前 revision；重复请求保持相同 requestId。", map[string]any{"id": id, "subject": subject, "mode": map[string]any{"type": "string", "enum": []string{"following", "direct_only", "muted", "unmute"}}, "revision": map[string]any{"type": "integer", "minimum": 0}, "requestId": str("本次操作唯一标识")}, []string{"id", "subject", "mode", "revision", "requestId"}, false),
		tool("mark_member_activity_read", "仅在用户明确要求标为已读或清除提醒时调用。Agent 自己读取内容不代表人已读。传 eventIds 或列表返回的 snapshot；group 可限定快照中的讨论组。", map[string]any{"id": id, "eventIds": map[string]any{"type": "array", "maxItems": 500, "items": str("提醒事件 ID")}, "snapshot": str("列表返回的已读快照"), "group": str("可选的分组 ID")}, []string{"id"}, false),
	}
}

func (b Backend) invokeMemberActivity(ctx context.Context, name string, args map[string]any) (bool, json.RawMessage, error) {
	for _, schema := range memberActivityTools() {
		if schema["name"] != name {
			continue
		}
		if err := validateToolArgs(schema, args); err != nil {
			return true, nil, err
		}
		id, _ := args["id"].(string)
		if id == "" || strings.ContainsAny(id, "/?#\\") {
			return true, nil, fmt.Errorf("空间 ID 无效")
		}
		base := "collaborations/" + id
		if name == "list_member_activity" {
			path := "activity"
			if args["mode"] == "follows" {
				path = "follows"
			}
			if args["mode"] == "members" {
				path = "mention-members"
			}
			q := url.Values{}
			for _, key := range []string{"category", "unread", "cursor", "limit"} {
				if value, ok := args[key]; ok {
					q.Set(key, fmt.Sprint(value))
				}
			}
			out, err := b.Call(ctx, "GET", base+"/"+path+"?"+q.Encode(), nil)
			return true, out, err
		}
		body := map[string]any{}
		for key, value := range args {
			if key != "id" {
				body[key] = value
			}
		}
		path := "follow-preferences"
		if name == "mark_member_activity_read" {
			path = "activity/read"
		}
		out, err := b.Call(ctx, "POST", base+"/"+path, body)
		return true, out, err
	}
	return false, nil, nil
}
