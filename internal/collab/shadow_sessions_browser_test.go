package collab

import (
	"context"
	"fmt"
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
	s.record.Title = "产品工作间 · 合成界面验收"
	s.mu.Unlock()
	v := workbenchDecode[shadow.View](shadowOp(t, a, s.record.ID, "register", shadowInput{ID: uuid.NewString(), Name: "工程助手"}))
	offline := workbenchDecode[shadow.View](shadowOp(t, a, s.record.ID, "register", shadowInput{ID: uuid.NewString(), Name: "资料助手"}))
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
	v.Memories = []shadow.Memory{
		{ID: "fixture-memory", Text: "先给出结论，再列出核对依据。", Scope: "space", State: "active", Revision: 1},
		{ID: "fixture-preference", Text: "界面以浅色为主要设计基准，同时支持深色。", Scope: "member", SubjectID: v.Owner.ID, State: "active", Revision: 2},
		{ID: "fixture-candidate", Text: "发布说明中的技术细节采用折叠展示。", Scope: "space", State: "candidate", Revision: 1},
	}
	for i := 0; i < 12; i++ {
		at := now.Add(time.Duration(i-12) * time.Minute)
		v.Messages = append(v.Messages,
			shadow.Message{ID: fmt.Sprintf("fixture-user-%d", i), Actor: v.Owner, Role: "user", State: "completed", Text: "帮我梳理一下当前的工作进展，重点说明需要我确认的内容。", CreatedAt: at, ReceivedAt: &at},
			shadow.Message{ID: fmt.Sprintf("fixture-answer-%d", i), Actor: shadow.Actor{ID: v.ID, Name: v.Name}, Role: "assistant", State: "completed", Text: "已整理好当前进展，后台核对仍在继续。\n\n### 现在可以继续推进\n\n- **多人输入来源**：已核对两位成员，正在检查第三位。\n- **发布说明**：草稿已经保存，等待最低客户端版本。\n- **共享材料**：固定版本与讨论来源已对应。\n\n你可以继续讨论下一步，也可以在右侧查看任务和记忆。这里展示的是合成验收数据，没有调用真实模型。", CreatedAt: at.Add(time.Second)},
		)
	}
	// Seed authenticated delivery records for the synthetic member messages.
	// The production sync path still verifies authors against these records.
	s.mu.Lock()
	entry := s.record.Shadows[v.ID]
	for _, message := range v.Messages {
		if message.Role == "user" {
			entry.Deliveries[message.ID] = ShadowDelivery{Message: message, State: "delivered"}
		}
	}
	s.record.Shadows[v.ID] = entry
	s.mu.Unlock()
	shadowOp(t, a, s.record.ID, "sync", shadowInput{ID: v.ID, Snapshot: &v})
	assets, err := fs.Sub(webassets.Dist, "dist")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a.Handler(http.FileServer(http.FS(assets))))
	defer server.Close()
	if err := writeJSONFile(filepath.Join(dir, "fixture.json"), map[string]string{"url": server.URL + "/#/collaborations/" + s.record.ID, "api": server.URL, "spaceId": s.record.ID, "shadowId": v.ID, "offlineShadowId": offline.ID}); err != nil {
		t.Fatal(err)
	}
	t.Log("session browser fixture ready", dir)
	timer := time.NewTimer(25 * time.Minute)
	defer timer.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastAppend := ""
	for {
		select {
		case <-timer.C:
			t.Fatal("fixture timed out")
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(dir, "finish")); err == nil {
				return
			}
			if command, err := os.ReadFile(filepath.Join(dir, "append")); err == nil && string(command) != lastAppend {
				lastAppend = string(command)
				v.Revision++
				v.Messages = append(v.Messages, shadow.Message{ID: uuid.NewString(), Actor: shadow.Actor{ID: v.ID, Name: v.Name}, Role: "assistant", State: "completed", Text: "合成动态更新 · " + lastAppend, CreatedAt: time.Now().UTC()})
			}
			// Keep the synthetic owner heartbeat current without running a model.
			if _, err := a.shadowCall(context.Background(), s.record.ID, "sync", shadowInput{ID: v.ID, Snapshot: &v}); err != nil {
				t.Fatal(err)
			}
		}
	}
}
