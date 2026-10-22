package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/runtimeconfig"
	"teamcross/internal/shadow"
	"teamcross/internal/sharing"
)

// The space host stores only explicitly shared content and short delivery
// attempts. A message is delivered only after the executing member persists it.
type ShadowShared struct {
	View       shadow.View               `json:"view"`
	LastSeen   time.Time                 `json:"-"`
	Deliveries map[string]ShadowDelivery `json:"deliveries"`
}

type ShadowDelivery struct {
	Message  shadow.Message `json:"message"`
	Deadline time.Time      `json:"deadline"`
	State    string         `json:"state"` // sending, delivered, sendFailed
}

type shadowInput struct {
	ID        string         `json:"shadowId"`
	Name      string         `json:"name,omitempty"`
	RequestID string         `json:"requestId,omitempty"`
	Cwd       string         `json:"cwd,omitempty"`
	Message   shadow.Message `json:"message"`
	Snapshot  *shadow.View   `json:"snapshot,omitempty"`
	Offset    int            `json:"offset,omitempty"`
}

type shadowSpaceView struct {
	SelfID  string        `json:"selfId"`
	Shadows []shadow.View `json:"shadows"`
	Total   int           `json:"total"`
	Next    *int          `json:"nextOffset,omitempty"`
}

func (s *Session) shadowOperation(ctx context.Context, op string, in shadowInput) (out any, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.record.State == "unsupported" || s.space != nil || !s.callerValidLocked(ctx) {
		return nil, fmt.Errorf("共享已结束或成员已移除")
	}
	if s.record.State == "ended" && op != "view" {
		return nil, fmt.Errorf("空间已关闭，分身不再接收输入")
	}
	previous := libraryClone(s.record.Shadows)
	if s.record.Shadows == nil {
		s.record.Shadows = map[string]ShadowShared{}
	}
	changed := false
	defer func() {
		if err == nil && changed {
			err = s.saveLocked()
		}
		if err != nil {
			s.record.Shadows = previous
		}
		if err == nil {
			out = libraryClone[any](out)
		}
	}()
	actor := shadow.Actor{ID: sharing.MemberID(ctx), Name: sharing.MemberName(ctx)}
	if actor.Name == "" {
		actor.Name = actor.ID
	}
	now := time.Now()
	for id, shared := range s.record.Shadows {
		for mid, delivery := range shared.Deliveries {
			if delivery.State == "sending" && now.After(delivery.Deadline) {
				delivery.State = "sendFailed"
				shared.Deliveries[mid] = delivery
				changed = true
			}
		}
		s.record.Shadows[id] = shared
	}
	shared, exists := s.record.Shadows[in.ID]
	switch op {
	case "check":
		return actor, nil
	case "register":
		if _, err = uuid.Parse(in.ID); err != nil || strings.TrimSpace(in.Name) == "" || len([]rune(in.Name)) > 80 {
			return nil, fmt.Errorf("分身身份或名称无效")
		}
		if exists {
			if shared.View.Owner.ID != actor.ID || shared.View.Name != in.Name {
				return nil, fmt.Errorf("分身标识已属于其他配置")
			}
			return shared.View, nil
		}
		if len(s.record.Shadows) >= 64 {
			return nil, fmt.Errorf("空间分身数量已达上限")
		}
		shared = ShadowShared{View: shadow.View{ID: in.ID, SpaceID: s.record.ID, Name: in.Name, Owner: actor, Enabled: true, Segments: []shadow.Segment{}, Messages: []shadow.Message{}, Memories: []shadow.Memory{}, Jobs: []shadow.Job{}}, Deliveries: map[string]ShadowDelivery{}}
		s.record.Shadows[in.ID], changed = shared, true
		return shared.View, nil
	case "view":
		view := shadowSpaceView{SelfID: actor.ID, Shadows: []shadow.View{}}
		if in.Offset < 0 {
			return nil, fmt.Errorf("消息分页位置无效")
		}
		for id, entry := range s.record.Shadows {
			if in.ID != "" && in.ID != id {
				continue
			}
			v := libraryClone(entry.View)
			v.Online = v.Online && now.Sub(entry.LastSeen) < 10*time.Second
			if in.ID == "" {
				v.Messages, v.Memories, v.Jobs, v.Segments = nil, nil, nil, nil
			} else {
				messages := map[string]shadow.Message{}
				for _, m := range v.Messages {
					messages[m.ID] = m
				}
				for mid, d := range entry.Deliveries {
					if _, ok := messages[mid]; !ok {
						m := d.Message
						m.State = d.State
						messages[mid] = m
					}
				}
				v.Messages = []shadow.Message{}
				for _, m := range messages {
					v.Messages = append(v.Messages, m)
				}
				sort.SliceStable(v.Messages, func(i, j int) bool { return v.Messages[i].CreatedAt.Before(v.Messages[j].CreatedAt) })
				view.Total = len(v.Messages)
				end := max(0, len(v.Messages)-in.Offset)
				start := max(0, end-100)
				v.Messages = v.Messages[start:end]
				if start > 0 {
					n := in.Offset + len(v.Messages)
					view.Next = &n
				}
			}
			view.Shadows = append(view.Shadows, v)
		}
		sort.Slice(view.Shadows, func(i, j int) bool { return view.Shadows[i].Name < view.Shadows[j].Name })
		return view, nil
	case "send":
		if !exists {
			return nil, fmt.Errorf("分身不存在")
		}
		m := in.Message
		if _, parse := uuid.Parse(m.ID); parse != nil || strings.TrimSpace(m.Text) == "" || len(m.Text) > 64<<10 {
			return nil, fmt.Errorf("消息标识或正文无效")
		}
		if old, ok := shared.Deliveries[m.ID]; ok {
			if old.Message.Actor.ID != actor.ID || old.Message.Text != m.Text {
				return nil, fmt.Errorf("消息 ID 已用于不同作者或内容")
			}
			if old.State == "delivered" || old.State == "sending" {
				return old, nil
			}
			m = old.Message
		} else {
			m = shadow.Message{ID: m.ID, Actor: actor, Role: "user", Text: m.Text, CreatedAt: now}
		}
		state := "sending"
		if !shared.View.Enabled || !shared.View.Online || now.Sub(shared.LastSeen) >= 10*time.Second {
			state = "sendFailed"
		}
		d := ShadowDelivery{Message: m, State: state, Deadline: now.Add(12 * time.Second)}
		if shared.Deliveries == nil {
			shared.Deliveries = map[string]ShadowDelivery{}
		}
		shared.Deliveries[m.ID] = d
		s.record.Shadows[in.ID], changed = shared, true
		return d, nil
	case "sync":
		if !exists || shared.View.Owner.ID != actor.ID || in.Snapshot == nil {
			return nil, fmt.Errorf("只有分身所在成员可以同步状态")
		}
		v := *in.Snapshot
		if v.ID != in.ID || v.Owner.ID != actor.ID || v.Name != shared.View.Name || v.SpaceID != s.record.ID {
			return nil, fmt.Errorf("分身同步身份不匹配")
		}
		if v.Revision > shared.View.Revision || v.Online != shared.View.Online {
			known := map[string]int{}
			messages := libraryClone(shared.View.Messages)
			for i, m := range messages {
				known[m.ID] = i
			}
			for _, m := range v.Messages {
				if m.Role == "user" {
					d, ok := shared.Deliveries[m.ID]
					if !ok || d.Message.Text != m.Text || d.Message.Actor.ID != m.Actor.ID || m.ReceivedAt == nil {
						return nil, fmt.Errorf("分身同步包含未核对的成员消息")
					}
					// The receiver confirms persistence, not the author's identity
					// or original send time. Keep the host's authenticated values.
					m.Actor, m.CreatedAt = d.Message.Actor, d.Message.CreatedAt
					d.State = "delivered"
					d.Message = m
					shared.Deliveries[m.ID] = d
				} else if m.Role != "assistant" || m.Actor.ID != v.ID {
					return nil, fmt.Errorf("分身回复身份无效")
				}
				if i, ok := known[m.ID]; ok {
					messages[i] = m
				} else {
					known[m.ID] = len(messages)
					messages = append(messages, m)
				}
			}
			v.Messages = messages
			shared.View = v
			changed = true
		}
		shared.LastSeen = now
		s.record.Shadows[in.ID] = shared
		pending := []shadow.Message{}
		if shared.View.Enabled && shared.View.Online {
			for _, d := range shared.Deliveries {
				if d.State == "sending" && now.Before(d.Deadline) {
					pending = append(pending, d.Message)
				}
			}
		}
		sort.Slice(pending, func(i, j int) bool { return pending[i].CreatedAt.Before(pending[j].CreatedAt) })
		return pending, nil
	default:
		return nil, fmt.Errorf("未知分身操作")
	}
}

