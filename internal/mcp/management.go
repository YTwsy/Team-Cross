package mcp

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"teamcross/internal/runtimeconfig"
)

type creationInput struct {
	SpaceID       string             `json:"spaceId,omitempty"`
	Provider      string             `json:"provider"`
	SourceID      string             `json:"sourceId"`
	WorkspaceMode string             `json:"workspaceMode"`
	RuntimeMode   runtimeconfig.Mode `json:"runtimeMode"`
	Title         string             `json:"title"`
	RequestID     string             `json:"requestId"`
	PreviewHash   string             `json:"previewHash"`
}

func choice(values ...string) map[string]any { return map[string]any{"type": "string", "enum": values} }

func creationProperties() map[string]any {
	return map[string]any{
		"spaceId": str("可选：本机已有只读空间 ID；启用执行保留原链接和成员；随后用 set_execution_access 明确开放完整历史与目录"), "provider": choice("codex", "claude"), "sourceId": str("明确选择的本机来源会话 UUID；不能用协作 ID 代替"),
		"workspaceMode": choice("existing", "worktree"), "runtimeMode": choice("restricted", "trusted"),
		"title": str("可选的协作名称"), "requestId": str("创建 UUID；预览可省略，由工具生成；创建和重试保持相同"),
	}
}

func managementTools() []map[string]any {
	preview, create := creationProperties(), creationProperties()
	create["previewHash"] = str("preview_collaboration 返回且用户已确认的起点哈希")
	id := map[string]any{"id": str("本机协作 ID")}
	return []map[string]any{
		tool("list_source_sessions", "分页搜索本机个人来源会话。provider 指来源，而非调用工具的个人客户端；不查询远端私人历史，不把最新会话当成当前会话。", map[string]any{"provider": choice("codex", "claude"), "search": str("名称或内容搜索"), "cursor": str("上一页的 nextCursor")}, []string{"provider"}, true),
		tool("preview_collaboration", "预览明确选定来源、Git 起点、执行目录和权限模式；不创建 fork。existing 保留原现场，worktree 从 HEAD 检出且不复制未提交内容。默认 restricted，trusted 沿用发起者原生配置与权限。展示结果；保留返回的 requestId 与 previewHash。", preview, []string{"provider", "sourceId", "workspaceMode"}, true),
		tool("create_collaboration", "根据已确认的预览创建新的原生 fork，不发送业务输入。必须保留预览的 requestId、previewHash 和模式；超时先以 requestId 查询协作，不能改 ID 重建。成功后单独 create_invitation。", create, []string{"provider", "sourceId", "workspaceMode", "requestId", "previewHash"}, false),
		tool("create_invitation", "生成或取回当前空间的可复用邀请链接，多位同事使用同一链接分别加入。明确选择 LAN 或实验性 Tailcat；不自动降级。reset=true 与 requestId 明确重置链接，旧链接失效，现有成员保留。链接有效至关闭、重置或本次共享结束；不会自动发给同事。", map[string]any{"id": id["id"], "transport": choice("lan", "tailcat"), "requestId": str("操作的唯一请求标识；重置时必填，重试保持相同"), "reset": map[string]any{"type": "boolean"}, "invitationId": str("可选：只取回这份邀请；不能与 requestId 或 reset 同时使用")}, []string{"id", "transport"}, false),
		tool("remove_member", "发起者撤销指定成员的访问；其他成员继续参与，当前输入者被移除时控制归还发起者。", map[string]any{"id": id["id"], "memberId": str("members 中的具体成员 ID")}, []string{"id", "memberId"}, false),
		tool("revoke_invitation", "关闭链接加入；已加入成员继续参与。", map[string]any{"id": id["id"], "invitationId": str("invitations 中的邀请 ID")}, []string{"id", "invitationId"}, false),
		tool("set_execution_access", "发起者为指定成员开放或收回原生历史、目录和执行访问。成员仍留在空间；收回时接回该成员的输入，但不自动中断模型。", map[string]any{"id": id["id"], "memberId": str("具体成员 ID"), "allowed": map[string]any{"type": "boolean"}}, []string{"id", "memberId", "allowed"}, false),
		tool("preview_invitation", "只解析邀请中的名称、主机、模式与到期时间；不联系远端、不加入。显示信息尚未通过远端验证。", map[string]any{"invitation": str("用户提供的原始邀请码或 App 链接")}, []string{"invitation"}, true),
		tool("join_collaboration", "明确加入用户选定的邀请，复用本机加入记录；不打开浏览器或原生客户端，不自动取得输入权。首次加入前展示 preview_invitation 的信息。", map[string]any{"invitation": str("同一份已预览邀请")}, []string{"invitation"}, false),
		tool("open_client", "打开当前协作的直接原生客户端；必须已获得输入权。TUI 打开新终端窗口，Desktop 是独立专用窗口，Claude 仅 TUI。launch=false 只生成启动计划；launched 只表示启动请求成功，之后查询 clientState。", map[string]any{"id": id["id"], "client": choice("tui", "desktop"), "launch": map[string]any{"type": "boolean", "default": true}}, []string{"id", "client"}, false),
		tool("end_sharing", "发起者结束共享、撤销同事访问并接回输入；保留原生会话和工作目录，等待当前工作与直接客户端结束后释放运行时。", id, []string{"id"}, false),
		tool("leave_collaboration", "接收者主动离开并撤销自己的加入资格；后续需要新邀请。不会结束发起者的执行。", id, []string{"id"}, false),
		tool("resume_collaboration", "发起者恢复已有协作的同一原生 fork、执行目录和固定模式，不再创建 fork 或 worktree，不自动生成新邀请。", id, []string{"id"}, false),
	}
}

