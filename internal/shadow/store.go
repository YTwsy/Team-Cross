package shadow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

func validID(id string) bool { _, err := uuid.Parse(id); return err == nil }

func New(cfg Config, id, spaceID, name, home, cwd string, owner Actor, settings ModelSettings) (*Engine, error) {
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	if !validID(id) || spaceID == "" || strings.TrimSpace(name) == "" || len([]rune(name)) > 80 || owner.ID == "" {
		return nil, fmt.Errorf("分身标识、空间、名称或所有者无效")
	}
	if !filepath.IsAbs(home) || !filepath.IsAbs(cwd) {
		return nil, fmt.Errorf("分身需要明确的本机配置与工作目录")
	}
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("分身工作目录不可用")
	}
	if err = os.MkdirAll(cfg.Directory, 0700); err != nil {
		return nil, err
	}
	if _, err = os.Stat(filepath.Join(cfg.Directory, "shadow.json")); !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("分身记录已存在或无法检查")
	}
	now := time.Now()
	e := newEngine(cfg, Record{Settings: settings, Schema: 1, ID: id, SpaceID: spaceID, Name: strings.TrimSpace(name), Owner: owner, Home: home, Cwd: cwd, Enabled: true, Segments: []Segment{}, Messages: []Message{}, Memories: map[string]Memory{}, Topics: map[string]Topic{}, Jobs: map[string]Job{}, Runs: map[string]Run{}, ToolResults: map[string]json.RawMessage{}, CreatedAt: now, UpdatedAt: now})
	if err = e.saveLocked(); err != nil {
		return nil, err
	}
	return e, nil
}

func Open(cfg Config) (*Engine, error) {
	b, err := os.ReadFile(filepath.Join(cfg.Directory, "shadow.json"))
	if err != nil {
		return nil, err
	}
	var r Record
	if err = json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	if r.Schema != 1 || !validID(r.ID) || r.SpaceID == "" {
		return nil, fmt.Errorf("分身记录版本或身份无效")
	}
	// Older shadows with confirmed native settings can freeze that exact choice.
	// A shadow without a confirmed choice remains paused instead of adopting a
	// potentially changed native default during an upgrade.
	if r.Settings.Model == "" {
		r.Settings = ModelSettings{Model: r.Model, ModelProvider: r.ModelProvider, ReasoningEffort: r.ReasoningEffort}
	}
	if err := r.Settings.Validate(); err != nil {
		r.Enabled = false
		r.Error = "分身缺少已确认的启动配置，请创建新分身并选择模型和推理强度"
	}
	if r.Memories == nil {
		r.Memories = map[string]Memory{}
	}
	if r.Topics == nil {
		r.Topics = map[string]Topic{}
	}
	if r.Jobs == nil {
		r.Jobs = map[string]Job{}
	}
	if r.Runs == nil {
		r.Runs = map[string]Run{}
	}
	if r.ToolResults == nil {
		r.ToolResults = map[string]json.RawMessage{}
	}
	if r.Creating != "" {
		r.Error = "原生会话创建结果不明，保留现场；请核对后继续，不自动重新创建"
		r.Enabled = false
	}
	for id, run := range r.Runs {
		if run.State == "prepared" || run.State == "submitted" {
			run.State = "unknown"
			r.Runs[id] = run
		}
	}
	return newEngine(cfg, r), nil
}

func newEngine(cfg Config, r Record) *Engine {
	if cfg.CheckRatio <= 0 || cfg.CheckRatio >= .95 {
		cfg.CheckRatio = .8
	}
	if cfg.IdleMemoryDelay <= 0 {
		cfg.IdleMemoryDelay = 45 * time.Second
	}
	if cfg.MaxBackground <= 0 {
		cfg.MaxBackground = 2
	}
	if cfg.AuxiliaryTimeout <= 0 {
		cfg.AuxiliaryTimeout = 90 * time.Second
	}
	return &Engine{cfg: cfg, record: r, wake: make(chan struct{}, 1), done: make(chan struct{}), approvals: map[string]Approval{}, compacting: map[string]bool{}, lastActivity: time.Now()}
}

func clone[T any](in T) T {
	b, _ := json.Marshal(in)
	var out T
	_ = json.Unmarshal(b, &out)
	return out
}

