package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"teamcross/internal/nativeclaude"
	"teamcross/internal/problem"
)

type terminalFixture struct {
	*fakeRuntime
	terminalMu sync.Mutex
	peers      []net.Conn
	input      chan string
	gate       chan struct{}
	entered    chan struct{}
}

func (p *terminalFixture) Attach(ctx context.Context, _ nativeclaude.TerminalRequest) (net.Conn, json.RawMessage, error) {
	if p.entered != nil {
		close(p.entered)
	}
	if p.gate != nil {
		select {
		case <-p.gate:
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	a, b := net.Pipe()
	p.terminalMu.Lock()
	p.peers = append(p.peers, b)
	p.terminalMu.Unlock()
	go func() {
		defer b.Close()
		buf := make([]byte, 32768)
		for {
			n, e := b.Read(buf)
			if n > 0 {
				p.input <- string(buf[:n])
			}
			if e != nil {
				return
			}
		}
	}()
	return a, json.RawMessage(`{"ok":true,"op":"attach","imarkNonce":"fixture-nonce"}`), nil
}
func (p *terminalFixture) Resize(_ context.Context, r nativeclaude.TerminalRequest) error {
	p.input <- fmt.Sprintf("resize:%dx%d", r.Cols, r.Rows)
	return nil
}
func claudeFixture(t *testing.T) (*App, *Session, *terminalFixture) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	p := &terminalFixture{fakeRuntime: f, input: make(chan string, 20)}
	s.mu.Lock()
	s.record.Provider = "claude"
	s.record.NativeJobID = "12345678"
	s.process = p
	s.mu.Unlock()
	t.Cleanup(func() {
		p.terminalMu.Lock()
		defer p.terminalMu.Unlock()
		for _, c := range p.peers {
			c.Close()
		}
	})
	return a, s, p
}
func terminalHandshake(t *testing.T, c *websocket.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, []byte(`{"op":"attach","cols":80,"rows":24,"attachId":"test","caps":{}}`)); err != nil {
		t.Fatal(err)
	}
	k, b, e := c.Read(ctx)
	if e != nil || k != websocket.MessageText || !json.Valid(b) {
		t.Fatal("handshake", string(b), e)
	}
}
func TestClaudeTLSHandoffFencesStaleInputAndKeepsWorker(t *testing.T) {
	a, s, p := claudeFixture(t)
	b, _, _ := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e := s.Action(ctx, "share"); e != nil {
		t.Fatal(e)
	}
	j, e := b.Join(ctx, s.share.Token())
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Action(ctx, "handoff"); e != nil {
		t.Fatal(e)
	}
	remote, e := b.dialClaude(ctx, j.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer remote.CloseNow()
	terminalHandshake(t, remote)
	if s.view()["clientState"] != "connected" {
		t.Fatal("reported history ready before paint")
	}
	if e = remote.Write(ctx, websocket.MessageBinary, []byte("B input")); e != nil {
		t.Fatal(e)
	}
	select {
	case got := <-p.input:
		if got != "B input" {
			t.Fatal(got)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, e = a.dialClaude(ctx, s.record.ID); e == nil {
		t.Fatal("A attached while B owns input")
	}
	if e = s.Action(ctx, "reclaim"); e != nil {
		t.Fatal(e)
	}
	_ = remote.Write(ctx, websocket.MessageBinary, []byte("STALE APPROVAL"))
	owner, e := a.dialClaude(ctx, s.record.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer owner.CloseNow()
	terminalHandshake(t, owner)
	if e = owner.Write(ctx, websocket.MessageBinary, []byte("A approval")); e != nil {
		t.Fatal(e)
	}
	select {
	case got := <-p.input:
		if got != "A approval" {
			t.Fatal("old packet reached worker", got)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if e = s.Action(ctx, "end"); e != nil {
		t.Fatal(e)
	}
	if _, e = b.dialClaude(ctx, j.ID); e == nil {
		t.Fatal("ended member reattached")
	}
	s.mu.Lock()
	same := s.process == p
	s.mu.Unlock()
	if !same || !p.Alive() {
		t.Fatal("handoff replaced worker")
	}
}
func TestClaudeReclaimCancelsPendingHandshake(t *testing.T) {
	a, s, p := claudeFixture(t)
	p.entered = make(chan struct{})
	p.gate = make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := s.Action(ctx, "share"); e != nil {
		t.Fatal(e)
	}
	c, e := a.dialClaude(ctx, s.record.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer c.CloseNow()
	_ = c.Write(ctx, websocket.MessageText, []byte(`{"op":"attach","cols":80,"rows":24,"attachId":"pending","caps":{}}`))
	select {
	case <-p.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if e = s.Action(ctx, "reclaim"); e != nil {
		t.Fatal(e)
	}
	if _, _, e = c.Read(ctx); e == nil {
		t.Fatal("old handshake survived epoch change")
	}
	p.terminalMu.Lock()
	n := len(p.peers)
	p.terminalMu.Unlock()
	if n != 0 {
		t.Fatal("cancelled handshake opened worker")
	}
}
func TestClaudeTerminalReadinessAndBrowserOrigin(t *testing.T) {
	a, s, p := claudeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := s.Action(ctx, "share"); e != nil {
		t.Fatal(e)
	}
	url, e := a.endpoint(s.record.ID)
	if e != nil {
		t.Fatal(e)
	}
	c, _, e := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"http://localhost"}}})
	if e == nil {
		c.CloseNow()
		t.Fatal("browser obtained terminal")
	}
	c, e = a.dialClaude(ctx, s.record.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer c.CloseNow()
	terminalHandshake(t, c)
	p.terminalMu.Lock()
	peer := p.peers[0]
	p.terminalMu.Unlock()
	go func() {
		_, _ = peer.Write([]byte("\x1b_cc-d-imark;{\"kind\":\"content_paint\",\"nonce\":\"fixture-nonce\",\"msgsLoaded\":3}\x1b\\"))
	}()
	if k, _, e := c.Read(ctx); e != nil || k != websocket.MessageBinary {
		t.Fatal(e)
	}
	eventually(t, func() bool { return s.view()["clientState"] == "session_ready" })
	if _, e = a.dialClaude(ctx, s.record.ID); e == nil {
		t.Fatal("second direct client admitted")
	}
}
func TestClaudeCapabilitiesRejectUnavailableRPCBeforeSending(t *testing.T) {
	_, s, p := claudeFixture(t)
	ctx := context.Background()
	for _, method := range []string{"turn/steer", "turn/interrupt", "thread/settings/update", "getAuthStatus", "thread/resume"} {
		_, err := s.RPC(ctx, "owner", method, nil, "unsupported")
		if problem.Describe(err).Code != "native_client_required" {
			t.Fatal(method, err)
		}
	}
	if err := s.Respond(ctx, "owner", json.RawMessage(`"request"`), true); problem.Describe(err).Code != "native_client_required" {
		t.Fatal(err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, method := range p.calls {
		if method == "turn/interrupt" || method == "turn/steer" || method == "thread/settings/update" {
			t.Fatal("unsupported request reached runtime")
		}
	}
	if got := s.view()["capabilities"].(map[string]bool); got["respondToRequest"] || got["nativeDesktop"] || !got["nativeTui"] {
		t.Fatal(got)
	}
}
func TestContentPaintRequiresNativeNonceAndLoadedMessages(t *testing.T) {
	for _, body := range []string{`{"kind":"content_paint","nonce":"other","msgsLoaded":3}`, `{"kind":"content_paint","nonce":"n","msgsLoaded":0}`, `{"kind":"prompt_idle","nonce":"n","msgsLoaded":3}`} {
		if hasContentPaint([]byte("\x1b_cc-d-imark;"+body+"\x1b\\"), "n") {
			t.Fatal(body)
		}
	}
}

type uncertainClaudeRuntime struct {
	*fakeRuntime
	writes int
}

func (p *uncertainClaudeRuntime) Call(ctx context.Context, method string, in, out any) error {
	if method == "turn/start" {
		p.writes++
		return context.DeadlineExceeded
	}
	return p.fakeRuntime.Call(ctx, method, in, out)
}
func TestClaudeUncertainSendIsNotReplayed(t *testing.T) {
	_, s, f := claudeFixture(t)
	p := &uncertainClaudeRuntime{fakeRuntime: f.fakeRuntime}
	s.mu.Lock()
	s.process = p
	s.mu.Unlock()
	ctx := context.Background()
	input := func() map[string]any {
		return map[string]any{"input": []any{map[string]any{"type": "text", "text": "one selected input"}}}
	}
	if _, err := s.RPC(ctx, "owner", "turn/start", input(), "same-request"); err == nil {
		t.Fatal("unacknowledged send succeeded")
	}
	if _, err := s.RPC(ctx, "owner", "turn/start", input(), "same-request"); err == nil {
		t.Fatal("replayed uncertain send")
	}
	if p.writes != 1 {
		t.Fatal("multiple writes", p.writes)
	}
	s.mu.Lock()
	state := s.record.Commands["same-request"].State
	s.mu.Unlock()
	if state != "unknown" {
		t.Fatal(state)
	}
}
