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

// Real Core/HTTP/storage and three memberships; provider history is synthetic.
func TestMultipleExecutionsBrowserFixture(t *testing.T) {
	dir := os.Getenv("TEAMCROSS_MULTI_BROWSER_DIR")
	if dir == "" {
		t.Skip("set a fresh TEAMCROSS_MULTI_BROWSER_DIR")
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a, reader, space := multiExecutionFixture(t)
	a.Host = "多会话验证 Mac"
	_ = a.SetUILanguage("zh-CN")
	space.mu.Lock()
	space.record.Title = "连接池调查 · 协作空间"
	space.mu.Unlock()
	source := reader.source
	draft := materialDraft(t, a, reader)
	if _, err := a.Publish(context.Background(), space.record.ID, PublishInput{PreviewID: draft.ID, PreviewHash: draft.Hash, RequestID: uuid.NewString()}); err != nil {
		t.Fatal(err)
	}
	reader.mu.Lock()
	reader.source = source
	reader.mu.Unlock()
	one := createInSpace(t, a, reader, space, "worktree")
	two := createInSpace(t, a, reader, space, "worktree")
	for index, s := range []*Session{one, two} {
		titles := []string{"定位连接池等待", "复核并发与断线"}
		s.mu.Lock()
		s.record.Title = titles[index]
		_ = s.saveLocked()
		runtime := s.process.(*fakeRuntime)
		s.mu.Unlock()
		runtime.mu.Lock()
		runtime.nativeUserContent = true
		runtime.history = []MaterialTurn{{ID: "turn", Status: "completed", Items: []MaterialItem{
			{ID: "question", Type: "userMessage", Text: "请核对这份材料，说明本会话需要继续处理的问题。"},
			{ID: "answer", Type: "agentMessage", Text: "## " + titles[index] + "\n\n当前会话有独立的历史、目录和输入归属。\n\n- 核对来源与已确认结论。\n- 记录尚未解决的问题。\n- 将确认后的结果发布为空间材料。\n\n这段内容是浏览器验证使用的合成历史。"},
		}}}
		runtime.mu.Unlock()
	}
	_, _ = one.annotate(Annotation{Text: "这里讨论定位思路，所有空间成员均可参与。"}, "发起者")
	_, _ = space.annotate(Annotation{Text: "空间讨论与会话中的实时上下文分别管理。"}, "发起者")
	if err := space.Share(context.Background(), "lan"); err != nil {
		t.Fatal(err)
	}
	bob, carol := joinSpace(t, space, "成员 B 的 Mac"), joinSpace(t, space, "成员 C 的 Mac")

	assets, err := fs.Sub(webassets.Dist, "dist")
	if err != nil {
		t.Fatal(err)
	}
	manifest := map[string]string{"spaceId": space.record.ID, "first": one.record.ID, "second": two.record.ID}
	for _, entry := range []struct {
		name string
		app  *App
		id   string
	}{{"owner", a, space.record.ID}, {"bob", bob.app, bob.ID}, {"carol", carol.app, carol.ID}} {
		_ = entry.app.SetUILanguage("zh-CN")
		server := httptest.NewServer(entry.app.Handler(http.FileServer(http.FS(assets))))
		defer server.Close()
		manifest[entry.name] = server.URL + "/#/collaborations/" + entry.id + "/sessions/" + one.record.ID
		manifest[entry.name+"Api"] = server.URL
	}
	if err = writeJSONFile(filepath.Join(dir, "fixture.json"), manifest); err != nil {
		t.Fatal(err)
	}
	t.Log("multi-session fixture ready", dir)
	deadline := time.NewTimer(25 * time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("browser fixture expired")
		case <-tick.C:
			if _, err := os.Stat(filepath.Join(dir, "finish")); err == nil {
				return
			}
		}
	}
}