func (a *App) shadowCall(ctx context.Context, spaceID, op string, in shadowInput) (any, error) {
	a.mu.Lock()
	s, j, closed := a.sessions[spaceID], a.joined[spaceID], a.closed
	a.mu.Unlock()
	if closed {
		return nil, fmt.Errorf("Core 已退出")
	}
	if s != nil {
		return s.shadowOperation(ctx, op, in)
	}
	if j == nil {
		return nil, fmt.Errorf("协作空间不存在")
	}
	j.mu.Lock()
	hostID := j.Invitation.ID
	j.mu.Unlock()
	var out any
	if err := j.request(ctx, "POST", "/v2/shadows/"+op, remapWorkbench(in, spaceID, hostID), &out); err != nil {
		return nil, err
	}
	return remapWorkbench(out, hostID, spaceID), nil
}

func (a *App) shadowConfig(id string) shadow.Config {
	return shadow.Config{Directory: filepath.Join(a.Config.DataDir, "shadows", id), Start: func(ctx context.Context, home, cwd, log string) (shadow.Runtime, error) {
		return a.startProcessWithMode(ctx, home, cwd, log, runtimeconfig.Trusted)
	}}
}

func (a *App) loadShadows() error {
	a.shadows = map[string]*shadow.Engine{}
	root := filepath.Join(a.Config.DataDir, "shadows")
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
		if _, err := uuid.Parse(entry.Name()); err != nil {
			continue
		}
		e, err := shadow.Open(a.shadowConfig(entry.Name()))
		if err != nil {
			return fmt.Errorf("无法读取分身记录 %s: %w", entry.Name(), err)
		}
		a.shadows[entry.Name()] = e
	}
	return nil
}

