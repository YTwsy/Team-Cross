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

// Browser-only synthetic conversations with real Core, membership and storage.
// Model-level transport is verified separately by verify-space-workbench.py.
func TestWorkbenchBrowserFixture(t *testing.T) {
	dir := os.Getenv("TEAMCROSS_WORKBENCH_BROWSER_DIR")
	if dir == "" {
		t.Skip("set a new TEAMCROSS_WORKBENCH_BROWSER_DIR")
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	a, f, s := spaceFixture(t)
	a.Host = "工作台验证 Mac"
	_ = a.SetUILanguage("zh-CN")
	s.mu.Lock()
	s.record.Title = "接口超时调查 · 协作空间"
	s.mu.Unlock()
	d := materialDraft(t, a, f)
	pub, err := a.Publish(ctx, s.record.ID, PublishInput{PreviewID: d.ID, PreviewHash: d.Hash, RequestID: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	mid := pub.(map[string]any)["materialId"].(string)
	ref := LibraryReference{SpaceID: s.record.ID, Kind: "material", MaterialID: mid, Version: 1}
	note, err := s.annotate(Annotation{Text: "这组验证是否覆盖重试和断线恢复？", Materials: []MaterialReference{{MaterialID: mid, Version: 1}}}, "发起者")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Share(ctx, "lan"); err != nil {
		t.Fatal(err)
	}
	b, _, _ := fixture(t)
	b.Host = "同事 B 的 Mac"
	stopWorkbenchPump(b)
	j, err := b.Join(ctx, s.share.Token())
	if err != nil {
		t.Fatal(err)
	}
	_, caller, target := spaceChannel(t, b, j.ID)
	_, _, ownTarget := spaceChannel(t, a, s.record.ID)
	wb(t, a, s.record.ID, "brief", workbenchInput{Brief: SpaceBrief{Topic: "确认重试、断线与并发场景中的协作行为，再决定是否进入共同执行。", Decisions: []BriefItem{{Text: "只共享已发布的固定材料版本；私人会话保持在各自客户端。", Sources: []LibraryReference{ref}}}, Questions: []BriefItem{{Text: "投递结果不明时，怎样避免启动第二轮输入？", Sources: []LibraryReference{{SpaceID: s.record.ID, Kind: "annotation", AnnotationID: note.ID}}}}}})
	requestID := uuid.NewString()
	wb(t, a, s.record.ID, "send", workbenchInput{RequestID: requestID, TargetID: target.ID, References: []LibraryReference{ref}, Instruction: "请从并发和断线恢复两个角度检查这份材料，指出还缺少的验证。", Intent: "analyze"})
	b.pumpWorkbench(ctx)
	if _, err = b.readAgentRequest(ctx, caller, requestID, "", 0); err != nil {
		t.Fatal(err)
	}
	if _, err = b.finishAgentRequest(ctx, caller, requestID, "", "completed", "已核对固定版本。建议补充：同一请求并发提交、收到请求后断线、晚到结果与新绑定隔离。这些建议尚未纳入成员确认的简报。"); err != nil {
		t.Fatal(err)
	}
	wb(t, a, s.record.ID, "send", workbenchInput{RequestID: uuid.NewString(), TargetID: ownTarget.ID, ParentRequestID: requestID, Instruction: "请复核前一条请求提出的测试缺口。", Intent: "analyze"})
	a.startWorkbench()
	b.startWorkbench()
	assets, err := fs.Sub(webassets.Dist, "dist")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a.Handler(http.FileServer(http.FS(assets))))
	defer server.Close()
	if err = writeJSONFile(filepath.Join(dir, "fixture.json"), map[string]string{"url": server.URL + "/#/collaborations/" + s.record.ID, "api": server.URL, "id": s.record.ID, "dataDir": a.Config.DataDir}); err != nil {
		t.Fatal(err)
	}
	t.Log("workbench browser fixture ready", dir)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	timer := time.NewTimer(25 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			t.Fatal("browser fixture expired")
		case <-ticker.C:
			for _, app := range []*App{a, b} {
				app.agentMu.Lock()
				for _, r := range app.agentReceivers {
					r.lastSeen = time.Now()
				}
				app.agentMu.Unlock()
			}
			if _, err = os.Stat(filepath.Join(dir, "finish")); err == nil {
				return
			}
		}
	}
}
