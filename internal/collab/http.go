package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"teamcross/internal/problem"
	"teamcross/internal/sharing"
	"time"
)

type RPCInput struct {
	Method    string         `json:"method"`
	Params    map[string]any `json:"params"`
	RequestID string         `json:"requestId"`
}
type ReplyInput struct {
	ID     json.RawMessage `json:"id"`
	Result any             `json:"result"`
}

func number(s string) uint64 { v, _ := strconv.ParseUint(s, 10, 64); return v }
func respond(w http.ResponseWriter, value any, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		w.WriteHeader(400)
		_ = json.NewEncoder(w).Encode(problem.Describe(err))
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}
func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	if e := json.NewDecoder(r.Body).Decode(out); e != nil {
		respond(w, nil, fmt.Errorf("请求内容无效: %w", e))
		return false
	}
	return true
}
func (a *App) View(ctx context.Context, id string) (map[string]any, error) {
	a.mu.Lock()
	s, j := a.sessions[id], a.joined[id]
	a.mu.Unlock()
	if s != nil {
		return s.view(), nil
	}
	if j != nil {
		return j.view(ctx), nil
	}
	return nil, fmt.Errorf("没有找到协作")
}
func (s *Session) annotate(in Annotation, author string, contexts ...context.Context) (Annotation, error) {
	text := strings.TrimSpace(in.Text)
	if text == "" || len([]rune(text)) > 4000 {
		return Annotation{}, fmt.Errorf("请输入 1–4000 字的批注")
	}
	if len(in.Reference) > 1000 {
		return Annotation{}, fmt.Errorf("引用过长")
	}
	if in.Target != nil {
		// Own this value before normalization or binding it to the shared fork.
		target := *in.Target
		in.Target = &target
	}
	if err := in.Target.validate(); err != nil {
		return Annotation{}, err
	}
	in.ID = uuid.NewString()
	in.Replies = nil // Client-supplied replies and author identities are never imported.
	in.Text = text
	in.Author = author
	in.CreatedAt = time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(contexts) > 0 && !s.callerValidLocked(contexts[0]) {
		return Annotation{}, fmt.Errorf("共享已结束")
	}
	if in.Target != nil {
		if in.Target.SessionID != "" && in.Target.SessionID != s.record.SessionID {
			return Annotation{}, fmt.Errorf("批注不属于当前协作会话")
		}
		in.Target.SessionID = s.record.SessionID
	}
	previousUpdatedAt := s.record.UpdatedAt
	s.record.Annotations = append(s.record.Annotations, in)
	s.record.UpdatedAt = time.Now()
	if err := s.saveLocked(); err != nil {
		s.record.Annotations = s.record.Annotations[:len(s.record.Annotations)-1]
		s.record.UpdatedAt = previousUpdatedAt
		return Annotation{}, err
	}
	return in, nil
}
func (a *App) target(ctx context.Context, id, method, path string, input any) (any, error) {
	a.mu.Lock()
	s, j := a.sessions[id], a.joined[id]
	a.mu.Unlock()
	if j != nil {
		var out json.RawMessage
		e := j.request(ctx, method, "/v2/"+path, input, &out)
		return out, e
	}
	if s == nil {
		return nil, fmt.Errorf("没有找到协作")
	}
	if strings.HasPrefix(path, "context") {
		u, _ := url.Parse(path)
		return s.Context(ctx, u.Query().Get("kind"), u.Query().Get("path"), number(u.Query().Get("after")), u.Query().Get("cursor"))
	}
	switch path {
	case "rpc":
		in := input.(RPCInput)
		return s.RPC(ctx, "owner", in.Method, in.Params, in.RequestID)
	case "respond":
		in := input.(ReplyInput)
		return map[string]bool{"ok": true}, s.Respond(ctx, "owner", in.ID, in.Result)
	case "annotations":
		return s.annotate(input.(Annotation), "发起者")
	case "annotation-replies":
		return s.replyAnnotation(ctx, input.(AnnotationReplyInput), "发起者")
	}
	return nil, fmt.Errorf("操作不受支持")
}
func (a *App) Handler(web http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, e := net.SplitHostPort(r.Host)
		if e != nil {
			host = r.Host
		}
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			http.Error(w, "仅接受本机访问", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, e := url.Parse(origin)
			if e != nil || u.Host != r.Host {
				http.Error(w, "请求来源不匹配", 403)
				return
			}
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			web.ServeHTTP(w, r)
			return
		}
		a.http(w, r)
	})
}
func (a *App) http(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	ctx := r.Context()
	if strings.HasPrefix(path, "runtime-annotations/") {
		a.runtimeAnnotationsHTTP(w, r, strings.TrimPrefix(path, "runtime-annotations/"))
		return
	}
	if a.onboarding(w, r, path) {
		return
	}
	switch path {
	case "info":
		respond(w, a.Info(ctx), nil)
		return
	case "settings":
		if r.Method != "POST" {
			http.NotFound(w, r)
			return
		}
		var in Settings
		if !decode(w, r, &in) {
			return
		}
		a.mu.Lock()
		a.settings = in
		e := writeJSONFile(filepath.Join(a.Config.DataDir, "settings.json"), in)
		a.mu.Unlock()
		respond(w, map[string]bool{"ok": e == nil}, e)
		return
	case "mcp/setup":
		if r.Method != "POST" {
			http.NotFound(w, r)
			return
		}
		var in struct {
			Provider string `json:"provider"`
		}
		if !decode(w, r, &in) {
			return
		}
		e := a.SetupMCP(ctx, in.Provider)
		respond(w, map[string]bool{"ok": e == nil}, e)
		return
	case "sources":
		out, e := a.SourcesFor(ctx, r.URL.Query().Get("provider"), r.URL.Query().Get("search"), r.URL.Query().Get("cursor"))
		respond(w, out, e)
		return
	case "preview":
		if r.Method != "POST" {
			http.NotFound(w, r)
			return
		}
		var in CreateInput
		if !decode(w, r, &in) {
			return
		}
		out, e := a.Preview(ctx, in)
		respond(w, out, e)
		return
	case "collaborations":
		if r.Method == "GET" {
			respond(w, a.List(ctx), nil)
			return
		}
		if r.Method != "POST" {
			http.NotFound(w, r)
			return
		}
		var in CreateInput
		if !decode(w, r, &in) {
			return
		}
		s, e := a.Create(ctx, in)
		if e != nil {
			respond(w, nil, e)
			return
		}
		respond(w, s.view(), nil)
		return
	case "join":
		if r.Method != "POST" {
			http.NotFound(w, r)
			return
		}
		var in struct {
			Invitation string `json:"invitation"`
			PendingID  string `json:"pendingId"`
		}
		if !decode(w, r, &in) {
			return
		}
		token, e := a.invitation(in.Invitation, in.PendingID)
		if e != nil {
			respond(w, nil, e)
			return
		}
		j, e := a.Join(ctx, token)
		if e != nil {
			respond(w, nil, e)
			return
		}
		respond(w, j.view(ctx), nil)
		return
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] != "collaborations" {
		http.NotFound(w, r)
		return
	}
	id := parts[1]
	if len(parts) == 2 && r.Method == "GET" {
		out, e := a.View(ctx, id)
		respond(w, out, e)
		return
	}
	if len(parts) != 3 {
		http.NotFound(w, r)
		return
	}
	action := parts[2]
	if action == "context" && r.Method == "GET" {
		out, e := a.target(ctx, id, "GET", "context?"+r.URL.RawQuery, nil)
		respond(w, out, e)
		return
	}
	if r.Method != "POST" {
		http.NotFound(w, r)
		return
	}
	switch action {
	case "action":
		var in struct {
			Action    string `json:"action"`
			Transport string `json:"transport"`
			Epoch     uint64 `json:"epoch"`
		}
		if !decode(w, r, &in) {
			return
		}
		if in.Action == "leave" {
			a.mu.Lock()
			j := a.joined[id]
			a.mu.Unlock()
			if j != nil {
				e := j.leave(ctx)
				respond(w, map[string]bool{"ok": e == nil}, e)
				return
			}
		}
		if in.Action == "return" || in.Action == "request_input" || in.Action == "cancel_input" {
			a.mu.Lock()
			j := a.joined[id]
			a.mu.Unlock()
			if j != nil {
				var result any
				e := j.request(ctx, "POST", "/v2/"+in.Action, map[string]uint64{"epoch": in.Epoch}, &result)
				respond(w, result, e)
				return
			}
		}
		s, e := a.owned(id)
		if e != nil {
			respond(w, nil, e)
			return
		}
		s.mu.Lock()
		stale := (in.Action == "handoff" || in.Action == "reclaim") && in.Epoch != s.epoch
		s.mu.Unlock()
		if stale {
			respond(w, nil, fmt.Errorf("输入状态已变化，请刷新后重试"))
			return
		}
		if in.Action == "share" {
			if in.Transport == "" {
				in.Transport = string(sharing.TransportLAN)
			}
			e = s.Share(ctx, in.Transport)
		} else {
			e = s.Action(ctx, in.Action, in.Epoch)
		}
		respond(w, s.view(), e)
	case "personal-desktop":
		var in struct {
			Launch bool `json:"launch"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, e := a.PersonalDesktopPlan(ctx, id, in.Launch)
		respond(w, out, e)
	case "open", "assist":
		var in struct {
			Provider string `json:"provider"`
			Client   string `json:"client"`
			Launch   bool   `json:"launch"`
		}
		if !decode(w, r, &in) {
			return
		}
		var out map[string]any
		var e error
		if action == "assist" {
			out, e = a.AssistPlan(ctx, id, in.Provider, in.Client, in.Launch)
		} else {
			out, e = a.ClientPlan(ctx, id, in.Client, in.Launch)
		}
		respond(w, out, e)
	case "rpc":
		var in RPCInput
		if !decode(w, r, &in) {
			return
		}
		out, e := a.target(ctx, id, "POST", "rpc", in)
		respond(w, out, e)
	case "respond":
		var in ReplyInput
		if !decode(w, r, &in) {
			return
		}
		out, e := a.target(ctx, id, "POST", "respond", in)
		respond(w, out, e)
	case "annotations":
		var in Annotation
		if !decode(w, r, &in) {
			return
		}
		out, e := a.target(ctx, id, "POST", "annotations", in)
		respond(w, out, e)
	case "annotation-replies":
		var in AnnotationReplyInput
		if !decodeAnnotationReply(w, r, &in) {
			return
		}
		out, e := a.target(ctx, id, "POST", "annotation-replies", in)
		respond(w, out, e)
	default:
		http.NotFound(w, r)
	}
}

func decodeAnnotationReply(w http.ResponseWriter, r *http.Request, out *AnnotationReplyInput) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		respond(w, nil, fmt.Errorf("回复仅接受 annotationId、text 和 requestId: %w", err))
		return false
	}
	return true
}
