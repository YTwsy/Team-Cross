package collab

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/mcp"
	"teamcross/internal/nativecodex"
	"teamcross/internal/readview"
	"teamcross/internal/runtimeconfig"
)

// A receiver reuses the native runtime engine, but is not a shared execution:
// no source fork, Git checkout, space ExecutionRecord or remote native access.
type receiverRecord struct {
	SpaceID       string `json:"spaceId"`
	CreationStage string `json:"creationStage,omitempty"`
	Record        Record `json:"record"`
}

func (s *Session) runtimeDirectory() string {
	group := "collaborations"
	if s.receiverSpace != "" {
		group = "receivers"
	}
	return filepath.Join(s.app.Config.DataDir, group, s.record.ID)
}

func (a *App) newReceiver(r receiverRecord) *Session {
	// Empty command ledgers are omitted on disk; every restored receiver must
	// still be able to record its first write before sending it to the provider.
	if r.Record.ExecutionRecord != nil && r.Record.Commands == nil {
		r.Record.Commands = map[string]Command{}
	}
	return &Session{app: a, record: r.Record, receiverSpace: r.SpaceID, receiverCreation: r.CreationStage, writer: "owner", epoch: 1, presence: map[string]memberPresence{}, approvals: map[string]Approval{}}
}

func (a *App) loadSpaceReceivers() error {
	a.receivers = map[string]*Session{}
	root := filepath.Join(a.Config.DataDir, "receivers")
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		var saved receiverRecord
		if err := readJSON(filepath.Join(root, entry.Name(), "receiver.json"), &saved); err != nil {
			continue
		}
		if saved.SpaceID == "" || saved.Record.ID != entry.Name() || saved.Record.ExecutionRecord == nil {
			continue
		}
		if saved.Record.State == "preparing" {
			saved.Record.State, saved.Record.Error = "error", "原生会话创建结果需核对，保留已有文件，不自动重建"
			if saved.CreationStage == "client" && saved.Record.SessionID == "" {
				saved.Record.Error = "原生客户端启动中断，可以重试创建"
			}
		}
		for id, c := range saved.Record.Commands {
			if c.State == "pending" {
				c.State, c.Error = "unknown", "Core 已重启，不自动重发"
				saved.Record.Commands[id] = c
			}
		}
		a.receivers[saved.Record.ID] = a.newReceiver(saved)
	}
	return nil
}

func (a *App) createSpaceReceiver(ctx context.Context, spaceID, requestID, name string) (any, error) {
	if _, err := uuid.Parse(requestID); err != nil {
		return nil, fmt.Errorf("创建接收会话需要唯一 requestId")
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return nil, fmt.Errorf("请填写 1–80 字的接收会话名称")
	}
	if _, err := a.workbenchCall(ctx, spaceID, "receiver-check", workbenchInput{}); err != nil {
		return nil, err
	}
	id := "receiver-" + requestID
	a.spaceCreateMu.Lock()
	defer a.spaceCreateMu.Unlock()
	a.mu.Lock()
	old, closed := a.receivers[id], a.closed
	a.mu.Unlock()
	if closed {
		return nil, fmt.Errorf("Core 已退出")
	}
	if old != nil {
		old.mu.Lock()
		matches := old.receiverSpace == spaceID && old.record.Title == name
		old.mu.Unlock()
		if !matches {
			return nil, fmt.Errorf("创建标识已用于其他接收会话")
		}
		old.mu.Lock()
		ready, pairingID := old.record.State == "ready", old.record.RequestID
		old.mu.Unlock()
		if ready {
			if _, err := a.registerSpacePairing(ctx, spaceID, pairingID); err != nil {
				return a.receiverView(old), err
			}
		}
		return a.receiverView(old), nil
	}
	// Reject missing clients before allocating another persistent failed card.
	if _, err := a.binary(); err != nil {
		return nil, err
	}
	home, err := a.providerHome("codex")
	if err != nil {
		return nil, err
	}
	cwd := filepath.Join(a.Config.DataDir, "receivers", id, "workspace")
	if err = os.MkdirAll(cwd, 0700); err != nil {
		return nil, err
	}
	now := time.Now()
	r := Record{Schema: 3, ID: id, Title: name, State: "preparing", CreatedAt: now, UpdatedAt: now, Annotations: []Annotation{}, ExecutionRecord: &ExecutionRecord{RequestID: requestID, RuntimeMode: runtimeconfig.Restricted, Provider: "codex", ProviderHome: home, ExecutionCwd: cwd, WorkspaceRoot: cwd, WorkspaceOwned: true, Commands: map[string]Command{}}}
	s := a.newReceiver(receiverRecord{SpaceID: spaceID, CreationStage: "client", Record: r})
	a.mu.Lock()
	a.receivers[id] = s
	a.mu.Unlock()
	return a.initializeSpaceReceiver(ctx, s)
}

