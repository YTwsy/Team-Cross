package nativeclaude

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func facade() *Client {
	return &Client{JobID: "12345678", attachments: map[string]*websocket.Conn{}, connections: map[net.Conn]bool{}}
}
func TestFacadeReportsActualClientVersion(t *testing.T) {
	c := facade()
	c.version = "2.2.0"
	a, b := net.Pipe()
	defer a.Close()
	done := make(chan struct{})
	go func() { defer b.Close(); defer close(done); c.handle(b) }()
	a.SetDeadline(time.Now().Add(time.Second))
	a.Write([]byte("{\"proto\":1,\"op\":\"nudge\"}\n"))
	line, err := bufio.NewReader(a).ReadBytes('\n')
	<-done
	if err != nil || !strings.Contains(string(line), `"version":"2.2.0"`) {
		t.Fatal(string(line), err)
	}
}
func TestFacadeRejectsOtherJobsAndDaemonControls(t *testing.T) {
	c := facade()
	calls := 0
	c.dial = func(context.Context) (*websocket.Conn, error) {
		calls++
		t.Error("local request contacted upstream")
		return nil, context.Canceled
	}
	for _, request := range []string{
		`{"proto":1,"op":"has","short":"ffffffff"}`,
		`{"proto":1,"op":"reply","short":"12345678","text":"unauthorized"}`,
		`{"proto":1,"op":"stop","short":"12345678"}`,
		`{"proto":1,"op":"attach","short":"ffffffff","cols":80,"rows":24,"attachId":"x","caps":{}}`,
	} {
		a, b := net.Pipe()
		done := make(chan struct{})
		go func() { defer b.Close(); defer close(done); c.handle(b) }()
		a.SetDeadline(time.Now().Add(time.Second))
		_, _ = a.Write([]byte(request + "\n"))
		line, err := bufio.NewReader(a).ReadBytes('\n')
		a.Close()
		<-done
		if err != nil || !strings.Contains(string(line), `"ok":false`) {
			t.Fatal(string(line), err)
		}
	}
	if calls != 0 {
		t.Fatal("forwarded daemon operation")
	}
}
func TestFacadeDoesNotReplayInputAfterDisconnectedAttach(t *testing.T) {
	var connections atomic.Int32
	inputs := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		connections.Add(1)
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if _, _, err = c.Read(ctx); err != nil {
			return
		}
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"ok":true,"op":"attach"}`))
		_, packet, err := c.Read(ctx)
		if err == nil {
			inputs <- string(packet)
		}
		// Simulate a dropped input after transport receipt and before any worker ACK.
	}))
	defer server.Close()
	c := facade()
	c.dial = func(ctx context.Context) (*websocket.Conn, error) {
		up, _, e := websocket.Dial(ctx, strings.Replace(server.URL, "http:", "ws:", 1), nil)
		return up, e
	}
	for _, packet := range []string{"lost first input", "explicit new input"} {
		a, b := net.Pipe()
		done := make(chan struct{})
		go func() { defer b.Close(); defer close(done); c.handle(b) }()
		a.SetDeadline(time.Now().Add(3 * time.Second))
		request := TerminalRequest{Op: "attach", Cols: 80, Rows: 24, AttachID: packet, Caps: json.RawMessage(`{}`)}
		in := map[string]any{"proto": 1, "short": "12345678", "op": request.Op, "cols": request.Cols, "rows": request.Rows, "attachId": request.AttachID, "caps": request.Caps}
		encoded, _ := json.Marshal(in)
		_, _ = a.Write(append(encoded, '\n'))
		reader := bufio.NewReader(a)
		if _, err := reader.ReadBytes('\n'); err != nil {
			t.Fatal(err)
		}
		if _, err := a.Write([]byte(packet)); err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, reader)
		a.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("facade did not close")
		}
		select {
		case got := <-inputs:
			if got != packet {
				t.Fatal("replayed input", got)
			}
		case <-time.After(time.Second):
			t.Fatal("input never arrived")
		}
	}
	if connections.Load() != 2 {
		t.Fatal("facade retried without a native client request", connections.Load())
	}
	select {
	case extra := <-inputs:
		t.Fatal("duplicate input", extra)
	default:
	}
}

func TestFacadeRecoversOnlyItsOwnStaleSocket(t *testing.T) {
	path := shortSocket(t)
	old, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	old.(*net.UnixListener).SetUnlinkOnClose(false)
	ctx := context.Background()
	if got, err := listenClient(ctx, path, true); err == nil {
		got.Close()
		t.Fatal("replaced a live listener")
	}
	old.Close()
	if got, err := listenClient(ctx, path, false); err == nil {
		got.Close()
		t.Fatal("removed an unowned stale socket")
	}
	got, err := listenClient(ctx, path, true)
	if err != nil {
		t.Fatal("failed to recover owned socket", err)
	}
	got.Close()
}
