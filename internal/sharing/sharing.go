// Package sharing supplies reusable space links and pinned-TLS membership
// over explicitly selected LAN or Tailcat transports.
package sharing

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"teamcross/internal/problem"
	"teamcross/internal/runtimeconfig"
	"time"

	"github.com/grandcat/zeroconf"
)

const Capability = "collaboration-spaces-v3-links"

type Transport string

const (
	TransportLAN     Transport = "lan"
	TransportTailcat Transport = "tailcat"
)

func ParseTransport(value string) (Transport, error) {
	switch Transport(strings.ToLower(strings.TrimSpace(value))) {
	case TransportLAN:
		return TransportLAN, nil
	case TransportTailcat:
		return TransportTailcat, nil
	default:
		return "", problem.New("transport_invalid", "连接方式不受支持", "请选择局域网或 Tailcat")
	}
}

type TailcatCandidate struct {
	Address        string `json:"address"`
	Port           uint16 `json:"port"`
	LibraryVersion string `json:"libraryVersion"`
}

type Invitation struct {
	ReadOnly    bool               `json:"readOnly"`
	RuntimeMode runtimeconfig.Mode `json:"runtimeMode,omitempty"`
	Version     int                `json:"version"`
	ID          string             `json:"id"`
	Title       string             `json:"title"`
	Host        string             `json:"host"`
	Transport   Transport          `json:"transport"`
	Endpoints   []string           `json:"endpoints,omitempty"`
	Tailcat     *TailcatCandidate  `json:"tailcat,omitempty"`
	Pin         string             `json:"pin"`
	Secret      string             `json:"secret"`
	ExpiresAt   time.Time          `json:"expiresAt,omitzero"`
	Capability  string             `json:"capability"`
}
type Runtime struct {
	mu              sync.Mutex
	revoked         bool
	invitations     map[string]*admission
	members         map[string]*member
	inviteRequests  map[string]string
	latestInvite    string
	initialExposed  bool
	Invitation      Invitation
	server          *http.Server
	mdns            *zeroconf.Server
	transport       Transport
	transportCloser io.Closer
	once            sync.Once
}

