package coreclient

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"teamcross/internal/service"
)

func TestNativeStatusRemovesCredentialAndDistinguishesStartingCore(t *testing.T) {
	c, _, _ := fixture(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected business call") })
	s, err := c.Status(context.Background())
	if err != nil || !s.Running || s.Token != "" {
		t.Fatal(s.Running, err)
	}
	os.Remove(filepath.Join(c.directory, "connection.json"))
	lock, err := service.Lock(c.directory, "core.lock")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Status(context.Background()); err == nil {
		t.Fatal("starting Core reported stopped")
	}
	lock.Close()
	if s, err = c.Status(context.Background()); err != nil || s.Running {
		t.Fatal(s.Running, err)
	}
}

func TestStopChecksConfirmedIdentityAndDoesNotForceInactiveCore(t *testing.T) {
	var calls atomic.Int32
	c, _, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/api/control/stop" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-private-token" || r.Header.Get("Origin") != "" || string(body) != `{"force":false}` {
			t.Error("invalid stop request")
		}
		// A collaboration started after the inactive snapshot: Core refuses it.
		w.WriteHeader(409)
		io.WriteString(w, `{"error":"active"}`)
	})
	s, err := c.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	changed := s
	changed.Instance = "replacement"
	if err = c.Stop(context.Background(), changed, true); err != errChanged || calls.Load() != 0 {
		t.Fatal(err, calls.Load())
	}
	if err = c.Stop(context.Background(), s, false); err != errStop || calls.Load() != 1 {
		t.Fatal(err, calls.Load())
	}
}

func TestStopWaitsForCoreLockAndPreservesUserFiles(t *testing.T) {
	accepted := make(chan struct{})
	c, _, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"force":true}` {
			t.Error("confirmation lost")
		}
		io.WriteString(w, `{"stopping":true}`)
		close(accepted)
	})
	lock, err := service.Lock(c.directory, "core.lock")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	file := filepath.Join(c.directory, "user-owned.txt")
	if err = os.WriteFile(file, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	s, _ := c.Status(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- c.Stop(ctx, s, true) }()
	<-accepted
	select {
	case err := <-result:
		t.Fatal("returned before Core released lock", err)
	case <-time.After(80 * time.Millisecond):
	}
	lock.Close()
	if err = <-result; err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(file); err != nil || string(b) != "preserve" {
		t.Fatal("user file changed")
	}
}

func TestStopDroppedResponseIsNotReplayed(t *testing.T) {
	var calls atomic.Int32
	c, _, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		conn.Close()
	})
	s, _ := c.Status(context.Background())
	if err := c.Stop(context.Background(), s, true); err != errStop || calls.Load() != 1 {
		t.Fatal(err, calls.Load())
	}
}

func TestCoreLockSymlinkIsNotUsed(t *testing.T) {
	c, _ := New(t.TempDir())
	outside := filepath.Join(t.TempDir(), "outside")
	os.WriteFile(outside, []byte("keep"), 0600)
	os.Symlink(outside, filepath.Join(c.directory, "core.lock"))
	if _, err := c.Status(context.Background()); err == nil {
		t.Fatal("followed Core lock symlink")
	}
	if b, _ := os.ReadFile(outside); !strings.EqualFold(string(b), "keep") {
		t.Fatal("outside file changed")
	}
}

func TestUnreachableLiveProcessIsNotReportedStopped(t *testing.T) {
	c, server, _ := fixture(t, func(http.ResponseWriter, *http.Request) {})
	server.Close()
	s, err := c.Status(context.Background())
	if err == nil || s.Token != "" {
		t.Fatal("live unreachable Core was hidden or credential exposed")
	}
}

func TestCrashedCoreWithStaleDiscoveryCanQuit(t *testing.T) {
	c, server, connection := fixture(t, func(http.ResponseWriter, *http.Request) {})
	server.Close()
	connection.PID = 1 << 30
	if err := service.Save(c.directory, connection); err != nil {
		t.Fatal(err)
	}
	s, err := c.Status(context.Background())
	if err != nil || s.Running {
		t.Fatal("dead Core with free lock blocked quit", err)
	}
}

func TestStopTimeoutDoesNotReleaseCoreLockOrReplay(t *testing.T) {
	var calls atomic.Int32
	c, _, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); io.WriteString(w, `{"stopping":true}`) })
	lock, err := service.Lock(c.directory, "core.lock")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	s, _ := c.Status(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if err = c.Stop(ctx, s, true); err != errStop || calls.Load() != 1 {
		t.Fatal(err, calls.Load())
	}
	if idle, _ := c.coreIdle(); idle {
		t.Fatal("timeout took ownership of Core lock")
	}
}
