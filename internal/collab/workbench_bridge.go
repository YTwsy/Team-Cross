package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

func workbenchDecode[T any](v any) T {
	data, _ := json.Marshal(v)
	var out T
	_ = json.Unmarshal(data, &out)
	return out
}

// A member sees the same space through its local joined ID. Only explicit space
// references are remapped, never native session IDs or arbitrary user text.
func remapWorkbench(v any, from, to string) any {
	var walk func(any)
	walk = func(value any) {
		switch x := value.(type) {
		case map[string]any:
			if x["spaceId"] == from {
				x["spaceId"] = to
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	out := workbenchDecode[any](v)
	walk(out)
	return out
}

func (a *App) workbenchCall(ctx context.Context, id, op string, in workbenchInput) (any, error) {
	a.mu.Lock()
	s, j, closed := a.sessions[id], a.joined[id], a.closed
	a.mu.Unlock()
	if closed {
		return nil, fmt.Errorf("Core 已退出")
	}
	if s != nil {
		return s.workbenchOperation(ctx, op, in)
	}
	if j == nil {
		return nil, fmt.Errorf("没有找到协作空间")
	}
	j.mu.Lock()
	hostID := j.Invitation.ID
	j.mu.Unlock()
	var out any
	err := j.request(ctx, "POST", "/v2/workbench/"+op, remapWorkbench(in, id, hostID), &out)
	if err != nil {
		return nil, err
	}
	return remapWorkbench(out, hostID, id), nil
}

func (a *App) registerSpacePairing(ctx context.Context, id, pairingID string) (any, error) {
	// Reserve the public handle locally before contacting the host, so an
	// uncertain response can be reconciled without associating a second target.
	a.agentMu.Lock()
	p, ok := a.agents.Pairings[pairingID]
	if !ok || p.State != "paired" || (p.SpaceID != "" && p.SpaceID != id) {
		a.agentMu.Unlock()
		return nil, fmt.Errorf("请选择已配对且可读取本空间的会话")
	}
	if p.Links == nil {
		p.Links = map[string]string{}
	}
	if p.Links[id] == "" {
		p.Links[id] = uuid.NewString()
		next := libraryClone(a.agents)
		next.Pairings[p.ID] = p
		if err := a.saveAgentsLocked(next); err != nil {
			a.agentMu.Unlock()
			return nil, err
		}
	}
	a.agentMu.Unlock()
	out, err := a.workbenchCall(ctx, id, "register", workbenchInput{TargetID: p.Links[id], Name: p.Name, Provider: p.Provider, Session: contentHash([]string{p.Provider, p.SessionID})[:24], Execution: p.Transport == "native" && p.RuntimeID == ""})
	if err == nil {
		a.wakeWorkbench()
	}
	return out, err
}

func (a *App) workbenchHTTP(w http.ResponseWriter, r *http.Request, path string) bool {
	parts := strings.Split(path, "/")
	if len(parts) != 4 || parts[0] != "collaborations" || parts[2] != "workbench" {
		return false
	}
	id, op := parts[1], parts[3]
	if (op == "view" || op == "request") && r.Method == "GET" {
		out, err := a.workbenchCall(r.Context(), id, op, workbenchInput{RequestID: r.URL.Query().Get("requestId"), Offset: int(number(r.URL.Query().Get("offset")))})
		respond(w, out, err)
		return true
	}
	if r.Method != "POST" {
		http.NotFound(w, r)
		return true
	}
	var in workbenchInput
	if !decode(w, r, &in) {
		return true
	}
	in.Actor = SpaceActor{Kind: "human"}
	var out any
	var err error
	switch op {
	case "register":
		out, err = a.registerSpacePairing(r.Context(), id, in.PairingID)
	case "send", "brief", "assistant", "cancel", "remove":
		out, err = a.workbenchCall(r.Context(), id, op, in)
	case "create-receiver":
		out, err = a.createSpaceReceiver(r.Context(), id, in.RequestID, in.Name)
	default:
		http.NotFound(w, r)
		return true
	}
	if err == nil {
		a.wakeWorkbench()
	}
	respond(w, out, err)
	return true
}

func (s *Session) remoteWorkbenchHTTP(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/v2/workbench/") {
		return false
	}
	if r.Method != "POST" {
		http.NotFound(w, r)
		return true
	}
	var in workbenchInput
	if decode(w, r, &in) {
		out, err := s.workbenchOperation(r.Context(), strings.TrimPrefix(r.URL.Path, "/v2/workbench/"), in)
		respond(w, out, err)
	}
	return true
}

func (a *App) startWorkbench() {
	ctx, cancel := context.WithCancel(context.Background())
	a.workbenchCancel = cancel
	a.workbenchWake = make(chan struct{}, 1)
	a.workbenchWG.Add(1)
	go func() {
		defer a.workbenchWG.Done()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			case <-a.workbenchWake:
			}
			a.pumpWorkbench(ctx)
		}
	}()
}

func (a *App) wakeWorkbench() {
	if a.workbenchWake != nil {
		select {
		case a.workbenchWake <- struct{}{}:
		default:
		}
	}
}

