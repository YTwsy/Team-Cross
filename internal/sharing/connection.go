package sharing

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"teamcross/internal/problem"
	"time"

	"github.com/tailscale/tailcat"
)

// Connection owns one concrete transport and the HTTP client layered over it.
// Team Cross authentication and TLS pinning are identical for every transport.
type Connection struct {
	URL       string
	Client    *http.Client
	Transport Transport

	httpTransport *http.Transport
	tailcatClient *tailcat.Client
	closeOnce     sync.Once
	closeErr      error
}

func (c *Connection) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		if c.httpTransport != nil {
			c.httpTransport.CloseIdleConnections()
		}
		if c.tailcatClient != nil {
			c.closeErr = c.tailcatClient.Close()
		}
	})
	return c.closeErr
}

func pinnedTLSConfig(i Invitation) *tls.Config {
	return &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true, // The invitation's SPKI pin is the trust root.
		VerifyConnection: func(c tls.ConnectionState) error {
			if len(c.PeerCertificates) == 0 {
				return fmt.Errorf("主机没有提供证书")
			}
			pin := sha256.Sum256(c.PeerCertificates[0].RawSubjectPublicKeyInfo)
			if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(pin[:])), []byte(i.Pin)) != 1 {
				return fmt.Errorf("协作主机指纹不匹配")
			}
			return nil
		},
	}
}

// NewConnection reconstructs a transport without performing an admission or
// status request. endpoint is only used to restore a previously selected LAN
// address; Tailcat addresses are carried by the invitation itself.
func NewConnection(i Invitation, endpoint string) (*Connection, error) {
	transport, err := ParseTransport(string(i.Transport))
	if err != nil {
		return nil, err
	}
	if err = validateInvitation(i); err != nil {
		return nil, err
	}

	tlsConfig := pinnedTLSConfig(i)
	switch transport {
	case TransportLAN:
		if endpoint != "" {
			matched := false
			for _, candidate := range i.Endpoints {
				if endpoint == candidate || endpoint == "https://"+candidate {
					matched = true
					break
				}
			}
			if !matched {
				return nil, fmt.Errorf("保存的局域网地址不属于这份邀请")
			}
			endpoint = strings.TrimPrefix(endpoint, "https://")
		}
		dialer := &net.Dialer{Timeout: 1500 * time.Millisecond}
		roundTripper := &http.Transport{TLSClientConfig: tlsConfig, DialContext: dialer.DialContext}
		url := ""
		if endpoint != "" {
			url = "https://" + endpoint
		}
		return &Connection{
			URL:           url,
			Client:        newHTTPClient(roundTripper),
			Transport:     transport,
			httpTransport: roundTripper,
		}, nil
	case TransportTailcat:
		client := &tailcat.Client{
			Server: tailcat.Addr(i.Tailcat.Address),
			Logf:   func(string, ...any) {},
		}
		roundTripper := &http.Transport{
			TLSClientConfig: tlsConfig,
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return client.DialTCPPort(ctx, i.Tailcat.Port)
			},
		}
		return &Connection{
			URL:           "https://tailcat.invalid",
			Client:        newHTTPClient(roundTripper),
			Transport:     transport,
			httpTransport: roundTripper,
			tailcatClient: client,
		}, nil
	default:
		return nil, fmt.Errorf("不支持的连接方式")
	}
}

func newHTTPClient(roundTripper *http.Transport) *http.Client {
	return &http.Client{
		Timeout:   45 * time.Second,
		Transport: roundTripper,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Connect verifies one transport with a read-only request. Supplying a member
// credential reconnects an existing member; it never replays admission or any
// collaboration write.
func Connect(ctx context.Context, i Invitation, credentials ...string) (*Connection, error) {
	path, credential := "/v2/invitation", i.Secret
	if len(credentials) > 0 {
		path, credential = "/v2/status", credentials[0]
	}
	transport, err := ParseTransport(string(i.Transport))
	if err != nil {
		return nil, err
	}
	candidates := append([]string(nil), i.Endpoints...)
	if transport == TransportTailcat {
		candidates = []string{""}
	}
	for _, endpoint := range candidates {
		if transport == TransportLAN {
			host, _, splitErr := net.SplitHostPort(endpoint)
			if splitErr != nil || net.ParseIP(host) == nil {
				continue
			}
		}
		connection, createErr := NewConnection(i, endpoint)
		if createErr != nil {
			continue
		}
		attemptTimeout := 2500 * time.Millisecond
		if transport == TransportTailcat {
			attemptTimeout = 15 * time.Second
		}
		attempt, cancel := context.WithTimeout(ctx, attemptTimeout)
		request, _ := http.NewRequestWithContext(attempt, http.MethodGet, connection.URL+path, nil)
		request.Header.Set("Authorization", "Bearer "+credential)
		response, requestErr := connection.Client.Do(request)
		cancel()
		if requestErr != nil {
			_ = connection.Close()
			continue
		}
		var remote struct {
			Code  string `json:"code"`
			Error string `json:"error"`
		}
		if response.StatusCode >= 400 {
			_ = json.NewDecoder(response.Body).Decode(&remote)
		}
		_ = response.Body.Close()
		if response.StatusCode == http.StatusOK {
			return connection, nil
		}
		_ = connection.Close()
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusGone {
			if remote.Code == "" {
				remote.Code = "invitation_invalid"
			}
			if remote.Error == "" {
				remote.Error = "邀请已失效或共享已结束"
			}
			return nil, problem.New(remote.Code, remote.Error, "请让发起者重新分享")
		}
	}
	recovery := "请确认双方在同一局域网且发起者仍在共享；连接中断不代表共享结束"
	if transport == TransportTailcat {
		recovery = "请确认双方可以访问 Tailcat DERP 服务且发起者仍在共享；连接中断不代表共享结束"
	}
	return nil, problem.New("host_unreachable", "无法连接协作主机", recovery)
}
