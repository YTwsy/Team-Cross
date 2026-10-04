package collab

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/mcp"
)

// Pairings name an exact receiving conversation. They confer no additional
// resource access or execution ownership. This state is local to one Core.
type AgentPairing struct {
	Links      map[string]string `json:"links,omitempty"`
	RuntimeID  string            `json:"runtimeId,omitempty"`
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Provider   string            `json:"provider,omitempty"`
	SessionID  string            `json:"sessionId,omitempty"`
	SpaceID    string            `json:"spaceId,omitempty"`
	Transport  string            `json:"transport,omitempty"`
	State      string            `json:"state"`
	Reason     string            `json:"reason,omitempty"`
	CreatedAt  time.Time         `json:"createdAt"`
	ExpiresAt  time.Time         `json:"expiresAt"`
	CodeHash   string            `json:"codeHash,omitempty"`
	ReceiverID string            `json:"receiverId,omitempty"`
	Challenge  string            `json:"challenge,omitempty"`
}

type AgentRequest struct {
	WorkbenchSpaceID string             `json:"workbenchSpaceId,omitempty"`
	TargetID         string             `json:"targetId,omitempty"`
	ID               string             `json:"id"`
	PairingID        string             `json:"pairingId"`
	References       []LibraryReference `json:"references"`
	Instruction      string             `json:"instruction"`
	Intent           string             `json:"intent"`
	State            string             `json:"state"`
	Summary          string             `json:"summary,omitempty"`
	Error            string             `json:"error,omitempty"`
	TurnID           string             `json:"turnId,omitempty"`
	CreatedAt        time.Time          `json:"createdAt"`
	UpdatedAt        time.Time          `json:"updatedAt"`
}

type agentState struct {
	Pairings map[string]AgentPairing `json:"pairings"`
	Requests map[string]AgentRequest `json:"requests"`
}

type agentEvent struct {
	Kind      string `json:"kind"`
	PairingID string `json:"pairingId,omitempty"`
	Challenge string `json:"challenge,omitempty"`
	RequestID string `json:"requestId,omitempty"`
}

type agentReceiver struct {
	events   chan agentEvent
	lastSeen time.Time
}

type agentCaller struct {
	Caller     mcp.CallerSource `json:"caller"`
	ReceiverID string           `json:"receiverId,omitempty"`
}

type agentPairInput struct {
	agentCaller
	Code string `json:"code"`
}

type agentRequestInput struct {
	WorkbenchSpaceID string             `json:"-"`
	TargetID         string             `json:"-"`
	Role             string             `json:"-"`
	RequestID        string             `json:"requestId"`
	PairingID        string             `json:"pairingId"`
	References       []LibraryReference `json:"references"`
	Instruction      string             `json:"instruction"`
	Intent           string             `json:"intent"`
}

func (a *App) loadAgents() error {
	a.agents = agentState{Pairings: map[string]AgentPairing{}, Requests: map[string]AgentRequest{}}
	a.agentReceivers = map[string]*agentReceiver{}
	a.agentProxies = map[string]Runtime{}
	a.agentDone = make(chan struct{})
	err := readJSON(filepath.Join(a.Config.DataDir, "agent-pairings.json"), &a.agents)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("读取会话配对失败: %w", err)
	}
	if a.agents.Pairings == nil {
		a.agents.Pairings = map[string]AgentPairing{}
	}
	if a.agents.Requests == nil {
		a.agents.Requests = map[string]AgentRequest{}
	}
	for id, r := range a.agents.Requests {
		if r.State == "submitting" {
			r.State, r.Error = "unknown", "Core 已重启，请核对目标会话；不会自动重发"
			a.agents.Requests[id] = r
		}
	}
	return nil
}

