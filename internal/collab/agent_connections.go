package collab

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/mcp"
	"teamcross/internal/nativecodex"
)

// Receiver connections are borrowed from the running native service. They are
// never restored on Core startup; an explicit connect action is required.
func (a *App) connectCodexReceiver(ctx context.Context, sessionID string) (Runtime, error) {
	a.agentMu.Lock()
	old, running := a.agentProxies[sessionID], a.agentDone != nil
	a.agentMu.Unlock()
	if !running {
		return nil, fmt.Errorf("接收服务已停止")
	}
	if old != nil && old.Alive() {
		return old, nil
	}
	if a.Config.StartProcess != nil && a.Config.ConnectProcess == nil {
		return nil, fmt.Errorf("测试运行时未提供原生接收连接")
	}
	binary, err := a.binary()
	if err != nil {
		return nil, err
	}
	home, err := a.providerHome("codex")
	if err != nil {
		return nil, err
	}
	log := filepath.Join(a.Config.DataDir, "receiver-"+sessionID+".log")
	var p Runtime
	if a.Config.ConnectProcess != nil {
		p, err = a.Config.ConnectProcess(ctx, binary, home, log, sessionID)
	} else {
		p, err = nativecodex.ConnectExisting(ctx, binary, home, log, sessionID)
	}
	if err != nil {
		return nil, err
	}
	a.agentMu.Lock()
	if a.agentDone == nil {
		a.agentMu.Unlock()
		p.Close()
		return nil, fmt.Errorf("接收服务已停止")
	}
	if current := a.agentProxies[sessionID]; current != nil && current.Alive() {
		a.agentMu.Unlock()
		p.Close()
		return current, nil
	}
	a.agentProxies[sessionID] = p
	a.agentMu.Unlock()
	if old != nil {
		old.Close()
	}
	return p, nil
}

