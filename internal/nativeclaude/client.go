package nativeclaude

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
)

// Client is a minimal local daemon facade for the unchanged native CLI. It
// implements only attach discovery and terminal resizing, not daemon management.
// The supplied dialer authenticates to the single Team Cross collaboration.
type Client struct {
	Home, Cwd, JobID string
	version          string
	socket           string
	listener         net.Listener
	fileInfo         os.FileInfo
	dial             func(context.Context) (*websocket.Conn, error)
	mu               sync.Mutex
	closed           bool
	connections      map[net.Conn]bool
	attachments      map[string]*websocket.Conn
	closeOnce        sync.Once
	wg               sync.WaitGroup
}

func StartClient(ctx context.Context, binary, home, cwd, job string, dial func(context.Context) (*websocket.Conn, error)) (*Client, error) {
	if !regexpJob.MatchString(job) {
		return nil, fmt.Errorf("无效的 Claude 后台会话 ID")
	}
	version, err := Version(ctx, binary)
	if err != nil {
		return nil, err
	}
	if err := ValidateVersion(version); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cwd, 0700); err != nil {
		return nil, err
	}
	if err := Bootstrap(home, cwd); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(home, "jobs", job), 0700); err != nil {
		return nil, err
	}
	path, err := SocketPath(ctx, Config{Binary: binary, Home: home, Cwd: cwd})
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	var previous struct {
		Job    string `json:"job"`
		Socket string `json:"socket"`
	}
	marker := filepath.Join(home, "teamcross-client.json")
	if b, e := os.ReadFile(marker); e == nil {
		_ = json.Unmarshal(b, &previous)
	}
	// An exclusive Core owns this dedicated client config. Only an unreachable
	// socket with the matching saved ownership marker can be removed after a crash.
	l, err := listenClient(ctx, path, previous.Job == job && previous.Socket == path)
	if err != nil {
		return nil, fmt.Errorf("Claude 本地连接入口已占用：%w", err)
	}
	if u, ok := l.(*net.UnixListener); ok {
		u.SetUnlinkOnClose(false)
	}
	if err = os.Chmod(path, 0600); err != nil {
		_ = l.Close()
		return nil, err
	}
	st, _ := os.Stat(path)
	if err = writeJSON(marker, map[string]string{"job": job, "socket": path}); err != nil {
		_ = l.Close()
		if current, e := os.Lstat(path); e == nil && st != nil && os.SameFile(st, current) {
			_ = os.Remove(path)
		}
		return nil, err
	}
	c := &Client{Home: home, Cwd: cwd, JobID: job, version: strings.Fields(version)[0], socket: path, listener: l, fileInfo: st, dial: dial, connections: map[net.Conn]bool{}, attachments: map[string]*websocket.Conn{}}
	c.wg.Add(1)
	go c.accept()
	return c, nil
}