func (a *App) initializeSpaceReceiver(ctx context.Context, s *Session) (any, error) {
	s.mu.Lock()
	r, spaceID := s.snapshotLocked(), s.receiverSpace
	s.mu.Unlock()
	fail := func(err error) (any, error) {
		s.mu.Lock()
		s.record.State, s.record.Error = "error", err.Error()
		_ = s.saveLocked()
		s.mu.Unlock()
		return a.receiverView(s), err
	}
	s.mu.Lock()
	err := s.saveLocked()
	s.mu.Unlock()
	if err != nil {
		return fail(err)
	}
	if err = s.start(ctx, false); err != nil {
		return fail(err)
	}
	s.mu.Lock()
	process := s.process
	// Persist the uncertain-outcome boundary before sending thread/start. Only
	// failures before this point can be retried without duplicating a session.
	s.receiverCreation = "thread"
	err = s.saveLocked()
	s.mu.Unlock()
	if err != nil {
		return fail(err)
	}
	params := nativecodex.SessionOverrides("", r.ExecutionCwd, r.RuntimeMode)
	delete(params, "threadId")
	params["persistExtendedHistory"] = true
	var started struct {
		Thread          Source  `json:"thread"`
		Model           string  `json:"model"`
		ModelProvider   string  `json:"modelProvider"`
		ReasoningEffort *string `json:"reasoningEffort"`
	}
	if err = process.Call(ctx, "thread/start", params, &started); err != nil {
		return fail(err)
	}
	if _, err = uuid.Parse(started.Thread.ID); err != nil {
		return fail(fmt.Errorf("原生运行时未确认接收会话身份"))
	}
	_ = process.Call(ctx, "thread/name/set", map[string]any{"threadId": started.Thread.ID, "name": r.Title}, nil)
	s.mu.Lock()
	s.record.SessionID, s.record.Model, s.record.ModelProvider, s.record.ReasoningEffort = started.Thread.ID, started.Model, started.ModelProvider, started.ReasoningEffort
	s.record.State, s.record.Error = "ready", ""
	err = s.saveLocked()
	s.mu.Unlock()
	if err != nil {
		return fail(err)
	}
	var checked struct {
		Thread Source `json:"thread"`
	}
	if err = process.Call(ctx, "thread/read", map[string]any{"threadId": started.Thread.ID, "includeTurns": false}, &checked); err != nil || checked.Thread.ID != started.Thread.ID {
		return fail(fmt.Errorf("无法核对新建原生会话，请查看原生运行时"))
	}
	// The exact session is created and verified here, so no copy/paste challenge
	// is needed. Read/finish of the bootstrap still proves model-level receipt.
	p := AgentPairing{ID: r.RequestID, Name: r.Title, Provider: "codex", SessionID: started.Thread.ID, SpaceID: spaceID, RuntimeID: r.ID, Transport: "native", State: "paired", CreatedAt: time.Now(), Links: map[string]string{}}
	a.agentMu.Lock()
	next := libraryClone(a.agents)
	if _, exists := next.Pairings[p.ID]; exists || len(next.Pairings) >= 64 {
		a.agentMu.Unlock()
		return fail(fmt.Errorf("配对记录已存在或达到上限"))
	}
	next.Pairings[p.ID] = p
	err = a.saveAgentsLocked(next)
	a.agentMu.Unlock()
	if err != nil {
		return fail(err)
	}
	if _, err = a.registerSpacePairing(ctx, spaceID, p.ID); err != nil {
		return a.receiverView(s), err
	}
	return a.receiverView(s), nil
}

