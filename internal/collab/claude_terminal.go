package collab

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"teamcross/internal/nativeclaude"
)

type claudeTerminal interface {
	Runtime
	Attach(context.Context, nativeclaude.TerminalRequest) (net.Conn, json.RawMessage, error)
	Resize(context.Context, nativeclaude.TerminalRequest) error
}

func (s *Session) attachClaude(w http.ResponseWriter, r *http.Request, role string) {
	if r.Header.Get("Origin") != "" {
		http.Error(w, "请使用原生客户端连接", 403)
		return
	}
	s.mu.Lock()
	p, ok := s.process.(claudeTerminal)
	if !ok || !s.callerValidLocked(r.Context()) || s.writer != role || !s.online || s.starting || s.record.State != "ready" || (role != "owner" && (s.share == nil || !s.share.HasMember(role))) {
		s.mu.Unlock()
		http.Error(w, "等待输入交接或恢复运行时", 403)
		return
	}
	if s.direct != nil {
		s.mu.Unlock()
		http.Error(w, "请先关闭已有直接操作客户端", 409)
		return
	}
	d := &direct{role: role, kind: "Claude Code TUI", done: make(chan struct{})}
	s.direct = d
	epoch := s.epoch
	if s.share == nil {
		s.releaseWhenIdle = true
	}
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
	conn.SetReadLimit(64 << 10)
	ctx, cancel := context.WithCancel(context.WithValue(r.Context(), directKey{}, d))
	defer cancel()
	go func() {
		select {
		case <-d.done:
		case <-ctx.Done():
		}
		cancel()
		_ = conn.CloseNow()
	}()
	ready, stop := context.WithTimeout(ctx, 15*time.Second)
	kind, b, err := conn.Read(ready)
	var in nativeclaude.TerminalRequest
	if err != nil || kind != websocket.MessageText || json.Unmarshal(b, &in) != nil || in.Op != "attach" || in.Validate() != nil {
		stop()
		return
	}
	up, ack, err := p.Attach(ready, in)
	stop()
	if err != nil {
		response, _ := json.Marshal(map[string]any{"ok": false, "op": "attach", "code": "ETCX", "error": err.Error()})
		_ = conn.Write(ctx, websocket.MessageText, response)
		return
	}
	defer up.Close()
	go func() { <-ctx.Done(); _ = up.Close() }()
	s.mu.Lock()
	valid := s.direct == d && s.epoch == epoch && s.writer == role && s.callerValidLocked(ctx)
	s.mu.Unlock()
	if !valid {
		return
	}
	if conn.Write(ctx, websocket.MessageText, ack) != nil {
		return
	}
	var handshake struct {
		Nonce string `json:"imarkNonce"`
	}
	_ = json.Unmarshal(ack, &handshake)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		buf := make([]byte, 32<<10)
		paint := []byte{}
		painted := false
		for {
			n, err := up.Read(buf)
			if n > 0 {
				if !painted {
					paint = append(paint, buf[:n]...)
					if hasContentPaint(paint, handshake.Nonce) {
						s.mu.Lock()
						if s.direct == d {
							d.ready = true
						}
						s.mu.Unlock()
						painted = true
						paint = nil
					}
					if len(paint) > 64<<10 {
						paint = append([]byte{}, paint[len(paint)-(32<<10):]...)
					}
				}
				if conn.Write(ctx, websocket.MessageBinary, buf[:n]) != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	for {
		kind, b, err = conn.Read(ctx)
		if err != nil {
			break
		}
		s.mu.Lock()
		valid = s.direct == d && s.epoch == epoch && s.writer == role && s.process == p && s.online && s.callerValidLocked(ctx)
		if !valid {
			s.mu.Unlock()
			break
		}
		// Ownership cannot change between checking this packet and forwarding it.
		// A bounded write prevents a stuck terminal from holding Session.mu forever.
		if kind == websocket.MessageBinary {
			s.nativeLastWrite = time.Now()
			_ = up.SetWriteDeadline(time.Now().Add(2 * time.Second))
			_, err = up.Write(b)
			_ = up.SetWriteDeadline(time.Time{})
		} else if kind == websocket.MessageText {
			var resize nativeclaude.TerminalRequest
			if json.Unmarshal(b, &resize) != nil || resize.Op != "resize" || resize.AttachID != in.AttachID || resize.Validate() != nil {
				s.mu.Unlock()
				break
			}
			resizeCtx, finish := context.WithTimeout(ctx, 2*time.Second)
			err = p.Resize(resizeCtx, resize)
			finish()
		} else {
			s.mu.Unlock()
			break
		}
		s.mu.Unlock()
		if err != nil {
			break
		}
	}
	cancel()
	_ = up.Close()
	_ = conn.CloseNow()
	<-done
}

// The nonce comes from A's native attach acknowledgement. A terminal connection
// alone does not mean the worker has rendered the shared conversation.
func hasContentPaint(data []byte, nonce string) bool {
	if nonce == "" {
		return false
	}
	prefix := []byte("\x1b_cc-d-imark;")
	for {
		start := bytes.Index(data, prefix)
		if start < 0 {
			return false
		}
		data = data[start+len(prefix):]
		end := bytes.Index(data, []byte("\x1b\\"))
		if end < 0 {
			return false
		}
		var event struct {
			Kind   string `json:"kind"`
			Nonce  string `json:"nonce"`
			Loaded int    `json:"msgsLoaded"`
		}
		if json.Unmarshal(data[:end], &event) == nil && event.Kind == "content_paint" && event.Nonce == nonce && event.Loaded > 0 {
			return true
		}
		data = data[end+2:]
	}
}