func (a *App) codexReceiverReason(ctx context.Context, p AgentPairing) string {
	a.agentMu.Lock()
	runtime := a.agentProxies[p.SessionID]
	a.agentMu.Unlock()
	if runtime == nil || !runtime.Alive() {
		return "原生接收连接已断开，请在目标会话重新连接"
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var read struct {
		Thread Source `json:"thread"`
	}
	if err := runtime.Call(ctx, "thread/read", map[string]any{"threadId": p.SessionID, "includeTurns": false}, &read); err != nil || read.Thread.ID != p.SessionID {
		return "无法核对原生接收会话，请重新连接"
	}
	if read.Thread.Status.Type != "idle" {
		return "接收会话正在运行、等待原生交互或已关闭，请在原客户端核对"
	}
	return ""
}

func (a *App) deliverCodexRequest(ctx context.Context, p AgentPairing, r AgentRequest) (AgentRequest, error) {
	// The durable request is already submitting. Never retry an uncertain RPC.
	if reason := a.codexReceiverReason(ctx, p); reason != "" {
		return a.agentDeliveryResult(r.ID, "failed", "", reason)
	}
	a.agentMu.Lock()
	runtime := a.agentProxies[p.SessionID]
	current, exists := a.agents.Pairings[p.ID]
	a.agentMu.Unlock()
	if !exists || current.State != "paired" || current.SessionID != p.SessionID || runtime == nil {
		return a.agentDeliveryResult(r.ID, "failed", "", "接收会话已解除绑定")
	}
	prompt := "请使用 Team Cross 的 read_agent_request 读取请求 " + r.ID + "，按用户处理要求核对原文。结束后用 finish_agent_request 记录结果摘要。"
	var result struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	err := runtime.Call(ctx, "turn/start", map[string]any{"threadId": p.SessionID, "input": []any{map[string]any{"type": "text", "text": prompt}}}, &result)
	if err != nil {
		return a.agentDeliveryResult(r.ID, "unknown", "", err.Error())
	}
	return a.agentDeliveryResult(r.ID, "submitted", result.Turn.ID, "")
}

func (a *App) connectCurrentSession(ctx context.Context, c agentCaller, spaceID, name, runtimeID string) (any, error) {
	if _, err := a.workbenchCall(ctx, spaceID, "receiver-check", workbenchInput{}); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "当前会话"
	}
	if len([]rune(name)) > 80 {
		return nil, fmt.Errorf("接收会话名称最多 80 字")
	}
	// Serialize discovery/creation so repeating the same explicit action cannot
	// create a second pairing or a second public target.
	a.agentConnectMu.Lock()
	defer a.agentConnectMu.Unlock()
	if runtimeID == "" {
		if err := a.verifyConversation(ctx, c.Caller); err != nil {
			return nil, err
		}
	}
	a.agentMu.Lock()
	var p AgentPairing
	for _, existing := range a.agents.Pairings {
		if existing.Provider == c.Caller.Provider && existing.SessionID == c.Caller.SourceID && (existing.SpaceID == "" || existing.SpaceID == spaceID) && (existing.State == "paired" || existing.State == "linked" || existing.State == "verifying") && (existing.Transport != "claude_channel" || existing.ReceiverID == c.ReceiverID) {
			p = existing
			break
		}
	}
	a.agentMu.Unlock()
	if p.ID == "" && mcp.ChatGPTConversation(c.Caller) {
		p = AgentPairing{ID: uuid.NewString(), Name: name, Provider: "chatgpt", SessionID: c.Caller.SourceID, State: "linked", Transport: "chatgpt", CreatedAt: time.Now(), Reason: "已关联当前 ChatGPT 对话；后续接收需要单独启用订阅"}
		a.agentMu.Lock()
		next := libraryClone(a.agents)
		if len(next.Pairings) >= 64 {
			a.agentMu.Unlock()
			return nil, fmt.Errorf("配对记录已达上限，请先移除不再使用的会话")
		}
		next.Pairings[p.ID] = p
		err := a.saveAgentsLocked(next)
		a.agentMu.Unlock()
		if err != nil {
			return nil, err
		}
	} else if p.ID == "" {
		created, err := a.createAgentPairing(name)
		if err != nil {
			return nil, err
		}
		p, err = a.pairAgent(ctx, agentPairInput{agentCaller: c, Code: created["code"].(string)}, runtimeID)
		if err != nil {
			return nil, err
		}
	} else if p.Transport == "codex_proxy" || (p.State == "linked" && p.Provider == "codex") {
		if _, err := a.connectCodexReceiver(ctx, p.SessionID); err == nil {
			p.State, p.Transport, p.Reason = "paired", "codex_proxy", ""
		} else {
			p.State, p.Reason = "linked", "原生接收连接未接通，请在原客户端打开此会话后重新连接"
		}
	}
	if p.State == "unsupported" {
		p.State = "linked"
	}
	a.agentMu.Lock()
	stored, exists := a.agents.Pairings[p.ID]
	if !exists {
		a.agentMu.Unlock()
		return nil, fmt.Errorf("配对已移除，请重新连接")
	}
	stored.State, stored.Transport, stored.Reason = p.State, p.Transport, p.Reason
	next := libraryClone(a.agents)
	next.Pairings[p.ID] = stored
	err := a.saveAgentsLocked(next)
	a.agentMu.Unlock()
	if err != nil {
		return nil, err
	}
	target, err := a.registerSpacePairing(ctx, spaceID, p.ID)
	if err != nil {
		return nil, err
	}
	// Persisted association and verified delivery are deliberately separate.
	return map[string]any{"spaceId": spaceID, "pairing": pairingPublic(stored), "target": target, "linked": true, "receiving": p.State == "paired", "guidance": "关联只公开会话名称与接收状态，不公开私人历史；后续输入仍需用户明确授权。"}, nil
}

func (a *App) verifyConversation(ctx context.Context, caller mcp.CallerSource) error {
	if mcp.ChatGPTConversation(caller) {
		return nil
	}
	_, err := a.currentSource(ctx, caller)
	return err
}
