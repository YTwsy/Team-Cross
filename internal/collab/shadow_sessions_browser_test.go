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
	"teamcross/internal/shadow"
	"teamcross/internal/webassets"
)

// Real Core and production assets; the session/task snapshots are synthetic.
// This fixture never creates a native thread or calls a model.
func TestShadowSessionsBrowserFixture(t *testing.T) {
	dir := os.Getenv("TEAMCROSS_SESSIONS_BROWSER_DIR")
	if dir == "" {
		t.Skip("set a fresh TEAMCROSS_SESSIONS_BROWSER_DIR")
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a, _, s := spaceFixture(t)
	a.shadowCancel()
	a.shadowWG.Wait()
	if err := a.SetUILanguage("zh-CN"); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.record.Title = "会话与任务 · 合成状态验收"
	s.mu.Unlock()
	v := workbenchDecode[shadow.View](shadowOp(t, a, s.record.ID, "register", shadowInput{ID: uuid.NewString(), Name: "会话结构测试分身"}))
	now := time.Now().UTC()
	closed := now.Add(-time.Hour)
	v.Current, v.Online, v.Revision = "fixture-s2", true, 1
	v.Settings = shadow.ModelSettings{Model: "fixture-model", ModelProvider: "fixture", ReasoningEffort: "low"}
	v.Segments = []shadow.Segment{
		{ID: "fixture-s1", Number: 1, CreatedAt: now.Add(-2 * time.Hour), ClosedAt: &closed, Compactions: 2},
		{ID: "fixture-s2", Number: 2, CreatedAt: now.Add(-time.Hour), InputTokens: 19200, ContextWindow: 100000, UsageValid: true},
	}
	v.Jobs = []shadow.Job{
		{ID: "fixture-j1", Kind: "work", Title: "核对多人连续输入的来源", State: "running", Version: 1, ThreadID: "fixture-w1", CreatedAt: now.Add(-40 * time.Minute), CompletionCriteria: []string{"每位成员的回复返回原入口"}, Checkpoint: &shadow.TaskCheckpoint{Summary: "已核对两位成员的来源，正在检查第三位。", NextStep: "核对并发到达的回复"}},
		{ID: "fixture-j2", Kind: "work", Title: "整理发布说明", State: "waiting", Version: 2, ThreadID: "fixture-w2", CreatedAt: now.Add(-30 * time.Minute), Checkpoint: &shadow.TaskCheckpoint{Summary: "草稿已保存，等待最低客户端版本。"}},
		{ID: "fixture-j3", Kind: "work", Title: "检查共享材料引用", State: "completed", Version: 1, ThreadID: "fixture-w3", CreatedAt: now.Add(-20 * time.Minute), Result: "合成验收结果：材料固定版本与讨论来源均可对应。"},
		{ID: "fixture-j4", Kind: "work", Title: "补充测试清单", State: "queued", Version: 1, CreatedAt: now.Add(-10 * time.Minute)},
	}
	v.Memories = []shadow.Memory{{ID: "fixture-memory", Text: "先给出结论，再列出核对依据。", Scope: "space", State: "active", Revision: 1}}
	v.Messages = []shadow.Message{{ID: "fixture-answer", Actor: shadow.Actor{ID: v.ID, Name: v.Name}, Role: "assistant", State: "completed", Text: "这是合成状态界面验收，没有真实模型执行。打开“会话与任务”查看 S2、运行中的 W1、等待中的 W2、已完成的 W3 和尚未创建会话的任务。", CreatedAt: now}}
	shadowOp(t, a, s.record.ID, "sync", shadowInput{ID: v.ID, Snapshot: &v})
	assets, err := fs.Sub(webassets.Dist, "dist")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a.Handler(http.FileServer(http.FS(assets))))
	defer server.Close()
	if err := writeJSONFile(filepath.Join(dir, "fixture.json"), map[string]string{"url": server.URL + "/#/collaborations/" + s.record.ID, "api": server.URL, "spaceId": s.record.ID, "shadowId": v.ID}); err != nil {
		t.Fatal(err)
	}
	t.Log("session browser fixture ready", dir)
	timer := time.NewTimer(25 * time.Minute)
	defer timer.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-timer.C:
			t.Fatal("fixture timed out")
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(dir, "finish")); err == nil {
				return
			}
			// Keep the synthetic owner heartbeat current without running a model.
			if _, err := a.shadowCall(context.Background(), s.record.ID, "sync", shadowInput{ID: v.ID, Snapshot: &v}); err != nil {
				t.Fatal(err)
			}
		}
	}
}