func (r *Runtime) Token() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.initializeLocked()
	r.initialExposed = true
	return encodeInvitation(*r.invitations[r.latestInvite].invitation)
}
func (r *Runtime) Transport() Transport { return r.transport }
func (r *Runtime) Revoke() {
	r.mu.Lock()
	r.revokeLocked()
	r.mu.Unlock()
	time.AfterFunc(30*time.Second, r.Close)
}
func (r *Runtime) Close() {
	r.mu.Lock()
	r.revokeLocked()
	r.mu.Unlock()
	r.once.Do(func() {
		if r.mdns != nil {
			r.mdns.Shutdown()
		}
		if r.transportCloser != nil {
			_ = r.transportCloser.Close()
		}
		if r.server != nil {
			_ = r.server.Close()
		}
	})
}
func Start(ctx context.Context, transport Transport, id, title, host string, handler http.Handler, loopback bool, modes ...runtimeconfig.Mode) (*Runtime, error) {
	mode := runtimeconfig.Restricted
	if len(modes) > 0 {
		var err error
		mode, err = runtimeconfig.Parse(modes[0])
		if err != nil {
			return nil, err
		}
	}
	return StartSpace(ctx, transport, id, title, host, handler, loopback, false, mode)
}
func StartSpace(ctx context.Context, transport Transport, id, title, host string, handler http.Handler, loopback, readOnly bool, mode runtimeconfig.Mode) (*Runtime, error) {
	if readOnly {
		mode = ""
	} else {
		var err error
		mode, err = runtimeconfig.Parse(mode)
		if err != nil {
			return nil, err
		}
	}
	transport, err := ParseTransport(string(transport))
	if err != nil {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Team Cross " + id}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, _ := x509.ParseCertificate(der)
	pin := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	r := &Runtime{
		transport: transport,
		Invitation: Invitation{
			RuntimeMode: mode, ReadOnly: readOnly,
			Version: 3, ID: id, Title: title, Host: host, Transport: transport,
			Pin: hex.EncodeToString(pin[:]), Secret: NewCredential(), Capability: Capability,
		},
	}
	r.server = &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: r.authorize(handler)}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	var listener net.Listener
	switch transport {
	case TransportLAN:
		listener, err = net.Listen("tcp4", "0.0.0.0:0")
		if err != nil {
			return nil, err
		}
		port := listener.Addr().(*net.TCPAddr).Port
		addrs, _ := net.InterfaceAddrs()
		for _, addr := range addrs {
			ip, _, parseErr := net.ParseCIDR(addr.String())
			if parseErr == nil && ip.To4() != nil && ip.IsPrivate() {
				r.Invitation.Endpoints = append(r.Invitation.Endpoints, net.JoinHostPort(ip.String(), fmt.Sprint(port)))
			}
		}
		if loopback {
			r.Invitation.Endpoints = append(r.Invitation.Endpoints, net.JoinHostPort("127.0.0.1", fmt.Sprint(port)))
		}
		if len(r.Invitation.Endpoints) == 0 {
			_ = listener.Close()
			return nil, fmt.Errorf("未找到可分享的局域网地址，请连接局域网")
		}
		r.mdns, _ = zeroconf.Register("tcx-"+id, "_teamcross._tcp", "local.", port, []string{"id=" + id, "pin=" + r.Invitation.Pin, "version=3"}, nil)
	case TransportTailcat:
		tunnelListener := newConnectionListener()
		tailcatHost, address, startErr := startTailcatHost(ctx, tunnelListener)
		if startErr != nil {
			_ = tunnelListener.Close()
			return nil, startErr
		}
		listener = tunnelListener
		r.transportCloser = tailcatHost
		r.Invitation.Tailcat = &TailcatCandidate{Address: address, Port: TailcatVirtualPort, LibraryVersion: TailcatLibraryVersion}
	}
	tlsListener := tls.NewListener(listener, tlsConfig)
	go func() { _ = r.server.Serve(tlsListener) }()
	return r, nil
}
func Decode(token string) (Invitation, error) {
	token = strings.TrimSpace(token)
	if len(token) > 65536 {
		return Invitation{}, problem.New("invitation_invalid", "邀请内容过长", "")
	}
	// Copy-invitation messages contain both a link and the same raw token.
	if strings.ContainsAny(token, "\r\n") {
		candidate := ""
		for _, field := range strings.Fields(token) {
			if strings.HasPrefix(field, "tcx3.") {
				if candidate != "" && candidate != field {
					return Invitation{}, problem.New("invitation_invalid", "发现多个不同邀请", "请只粘贴一个邀请码")
				}
				candidate = field
			}
		}
		if candidate != "" {
			token = candidate
		}
	}
	if strings.HasPrefix(token, "teamcross:") {
		u, e := url.Parse(token)
		if e != nil || u.Scheme != "teamcross" || u.Host != "join" || u.User != nil || u.Path != "" || u.Fragment != "" || len(u.Query()) != 1 || len(u.Query()["invite"]) != 1 {
			return Invitation{}, problem.New("invitation_invalid", "邀请链接格式无效", "请重新复制完整邀请")
		}
		token = u.Query().Get("invite")
	}
	if len(token) > 65536 {
		return Invitation{}, problem.New("invitation_invalid", "邀请内容过长", "")
	}
	var i Invitation
	if !strings.HasPrefix(strings.TrimSpace(token), "tcx3.") {
		return i, problem.New("invitation_invalid", "请粘贴新的 Team Cross 协作邀请（tcx3.…）", "可粘贴原始邀请码或 App 链接")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(strings.TrimSpace(token), "tcx3."))
	if err != nil {
		return i, problem.New("invitation_invalid", "邀请格式不完整", "请复制完整邀请码或 App 链接")
	}
	if err = json.Unmarshal(b, &i); err != nil {
		return i, problem.New("version_incompatible", "邀请版本或内容不受支持", "请确认双方使用兼容的 Team Cross 版本")
	}
	if i.ReadOnly {
		if i.RuntimeMode != "" {
			return i, fmt.Errorf("只读邀请不能含执行权限")
		}
	} else {
		i.RuntimeMode, err = runtimeconfig.Parse(i.RuntimeMode)
	}
	if err != nil {
		return i, problem.New("version_incompatible", "邀请的协作模式不受支持", "请确认双方使用兼容的 Team Cross 版本")
	}
	if err = validateInvitation(i); err != nil {
		return i, problem.New("version_incompatible", "邀请版本或内容不受支持", "请确认双方使用兼容的 Team Cross 版本")
	}
	if !i.ExpiresAt.IsZero() && time.Now().After(i.ExpiresAt) {
		return i, problem.New("invitation_expired", "邀请已到期", "请让发起者重新分享")
	}
	return i, nil
}

func validateInvitation(i Invitation) error {
	if i.Version != 3 || i.Capability != Capability || i.ID == "" || len(i.Secret) < 32 || len(i.Pin) != 64 {
		return fmt.Errorf("邀请基础字段无效")
	}
	transport, err := ParseTransport(string(i.Transport))
	if err != nil {
		return err
	}
	switch transport {
	case TransportLAN:
		if len(i.Endpoints) == 0 || len(i.Endpoints) > 32 || i.Tailcat != nil {
			return fmt.Errorf("局域网候选无效")
		}
		for _, endpoint := range i.Endpoints {
			host, _, splitErr := net.SplitHostPort(endpoint)
			ip := net.ParseIP(host)
			if splitErr != nil || ip == nil || ip.To4() == nil || (!ip.IsPrivate() && !ip.IsLoopback()) {
				return fmt.Errorf("局域网地址无效")
			}
		}
	case TransportTailcat:
		if len(i.Endpoints) != 0 || i.Tailcat == nil || !strings.HasPrefix(i.Tailcat.Address, "tc") || len(i.Tailcat.Address) > 8192 || i.Tailcat.Port != TailcatVirtualPort || i.Tailcat.LibraryVersion != TailcatLibraryVersion || validateTailcatAddress(i.Tailcat.Address) != nil {
			return fmt.Errorf("Tailcat 候选无效")
		}
	}
	return nil
}
