package collab

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"sort"

	"github.com/google/uuid"
	"teamcross/internal/inflow"
	"teamcross/internal/mcp"
)

type inFlowCall struct {
	Caller    mcp.CallerSource `json:"caller"`
	Operation string           `json:"operation"`
	Batch     inflow.Batch     `json:"batch"`
}

func (a *App) inFlowHTTP(w http.ResponseWriter, r *http.Request, path string) bool {
	if path != "inflow" {
		return false
	}
	if r.Method != "POST" || r.Header.Get("Origin") != "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+a.Token)) != 1 {
		http.Error(w, "顺流感知访问不可用", 403)
		return true
	}
	var in inFlowCall
	if decode(w, r, &in) {
		out, err := a.personalInFlow(r.Context(), in)
		respond(w, out, err)
	}
	return true
}

func validInFlowOperation(op string) bool {
	return op == "prepare" || op == "delivered" || op == "release"
}

// Identity comes from the local authenticated transport. Matching an existing
// association is sufficient for passive reads; never start/attach a native client.
func (a *App) personalInFlow(ctx context.Context, in inFlowCall) (any, error) {
	return a.personalInFlowScope(ctx, in, "")
}

func (a *App) personalInFlowScope(ctx context.Context, in inFlowCall, scope string) (any, error) {
	if !validInFlowOperation(in.Operation) {
		return nil, fmt.Errorf("未知顺流操作")
	}
	if !mcp.ChatGPTConversation(in.Caller) {
		if _, err := uuid.Parse(in.Caller.SourceID); err != nil || (in.Caller.Provider != "codex" && in.Caller.Provider != "claude") {
			return nil, fmt.Errorf("缺少当前会话身份")
		}
	}
	if out, handled, err := a.shadowReturnInFlow(ctx, in, scope); handled {
		return out, err
	}
	// A managed execution already has an exact native identity. This also lets a
	// trusted user-level hook reach it without creating an artificial pairing.
	a.mu.Lock()
	executions := make([]*Session, 0, len(a.executions))
	for _, ex := range a.executions {
		executions = append(executions, ex)
	}
	a.mu.Unlock()
	for _, ex := range executions {
		ex.mu.Lock()
		matches := ex.record.Provider == in.Caller.Provider && ex.record.SessionID == in.Caller.SourceID
		ex.mu.Unlock()
		if matches && scope == "" {
			return a.runtimeInFlow(ctx, ex, in)
		}
	}
	a.agentMu.Lock()
	pairs := libraryClone(a.agents.Pairings)
	a.agentMu.Unlock()
	type association struct{ space, target string }
	unique := map[association]bool{}
	for _, p := range pairs {
		if p.Provider != in.Caller.Provider || p.SessionID != in.Caller.SourceID || (p.State != "linked" && p.State != "paired" && p.State != "verifying") {
			continue
		}
		for space, target := range p.Links {
			if scope == "" || space == scope {
				unique[association{space, target}] = true
			}
		}
	}
	targets := []association{}
	for target := range unique {
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].space == targets[j].space {
			return targets[i].target < targets[j].target
		}
		return targets[i].space < targets[j].space
	})
	for _, target := range targets {
		if in.Operation != "prepare" && (target.space != in.Batch.SpaceID || target.target != in.Batch.TargetID || in.Batch.ExecutionID != "") {
			continue
		}
		out, err := a.workbenchCall(ctx, target.space, "inflow-"+in.Operation, workbenchInput{TargetID: target.target, Receipt: in.Batch.Receipt, Actor: inFlowActor(in.Caller)})
		if in.Operation != "prepare" {
			return out, err
		}
		if err != nil {
			continue
		} // Existing connection UI handles unavailable hosts.
		batch := workbenchDecode[inflow.Batch](out)
		if len(batch.Items) > 0 {
			return batch, nil
		}
	}
	if in.Operation != "prepare" {
		return nil, fmt.Errorf("接收会话已解除关联")
	}
	return inflow.Batch{Items: []inflow.Item{}}, nil
}

func inFlowActor(c mcp.CallerSource) SpaceActor {
	return SpaceActor{Kind: "session", Provider: c.Provider, Session: contentHash([]string{c.Provider, c.SourceID})[:24]}
}

// s has already been authenticated by its runtime-only endpoint. The caller
// cannot select another runtime, space, or target in the delivery body.
func (a *App) runtimeInFlow(ctx context.Context, s *Session, in inFlowCall) (any, error) {
	if !validInFlowOperation(in.Operation) {
		return nil, fmt.Errorf("未知顺流操作")
	}
	s.mu.Lock()
	space, id, source, provider := s.record.SpaceID, s.record.ID, s.record.SessionID, s.record.Provider
	receiver := s.receiverSpace
	s.mu.Unlock()
	if in.Caller.SourceID != "" && (in.Caller.SourceID != source || in.Caller.Provider != provider) {
		return nil, fmt.Errorf("顺流感知不属于当前会话")
	}
	caller := mcp.CallerSource{Provider: provider, SourceID: source}
	in.Caller = caller
	if receiver != "" {
		in.Caller = caller
		return a.personalInFlowScope(ctx, in, receiver)
	}
	if out, handled, err := a.shadowReturnInFlow(ctx, in, space); handled {
		return out, err
	}
	if in.Operation != "prepare" && (in.Batch.SpaceID != space || (in.Batch.ExecutionID != "" && in.Batch.ExecutionID != id) || in.Batch.TargetID != "") {
		return nil, fmt.Errorf("顺流批次不属于绑定协作会话")
	}
	return a.workbenchCall(ctx, space, "inflow-"+in.Operation, workbenchInput{ExecutionID: id, Receipt: in.Batch.Receipt, Actor: inFlowActor(caller)})
}

func (a *App) configureCurrentInFlow(ctx context.Context, space string, c mcp.CallerSource, runtimeID string, enabled bool) (any, error) {
	if runtimeID != "" {
		a.mu.Lock()
		ex := a.executions[runtimeID]
		a.mu.Unlock()
		if ex != nil {
			out, err := a.workbenchCall(ctx, space, "inflow-config", workbenchInput{ExecutionID: runtimeID, Enabled: enabled, Actor: inFlowActor(c)})
			if err == nil {
				_, err = a.configureTaskReturns(space, c, enabled)
			}
			return out, err
		}
	}
	a.agentMu.Lock()
	pairs := libraryClone(a.agents.Pairings)
	a.agentMu.Unlock()
	targets := map[string]bool{}
	for _, p := range pairs {
		if p.Provider == c.Provider && p.SessionID == c.SourceID && p.Links[space] != "" && (p.State == "paired" || p.State == "linked" || p.State == "verifying") {
			targets[p.Links[space]] = true
		}
	}
	if len(targets) > 1 {
		return nil, fmt.Errorf("当前会话有多个空间关联，请在界面选择准确的关联设置顺流感知")
	}
	for target := range targets {
		out, err := a.workbenchCall(ctx, space, "inflow-config", workbenchInput{TargetID: target, Enabled: enabled, Actor: inFlowActor(c)})
		if err == nil {
			_, err = a.configureTaskReturns(space, c, enabled)
		}
		return out, err
	}
	if found, err := a.configureTaskReturns(space, c, enabled); found || err != nil {
		return map[string]any{"spaceId": space, "enabled": enabled, "scope": "task_returns"}, err
	}
	return nil, fmt.Errorf("请先明确关联当前会话到此空间")
}
