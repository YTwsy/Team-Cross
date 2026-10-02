package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

const agentInstructions = " 用户要求配对时调用 pair_current_session，配对只绑定当前会话，不授予额外读取或输入权限。收到 Team Cross Channel 的 pairing_verification 时调用 confirm_pairing，挑战值只来自该通知；不要循环等待。收到 agent_request 时调用 read_agent_request 获取用户明确选择的引用与处理要求，核对原文后执行；结束时用 finish_agent_request 记录摘要。通知的传输送达不代表分析完成。"

func AgentTools() []map[string]any {
	return []map[string]any{
		tool("pair_current_session", "使用用户从 Team Cross 复制的一次性配对码绑定当前会话；会核对传输提供的身份和接收能力。verifying 表示还在核验，unsupported 表示此客户端不能主动接收，均不可称为配对完成。不创建或恢复会话，不自动发送任务。", map[string]any{"code": str("Team Cross 生成的 TCP- 配对码，有效期十分钟")}, []string{"code"}, false),
		tool("confirm_pairing", "只用于确认刚收到的 Team Cross Channel 核验通知。必须使用通知中的 pairingId 和 challenge；不猜测、不轮询。", map[string]any{"pairingId": str("核验通知的配对 ID"), "challenge": str("仅在核验通知中收到的挑战值")}, []string{"pairingId", "challenge"}, false),
		tool("read_agent_request", "读取投递到当前已配对会话的请求；返回用户明确的处理要求和固定引用，记录已读取回执。每页一项引用，按 nextOffset 继续；按 readingGuidance 使用现有工具读取原文，不能仅凭通知分析。", map[string]any{"requestId": str("Team Cross 投递通知中的请求 ID"), "offset": map[string]any{"type": "integer", "minimum": 0, "maximum": 31, "description": "引用分页位置，默认 0；按 nextOffset 继续，直到全部引用已核对"}}, []string{"requestId"}, false),
		tool("finish_agent_request", "在本次请求已读取且处理结束后记录结果与摘要；这是 Agent 的处理回执，不会自动保存批注回复或启动其他输入。成功后不必重复查询。", map[string]any{"requestId": str("本次请求 ID"), "status": choice("completed", "failed"), "summary": str("给用户的结果摘要，1–2000 字")}, []string{"requestId", "status", "summary"}, false),
	}
}
func ValidateAgentCall(name string, args map[string]any) error {
	for _, t := range AgentTools() {
		if t["name"] == name {
			return validateToolArgs(t, args)
		}
	}
	return fmt.Errorf("未知的会话工具")
}
func isAgentTool(name string) bool {
	for _, t := range AgentTools() {
		if t["name"] == name {
			return true
		}
	}
	return false
}

type agentConnectionKey struct{}
type agentConnection struct {
	id     string
	once   sync.Once
	notify func(any) error
	wg     sync.WaitGroup
}

func (b Backend) invokeAgent(ctx context.Context, name string, args map[string]any) (bool, json.RawMessage, error) {
	if !isAgentTool(name) {
		return false, nil, nil
	}
	if err := ValidateAgentCall(name, args); err != nil {
		return true, nil, err
	}
	caller, err := callerSource(ctx)
	if err != nil {
		if name != "pair_current_session" {
			return true, nil, err
		}
		// Let the code's owner see an explicit unsupported status in the UI.
		// Never substitute an ID supplied by the model or a recent session.
		caller = CallerSource{}
	}
	input := map[string]any{"name": name, "arguments": args, "caller": caller}
	if connection, _ := ctx.Value(agentConnectionKey{}).(*agentConnection); connection != nil && caller.Provider == "claude" {
		input["receiverId"] = connection.id
		connection.once.Do(func() {
			connection.wg.Add(1)
			go func() { defer connection.wg.Done(); b.receiveAgentEvents(ctx, connection) }()
		})
	}
	out, err := b.Call(ctx, "POST", "agent-tools/call", input)
	return true, out, err
}

func newAgentConnection(notify func(any) error) *agentConnection {
	return &agentConnection{id: uuid.NewString(), notify: notify}
}
func (b Backend) receiveAgentEvents(ctx context.Context, c *agentConnection) {
	for {
		raw, err := b.Call(ctx, "POST", "agent-receivers/poll", map[string]any{"receiverId": c.id})
		if err != nil {
			return
		}
		var result struct {
			Event json.RawMessage `json:"event"`
		}
		if json.Unmarshal(raw, &result) != nil {
			return
		}
		if len(result.Event) == 0 || string(result.Event) == "null" {
			continue
		}
		notification := map[string]any{"jsonrpc": "2.0", "method": "notifications/claude/channel", "params": map[string]any{"content": string(result.Event), "meta": map[string]string{"event_source": "teamcross"}}}
		if err = c.notify(notification); err != nil {
			return
		}
	}
}