func (a *App) createShadow(ctx context.Context, spaceID string, in shadowInput) (any, error) {
	a.shadowCreateMu.Lock()
	defer a.shadowCreateMu.Unlock()
	if _, err := uuid.Parse(in.RequestID); err != nil {
		return nil, fmt.Errorf("创建分身需要唯一 requestId")
	}
	if _, err := a.binary(); err != nil {
		return nil, err
	}
	actorValue, err := a.shadowCall(ctx, spaceID, "check", shadowInput{})
	if err != nil {
		return nil, err
	}
	actor := workbenchDecode[shadow.Actor](actorValue)
	a.mu.Lock()
	e := a.shadows[in.RequestID]
	a.mu.Unlock()
	if e != nil {
		v := e.View()
		_, storedSpace, _, cwd := e.Identity()
		if storedSpace != spaceID || cwd != in.Cwd || v.Name != strings.TrimSpace(in.Name) {
			return nil, fmt.Errorf("创建标识已用于不同分身配置")
		}
	} else {
		home, err := a.providerHome("codex")
		if err != nil {
			return nil, err
		}
		e, err = shadow.New(a.shadowConfig(in.RequestID), in.RequestID, spaceID, in.Name, home, in.Cwd, actor)
		if err != nil {
			return nil, err
		}
		a.mu.Lock()
		a.shadows[in.RequestID] = e
		a.mu.Unlock()
	}
	if _, err = a.shadowCall(ctx, spaceID, "register", shadowInput{ID: in.RequestID, Name: e.View().Name}); err != nil {
		return nil, err
	}
	e.Start()
	return e.View(), nil
}

func (a *App) startShadows() {
	ctx, cancel := context.WithCancel(context.Background())
	a.shadowCancel = cancel
	a.shadowWG.Add(1)
	go func() {
		defer a.shadowWG.Done()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.pumpShadows(ctx)
			}
		}
	}()
}

