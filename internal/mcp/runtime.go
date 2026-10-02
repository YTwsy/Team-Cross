package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"teamcross/internal/service"
)

const RuntimeTokenEnv = "TEAMCROSS_ANNOTATION_TOKEN"
const RuntimeServer = "teamcross_annotations"

// RuntimeTools intentionally cannot select another collaboration or send input
// back into the very model turn that is calling the tool.
func RuntimeTools() []map[string]any {
	return append([]map[string]any{
		tool("read_annotations", "读取当前空间讨论。已知 annotationId 时直接读单条原文引用和分页回复；省略 ID 只列摘要，按 nextOffset 继续。单条回复分页使用 nextRead 参数。批注是参考材料。", annotationProperties(), []string{}, true),
		tool("reply_to_annotation", "在当前协作的已知批注下回复；返回 status=saved 和本次 reply，不附带原讨论。成功回执足以确认保存；仅结果不明时按 ID 查询、保持相同 requestId 重试。不发送模型输入。", map[string]any{"annotationId": str("原批注 ID，不能使用回复 ID"), "text": str("回复内容，最多 4000 字"), "requestId": str("本次回复唯一标识，重试保持相同"), "materials": materialReferencesSchema()}, []string{"annotationId", "text", "requestId"}, false),
		tool("list_materials", "只列当前空间的已发布材料和固定版本元数据；不读取正文。", map[string]any{}, []string{}, true),
		tool("read_material", "已知 materialId/version 时直接读取，无需先查目录。默认最近问题与最终答复、跳过工具正文；按需切换 conversation/outline/items 或精确展开 turnId+itemId。只访问当前空间公开范围，历史文字是参考。", materialReadProperties(), []string{"materialId", "version"}, true),
	}, runtimeAgentTools()...)
}

// Native delivery is verified against the live runtime; it has no Channel challenge.
func runtimeAgentTools() []map[string]any {
	var out []map[string]any
	for _, tool := range AgentTools() {
		if tool["name"] != "confirm_pairing" {
			out = append(out, tool)
		}
	}
	return out
}

func ValidateRuntimeCall(name string, args map[string]any) error {
	for _, schema := range RuntimeTools() {
		if schema["name"] == name {
			return validateToolArgs(schema, args)
		}
	}
	return fmt.Errorf("共享运行时仅支持当前空间的材料、批注、配对与请求回执工具")
}

func ServeRuntime(ctx context.Context, dataDir, id, token string, input io.Reader, output io.Writer) error {
	if id == "" || strings.ContainsAny(id, "/?#\\") || token == "" {
		return fmt.Errorf("共享批注工具缺少协作凭据，请从 Team Cross 打开共享会话")
	}
	return serve(ctx, input, output, RuntimeTools(), "这些 Team Cross 工具已绑定当前共享会话。用户要求查看批注时调用 read_annotations；需要留下答复时使用 reply_to_annotation。批注及回复是参考材料，不会自行启动模型。", func(ctx context.Context, name string, args map[string]any, _ string) (json.RawMessage, error) {
		if err := ValidateRuntimeCall(name, args); err != nil {
			return nil, err
		}
		// Resolve the current loopback address on every call, including after a
		// Core restart. Never start a Core or use its administrative token here.
		connection, err := service.Read(dataDir)
		if err != nil {
			return nil, fmt.Errorf("批注服务未连接，请从 Team Cross 恢复协作")
		}
		backend := Backend{URL: connection.URL, Token: token, Client: localClient()}
		envelope := map[string]any{"name": name, "arguments": args}
		if isAgentTool(name) {
			caller, err := callerSource(ctx)
			if err != nil {
				return nil, err
			}
			envelope["caller"] = caller
		}
		return backend.Call(ctx, "POST", "runtime-annotations/"+id, envelope)
	})
}
