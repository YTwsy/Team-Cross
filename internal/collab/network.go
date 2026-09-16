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
	"teamcross/internal/problem"
	"teamcross/internal/sharing"
)

func (s *Session) endShareLocked() {
	if s.shareCancel != nil {
		s.shareCancel()
		s.shareCancel = nil
	}
	s.sharePreparing = false
	s.shareTransport = ""
	s.shareGeneration++
	if s.share != nil {
		s.share.Revoke()
		s.app.mu.Lock()
		s.app.retiredShares = append(s.app.retiredShares, s.share)
		s.app.mu.Unlock()
		s.share = nil
	}
	if s.direct != nil && s.direct.role == "remote" {
		s.direct.close()
		s.direct = nil
	}
	s.writer = "owner"
	s.inputRequested = false
	s.remoteSeen = time.Time{}
	s.epoch++
	s.releaseWhenIdle = true
	s.annotationAccess = false
}

func (s *Session) Share(ctx context.Context, requested string) error {
	transport, err := sharing.ParseTransport(requested)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.sharePreparing {
		if s.shareTransport == transport {
			s.mu.Unlock()
			return problem.New("sharing_preparing", "正在生成协作邀请", "请等待当前连接方式准备完成")
		}
		s.mu.Unlock()
		return problem.New("sharing_preparing", "另一种连接方式正在准备", "请等待完成或先结束共享")
	}
	if s.share != nil {
		state := s.share.InvitationState()
		if state == "pending" || state == "joined" {
			if s.share.Transport() == transport {
				s.mu.Unlock()
				return nil
			}
			if state == "joined" {
				s.mu.Unlock()
				return problem.New("sharing_active", "同事已经加入当前共享", "请先结束共享，再选择其他连接方式")
			}
		}
		s.endShareLocked()
	}
	if !s.online || s.record.State != "ready" {
		s.mu.Unlock()
		return fmt.Errorf("请先恢复协作运行时")
	}
	startCtx, cancel := context.WithCancel(ctx)
	s.sharePreparing = true
	s.shareTransport = transport
	s.shareCancel = cancel
	s.shareGeneration++
	generation := s.shareGeneration
	id, title := s.record.ID, s.record.Title
	mode := s.record.RuntimeMode
	host, loopback := s.app.Host, s.app.Config.Loopback
	s.mu.Unlock()

	runtime, startErr := sharing.Start(startCtx, transport, id, title, host, http.HandlerFunc(s.remoteHTTP), loopback, mode)
	cancel()

	s.mu.Lock()
	defer func() { s.releaseIfIdleLocked(); s.mu.Unlock() }()
	if generation != s.shareGeneration || s.closed {
		if runtime != nil {
			runtime.Close()
		}
		return problem.New("sharing_cancelled", "邀请生成已取消", "请重新选择连接方式")
	}
	s.sharePreparing = false
	s.shareTransport = ""
	s.shareCancel = nil
	if startErr != nil {
		return startErr
	}
	if !s.online || s.record.State != "ready" {
		runtime.Close()
		return fmt.Errorf("协作运行时已经停止，请恢复后重试")
	}
	s.share = runtime
	s.releaseWhenIdle = false
	s.annotationAccess = true
	return nil
}