func (a *App) stopAgentReceivers() {
	a.agentMu.Lock()
	if a.agentDone != nil {
		close(a.agentDone)
		a.agentDone = nil
	}
	a.agentReceivers = map[string]*agentReceiver{}
	proxies := a.agentProxies
	a.agentProxies = map[string]Runtime{}
	a.agentMu.Unlock()
	for _, p := range proxies {
		p.Close()
	}
}

func (a *App) saveAgentsLocked(next agentState) error {
	if err := writeJSONFile(filepath.Join(a.Config.DataDir, "agent-pairings.json"), next); err != nil {
		return err
	}
	a.agents = next
	return nil
}
func agentCodeHash(code string) string {
	hash := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(code))))
	return hex.EncodeToString(hash[:])
}
func agentSecret() (string, error) {
	var p [16]byte
	if _, err := rand.Read(p[:]); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(p[:]), nil
}
func pairingPublic(p AgentPairing) AgentPairing {
	p.Links = nil
	p.CodeHash, p.ReceiverID, p.Challenge = "", "", ""
	return p
}

func (a *App) createAgentPairing(name string) (map[string]any, error) {
	name = strings.TrimSpace(name)
	if len([]rune(name)) == 0 || len([]rune(name)) > 80 {
		return nil, fmt.Errorf("请为接收会话填写 1–80 字的名称")
	}
	secret, err := agentSecret()
	if err != nil {
		return nil, err
	}
	code := "TCP-" + secret
	now := time.Now()
	p := AgentPairing{ID: uuid.NewString(), Name: name, State: "waiting", CodeHash: agentCodeHash(code), CreatedAt: now, ExpiresAt: now.Add(10 * time.Minute)}
	a.agentMu.Lock()
	defer a.agentMu.Unlock()
	next := libraryClone(a.agents)
	for id, old := range next.Pairings {
		if old.State != "paired" && old.State != "linked" && now.After(old.ExpiresAt) {
			delete(next.Pairings, id)
		}
	}
	if len(next.Pairings) >= 64 {
		return nil, fmt.Errorf("配对记录已达上限，请先移除不再使用的会话")
	}
	next.Pairings[p.ID] = p
	if err := a.saveAgentsLocked(next); err != nil {
		return nil, err
	}
	return map[string]any{"pairing": pairingPublic(p), "code": code}, nil
}

