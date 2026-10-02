package collab

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/mcp"
)

func (a *App) agentsHTTP(w http.ResponseWriter, r *http.Request, path string) bool {
	if !strings.HasPrefix(path, "agent-") {
		return false
	}
	if strings.HasPrefix(path, "agent-tools/") || path == "agent-receivers/poll" {
		if r.Method != "POST" || r.Header.Get("Origin") != "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+a.Token)) != 1 {
			http.Error(w, "会话工具访问不可用", 403)
			return true
		}
		if path == "agent-receivers/poll" {
			a.pollAgentReceiver(w, r)
			return true
		}
		var in struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
			agentCaller
		}
		if !decode(w, r, &in) {
			return true
		}
		out, err := a.invokeAgentTool(r, in.Name, in.Arguments, in.agentCaller, "")
		respond(w, out, err)
		return true
	}
	if path == "agent-pairings" {
		if r.Method == "GET" {
			respond(w, a.agentPairings(), nil)
			return true
		}
		if r.Method == "POST" {
			var in struct {
				Name string `json:"name"`
			}
			if decode(w, r, &in) {
				out, err := a.createAgentPairing(in.Name)
				respond(w, out, err)
			}
			return true
		}
	}
	if path == "agent-requests" && r.Method == "POST" {
		var in agentRequestInput
		if decode(w, r, &in) {
			out, err := a.sendAgentRequest(r.Context(), in)
			respond(w, out, err)
		}
		return true
	}
	if path == "agent-requests" && r.Method == "GET" {
		a.agentMu.Lock()
		requests := make([]AgentRequest, 0, len(a.agents.Requests))
		for _, request := range a.agents.Requests {
			requests = append(requests, libraryClone(request))
		}
		a.agentMu.Unlock()
		sort.Slice(requests, func(i, j int) bool { return requests[i].CreatedAt.After(requests[j].CreatedAt) })
		if len(requests) > 20 {
			requests = requests[:20]
		}
		respond(w, requests, nil)
		return true
	}
	parts := strings.Split(path, "/")
	if len(parts) == 2 && parts[0] == "agent-requests" && r.Method == "GET" {
		a.agentMu.Lock()
		request, ok := a.agents.Requests[parts[1]]
		a.agentMu.Unlock()
		if !ok {
			respond(w, nil, fmt.Errorf("没有找到 Agent 请求"))
		} else {
			respond(w, request, nil)
		}
		return true
	}
	if len(parts) == 3 && parts[0] == "agent-pairings" && parts[2] == "remove" && r.Method == "POST" {
		a.agentMu.Lock()
		next := libraryClone(a.agents)
		delete(next.Pairings, parts[1])
		err := a.saveAgentsLocked(next)
		a.agentMu.Unlock()
		respond(w, map[string]bool{"ok": err == nil}, err)
		return true
	}
	http.NotFound(w, r)
	return true
}

func (a *App) pollAgentReceiver(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ReceiverID string `json:"receiverId"`
	}
	if !decode(w, r, &in) {
		return
	}
	if _, err := uuid.Parse(in.ReceiverID); err != nil {
		respond(w, nil, fmt.Errorf("接收连接标识无效"))
		return
	}
	a.agentMu.Lock()
	for id, receiver := range a.agentReceivers {
		if time.Since(receiver.lastSeen) > time.Minute && id != in.ReceiverID {
			delete(a.agentReceivers, id)
		}
	}
	if a.agentDone == nil || (a.agentReceivers[in.ReceiverID] == nil && len(a.agentReceivers) >= 64) {
		a.agentMu.Unlock()
		respond(w, nil, fmt.Errorf("接收服务不可用"))
		return
	}
	receiver := a.receiverLocked(in.ReceiverID)
	receiver.lastSeen = time.Now()
	done := a.agentDone
	a.agentMu.Unlock()
	timer := time.NewTimer(25 * time.Second)
	defer timer.Stop()
	var event *agentEvent
	select {
	case e := <-receiver.events:
		event = &e
	case <-r.Context().Done():
		return
	case <-done:
		return
	case <-timer.C:
	}
	a.agentMu.Lock()
	receiver.lastSeen = time.Now()
	a.agentMu.Unlock()
	// A dequeued event is never replayed. Only the Agent's subsequent tool call
	// proves receipt; a lost HTTP/stdio response remains unconfirmed.
	respond(w, map[string]any{"event": event}, nil)
}

func (a *App) invokeAgentTool(r *http.Request, name string, args map[string]any, c agentCaller, runtimeID string) (any, error) {
	if err := mcp.ValidateAgentCall(name, args); err != nil {
		return nil, err
	}
	value := func(key string) string { v, _ := args[key].(string); return v }
	switch name {
	case "pair_current_session":
		return a.pairAgent(r.Context(), agentPairInput{agentCaller: c, Code: value("code")}, runtimeID)
	case "confirm_pairing":
		return a.confirmAgentPairing(r.Context(), c, value("pairingId"), value("challenge"))
	case "read_agent_request":
		offset := 0
		if raw, ok := args["offset"]; ok {
			data, _ := json.Marshal(raw)
			if err := json.Unmarshal(data, &offset); err != nil {
				return nil, fmt.Errorf("请求引用分页位置无效")
			}
		}
		return a.readAgentRequest(r.Context(), c, value("requestId"), runtimeID, offset)
	case "finish_agent_request":
		return a.finishAgentRequest(r.Context(), c, value("requestId"), runtimeID, value("status"), value("summary"))
	}
	return nil, fmt.Errorf("未知的会话工具")
}

// Keep the runtime route's authority narrower than the personal MCP route.
func (s *Session) runtimeAgentCaller(c mcp.CallerSource) (agentCaller, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.ExecutionRecord == nil || c.SourceID != s.record.SessionID || c.Provider != s.record.Provider {
		return agentCaller{}, fmt.Errorf("工具调用不属于当前协作会话")
	}
	return agentCaller{Caller: c}, nil
}
