package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"teamcross/internal/invite"
	"teamcross/internal/share"
	"teamcross/internal/transport"
)

type joinOfflineTailnet struct{}

func (joinOfflineTailnet) Status(context.Context) (transport.TailnetStatus, error) {
	return transport.TailnetStatus{}, nil
}

type joinRoundTripFunc func(*http.Request) (*http.Response, error)

func (function joinRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type joinFailingConnector struct {
	closes *atomic.Int32
}

func (*joinFailingConnector) Dial(context.Context) (net.Conn, error) {
	client, server := net.Pipe()
	_ = server.Close()
	return client, nil
}

func (connector *joinFailingConnector) Close() error {
	connector.closes.Add(1)
	return nil
}

func joinTestInvitation() invite.InvitationV1 {
	return invite.InvitationV1{
		Version:          invite.Version,
		ShareID:          "join-reconnect",
		ExpiresAt:        time.Now().Add(time.Hour).Unix(),
		ServerSPKISHA256: bytes.Repeat([]byte{1}, 32),
		Secret:           bytes.Repeat([]byte{2}, 32),
		Scope:            invite.ScopeCollaborate,
		Capabilities:     []string{"view"},
		LAN:              invite.LANCandidates{Endpoints: []string{"192.168.1.2:443"}},
	}
}

func TestReconnectRetriesBodylessRequestAndClearsFailedPath(t *testing.T) {
	var factories atomic.Int32
	var closes atomic.Int32
	selector := &transport.Selector{
		Browse: func(context.Context, string) ([]string, error) { return nil, nil },
		DirectFactory: func(string) (transport.Connector, error) {
			factories.Add(1)
			return &joinFailingConnector{closes: &closes}, nil
		},
		Verify: func(context.Context, net.Conn, invite.InvitationV1, transport.Kind, string) error { return nil },
	}
	invitation := joinTestInvitation()
	selection, err := selector.Dial(context.Background(), invitation)
	if err != nil {
		t.Fatal(err)
	}
	dialer := &reconnectingRoundTripper{
		selector: selector, invitation: invitation, selection: selection,
		transport: joinRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("connection reset")
		}),
	}
	request := httptest.NewRequest(http.MethodGet, "https://teamcross.invalid/api/v1/info", nil)
	if request.Body != http.NoBody {
		t.Fatalf("test request body = %#v, want http.NoBody", request.Body)
	}
	if _, err := dialer.RoundTrip(request); err == nil {
		t.Fatal("RoundTrip succeeded through failing connections")
	}
	if factories.Load() < 2 {
		t.Fatalf("connector factories = %d, want reconnect selection", factories.Load())
	}
	if closes.Load() < 2 || dialer.kind() != "" {
		t.Fatalf("failed paths not cleared: closes=%d kind=%q", closes.Load(), dialer.kind())
	}
}

func TestReconnectInvalidatesPathWhenBodyCannotBeReplayed(t *testing.T) {
	var factories atomic.Int32
	var closes atomic.Int32
	selector := &transport.Selector{
		Browse: func(context.Context, string) ([]string, error) { return nil, nil },
		DirectFactory: func(string) (transport.Connector, error) {
			factories.Add(1)
			return &joinFailingConnector{closes: &closes}, nil
		},
		Verify: func(context.Context, net.Conn, invite.InvitationV1, transport.Kind, string) error { return nil },
	}
	invitation := joinTestInvitation()
	selection, err := selector.Dial(context.Background(), invitation)
	if err != nil {
		t.Fatal(err)
	}
	dialer := &reconnectingRoundTripper{
		selector: selector, invitation: invitation, selection: selection,
		transport: joinRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("connection reset")
		}),
	}
	request := httptest.NewRequest(http.MethodPost, "https://teamcross.invalid/api/v1/write", io.NopCloser(strings.NewReader("one shot")))
	request.GetBody = nil
	if _, err := dialer.RoundTrip(request); err == nil {
		t.Fatal("RoundTrip succeeded through failing connection")
	}
	if factories.Load() != 1 || closes.Load() != 1 || dialer.kind() != "" {
		t.Fatalf("non-replayable failure state: factories=%d closes=%d kind=%q", factories.Load(), closes.Load(), dialer.kind())
	}
}