// A runtime token is already scoped to a specific execution. Personal tool
// callers must provide transport identity; a supplied thread ID is never enough.
func (a *App) pairAgent(ctx context.Context, in agentPairInput, runtimeID string) (AgentPairing, error) {
	a.agentMu.Lock()
	var p AgentPairing
	for _, candidate := range a.agents.Pairings {
		if candidate.CodeHash != "" && subtle.ConstantTimeCompare([]byte(candidate.CodeHash), []byte(agentCodeHash(in.Code))) == 1 {
			p = candidate
			break
		}
	}
	a.agentMu.Unlock()
	if p.ID == "" || time.Now().After(p.ExpiresAt) {
		return AgentPairing{}, fmt.Errorf("配对码无效或已到期，请在 Team Cross 重新生成")
	}
	if p.SessionID != "" {
		if p.SessionID != in.Caller.SourceID || p.Provider != in.Caller.Provider || (runtimeID != "" && p.SpaceID != runtimeID) || (p.Transport == "claude_channel" && p.ReceiverID != in.ReceiverID) {
			return AgentPairing{}, fmt.Errorf("此配对码已由另一个会话使用")
		}
		return pairingPublic(p), nil
	}
	if _, err := uuid.Parse(in.Caller.SourceID); err != nil {
		a.agentMu.Lock()
		defer a.agentMu.Unlock()
		current, ok := a.agents.Pairings[p.ID]
		if !ok || current.SessionID != "" || time.Now().After(current.ExpiresAt) {
			return AgentPairing{}, fmt.Errorf("配对状态已变化，请重新核对")
		}
		current.State, current.Reason = "unsupported", "客户端没有提供可核对的当前会话身份，暂时不能配对"
		next := libraryClone(a.agents)
		next.Pairings[p.ID] = current
		if err := a.saveAgentsLocked(next); err != nil {
			return AgentPairing{}, err
		}
		return pairingPublic(current), nil
	}
	if in.Caller.Provider != "codex" && in.Caller.Provider != "claude" {
		return AgentPairing{}, fmt.Errorf("当前客户端尚无经过验证的会话接收方式")
	}
	var bound *Session
	a.mu.Lock()
	sessions := make([]*Session, 0, len(a.sessions))
	for _, s := range a.sessions {
		sessions = append(sessions, s)
	}
	a.mu.Unlock()
	for _, s := range sessions {
		s.mu.Lock()
		match := s.record.ExecutionRecord != nil && s.record.SessionID == in.Caller.SourceID && s.record.Provider == in.Caller.Provider && (runtimeID == "" || s.record.ID == runtimeID)
		s.mu.Unlock()
		if match {
			bound = s
			break
		}
	}
	p.Provider, p.SessionID = in.Caller.Provider, in.Caller.SourceID
	if bound != nil {
		bound.mu.Lock()
		p.SpaceID = bound.record.ID
		online := bound.online && bound.process != nil && bound.process.Alive()
		bound.mu.Unlock()
		if !online {
			return AgentPairing{}, fmt.Errorf("接收会话未连接，请先恢复运行时")
		}
		// Read the exact live runtime, without resuming or creating a conversation.
		raw, err := bound.RPC(ctx, "owner", "thread/read", map[string]any{"threadId": p.SessionID, "includeTurns": false}, "")
		if err != nil {
			return AgentPairing{}, err
		}
		var result struct {
			Thread Source `json:"thread"`
		}
		if json.Unmarshal(raw, &result) != nil || result.Thread.ID != p.SessionID {
			return AgentPairing{}, fmt.Errorf("运行时返回了不同的会话身份")
		}
		p.Transport, p.State, p.ReceiverID = "native", "paired", ""
	} else {
		if runtimeID != "" {
			return AgentPairing{}, fmt.Errorf("配对会话不属于当前运行时")
		}
		if _, err := a.currentSource(ctx, in.Caller); err != nil {
			return AgentPairing{}, err
		}
		p.State, p.Reason = "unsupported", "此客户端尚无经过验证的主动接收方式，可继续复制读取提示"
		if in.Caller.Provider == "codex" {
			if _, err := a.connectCodexReceiver(ctx, in.Caller.SourceID); err == nil {
				p.Transport, p.State, p.Reason = "codex_proxy", "paired", ""
			} else {
				p.Reason = "当前会话身份已核对，但原生接收连接未接通；请在原客户端打开此会话后重新连接"
			}
		}
		if in.Caller.Provider == "claude" && in.ReceiverID != "" {
			p.Transport, p.State, p.Reason, p.ReceiverID = "claude_channel", "verifying", "等待客户端接收核验；Claude Code 需要启用 Team Cross Channel", in.ReceiverID
			challenge, err := agentSecret()
			if err != nil {
				return AgentPairing{}, err
			}
			p.Challenge = challenge
		}
	}
	a.agentMu.Lock()
	defer a.agentMu.Unlock()
	// Validate again after slow native IO; cancellation/another claimant wins.
	old, exists := a.agents.Pairings[p.ID]
	if !exists || time.Now().After(old.ExpiresAt) || old.SessionID != "" {
		return AgentPairing{}, fmt.Errorf("配对状态已变化，请重新核对")
	}
	if p.Transport == "claude_channel" {
		if a.agentReceivers[p.ReceiverID] == nil && len(a.agentReceivers) >= 64 {
			return AgentPairing{}, fmt.Errorf("接收连接过多，请稍后重新配对")
		}
		receiver := a.receiverLocked(p.ReceiverID)
		if len(receiver.events) >= cap(receiver.events) {
			return AgentPairing{}, fmt.Errorf("接收连接繁忙，请稍后重新配对")
		}
	}
	next := libraryClone(a.agents)
	next.Pairings[p.ID] = p
	if err := a.saveAgentsLocked(next); err != nil {
		return AgentPairing{}, err
	}
	if p.Transport == "claude_channel" {
		receiver := a.receiverLocked(p.ReceiverID)
		receiver.events <- agentEvent{Kind: "pairing_verification", PairingID: p.ID, Challenge: p.Challenge}
	}
	return pairingPublic(p), nil
}

