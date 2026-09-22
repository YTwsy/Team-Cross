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

// Dedicated browser fixture: native history is simulated, all HTTP and storage
// are real. An explicit fresh directory makes cleanup independent of user data.
func TestLibraryBrowserFixture(t *testing.T) {
	dir := os.Getenv("TEAMCROSS_LIBRARY_BROWSER_DIR")
	if dir == "" {
		t.Skip("set a fresh TEAMCROSS_LIBRARY_BROWSER_DIR")
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a, f, _ := fixture(t)
	a.Host = "资源库验证 · 本机"
	ctx := context.Background()
	var main *Session
	for i, name := range []string{"接口超时调查", "发布前检查"} {
		f.mu.Lock()
		f.source.ID = uuid.NewString()
		f.source.Name = name
		f.history = []MaterialTurn{{ID: "summary", Status: "completed", Items: []MaterialItem{{ID: "answer", Type: "agentMessage", Text: "## 验证结果\n\n连接池等待下降，服务端耗时保持稳定。\n\n| 指标 | 调整前 | 调整后 |\n| --- | --- | --- |\n| 连接池等待 P95 | 420 ms | 38 ms |\n| 服务端耗时 | 30 ms | 31 ms |\n\n样本来自相同并发条件，后续需要补充真实网络数据。"}}}, {ID: "analysis", Status: "completed", Items: []MaterialItem{{ID: "question", Type: "agentMessage", Text: "## 连接池分析\n\n先检查 **连接复用**，再核对限流与服务端耗时。\n\n```go\ntransport.MaxConnsPerHost = 20\n```\n\n这段调查在原会话完成后发布，后续内容不会自动进入材料。"}}}}
		f.mu.Unlock()
		s, err := a.CreateSpace(ctx, SpaceInput{RequestID: uuid.NewString(), Title: name})
		if err != nil {
			t.Fatal(err)
		}
		d, err := a.FreezeSource(ctx, "codex", f.source.ID)
		if err != nil {
			t.Fatal(err)
		}
		d, err = a.PreviewPublication(PublicationSelection{DraftID: d.ID, Title: []string{"连接池等待与重试策略", "发布检查记录"}[i], StartTurnID: "analysis", EndTurnID: "summary"})
		if err != nil {
			t.Fatal(err)
		}
		p, err := a.Publish(ctx, s.record.ID, PublishInput{PreviewID: d.ID, PreviewHash: d.Hash, RequestID: uuid.NewString()})
		if err != nil {
			t.Fatal(err)
		}
		mid := p.(map[string]any)["materialId"].(string)
		n, err := s.annotate(Annotation{Text: []string{"这组结论是否覆盖了连接池复用的场景？", "发布前请再核对一次版本与来源。"}[i], Target: &AnnotationTarget{Kind: "material", MaterialID: mid, Version: 1, TurnID: "summary", ItemID: "answer", Quote: "连接池等待下降", StartOffset: 9, EndOffset: 16}}, "发起者")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.replyAnnotation(ctx, AnnotationReplyInput{AnnotationID: n.ID, Text: "已复核样本范围，建议保留复测条件。", RequestID: uuid.NewString()}, "协作者"); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			main = s
			in := CreateInput{SpaceID: s.record.ID, RequestID: uuid.NewString(), SourceID: f.source.ID, WorkspaceMode: "existing"}
			preview, err := a.Preview(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			in.PreviewHash = preview.Hash
			if _, err = a.Create(ctx, in); err != nil {
				t.Fatal(err)
			}
		}
	}
	assets, err := fs.Sub(webassets.Dist, "dist")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a.Handler(http.FileServer(http.FS(assets))))
	defer server.Close()
	manifest := map[string]string{"url": server.URL + "/#/library", "api": server.URL, "dataDir": a.Config.DataDir, "id": main.record.ID}
	if err = writeJSONFile(filepath.Join(dir, "fixture.json"), manifest); err != nil {
		t.Fatal(err)
	}
	t.Log("library fixture ready", dir)
	timeout := time.NewTimer(25 * time.Minute)
	defer timeout.Stop()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-timeout.C:
			t.Fatal("browser fixture expired")
		case <-tick.C:
			if _, err = os.Stat(filepath.Join(dir, "finish")); err == nil {
				return
			}
		}
	}
}