func receiverPathFailure(message string) bool {
	switch message {
	case "Codex 路径不可用", "Codex CLI 文件不存在", "Codex CLI 文件不可执行", "未找到可用的 Codex CLI":
		return true
	}
	return false
}

func (s *Session) canRetryReceiverLocked() bool {
	return s.record.State == "error" && s.record.SessionID == "" &&
		(s.receiverCreation == "client" || (s.receiverCreation == "" && s.record.Error == "Codex 路径不可用"))
}

func (a *App) retrySpaceReceiver(ctx context.Context, s *Session) (any, error) {
	if _, err := a.workbenchCall(ctx, s.receiverSpace, "receiver-check", workbenchInput{}); err != nil {
		return nil, err
	}
	a.spaceCreateMu.Lock()
	defer a.spaceCreateMu.Unlock()
	if _, err := a.binary(); err != nil {
		return a.receiverView(s), err
	}
	s.mu.Lock()
	if !s.canRetryReceiverLocked() {
		s.mu.Unlock()
		return a.receiverView(s), fmt.Errorf("仅能重试尚未发送原生会话创建请求的失败记录")
	}
	s.record.State, s.record.Error, s.receiverCreation = "preparing", "", "client"
	s.mu.Unlock()
	return a.initializeSpaceReceiver(ctx, s)
}

func (a *App) receiverView(s *Session) map[string]any {
	s.mu.Lock()
	r := s.snapshotLocked()
	out := map[string]any{"id": r.ID, "spaceId": s.receiverSpace, "name": r.Title, "state": r.State, "error": r.Error, "sessionId": r.SessionID, "online": s.online, "busy": s.busy, "approvals": len(s.approvals), "model": r.Model, "modelProvider": r.ModelProvider, "reasoningEffort": r.ReasoningEffort, "pairingId": r.RequestID}
	process := s.process
	canRetry := s.canRetryReceiverLocked()
	s.mu.Unlock()
	// Settings, creation, resumption and this view share the current resolver.
	// A saved creation error must never masquerade as current CLI discovery.
	binary, err := a.binary()
	out["binary"], out["clientError"], out["canRetryCreation"] = binary, "", canRetry
	if err != nil {
		out["clientError"] = err.Error()
	}
	if canRetry && receiverPathFailure(r.Error) {
		out["error"] = ""
		out["clientRecovered"] = err == nil
	}
	if p, ok := process.(interface{ Endpoint() string }); ok {
		if err == nil {
			out["command"] = nativecodex.Command(binary, r.ProviderHome, r.SessionID, p.Endpoint())
		}
	}
	return out
}