func (a *App) receiverLocked(id string) *agentReceiver {
	r := a.agentReceivers[id]
	if r == nil {
		r = &agentReceiver{events: make(chan agentEvent, 32), lastSeen: time.Now()}
		a.agentReceivers[id] = r
	}
	return r
}

func (a *App) confirmAgentPairing(ctx context.Context, c agentCaller, id, challenge string) (AgentPairing, error) {
	if _, err := a.currentSource(ctx, c.Caller); err != nil {
		return AgentPairing{}, err
	}
	a.agentMu.Lock()
	defer a.agentMu.Unlock()
	p, ok := a.agents.Pairings[id]
	if !ok || p.State != "verifying" || time.Now().After(p.ExpiresAt) || p.Challenge == "" || p.ReceiverID != c.ReceiverID || p.SessionID != c.Caller.SourceID || p.Provider != c.Caller.Provider || subtle.ConstantTimeCompare([]byte(p.Challenge), []byte(challenge)) != 1 {
		return AgentPairing{}, fmt.Errorf("接收核验不匹配或已到期，请重新配对")
	}
	p.State, p.Reason, p.Challenge = "paired", "", ""
	next := libraryClone(a.agents)
	next.Pairings[id] = p
	if err := a.saveAgentsLocked(next); err != nil {
		return AgentPairing{}, err
	}
	return pairingPublic(p), nil
}

