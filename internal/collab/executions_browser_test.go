package collab

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	reader.mu.Lock()
	reader.nativeUserContent = true
	reader.history = browserProcessHistory("连续命令与工具记录")
	reader.mu.Unlock()
	// Enough materials to exercise sidebar overflow; search can reduce the list
	// without changing the active reader or the navigation's available width.
	for index := range 12 {
		draft, err := a.FreezeSource(context.Background(), "codex", source.ID)
		if err != nil {
			t.Fatal(err)
		}
		draft, err = a.PreviewPublication(PublicationSelection{DraftID: draft.ID, Title: fmt.Sprintf("阅读布局复核 %02d · 命令与工具", index+1), StartTurnID: "turn", EndTurnID: "turn"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.Publish(context.Background(), space.record.ID, PublishInput{PreviewID: draft.ID, PreviewHash: draft.Hash, RequestID: uuid.NewString()}); err != nil {
			t.Fatal(err)
		}
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
		runtime.history = browserProcessHistory(titles[index])
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

func browserProcessHistory(title string) []MaterialTurn {
	items := []MaterialItem{
		{ID: "question", Type: "userMessage", Text: "请核对这份材料，说明本会话需要继续处理的问题。"},
		{ID: "plan", Type: "agentMessage", Text: "我会先核对来源和当前处理路径，再整理验证结果。下方保留检查过程中使用的命令与工具记录。"},
	}
	for index := range 10 {
		kind := "commandExecution"
		if index%2 == 1 {
			kind = "mcpToolCall"
		}
		text := fmt.Sprintf("检查步骤 %02d\n合成输出：已核对当前材料和会话范围。", index+1)
		if index == 2 {
			text += "\n" + strings.Repeat("分页输出：用于核对按需读取与原文位置。\n", 240)
		}
		items = append(items, MaterialItem{ID: fmt.Sprintf("process-%02d", index+1), Type: kind, Text: text})
	}
	items = append(items, MaterialItem{ID: "answer", Type: "agentMessage", Text: "## " + title + "\n\n当前会话有独立的历史、目录和输入归属。\n\n- 核对来源与已确认结论。\n- 记录尚未解决的问题。\n- 将确认后的结果发布为空间材料。\n\n这段内容是浏览器验证使用的合成历史。"})
	return []MaterialTurn{{ID: "turn", Status: "completed", Items: items}}
}
