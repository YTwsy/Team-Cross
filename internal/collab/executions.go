package collab

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"teamcross/internal/sharing"
	"teamcross/internal/workspace"
)

// The space owns membership. An execution never creates or revokes the space's
// transport. Both objects use the same state mutex, so callers can snapshot the
// membership and execution state together without taking locks in reverse order.
func (s *Session) sharedRuntime() *sharing.Runtime {
	if s.space != nil {
		return s.space.share
	}
	return s.share
}

func (s *Session) spaceID() string {
	if s.space != nil {
		return s.record.SpaceID
	}
	return s.record.ID
}

func executionAddress(spaceID, executionID string) string {
	return spaceID + "/sessions/" + executionID
}

func splitExecutionAddress(id string) (spaceID, executionID string) {
	parts := strings.Split(id, "/sessions/")
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" && !strings.Contains(parts[1], "/") {
		return parts[0], parts[1]
	}
	return id, ""
}

// Called with App.mu. Public execution operations use the composite address;
// native callbacks can resolve their independently unique runtime ID directly.
func (a *App) localSessionLocked(id string) *Session {
	spaceID, executionID := splitExecutionAddress(id)
	if executionID != "" {
		s := a.executions[executionID]
		if s != nil && s.record.SpaceID == spaceID {
			return s
		}
		return nil
	}
	if s := a.sessions[id]; s != nil {
		return s
	}
	return a.executions[id]
}

func (a *App) targetLocked(id string) (*Session, *Joined, string) {
	spaceID, executionID := splitExecutionAddress(id)
	return a.localSessionLocked(id), a.joined[spaceID], executionID
}

func remoteExecutionPath(executionID, path string) string {
	if executionID == "" {
		return "/v2/" + path
	}
	return "/v2/sessions/" + executionID + "/" + path
}

func (j *Joined) executionView(ctx context.Context, executionID string) (map[string]any, error) {
	var out map[string]any
	if err := j.request(ctx, "GET", remoteExecutionPath(executionID, "status"), nil, &out); err != nil {
		return nil, err
	}
	out["spaceId"], out["executionId"] = j.ID, executionID
	return out, nil
}

func (a *App) newExecution(space *Session, r Record) *Session {
	return &Session{app: a, record: r, space: space, mu: space.mu, materialStore: space.materialStore, writer: "owner", epoch: 1, presence: map[string]memberPresence{}, approvals: map[string]Approval{}}
}

func recoverRecord(r *Record) {
	if r.State == "preparing" {
		r.State, r.Error = "error", "上次创建被中断。已保留会话和目录，请先核实创建结果。"
	}
	if r.ExecutionRecord != nil && r.Commands == nil {
		r.Commands = map[string]Command{}
	}
	for id, c := range executionCommands(*r) {
		if c.State == "pending" {
			c.State, c.Error = "unknown", "服务曾重启，请先查看会话结果"
			r.Commands[id] = c
		}
	}
	if r.Workbench != nil {
		for id, request := range r.Workbench.Requests {
			if request.State == "queued" {
				request.State, request.Error = "failed", "Core 已重启，未自动投递，请核对后明确新建请求"
				r.Workbench.Requests[id] = request
			}
		}
		if r.Workbench.Assistant.State == "initializing" {
			r.Workbench.Assistant.State = "failed"
		}
	}
}

