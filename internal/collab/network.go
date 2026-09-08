package collab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"teamcross/internal/nativecodex"
	"teamcross/internal/sharing"
)

func (s *Session) endShareLocked() {
	if s.expiry != nil {
		s.expiry.Stop()
		s.expiry = nil
	}
	if s.share != nil {
		s.share.Revoke()
		s.app.mu.Lock()
		s.app.retiredShares = append(s.app.retiredShares, s.share)
		s.app.mu.Unlock()
		s.share = nil
	}
	if s.direct != nil && s.direct.role == "remote" {
		s.direct.close()
	}
	s.writer = "owner"
	s.epoch++
}
func (s *Session) Action(ctx context.Context, action string, expected ...uint64) error {
	if action == "start" {
		return s.start(ctx, true)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(expected) > 0 && (action == "handoff" || action == "reclaim") && expected[0] != s.epoch {
		return fmt.Errorf("输入状态已变化，请刷新后重试")
	}
	switch action {
	case "share":
		if s.share != nil {
			return nil
		}
		if !s.online || s.record.State != "ready" {
			return fmt.Errorf("请先恢复协作运行时")
		}
		rt, e := sharing.Start(s.record.ID, s.record.Title, s.app.Host, http.HandlerFunc(s.remoteHTTP), s.app.Config.Loopback)
		if e != nil {
			return e
		}
		s.share = rt
		s.expiry = time.AfterFunc(time.Until(rt.Invitation.ExpiresAt), func() { s.mu.Lock(); defer s.mu.Unlock(); s.endShareLocked() })
	case "end":
		s.endShareLocked()
	case "handoff":
		if s.share == nil {
			return fmt.Errorf("请先创建邀请")
		}
		if s.busy {
			return fmt.Errorf("当前轮正在运行，请完成后交出输入")
		}
		if s.direct != nil {
			s.direct.close()
			s.direct = nil
		}
		s.writer = "remote"
		s.epoch++
	case "reclaim":
		if s.direct != nil {
			s.direct.close()
			s.direct = nil
		}
		s.writer = "owner"
		s.epoch++
	default:
		return fmt.Errorf("未知协作操作")
	}
	return nil
}
func (s *Session) remoteHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v2/connect" {
		s.attach(w, r, "remote")
		return
	}
	if r.URL.Path == "/v2/status" {
		out := s.view()
		delete(out, "invitation")
		out["role"] = "remote"
		respond(w, out, nil)
		return
	}
	if r.Method == "POST" && r.URL.Path == "/v2/return" {
		var in struct {
			Epoch uint64 `json:"epoch"`
		}
		if !decode(w, r, &in) {
			return
		}
		s.mu.Lock()
		var e error
		if in.Epoch != s.epoch || s.writer != "remote" {
			e = fmt.Errorf("输入状态已变化，请刷新")
		} else if s.busy {
			e = fmt.Errorf("请等待当前轮完成后交还输入")
		} else {
			if s.direct != nil {
				s.direct.close()
				s.direct = nil
			}
			s.writer = "owner"
			s.epoch++
		}
		s.mu.Unlock()
		respond(w, map[string]bool{"ok": e == nil}, e)
		return
	}
	if r.URL.Path == "/v2/context" {
		after := number(r.URL.Query().Get("after"))
		out, e := s.Context(r.Context(), r.URL.Query().Get("kind"), r.URL.Query().Get("path"), after, r.URL.Query().Get("cursor"))
		respond(w, out, e)
		return
	}
	if r.Method == "POST" && r.URL.Path == "/v2/rpc" {
		var in RPCInput
		if !decode(w, r, &in) {
			return
		}
		out, e := s.RPC(r.Context(), "remote", in.Method, in.Params, in.RequestID)
		respond(w, out, e)
		return
	}
	if r.Method == "POST" && r.URL.Path == "/v2/respond" {
		var in ReplyInput
		if !decode(w, r, &in) {
			return
		}
		respond(w, map[string]bool{"ok": true}, s.Respond(r.Context(), "remote", in.ID, in.Result))
		return
	}
	if r.Method == "POST" && r.URL.Path == "/v2/annotations" {
		var in Annotation
		if !decode(w, r, &in) {
			return
		}
		out, e := s.annotate(in, "协作者")
		respond(w, out, e)
		return
	}
	http.NotFound(w, r)
}
func (a *App) Join(ctx context.Context, token string) (*Joined, error) {
	invitation, e := sharing.Decode(token)
	if e != nil {
		return nil, e
	}
	url, client, e := sharing.Connect(ctx, invitation)
	if e != nil {
		return nil, e
	}
	a.mu.Lock()
	defer func() { a.mu.Unlock(); _ = a.saveJoined() }()
	for _, old := range a.joined {
		old.mu.Lock()
		match := old.Invitation.ID == invitation.ID && old.Invitation.Secret == invitation.Secret && !old.left
		old.mu.Unlock()
		if match {
			return old, nil
		}
	}
	j := &Joined{app: a, ID: "joined-" + uuid.NewString(), Invitation: invitation, URL: url, Client: client, done: make(chan struct{})}
	a.joined[j.ID] = j
	return j, nil
}
func (j *Joined) request(ctx context.Context, method, path string, in, out any) error {
	j.mu.Lock()
	if j.left || j.ended {
		ended := j.ended
		j.mu.Unlock()
		if ended {
			return fmt.Errorf("共享已结束，请向发起者获取新邀请")
		}
		return fmt.Errorf("已离开协作")
	}
	url, client, inv := j.URL, j.Client, j.Invitation
	j.mu.Unlock()
	if time.Now().After(inv.ExpiresAt) {
		return fmt.Errorf("邀请已到期")
	}
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(ctx, method, url+path, body)
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+inv.Secret)
	req.Header.Set("Content-Type", "application/json")
	res, e := client.Do(req)
	if e != nil {
		if method == "GET" {
			next, nextClient, connectErr := sharing.Connect(ctx, inv)
			if connectErr == nil {
				j.mu.Lock()
				j.URL = next
				j.Client = nextClient
				j.mu.Unlock()
				req.URL, _ = neturl.Parse(next + path)
				res, e = nextClient.Do(req)
			}
		}
		if e != nil {
			return fmt.Errorf("协作主机连接中断，请刷新后重试：%w", e)
		}
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if e != nil {
		return e
	}
	if res.StatusCode >= 400 {
		if res.StatusCode == http.StatusGone {
			j.mu.Lock()
			j.ended = true
			j.mu.Unlock()
			_ = j.app.saveJoined()
		}
		var result struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(b, &result)
		if result.Error == "" {
			result.Error = strings.TrimSpace(string(b))
		}
		return fmt.Errorf("%s", result.Error)
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}
func (j *Joined) view(ctx context.Context) map[string]any {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var out map[string]any
	e := j.request(ctx, "GET", "/v2/status", nil, &out)
	j.mu.Lock()
	defer j.mu.Unlock()
	if e != nil {
		out = map[string]any{}
		for k, v := range j.Last {
			out[k] = v
		}
		out["online"] = false
		out["error"] = e.Error()
		if j.ended || time.Now().After(j.Invitation.ExpiresAt) {
			out["state"] = "ended"
			if time.Now().After(j.Invitation.ExpiresAt) {
				out["state"] = "expired"
			}
			out["sharing"] = false
			out["writer"] = "owner"
			out["connected"] = false
			out["busy"] = false
			out["approvals"] = 0
		}
		if j.left {
			out["state"] = "left"
			out["sharing"] = false
		}
		if out["title"] == nil {
			out["title"] = j.Invitation.Title
		}
		out["host"] = j.Invitation.Host
	} else {
		j.Last = map[string]any{}
		for k, v := range out {
			j.Last[k] = v
		}
		j.Error = ""
	}
	out["remoteId"] = j.Invitation.ID
	out["id"] = j.ID
	out["role"] = "remote"
	out["transport"] = "LAN"
	out["expiresAt"] = j.Invitation.ExpiresAt
	return out
}
func (j *Joined) close() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.left {
		return
	}
	j.left = true
	if j.done != nil {
		close(j.done)
	}
	if j.server != nil {
		_ = j.server.Close()
	}
}
func (j *Joined) attach(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != "" {
		http.Error(w, "请使用原生客户端", 403)
		return
	}
	j.mu.Lock()
	if j.left {
		j.mu.Unlock()
		http.Error(w, "已离开", 410)
		return
	}
	url, client, inv := j.URL, j.Client, j.Invitation
	j.mu.Unlock()
	upstream, _, e := websocket.Dial(r.Context(), strings.Replace(url, "https:", "wss:", 1)+"/v2/connect", &websocket.DialOptions{HTTPClient: client, HTTPHeader: http.Header{"Authorization": []string{"Bearer " + inv.Secret}}})
	if e != nil {
		http.Error(w, e.Error(), 502)
		return
	}
	defer upstream.CloseNow()
	downstream, e := websocket.Accept(w, r, nil)
	if e != nil {
		return
	}
	defer downstream.CloseNow()
	upstream.SetReadLimit(32 << 20)
	downstream.SetReadLimit(32 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		select {
		case <-ctx.Done():
		case <-j.done:
		}
		_ = upstream.CloseNow()
		_ = downstream.CloseNow()
	}()
	done := make(chan struct{}, 2)
	localClient := &localClient{app: j.app, id: j.ID, emit: func(m nativecodex.Message) {
		payload, _ := json.Marshal(m)
		_ = downstream.Write(ctx, websocket.MessageText, payload)
	}}
	defer localClient.close()
	copy := func(to, from *websocket.Conn, local bool) {
		defer func() { done <- struct{}{} }()
		for {
			kind, b, e := from.Read(ctx)
			if e != nil {
				return
			}
			if local && j.app != nil {
				var message nativecodex.Message
				_ = json.Unmarshal(b, &message)
				if message.Method == "" && localClient.reply(ctx, message) {
					continue
				}
				if localClientMethod(message.Method) && len(message.ID) > 0 {
					var params map[string]any
					_ = json.Unmarshal(message.Params, &params)
					var result json.RawMessage
					e := localClient.call(ctx, message.Method, params, &result)
					reply := nativecodex.Message{ID: message.ID, Result: result}
					if e != nil {
						reply.Error, _ = json.Marshal(map[string]any{"code": -32603, "message": e.Error()})
						reply.Result = nil
					}
					payload, _ := json.Marshal(reply)
					if downstream.Write(ctx, websocket.MessageText, payload) != nil {
						return
					}
					continue
				}
			}
			if to.Write(ctx, kind, b) != nil {
				return
			}
		}
	}
	go copy(upstream, downstream, true)
	go copy(downstream, upstream, false)
	<-done
	cancel()
	_ = upstream.CloseNow()
	_ = downstream.CloseNow()
	<-done
}
func (a *App) endpoint(id string) (string, error) {
	a.mu.Lock()
	s, j := a.sessions[id], a.joined[id]
	a.mu.Unlock()
	if s != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.writer != "owner" {
			return "", fmt.Errorf("请先接回输入")
		}
		if s.endpoint != "" {
			return s.endpoint, nil
		}
		l, e := net.Listen("tcp4", "127.0.0.1:0")
		if e != nil {
			return "", e
		}
		s.listener = l
		s.endpoint = "ws://" + l.Addr().String()
		s.server = &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.attach(w, r, "owner") })}
		go func() { _ = s.server.Serve(l) }()
		return s.endpoint, nil
	}
	if j != nil {
		j.mu.Lock()
		defer j.mu.Unlock()
		if j.left {
			return "", fmt.Errorf("已离开协作")
		}
		if j.endpoint != "" {
			return j.endpoint, nil
		}
		l, e := net.Listen("tcp4", "127.0.0.1:0")
		if e != nil {
			return "", e
		}
		j.listener = l
		j.endpoint = "ws://" + l.Addr().String()
		j.server = &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(j.attach)}
		go func() { _ = j.server.Serve(l) }()
		return j.endpoint, nil
	}
	return "", fmt.Errorf("没有找到协作")
}

type joinedRecord struct {
	ID         string             `json:"id"`
	Invitation sharing.Invitation `json:"invitation"`
	URL        string             `json:"url"`
	Last       map[string]any     `json:"last,omitempty"`
	Ended      bool               `json:"ended,omitempty"`
}

func (a *App) saveJoined() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	records := []joinedRecord{}
	for _, j := range a.joined {
		j.mu.Lock()
		if !j.left {
			records = append(records, joinedRecord{ID: j.ID, Invitation: j.Invitation, URL: j.URL, Last: j.Last, Ended: j.ended})
		}
		j.mu.Unlock()
	}
	return writeJSONFile(filepath.Join(a.Config.DataDir, "joined.json"), records)
}
