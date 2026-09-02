package transport

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"teamcross/internal/invite"
)

type Kind string

const (
	KindLAN       Kind = "lan"
	KindTailscale Kind = "tailscale"
	KindTailcat   Kind = "tailcat"
)

type Budgets struct {
	MDNSDiscovery time.Duration
	LAN           time.Duration
	Tailscale     time.Duration
	Tailcat       time.Duration
}

func DefaultBudgets() Budgets {
	return Budgets{
		MDNSDiscovery: 750 * time.Millisecond,
		LAN:           1500 * time.Millisecond,
		Tailscale:     3 * time.Second,
		Tailcat:       12 * time.Second,
	}
}

type Connector interface {
	Dial(context.Context) (net.Conn, error)
	Close() error
}

type ConnectorFactory func(endpoint string) (Connector, error)
type TailcatConnectorFactory func(connBlob string, virtualPort uint16) (Connector, error)
type BrowseFunc func(ctx context.Context, instance string) ([]string, error)
type VerifyFunc func(ctx context.Context, conn net.Conn, inv invite.InvitationV1, kind Kind, endpoint string) error

type Selector struct {
	Budgets        Budgets
	Browse         BrowseFunc
	DirectFactory  ConnectorFactory
	Tailnet        TailnetStatusProvider
	TailcatFactory TailcatConnectorFactory
	Verify         VerifyFunc
}

func NewSelector() *Selector {
	return &Selector{
		Budgets: DefaultBudgets(),
		Browse:  BrowseMDNS,
		DirectFactory: func(endpoint string) (Connector, error) {
			if _, _, err := net.SplitHostPort(endpoint); err != nil {
				return nil, fmt.Errorf("invalid endpoint %q: %w", endpoint, err)
			}
			return &directConnector{endpoint: endpoint}, nil
		},
		Tailnet: LocalAPITailnetStatus{},
		TailcatFactory: func(connBlob string, virtualPort uint16) (Connector, error) {
			return NewTailcatConnector(connBlob, virtualPort)
		},
		Verify: VerifyShareHandshake,
	}
}

type directConnector struct {
	endpoint string
	dialer   net.Dialer
}

func (connector *directConnector) Dial(ctx context.Context) (net.Conn, error) {
	return connector.dialer.DialContext(ctx, "tcp", connector.endpoint)
}

func (*directConnector) Close() error { return nil }

type ProtocolFailure string

const (
	ProtocolExpired      ProtocolFailure = "expired"
	ProtocolRevoked      ProtocolFailure = "revoked"
	ProtocolUnauthorized ProtocolFailure = "unauthorized"
	ProtocolForbidden    ProtocolFailure = "forbidden"
	ProtocolIncompatible ProtocolFailure = "incompatible"
)

// ProtocolError is terminal because it was received after the invitation's
// SPKI pin authenticated the host. Trying a different route cannot repair it.
type ProtocolError struct {
	Failure    ProtocolFailure
	StatusCode int
	Detail     string
}

func (err *ProtocolError) Error() string {
	if err.Detail == "" {
		return fmt.Sprintf("Team Cross handshake failed: %s (HTTP %d)", err.Failure, err.StatusCode)
	}
	return fmt.Sprintf("Team Cross handshake failed: %s (HTTP %d): %s", err.Failure, err.StatusCode, err.Detail)
}

func IsTerminal(err error) bool {
	var protocolError *ProtocolError
	return errors.As(err, &protocolError)
}

type Attempt struct {
	Transport Kind
	Endpoint  string
	Err       error
}

type DialError struct {
	Attempts []Attempt
}

func (err *DialError) Error() string {
	if len(err.Attempts) == 0 {
		return "no usable Team Cross connection candidates"
	}
	parts := make([]string, 0, len(err.Attempts))
	for _, attempt := range err.Attempts {
		parts = append(parts, fmt.Sprintf("%s %s: %v", attempt.Transport, attempt.Endpoint, attempt.Err))
	}
	return "all Team Cross connection candidates failed: " + strings.Join(parts, "; ")
}

func (err *DialError) Unwrap() []error {
	result := make([]error, 0, len(err.Attempts))
	for _, attempt := range err.Attempts {
		result = append(result, attempt.Err)
	}
	return result
}

// Selection fixes the transport for the current join session. Call DialTLS or
// HTTPClient for additional connections; rerun Selector.Dial only after the
// session disconnects and needs path selection again.
type Selection struct {
	Transport Kind
	Endpoint  string
	inv       invite.InvitationV1
	connector Connector
	once      sync.Once
	closeErr  error
}

func (selection *Selection) DialTLS(ctx context.Context) (net.Conn, error) {
	raw, err := selection.connector.Dial(ctx)
	if err != nil {
		return nil, err
	}
	return DialPinnedTLS(ctx, raw, selection.inv.ServerSPKISHA256)
}