func createInput(args map[string]any, creating bool) (creationInput, error) {
	var in creationInput
	b, err := json.Marshal(args)
	if err != nil {
		return in, err
	}
	if err = json.Unmarshal(b, &in); err != nil {
		return in, fmt.Errorf("创建参数类型无效")
	}
	if in.Provider != "codex" && in.Provider != "claude" {
		return in, fmt.Errorf("请明确选择来源 provider: codex 或 claude")
	}
	if _, err = uuid.Parse(in.SourceID); err != nil {
		return in, fmt.Errorf("请选择来源会话 UUID")
	}
	if in.WorkspaceMode != "existing" && in.WorkspaceMode != "worktree" {
		return in, fmt.Errorf("请选择 existing 或 worktree 目录模式")
	}
	if in.RuntimeMode, err = runtimeconfig.Parse(in.RuntimeMode); err != nil {
		return in, err
	}
	if !creating && in.RequestID == "" {
		in.RequestID = uuid.NewString()
	}
	if _, err = uuid.Parse(in.RequestID); err != nil {
		return in, fmt.Errorf("需要预览时的 requestId UUID")
	}
	if creating {
		hash, e := hex.DecodeString(in.PreviewHash)
		if e != nil || len(hash) != 32 {
			return in, fmt.Errorf("需要预览时的 previewHash")
		}
	}
	return in, nil
}