// Atomic replacement plus directory sync makes a delivered receipt mean that
// the message survives a process crash. Callers roll back failed mutations.
func writeAtomic(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".shadow-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	if err == nil {
		err = f.Close()
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (e *Engine) saveLocked() error {
	e.record.Revision++
	e.record.UpdatedAt = time.Now()
	return writeAtomic(filepath.Join(e.cfg.Directory, "shadow.json"), e.record)
}

func (e *Engine) mutateLocked(fn func() error) error {
	previous := clone(e.record)
	if err := fn(); err != nil {
		e.record = previous
		return err
	}
	if err := e.saveLocked(); err != nil {
		e.record = previous
		return err
	}
	return nil
}

func (e *Engine) notify() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func (e *Engine) Receive(m Message) (Message, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !validID(m.ID) || m.Actor.ID == "" || strings.TrimSpace(m.Text) == "" || len(m.Text) > 64<<10 {
		return Message{}, fmt.Errorf("消息标识、作者或内容无效")
	}
	for _, old := range e.record.Messages {
		if old.ID == m.ID {
			if old.Role != "user" || old.Text != m.Text || old.Actor.ID != m.Actor.ID {
				return Message{}, fmt.Errorf("消息 ID 已用于不同内容或作者")
			}
			return old, nil
		}
	}
	if e.closed || !e.record.Enabled {
		return Message{}, fmt.Errorf("分身已暂停，消息未送达")
	}
	received := time.Now()
	m.Role, m.State = "user", "delivered"
	if m.CreatedAt.IsZero() {
		m.CreatedAt = received
	}
	m.ReceivedAt = &received
	m.ReplyTo, m.SegmentID, m.TurnID, m.Error = "", "", "", ""
	if err := e.mutateLocked(func() error { e.record.Messages = append(e.record.Messages, m); return nil }); err != nil {
		return Message{}, err
	}
	e.lastActivity = time.Now()
	e.notify()
	return m, nil
}

func (e *Engine) View() View {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.record
	v := View{Settings: r.Settings, ID: r.ID, SpaceID: r.SpaceID, Name: r.Name, Owner: r.Owner, Revision: r.Revision, Enabled: r.Enabled, Online: !e.closed && e.runtime != nil && e.runtime.Alive(), Current: r.Current, Segments: r.Segments, Messages: r.Messages, Memories: []Memory{}, Jobs: []Job{}, Error: r.Error}
	v.Model, v.ModelProvider, v.ReasoningEffort = r.Model, r.ModelProvider, r.ReasoningEffort
	for _, run := range r.Runs {
		if run.ThreadID == r.Current && activeRun(run) {
			v.Busy = true
		}
	}
	for _, m := range r.Memories {
		v.Memories = append(v.Memories, m)
	}
	for _, j := range r.Jobs {
		v.Jobs = append(v.Jobs, j)
	}
	sort.Slice(v.Memories, func(i, j int) bool { return v.Memories[i].Revision < v.Memories[j].Revision })
	sort.Slice(v.Jobs, func(i, j int) bool { return v.Jobs[i].CreatedAt.Before(v.Jobs[j].CreatedAt) })
	v = clone(v)
	if v.Error != "" {
		v.Error = "本机运行需要核对，请联系分身所在成员"
	}
	for i := range v.Messages {
		if v.Messages[i].Error != "" {
			v.Messages[i].Error = "原生处理结果需要核对"
		}
	}
	for i := range v.Jobs {
		if v.Jobs[i].Error != "" {
			v.Jobs[i].Error = "原生任务结果需要核对"
		}
	}
	return v
}

func (e *Engine) DiagnosticError() string { e.mu.Lock(); defer e.mu.Unlock(); return e.record.Error }

func activeRun(r Run) bool {
	return r.State == "prepared" || r.State == "submitted" || r.State == "unknown"
}

func (e *Engine) Identity() (id, spaceID, home, cwd string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.record.ID, e.record.SpaceID, e.record.Home, e.record.Cwd
}

func (e *Engine) SetEnabled(enabled bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return fmt.Errorf("分身运行时已关闭")
	}
	err := e.mutateLocked(func() error {
		if enabled {
			if err := e.record.Settings.Validate(); err != nil {
				return err
			}
		}
		if enabled && e.record.Creating != "" {
			return fmt.Errorf("原生创建结果仍需核对")
		}
		e.record.Enabled = enabled
		if enabled {
			e.record.Error = ""
		}
		return nil
	})
	e.notify()
	return err
}
