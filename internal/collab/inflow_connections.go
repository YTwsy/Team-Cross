package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/mcp"
)

type inFlowSourceInput struct {
	Provider string `json:"provider"`
	SourceID string `json:"sourceId"`
	Name     string `json:"name,omitempty"`
}

func decodeInFlowConnection(w http.ResponseWriter, r *http.Request, out any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		respond(w, nil, fmt.Errorf("顺流关联参数无效: %w", err))
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		respond(w, nil, fmt.Errorf("顺流关联需要一组明确参数"))
		return false
	}
	return true
}

// The local UI explicitly selects a catalog entry; a plugin supplies its own
// transport identity. Neither path borrows a writer or requires a model turn.
// The existing linked record stores only the local identity/space association;
// active reception keeps its separate verification and lifetime.
func (a *App) connectInFlow(ctx context.Context, space string, caller mcp.CallerSource, name string) (any, error) {
	if _, err := a.workbenchCall(ctx, space, "receiver-check", workbenchInput{}); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if len([]rune(name)) > 80 {
		return nil, fmt.Errorf("接收会话名称最多 80 字")
	}
	var source Source
	if !mcp.ChatGPTConversation(caller) {
		if _, err := uuid.Parse(caller.SourceID); err != nil || (caller.Provider != "codex" && caller.Provider != "claude") {
			return nil, fmt.Errorf("请选择有效的本机会话")
		}
		var read struct {
			Thread Source `json:"thread"`
		}
		if err := a.sourceCall(ctx, caller.Provider, "thread/read", map[string]any{"threadId": caller.SourceID, "includeTurns": false}, &read); err != nil {
			return nil, err
		}
		if read.Thread.ID != caller.SourceID {
			return nil, fmt.Errorf("无法核对选定的本机会话")
		}
		source = read.Thread
	}
	a.agentConnectMu.Lock()
	defer a.agentConnectMu.Unlock()
	// Managed sessions retain their exact space and execution restrictions.
	a.mu.Lock()
	runtimes := make([]*Session, 0, len(a.executions)+len(a.receivers))
	for _, s := range a.executions {
		runtimes = append(runtimes, s)
	}
	for _, s := range a.receivers {
		runtimes = append(runtimes, s)
	}
	a.mu.Unlock()
	var executionID, runtimeID, boundSpace string
	for _, s := range runtimes {
		s.mu.Lock()
		if s.record.Provider == caller.Provider && s.record.SessionID == caller.SourceID {
			boundSpace = s.record.SpaceID
			if s.receiverSpace != "" {
				boundSpace, runtimeID = s.receiverSpace, s.record.ID
			} else {
				executionID = s.record.ID
			}
		}
		s.mu.Unlock()
	}
	if boundSpace != "" && boundSpace != space {
		return nil, fmt.Errorf("此会话只可关联所属空间")
	}
	a.agentMu.Lock()
	var p AgentPairing
	for _, candidate := range a.agents.Pairings {
		if candidate.Provider != caller.Provider || candidate.SessionID != caller.SourceID || (candidate.State != "paired" && candidate.State != "linked" && candidate.State != "verifying") {
			continue
		}
		if candidate.SpaceID != "" && candidate.SpaceID != space {
			continue
		}
		if candidate.Links[space] != "" {
			if p.ID != "" && p.Links[space] != "" && p.Links[space] != candidate.Links[space] {
				a.agentMu.Unlock()
				return nil, fmt.Errorf("当前会话有多个空间关联，请在界面选择准确的关联设置顺流感知")
			}
			p = candidate
		} else if p.ID == "" {
			p = candidate
		}
	}
	if p.ID == "" {
		if len(a.agents.Pairings) >= 64 {
			a.agentMu.Unlock()
			return nil, fmt.Errorf("配对记录已达上限，请先移除不再使用的会话")
		}
		p = AgentPairing{ID: uuid.NewString(), Provider: caller.Provider, SessionID: caller.SourceID, State: "linked", Transport: "inflow", CreatedAt: time.Now(), SpaceID: boundSpace, ExecutionID: executionID, RuntimeID: runtimeID}
	}
	if name != "" {
		p.Name, p.NameSource = name, "custom"
	} else if !customAgentName(p) {
		p.Name, p.NameSource = defaultAgentName(caller.Provider, caller.SourceID, source.Name)
	}
	next := libraryClone(a.agents)
	next.Pairings[p.ID] = p
	err := a.saveAgentsLocked(next)
	a.agentMu.Unlock()
	if err != nil {
		return nil, err
	}
	raw, err := a.registerSpacePairing(ctx, space, p.ID)
	if err != nil {
		return nil, err
	}
	target := workbenchDecode[SpaceTarget](raw)
	if _, err = a.workbenchCall(ctx, space, "inflow-config", workbenchInput{TargetID: target.ID, Enabled: true}); err != nil {
		return nil, err
	}
	_, err = a.configureTaskReturns(space, caller, true)
	if err != nil {
		return nil, err
	}
	return map[string]any{"linked": true, "inFlow": true, "targetId": target.ID, "name": p.Name}, nil
}