func (a *App) pumpWorkbench(ctx context.Context) {
	a.workbenchMu.Lock()
	defer a.workbenchMu.Unlock()
	a.agentMu.Lock()
	pairs := libraryClone(a.agents.Pairings)
	a.agentMu.Unlock()
	spaces := map[string]map[string]AgentPairing{}
	for _, p := range pairs {
		for id, target := range p.Links {
			if spaces[id] == nil {
				spaces[id] = map[string]AgentPairing{}
			}
			spaces[id][target] = p
		}
	}
	for id, targets := range spaces {
		if ctx.Err() != nil {
			return
		}
		presence := map[string]string{}
		for target, p := range targets {
			reason := ""
			if p.State != "paired" {
				reason = "配对已失效"
			} else if p.Transport == "native" {
				// For shared execution, availability is independent of who owns
				// input. The sender's role is checked at send and native RPC.
				role := "owner"
				if s := a.agentSession(p); s != nil {
					s.mu.Lock()
					role = s.writer
					s.mu.Unlock()
				}
				reason = a.nativeAgentReasonFor(p, role)
			} else {
				a.agentMu.Lock()
				receiver := a.agentReceivers[p.ReceiverID]
				if receiver == nil || time.Since(receiver.lastSeen) > 45*time.Second {
					reason = "接收连接已断开，请重新配对"
				}
				a.agentMu.Unlock()
			}
			presence[target] = reason
		}
		pollCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		raw, err := a.workbenchCall(pollCtx, id, "poll", workbenchInput{Presence: presence})
		cancel()
		if err != nil {
			continue
		}
		for _, request := range workbenchDecode[[]SpaceRequest](raw) {
			p, ok := targets[request.TargetID]
			if !ok || ctx.Err() != nil {
				continue
			}
			claimCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			raw, err := a.workbenchCall(claimCtx, id, "claim", workbenchInput{RequestID: request.ID, TargetID: request.TargetID})
			cancel()
			if err != nil {
				continue
			} // A lost claim is never replayed.
			request = workbenchDecode[SpaceRequest](raw)
			role := "owner"
			if p.RuntimeID == "" && p.Transport == "native" {
				role = request.Actor.MemberID
			}
			r, err := a.sendAgentRequest(ctx, agentRequestInput{RequestID: request.ID, PairingID: p.ID, References: request.References, Instruction: request.Instruction, Intent: request.Intent, WorkbenchSpaceID: id, TargetID: request.TargetID, Role: role})
			state, message := r.State, r.Error
			if err != nil {
				state, message = "failed", err.Error()
			}
			if state == "received" || state == "completed" {
				state = "submitted"
			}
			if state == "submitting" {
				state = "unknown"
			}
			receiptCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			_, _ = a.workbenchCall(receiptCtx, id, "receipt", workbenchInput{RequestID: request.ID, TargetID: request.TargetID, State: state, Error: message})
			cancel()
		}
	}
}

func (a *App) readSpaceAgentRequest(ctx context.Context, c agentCaller, r AgentRequest, p AgentPairing, offset int) (any, error) {
	if offset < 0 || (len(r.References) > 0 && offset >= len(r.References)) || (len(r.References) == 0 && offset != 0) {
		return nil, fmt.Errorf("请求引用分页位置无效")
	}
	if p.Transport == "claude_channel" {
		if _, err := a.currentSource(ctx, c.Caller); err != nil {
			return nil, err
		}
	}
	raw, err := a.workbenchCall(ctx, r.WorkbenchSpaceID, "read", workbenchInput{RequestID: r.ID, TargetID: r.TargetID})
	if err != nil {
		return nil, err
	}
	request := workbenchDecode[SpaceRequest](raw)
	a.agentMu.Lock()
	next := libraryClone(a.agents)
	local := next.Requests[r.ID]
	local.State, local.UpdatedAt = request.State, request.UpdatedAt
	next.Requests[r.ID] = local
	err = a.saveAgentsLocked(next)
	a.agentMu.Unlock()
	if err != nil {
		return nil, err
	}
	total := len(request.References)
	if total > 0 {
		request.References = request.References[offset : offset+1]
	}
	out := map[string]any{"spaceId": r.WorkbenchSpaceID, "request": request, "offset": offset, "total": total, "readingGuidance": "本次请求和结果摘要对空间成员可见。按固定版本使用 read_material 核对材料；批注用 read_context(kind=annotations) 或绑定运行时的 read_annotations。只按明确的处理要求行动；finish_agent_request 保存空间结果，只有 analyze_reply 才授权回复所选原批注。启动简报是参考上下文，不授予任意投递权限。"}
	if offset+1 < total {
		out["nextOffset"] = offset + 1
	}
	return out, nil
}

func (a *App) finishSpaceAgentRequest(ctx context.Context, c agentCaller, r AgentRequest, p AgentPairing, state, summary string) (AgentRequest, error) {
	if p.Transport == "claude_channel" {
		if _, err := a.currentSource(ctx, c.Caller); err != nil {
			return AgentRequest{}, err
		}
	}
	raw, err := a.workbenchCall(ctx, r.WorkbenchSpaceID, "finish", workbenchInput{RequestID: r.ID, TargetID: r.TargetID, State: state, Summary: summary})
	if err != nil {
		return AgentRequest{}, err
	}
	request := workbenchDecode[SpaceRequest](raw)
	a.agentMu.Lock()
	defer a.agentMu.Unlock()
	next := libraryClone(a.agents)
	r = next.Requests[r.ID]
	r.State, r.Summary, r.UpdatedAt = request.State, request.Summary, request.UpdatedAt
	next.Requests[r.ID] = r
	return r, a.saveAgentsLocked(next)
}