func TestJoinProxyForwardsOnlyThroughAuthenticatedShare(t *testing.T) {
	remote := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/ping" {
			http.NotFound(response, request)
			return
		}
		if request.Header.Get("X-TeamCross-Participant-ID") == "" {
			http.Error(response, "missing participant", http.StatusBadRequest)
			return
		}
		if got := request.Header.Get("X-TeamCross-Participant-Name"); got != "Alice" {
			http.Error(response, "participant name "+got, http.StatusBadRequest)
			return
		}
		if got := request.Header.Get("X-TeamCross-Transport"); got != string(transport.KindLAN) {
			http.Error(response, "transport "+got, http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(response, "pong")
	})
	runtime, err := share.Start(context.Background(), share.RuntimeConfig{
		ExpiresAt:         time.Now().Add(time.Hour),
		Handler:           remote,
		Tailnet:           joinOfflineTailnet{},
		ExtraLANEndpoints: []string{"127.0.0.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	token, err := runtime.Token()
	if err != nil {
		t.Fatal(err)
	}
	selector := transport.NewSelector()
	selector.Browse = func(context.Context, string) ([]string, error) { return nil, nil }
	selector.Tailnet = joinOfflineTailnet{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	proxy, err := StartJoinProxy(ctx, JoinConfig{Invitation: token, Name: " Alice ", Selector: selector})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()

	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(proxy.URL() + "/api/v1/ping")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "pong" {
		t.Fatalf("join response = %d %q", response.StatusCode, body)
	}
	if got := proxy.Transport(); got != string(transport.KindLAN) {
		t.Fatalf("proxy transport = %q, want LAN", got)
	}

	runtime.Revoke()
	revoked, err := client.Get(proxy.URL() + "/api/v1/ping")
	if err != nil {
		t.Fatal(err)
	}
	defer revoked.Body.Close()
	if revoked.StatusCode != http.StatusGone || revoked.Header.Get("X-TeamCross-Error") != "revoked" {
		t.Fatalf("revoked response = %d/%q, want 410/revoked", revoked.StatusCode, revoked.Header.Get("X-TeamCross-Error"))
	}
}

func TestWriteJoinProxyErrorPreservesTerminalMeaning(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "invitation expired before dial", err: invite.ErrExpired, wantStatus: http.StatusGone, wantCode: "share_expired"},
		{name: "remote expired", err: &transport.ProtocolError{Failure: transport.ProtocolExpired, StatusCode: http.StatusGone}, wantStatus: http.StatusGone, wantCode: "share_expired"},
		{name: "remote revoked", err: &transport.ProtocolError{Failure: transport.ProtocolRevoked, StatusCode: http.StatusGone}, wantStatus: http.StatusGone, wantCode: "share_revoked"},
		{name: "bad secret", err: &transport.ProtocolError{Failure: transport.ProtocolUnauthorized, StatusCode: http.StatusUnauthorized}, wantStatus: http.StatusUnauthorized, wantCode: "share_unauthorized"},
		{name: "forbidden", err: &transport.ProtocolError{Failure: transport.ProtocolForbidden, StatusCode: http.StatusForbidden}, wantStatus: http.StatusForbidden, wantCode: "share_forbidden"},
		{name: "version mismatch", err: &transport.ProtocolError{Failure: transport.ProtocolIncompatible, StatusCode: http.StatusUpgradeRequired}, wantStatus: http.StatusUpgradeRequired, wantCode: "protocol_incompatible"},
		{name: "network error", err: errors.New("connection reset"), wantStatus: http.StatusBadGateway, wantCode: "remote_unreachable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeJoinProxyError(recorder, nil, test.err)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			var body struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != test.wantCode {
				t.Fatalf("code = %q, want %q", body.Code, test.wantCode)
			}
		})
	}
}
