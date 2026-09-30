// Package coreclient connects the desktop to an independently running Core.
// It does not start runtimes or make collaboration decisions.
package coreclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"teamcross/internal/buildinfo"
	"teamcross/internal/service"
)

const maxResponse = 32 << 20

var errConnection = errors.New("Core connection is unavailable or incompatible")

type Client struct {
	directory string
	http      *http.Client
}

func New(directory string) (*Client, error) {
	directory, err := service.Normalize(directory)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	// Fresh connections also prevent net/http from replaying a bodyless POST on
	// a reused connection. All reconnects happen on the next explicit request.
	transport.DisableKeepAlives = true
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || host != "127.0.0.1" {
			return nil, errConnection
		}
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp4", net.JoinHostPort(host, port))
	}
	return &Client{directory: directory, http: &http.Client{
		Transport: transport, Timeout: 50 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (c *Client) probe(ctx context.Context) (service.Status, error) {
	var connection service.Connection
	f, err := os.Open(filepath.Join(c.directory, "connection.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return service.Status{}, errMissing
		}
		return service.Status{}, errConnection
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16<<10 || info.Mode().Perm()&0077 != 0 {
		return service.Status{}, errConnection
	}
	if json.NewDecoder(io.LimitReader(f, 16<<10)).Decode(&connection) != nil {
		return service.Status{}, errConnection
	}
	u, err := url.Parse(connection.URL)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" ||
		u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" ||
		connection.DataDir != c.directory || connection.PID <= 0 || connection.Instance == "" ||
		connection.Token == "" || strings.ContainsAny(connection.Token, "\r\n") ||
		connection.Protocol != buildinfo.ControlProtocol || connection.Version != buildinfo.Version || connection.Commit != buildinfo.Commit {
		return service.Status{}, errConnection
	}
	probe, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probe, http.MethodGet, connection.URL+"/api/control/status", nil)
	if err != nil {
		return service.Status{}, errConnection
	}
	req.Header.Set("Authorization", "Bearer "+connection.Token)
	res, err := c.http.Do(req)
	if err != nil {
		return service.Status{Connection: connection}, errUnreachable
	}
	defer res.Body.Close()
	var status service.Status
	if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, 16<<10)).Decode(&status) != nil ||
		!status.Running || status.URL != connection.URL || status.PID != connection.PID || status.Instance != connection.Instance ||
		status.DataDir != connection.DataDir || status.Protocol != connection.Protocol || status.Version != connection.Version || status.Commit != connection.Commit {
		return service.Status{}, errConnection
	}
	status.Token = connection.Token
	return status, nil
}

func (c *Client) discover(ctx context.Context) (service.Connection, error) {
	status, err := c.probe(ctx)
	return status.Connection, err
}

var collaborationPath = regexp.MustCompile(`^/api/collaborations/[A-Za-z0-9-]+(?:/([a-z-]+))?$`)

// Allowed mirrors the WebGUI surface, not Core control or runtime-only APIs.
func Allowed(method, path string) bool {
	switch path {
	case "/api/ui-language", "/api/ui-theme", "/api/collaborations":
		return method == http.MethodGet || method == http.MethodPost
	case "/api/info", "/api/sources", "/api/library":
		return method == http.MethodGet
	case "/api/settings", "/api/mcp/setup", "/api/mcp/probe", "/api/preview", "/api/spaces", "/api/join", "/api/invitations/preview",
		"/api/publications/source", "/api/publications/preview", "/api/publications/draft", "/api/publications/read-draft",
		"/api/library/state", "/api/library/read", "/api/library/bundles", "/api/library/prepare-send", "/api/library/read-selection":
		return method == http.MethodPost
	}
	if match := collaborationPath.FindStringSubmatch(path); match != nil {
		switch match[1] {
		case "", "context":
			return method == http.MethodGet
		case "materials":
			return method == http.MethodGet || method == http.MethodPost
		case "read-material", "withdraw-material", "publication-status", "action", "invitations", "revoke-invitation", "remove-member", "execution-access", "personal-desktop", "open", "assist", "rpc", "respond", "annotations", "annotation-replies":
			return method == http.MethodPost
		}
	}
	return false
}

func unavailable(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = io.WriteString(w, `{"error":"本机服务连接已中断","code":"core_unreachable","recovery":"请重新连接后先查看操作结果，避免重复提交"}`)
}

func (c *Client) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !Allowed(r.Method, r.URL.Path) || r.URL.RawPath != "" {
		http.NotFound(w, r)
		return
	}
	connection, err := c.discover(r.Context())
	if err != nil {
		unavailable(w)
		return
	}
	// Never copy renderer credentials, Origin, forwarding, or idempotency headers.
	// A native control credential is used only by discover above.
	u, _ := url.Parse(connection.URL)
	u.Path, u.RawQuery = r.URL.Path, r.URL.RawQuery
	input, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil || len(input) > 1<<20 {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, u.String(), strings.NewReader(string(input)))
	if err != nil {
		unavailable(w)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		unavailable(w)
		return
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 && res.StatusCode < 400 {
		unavailable(w)
		return
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
	if err != nil || len(body) > maxResponse {
		unavailable(w)
		return
	}
	// Error responses are JSON too. Do not relay cookies, redirects, or HTML.
	if !json.Valid(body) {
		unavailable(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(res.StatusCode)
	_, _ = w.Write(body)
}