func (selection *Selection) HTTPClient() *http.Client {
	transport := &http.Transport{
		ForceAttemptHTTP2: false,
		DialTLSContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return selection.DialTLS(ctx)
		},
	}
	return &http.Client{Transport: transport}
}

// Authorize adds the private headers expected by the share-only listener.
func (selection *Selection) Authorize(request *http.Request) {
	AuthorizeRequest(request, selection.inv)
}

func (selection *Selection) Close() error {
	selection.once.Do(func() { selection.closeErr = selection.connector.Close() })
	return selection.closeErr
}

func (selector *Selector) Dial(ctx context.Context, inv invite.InvitationV1) (*Selection, error) {
	if err := inv.Validate(time.Now()); err != nil {
		return nil, err
	}
	configured := *selector
	configured.fillDefaults()
	selector = &configured
	var attempts []Attempt

	lanCtx, cancelLAN := context.WithTimeout(ctx, selector.Budgets.LAN)
	selection, phaseAttempts, terminal := selector.dialLAN(lanCtx, inv)
	cancelLAN()
	attempts = append(attempts, phaseAttempts...)
	if selection != nil || terminal != nil {
		if terminal != nil {
			return nil, terminal
		}
		return selection, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if inv.Tailscale != nil {
		tailnetCtx, cancelTailnet := context.WithTimeout(ctx, selector.Budgets.Tailscale)
		status, err := selector.Tailnet.Status(tailnetCtx)
		if err != nil {
			attempts = append(attempts, Attempt{Transport: KindTailscale, Endpoint: "localapi", Err: err})
		} else if status.Running {
			selection, phaseAttempts, terminal = selector.raceStatic(tailnetCtx, inv, KindTailscale, inv.Tailscale.Endpoints, selector.DirectFactory)
			attempts = append(attempts, phaseAttempts...)
		}
		cancelTailnet()
		if selection != nil || terminal != nil {
			if terminal != nil {
				return nil, terminal
			}
			return selection, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}

	if inv.Tailcat != nil {
		tailcatCtx, cancelTailcat := context.WithTimeout(ctx, selector.Budgets.Tailcat)
		factory := func(_ string) (Connector, error) {
			return selector.TailcatFactory(inv.Tailcat.ConnBlob, inv.Tailcat.VirtualPort)
		}
		selection, phaseAttempts, terminal = selector.raceStatic(tailcatCtx, inv, KindTailcat, []string{"tailcat"}, factory)
		attempts = append(attempts, phaseAttempts...)
		cancelTailcat()
		if selection != nil || terminal != nil {
			if terminal != nil {
				return nil, terminal
			}
			return selection, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, &DialError{Attempts: attempts}
}

func (selector *Selector) fillDefaults() {
	defaults := NewSelector()
	if selector.Budgets.MDNSDiscovery <= 0 {
		selector.Budgets.MDNSDiscovery = defaults.Budgets.MDNSDiscovery
	}
	if selector.Budgets.LAN <= 0 {
		selector.Budgets.LAN = defaults.Budgets.LAN
	}
	if selector.Budgets.Tailscale <= 0 {
		selector.Budgets.Tailscale = defaults.Budgets.Tailscale
	}
	if selector.Budgets.Tailcat <= 0 {
		selector.Budgets.Tailcat = defaults.Budgets.Tailcat
	}
	if selector.Browse == nil {
		selector.Browse = defaults.Browse
	}
	if selector.DirectFactory == nil {
		selector.DirectFactory = defaults.DirectFactory
	}
	if selector.Tailnet == nil {
		selector.Tailnet = defaults.Tailnet
	}
	if selector.TailcatFactory == nil {
		selector.TailcatFactory = defaults.TailcatFactory
	}
	if selector.Verify == nil {
		selector.Verify = defaults.Verify
	}
}

type attemptResult struct {
	connector Connector
	attempt   Attempt
}

func (selector *Selector) dialLAN(ctx context.Context, inv invite.InvitationV1) (*Selection, []Attempt, error) {
	discovered := make(chan []string, 1)
	if inv.LAN.MDNSInstance == "" {
		close(discovered)
	} else {
		go func() {
			discoveryCtx, cancel := context.WithTimeout(ctx, selector.Budgets.MDNSDiscovery)
			defer cancel()
			endpoints, _ := selector.Browse(discoveryCtx, inv.LAN.MDNSInstance)
			select {
			case discovered <- endpoints:
			case <-ctx.Done():
			}
			close(discovered)
		}()
	}
	return selector.raceDynamic(ctx, inv, KindLAN, inv.LAN.Endpoints, discovered, selector.DirectFactory)
}

func (selector *Selector) raceStatic(ctx context.Context, inv invite.InvitationV1, kind Kind, endpoints []string, factory ConnectorFactory) (*Selection, []Attempt, error) {
	discovered := make(chan []string)
	close(discovered)
	return selector.raceDynamic(ctx, inv, kind, endpoints, discovered, factory)
}

func (selector *Selector) raceDynamic(ctx context.Context, inv invite.InvitationV1, kind Kind, initial []string, discovered <-chan []string, factory ConnectorFactory) (*Selection, []Attempt, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan attemptResult)
	seen := map[string]bool{}
	active := 0
	var attempts []Attempt
	start := func(endpoint string) {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" || seen[endpoint] {
			return
		}
		seen[endpoint] = true
		active++
		go func() {
			connector, err := factory(endpoint)
			if err == nil {
				var conn net.Conn
				conn, err = connector.Dial(ctx)
				if err == nil {
					err = selector.Verify(ctx, conn, inv, kind, endpoint)
					_ = conn.Close()
				}
			}
			result := attemptResult{connector: connector, attempt: Attempt{Transport: kind, Endpoint: endpoint, Err: err}}
			select {
			case results <- result:
			case <-ctx.Done():
				if connector != nil {
					_ = connector.Close()
				}
			}
		}()
	}
	for _, endpoint := range uniqueStrings(initial) {
		start(endpoint)
	}
	discoveryDone := false
	for {
		if active == 0 && discoveryDone {
			return nil, attempts, nil
		}
		select {
		case endpoints, ok := <-discovered:
			if !ok {
				discoveryDone = true
				discovered = nil
				continue
			}
			for _, endpoint := range uniqueStrings(endpoints) {
				start(endpoint)
			}
		case result := <-results:
			active--
			if result.attempt.Err == nil {
				cancel()
				return &Selection{Transport: kind, Endpoint: result.attempt.Endpoint, inv: inv, connector: result.connector}, attempts, nil
			}
			if result.connector != nil {
				_ = result.connector.Close()
			}
			attempts = append(attempts, result.attempt)
			if IsTerminal(result.attempt.Err) {
				cancel()
				return nil, attempts, result.attempt.Err
			}
		case <-ctx.Done():
			if len(attempts) == 0 || !errors.Is(ctx.Err(), context.Canceled) {
				attempts = append(attempts, Attempt{Transport: kind, Endpoint: "phase", Err: ctx.Err()})
			}
			return nil, attempts, nil
		}
	}
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func AuthorizeRequest(request *http.Request, inv invite.InvitationV1) {
	request.Header.Set("Authorization", "Bearer "+base64.RawURLEncoding.EncodeToString(inv.Secret))
	request.Header.Set("X-TeamCross-Share-ID", inv.ShareID)
	request.Header.Set("X-TeamCross-Protocol", strconv.FormatUint(inv.Version, 10))
}

// VerifyShareHandshake authenticates TLS first, then asks the pinned host to
// prove the invitation is currently valid at the application layer.
func VerifyShareHandshake(ctx context.Context, raw net.Conn, inv invite.InvitationV1, _ Kind, _ string) error {
	conn, err := DialPinnedTLS(ctx, raw, inv.ServerSPKISHA256)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://teamcross.invalid/share/v1/handshake", nil)
	if err != nil {
		return err
	}
	AuthorizeRequest(request, inv)
	request.Header.Set("Connection", "close")
	if err := request.Write(conn); err != nil {
		return err
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	detailBytes, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	detail := strings.TrimSpace(string(detailBytes))
	return classifyHandshakeResponse(response.StatusCode, response.Header, detail)
}

func classifyHandshakeResponse(statusCode int, header http.Header, detail string) error {
	switch statusCode {
	case http.StatusUnauthorized:
		return &ProtocolError{Failure: ProtocolUnauthorized, StatusCode: statusCode, Detail: detail}
	case http.StatusForbidden:
		return &ProtocolError{Failure: ProtocolForbidden, StatusCode: statusCode, Detail: detail}
	case http.StatusGone:
		failure := ProtocolRevoked
		if header.Get("X-TeamCross-Error") == string(ProtocolExpired) {
			failure = ProtocolExpired
		}
		return &ProtocolError{Failure: failure, StatusCode: statusCode, Detail: detail}
	case http.StatusUpgradeRequired:
		return &ProtocolError{Failure: ProtocolIncompatible, StatusCode: statusCode, Detail: detail}
	}
	if statusCode < 200 || statusCode >= 300 {
		return fmt.Errorf("Team Cross handshake returned HTTP %d: %s", statusCode, detail)
	}
	serverProtocol := header.Get("X-TeamCross-Protocol")
	if serverProtocol != strconv.FormatUint(invite.Version, 10) {
		return &ProtocolError{Failure: ProtocolIncompatible, StatusCode: statusCode, Detail: "server protocol " + serverProtocol}
	}
	return nil
}
