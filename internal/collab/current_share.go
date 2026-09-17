package collab

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/mcp"
	"teamcross/internal/nativecodex"
	"teamcross/internal/problem"
	"teamcross/internal/runtimeconfig"
	"teamcross/internal/sharing"
)

type currentShareInput struct {
	CreateInput
	Caller    mcp.CallerSource `json:"caller"`
	Transport string           `json:"transport"`
}

type shareRequestRecord struct {
	ID              string            `json:"id"`
	State           string            `json:"state"`
	Input           currentShareInput `json:"input"`
	SourceTurnID    string            `json:"sourceTurnId"`
	CollaborationID string            `json:"collaborationId,omitempty"`
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
	Error           *problem.Error    `json:"error,omitempty"`
}

type shareRequest struct {
	mu     sync.Mutex
	app    *App
	record shareRequestRecord
	cancel context.CancelFunc
}

func shareRequestActive(state string) bool {
	return state == "waiting" || state == "creating" || state == "inviting"
}
func (j *shareRequest) snapshot() shareRequestRecord {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.record
}
func (j *shareRequest) saveLocked() error {
	return writeJSONFile(filepath.Join(j.app.Config.DataDir, "share-requests", j.record.ID+".json"), j.record)
}
func (j *shareRequest) transition(state, collaborationID string, err error) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	// A successful cancellation of waiting wins over a late reader response.
	if j.record.State == "cancelled" {
		return context.Canceled
	}
	j.record.State, j.record.UpdatedAt = state, time.Now()
	if collaborationID != "" {
		j.record.CollaborationID = collaborationID
	}
	if err != nil {
		j.record.Error = problem.Describe(err)
	}
	return j.saveLocked()
}

func (a *App) loadShareRequests() error {
	dir := filepath.Join(a.Config.DataDir, "share-requests")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	a.shareRequests = map[string]*shareRequest{}
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		var r shareRequestRecord
		if err := readJSON(filepath.Join(dir, f.Name()), &r); err != nil {
			return err
		}
		if _, err := uuid.Parse(r.ID); err != nil || f.Name() != r.ID+".json" {
			return fmt.Errorf("分享请求记录身份无效")
		}
		j := &shareRequest{app: a, record: r}
		if shareRequestActive(r.State) {
			j.record.State = "interrupted"
			j.record.UpdatedAt = time.Now()
			j.record.Error = &problem.Error{Code: "share_interrupted", Message: "Core 曾停止，分享请求没有自动重放", Recovery: "先以请求 ID 查询已有协作；若已创建，只恢复原协作并重试邀请，否则重新预览来源"}
			if err := j.saveLocked(); err != nil {
				return err
			}
		}
		a.shareRequests[r.ID] = j
	}
	return nil
}

