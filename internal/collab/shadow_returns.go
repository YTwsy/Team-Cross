package collab

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/inflow"
	"teamcross/internal/mcp"
)

// Native addressing stays on the initiating Core. The space host receives only
// an opaque route and a session fingerprint used to honor existing mute state.
type ShadowReturnRoute struct {
	ID          string    `json:"id"`
	SpaceID     string    `json:"spaceId"`
	ShadowID    string    `json:"shadowId"`
	MessageID   string    `json:"messageId"`
	Provider    string    `json:"provider"`
	SessionID   string    `json:"sessionId"`
	ContentHash string    `json:"contentHash"`
	CreatedAt   time.Time `json:"createdAt"`
}

func taskReturnPreferenceKey(space string, caller mcp.CallerSource) string {
	return contentHash([]string{space, caller.Provider, caller.SourceID})
}

// Persist before sending. An unknown send keeps its original route; retrying
// another conversation or body with that message ID cannot redirect a result.
func (a *App) bindShadowReturn(space string, in shadowInput, caller mcp.CallerSource) (shadowInput, error) {
	if caller.Provider == "" || caller.SourceID == "" {
		return in, fmt.Errorf("分身委托缺少当前会话身份")
	}
	a.agentMu.Lock()
	defer a.agentMu.Unlock()
	for _, route := range a.agents.ShadowReturns {
		if route.SpaceID != space || route.ShadowID != in.ID || route.MessageID != in.Message.ID {
			continue
		}
		if route.Provider != caller.Provider || route.SessionID != caller.SourceID || route.ContentHash != contentHash(in.Message.Text) {
			return in, fmt.Errorf("消息 ID 已属于另一项委托或发起会话")
		}
		in.ReturnRouteID, in.ReturnSessionKey = route.ID, inFlowActor(caller).Session
		return in, nil
	}
	next := libraryClone(a.agents)
	if next.ShadowReturns == nil {
		next.ShadowReturns = map[string]ShadowReturnRoute{}
	}
	route := ShadowReturnRoute{ID: uuid.NewString(), SpaceID: space, ShadowID: in.ID, MessageID: in.Message.ID, Provider: caller.Provider, SessionID: caller.SourceID, ContentHash: contentHash(in.Message.Text), CreatedAt: time.Now()}
	next.ShadowReturns[route.ID] = route
	if err := a.saveAgentsLocked(next); err != nil {
		return in, err
	}
	in.ReturnRouteID, in.ReturnSessionKey = route.ID, inFlowActor(caller).Session
	return in, nil
}

func (a *App) configureTaskReturns(space string, caller mcp.CallerSource, enabled bool) (bool, error) {
	a.agentMu.Lock()
	defer a.agentMu.Unlock()
	found := false
	for _, route := range a.agents.ShadowReturns {
		found = found || route.SpaceID == space && route.Provider == caller.Provider && route.SessionID == caller.SourceID
	}
	if !found {
		return false, nil
	}
	next := libraryClone(a.agents)
	if next.TaskReturnMuted == nil {
		next.TaskReturnMuted = map[string]bool{}
	}
	next.TaskReturnMuted[taskReturnPreferenceKey(space, caller)] = !enabled
	return true, a.saveAgentsLocked(next)
}

// Delivery is an ordinary MCP/Hook attachment. No pairing, native process,
// active receive channel, or follow subscription is created by a delegation.
func (a *App) shadowReturnInFlow(ctx context.Context, in inFlowCall, scope string) (any, bool, error) {
	if in.Operation != "prepare" && in.Batch.ReturnRouteID == "" {
		return nil, false, nil
	}
	a.agentMu.Lock()
	routes := []ShadowReturnRoute{}
	for _, route := range a.agents.ShadowReturns {
		if route.Provider != in.Caller.Provider || route.SessionID != in.Caller.SourceID || scope != "" && route.SpaceID != scope {
			continue
		}
		if in.Operation != "prepare" && (route.ID != in.Batch.ReturnRouteID || route.SpaceID != in.Batch.SpaceID || in.Batch.TargetID != "" || in.Batch.ExecutionID != "") {
			continue
		}
		if in.Operation == "prepare" && a.agents.TaskReturnMuted[taskReturnPreferenceKey(route.SpaceID, in.Caller)] {
			continue
		}
		routes = append(routes, route)
	}
	a.agentMu.Unlock()
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].CreatedAt.Equal(routes[j].CreatedAt) {
			return routes[i].ID < routes[j].ID
		}
		return routes[i].CreatedAt.Before(routes[j].CreatedAt)
	})
	for _, route := range routes {
		if ctx.Err() != nil {
			break
		}
		out, err := a.shadowCall(ctx, route.SpaceID, "return-"+in.Operation, shadowInput{ID: route.ShadowID, ReturnRouteID: route.ID, ReturnSessionKey: inFlowActor(in.Caller).Session, Receipt: in.Batch.Receipt})
		if in.Operation != "prepare" {
			return out, true, err
		}
		if err != nil {
			continue
		}
		batch := workbenchDecode[inflow.Batch](out)
		if len(batch.Items) > 0 {
			return batch, true, nil
		}
	}
	if in.Operation != "prepare" {
		return nil, true, fmt.Errorf("任务返回批次不属于当前发起会话")
	}
	return nil, false, nil
}
