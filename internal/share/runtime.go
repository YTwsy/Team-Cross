package share

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"teamcross/internal/invite"
	"teamcross/internal/transport"
)

const (
	DefaultTTL      = time.Hour
	MaximumTTL      = 24 * time.Hour
	TailcatHTTPPort = uint16(443)
)

var DefaultCapabilities = []string{"view", "annotate", "send", "steer", "interrupt"}

var ErrTailcatPrewarm = errors.New("Tailcat prewarm failed")

type RuntimeConfig struct {
	ShareID           string
	Secret            []byte
	ExpiresAt         time.Time
	Capabilities      []string
	Handler           http.Handler
	EnableMDNS        bool
	EnableTailcat     bool
	TailcatTimeout    time.Duration
	Tailnet           transport.TailnetStatusProvider
	ExtraLANEndpoints []string
	Now               func() time.Time
}

// Runtime owns every ephemeral resource for exactly one share. Nothing here
// exposes the local admin listener or any other Thread.
type Runtime struct {
	invitation invite.InvitationV1
	listener   net.Listener
	server     *http.Server
	mdns       io.Closer
	tailcat    *transport.TailcatHost
	serveDone  chan error
	warnings   []string
	revoked    atomic.Bool
	closeOnce  sync.Once
	closeErr   error
}

func Start(ctx context.Context, config RuntimeConfig) (*Runtime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	nowFunc := time.Now
	if config.Now != nil {
		nowFunc = config.Now
	}
	now := nowFunc()
	if config.ExpiresAt.IsZero() {
		config.ExpiresAt = now.Add(DefaultTTL)
	}
	if !config.ExpiresAt.After(now) || config.ExpiresAt.After(now.Add(MaximumTTL)) {
		return nil, fmt.Errorf("share expiry must be within %s", MaximumTTL)
	}
	// InvitationV1 carries whole Unix seconds. Use that same instant for the
	// gate and certificate so the host never advertises a token as active after
	// a joiner already considers it expired.
	config.ExpiresAt = time.Unix(config.ExpiresAt.Unix(), 0).UTC()
	if !config.ExpiresAt.After(now) {
		config.ExpiresAt = config.ExpiresAt.Add(time.Second)
	}
	shareID := config.ShareID
	if shareID == "" {
		var err error
		shareID, err = randomID(16)
		if err != nil {
			return nil, err
		}
	}
	secret := append([]byte(nil), config.Secret...)
	if len(secret) == 0 {
		secret = make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, fmt.Errorf("generate share secret: %w", err)
		}
	}
	if len(secret) != 32 {
		return nil, fmt.Errorf("share secret is %d bytes, want 32", len(secret))
	}
	capabilities := append([]string(nil), config.Capabilities...)
	if len(capabilities) == 0 {
		capabilities = append([]string(nil), DefaultCapabilities...)
	}

	identity, err := transport.GenerateTLSIdentity(now, config.ExpiresAt)
	if err != nil {
		return nil, err
	}
	rawListener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		return nil, fmt.Errorf("listen for share: %w", err)
	}
	runtime := &Runtime{listener: rawListener, serveDone: make(chan error, 1)}
	port := rawListener.Addr().(*net.TCPAddr).Port
	lanEndpoints, err := transport.LANEndpoints(port)
	if err != nil {
		_ = rawListener.Close()
		return nil, fmt.Errorf("enumerate LAN endpoints: %w", err)
	}
	for _, endpoint := range config.ExtraLANEndpoints {
		if addr, err := netip.ParseAddr(endpoint); err == nil {
			endpoint = net.JoinHostPort(addr.String(), strconv.Itoa(port))
		}
		lanEndpoints = append(lanEndpoints, endpoint)
	}

	runtime.invitation = invite.InvitationV1{
		Version:          invite.Version,
		ShareID:          shareID,
		ExpiresAt:        config.ExpiresAt.Unix(),
		ServerSPKISHA256: append([]byte(nil), identity.SPKISHA256[:]...),
		Secret:           secret,
		Scope:            invite.ScopeCollaborate,
		Capabilities:     capabilities,
		LAN: invite.LANCandidates{
			Endpoints: unique(lanEndpoints),
		},
	}

	provider := config.Tailnet
	if provider == nil {
		provider = transport.LocalAPITailnetStatus{}
	}
	statusCtx, cancelStatus := context.WithTimeout(ctx, time.Second)
	status, statusErr := provider.Status(statusCtx)
	cancelStatus()
	if err := ctx.Err(); err != nil {
		_ = rawListener.Close()
		return nil, err
	}
	if statusErr == nil && status.Running && len(status.IPs) > 0 {
		candidate := &invite.TailnetCandidates{DNSName: status.DNSName}
		candidate.Endpoints = tailnetIPv4Endpoints(status.IPs, port)
		if len(candidate.Endpoints) > 0 {
			runtime.invitation.Tailscale = candidate
		}
	}

	gate := AccessGate{
		ShareID:   shareID,
		Secret:    secret,
		ExpiresAt: config.ExpiresAt,
		Revoked:   runtime.revoked.Load,
		Now:       nowFunc,
		Next:      config.Handler,
	}
	runtime.server = &http.Server{
		Handler:           gate,
		ReadHeaderTimeout: 10 * time.Second,
	}
	tlsConfig := identity.ServerConfig()
	tlsListener := tls.NewListener(rawListener, tlsConfig)
	go func() {
		err := runtime.server.Serve(tlsListener)
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
			err = nil
		}
		runtime.serveDone <- err
	}()

	if config.EnableMDNS && len(runtime.invitation.LAN.Endpoints) > 0 {
		instance := "tcx-" + shareID
		pin := base64.RawURLEncoding.EncodeToString(identity.SPKISHA256[:])
		advertiser, err := transport.PublishMDNS(instance, port, []string{
			"version=1",
			"share=" + shareID,
			"spki=" + pin,
		})
		if err != nil {
			runtime.warnings = append(runtime.warnings, "mDNS unavailable: "+err.Error())
		} else {
			runtime.mdns = advertiser
			runtime.invitation.LAN.MDNSInstance = instance
		}
	}

	if config.EnableTailcat {
		timeout := config.TailcatTimeout
		if timeout <= 0 {
			timeout = 20 * time.Second
		}
		tailcatCtx, cancelTailcat := context.WithTimeout(ctx, timeout)
		host, err := transport.StartTailcatHost(tailcatCtx, TailcatHTTPPort, func(conn net.Conn) {
			serveTLSConnection(conn, tlsConfig, gate)
		})
		cancelTailcat()
		if err != nil {
			_ = runtime.Close()
			return nil, fmt.Errorf("%w: %w", ErrTailcatPrewarm, err)
		}
		runtime.tailcat = host
		runtime.invitation.Tailcat = &invite.TailcatCandidate{
			ConnBlob:       host.ConnBlob(),
			VirtualPort:    TailcatHTTPPort,
			LibraryVersion: invite.TailcatLibraryV040,
		}
	}

	if err := runtime.invitation.Validate(now); err != nil {
		_ = runtime.Close()
		return nil, err
	}
	return runtime, nil
}

