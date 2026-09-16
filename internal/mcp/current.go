package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/google/uuid"
)

// CallerSource comes from the native client's transport, never tool arguments.
// It identifies a source, not an authorization to share it.
type CallerSource struct {
	Provider  string `json:"provider"`
	SourceID  string `json:"sourceId"`
	TurnID    string `json:"turnId,omitempty"`
	ToolUseID string `json:"toolUseId,omitempty"`
}

type callerSourceKey struct{}

func callContext(ctx context.Context, provider string, meta map[string]json.RawMessage) context.Context {
	c := CallerSource{Provider: provider}
	switch provider {
	case "codex":
		var details struct {
			ThreadID string `json:"thread_id"`
			TurnID   string `json:"turn_id"`
		}
		_ = json.Unmarshal(meta["x-codex-turn-metadata"], &details)
		var thread string
		_ = json.Unmarshal(meta["threadId"], &thread)
		if thread == "" {
			thread = details.ThreadID
		}
		if details.ThreadID != "" && details.ThreadID != thread {
			thread = ""
		}
		c.SourceID, c.TurnID = thread, details.TurnID
	case "claude":
		// Claude sets this for its MCP child. CODEX_THREAD_ID may be inherited
		// from an unrelated launcher and must never be used here.
		c.SourceID = os.Getenv("CLAUDE_CODE_SESSION_ID")
		_ = json.Unmarshal(meta["claudecode/toolUseId"], &c.ToolUseID)
	}
	return context.WithValue(ctx, callerSourceKey{}, c)
}

func callerSource(ctx context.Context) (CallerSource, error) {
	c, _ := ctx.Value(callerSourceKey{}).(CallerSource)
	_, err := uuid.Parse(c.SourceID)
	if err != nil || (c.Provider != "codex" && c.Provider != "claude") || (c.Provider == "codex" && c.TurnID == "") || (c.Provider == "claude" && c.ToolUseID == "") {
		return CallerSource{}, fmt.Errorf("原生客户端未提供可核对的当前 Session 身份；请使用 list_source_sessions 明确选择来源，不能猜测最近会话")
	}
	return c, nil
}

func currentTools() []map[string]any {
	properties := func() map[string]any {
		return map[string]any{
			"workspaceMode": choice("existing", "worktree"), "runtimeMode": choice("restricted", "trusted"), "transport": choice("lan", "tailcat"), "title": str("可选协作名称"), "requestId": str("预览生成的 UUID；提交及重试保持相同"),
		}
	}
	preview, share := properties(), properties()
	share["previewHash"] = str("已确认的 preview_current_share 起点哈希")
	id := map[string]any{"id": str("分享请求 ID，即预览返回的 requestId")}
	return []map[string]any{
		tool("get_current_source", "从原生客户端提供的会话身份核对当前 Session，不猜最近会话。Claude 另核对本次工具调用确实属于该 transcript。仅查看，不创建。", map[string]any{}, []string{}, true),
		tool("preview_current_share", "预览当前 Session 的分享：本轮结束后创建新 fork，包含本轮完整内容。选择目录、权限模式和传输并展示结果；保留 requestId/previewHash。当前轮仍在运行时不创建。", preview, []string{"workspaceMode", "transport"}, true),
		tool("share_current_session", "登记已确认预览的当前 Session 分享请求。立即返回请求状态，不等待自己所在轮次结束；随后结束本轮答复，让 Core 在本轮完成后创建并邀请。不要在本轮循环等待状态，不要把已登记说成已生成邀请。之后用 get_share_request 查询，ready 后 create_invitation 取回邀请。", share, []string{"workspaceMode", "transport", "requestId", "previewHash"}, false),
		tool("get_share_request", "查询等待本轮完成的分享请求；只读，不重试创建，不返回邀请秘密。ready 后使用 collaborationId 调用 create_invitation 取回邀请；失败先看 recovery。", id, []string{"id"}, true),
		tool("cancel_share_request", "取消尚在等待本轮结束的分享请求。已经开始创建时不假装撤销或删除已生成资源，应查询协作后按需结束共享。", id, []string{"id"}, false),
	}
}

func (b Backend) invokeCurrent(ctx context.Context, name string, args map[string]any) (bool, json.RawMessage, error) {
	var schema map[string]any
	for _, t := range currentTools() {
		if t["name"] == name {
			schema = t
			break
		}
	}
	if schema == nil {
		return false, nil, nil
	}
	if err := validateToolArgs(schema, args); err != nil {
		return true, nil, err
	}
	if name == "get_share_request" || name == "cancel_share_request" {
		id, _ := args["id"].(string)
		if _, err := uuid.Parse(id); err != nil {
			return true, nil, fmt.Errorf("无效的分享请求 ID")
		}
		method, path := "GET", "share-requests/"+id
		if name == "cancel_share_request" {
			method, path = "POST", path+"/cancel"
		}
		out, err := b.Call(ctx, method, path, nil)
		return true, out, err
	}
	caller, err := callerSource(ctx)
	if err != nil {
		return true, nil, err
	}
	if name == "get_current_source" {
		out, err := b.Call(ctx, "POST", "sources/current", caller)
		return true, out, err
	}
	input := make(map[string]any, len(args)+3)
	for k, v := range args {
		input[k] = v
	}
	input["provider"], input["sourceId"] = caller.Provider, caller.SourceID
	in, err := createInput(input, name == "share_current_session")
	if err != nil {
		return true, nil, err
	}
	input["runtimeMode"], input["requestId"], input["caller"] = in.RuntimeMode, in.RequestID, caller
	path := "share-requests/preview"
	if name == "share_current_session" {
		path = "share-requests"
	}
	out, err := b.Call(ctx, "POST", path, input)
	return true, out, err
}
