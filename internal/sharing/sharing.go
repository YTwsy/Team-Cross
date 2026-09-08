// Package sharing supplies temporary, pinned-TLS LAN invitations.
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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/grandcat/zeroconf"
)

const Capability = "codex-collaboration-v2"

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
	revoked    atomic.Bool
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
	r.revoked.Store(true)
	time.AfterFunc(30*time.Second, r.Close)
}
func (r *Runtime) Close() {
	r.revoked.Store(true)
	r.once.Do(func() {
		if r.mdns != nil {
			r.mdns.Shutdown()
		}
		_ = r.server.Close()
	})
}
func Start(id, title, host string, handler http.Handler, loopback bool) (*Runtime, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	expiry := time.Now().Add(time.Hour)
	tmpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Team Cross " + id}, NotBefore: time.Now().Add(-time.Minute), NotAfter: expiry, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
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
	secret := make([]byte, 32)
	_, _ = rand.Read(secret)
	r := &Runtime{Invitation: Invitation{Version: 2, ID: id, Title: title, Host: host, Endpoints: endpoints, Pin: hex.EncodeToString(pin[:]), Secret: base64.RawURLEncoding.EncodeToString(secret), ExpiresAt: expiry, Capability: Capability}}
	r.server = &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r.revoked.Load() {
			http.Error(w, "发起者已结束共享，请使用新的邀请", http.StatusGone)
			return
		}
		if time.Now().After(expiry) {
			http.Error(w, "邀请已到期", http.StatusGone)
			return
		}
		if subtle.ConstantTimeCompare([]byte(req.Header.Get("Authorization")), []byte("Bearer "+r.Invitation.Secret)) != 1 {
			http.Error(w, "邀请无效", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, req)
	})}
	tlsListener := tls.NewListener(listener, &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}})
	go func() { _ = r.server.Serve(tlsListener) }()
	r.mdns, _ = zeroconf.Register("tcx-"+id, "_teamcross._tcp", "local.", port, []string{"id=" + id, "pin=" + r.Invitation.Pin, "version=2"}, nil)
	return r, nil
}
func Decode(token string) (Invitation, error) {
	var i Invitation
	if !strings.HasPrefix(strings.TrimSpace(token), "tcx2.") {
		return i, fmt.Errorf("请粘贴新的 Team Cross 协作邀请（tcx2.…）")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(strings.TrimSpace(token), "tcx2."))
	if err != nil {
		return i, fmt.Errorf("邀请格式不完整")
	}
	if err = json.Unmarshal(b, &i); err != nil || i.Version != 2 || i.Capability != Capability || i.ID == "" || len(i.Endpoints) == 0 || len(i.Endpoints) > 32 || len(i.Secret) < 32 || len(i.Pin) != 64 {
		return i, fmt.Errorf("邀请版本或内容不受支持")
	}
	if time.Now().After(i.ExpiresAt) {
		return i, fmt.Errorf("邀请已到期，请让发起者重新分享")
	}
	return i, nil
}
func Client(i Invitation) *http.Client {
	return &http.Client{Timeout: 45 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, InsecureSkipVerify: true, VerifyConnection: func(c tls.ConnectionState) error {
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
func Connect(ctx context.Context, i Invitation) (string, *http.Client, error) {
	client := Client(i)
	var last error
	for _, endpoint := range i.Endpoints {
		host, _, err := net.SplitHostPort(endpoint)
		if err != nil || net.ParseIP(host) == nil {
			continue
		}
		attempt, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
		req, _ := http.NewRequestWithContext(attempt, "GET", "https://"+endpoint+"/v2/status", nil)
		req.Header.Set("Authorization", "Bearer "+i.Secret)
		res, err := client.Do(req)
		if err == nil {
			_ = res.Body.Close()
			cancel()
			if res.StatusCode == 200 {
				return "https://" + endpoint, client, nil
			}
			if res.StatusCode == 401 || res.StatusCode == 403 || res.StatusCode == 410 {
				return "", nil, fmt.Errorf("邀请已失效，请让发起者重新分享")
			}
			last = fmt.Errorf("主机返回 %d", res.StatusCode)
		} else {
			cancel()
			last = err
		}
	}
	return "", nil, fmt.Errorf("无法连接协作主机；请确认双方在同一局域网且主机在线：%v", last)
}
