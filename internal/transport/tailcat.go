package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/tailscale/tailcat"
)

const TailcatLibraryVersion = "v0.4.0"

type TailcatHost struct {
	server      *tailcat.Server
	virtualPort uint16
	closeOnce   sync.Once
	closeErr    error
}

// StartTailcatHost prewarms an ephemeral Tailcat listener. Start currently has
// no context parameter upstream, so cancellation arranges cleanup if a late
// start eventually succeeds.
func StartTailcatHost(ctx context.Context, virtualPort uint16, handler func(net.Conn)) (*TailcatHost, error) {
	if virtualPort == 0 {
		return nil, errors.New("tailcat virtual port is required")
	}
	if handler == nil {
		return nil, errors.New("tailcat connection handler is required")
	}
	server := &tailcat.Server{
		OnTCP: func(port uint16) func(net.Conn) {
			if port != virtualPort {
				return nil
			}
			return handler
		},
	}
	started := make(chan error, 1)
	go func() { started <- server.Start() }()
	select {
	case err := <-started:
		if err != nil {
			return nil, fmt.Errorf("start tailcat host: %w", err)
		}
		return &TailcatHost{server: server, virtualPort: virtualPort}, nil
	case <-ctx.Done():
		go func() {
			if err := <-started; err == nil {
				_ = server.Close()
			}
		}()
		return nil, ctx.Err()
	}
}

func (host *TailcatHost) ConnBlob() string {
	if host == nil || host.server == nil {
		return ""
	}
	return string(host.server.ConnBlob())
}

func (host *TailcatHost) VirtualPort() uint16 {
	if host == nil {
		return 0
	}
	return host.virtualPort
}

func (host *TailcatHost) Close() error {
	if host == nil || host.server == nil {
		return nil
	}
	host.closeOnce.Do(func() { host.closeErr = host.server.Close() })
	return host.closeErr
}

type TailcatConnector struct {
	client *tailcat.Client
	port   uint16
	once   sync.Once
	err    error
}

func NewTailcatConnector(connBlob string, virtualPort uint16) (*TailcatConnector, error) {
	if connBlob == "" || virtualPort == 0 {
		return nil, errors.New("incomplete tailcat connection candidate")
	}
	return &TailcatConnector{
		client: tailcat.NewClient(tailcat.ConnBlob(connBlob)),
		port:   virtualPort,
	}, nil
}

func (connector *TailcatConnector) Dial(ctx context.Context) (net.Conn, error) {
	return connector.client.DialTCPPort(ctx, connector.port)
}

func (connector *TailcatConnector) Close() error {
	connector.once.Do(func() { connector.err = connector.client.Close() })
	return connector.err
}