func (a *App) pumpShadows(ctx context.Context) {
	a.mu.Lock()
	engines := make([]*shadow.Engine, 0, len(a.shadows))
	for _, e := range a.shadows {
		engines = append(engines, e)
	}
	a.mu.Unlock()
	for _, e := range engines {
		if ctx.Err() != nil {
			return
		}
		id, spaceID, _, _ := e.Identity()
		check, cancel := context.WithTimeout(ctx, 4*time.Second)
		_, err := a.shadowCall(check, spaceID, "check", shadowInput{})
		cancel()
		if err != nil {
			// A transient network error does not cancel accepted work. Explicit
			// local knowledge of removal/closure stops the owned runtime.
			a.mu.Lock()
			s, j := a.sessions[spaceID], a.joined[spaceID]
			a.mu.Unlock()
			ended := s == nil && j == nil
			if s != nil {
				s.mu.Lock()
				ended = s.closed || s.record.State == "ended"
				s.mu.Unlock()
			}
			if j != nil {
				j.mu.Lock()
				ended = j.left || j.ended || j.closed
				j.mu.Unlock()
			}
			v := e.View()
			if ended && (v.Enabled || v.Online) {
				_ = e.Suspend()
			}
			continue
		}
		e.Start()
		v := e.View()
		if len(v.Messages) > 200 {
			v.Messages = v.Messages[len(v.Messages)-200:]
		}
		syncCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		out, err := a.shadowCall(syncCtx, spaceID, "sync", shadowInput{ID: id, Snapshot: &v})
		cancel()
		if err != nil {
			continue
		}
		for _, message := range workbenchDecode[[]shadow.Message](out) {
			if _, err = e.Receive(message); err != nil {
				break
			}
		}
	}
}

func (a *App) shadowsHTTP(w http.ResponseWriter, r *http.Request, path string) bool {
	parts := strings.Split(path, "/")
	if len(parts) == 4 && parts[0] == "collaborations" && parts[2] == "shadows" {
		spaceID, op := parts[1], parts[3]
		if op == "view" && r.Method == "GET" {
			out, err := a.shadowCall(r.Context(), spaceID, "view", shadowInput{ID: r.URL.Query().Get("shadowId"), Offset: int(number(r.URL.Query().Get("offset")))})
			respond(w, out, err)
			return true
		}
		if r.Method != "POST" {
			http.NotFound(w, r)
			return true
		}
		var in shadowInput
		if !decode(w, r, &in) {
			return true
		}
		var out any
		var err error
		switch op {
		case "create":
			out, err = a.createShadow(r.Context(), spaceID, in)
		case "send":
			out, err = a.shadowCall(r.Context(), spaceID, "send", in)
		default:
			http.NotFound(w, r)
			return true
		}
		respond(w, out, err)
		return true
	}
	if len(parts) == 3 && parts[0] == "shadows" {
		a.mu.Lock()
		e := a.shadows[parts[1]]
		a.mu.Unlock()
		if e == nil {
			http.NotFound(w, r)
			return true
		}
		if parts[2] == "local" && r.Method == "GET" {
			_, _, _, cwd := e.Identity()
			respond(w, map[string]any{"view": e.View(), "cwd": cwd, "approvals": e.Approvals(), "diagnosticError": e.DiagnosticError()}, nil)
			return true
		}
		if parts[2] != "action" || r.Method != "POST" {
			http.NotFound(w, r)
			return true
		}
		var in struct {
			Action string          `json:"action"`
			ID     json.RawMessage `json:"id"`
			Result any             `json:"result"`
		}
		if !decode(w, r, &in) {
			return true
		}
		_, spaceID, _, _ := e.Identity()
		if in.Action != "pause" {
			if _, err := a.shadowCall(r.Context(), spaceID, "check", shadowInput{}); err != nil {
				respond(w, nil, err)
				return true
			}
		}
		var err error
		switch in.Action {
		case "pause":
			err = e.SetEnabled(false)
		case "resume":
			err = e.SetEnabled(true)
			e.Start()
		case "rollover":
			err = e.Rollover()
		case "reconcile":
			err = e.Reconcile(r.Context())
		case "respond":
			err = e.Respond(r.Context(), in.ID, in.Result)
		default:
			err = fmt.Errorf("未知分身管理操作")
		}
		respond(w, e.View(), err)
		return true
	}
	return false
}

func (s *Session) remoteShadowsHTTP(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/v2/shadows/") {
		return false
	}
	if r.Method != "POST" {
		http.NotFound(w, r)
		return true
	}
	var in shadowInput
	if decode(w, r, &in) {
		out, err := s.shadowOperation(r.Context(), strings.TrimPrefix(r.URL.Path, "/v2/shadows/"), in)
		respond(w, out, err)
	}
	return true
}