func listenClient(ctx context.Context, path string, owned bool) (net.Listener, error) {
	l, err := net.Listen("unix", path)
	if err == nil || !owned {
		return l, err
	}
	st, statErr := os.Lstat(path)
	if statErr != nil || st.Mode()&os.ModeSocket == 0 {
		return nil, err
	}
	c, dialErr := (&net.Dialer{Timeout: 150 * time.Millisecond}).DialContext(ctx, "unix", path)
	if c != nil {
		_ = c.Close()
	}
	if !errors.Is(dialErr, syscall.ECONNREFUSED) {
		return nil, err
	}
	current, statErr := os.Lstat(path)
	if statErr != nil || !os.SameFile(st, current) {
		return nil, err
	}
	if e := os.Remove(path); e != nil {
		return nil, e
	}
	return net.Listen("unix", path)
}
func (c *Client) accept() {
	defer c.wg.Done()
	for {
		conn, err := c.listener.Accept()
		if err != nil {
			return
		}
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			_ = conn.Close()
			return
		}
		c.connections[conn] = true
		c.wg.Add(1)
		c.mu.Unlock()
		go func() {
			defer c.wg.Done()
			defer conn.Close()
			defer func() { c.mu.Lock(); delete(c.connections, conn); c.mu.Unlock() }()
			c.handle(conn)
		}()
	}
}
func replyLocal(c net.Conn, v any) { b, _ := json.Marshal(v); _, _ = c.Write(append(b, '\n')) }
func (c *Client) handle(conn net.Conn) {
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	reader := bufio.NewReaderSize(conn, 16<<10)
	line, err := reader.ReadSlice('\n')
	if err != nil {
		return
	}
	var in struct {
		TerminalRequest
		Proto int    `json:"proto"`
		Short string `json:"short"`
	}
	if json.Unmarshal(line, &in) != nil || in.Proto != 1 {
		return
	}
	reject := func(message string) {
		replyLocal(conn, map[string]any{"ok": false, "op": in.Op, "code": "ETCX", "error": message})
	}
	if in.Op == "nudge" {
		replyLocal(conn, map[string]any{"ok": true, "op": "nudge", "restarting": false, "upgradePending": false, "version": c.version, "processWrapper": ""})
		return
	}
	if in.Short != c.JobID {
		reject("此入口只访问指定协作")
		return
	}
	if in.Op == "has" {
		replyLocal(conn, map[string]any{"ok": true, "op": "has", "alive": true, "present": true, "ready": true})
		return
	}
	if err := in.Validate(); err != nil {
		reject(err.Error())
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if in.Op == "resize" {
		c.mu.Lock()
		up := c.attachments[in.AttachID]
		c.mu.Unlock()
		if up == nil {
			reject("终端连接已失效")
			return
		}
		b, _ := json.Marshal(in.TerminalRequest)
		ctx, stop := context.WithTimeout(ctx, 2*time.Second)
		defer stop()
		if err = up.Write(ctx, websocket.MessageText, b); err != nil {
			reject("终端尺寸发送结果不明")
			return
		}
		replyLocal(conn, map[string]any{"ok": true, "op": "resize"})
		return
	}
	ready, stop := context.WithTimeout(ctx, 15*time.Second)
	up, err := c.dial(ready)
	if err != nil {
		stop()
		reject("等待输入交接、重新连接或恢复运行时")
		return
	}
	defer up.CloseNow()
	up.SetReadLimit(1 << 20)
	request, _ := json.Marshal(in.TerminalRequest)
	if err = up.Write(ready, websocket.MessageText, request); err != nil {
		stop()
		reject("连接失败")
		return
	}
	kind, ack, err := up.Read(ready)
	stop()
	if err != nil || kind != websocket.MessageText {
		reject("Claude 连接未确认")
		return
	}
	var accepted struct {
		OK bool `json:"ok"`
	}
	if json.Unmarshal(ack, &accepted) != nil || !accepted.OK {
		_, _ = conn.Write(append(ack, '\n'))
		return
	}
	c.mu.Lock()
	if c.closed || c.attachments[in.AttachID] != nil {
		c.mu.Unlock()
		reject("终端连接已关闭或重复")
		return
	}
	c.attachments[in.AttachID] = up
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.attachments, in.AttachID); c.mu.Unlock() }()
	if _, err = conn.Write(append(ack, '\n')); err != nil {
		return
	}
	_ = conn.SetDeadline(time.Time{})
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 32<<10)
		for {
			n, e := reader.Read(buf)
			if n > 0 {
				if up.Write(ctx, websocket.MessageBinary, buf[:n]) != nil {
					return
				}
			}
			if e != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			k, b, e := up.Read(ctx)
			if e != nil {
				return
			}
			if k != websocket.MessageBinary {
				return
			}
			if _, e = conn.Write(b); e != nil {
				return
			}
		}
	}()
	<-done
	cancel()
	_ = up.CloseNow()
	_ = conn.Close()
	<-done
}
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		_ = c.listener.Close()
		for conn := range c.connections {
			_ = conn.Close()
		}
		for _, up := range c.attachments {
			_ = up.CloseNow()
		}
		c.mu.Unlock()
		c.wg.Wait()
		if st, err := os.Stat(c.socket); err == nil && c.fileInfo != nil && os.SameFile(st, c.fileInfo) {
			_ = os.Remove(c.socket)
		}
	})
}
