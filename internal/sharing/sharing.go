// Package sharing supplies single-use invitations and pinned-TLS LAN membership.
package sharing

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"teamcross/internal/problem"
	"time"

	"github.com/grandcat/zeroconf"
)

const Capability = "codex-collaboration-v2-membership"

type Invitation struct {
	Version    int       `json:"version"`
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Host       string    `json:"host"`
	Endpoints  []string  `json:"endpoints"`
	Pin        string    `json:"pin"`
	Secret     string    `json:"secret"`
	ExpiresAt  time.Time `json:"expiresAt"`
	Capability string    `json:"capability"`
}
type Runtime struct {
	mu         sync.Mutex
	revoked    bool
	used       bool
	member     string
	memberCtx  context.Context
	cancel     context.CancelFunc
	Invitation Invitation
	server     *http.Server
	mdns       *zeroconf.Server
	once       sync.Once
}

func (r *Runtime) Token() string {
	b, _ := json.Marshal(r.Invitation)
	return "tcx2." + base64.RawURLEncoding.EncodeToString(b)
}
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
		if r.server != nil {
			_ = r.server.Close()
		}
	})
}
func Start(id, title, host string, handler http.Handler, loopback bool) (*Runtime, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	// Admission uses wall time on both Macs, including time spent asleep.
	expiry := time.Now().Add(time.Hour).Round(0)
	tmpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Team Cross " + id}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, _ := x509.ParseCertificate(der)
	pin := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		return nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	endpoints := []string{}
	addrs, _ := net.InterfaceAddrs()
	for _, addr := range addrs {
		ip, _, e := net.ParseCIDR(addr.String())
		if e == nil && ip.To4() != nil && ip.IsPrivate() {
			endpoints = append(endpoints, net.JoinHostPort(ip.String(), fmt.Sprint(port)))
		}
	}
	if loopback {
		endpoints = append(endpoints, net.JoinHostPort("127.0.0.1", fmt.Sprint(port)))
	}
	if len(endpoints) == 0 {
		_ = listener.Close()
		return nil, fmt.Errorf("未找到可分享的局域网地址，请连接局域网")
	}
	r := &Runtime{Invitation: Invitation{Version: 2, ID: id, Title: title, Host: host, Endpoints: endpoints, Pin: hex.EncodeToString(pin[:]), Secret: NewCredential(), ExpiresAt: expiry, Capability: Capability}}
	r.server = &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: r.authorize(handler)}
	tlsListener := tls.NewListener(listener, &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}})
	go func() { _ = r.server.Serve(tlsListener) }()
	r.mdns, _ = zeroconf.Register("tcx-"+id, "_teamcross._tcp", "local.", port, []string{"id=" + id, "pin=" + r.Invitation.Pin, "version=2"}, nil)
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
			if strings.HasPrefix(field, "tcx2.") {
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
	if !strings.HasPrefix(strings.TrimSpace(token), "tcx2.") {
		return i, problem.New("invitation_invalid", "请粘贴新的 Team Cross 协作邀请（tcx2.…）", "可粘贴原始邀请码或 App 链接")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(strings.TrimSpace(token), "tcx2."))
	if err != nil {
		return i, problem.New("invitation_invalid", "邀请格式不完整", "请复制完整邀请码或 App 链接")
	}
	if err = json.Unmarshal(b, &i); err != nil || i.Version != 2 || i.Capability != Capability || i.ID == "" || len(i.Endpoints) == 0 || len(i.Endpoints) > 32 || len(i.Secret) < 32 || len(i.Pin) != 64 {
		return i, problem.New("version_incompatible", "邀请版本或内容不受支持", "请确认双方使用兼容的 Team Cross 版本")
	}
	if time.Now().After(i.ExpiresAt) {
		return i, problem.New("invitation_expired", "邀请已到期", "请让发起者重新分享")
	}
	return i, nil
}
func Client(i Invitation) *http.Client {
	return &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, InsecureSkipVerify: true, VerifyConnection: func(c tls.ConnectionState) error {
		if len(c.PeerCertificates) == 0 {
			return fmt.Errorf("主机没有提供证书")
		}
		pin := sha256.Sum256(c.PeerCertificates[0].RawSubjectPublicKeyInfo)
		if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(pin[:])), []byte(i.Pin)) != 1 {
			return fmt.Errorf("协作主机指纹不匹配")
		}
		return nil
	}}, DialContext: (&net.Dialer{Timeout: 1500 * time.Millisecond}).DialContext}}
}

// Connect discovers an endpoint without admitting a participant or replaying a
// write. With a credential it reconnects an existing member, regardless of TTL.
func Connect(ctx context.Context, i Invitation, credentials ...string) (string, *http.Client, error) {
	client := Client(i)
	path, credential := "/v2/invitation", i.Secret
	if len(credentials) > 0 {
		path, credential = "/v2/status", credentials[0]
	}
	var last error
	for _, endpoint := range i.Endpoints {
		host, _, err := net.SplitHostPort(endpoint)
		if err != nil || net.ParseIP(host) == nil {
			continue
		}
		attempt, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
		req, _ := http.NewRequestWithContext(attempt, "GET", "https://"+endpoint+path, nil)
		req.Header.Set("Authorization", "Bearer "+credential)
		res, err := client.Do(req)
		if err == nil {
			var remote struct {
				Code  string `json:"code"`
				Error string `json:"error"`
			}
			if res.StatusCode >= 400 {
				_ = json.NewDecoder(res.Body).Decode(&remote)
			}
			_ = res.Body.Close()
			cancel()
			if res.StatusCode == 200 {
				return "https://" + endpoint, client, nil
			}
			if res.StatusCode == 401 || res.StatusCode == 403 || res.StatusCode == 410 {
				if remote.Code == "" {
					remote.Code = "invitation_invalid"
				}
				if remote.Error == "" {
					remote.Error = "邀请已失效或共享已结束"
				}
				return "", nil, problem.New(remote.Code, remote.Error, "请让发起者重新分享")
			}
			last = fmt.Errorf("主机返回 %d", res.StatusCode)
		} else {
			cancel()
			last = err
		}
	}
	_ = last
	return "", nil, problem.New("host_unreachable", "无法连接协作主机", "请确认双方在同一局域网且发起者仍在共享；连接中断不代表共享结束")
}
