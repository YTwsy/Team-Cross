package collab

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"teamcross/internal/webassets"
)

// An opt-in fixture uses real Core HTTP/TLS with a simulated native provider.
// It never reads personal sessions, sends model requests, or launches clients.
func TestMembersBrowserFixture(t *testing.T) {
	dir := os.Getenv("TEAMCROSS_MEMBERS_BROWSER_DIR")
	if dir == "" {
		t.Skip("set a fresh TEAMCROSS_MEMBERS_BROWSER_DIR for browser checks")
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal("fixture directory must be new", err)
	}
	a, f, _ := fixture(t)
	a.Host = "A 的 Mac · 界面验证"
	s := createFixture(t, a, f, "existing")
	s.mu.Lock()
	s.record.Title = "接口超时排查 · 多人协作"
	s.mu.Unlock()
	assets, err := fs.Sub(webassets.Dist, "dist")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a.Handler(http.FileServer(http.FS(assets))))
	defer server.Close()
	manifest := map[string]string{"owner": server.URL + "/#/collaborations/" + s.record.ID, "ownerApi": server.URL, "id": s.record.ID}
	for _, name := range []string{"Bob", "Carol"} {
		_, err := s.Invite(context.Background(), "lan", name)
		if err != nil {
			t.Fatal(err)
		}
		app, err := Open(Config{DataDir: t.TempDir(), Binary: "/missing-fixture-codex"})
		if err != nil {
			t.Fatal(err)
		}
		defer app.Close()
		app.Host = name
		j, err := app.Join(context.Background(), s.share.Token())
		if err != nil {
			t.Fatal(err)
		}
		_ = j.request(context.Background(), "POST", "/v2/request_input", map[string]any{"epoch": s.view()["epoch"]}, nil)
		guest := httptest.NewServer(app.Handler(http.FileServer(http.FS(assets))))
		defer guest.Close()
		manifest[name] = guest.URL + "/#/collaborations/" + j.ID
	}
	if err = writeJSONFile(filepath.Join(dir, "fixture.json"), manifest); err != nil {
		t.Fatal(err)
	}
	t.Log("browser fixture ready", dir)
	timeout := time.NewTimer(15 * time.Minute)
	defer timeout.Stop()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-timeout.C:
			t.Fatal("browser fixture expired; resources are closing")
		case <-tick.C:
			if _, err = os.Stat(filepath.Join(dir, "finish")); err == nil {
				return
			}
		}
	}
}
