package collab

import (
	"context"
	"crypto/sha256"
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
	"teamcross/internal/nativecodex"
	"teamcross/internal/sharing"
	"teamcross/internal/workspace"
)

func DefaultDataDir() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "Library", "Application Support", "Team Cross Next")
}
func Open(cfg Config) (*App, error) {
	if cfg.DataDir == "" {
		cfg.DataDir = DefaultDataDir()
	}
	if cfg.Repo == "" {
		cfg.Repo = "."
	}
	var err error
	cfg.DataDir, err = filepath.Abs(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	cfg.Repo, err = filepath.Abs(cfg.Repo)
	if err != nil {
		return nil, err
	}
	for _, d := range []string{cfg.DataDir, filepath.Join(cfg.DataDir, "collaborations"), filepath.Join(cfg.DataDir, "clients")} {
		if err = os.MkdirAll(d, 0700); err != nil {
			return nil, err
		}
	}
	host, _ := os.Hostname()
	a := &App{Config: cfg, Host: host, Token: uuid.NewString(), sessions: map[string]*Session{}, joined: map[string]*Joined{}}
	_ = readJSON(filepath.Join(cfg.DataDir, "settings.json"), &a.settings)
	if cfg.Binary != "" {
		a.settings.Binary = cfg.Binary
	}
	if cfg.DesktopApp != "" {
		a.settings.DesktopApp = cfg.DesktopApp
	}
	entries, _ := os.ReadDir(filepath.Join(cfg.DataDir, "collaborations"))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		var r Record
		if readJSON(filepath.Join(cfg.DataDir, "collaborations", entry.Name(), "collaboration.json"), &r) == nil && r.ID == entry.Name() {
			if r.State == "preparing" {
				r.State = "error"
				r.Error = "上次创建被中断。已保留会话和目录，请先核实创建结果。"
			}
			if r.Commands == nil {
				r.Commands = map[string]Command{}
			}
			for id, c := range r.Commands {
				if c.State == "pending" {
					c.State = "unknown"
					c.Error = "服务曾重启，请先查看会话结果"
					r.Commands[id] = c
				}
			}
			s := a.newSession(r)
			a.sessions[r.ID] = s
		}
	}
	var guests []joinedRecord
	if readJSON(filepath.Join(cfg.DataDir, "joined.json"), &guests) == nil {
		for _, g := range guests {
			a.joined[g.ID] = &Joined{app: a, ID: g.ID, Invitation: g.Invitation, URL: g.URL, Client: sharing.Client(g.Invitation), Last: g.Last, ended: g.Ended, done: make(chan struct{})}
		}
	}
	return a, nil
}
func readJSON(path string, out any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, out)
}
func writeJSONFile(path string, value any) error {
	b, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		return e
	}
	tmp := path + ".tmp"
	if e = os.WriteFile(tmp, b, 0600); e != nil {
		return e
	}
	return os.Rename(tmp, path)
}
func (a *App) newSession(r Record) *Session {
	return &Session{app: a, record: r, writer: "owner", epoch: 1, approvals: map[string]Approval{}}
}
func (s *Session) saveLocked() error {
	return writeJSONFile(filepath.Join(s.app.Config.DataDir, "collaborations", s.record.ID, "collaboration.json"), s.record)
}
func (a *App) binary() (string, error) {
	a.mu.Lock()
	v := a.settings.Binary
	a.mu.Unlock()
	if v != "" {
		if stat, e := os.Stat(v); e != nil || !stat.Mode().IsRegular() {
			return "", fmt.Errorf("Codex 路径不可用: %s", v)
		}
		return v, nil
	}
	return nativecodex.Binary()
}
func (a *App) startProcess(ctx context.Context, home, cwd, log string) (Runtime, error) {
	binary, e := a.binary()
	if e != nil {
		return nil, e
	}
	if a.Config.StartProcess != nil {
		return a.Config.StartProcess(binary, home, cwd, log)
	}
	return nativecodex.Start(ctx, binary, home, cwd, log)
}
func (a *App) readerCall(ctx context.Context, method string, params, out any) error {
	a.readerMu.Lock()
	defer a.readerMu.Unlock()
	if a.reader == nil || !a.reader.Alive() {
		if a.reader != nil {
			a.reader.Close()
		}
		home, e := nativecodex.Home()
		if e != nil {
			return e
		}
		p, e := a.startProcess(ctx, home, a.Config.Repo, filepath.Join(a.Config.DataDir, "reader.log"))
		if e != nil {
			return e
		}
		a.reader = p
	}
	return a.reader.Call(ctx, method, params, out)
}
func (a *App) Sources(ctx context.Context, search, cursor string) (map[string]any, error) {
	params := map[string]any{"limit": 40, "sortKey": "updated_at"}
	if cursor != "" {
		params["cursor"] = cursor
	}
	if search != "" {
		params["searchTerm"] = search
	}
	var result map[string]any
	e := a.readerCall(ctx, "thread/list", params, &result)
	return result, e
}
func (a *App) Preview(ctx context.Context, in CreateInput) (Preview, error) {
	var p Preview
	if _, e := uuid.Parse(in.SourceID); e != nil {
		return p, fmt.Errorf("请选择有效的 Codex 来源会话")
	}
	var read struct {
		Thread Source `json:"thread"`
	}
	if e := a.readerCall(ctx, "thread/read", map[string]any{"threadId": in.SourceID, "includeTurns": false}, &read); e != nil {
		return p, e
	}
	p.Source = read.Thread
	if p.Source.ID != in.SourceID || p.Source.Cwd == "" {
		return p, fmt.Errorf("无法确认来源会话和目录")
	}
	var turns struct {
		Data []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if e := a.readerCall(ctx, "thread/turns/list", map[string]any{"threadId": in.SourceID, "limit": 1, "sortDirection": "desc", "itemsView": "summary"}, &turns); e != nil {
		return p, e
	}
	if len(turns.Data) == 0 || turns.Data[0].ID == "" || turns.Data[0].Status == "inProgress" {
		return p, fmt.Errorf("来源还没有可分支的已完成对话，请等待当前轮完成")
	}
	p.SourceTurnID = turns.Data[0].ID
	w, e := workspace.Inspect(ctx, p.Source.Cwd, in.WorkspaceMode)
	if e != nil {
		return p, e
	}
	p.Workspace = w
	p.TargetDirectory = w.SourceCwd
	if in.WorkspaceMode == "worktree" {
		if _, e := uuid.Parse(in.RequestID); e == nil {
			p.TargetDirectory = filepath.Join(a.Config.DataDir, "collaborations", in.RequestID, "worktree", w.RelativeCwd)
		}
	}
	// Dirty contents are informational and never captured or copied.
	b, _ := json.Marshal([]any{in.SourceID, p.SourceTurnID, w.Mode, w.Repo, w.SourceCwd, w.Head, w.Branch})
	sum := sha256.Sum256(b)
	p.Hash = hex.EncodeToString(sum[:])
	return p, nil
}
func (a *App) Create(ctx context.Context, in CreateInput) (*Session, error) {
	if _, e := uuid.Parse(in.RequestID); e != nil {
		return nil, fmt.Errorf("创建请求缺少唯一标识")
	}
	a.mu.Lock()
	old := a.sessions[in.RequestID]
	a.mu.Unlock()
	if old != nil {
		old.mu.Lock()
		matches := old.record.SourceID == in.SourceID && old.record.PreviewHash == in.PreviewHash
		old.mu.Unlock()
		if !matches {
			return nil, fmt.Errorf("该创建请求已用于不同起点")
		}
		return old, nil
	}
	p, e := a.Preview(ctx, in)
	if e != nil {
		return nil, e
	}
	if p.Hash != in.PreviewHash {
		return nil, fmt.Errorf("会话或 Git 起点已变化，请重新查看起点")
	}
	home, e := nativecodex.Home()
	if e != nil {
		return nil, e
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = p.Source.Name
	}
	if title == "" {
		title = shortText(p.Source.Preview, 42)
	}
	if title == "" {
		title = "新的协作"
	}
	now := time.Now()
	r := Record{ID: in.RequestID, Title: title, SourceID: in.SourceID, SourceTurnID: p.SourceTurnID, WorkspaceMode: in.WorkspaceMode, Repo: p.Workspace.Repo, ExecutionCwd: p.Workspace.SourceCwd, WorkspaceRoot: p.Workspace.Repo, WorkspaceOwned: in.WorkspaceMode == "worktree", Head: p.Workspace.Head, Branch: p.Workspace.Branch, ProviderHome: home, State: "preparing", CreatedAt: now, UpdatedAt: now, PreviewHash: p.Hash, Annotations: []Annotation{}, Commands: map[string]Command{}}
	dir := filepath.Join(a.Config.DataDir, "collaborations", r.ID)
	if e = os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	s := a.newSession(r)
	a.mu.Lock()
	if old = a.sessions[r.ID]; old != nil {
		a.mu.Unlock()
		return a.Create(ctx, in)
	}
	a.sessions[r.ID] = s
	a.mu.Unlock()
	s.mu.Lock()
	e = s.saveLocked()
	s.mu.Unlock()
	if e != nil {
		return nil, e
	}
	fail := func(err error) (*Session, error) {
		s.mu.Lock()
		s.record.State = "error"
		s.record.Error = err.Error()
		_ = s.saveLocked()
		s.mu.Unlock()
		return s, err
	}
	branch := r.Branch
	destination := filepath.Join(dir, "worktree")
	if r.WorkspaceOwned {
		branch = "codex/collab-" + r.ID[:8]
	}
	cwd, e := workspace.Prepare(ctx, p.Workspace, destination, branch)
	if e != nil {
		return fail(e)
	}
	s.mu.Lock()
	s.record.ExecutionCwd = cwd
	s.record.Branch = branch
	if r.WorkspaceOwned {
		s.record.WorkspaceRoot = destination
	}
	e = s.saveLocked()
	s.mu.Unlock()
	if e != nil {
		return fail(e)
	}
	if e = s.start(ctx, false); e != nil {
		return fail(e)
	}
	params := nativecodex.Overrides(r.SourceID, cwd)
	inheritModel(params, p.Source)
	params["lastTurnId"] = r.SourceTurnID
	params["excludeTurns"] = true
	var fork struct {
		Model           string  `json:"model"`
		ModelProvider   string  `json:"modelProvider"`
		ReasoningEffort *string `json:"reasoningEffort"`
		Thread          struct {
			ID           string `json:"id"`
			ForkedFromID string `json:"forkedFromId"`
			Cwd          string `json:"cwd"`
		} `json:"thread"`
	}
	if e = s.process.Call(ctx, "thread/fork", params, &fork); e != nil {
		return fail(e)
	}
	if fork.Thread.ID == "" || fork.Thread.ID == r.SourceID || fork.Thread.ForkedFromID != r.SourceID || fork.Thread.Cwd != cwd {
		return fail(fmt.Errorf("Codex 未确认新的会话身份或工作目录"))
	}
	s.mu.Lock()
	s.record.SessionID = fork.Thread.ID
	s.modelLocked(fork.Model, fork.ModelProvider, fork.ReasoningEffort)
	s.record.State = "ready"
	s.record.UpdatedAt = time.Now()
	e = s.saveLocked()
	s.mu.Unlock()
	if e != nil {
		return fail(e)
	}
	_ = s.process.Call(ctx, "thread/name/set", map[string]any{"threadId": fork.Thread.ID, "name": title}, nil)
	return s, nil
}
func (s *Session) start(ctx context.Context, resume bool) error {
	s.mu.Lock()
	if s.process != nil && s.process.Alive() {
		s.mu.Unlock()
		return nil
	}
	if s.starting {
		s.mu.Unlock()
		return fmt.Errorf("正在启动，请稍后刷新")
	}
	s.starting = true
	r := s.record
	if resume && (r.State != "ready" || r.SessionID == "") {
		s.starting = false
		s.mu.Unlock()
		return fmt.Errorf("该协作尚未成功创建，请检查创建失败原因")
	}
	old := s.process
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.starting = false; s.mu.Unlock() }()
	if old != nil {
		old.Close()
	}
	p, e := s.app.startProcess(ctx, r.ProviderHome, r.ExecutionCwd, filepath.Join(s.app.Config.DataDir, "collaborations", r.ID, "runtime.log"))
	if e != nil {
		return e
	}
	p.SetHandler(s.onMessage)
	s.mu.Lock()
	s.process = p
	s.online = true
	s.busy = false
	s.approvals = map[string]Approval{}
	s.mu.Unlock()
	if resume && r.SessionID != "" {
		var read struct {
			Thread Source `json:"thread"`
		}
		if e = p.Call(ctx, "thread/read", map[string]any{"threadId": r.SessionID, "includeTurns": false}, &read); e != nil {
			p.Close()
			return e
		}
		params := nativecodex.Overrides(r.SessionID, r.ExecutionCwd)
		inheritModel(params, read.Thread)
		params["excludeTurns"] = true
		if e = p.Call(ctx, "thread/resume", params, nil); e != nil {
			p.Close()
			return e
		}
		s.refreshModel(ctx, p)
	}
	return nil
}
func (a *App) Close() {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	a.mu.Unlock()
	_ = a.saveJoined()
	a.mu.Lock()
	sessions := make([]*Session, 0, len(a.sessions))
	for _, s := range a.sessions {
		sessions = append(sessions, s)
	}
	joined := make([]*Joined, 0, len(a.joined))
	for _, j := range a.joined {
		joined = append(joined, j)
	}
	a.mu.Unlock()
	for _, j := range joined {
		j.close()
	}
	for _, s := range sessions {
		s.mu.Lock()
		s.endShareLocked()
		if s.direct != nil {
			s.direct.close()
		}
		p := s.process
		server := s.server
		s.mu.Unlock()
		if server != nil {
			_ = server.Close()
		}
		if p != nil {
			p.Close()
		}
	}
	a.mu.Lock()
	retired := append([]*sharing.Runtime(nil), a.retiredShares...)
	a.mu.Unlock()
	for _, rt := range retired {
		rt.Close()
	}
	a.readerMu.Lock()
	if a.reader != nil {
		a.reader.Close()
	}
	a.readerMu.Unlock()
}
func shortText(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return string(r)
}
func (a *App) owned(id string) (*Session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[id]
	if s == nil {
		return nil, errors.New("没有找到这次协作")
	}
	return s, nil
}
func (s *Session) view() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.record
	out := map[string]any{"id": r.ID, "title": r.Title, "sourceId": r.SourceID, "sourceTurnId": r.SourceTurnID, "sessionId": r.SessionID, "workspaceMode": r.WorkspaceMode, "executionCwd": r.ExecutionCwd, "repo": r.Repo, "head": r.Head, "branch": r.Branch, "workspaceOwned": r.WorkspaceOwned, "state": r.State, "error": r.Error, "createdAt": r.CreatedAt, "updatedAt": r.UpdatedAt, "host": s.app.Host, "role": "owner", "writer": s.writer, "busy": s.busy, "online": s.online, "epoch": s.epoch, "sharing": s.share != nil, "connected": s.direct != nil, "sequence": s.sequence, "approvals": len(s.approvals), "annotations": r.Annotations, "model": r.Model, "modelProvider": r.ModelProvider, "reasoningEffort": r.ReasoningEffort}
	if s.direct != nil {
		out["client"] = s.direct.kind
	}
	if s.share != nil {
		out["invitation"] = s.share.Token()
		out["expiresAt"] = s.share.Invitation.ExpiresAt
	}
	return out
}
func (a *App) List(ctx context.Context) []map[string]any {
	a.mu.Lock()
	ss := []*Session{}
	js := []*Joined{}
	for _, s := range a.sessions {
		ss = append(ss, s)
	}
	for _, j := range a.joined {
		js = append(js, j)
	}
	a.mu.Unlock()
	list := []map[string]any{}
	for _, s := range ss {
		list = append(list, s.view())
	}
	for _, j := range js {
		list = append(list, j.view(ctx))
	}
	sort.Slice(list, func(i, j int) bool { return fmt.Sprint(list[i]["createdAt"]) > fmt.Sprint(list[j]["createdAt"]) })
	return list
}