func (a *App) receiversHTTP(w http.ResponseWriter, r *http.Request, path string) bool {
	if path == "space-receivers" && r.Method == "GET" {
		a.mu.Lock()
		sessions := []*Session{}
		for _, s := range a.receivers {
			if s.receiverSpace == r.URL.Query().Get("spaceId") {
				sessions = append(sessions, s)
			}
		}
		a.mu.Unlock()
		out := []map[string]any{}
		for _, s := range sessions {
			out = append(out, a.receiverView(s))
		}
		respond(w, out, nil)
		return true
	}
	parts := strings.Split(path, "/")
	if len(parts) != 3 || parts[0] != "space-receivers" {
		return false
	}
	a.mu.Lock()
	s := a.receivers[parts[1]]
	a.mu.Unlock()
	if s == nil {
		http.NotFound(w, r)
		return true
	}
	if parts[2] == "events" && r.Method == "GET" {
		respond(w, s.Events(0), nil)
		return true
	}
	if r.Method != "POST" {
		http.NotFound(w, r)
		return true
	}
	if parts[2] == "respond" {
		var in ReplyInput
		if decode(w, r, &in) {
			err := s.Respond(r.Context(), "owner", in.ID, in.Result)
			respond(w, map[string]bool{"ok": err == nil}, err)
		}
		return true
	}
	var in struct {
		Action string `json:"action"`
	}
	if !decode(w, r, &in) {
		return true
	}
	if parts[2] != "action" || (in.Action != "start" && in.Action != "retry") {
		http.NotFound(w, r)
		return true
	}
	if in.Action == "retry" {
		out, err := a.retrySpaceReceiver(r.Context(), s)
		respond(w, out, err)
		return true
	}
	if _, err := a.workbenchCall(r.Context(), s.receiverSpace, "receiver-check", workbenchInput{}); err != nil {
		respond(w, nil, err)
		return true
	}
	err := s.start(r.Context(), true)
	if err == nil {
		a.wakeWorkbench()
	}
	respond(w, a.receiverView(s), err)
	return true
}

func (a *App) receiverToolsHTTP(w http.ResponseWriter, r *http.Request, s *Session) {
	s.mu.Lock()
	token, id, sessionID, spaceID := s.record.AnnotationToken, s.record.ID, s.record.SessionID, s.receiverSpace
	valid := !s.closed && s.online && token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) == 1
	s.mu.Unlock()
	if !valid || r.Method != "POST" || r.Header.Get("Origin") != "" {
		http.Error(w, "接收会话工具不可用", 403)
		return
	}
	var input struct {
		Name      string           `json:"name"`
		Arguments map[string]any   `json:"arguments"`
		Caller    mcp.CallerSource `json:"caller"`
	}
	if !decode(w, r, &input) {
		return
	}
	if err := mcp.ValidateRuntimeCall(input.Name, input.Arguments); err != nil {
		respond(w, nil, err)
		return
	}
	// Every scoped tool carries the native caller identity, not just delivery
	// receipts. A copied token from another conversation is insufficient.
	if input.Caller.Provider != "codex" || input.Caller.SourceID != sessionID || input.Caller.TurnID == "" {
		respond(w, nil, fmt.Errorf("工具调用不属于当前接收会话"))
		return
	}
	if _, err := a.workbenchCall(r.Context(), spaceID, "view", workbenchInput{}); err != nil {
		respond(w, nil, err)
		return
	}
	if input.Name == "pair_current_session" || input.Name == "confirm_pairing" {
		respond(w, nil, fmt.Errorf("此接收会话已由 Team Cross 核对配对"))
		return
	}
	if mcp.ValidateAgentCall(input.Name, input.Arguments) == nil || strings.Contains(input.Name, "_space_") {
		out, err := a.invokeAgentTool(r, input.Name, input.Arguments, agentCaller{Caller: input.Caller}, id)
		respond(w, out, err)
		return
	}
	var out any
	var err error
	switch input.Name {
	case "list_materials":
		out, err = a.target(r.Context(), spaceID, "GET", "materials", nil)
	case "read_material":
		readview.Defaults(input.Arguments)
		in := workbenchDecode[MaterialRead](input.Arguments)
		out, err = a.target(r.Context(), spaceID, "POST", "read-material", in)
	case "read_annotations":
		in := workbenchDecode[HistoryRead](input.Arguments)
		out, err = a.workbenchCall(r.Context(), spaceID, "annotations", workbenchInput{Read: in})
	case "reply_to_annotation":
		in := workbenchDecode[AnnotationReplyInput](input.Arguments)
		out, err = a.workbenchCall(r.Context(), spaceID, "reply", workbenchInput{Reply: in, Actor: SpaceActor{Kind: "session", Provider: "codex", Session: contentHash([]string{"codex", sessionID})[:24]}})
	default:
		err = fmt.Errorf("接收会话工具不受支持")
	}
	respond(w, out, err)
}
