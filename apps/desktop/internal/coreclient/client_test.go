package coreclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"teamcross/internal/buildinfo"
	"teamcross/internal/service"
)

func fixture(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server, service.Connection) {
	t.Helper()
	directory, err := service.Normalize(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return fixtureAt(t, directory, handler)
}

func fixtureAt(t *testing.T, directory string, handler http.HandlerFunc) (*Client, *httptest.Server, service.Connection) {
	t.Helper()
	var connection service.Connection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/control/status" {
			if r.Header.Get("Authorization") != "Bearer test-private-token" || r.Header.Get("Origin") != "" {
				t.Error("invalid native probe")
			}
			public := connection
			public.Token = ""
			json.NewEncoder(w).Encode(service.Status{Connection: public, Running: true})
			return
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	connection = service.Connection{URL: server.URL, Token: "test-private-token", Instance: "fixture", PID: os.Getpid(), DataDir: directory, Protocol: buildinfo.ControlProtocol, Version: buildinfo.Version, Commit: buildinfo.Commit}
	if err := service.Save(directory, connection); err != nil {
		t.Fatal(err)
	}
	client, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	return client, server, connection
}

func request(client *Client, method, path string, body io.Reader) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, body)
	r.Header.Set("Authorization", "Bearer renderer-secret")
	r.Header.Set("Cookie", "renderer-cookie")
	r.Header.Set("Origin", "wails://localhost")
	r.Header.Set("Idempotency-Key", "must-not-replay")
	client.ServeHTTP(w, r)
	return w
}

func TestRelayKeepsCredentialsNative(t *testing.T) {
	var count atomic.Int32
	client, _, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		for _, key := range []string{"Authorization", "Cookie", "Origin", "Idempotency-Key", "X-TeamCross-Desktop"} {
			if r.Header.Get(key) != "" {
				t.Errorf("forwarded %s", key)
			}
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"mode":"zh-Hans"}` || r.URL.RawQuery != "fixture=1" {
			t.Error("lost request")
		}
		w.Header().Set("Set-Cookie", "private")
		io.WriteString(w, `{"mode":"zh-Hans"}`)
	})
	w := request(client, "POST", "/api/ui-language?fixture=1", strings.NewReader(`{"mode":"zh-Hans"}`))
	if w.Code != 200 || w.Header().Get("Set-Cookie") != "" || count.Load() != 1 {
		t.Fatal(w.Code, w.Body.String(), count.Load())
	}
}

func TestRelayRejectsControlAndUnknownRoutesBeforeDiscovery(t *testing.T) {
	client, _ := New(t.TempDir())
	for _, path := range []string{"/api/control/status", "/api/control/stop", "/api/invitations/pending", "/api/mcp/observed", "/api/runtime-annotations/x", "/api/unknown", "/api/%75i-language", "/api/ui-language/../control/stop"} {
		for _, method := range []string{"GET", "POST"} {
			if w := request(client, method, path, nil); w.Code != 404 {
				t.Fatal(method, path, w.Code)
			}
		}
	}
	if w := request(client, "DELETE", "/api/ui-language", nil); w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func TestDiscoveryRejectsMismatchWithoutSendingBusinessRequest(t *testing.T) {
	for _, change := range []string{"url", "directory", "instance", "pid", "protocol", "version", "commit", "permissions", "size"} {
		t.Run(change, func(t *testing.T) {
			client, _, connection := fixture(t, func(http.ResponseWriter, *http.Request) { t.Error("business request reached mismatched Core") })
			switch change {
			case "url":
				connection.URL = "http://example.com:43210"
			case "directory":
				connection.DataDir += "-other"
			case "instance":
				connection.Instance += "-other"
			case "pid":
				connection.PID++
			case "protocol":
				connection.Protocol++
			case "version":
				connection.Version += "-other"
			case "commit":
				connection.Commit += "-other"
			}
			if err := service.Save(client.directory, connection); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(client.directory, "connection.json")
			if change == "permissions" {
				if err := os.Chmod(file, 0644); err != nil {
					t.Fatal(err)
				}
			}
			if change == "size" {
				if err := os.WriteFile(file, []byte(strings.Repeat(" ", 17<<10)), 0600); err != nil {
					t.Fatal(err)
				}
			}
			w := request(client, "GET", "/api/ui-language", nil)
			if w.Code != 503 || strings.Contains(w.Body.String(), connection.Token) {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}

func TestMutationFailureIsNotReplayedAndNextReadRediscovers(t *testing.T) {
	var writes atomic.Int32
	client, first, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		writes.Add(1)
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		connection.Close() // Server accepted the write, but its reply was lost.
	})
	if w := request(client, "POST", "/api/ui-language", nil); w.Code != 503 || writes.Load() != 1 {
		t.Fatal(w.Code, writes.Load())
	}
	first.Close()
	if w := request(client, "GET", "/api/ui-language", nil); w.Code != 503 {
		t.Fatal(w.Code)
	}
	fixtureAt(t, client.directory, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"mode":"auto"}`) })
	if w := request(client, "GET", "/api/ui-language", nil); w.Code != 200 || writes.Load() != 1 {
		t.Fatal(w.Code, writes.Load())
	}
}

func TestRedirectAndHTMLAreNotForwarded(t *testing.T) {
	for _, mode := range []string{"redirect", "html"} {
		t.Run(mode, func(t *testing.T) {
			client, _, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if mode == "redirect" {
					http.Redirect(w, r, "https://example.com/secret", 302)
					return
				}
				io.WriteString(w, "<html>not an API response</html>")
			})
			if w := request(client, "GET", "/api/ui-language", nil); w.Code != 503 || w.Header().Get("Location") != "" {
				t.Fatal(w.Code)
			}
		})
	}
}

func TestRendererCancellationClosesUpstream(t *testing.T) {
	started, cancelled := make(chan struct{}), make(chan struct{})
	client, _, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		client.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/ui-language", nil).WithContext(ctx))
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	for _, signal := range []chan struct{}{cancelled, done} {
		select {
		case <-signal:
		case <-time.After(3 * time.Second):
			t.Fatal("request did not cancel")
		}
	}
}