func (a *App) currentSource(ctx context.Context, c mcp.CallerSource) (map[string]any, error) {
	if c.Provider != "codex" && c.Provider != "claude" {
		return nil, fmt.Errorf("当前来源 Provider 无效")
	}
	if _, err := uuid.Parse(c.SourceID); err != nil {
		return nil, fmt.Errorf("当前来源 Session ID 无效")
	}
	if c.Provider == "claude" {
		return a.currentClaudeSource(ctx, c)
	}
	var read struct {
		Thread Source `json:"thread"`
	}
	if err := a.sourceCall(ctx, c.Provider, "thread/read", map[string]any{"threadId": c.SourceID, "includeTurns": false}, &read); err != nil {
		return nil, err
	}
	if read.Thread.ID != c.SourceID || read.Thread.Cwd == "" {
		return nil, fmt.Errorf("原生来源身份或目录不匹配")
	}
	turnID := c.TurnID
	if turnID == "" {
		return nil, fmt.Errorf("原生客户端未提供当前轮次")
	}
	var turns struct {
		Data []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := a.sourceCall(ctx, c.Provider, "thread/turns/list", map[string]any{"threadId": c.SourceID, "limit": 1, "sortDirection": "desc", "itemsView": "summary"}, &turns); err != nil {
		return nil, err
	}
	if len(turns.Data) != 1 || turns.Data[0].ID != turnID {
		return nil, problem.New("source_context_changed", "当前 Session 的轮次身份已变化", "请重新取得当前会话上下文或明确选择来源")
	}
	if read.Thread.Path != "" {
		id, status, err := nativecodex.SourceTurn(read.Thread.Path, c.SourceID)
		if err != nil {
			return nil, err
		}
		if id != turnID {
			return nil, problem.New("source_context_changed", "来源轮次已变化", "请重新取得当前会话上下文")
		}
		turns.Data[0].Status = status
	}
	read.Thread.Provider = c.Provider
	return map[string]any{"provider": c.Provider, "sourceId": c.SourceID, "turnId": turnID, "turnStatus": turns.Data[0].Status, "source": read.Thread}, nil
}

func (a *App) currentClaudeSource(ctx context.Context, c mcp.CallerSource) (map[string]any, error) {
	// Claude can dispatch MCP just before its asynchronous transcript append.
	// Wait briefly for evidence, never substitute another session or tool call.
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		h, err := a.claudeSource(c.SourceID)
		if err == nil && h.ID == c.SourceID && h.Cwd != "" {
			for i, turn := range h.Turns {
				for _, item := range turn.Items {
					if item.Type != "toolCall" || item.ID != c.ToolUseID || c.ToolUseID == "" {
						continue
					}
					if i != len(h.Turns)-1 {
						return nil, problem.New("source_context_changed", "Claude 的轮次身份已变化", "请重新取得当前会话上下文")
					}
					var source Source
					if err := marshalInto(h, &source); err != nil {
						return nil, err
					}
					source.Provider = "claude"
					return map[string]any{"provider": "claude", "sourceId": h.ID, "turnId": turn.ID, "turnStatus": turn.Status, "source": source}, nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, problem.New("source_context_unavailable", "无法在来源历史中核对本次 Claude 工具调用", "请重新连接 MCP 或明确选择来源；不使用可能过时的进程环境自动分享")
		case <-tick.C:
		}
	}
}

// Bind the chosen turn and Git/mode/transport, but not a running Claude JSONL
// fingerprint: the tool's own results necessarily append to that transcript.
// Once the turn completes, ordinary Preview/Create revalidate the full snapshot.
func currentShareHash(p Preview, in currentShareInput) string {
	b, _ := json.Marshal([]any{"after-current-v1", in.Provider, p.RuntimeMode, in.SourceID, p.SourceTurnID, p.Workspace.Mode, p.Workspace.Repo, p.Workspace.SourceCwd, p.Workspace.Head, p.Workspace.Branch, in.Transport})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (a *App) previewCurrentShare(ctx context.Context, in currentShareInput) (Preview, error) {
	var empty Preview
	if _, err := uuid.Parse(in.RequestID); err != nil {
		return empty, fmt.Errorf("需要分享请求 UUID")
	}
	if in.Provider != in.Caller.Provider || in.SourceID != in.Caller.SourceID {
		return empty, fmt.Errorf("当前来源不能由工具参数替换")
	}
	if _, err := sharing.ParseTransport(in.Transport); err != nil {
		return empty, err
	}
	current, err := a.currentSource(ctx, in.Caller)
	if err != nil {
		return empty, err
	}
	p, err := a.preview(ctx, in.CreateInput, true)
	if err != nil {
		return p, err
	}
	if p.SourceTurnID != current["turnId"] {
		return empty, problem.New("source_context_changed", "来源起点已经变化", "请重新预览")
	}
	p.Hash = currentShareHash(p, in)
	return p, nil
}

func (a *App) submitCurrentShare(ctx context.Context, in currentShareInput) (*shareRequest, error) {
	mode, err := runtimeconfig.Parse(in.RuntimeMode)
	if err != nil {
		return nil, err
	}
	in.RuntimeMode = mode
	if _, err := uuid.Parse(in.RequestID); err != nil {
		return nil, fmt.Errorf("需要分享请求 UUID")
	}
	if in.Provider != in.Caller.Provider || in.SourceID != in.Caller.SourceID {
		return nil, fmt.Errorf("当前来源不能由工具参数替换")
	}
	a.mu.Lock()
	old := a.shareRequests[in.RequestID]
	a.mu.Unlock()
	if old != nil {
		r := old.snapshot()
		if r.Input.Provider != in.Provider || r.Input.SourceID != in.SourceID || r.Input.RuntimeMode != mode || r.Input.WorkspaceMode != in.WorkspaceMode || r.Input.PreviewHash != in.PreviewHash || r.Input.Transport != in.Transport {
			return nil, fmt.Errorf("该分享请求已用于不同起点")
		}
		return old, nil
	}
	p, err := a.previewCurrentShare(ctx, in)
	if err != nil {
		return nil, err
	}
	if p.Hash != in.PreviewHash {
		return nil, problem.New("preview_changed", "会话或 Git 起点已变化", "请重新预览当前 Session")
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil, fmt.Errorf("Core 已退出")
	}
	if a.shareRequests[in.RequestID] != nil {
		a.mu.Unlock()
		return a.submitCurrentShare(ctx, in)
	}
	active := 0
	for _, other := range a.shareRequests {
		if shareRequestActive(other.snapshot().State) {
			active++
		}
	}
	if active >= 32 {
		a.mu.Unlock()
		return nil, fmt.Errorf("待完成分享过多，请先查询或取消已有请求")
	}
	jobCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	now := time.Now()
	j := &shareRequest{app: a, cancel: cancel, record: shareRequestRecord{ID: in.RequestID, State: "waiting", Input: in, SourceTurnID: p.SourceTurnID, CreatedAt: now, UpdatedAt: now}}
	if err := j.saveLocked(); err != nil {
		a.mu.Unlock()
		cancel()
		return nil, err
	}
	a.shareRequests[in.RequestID] = j
	a.shareWorkers.Add(1)
	a.mu.Unlock()
	go func() { defer a.shareWorkers.Done(); defer cancel(); j.run(jobCtx) }()
	return j, nil
}

func (j *shareRequest) run(ctx context.Context) {
	r := j.snapshot()
	fail := func(err error) {
		state := "failed"
		if ctx.Err() == context.DeadlineExceeded {
			err = problem.New("share_wait_expired", "分享请求超时", "先查询同 ID 协作的实际结果，再决定恢复或重新预览")
		} else if ctx.Err() != nil {
			state = "interrupted"
			err = problem.New("share_interrupted", "分享请求被停止，未自动重放", "先查询同 ID 协作的实际结果，再决定恢复或重新预览")
		}
		_ = j.transition(state, "", err)
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			fail(err)
			return
		}
		if time.Since(r.CreatedAt) > 15*time.Minute {
			fail(problem.New("share_wait_expired", "等待本轮完成超时", "请查看来源状态后重新预览"))
			return
		}
		p, err := j.app.preview(ctx, r.Input.CreateInput, true)
		if err != nil {
			fail(err)
			return
		}
		if p.SourceTurnID != r.SourceTurnID || currentShareHash(p, r.Input) != r.Input.PreviewHash {
			fail(problem.New("preview_changed", "等待期间来源轮次或 Git 起点已变化", "请重新查看起点；没有自动改为分享新的轮次"))
			return
		}
		if p.SourceTurnStatus == "inProgress" {
			select {
			case <-ctx.Done():
				fail(ctx.Err())
				return
			case <-tick.C:
				continue
			}
		}
		if p.SourceTurnStatus != "completed" {
			fail(problem.New("source_turn_incomplete", "来源轮次未正常完成", "请核对来源结果后手动预览并创建"))
			return
		}
		if err := j.transition("creating", "", nil); err != nil {
			fail(err)
			return
		}
		in := r.Input.CreateInput
		// Use the final ordinary preview hash, including Claude's final fingerprint.
		in.PreviewHash = p.Hash
		s, err := j.app.Create(ctx, in)
		if s != nil {
			// Creation may have persisted a partial result before failing. Keep
			// its identity so recovery never creates another fork by accident.
			if saveErr := j.transition("creating", s.record.ID, nil); saveErr != nil {
				fail(saveErr)
				return
			}
		}
		if err != nil {
			fail(err)
			return
		}
		if s == nil || s.view()["state"] != "ready" {
			fail(problem.New("creation_incomplete", "已有创建尚未完成", "以分享请求 ID 查询协作，不重复创建"))
			return
		}
		if err := j.transition("inviting", s.record.ID, nil); err != nil {
			fail(err)
			return
		}
		if err := ctx.Err(); err != nil {
			fail(err)
			return
		}
		if err := s.Share(ctx, r.Input.Transport); err != nil {
			fail(problem.New("invitation_failed", err.Error(), "协作已创建；使用 collaborationId 单独重试邀请，不重新创建"))
			return
		}
		if err := j.transition("ready", s.record.ID, nil); err != nil {
			fail(err)
		}
		return
	}
}

func (a *App) stopShareRequests() {
	a.mu.Lock()
	jobs := make([]*shareRequest, 0, len(a.shareRequests))
	for _, j := range a.shareRequests {
		jobs = append(jobs, j)
	}
	a.mu.Unlock()
	for _, j := range jobs {
		if j.cancel != nil {
			j.cancel()
		}
	}
	a.shareWorkers.Wait()
}

func (a *App) currentShareHTTP(w http.ResponseWriter, r *http.Request, path string) bool {
	if path == "sources/current" && r.Method == "POST" {
		var in mcp.CallerSource
		if !decode(w, r, &in) {
			return true
		}
		out, err := a.currentSource(r.Context(), in)
		respond(w, out, err)
		return true
	}
	if (path == "share-requests/preview" || path == "share-requests") && r.Method == "POST" {
		var in currentShareInput
		if !decode(w, r, &in) {
			return true
		}
		if path == "share-requests/preview" {
			p, err := a.previewCurrentShare(r.Context(), in)
			respond(w, struct {
				Preview
				RequestID     string `json:"requestId"`
				Transport     string `json:"transport"`
				WorkspaceMode string `json:"workspaceMode"`
			}{p, in.RequestID, in.Transport, in.WorkspaceMode}, err)
		} else {
			j, err := a.submitCurrentShare(r.Context(), in)
			if err != nil {
				respond(w, nil, err)
			} else {
				respond(w, j.snapshot(), nil)
			}
		}
		return true
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] != "share-requests" {
		return false
	}
	a.mu.Lock()
	j := a.shareRequests[parts[1]]
	a.mu.Unlock()
	if j == nil {
		respond(w, nil, problem.New("share_request_missing", "没有找到分享请求", "请核对请求 ID 和本机数据目录"))
		return true
	}
	if len(parts) == 2 && r.Method == "GET" {
		respond(w, j.snapshot(), nil)
		return true
	}
	if len(parts) == 3 && parts[2] == "cancel" && r.Method == "POST" {
		j.mu.Lock()
		var err error
		if j.record.State == "waiting" {
			j.record.State, j.record.UpdatedAt = "cancelled", time.Now()
			err = j.saveLocked()
			if j.cancel != nil {
				j.cancel()
			}
		} else if shareRequestActive(j.record.State) {
			err = problem.New("share_already_creating", "已经开始创建协作，无法撤销已发生的创建", "查询请求及同 ID 协作；需要时结束共享，保留目录与会话")
		}
		out := j.record
		j.mu.Unlock()
		respond(w, out, err)
		return true
	}
	http.NotFound(w, r)
	return true
}
