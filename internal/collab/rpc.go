package collab

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"teamcross/internal/nativecodex"
	"teamcross/internal/workspace"
)

func (s *Session) onMessage(m nativecodex.Message) {
	if localAccountNotification(m.Method) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if m.Method == "teamcross/runtimeDisconnected" {
		s.online = false
		if s.direct != nil {
			s.direct.close()
		}
		return
	}
	s.sequence++
	s.events = append(s.events, Event{Sequence: s.sequence, Method: m.Method, Params: m.Params, Time: time.Now()})
	if len(s.events) > 600 {
		s.events = append([]Event(nil), s.events[len(s.events)-600:]...)
	}
	if m.Method == "turn/started" {
		s.busy = true
	}
	if m.Method == "turn/completed" {
		s.busy = false
		s.record.UpdatedAt = time.Now()
		_ = s.saveLocked()
	}
	if len(m.ID) > 0 {
		s.approvals[string(m.ID)] = Approval{ID: m.ID, Method: m.Method, Params: m.Params}
	}
	if d := s.direct; d != nil && d.subscribed {
		select {
		case d.send <- m:
		default:
			d.close()
		}
	}
}
func (s *Session) Events(after uint64) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := []Event{}
	for _, e := range s.events {
		if e.Sequence > after {
			events = append(events, e)
		}
	}
	approvals := []Approval{}
	for _, p := range s.approvals {
		approvals = append(approvals, p)
	}
	return map[string]any{"events": events, "cursor": s.sequence, "approvals": approvals, "busy": s.busy, "online": s.online}
}
func mutating(method string) bool {
	return method == "turn/start" || method == "turn/steer" || method == "turn/interrupt" || method == "thread/name/set"
}
func (s *Session) RPC(ctx context.Context, role, method string, params map[string]any, requestID string) (json.RawMessage, error) {
	if params == nil {
		params = map[string]any{}
	}
	s.mu.Lock()
	p := s.process
	r := s.record
	if p == nil || !s.online {
		s.mu.Unlock()
		return nil, fmt.Errorf("协作运行时未连接，请让发起者恢复运行时")
	}
	if role == "remote" && s.share == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("共享已结束")
	}
	if method == "thread/list" || method == "thread/loaded/list" {
		s.mu.Unlock()
		var read map[string]any
		if e := p.Call(ctx, "thread/read", map[string]any{"threadId": r.SessionID, "includeTurns": false}, &read); e != nil {
			return nil, e
		}
		if method == "thread/loaded/list" {
			return json.Marshal(map[string]any{"data": []string{r.SessionID}, "nextCursor": nil})
		}
		return json.Marshal(map[string]any{"data": []any{read["thread"]}, "nextCursor": nil})
	}
	switch method {
	case "threadSection/list":
		s.mu.Unlock()
		return json.Marshal(map[string]any{"data": []any{}, "nextCursor": nil})
	case "externalAgentConfig/detect":
		s.mu.Unlock()
		return json.Marshal(map[string]any{"items": []any{}, "connectors": []any{}})
	case "externalAgentConfig/import/readHistories":
		s.mu.Unlock()
		return json.Marshal(map[string]any{"data": []any{}, "connectors": []any{}})
	case "permissionProfile/list":
		s.mu.Unlock()
		return json.Marshal(map[string]any{"data": []any{map[string]any{"id": nativecodex.Profile, "allowed": true, "description": "协作执行目录"}}, "nextCursor": nil})
	case "project/list":
		s.mu.Unlock()
		return json.Marshal(map[string]any{"data": []any{map[string]any{"id": r.ID, "name": r.Title, "roots": []any{map[string]any{"path": r.ExecutionCwd}}, "metadata": map[string]string{}, "position": 0, "createdAt": r.CreatedAt.Unix(), "updatedAt": r.UpdatedAt.Unix()}}, "nextCursor": nil})
	case "fs/readFile":
		path, _ := params["path"].(string)
		s.mu.Unlock()
		if filepath.IsAbs(path) {
			var e error
			path, e = filepath.Rel(r.ExecutionCwd, path)
			if e != nil {
				return nil, e
			}
		}
		content, e := workspace.ReadFile(r.ExecutionCwd, path)
		if e != nil {
			return nil, e
		}
		return json.Marshal(map[string]any{"dataBase64": base64.StdEncoding.EncodeToString([]byte(content))})
	case "getAuthStatus":
		// Desktop's private bootstrap method asks for account bearer tokens. The
		// collaboration gateway may report auth state, but never exports A's token.
		params["includeToken"] = false
		params["refreshToken"] = false
	case "thread/read", "thread/resume", "thread/turns/list", "thread/items/list", "thread/unsubscribe", "thread/goal/get", "thread/name/set", "turn/start", "turn/steer", "turn/interrupt":
		if id, ok := params["threadId"]; ok && id != r.SessionID {
			s.mu.Unlock()
			return nil, fmt.Errorf("此连接只访问指定的协作会话")
		}
		params["threadId"] = r.SessionID
	case "account/read", "account/rateLimits/read", "model/list", "config/read", "configRequirements/read", "skills/list", "hooks/list", "mcpServerStatus/list", "experimentalFeature/list", "app/list", "plugin/list", "collaborationMode/list", "remoteControl/status/read":
		if method == "config/read" {
			params["includeLayers"] = false
			params["cwd"] = r.ExecutionCwd
		}
		if method == "skills/list" {
			params["cwds"] = []string{r.ExecutionCwd}
		}
		if method == "hooks/list" {
			params["cwd"] = r.ExecutionCwd
		}
	default:
		s.mu.Unlock()
		log.Printf("unsupported native method: %s", method)
		return nil, fmt.Errorf("协作入口暂不支持 %s", method)
	}
	if method == "thread/unsubscribe" {
		if s.direct != nil && s.direct.role == role {
			s.direct.subscribed = false
		}
		s.mu.Unlock()
		return json.Marshal(map[string]string{"status": "unsubscribed"})
	}
	if method == "thread/resume" && s.direct != nil && s.direct.role == role {
		s.direct.subscribed = true
	}
	write := mutating(method)
	if write && role != s.writer {
		s.mu.Unlock()
		return nil, fmt.Errorf("当前由另一位参与者输入，请先交接输入")
	}
	if method == "turn/start" || method == "thread/resume" {
		for _, k := range []string{"path", "history", "config", "sandbox", "sandboxPolicy", "baseInstructions", "developerInstructions", "environments", "multiAgentMode", "collaborationMode"} {
			delete(params, k)
		}
		for k, v := range nativecodex.Overrides(r.SessionID, r.ExecutionCwd) {
			params[k] = v
		}
		params["effort"] = "low"
	}
	hash := ""
	if write {
		if requestID == "" {
			s.mu.Unlock()
			return nil, fmt.Errorf("写入需要 requestId")
		}
		b, _ := json.Marshal([]any{method, params})
		sum := sha256.Sum256(b)
		hash = hex.EncodeToString(sum[:])
		if old, ok := s.record.Commands[requestID]; ok {
			s.mu.Unlock()
			if old.Hash != hash {
				return nil, fmt.Errorf("requestId 已用于不同输入")
			}
			if old.State == "completed" {
				return old.Result, nil
			}
			return nil, fmt.Errorf("该输入状态为 %s，请读取会话结果后再决定：%s", old.State, old.Error)
		}
		if method == "turn/start" && s.busy {
			s.mu.Unlock()
			return nil, fmt.Errorf("会话正在执行，请等待完成或使用补充输入")
		}
		s.record.Commands[requestID] = Command{ID: requestID, Hash: hash, State: "pending"}
		if method == "turn/start" {
			s.busy = true
		}
		if err := s.saveLocked(); err != nil {
			delete(s.record.Commands, requestID)
			if method == "turn/start" {
				s.busy = false
			}
			s.mu.Unlock()
			return nil, err
		}
	}
	s.mu.Unlock()
	var result json.RawMessage
	err := p.Call(ctx, method, params, &result)
	if method == "config/read" && err == nil {
		var v map[string]any
		if json.Unmarshal(result, &v) == nil {
			safe := map[string]any{}
			if config, ok := v["config"].(map[string]any); ok {
				for _, key := range []string{"model", "model_provider", "model_reasoning_effort", "model_context_window", "model_auto_compact_token_limit", "approval_policy", "default_permissions", "web_search", "tui", "features", "service_tier", "personality"} {
					if value, ok := config[key]; ok {
						safe[key] = value
					}
				}
			}
			safe["mcp_servers"] = map[string]any{}
			v["config"] = safe
			v["origins"] = map[string]any{}
			delete(v, "layers")
			result, _ = json.Marshal(v)
		}
	}
	if method == "getAuthStatus" && err == nil {
		var v map[string]any
		if json.Unmarshal(result, &v) == nil {
			delete(v, "authToken")
			delete(v, "accessToken")
			delete(v, "token")
			result, _ = json.Marshal(v)
		}
	}
	if write {
		s.mu.Lock()
		c := s.record.Commands[requestID]
		if err != nil {
			c.State = "unknown"
			c.Error = err.Error()
			if strings.Contains(err.Error(), `"code"`) {
				c.State = "failed"
				if method == "turn/start" {
					s.busy = false
				}
			}
		} else {
			c.State = "completed"
			c.Result = result
		}
		s.record.Commands[requestID] = c
		s.record.UpdatedAt = time.Now()
		_ = s.saveLocked()
		s.mu.Unlock()
	}
	return result, err
}
func (s *Session) Respond(ctx context.Context, role string, id json.RawMessage, result any) error {
	s.mu.Lock()
	if role != s.writer || (role == "remote" && s.share == nil) {
		s.mu.Unlock()
		return fmt.Errorf("请先取得输入权")
	}
	if _, ok := s.approvals[string(id)]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("该请求已处理或不存在")
	}
	p := s.process
	if p == nil || !s.online {
		s.mu.Unlock()
		return fmt.Errorf("运行时未连接")
	}
	delete(s.approvals, string(id))
	s.mu.Unlock()
	if e := p.Reply(ctx, id, result); e != nil {
		s.mu.Lock()
		s.record.Error = "审批回应结果不明，请检查运行时"
		s.mu.Unlock()
		return e
	}
	return nil
}
func (s *Session) Context(ctx context.Context, kind, path string, after uint64, cursors ...string) (any, error) {
	s.mu.Lock()
	p := s.process
	r := s.record
	s.mu.Unlock()
	switch kind {
	case "events":
		return s.Events(after), nil
	case "file":
		text, e := workspace.ReadFile(r.ExecutionCwd, path)
		return map[string]any{"path": path, "text": text}, e
	case "changes":
		out, e := workspace.Git(ctx, r.ExecutionCwd, "diff", "HEAD", "--no-ext-diff", "--no-textconv", "--stat")
		if e != nil {
			return nil, e
		}
		diff, e := workspace.Git(ctx, r.ExecutionCwd, "diff", "HEAD", "--no-ext-diff", "--no-textconv", "--", ".")
		if len(diff) > 256<<10 {
			diff = diff[:256<<10]
		}
		status, _ := workspace.Git(ctx, r.ExecutionCwd, "status", "--short")
		return map[string]any{"stat": string(out), "diff": string(diff), "status": string(status)}, e
	default:
		if p == nil {
			return nil, fmt.Errorf("运行时未连接")
		}
		var read struct {
			Thread map[string]any `json:"thread"`
		}
		if e := p.Call(ctx, "thread/read", map[string]any{"threadId": r.SessionID, "includeTurns": false}, &read); e != nil {
			return nil, e
		}
		params := map[string]any{"threadId": r.SessionID, "limit": 8, "itemsView": "full", "sortDirection": "desc"}
		if len(cursors) > 0 && cursors[0] != "" {
			params["cursor"] = cursors[0]
		}
		var page struct {
			Data       []json.RawMessage `json:"data"`
			NextCursor *string           `json:"nextCursor"`
		}
		if e := p.Call(ctx, "thread/turns/list", params, &page); e != nil {
			return nil, e
		}
		slices.Reverse(page.Data)
		read.Thread["turns"] = page.Data
		return map[string]any{"thread": read.Thread, "nextCursor": page.NextCursor}, nil
	}
}
func (s *Session) attach(w http.ResponseWriter, r *http.Request, role string) {
	if r.Header.Get("Origin") != "" {
		http.Error(w, "请使用原生客户端连接", 403)
		return
	}
	s.mu.Lock()
	if s.writer != role || !s.online || s.record.State != "ready" || (role == "remote" && s.share == nil) {
		s.mu.Unlock()
		http.Error(w, "等待输入交接或恢复运行时", 403)
		return
	}
	if s.direct != nil {
		s.mu.Unlock()
		http.Error(w, "请先关闭已有直接操作客户端", 409)
		return
	}
	d := &direct{subscribed: true, role: role, kind: "Codex", send: make(chan nativecodex.Message, 256), done: make(chan struct{})}
	s.direct = d
	init := s.process.Initialization()
	s.mu.Unlock()
	defer func() {
		d.close()
		s.mu.Lock()
		if s.direct == d {
			s.direct = nil
		}
		s.mu.Unlock()
	}()
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(32 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		select {
		case <-d.done:
		case <-ctx.Done():
		}
		_ = conn.CloseNow()
	}()
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		for {
			select {
			case <-ctx.Done():
				return
			case <-d.done:
				return
			case m := <-d.send:
				b, _ := json.Marshal(m)
				if conn.Write(ctx, websocket.MessageText, b) != nil {
					d.close()
					return
				}
			}
		}
	}()
	connectionID := uuid.NewString()
	local := &localClient{app: s.app, id: s.record.ID, emit: func(m nativecodex.Message) {
		select {
		case d.send <- m:
		case <-d.done:
		case <-ctx.Done():
		}
	}}
	defer local.close()
	for {
		_, b, e := conn.Read(ctx)
		if e != nil {
			break
		}
		var m nativecodex.Message
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		if m.Method == "initialize" {
			var in struct {
				ClientInfo struct {
					Name string `json:"name"`
				} `json:"clientInfo"`
			}
			_ = json.Unmarshal(m.Params, &in)
			s.mu.Lock()
			d.kind = in.ClientInfo.Name
			s.mu.Unlock()
			d.send <- nativecodex.Message{ID: m.ID, Result: init}
			continue
		}
		if m.Method == "initialized" {
			s.mu.Lock()
			for _, a := range s.approvals {
				d.send <- nativecodex.Message{ID: a.ID, Method: a.Method, Params: a.Params}
			}
			s.mu.Unlock()
			continue
		}
		if m.Method == "" {
			if role == "owner" && local.reply(ctx, m) {
				continue
			}
			var result any
			_ = json.Unmarshal(m.Result, &result)
			_ = s.Respond(ctx, role, m.ID, result)
			continue
		}
		var params map[string]any
		_ = json.Unmarshal(m.Params, &params)
		var result json.RawMessage
		if role == "owner" && localClientMethod(m.Method) {
			e = local.call(ctx, m.Method, params, &result)
		} else {
			result, e = s.RPC(ctx, role, m.Method, params, "direct:"+connectionID+":"+string(m.ID))
		}
		if len(m.ID) == 0 {
			continue
		}
		reply := nativecodex.Message{ID: m.ID, Result: result}
		if e != nil {
			reply.Result = nil
			reply.Error, _ = json.Marshal(map[string]any{"code": -32602, "message": e.Error()})
		}
		select {
		case d.send <- reply:
		case <-d.done:
		}
	}
	cancel()
	<-writeDone
}