func (s *Session) Action(ctx context.Context, action string, expected ...uint64) error {
	if action == "start" {
		return s.start(ctx, true)
	}
	if action == "share" {
		return s.Share(ctx, string(sharing.TransportLAN))
	}
	s.mu.Lock()
	defer func() { s.releaseIfIdleLocked(); s.mu.Unlock() }()
	if len(expected) > 0 && (action == "handoff" || action == "reclaim") && expected[0] != s.epoch {
		return fmt.Errorf("输入状态已变化，请刷新后重试")
	}
	switch action {
	case "end":
		s.endShareLocked()
	case "handoff":
		if s.share == nil {
			return fmt.Errorf("请先创建邀请")
		}
		if s.share.InvitationState() != "joined" {
			return problem.New("participant_required", "同事尚未加入协作", "请等待同事加入后交出输入")
		}
		if !s.online || s.writer != "owner" {
			return problem.New("input_changed", "当前无法交出输入", "请刷新协作状态")
		}
		if s.busy {
			return fmt.Errorf("当前轮正在运行，请完成后交出输入")
		}
		if s.direct != nil {
			s.direct.close()
			s.direct = nil
		}
		s.writer = "remote"
		s.inputRequested = false
		s.epoch++
	case "reclaim":
		if s.direct != nil {
			s.direct.close()
			s.direct = nil
		}
		s.writer = "owner"
		s.inputRequested = false
		s.epoch++
	default:
		return fmt.Errorf("未知协作操作")
	}
	return nil
}
func (s *Session) remoteHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	allowed := s.share != nil && sharing.Authorized(r.Context(), s.share)
	s.mu.Unlock()
	if !allowed {
		http.Error(w, "共享已结束", http.StatusGone)
		return
	}
	if r.Method == "POST" && r.URL.Path == "/v2/leave" {
		s.mu.Lock()
		ok := s.share != nil && s.share.Leave(r.Context())
		if ok {
			if s.direct != nil && s.direct.role == "remote" {
				s.direct.close()
				s.direct = nil
			}
			s.writer, s.inputRequested, s.remoteSeen = "owner", false, time.Time{}
			s.epoch++
		}
		s.mu.Unlock()
		respond(w, map[string]bool{"ok": ok}, nil)
		return
	}
	if r.Method == "POST" && (r.URL.Path == "/v2/presence" || r.URL.Path == "/v2/request_input" || r.URL.Path == "/v2/cancel_input") {
		var in struct {
			Online bool   `json:"online"`
			Epoch  uint64 `json:"epoch"`
		}
		if !decode(w, r, &in) {
			return
		}
		s.mu.Lock()
		var e error
		if s.share == nil || !sharing.Authorized(r.Context(), s.share) {
			e = problem.New("sharing_ended", "共享已结束", "请获取新邀请")
		} else if r.URL.Path == "/v2/presence" {
			if in.Online {
				if time.Since(s.remoteSeen) > 30*time.Second {
					s.inputRequested = false
				}
				s.remoteSeen = time.Now()
			} else {
				s.remoteSeen = time.Time{}
				s.inputRequested = false
			}
		} else if in.Epoch != s.epoch || s.writer != "owner" {
			e = problem.New("input_changed", "输入归属已变化", "请刷新后重试")
		} else {
			s.inputRequested = r.URL.Path == "/v2/request_input" && s.writer != "remote"
			s.remoteSeen = time.Now()
		}
		s.mu.Unlock()
		respond(w, map[string]bool{"ok": e == nil}, e)
		return
	}

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
		if !sharing.Authorized(r.Context(), s.share) || in.Epoch != s.epoch || s.writer != "remote" {
			e = fmt.Errorf("输入状态已变化，请刷新")
		} else if s.busy {
			e = fmt.Errorf("请等待当前轮完成后交还输入")
		} else {
			if s.direct != nil {
				s.direct.close()
				s.direct = nil
			}
			s.writer = "owner"
			s.inputRequested = false
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
		out, e := s.annotate(in, "协作者", r.Context())
		respond(w, out, e)
		return
	}
	if r.Method == "POST" && r.URL.Path == "/v2/annotation-replies" {
		var in AnnotationReplyInput
		if !decodeAnnotationReply(w, r, &in) {
			return
		}
		out, e := s.replyAnnotation(r.Context(), in, "协作者")
		respond(w, out, e)
		return
	}
	http.NotFound(w, r)
}
func (a *App) Join(ctx context.Context, token string) (*Joined, error) {
	// Serialize local admission so two clicks cannot consume one invitation with
	// different credentials. Network calls never hold App.mu.
	a.joinMu.Lock()
	defer a.joinMu.Unlock()
	invitation, decodeErr := sharing.Decode(token)
	if decodeErr != nil && problem.Describe(decodeErr).Code != "invitation_expired" {
		return nil, decodeErr
	}
	a.mu.Lock()
	var j *Joined
	for _, old := range a.joined {
		old.mu.Lock()
		match := old.Invitation.ID == invitation.ID && old.Invitation.Secret == invitation.Secret && !old.left && old.Credential != ""
		old.mu.Unlock()
		if match {
			j = old
			break
		}
	}
	a.mu.Unlock()
	if j != nil {
		j.mu.Lock()
		confirmed, ended := j.confirmed, j.ended
		j.mu.Unlock()
		if ended {
			return nil, problem.New("sharing_ended", "共享已结束", "请获取新邀请")
		}
		if confirmed {
			j.startHeartbeat()
			return j, nil
		}
		// Recovery is a read, never automatic replay of an admission or input.
		if e := j.request(ctx, "GET", "/v2/status", nil, nil); e == nil {
			j.mu.Lock()
			j.confirmed = true
			j.mu.Unlock()
			if e = a.saveJoined(); e != nil {
				return nil, e
			}
			j.startHeartbeat()
			return j, nil
		} else if code := problem.Describe(e).Code; code != "membership_invalid" {
			return nil, e
		}
	}
	if decodeErr != nil {
		return nil, decodeErr
	}
	connection, e := sharing.Connect(ctx, invitation)
	if e != nil {
		return nil, e
	}
	if j == nil {
		j = &Joined{app: a, ID: "joined-" + uuid.NewString(), Invitation: invitation, Credential: sharing.NewCredential(), URL: connection.URL, Client: connection.Client, Connection: connection, done: make(chan struct{})}
		a.mu.Lock()
		a.joined[j.ID] = j
		a.mu.Unlock()
	} else {
		j.mu.Lock()
		previous := j.Connection
		j.URL, j.Client, j.Connection = connection.URL, connection.Client, connection
		j.mu.Unlock()
		_ = previous.Close()
	}
	// Persist before the host accepts us; a lost response or B Core restart
	// retains the only credential capable of recovering this membership.
	if e = a.saveJoined(); e != nil {
		return nil, e
	}
	if e = j.request(ctx, "POST", "/v2/join", map[string]string{"credential": j.Credential}, nil); e != nil {
		return nil, e
	}
	j.mu.Lock()
	j.confirmed = true
	j.mu.Unlock()
	if e = a.saveJoined(); e != nil {
		return nil, e
	}
	j.startHeartbeat()
	return j, nil
}
func (j *Joined) request(ctx context.Context, method, path string, in, out any) error {
	j.mu.Lock()
	if j.left || j.ended || j.closed {
		ended := j.ended
		j.mu.Unlock()
		if ended {
			return fmt.Errorf("共享已结束，请向发起者获取新邀请")
		}
		return fmt.Errorf("已离开协作")
	}
	url, client, inv, credential := j.URL, j.Client, j.Invitation, j.Credential
	j.mu.Unlock()
	if url == "" || client == nil {
		return problem.New("host_unreachable", "协作连接尚未准备", "请重新连接")
	}
	if path == "/v2/join" {
		credential = inv.Secret
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
	req.Header.Set("Authorization", "Bearer "+credential)
	req.Header.Set("Content-Type", "application/json")
	res, e := client.Do(req)
	if e != nil {
		// LAN invitations carry multiple concrete endpoints, so a failed read
		// can safely select another one. Tailcat has one logical address and its
		// client owns network recovery; replacing that client here could tear
		// down an unrelated live WebSocket using the same transport.
		if method == "GET" && inv.Transport == sharing.TransportLAN {
			next, connectErr := sharing.Connect(ctx, inv, credential)
			if connectErr != nil && problem.Describe(connectErr).Code != "host_unreachable" {
				if problem.Describe(connectErr).Code == "sharing_ended" {
					j.markEnded()
				}
				return connectErr
			}
			if connectErr == nil {
				j.mu.Lock()
				previous := j.Connection
				j.URL = next.URL
				j.Client = next.Client
				j.Connection = next
				j.mu.Unlock()
				_ = previous.Close()
				req.URL, _ = neturl.Parse(next.URL + path)
				res, e = next.Client.Do(req)
			}
		}
		if e != nil {
			return problem.New("host_unreachable", "协作主机连接中断", "请重新连接；状态不明的写入不会自动重发")
		}
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if e != nil {
		return e
	}
	if res.StatusCode >= 400 {
		if res.StatusCode == http.StatusGone && path != "/v2/join" {
			j.markEnded()
		}
		var result struct {
			Error    string `json:"error"`
			Code     string `json:"code"`
			Recovery string `json:"recovery"`
		}
		_ = json.Unmarshal(b, &result)
		if result.Error == "" {
			result.Error = strings.TrimSpace(string(b))
		}
		if result.Code == "" {
			result.Code = "remote_error"
			if res.StatusCode == 410 {
				result.Code = "sharing_ended"
			}
		}
		return problem.New(result.Code, result.Error, result.Recovery)
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}
func (j *Joined) view(ctx context.Context) map[string]any {
	timeout := 4 * time.Second
	if j.Invitation.Transport == sharing.TransportTailcat {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var fresh map[string]any
	e := j.request(ctx, "GET", "/v2/status", nil, &fresh)
	j.mu.Lock()
	recovered := false
	if e != nil {
		j.Error = e.Error()
	} else {
		if !j.confirmed && !j.closed {
			j.confirmed, recovered = true, true
		}
		j.Last = map[string]any{}
		for k, v := range fresh {
			j.Last[k] = v
		}
		j.Error = ""
	}
	j.statusChecked = true
	out := j.cachedViewLocked()
	j.mu.Unlock()
	if recovered {
		_ = j.app.saveJoined()
		j.startHeartbeat()
	}
	return out
}

func (j *Joined) cachedView() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.cachedViewLocked()
}

func (j *Joined) cachedViewLocked() map[string]any {
	out := map[string]any{}
	for k, v := range j.Last {
		out[k] = v
	}
	if !j.statusChecked || j.Error != "" {
		out["online"] = false
		out["runtimeState"] = "offline"
		out["releasePending"] = false
	}
	if j.Error != "" {
		out["error"] = j.Error
	}
	if !j.confirmed {
		out["state"] = "joining"
	}
	if j.ended {
		out["state"] = "ended"
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
	if out["host"] == nil {
		out["host"] = j.Invitation.Host
	}
	out["remoteId"] = j.Invitation.ID
	out["id"] = j.ID
	out["role"] = "remote"
	transport := j.Invitation.Transport
	if transport == "" {
		transport = sharing.TransportLAN
	}
	out["transport"] = string(transport)
	delete(out, "expiresAt")
	return out
}

func (j *Joined) refreshView() {
	j.mu.Lock()
	if j.refreshing || j.left || j.ended || j.closed {
		j.mu.Unlock()
		return
	}
	j.refreshing = true
	j.mu.Unlock()
	go func() {
		_ = j.view(context.Background())
		j.mu.Lock()
		j.refreshing = false
		j.mu.Unlock()
	}()
}
func (j *Joined) stopLocked() {
	j.closed = true
	j.closeOnce.Do(func() {
		if j.done != nil {
			close(j.done)
		}
	})
	if j.server != nil {
		_ = j.server.Close()
	}
	if j.Connection != nil {
		_ = j.Connection.Close()
		j.Connection = nil
	}
}
func (j *Joined) markEnded() {
	j.mu.Lock()
	j.ended = true
	j.stopLocked()
	j.mu.Unlock()
	_ = j.app.saveJoined()
}

// Core shutdown only disconnects; it does not revoke membership on A.
func (j *Joined) close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	_ = j.request(ctx, "POST", "/v2/presence", map[string]bool{"online": false}, nil)
	cancel()
	j.mu.Lock()
	j.stopLocked()
	j.mu.Unlock()
}
func (j *Joined) leave(ctx context.Context) error {
	j.mu.Lock()
	ended, left := j.ended, j.left
	j.mu.Unlock()
	if !ended && !left {
		if e := j.request(ctx, "POST", "/v2/leave", nil, nil); e != nil {
			code := problem.Describe(e).Code
			if code != "sharing_ended" && code != "membership_invalid" {
				return e
			}
		}
	}
	j.mu.Lock()
	j.left = true
	j.stopLocked()
	j.mu.Unlock()
	return j.app.saveJoined()
}
func (j *Joined) attach(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != "" {
		http.Error(w, "请使用原生客户端", 403)
		return
	}
	j.mu.Lock()
	if j.left || j.ended || j.closed || !j.confirmed {
		j.mu.Unlock()
		http.Error(w, "已离开", 410)
		return
	}
	url, client, credential := j.URL, j.Client, j.Credential
	j.mu.Unlock()
	upstream, _, e := websocket.Dial(r.Context(), strings.Replace(url, "https:", "wss:", 1)+"/v2/connect", &websocket.DialOptions{HTTPClient: client, HTTPHeader: http.Header{"Authorization": []string{"Bearer " + credential}}})
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
				var params map[string]any
				_ = json.Unmarshal(message.Params, &params)
				if localClientRequest(message.Method, params) && len(message.ID) > 0 {
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
		if j.left || j.ended || j.closed || !j.confirmed {
			return "", fmt.Errorf("请先加入有效的协作")
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
	Credential string             `json:"credential"`
	Confirmed  bool               `json:"confirmed"`
}

func (a *App) saveJoined() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	records := []joinedRecord{}
	for _, j := range a.joined {
		j.mu.Lock()
		if !j.left {
			records = append(records, joinedRecord{ID: j.ID, Invitation: j.Invitation, URL: j.URL, Last: j.Last, Ended: j.ended, Credential: j.Credential, Confirmed: j.confirmed})
		}
		j.mu.Unlock()
	}
	return writeJSONFile(filepath.Join(a.Config.DataDir, "joined.json"), records)
}

func (j *Joined) startHeartbeat() {
	j.mu.Lock()
	confirmed := j.confirmed
	j.mu.Unlock()
	if !confirmed {
		return
	}
	j.heartbeatOnce.Do(func() {
		go func() {
			tick := time.NewTicker(10 * time.Second)
			defer tick.Stop()
			for {
				j.mu.Lock()
				finished := j.left || j.ended || j.closed || !j.confirmed
				j.mu.Unlock()
				if finished {
					return
				}
				timeout := 2 * time.Second
				if j.Invitation.Transport == sharing.TransportTailcat {
					timeout = 8 * time.Second
				}
				ctx, cancel := context.WithTimeout(context.Background(), timeout)
				_ = j.request(ctx, "POST", "/v2/presence", map[string]bool{"online": true}, nil)
				cancel()
				select {
				case <-j.done:
					return
				case <-tick.C:
				}
			}
		}()
	})
}
