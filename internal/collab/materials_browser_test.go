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

	"github.com/google/uuid"
	"teamcross/internal/webassets"
)

// Real browser -> three independent Cores -> TLS -> persisted publications.
// Native source history is simulated; no real user sessions/model calls occur.
func TestMaterialsBrowserFixture(t *testing.T) {
	dir := os.Getenv("TEAMCROSS_SPACE_BROWSER_DIR")
	if dir == "" {
		t.Skip("set a fresh TEAMCROSS_SPACE_BROWSER_DIR")
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	assets, err := fs.Sub(webassets.Dist, "dist")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	manifest := map[string]string{}
	a, af, _ := fixture(t)
	a.Host = "A 的 Mac · 空间验证"
	prepare := func(f *fakeRuntime, name string) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.source.Name = name + " · 接口超时调查"
		f.source.Preview = "连接池与服务端耗时的交叉验证"
		f.history = []MaterialTurn{
			{ID: "verification", Status: "completed", Items: []MaterialItem{{ID: "final", Type: "agentMessage", Text: "复测通过：两组数据中，连接池等待下降，服务端耗时保持稳定。建议在此次发布中保留验证条件。"}}},
			{ID: "analysis", Status: "completed", Items: []MaterialItem{{ID: "analysis", Type: "agentMessage", Text: "观察到超时集中在请求获取连接之前。可以先对照连接池等待与服务端耗时，排除业务逻辑本身。"}, {ID: "tool", Type: "toolResult", Text: "样本：请求 200 次；连接池等待 p95 420 ms；服务端 p95 30 ms。"}}},
			{ID: "private", Status: "completed", Items: []MaterialItem{{ID: "private", Type: "agentMessage", Text: "仅本机可见：另外一个客户的无关问题，不应纳入本次分享。"}}},
		}
	}
	prepare(af, "A")
	s, err := a.CreateSpace(ctx, SpaceInput{RequestID: uuid.NewString(), Title: "接口超时调查 · 会话材料协作"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := a.FreezeSource(ctx, "codex", af.source.ID)
	if err != nil {
		t.Fatal(err)
	}
	d, err = a.PreviewPublication(PublicationSelection{DraftID: d.ID, Title: "A 的连接池分析", StartTurnID: "analysis", EndTurnID: "verification", ReadingStartID: "analysis"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Publish(ctx, s.record.ID, PublishInput{PreviewID: d.ID, PreviewHash: d.Hash, RequestID: uuid.NewString()}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a.Handler(http.FileServer(http.FS(assets))))
	defer server.Close()
	manifest["owner"], manifest["ownerApi"], manifest["id"] = server.URL+"/#/collaborations/"+s.record.ID, server.URL, s.record.ID
	for _, name := range []string{"unshared", "unjoined"} {
		empty, err := a.CreateSpace(ctx, SpaceInput{RequestID: uuid.NewString(), Title: "尚无成员 · " + name})
		if err != nil {
			t.Fatal(err)
		}
		if name == "unjoined" {
			if err = empty.Share(ctx, "lan"); err != nil {
				t.Fatal(err)
			}
		}
		manifest[name] = server.URL + "/#/collaborations/" + empty.record.ID
	}
	for _, name := range []string{"Bob", "Carol"} {
		inv, err := s.Invite(ctx, "lan", uuid.NewString())
		if err != nil {
			t.Fatal(err)
		}
		app, f, _ := fixture(t)
		app.Host = name
		prepare(f, name)
		j, err := app.Join(ctx, inv.Token)
		if err != nil {
			t.Fatal(err)
		}
		guest := httptest.NewServer(app.Handler(http.FileServer(http.FS(assets))))
		defer guest.Close()
		manifest[name] = guest.URL + "/#/collaborations/" + j.ID
		manifest[name+"Api"], manifest[name+"ID"] = guest.URL, j.ID
	}
	if err = writeJSONFile(filepath.Join(dir, "fixture.json"), manifest); err != nil {
		t.Fatal(err)
	}
	t.Log("materials browser fixture ready", dir)
	timeout := time.NewTimer(15 * time.Minute)
	defer timeout.Stop()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-timeout.C:
			t.Fatal("browser fixture expired; closing resources")
		case <-tick.C:
			if _, err = os.Stat(filepath.Join(dir, "finish")); err == nil {
				return
			}
		}
	}
}
