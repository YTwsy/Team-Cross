package sharing

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/tailscale/tailcat"
	"tailscale.com/wgengine/filter"
)

const (
	TailcatLibraryVersion = "v0.6.0"
	TailcatVirtualPort    = uint16(443)
	tailcatStartTimeout   = 30 * time.Second
)

func validateTailcatAddress(address string) error {
	_, err := tailcat.ParseAddr(tailcat.Addr(address))
	return err
}

func newTailcatServer(listener *connectionListener) *tailcat.Server {
	return &tailcat.Server{
		Logf: func(string, ...any) {},
		ServedTCPPorts: []filter.PortRange{{
			First: TailcatVirtualPort,
			Last:  TailcatVirtualPort,
		}},
		OnTCP: func(port uint16) func(net.Conn) {
			if port != TailcatVirtualPort {
				return nil
			}
			return listener.Handle
		},
	}
}

func startTailcatHost(ctx context.Context, listener *connectionListener) (*tailcat.Server, string, error) {
	server := newTailcatServer(listener)
	started := make(chan error, 1)
	go func() { started <- server.Start() }()
	bounded, cancel := context.WithTimeout(ctx, tailcatStartTimeout)
	defer cancel()
	select {
	case err := <-started:
		if err != nil {
			return nil, "", fmt.Errorf("建立 Tailcat 跨网络通道失败：%w", err)
		}
		if err = bounded.Err(); err != nil {
			_ = server.Close()
			return nil, "", err
		}
		return server, string(server.TailcatAddr()), nil
	case <-bounded.Done():
		go func() {
			if err := <-started; err == nil {
				_ = server.Close()
			}
		}()
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		return nil, "", fmt.Errorf("建立 Tailcat 跨网络通道超时，请检查 DERP 网络后重试")
	}
}

// connectionListener turns Tailcat's per-stream callback into the listener
// expected by tls.Listener and http.Server.
type connectionListener struct {
	connections chan net.Conn
	mu          sync.Mutex
	closed      bool
	closeOnce   sync.Once
}

func newConnectionListener() *connectionListener {
	return &connectionListener{connections: make(chan net.Conn, 16)}
}

func (l *connectionListener) Handle(connection net.Conn) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		_ = connection.Close()
		return
	}
	select {
	case l.connections <- connection:
	default:
		// Bound unaccepted streams rather than letting Tailcat callbacks pile up.
		_ = connection.Close()
	}
}

func (l *connectionListener) Accept() (net.Conn, error) {
	connection, ok := <-l.connections
	if !ok {
		return nil, net.ErrClosed
	}
	return connection, nil
}

func (l *connectionListener) Close() error {
	l.closeOnce.Do(func() {
		l.mu.Lock()
		l.closed = true
		close(l.connections)
		l.mu.Unlock()
		for connection := range l.connections {
			_ = connection.Close()
		}
	})
	return nil
}

func (l *connectionListener) Addr() net.Addr { return tailcatListenerAddr{} }

type tailcatListenerAddr struct{}

func (tailcatListenerAddr) Network() string { return "tailcat" }
func (tailcatListenerAddr) String() string  { return "tailcat:443" }