func (b Backend) invokeManagement(ctx context.Context, name string, args map[string]any) (bool, json.RawMessage, error) {
	var schema map[string]any
	for _, item := range managementTools() {
		if item["name"] == name {
			schema = item
			break
		}
	}
	if schema == nil {
		return false, nil, nil
	}
	if err := validateToolArgs(schema, args); err != nil {
		return true, nil, err
	}
	call := func(method, path string, body any, invitations bool) (bool, json.RawMessage, error) {
		out, err := b.Call(ctx, method, path, body)
		if !invitations {
			out = withoutInvitations(out)
		}
		return true, out, err
	}
	switch name {
	case "list_source_sessions":
		q := url.Values{}
		for _, k := range []string{"provider", "search", "cursor"} {
			if v, ok := args[k].(string); ok {
				q.Set(k, v)
			}
		}
		return call("GET", "sources?"+q.Encode(), nil, false)
	case "preview_collaboration", "create_collaboration":
		in, err := createInput(args, name == "create_collaboration")
		if err != nil {
			return true, nil, err
		}
		if name == "create_collaboration" {
			return call("POST", "collaborations", in, false)
		}
		out, err := b.Call(ctx, "POST", "preview", in)
		if err != nil {
			return true, nil, err
		}
		var result map[string]any
		if err = json.Unmarshal(out, &result); err != nil {
			return true, nil, err
		}
		result["requestId"] = in.RequestID
		result["workspaceMode"] = in.WorkspaceMode
		out, err = json.Marshal(result)
		return true, out, err
	case "preview_invitation":
		return call("POST", "invitations/preview", args, false)
	case "join_collaboration":
		return call("POST", "join", args, false)
	}
	id, _ := args["id"].(string)
	if id == "" || strings.ContainsAny(id, "/?#\\") {
		return true, nil, fmt.Errorf("请选择有效的协作 ID")
	}
	base := "collaborations/" + url.PathEscape(id)
	switch name {
	case "create_invitation":
		if args["invitationId"] != nil && (args["requestId"] != nil || args["reset"] == true) {
			return true, nil, fmt.Errorf("新建邀请与取回指定邀请不能同时选择")
		}
		out, err := b.Call(ctx, "POST", base+"/invitations", map[string]any{"transport": args["transport"], "requestId": args["requestId"], "invitationId": args["invitationId"], "reset": args["reset"]})
		if err != nil {
			return true, nil, err
		}
		var result map[string]any
		if err = json.Unmarshal(out, &result); err != nil {
			return true, nil, err
		}
		if invitation, ok := result["invitation"].(string); ok && invitation != "" {
			result["invitationUrl"] = "teamcross://join?invite=" + url.QueryEscape(invitation)
		}
		out, err = json.Marshal(result)
		return true, out, err
	case "set_execution_access":
		return call("POST", base+"/execution-access", map[string]any{"memberId": args["memberId"], "allowed": args["allowed"]}, false)
	case "remove_member":
		return call("POST", base+"/remove-member", map[string]any{"memberId": args["memberId"]}, false)
	case "revoke_invitation":
		return call("POST", base+"/revoke-invitation", map[string]any{"invitationId": args["invitationId"]}, false)
	case "open_client":
		launch := true
		if v, ok := args["launch"].(bool); ok {
			launch = v
		}
		return call("POST", base+"/open", map[string]any{"client": args["client"], "launch": launch}, false)
	default:
		action := map[string]string{"end_sharing": "end", "leave_collaboration": "leave", "resume_collaboration": "start"}[name]
		return call("POST", base+"/action", map[string]any{"action": action}, false)
	}
}

// MCP clients advertise schemas, but Core adapters still validate at the boundary.
func validateToolArgs(item map[string]any, args map[string]any) error {
	schema := item["inputSchema"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	for _, k := range schema["required"].([]string) {
		if v, ok := args[k]; !ok || v == nil || v == "" {
			return fmt.Errorf("缺少参数 %s", k)
		}
	}
	for k, v := range args {
		p, ok := properties[k].(map[string]any)
		if !ok {
			return fmt.Errorf("不支持的参数 %s", k)
		}
		switch p["type"] {
		case "string":
			s, ok := v.(string)
			if !ok {
				return fmt.Errorf("参数 %s 必须为字符串", k)
			}
			if choices, ok := p["enum"].([]string); ok {
				valid := false
				for _, c := range choices {
					valid = valid || s == c
				}
				if !valid {
					return fmt.Errorf("参数 %s 必须为 %s", k, strings.Join(choices, " 或 "))
				}
			}
		case "boolean":
			if _, ok := v.(bool); !ok {
				return fmt.Errorf("参数 %s 必须为布尔值", k)
			}
		}
	}
	return nil
}
