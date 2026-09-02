package transport

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"teamcross/internal/invite"
)

type staticTailnetStatus struct {
	status TailnetStatus
	err    error
}

func (status staticTailnetStatus) Status(context.Context) (TailnetStatus, error) {
	return status.status, status.err
}

type tailnetStatusFunc func(context.Context) (TailnetStatus, error)

func (status tailnetStatusFunc) Status(ctx context.Context) (TailnetStatus, error) {
	return status(ctx)
}

type pipeConnector struct{}

func (*pipeConnector) Dial(context.Context) (net.Conn, error) {
	a, b := net.Pipe()
	go func() {
		defer b.Close()
		_, _ = b.Read(make([]byte, 1))
	}()
	return a, nil
}

func (*pipeConnector) Close() error { return nil }

func dialInvitation() invite.InvitationV1 {
	return invite.InvitationV1{
		Version:          invite.Version,
		ShareID:          "dial-test",
		ExpiresAt:        time.Now().Add(time.Hour).Unix(),
		ServerSPKISHA256: bytes.Repeat([]byte{1}, 32),
		Secret:           bytes.Repeat([]byte{2}, 32),
		Scope:            invite.ScopeCollaborate,
		Capabilities:     []string{"view"},
		LAN: invite.LANCandidates{
			Endpoints: []string{"192.168.1.2:443"},
		},
		Tailscale: &invite.TailnetCandidates{Endpoints: []string{"100.64.0.2:443"}},
		Tailcat: &invite.TailcatCandidate{
			ConnBlob:       "tcblob",
			VirtualPort:    443,
			LibraryVersion: invite.TailcatLibraryV040,
		},
	}
}

func testBudgets() Budgets {
	return Budgets{
		MDNSDiscovery: 5 * time.Millisecond,
		LAN:           100 * time.Millisecond,
		Tailscale:     100 * time.Millisecond,
		Tailcat:       100 * time.Millisecond,
	}
}

func TestSelectorStopsAtLAN(t *testing.T) {
	var mu sync.Mutex
	var attempts []Kind
	selector := &Selector{
		Budgets: testBudgets(),
		Browse:  func(context.Context, string) ([]string, error) { return nil, nil },
		DirectFactory: func(string) (Connector, error) {
			return &pipeConnector{}, nil
		},
		Tailnet: staticTailnetStatus{status: TailnetStatus{Running: true}},
		TailcatFactory: func(string, uint16) (Connector, error) {
			t.Fatal("tailcat should not be initialized after LAN succeeds")
			return nil, nil
		},
		Verify: func(_ context.Context, _ net.Conn, _ invite.InvitationV1, kind Kind, _ string) error {
			mu.Lock()
			attempts = append(attempts, kind)
			mu.Unlock()
			return nil
		},
	}
	selection, err := selector.Dial(context.Background(), dialInvitation())
	if err != nil {
		t.Fatal(err)
	}
	defer selection.Close()
	if selection.Transport != KindLAN {
		t.Fatalf("transport = %q, want LAN", selection.Transport)
	}
	if len(attempts) != 1 || attempts[0] != KindLAN {
		t.Fatalf("attempts = %v", attempts)
	}
}

func TestSelectorAddsMDNSCandidateDuringLANPhase(t *testing.T) {
	inv := dialInvitation()
	inv.LAN.MDNSInstance = "dial-test"
	selector := &Selector{
		Budgets: testBudgets(),
		Browse: func(context.Context, string) ([]string, error) {
			return []string{"192.168.1.3:443"}, nil
		},
		DirectFactory: func(string) (Connector, error) { return &pipeConnector{}, nil },
		Tailnet:       staticTailnetStatus{status: TailnetStatus{Running: true}},
		Verify: func(_ context.Context, _ net.Conn, _ invite.InvitationV1, _ Kind, endpoint string) error {
			if endpoint == "192.168.1.3:443" {
				return nil
			}
			return errors.New("explicit endpoint unavailable")
		},
	}
	selection, err := selector.Dial(context.Background(), inv)
	if err != nil {
		t.Fatal(err)
	}
	defer selection.Close()
	if selection.Transport != KindLAN || selection.Endpoint != "192.168.1.3:443" {
		t.Fatalf("selection = %s %s, want discovered LAN endpoint", selection.Transport, selection.Endpoint)
	}
}