func (a *App) agentPairings() []AgentPairing {
	a.agentMu.Lock()
	out := make([]AgentPairing, 0, len(a.agents.Pairings))
	for _, p := range a.agents.Pairings {
		if p.State != "paired" && p.State != "linked" && time.Now().After(p.ExpiresAt) {
			p.State, p.Reason = "expired", "配对码已到期，请重新生成"
		}
		if p.State == "paired" && p.Transport == "claude_channel" {
			r := a.agentReceivers[p.ReceiverID]
			if r == nil || time.Since(r.lastSeen) > 45*time.Second {
				p.Reason = "接收连接已断开，请在目标会话重新配对"
			}
		}
		out = append(out, pairingPublic(p))
	}
	a.agentMu.Unlock()
	for i := range out {
		if out[i].Transport == "codex_proxy" {
			out[i].Reason = a.codexReceiverReason(context.Background(), out[i])
		}
		if out[i].State == "paired" && out[i].Transport == "native" {
			out[i].Reason = a.nativeAgentReason(out[i])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (a *App) nativeAgentReason(p AgentPairing) string {
	return a.nativeAgentReasonFor(p, "owner")
}

func (a *App) agentSession(p AgentPairing) *Session {
	a.mu.Lock()
	defer a.mu.Unlock()
	if p.RuntimeID != "" {
		return a.receivers[p.RuntimeID]
	}
	return a.sessions[p.SpaceID]
}

func (a *App) nativeAgentReasonFor(p AgentPairing, role string) string {
	s := a.agentSession(p)
	if s == nil {
		return "接收会话已不可用"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.ExecutionRecord == nil || s.record.SessionID != p.SessionID || s.record.Provider != p.Provider {
		return "接收会话身份已变化，请重新配对"
	}
	if s.closed || !s.online || s.process == nil || !s.process.Alive() {
		return "接收会话未连接，请先恢复运行时"
	}
	if s.writer != role {
		return "当前输入权属于其他参与者，请先在空间中交接输入"
	}
	if s.busy || s.starting || len(s.approvals) > 0 || s.nativeWaiting != "" {
		return "接收会话正在运行或等待原生交互，请处理完成后发送"
	}
	return ""
}

func sameAgentRequest(r AgentRequest, in agentRequestInput) bool {
	return r.WorkbenchSpaceID == in.WorkbenchSpaceID && r.TargetID == in.TargetID && r.PairingID == in.PairingID && r.Intent == in.Intent && r.Instruction == in.Instruction && contentHash(r.References) == contentHash(in.References)
}

func (a *App) sendAgentRequest(ctx context.Context, in agentRequestInput) (AgentRequest, error) {
	if in.Role == "" {
		in.Role = "owner"
	}
	if _, err := uuid.Parse(in.RequestID); err != nil {
		return AgentRequest{}, fmt.Errorf("发送需要有效的 requestId")
	}
	in.Instruction = strings.TrimSpace(in.Instruction)
	if in.Instruction == "" || len([]rune(in.Instruction)) > 4000 || (in.Intent != "analyze" && in.Intent != "analyze_reply") {
		return AgentRequest{}, fmt.Errorf("请选择处理方式并填写 1–4000 字的处理要求")
	}
	a.agentMu.Lock()
	previous, exists := a.agents.Requests[in.RequestID]
	p, paired := a.agents.Pairings[in.PairingID]
	a.agentMu.Unlock()
	if exists {
		if !sameAgentRequest(previous, in) {
			return AgentRequest{}, fmt.Errorf("requestId 已用于不同的 Agent 请求")
		}
		return previous, nil
	}
	if !paired || p.State != "paired" {
		return AgentRequest{}, fmt.Errorf("请先完成接收会话配对")
	}
	if p.SpaceID != "" {
		for _, ref := range in.References {
			if ref.SpaceID != p.SpaceID {
				return AgentRequest{}, fmt.Errorf("这个接收会话只能读取它所在空间的内容，请选择其他已配对会话")
			}
		}
	}
	if in.Intent == "analyze_reply" {
		found := false
		for _, ref := range in.References {
			found = found || ref.Kind == "annotation"
		}
		if !found {
			return AgentRequest{}, fmt.Errorf("分析并回复需要至少一条原批注")
		}
	}
	if len(in.References) != 0 || in.WorkbenchSpaceID == "" {
		if err := a.validateLibrarySelection(ctx, in.References, false); err != nil {
			return AgentRequest{}, err
		}
	}
	if p.Transport == "native" {
		if reason := a.nativeAgentReasonFor(p, in.Role); reason != "" {
			return AgentRequest{}, fmt.Errorf("%s", reason)
		}
	}
	if p.Transport == "codex_proxy" {
		if reason := a.codexReceiverReason(ctx, p); reason != "" {
			return AgentRequest{}, fmt.Errorf("%s", reason)
		}
	}
	now := time.Now()
	r := AgentRequest{ID: in.RequestID, PairingID: p.ID, References: libraryClone(in.References), Instruction: in.Instruction, Intent: in.Intent, State: "submitting", CreatedAt: now, UpdatedAt: now}
	r.WorkbenchSpaceID, r.TargetID = in.WorkbenchSpaceID, in.TargetID
	a.agentMu.Lock()
	if old, exists := a.agents.Requests[r.ID]; exists {
		a.agentMu.Unlock()
		if !sameAgentRequest(old, in) {
			return AgentRequest{}, fmt.Errorf("requestId 已用于不同的 Agent 请求")
		}
		return old, nil
	}
	current, exists := a.agents.Pairings[p.ID]
	if !exists || current.State != "paired" || current.SessionID != p.SessionID {
		a.agentMu.Unlock()
		return AgentRequest{}, fmt.Errorf("配对已移除或变化，请重新选择")
	}
	if p.Transport == "claude_channel" {
		receiver := a.agentReceivers[p.ReceiverID]
		if receiver == nil || time.Since(receiver.lastSeen) > 45*time.Second || len(receiver.events) >= cap(receiver.events) {
			a.agentMu.Unlock()
			return AgentRequest{}, fmt.Errorf("接收连接已断开或繁忙，请重新配对后再发送")
		}
	}
	next := libraryClone(a.agents)
	for id, old := range next.Requests {
		if now.Sub(old.CreatedAt) > 30*24*time.Hour && (old.State == "completed" || old.State == "failed") {
			delete(next.Requests, id)
		}
	}
	if len(next.Requests) >= 512 {
		a.agentMu.Unlock()
		return AgentRequest{}, fmt.Errorf("Agent 请求记录已达上限")
	}
	next.Requests[r.ID] = r
	if err := a.saveAgentsLocked(next); err != nil {
		a.agentMu.Unlock()
		return AgentRequest{}, err
	}
	if p.Transport == "claude_channel" {
		a.agentReceivers[p.ReceiverID].events <- agentEvent{Kind: "agent_request", RequestID: r.ID}
		a.agentMu.Unlock()
		return a.agentDeliveryResult(r.ID, "submitted", "", "")
	}
	a.agentMu.Unlock()
	if p.Transport == "codex_proxy" {
		return a.deliverCodexRequest(ctx, p, r)
	}
	// Only the user's explicit send starts a turn. The existing RPC checks input
	// ownership again, records requestId before the write, and never replays it.
	s := a.agentSession(p)
	if s == nil {
		return a.agentDeliveryResult(r.ID, "unknown", "", "接收会话已不可用")
	}
	prompt := "请使用 Team Cross 的 read_agent_request 读取请求 " + r.ID + "，按其中的用户处理要求核对原文并分析。处理结束后用 finish_agent_request 记录结果摘要。"
	result, err := s.RPC(ctx, in.Role, "turn/start", map[string]any{"threadId": p.SessionID, "input": []any{map[string]any{"type": "text", "text": prompt}}}, r.ID)
	if err != nil {
		return a.agentDeliveryResult(r.ID, "unknown", "", err.Error())
	}
	var turn struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	_ = json.Unmarshal(result, &turn)
	return a.agentDeliveryResult(r.ID, "submitted", turn.Turn.ID, "")
}

func (a *App) agentDeliveryResult(id, state, turnID, message string) (AgentRequest, error) {
	a.agentMu.Lock()
	defer a.agentMu.Unlock()
	r := a.agents.Requests[id]
	// An Agent may read/finish before turn/start or the channel write returns.
	if r.State == "submitting" {
		r.State, r.Error = state, message
	}
	r.TurnID, r.UpdatedAt = turnID, time.Now()
	next := libraryClone(a.agents)
	next.Requests[id] = r
	if err := a.saveAgentsLocked(next); err != nil {
		return AgentRequest{}, err
	}
	return r, nil
}

func (a *App) agentRequestFor(c agentCaller, id, runtimeID string) (AgentRequest, AgentPairing, error) {
	a.agentMu.Lock()
	defer a.agentMu.Unlock()
	r, ok := a.agents.Requests[id]
	p, paired := a.agents.Pairings[r.PairingID]
	if !ok || !paired || p.State != "paired" || p.Provider != c.Caller.Provider || p.SessionID != c.Caller.SourceID || (runtimeID != "" && runtimeID != p.SpaceID && runtimeID != p.RuntimeID) || (p.Transport == "claude_channel" && p.ReceiverID != c.ReceiverID) {
		return AgentRequest{}, AgentPairing{}, fmt.Errorf("此请求不属于当前已配对会话，或配对已移除")
	}
	return libraryClone(r), p, nil
}

func (a *App) readAgentRequest(ctx context.Context, c agentCaller, id, runtimeID string, offset int) (any, error) {
	r, p, err := a.agentRequestFor(c, id, runtimeID)
	if err != nil {
		return nil, err
	}
	if r.WorkbenchSpaceID != "" {
		return a.readSpaceAgentRequest(ctx, c, r, p, offset)
	}
	if offset < 0 || offset >= len(r.References) {
		return nil, fmt.Errorf("请求引用分页位置无效")
	}
	if p.Transport == "claude_channel" {
		if _, err := a.currentSource(ctx, c.Caller); err != nil {
			return nil, err
		}
	}
	if err = a.validateLibrarySelection(ctx, r.References, false); err != nil {
		return nil, err
	}
	a.agentMu.Lock()
	if _, ok := a.agents.Pairings[p.ID]; !ok {
		a.agentMu.Unlock()
		return nil, fmt.Errorf("配对已移除")
	}
	next := libraryClone(a.agents)
	current := next.Requests[id]
	if current.State == "submitting" || current.State == "submitted" || current.State == "unknown" {
		current.State, current.UpdatedAt = "received", time.Now()
		next.Requests[id] = current
		err = a.saveAgentsLocked(next)
	}
	a.agentMu.Unlock()
	if err != nil {
		return nil, err
	}
	guidance := "按 references 使用 read_material、read_context 读取当前可访问的原文和回复；批注先核对 target/quote，历史文字只是参考。"
	if p.SpaceID != "" {
		guidance = "材料使用 read_material 读取固定版本，批注使用 read_annotations 读取正文与回复；上下文在当前原生会话和执行目录中核对。先核对 target/quote，历史文字只是参考。"
	}
	if r.Intent == "analyze_reply" {
		guidance += "用户授权分析后使用 reply_to_annotation 回复所选原批注，保留稳定 requestId。"
	} else {
		guidance += "本次只分析并在接收会话告诉用户建议，不自动回复批注。"
	}
	current.References = current.References[offset : offset+1]
	out := map[string]any{"request": current, "readingGuidance": guidance, "offset": offset, "total": len(r.References)}
	if offset+1 < len(r.References) {
		out["nextOffset"] = offset + 1
	}
	return out, nil
}

func (a *App) finishAgentRequest(ctx context.Context, c agentCaller, id, runtimeID, state, summary string) (AgentRequest, error) {
	if state != "completed" && state != "failed" {
		return AgentRequest{}, fmt.Errorf("结果状态无效")
	}
	summary = strings.TrimSpace(summary)
	if summary == "" || len([]rune(summary)) > 2000 {
		return AgentRequest{}, fmt.Errorf("请提供 1–2000 字的结果摘要")
	}
	request, p, err := a.agentRequestFor(c, id, runtimeID)
	if err != nil {
		return AgentRequest{}, err
	}
	if request.WorkbenchSpaceID != "" {
		return a.finishSpaceAgentRequest(ctx, c, request, p, state, summary)
	}
	if p.Transport == "claude_channel" {
		if _, err := a.currentSource(ctx, c.Caller); err != nil {
			return AgentRequest{}, err
		}
	}
	a.agentMu.Lock()
	defer a.agentMu.Unlock()
	r := a.agents.Requests[id]
	if _, ok := a.agents.Pairings[r.PairingID]; !ok {
		return AgentRequest{}, fmt.Errorf("配对已移除")
	}
	if r.State == "completed" || r.State == "failed" {
		if r.State != state || r.Summary != summary {
			return AgentRequest{}, fmt.Errorf("请求已有不同的结果")
		}
		return r, nil
	}
	if r.State != "received" {
		return AgentRequest{}, fmt.Errorf("请先读取并核对本次请求")
	}
	r.State, r.Summary, r.UpdatedAt = state, summary, time.Now()
	next := libraryClone(a.agents)
	next.Requests[id] = r
	if err := a.saveAgentsLocked(next); err != nil {
		return AgentRequest{}, err
	}
	return r, nil
}