func (runtime *Runtime) Invitation() invite.InvitationV1 {
	invitation := runtime.invitation
	invitation.Secret = append([]byte(nil), invitation.Secret...)
	invitation.ServerSPKISHA256 = append([]byte(nil), invitation.ServerSPKISHA256...)
	invitation.Capabilities = append([]string(nil), invitation.Capabilities...)
	invitation.LAN.Endpoints = append([]string(nil), invitation.LAN.Endpoints...)
	if invitation.Tailscale != nil {
		copyCandidate := *invitation.Tailscale
		copyCandidate.Endpoints = append([]string(nil), copyCandidate.Endpoints...)
		invitation.Tailscale = &copyCandidate
	}
	if invitation.Tailcat != nil {
		copyCandidate := *invitation.Tailcat
		invitation.Tailcat = &copyCandidate
	}
	return invitation
}

func (runtime *Runtime) Token() (string, error) {
	return invite.Encode(runtime.Invitation())
}

func (runtime *Runtime) Addr() net.Addr { return runtime.listener.Addr() }

func (runtime *Runtime) Warnings() []string { return append([]string(nil), runtime.warnings...) }

func (runtime *Runtime) Done() <-chan error { return runtime.serveDone }

func (runtime *Runtime) Revoke() { runtime.revoked.Store(true) }

func (runtime *Runtime) Close() error {
	runtime.closeOnce.Do(func() {
		if runtime.mdns != nil {
			_ = runtime.mdns.Close()
		}
		if runtime.tailcat != nil {
			_ = runtime.tailcat.Close()
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		runtime.closeErr = runtime.server.Shutdown(shutdownCtx)
		if runtime.closeErr != nil {
			_ = runtime.server.Close()
		}
		_ = runtime.listener.Close()
	})
	return runtime.closeErr
}

func randomID(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate random ID: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func unique(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

// The share listener is intentionally IPv4-only. Tailscale normally reports
// both 100.x and fd7a:: addresses, but advertising the IPv6 address would
// create a candidate that can never reach this listener.
func tailnetIPv4Endpoints(addresses []netip.Addr, port int) []string {
	var endpoints []string
	for _, address := range addresses {
		address = address.Unmap()
		if !address.Is4() {
			continue
		}
		endpoints = append(endpoints, net.JoinHostPort(address.String(), strconv.Itoa(port)))
	}
	return unique(endpoints)
}

type notifyingConn struct {
	net.Conn
	done chan struct{}
	once sync.Once
}

func (conn *notifyingConn) Close() error {
	err := conn.Conn.Close()
	conn.once.Do(func() { close(conn.done) })
	return err
}

type singleConnListener struct {
	conn *notifyingConn
	once sync.Once
}

func (listener *singleConnListener) Accept() (net.Conn, error) {
	accepted := false
	listener.once.Do(func() { accepted = true })
	if accepted {
		return listener.conn, nil
	}
	<-listener.conn.done
	return nil, net.ErrClosed
}

func (listener *singleConnListener) Close() error { return listener.conn.Close() }

func (listener *singleConnListener) Addr() net.Addr { return listener.conn.LocalAddr() }

func serveTLSConnection(raw net.Conn, config *tls.Config, handler http.Handler) {
	conn := &notifyingConn{Conn: tls.Server(raw, config), done: make(chan struct{})}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	_ = server.Serve(&singleConnListener{conn: conn})
}