func TestSelectorFallbackOrder(t *testing.T) {
	var mu sync.Mutex
	var order []Kind
	selector := &Selector{
		Budgets: testBudgets(),
		Browse:  func(context.Context, string) ([]string, error) { return nil, nil },
		DirectFactory: func(string) (Connector, error) {
			return &pipeConnector{}, nil
		},
		Tailnet: staticTailnetStatus{status: TailnetStatus{Running: true}},
		TailcatFactory: func(string, uint16) (Connector, error) {
			return &pipeConnector{}, nil
		},
		Verify: func(_ context.Context, _ net.Conn, _ invite.InvitationV1, kind Kind, _ string) error {
			mu.Lock()
			order = append(order, kind)
			mu.Unlock()
			if kind != KindTailcat {
				return errors.New("unreachable")
			}
			return nil
		},
	}
	selection, err := selector.Dial(context.Background(), dialInvitation())
	if err != nil {
		t.Fatal(err)
	}
	defer selection.Close()
	if selection.Transport != KindTailcat {
		t.Fatalf("transport = %q, want tailcat", selection.Transport)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []Kind{KindLAN, KindTailscale, KindTailcat}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestSelectorTerminalProtocolFailureStopsFallback(t *testing.T) {
	for _, failure := range []ProtocolFailure{
		ProtocolExpired,
		ProtocolRevoked,
		ProtocolUnauthorized,
		ProtocolForbidden,
		ProtocolIncompatible,
	} {
		t.Run(string(failure), func(t *testing.T) {
			tailnetCalled := false
			tailcatCalled := false
			selector := &Selector{
				Budgets: testBudgets(),
				Browse:  func(context.Context, string) ([]string, error) { return nil, nil },
				DirectFactory: func(string) (Connector, error) {
					return &pipeConnector{}, nil
				},
				Tailnet: tailnetStatusFunc(func(context.Context) (TailnetStatus, error) {
					tailnetCalled = true
					return TailnetStatus{Running: true}, nil
				}),
				TailcatFactory: func(string, uint16) (Connector, error) {
					tailcatCalled = true
					return &pipeConnector{}, nil
				},
				Verify: func(context.Context, net.Conn, invite.InvitationV1, Kind, string) error {
					return &ProtocolError{Failure: failure, StatusCode: http.StatusUnauthorized}
				},
			}
			_, err := selector.Dial(context.Background(), dialInvitation())
			var protocolError *ProtocolError
			if !errors.As(err, &protocolError) || protocolError.Failure != failure {
				t.Fatalf("Dial error = %v, want %s ProtocolError", err, failure)
			}
			if tailnetCalled || tailcatCalled {
				t.Fatalf("terminal failure reached later transport: tailnet=%v tailcat=%v", tailnetCalled, tailcatCalled)
			}
		})
	}
}

func TestClassifyHandshakeResponse(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		header    http.Header
		want      ProtocolFailure
		wantError bool
		terminal  bool
	}{
		{name: "ok", status: http.StatusOK, header: http.Header{"X-Teamcross-Protocol": {"1"}}},
		{name: "unauthorized", status: http.StatusUnauthorized, header: make(http.Header), want: ProtocolUnauthorized, wantError: true, terminal: true},
		{name: "forbidden", status: http.StatusForbidden, header: make(http.Header), want: ProtocolForbidden, wantError: true, terminal: true},
		{name: "expired", status: http.StatusGone, header: http.Header{"X-Teamcross-Error": {"expired"}}, want: ProtocolExpired, wantError: true, terminal: true},
		{name: "revoked", status: http.StatusGone, header: http.Header{"X-Teamcross-Error": {"revoked"}}, want: ProtocolRevoked, wantError: true, terminal: true},
		{name: "upgrade", status: http.StatusUpgradeRequired, header: make(http.Header), want: ProtocolIncompatible, wantError: true, terminal: true},
		{name: "success missing protocol", status: http.StatusOK, header: make(http.Header), want: ProtocolIncompatible, wantError: true, terminal: true},
		{name: "server error remains fallback eligible", status: http.StatusInternalServerError, header: http.Header{"X-Teamcross-Protocol": {"1"}}, wantError: true, terminal: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := classifyHandshakeResponse(test.status, test.header, "detail")
			if (err != nil) != test.wantError {
				t.Fatalf("error = %v, wantError %v", err, test.wantError)
			}
			if IsTerminal(err) != test.terminal {
				t.Fatalf("IsTerminal(%v) = %v, want %v", err, IsTerminal(err), test.terminal)
			}
			if test.want != "" {
				var protocolError *ProtocolError
				if !errors.As(err, &protocolError) || protocolError.Failure != test.want {
					t.Fatalf("error = %#v, want failure %q", err, test.want)
				}
			}
		})
	}
}

func TestSelectorSkipsTailnetWhenLocalAPIIsNotRunning(t *testing.T) {
	var order []Kind
	selector := &Selector{
		Budgets:        testBudgets(),
		Browse:         func(context.Context, string) ([]string, error) { return nil, nil },
		DirectFactory:  func(string) (Connector, error) { return &pipeConnector{}, nil },
		Tailnet:        staticTailnetStatus{status: TailnetStatus{Running: false}},
		TailcatFactory: func(string, uint16) (Connector, error) { return &pipeConnector{}, nil },
		Verify: func(_ context.Context, _ net.Conn, _ invite.InvitationV1, kind Kind, _ string) error {
			order = append(order, kind)
			if kind == KindLAN {
				return errors.New("LAN unavailable")
			}
			return nil
		},
	}
	selection, err := selector.Dial(context.Background(), dialInvitation())
	if err != nil {
		t.Fatal(err)
	}
	defer selection.Close()
	if selection.Transport != KindTailcat {
		t.Fatalf("transport = %q, want Tailcat", selection.Transport)
	}
	for _, kind := range order {
		if kind == KindTailscale {
			t.Fatal("Tailnet was attempted while LocalAPI was not Running")
		}
	}
}
