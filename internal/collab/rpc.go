package collab

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"teamcross/internal/nativeclaude"
	"teamcross/internal/nativecodex"
	"teamcross/internal/problem"
	"teamcross/internal/runtimeconfig"
	"teamcross/internal/sharing"
	"teamcross/internal/workspace"
)

func (s *Session) onMessage(m nativecodex.Message) {
	s.onRuntimeMessage(0, m)
}
func (s *Session) onRuntimeMessage(generation uint64, m nativecodex.Message) {
	if localAccountNotification(m.Method) {
		return
	}
	s.mu.Lock()
	defer func() { s.releaseIfIdleLocked(); s.mu.Unlock() }()
	if s.closed || (generation != 0 && s.generation != generation) {
		return
	}
	if m.Method == "teamcross/runtimeDisconnected" {
		s.online = false
		if s.direct != nil {
			s.direct.close()
		}
		return
	}
	if m.Method == "teamcross/claudeState" {
		var state struct {
			Busy       bool   `json:"busy"`
			WaitingFor string `json:"waitingFor"`
		}
		if json.Unmarshal(m.Params, &state) == nil {
			unchanged := s.busy == state.Busy && s.nativeWaiting == state.WaitingFor
			s.busy = state.Busy
			s.nativeWaiting = state.WaitingFor
			if unchanged {
				return
			}
		}
	}
	s.sequence++
	s.observeModelLocked(m.Method, m.Params)
	s.events = append(s.events, Event{Sequence: s.sequence, Method: m.Method, Params: m.Params, Time: time.Now()})
	if len(s.events) > 600 {
		s.events = append([]Event(nil), s.events[len(s.events)-600:]...)
	}
	if m.Method == "turn/started" {
		s.busy = true
	}
	if m.Method == "turn/completed" || m.Method == "teamcross/claudeHistory" {
		if m.Method == "turn/completed" {
			s.busy = false
		}
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
	return method == "turn/start" || method == "turn/steer" || method == "turn/interrupt" || method == "thread/name/set" || method == "thread/settings/update"
}

func resumeMutates(params map[string]any) bool {
	for _, key := range []string{"model", "modelProvider", "effort", "config", "collaborationMode", "permissions", "sandbox", "approvalPolicy", "approvalsReviewer", "runtimeWorkspaceRoots", "baseInstructions", "developerInstructions", "personality", "serviceTier"} {
		if params[key] != nil {
			return true
		}
	}
	return false
}
func (s *Session) RPC(ctx context.Context, role, method string, params map[string]any, requestID string) (json.RawMessage, error) {
	if params == nil {
		params = map[string]any{}
	}
	s.mu.Lock()
	p := s.process
	r := s.snapshotLocked()
	if !sharing.ExecutionAuthorized(ctx, s.share) {
		s.mu.Unlock()
		return nil, fmt.Errorf("尚未获得执行访问")
	}
	if r.ExecutionRecord == nil || p == nil || !s.online {
		s.mu.Unlock()
		return nil, fmt.Errorf("协作运行时未连接，请让发起者恢复运行时")
	}
	if role != "owner" && (s.share == nil || !s.share.HasExecutionAccess(role)) {
		s.mu.Unlock()
		return nil, fmt.Errorf("共享已结束")
	}
	if !s.callerValidLocked(ctx) {
		s.mu.Unlock()
		return nil, fmt.Errorf("连接或输入归属已变化，请重新连接")
	}
	if r.Provider == "claude" {
		if e := claudeMethod(method, params); e != nil {
			s.mu.Unlock()
			return nil, e
		}
	}
	s.activeCalls++
	defer s.finishCall()
	discoveryLookupCwd := ""
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
	case "config/value/write", "config/batchWrite":
		if r.RuntimeMode != runtimeconfig.Trusted || !hookTrustWrite(method, params) {
			s.mu.Unlock()
			return nil, fmt.Errorf("此入口只允许信任模式确认原生 hook")
		}
		// Trust is persisted in the owner's default config, never a path chosen
		// by the remote client. Native reload and optimistic locking still apply.
		delete(params, "filePath")
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
		if r.RuntimeMode == runtimeconfig.Trusted {
			params["cwd"] = r.ExecutionCwd
			break
		}
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
	case "thread/read", "thread/resume", "thread/turns/list", "thread/items/list", "thread/unsubscribe", "thread/goal/get", "thread/name/set", "thread/settings/update", "turn/start", "turn/steer", "turn/interrupt":
		if id, ok := params["threadId"]; ok && id != r.SessionID {
			s.mu.Unlock()
			return nil, fmt.Errorf("此连接只访问指定的协作会话")
		}
		params["threadId"] = r.SessionID
	case "account/read", "account/rateLimits/read", "model/list", "config/read", "configRequirements/read", "skills/list", "hooks/list", "mcpServerStatus/list", "experimentalFeature/list", "app/list", "app/installed", "plugin/list", "collaborationMode/list", "remoteControl/status/read":
		if method == "config/read" {
			params["includeLayers"] = false
			params["cwd"] = r.ExecutionCwd
		}
		if method == "skills/list" || method == "hooks/list" {
			// A remote TUI keys its pending discovery by its local cwd. Resolve
			// only A's execution directory, but preserve that lookup key below.
			if cwds, ok := params["cwds"].([]any); ok && len(cwds) == 1 {
				discoveryLookupCwd, _ = cwds[0].(string)
			}
			delete(params, "cwd")
			params["cwds"] = []string{r.ExecutionCwd}
		}
		if method == "plugin/list" {
			params["cwds"] = []string{r.ExecutionCwd}
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
	write := mutating(method) || hookTrustWrite(method, params) || (method == "thread/resume" && resumeMutates(params))
	if write && role != s.writer {
		s.mu.Unlock()
		return nil, fmt.Errorf("当前由另一位参与者输入，请先交接输入")
	}
	if method == "turn/start" || method == "thread/resume" || method == "thread/settings/update" {
		if mode, ok := params["collaborationMode"].(map[string]any); ok {
			if settings, ok := mode["settings"].(map[string]any); ok {
				if model, ok := settings["model"].(string); ok {
					params["model"] = model
				}
				if effort, ok := settings["reasoning_effort"]; ok {
					params["effort"] = effort
				}
			}
		}
		var modelConfig map[string]any
		if method == "thread/resume" {
			if config, ok := params["config"].(map[string]any); ok {
				modelConfig = map[string]any{}
				for _, k := range []string{"model", "model_reasoning_effort"} {
					if v, ok := config[k]; ok {
						modelConfig[k] = v
					}
				}
			}
			if effort, ok := params["effort"].(string); ok {
				if modelConfig == nil {
					modelConfig = map[string]any{}
				}
				modelConfig["model_reasoning_effort"] = effort
				delete(params, "effort")
			}
		}
		for _, k := range []string{"path", "history", "config", "baseInstructions", "developerInstructions", "environments", "runtimeWorkspaceRoots"} {
			delete(params, k)
		}
		if r.RuntimeMode != runtimeconfig.Trusted {
			for _, k := range []string{"sandbox", "sandboxPolicy", "multiAgentMode", "collaborationMode"} {
				delete(params, k)
			}
		}
		for k, v := range nativecodex.SessionOverrides(r.SessionID, r.ExecutionCwd, r.RuntimeMode) {
			params[k] = v
		}
		if len(modelConfig) > 0 {
			params["config"] = modelConfig
		}
	}
	commandKey := role + ":" + method + ":" + requestID
	hash := ""
	if write {
		if requestID == "" {
			s.mu.Unlock()
			return nil, fmt.Errorf("写入需要 requestId")
		}
		b, _ := json.Marshal([]any{method, params})
		sum := sha256.Sum256(b)
		hash = hex.EncodeToString(sum[:])
		if old, ok := s.record.Commands[commandKey]; ok {
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
		s.record.Commands[commandKey] = Command{ID: requestID, Hash: hash, State: "pending"}
		if method == "turn/start" {
			s.busy = true
		}
		if err := s.saveLocked(); err != nil {
			delete(s.record.Commands, commandKey)
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
	if (method == "hooks/list" || method == "skills/list") && err == nil && discoveryLookupCwd != "" {
		var response struct {
			Data []map[string]any `json:"data"`
		}
		if json.Unmarshal(result, &response) == nil && len(response.Data) == 1 {
			response.Data[0]["cwd"] = discoveryLookupCwd
			result, err = json.Marshal(response)
		}
	}
	if err == nil && (method == "turn/start" || method == "thread/resume" || method == "thread/settings/update") {
		s.refreshModel(ctx, p)
	}
	if method == "config/read" && err == nil {
		var v map[string]any
		if json.Unmarshal(result, &v) == nil {
			safe := map[string]any{}
			if config, ok := v["config"].(map[string]any); ok {
				for _, key := range []string{"model", "model_provider", "model_reasoning_effort", "model_context_window", "model_auto_compact_token_limit", "approval_policy", "approvals_reviewer", "default_permissions", "sandbox_mode", "web_search", "tui", "features", "service_tier", "personality"} {
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
		c := s.record.Commands[commandKey]
		if err != nil {
			c.State = "unknown"
			c.Error = err.Error()
			if errors.Is(err, nativeclaude.ErrRejected) || strings.Contains(err.Error(), `"code"`) {
				c.State = "failed"
				if method == "turn/start" && s.process == p {
					s.busy = false
				}
			}
		} else {
			c.State = "completed"
			c.Result = result
		}
		s.record.Commands[commandKey] = c
		s.record.UpdatedAt = time.Now()
		_ = s.saveLocked()
		s.mu.Unlock()
	}
	return result, err
}
func (s *Session) Respond(ctx context.Context, role string, id json.RawMessage, result any) error {
	s.mu.Lock()
	if s.record.ExecutionRecord == nil {
		s.mu.Unlock()
		return fmt.Errorf("空间尚未启用共同执行")
	}
	if !s.callerValidLocked(ctx) || !sharing.ExecutionAuthorized(ctx, s.share) || role != s.writer || (role != "owner" && (s.share == nil || !s.share.HasExecutionAccess(role))) {
		s.mu.Unlock()
		return fmt.Errorf("请先取得输入权")
	}
	if s.record.Provider == "claude" {
		s.mu.Unlock()
		return problem.New("native_client_required", "请在 Claude 原生 TUI 中回应审批", "")
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
	approval := s.approvals[string(id)]
	delete(s.approvals, string(id))
	s.activeCalls++
	s.mu.Unlock()
	defer s.finishCall()
	if e := p.Reply(ctx, id, result); e != nil {
		s.mu.Lock()
		s.record.Error = "审批回应结果不明，请检查运行时"
		if s.process == p {
			s.approvals[string(id)] = approval
		}
		s.mu.Unlock()
		return e
	}
	return nil
}
func (s *Session) Context(ctx context.Context, kind, path string, after uint64, cursors ...string) (any, error) {
	s.mu.Lock()
	if !s.callerValidLocked(ctx) {
		s.mu.Unlock()
		return nil, fmt.Errorf("共享已结束")
	}
	p := s.process
	r := s.snapshotLocked()
	executionAccess := sharing.ExecutionAuthorized(ctx, s.share)
	r.Annotations = visibleAnnotations(r.Annotations, executionAccess)
	if !executionAccess {
		r.ExecutionRecord = nil
		p = nil
	}
	if p != nil {
		s.activeCalls++
		defer s.finishCall()
	}
	s.mu.Unlock()
	if kind == "annotations" {
		out := map[string]any{"annotations": r.Annotations, "spaceId": r.ID}
		if r.ExecutionRecord != nil {
			out["sessionId"], out["executionCwd"] = r.SessionID, r.ExecutionCwd
		}
		return out, nil
	}
	if r.ExecutionRecord == nil {
		return nil, fmt.Errorf("只读空间仅提供已发布材料和讨论")
	}
	if r.Provider == "claude" && (kind == "" || kind == "history") {
		return claudeContext(r, s.app.Config.DataDir, cursors)
	}
	switch kind {
	case "events":
		return s.Events(after), nil
	case "annotations":
		return map[string]any{"annotations": r.Annotations, "sessionId": r.SessionID, "executionCwd": r.ExecutionCwd}, nil
	case "file":
		text, e := workspace.ReadFile(r.ExecutionCwd, path)
		hash := sha256.Sum256([]byte(text))
		return map[string]any{"path": filepath.ToSlash(filepath.Clean(path)), "text": text, "contentHash": hex.EncodeToString(hash[:])}, e
	case "changes":
		head, e := workspace.Git(ctx, r.ExecutionCwd, "rev-parse", "HEAD")
		if e != nil {
			return nil, e
		}
		base := strings.TrimSpace(string(head))
		out, e := workspace.Git(ctx, r.ExecutionCwd, "diff", base, "--relative", "--no-color", "--no-ext-diff", "--no-textconv", "--stat", "--", ".")
		if e != nil {
			return nil, e
		}
		diff, e := workspace.Git(ctx, r.ExecutionCwd, "diff", base, "--relative", "--no-color", "--src-prefix=a/", "--dst-prefix=b/", "--no-ext-diff", "--no-textconv", "--", ".")
		truncated := len(diff) > 256<<10
		if len(diff) > 256<<10 {
			diff = diff[:256<<10]
			// Do not expose a partial UTF-8 character or partial line as an anchor.
			if i := strings.LastIndexByte(string(diff), '\n'); i >= 0 {
				diff = diff[:i+1]
			} else {
				diff = nil
			}
		}
		status, _ := workspace.Git(ctx, r.ExecutionCwd, "status", "--short")
		hash := sha256.Sum256(diff)
		return map[string]any{"stat": string(out), "diff": string(diff), "status": string(status), "contentHash": hex.EncodeToString(hash[:]), "baseRevision": base, "truncated": truncated}, e
	default:
		call := s.app.readerCall
		if p != nil {
			call = p.Call
		}
		var read struct {
			Thread map[string]any `json:"thread"`
		}
		if e := call(ctx, "thread/read", map[string]any{"threadId": r.SessionID, "includeTurns": false}, &read); e != nil {
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
		if e := call(ctx, "thread/turns/list", params, &page); e != nil {
			return nil, e
		}
		slices.Reverse(page.Data)
		read.Thread["turns"] = page.Data
		return map[string]any{"thread": read.Thread, "nextCursor": page.NextCursor}, nil
	}
}
func (s *Session) attach(w http.ResponseWriter, r *http.Request, role string) {
	s.mu.Lock()
	if s.record.ExecutionRecord == nil || !sharing.ExecutionAuthorized(r.Context(), s.share) || (role != "owner" && (s.share == nil || !s.share.HasExecutionAccess(role))) {
		s.mu.Unlock()
		http.Error(w, "空间尚未启用共同执行", 403)
		return
	}
	claude := s.record.Provider == "claude"
	s.mu.Unlock()
	if claude {
		s.attachClaude(w, r, role)
		return
	}
	if r.Header.Get("Origin") != "" {
		http.Error(w, "请使用原生客户端连接", 403)
		return
	}
	s.mu.Lock()
	if !s.callerValidLocked(r.Context()) || !sharing.ExecutionAuthorized(r.Context(), s.share) || s.writer != role || !s.online || s.starting || s.record.State != "ready" || (role != "owner" && (s.share == nil || !s.share.HasExecutionAccess(role))) {
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
	if s.share == nil {
		s.releaseWhenIdle = true
	}
	init := s.process.Initialization()
	s.mu.Unlock()
	defer func() {
		d.close()
		s.mu.Lock()
		if s.direct == d {
			s.direct = nil
		}
		s.releaseIfIdleLocked()
		s.mu.Unlock()
	}()
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(32 << 20)
	ctx, cancel := context.WithCancel(context.WithValue(r.Context(), directKey{}, d))
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
		if role == "owner" && localClientRequest(m.Method, params) {
			e = local.call(ctx, m.Method, params, &result)
		} else {
			result, e = s.RPC(ctx, role, m.Method, params, "direct:"+connectionID+":"+string(m.ID))
		}
		if e == nil && (m.Method == "thread/read" || m.Method == "thread/resume") && params["threadId"] == s.record.SessionID {
			s.mu.Lock()
			d.ready = true
			s.mu.Unlock()
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
