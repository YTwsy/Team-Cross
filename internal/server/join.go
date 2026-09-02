package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"

	"teamcross/internal/invite"
	"teamcross/internal/transport"
)

type JoinConfig struct {
	Invitation    string
	Name          string
	ListenAddress string
	DevWeb        string
	Selector      *transport.Selector
}

type JoinProxy struct {
	listener net.Listener
	server   *http.Server
	dialer   *reconnectingRoundTripper
}

func StartJoinProxy(ctx context.Context, config JoinConfig) (*JoinProxy, error) {
	invitation, err := invite.Decode(config.Invitation, time.Now())
	if err != nil {
		return nil, err
	}
	selector := config.Selector
	if selector == nil {
		selector = transport.NewSelector()
	}
	selection, err := selector.Dial(ctx, invitation)
	if err != nil {
		return nil, err
	}
	dialer := &reconnectingRoundTripper{selector: selector, invitation: invitation, selection: selection, transport: selection.HTTPClient().Transport}
	participantID := uuid.NewString()
	remote, _ := url.Parse("https://teamcross.invalid")
	proxy := httputil.NewSingleHostReverseProxy(remote)
	proxy.Transport = dialer
	proxy.FlushInterval = -1
	originalDirector := proxy.Director
	proxy.Director = func(request *http.Request) {
		originalDirector(request)
		request.Host = "teamcross.invalid"
		request.Header.Set("X-TeamCross-Participant-ID", participantID)
		request.Header.Set("X-TeamCross-Participant-Name", cleanParticipantName(config.Name))
	}
	proxy.ErrorHandler = writeJoinProxyError
	root := http.NewServeMux()
	root.Handle("/api/", replayable(proxy))
	if config.DevWeb != "" {
		if target, parseErr := url.Parse(config.DevWeb); parseErr == nil {
			root.Handle("/", httputil.NewSingleHostReverseProxy(target))
		} else {
			_ = selection.Close()
			return nil, parseErr
		}
	} else {
		root.Handle("/", spaHandler())
	}
	address := config.ListenAddress
	if address == "" {
		address = "127.0.0.1:0"
	}
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		_ = selection.Close()
		return nil, fmt.Errorf("listen join proxy: %w", err)
	}
	join := &JoinProxy{listener: listener, dialer: dialer, server: &http.Server{Handler: secureHeaders(root), ReadHeaderTimeout: 10 * time.Second}}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = join.server.Shutdown(shutdown)
	}()
	go func() { _ = join.server.Serve(listener) }()
	return join, nil
}

func writeJoinProxyError(response http.ResponseWriter, _ *http.Request, proxyErr error) {
	if errors.Is(proxyErr, invite.ErrExpired) {
		writeError(response, http.StatusGone, "share_expired", "The Team Cross invitation expired")
		return
	}
	var protocolError *transport.ProtocolError
	if errors.As(proxyErr, &protocolError) {
		switch protocolError.Failure {
		case transport.ProtocolExpired:
			writeError(response, http.StatusGone, "share_expired", "The Team Cross Share expired")
		case transport.ProtocolRevoked:
			writeError(response, http.StatusGone, "share_revoked", "The Team Cross Share was revoked")
		case transport.ProtocolUnauthorized:
			writeError(response, http.StatusUnauthorized, "share_unauthorized", "The Team Cross invitation credentials were rejected")
		case transport.ProtocolForbidden:
			writeError(response, http.StatusForbidden, "share_forbidden", "The Team Cross Share rejected this operation")
		case transport.ProtocolIncompatible:
			writeError(response, http.StatusUpgradeRequired, "protocol_incompatible", "The Team Cross protocol is incompatible")
		default:
			writeError(response, http.StatusBadGateway, "remote_unreachable", protocolError.Error())
		}
		return
	}
	writeError(response, http.StatusBadGateway, "remote_unreachable", proxyErr.Error())
}

func (proxy *JoinProxy) URL() string       { return "http://" + proxy.listener.Addr().String() }
func (proxy *JoinProxy) Transport() string { return string(proxy.dialer.kind()) }

func (proxy *JoinProxy) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return joinErrors(proxy.server.Shutdown(ctx), proxy.dialer.Close())
}

type reconnectingRoundTripper struct {
	selector   *transport.Selector
	invitation invite.InvitationV1

	mu        sync.Mutex
	selection *transport.Selection
	transport http.RoundTripper
}

func (dialer *reconnectingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	for attempt := 0; attempt < 2; attempt++ {
		selection, roundTripper, err := dialer.current(request.Context())
		if err != nil {
			return nil, err
		}
		outgoing := request.Clone(request.Context())
		outgoing.Header = request.Header.Clone()
		selection.Authorize(outgoing)
		outgoing.Header.Set("X-TeamCross-Transport", string(selection.Transport))
		if attempt > 0 && request.GetBody != nil {
			body, bodyErr := request.GetBody()
			if bodyErr != nil {
				return nil, bodyErr
			}
			outgoing.Body = body
		}
		response, requestErr := roundTripper.RoundTrip(outgoing)
		if requestErr == nil {
			return response, nil
		}
		canRetry := request.Body == nil || request.Body == http.NoBody || request.GetBody != nil
		// A transport-level failure invalidates the selected path even when this
		// particular request cannot be replayed. Otherwise every later browser
		// request remains pinned to the known-bad connection.
		dialer.invalidate(selection)
		if attempt == 1 || !canRetry {
			return nil, requestErr
		}
	}
	return nil, fmt.Errorf("connection retry exhausted")
}

func (dialer *reconnectingRoundTripper) current(ctx context.Context) (*transport.Selection, http.RoundTripper, error) {
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	if dialer.selection == nil {
		selection, err := dialer.selector.Dial(ctx, dialer.invitation)
		if err != nil {
			return nil, nil, err
		}
		dialer.selection = selection
		dialer.transport = selection.HTTPClient().Transport
	}
	return dialer.selection, dialer.transport, nil
}

func (dialer *reconnectingRoundTripper) invalidate(selection *transport.Selection) {
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	if dialer.selection != selection {
		return
	}
	if closer, ok := dialer.transport.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
	_ = dialer.selection.Close()
	dialer.selection = nil
	dialer.transport = nil
}

func (dialer *reconnectingRoundTripper) kind() transport.Kind {
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	if dialer.selection == nil {
		return ""
	}
	return dialer.selection.Transport
}

func (dialer *reconnectingRoundTripper) Close() error {
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	if closer, ok := dialer.transport.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
	if dialer.selection == nil {
		return nil
	}
	err := dialer.selection.Close()
	dialer.selection = nil
	return err
}

func replayable(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Body != nil && request.ContentLength != 0 {
			body, err := io.ReadAll(http.MaxBytesReader(response, request.Body, 25<<20))
			if err != nil {
				writeError(response, http.StatusBadRequest, "body", err.Error())
				return
			}
			request.Body = io.NopCloser(bytes.NewReader(body))
			request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
			request.ContentLength = int64(len(body))
		}
		next.ServeHTTP(response, request)
	})
}