func (a *App) loadSpaces() error {
	root := filepath.Join(a.Config.DataDir, "collaborations")
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		var r Record
		if err = readJSON(filepath.Join(root, entry.Name(), "collaboration.json"), &r); err != nil {
			return fmt.Errorf("读取空间 %s: %w", entry.Name(), err)
		}
		if r.ID != entry.Name() {
			return fmt.Errorf("空间记录身份不匹配: %s", entry.Name())
		}
		if a.executions[r.ID] != nil {
			return fmt.Errorf("空间与协作会话标识重复: %s", r.ID)
		}
		if r.Schema != 4 || r.ExecutionRecord != nil || r.SpaceID != "" {
			// Show an explicit unsupported record without rewriting it or losing
			// the user's files, native history and worktrees.
			r = Record{Schema: r.Schema, ID: r.ID, Title: r.Title, State: "unsupported", Error: "旧空间格式不受支持，请新建空间。原数据与工作目录已保留。", CreatedAt: r.CreatedAt}
			a.sessions[r.ID] = a.newSession(r)
			continue
		}
		recoverRecord(&r)
		space := a.newSession(r)
		a.sessions[r.ID] = space
		children, readErr := os.ReadDir(filepath.Join(root, r.ID, "sessions"))
		if readErr != nil && !os.IsNotExist(readErr) {
			return readErr
		}
		for _, child := range children {
			if !child.IsDir() {
				continue
			}
			var execution Record
			if err = readJSON(filepath.Join(root, r.ID, "sessions", child.Name(), "execution.json"), &execution); err != nil {
				return err
			}
			if execution.Schema != 4 || execution.SpaceID != r.ID || execution.ID != child.Name() || execution.ExecutionRecord == nil {
				return fmt.Errorf("协作会话记录无效: %s/%s", r.ID, child.Name())
			}
			if a.executions[execution.ID] != nil || a.sessions[execution.ID] != nil {
				return fmt.Errorf("协作会话标识重复: %s", execution.ID)
			}
			recoverRecord(&execution)
			s := a.newExecution(space, execution)
			space.executions[execution.ID], a.executions[execution.ID] = s, s
		}
	}
	return nil
}

func (s *Session) executionViewsLocked(ctx context.Context) []map[string]any {
	views := []map[string]any{}
	for _, child := range s.executions {
		views = append(views, child.viewLocked(ctx))
	}
	sort.Slice(views, func(i, j int) bool { return fmt.Sprint(views[i]["createdAt"]) < fmt.Sprint(views[j]["createdAt"]) })
	return views
}

// End one execution and drain accepted work. Reading remains available to all
// space members; a later start retains its native ID, directory and mode.
func (s *Session) endExecutionLocked() error {
	previous := s.record.Paused
	s.record.Paused = true
	if err := s.saveLocked(); err != nil {
		s.record.Paused = previous
		return err
	}
	if s.direct != nil && s.direct.role != "owner" {
		s.direct.close()
		s.direct = nil
	}
	s.writer, s.epoch = "owner", s.epoch+1
	s.presence = map[string]memberPresence{}
	s.releaseWhenIdle, s.annotationAccess = true, false
	s.releaseIfIdleLocked()
	return nil
}

// Directory leases cover managed writable runtimes across spaces, even when
// two paths differ only by a symlink or refer to subdirectories of one worktree.
func (s *Session) acquireDirectory(cwd string) error {
	root, err := canonicalWorkspaceRoot(cwd)
	if err != nil {
		return err
	}
	s.app.directoryMu.Lock()
	defer s.app.directoryMu.Unlock()
	if s.app.directoryOwners == nil {
		s.app.directoryOwners = map[string]string{}
	}
	for occupied, owner := range s.app.directoryOwners {
		if owner != s.record.ID && (occupied == root || strings.HasPrefix(root, occupied+string(filepath.Separator)) || strings.HasPrefix(occupied, root+string(filepath.Separator))) {
			return fmt.Errorf("工作目录已由协作会话 %s 使用；请先暂停该会话，或选择新的 worktree", owner)
		}
	}
	s.app.directoryOwners[root] = s.record.ID
	return nil
}

func (s *Session) releaseDirectory() {
	s.app.directoryMu.Lock()
	defer s.app.directoryMu.Unlock()
	for root, id := range s.app.directoryOwners {
		if id == s.record.ID {
			delete(s.app.directoryOwners, root)
		}
	}
}

func canonicalWorkspaceRoot(cwd string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	root, gitErr := workspace.Git(ctx, abs, "rev-parse", "--show-toplevel")
	if gitErr == nil {
		return filepath.EvalSymlinks(strings.TrimSpace(string(root)))
	}
	// Independent receiving sessions may use a non-Git directory.
	return abs, nil
}

func creationSpaceID(in CreateInput) string {
	if in.SpaceID != "" {
		return in.SpaceID
	}
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("teamcross-space:"+in.RequestID)).String()
}

// Called under the common space state lock, including at send and claim.
func (s *Session) executionWriterLocked(id, member string) bool {
	child := s.executions[id]
	return child != nil && child.writer == member && !child.record.Paused && (member == "owner" || s.share != nil && s.share.HasMember(member))
}
