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

func TestMemberActivityBrowserFixture(t *testing.T) {
	dir := os.Getenv("TEAMCROSS_ACTIVITY_BROWSER_DIR")
	if dir == "" {
		t.Skip("set a fresh TEAMCROSS_ACTIVITY_BROWSER_DIR")
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a, f, s := spaceFixture(t)
	a.Host = "林澈"
	b, _, _ := fixture(t)
	b.Host = "陈宁"
	stopWorkbenchPump(b)
	c, _, _ := fixture(t)
	c.Host = "周悦"
	stopWorkbenchPump(c)
	ctx := context.Background()
	s.record.Title = "网络发布 · 协作空间"
	draft := materialDraft(t, a, f)
	pub, err := a.Publish(ctx, s.record.ID, PublishInput{PreviewID: draft.ID, PreviewHash: draft.Hash, RequestID: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	mid := pub.(map[string]any)["materialId"].(string)
	if err = s.Share(ctx, "lan"); err != nil {
		t.Fatal(err)
	}
	jb, err := b.Join(ctx, s.sharedRuntime().Token())
	if err != nil {
		t.Fatal(err)
	}
	jc, err := c.Join(ctx, s.sharedRuntime().Token())
	if err != nil {
		t.Fatal(err)
	}
	own, err := s.annotate(Annotation{Text: "发布前请核对错误处理与断线恢复，讨论中的结论会汇总到空间简报。"}, "林澈")
	if err != nil {
		t.Fatal(err)
	}
	var note Annotation
	if err = jb.request(ctx, "POST", "/v2/annotation-replies", AnnotationReplyInput{AnnotationID: own.ID, RequestID: uuid.NewString(), Text: "@林澈 已补齐断线恢复测试，可以一起复核最后的验证记录。", Mentions: []MemberMention{{MemberID: "owner", Label: "林澈", StartOffset: 0, EndOffset: 3}}}, &note); err != nil {
		t.Fatal(err)
	}
	if err = jc.request(ctx, "POST", "/v2/annotation-replies", AnnotationReplyInput{AnnotationID: own.ID, RequestID: uuid.NewString(), Text: "我核对了回归结果，下一步可以检查旧窗口与新消息并发时的已读状态。"}, &note); err != nil {
		t.Fatal(err)
	}
	var other Annotation
	if err = jb.request(ctx, "POST", "/v2/annotations", Annotation{Text: "@林澈 这份材料中还需要补充版本边界说明。", Mentions: []MemberMention{{MemberID: "owner", Label: "林澈", StartOffset: 0, EndOffset: 3}}}, &other); err != nil {
		t.Fatal(err)
	}
	assets, err := fs.Sub(webassets.Dist, "dist")
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("TEAMCROSS_HANDLING_BROWSER") == "1" {
		a.shadowCancel()
		a.shadowWG.Wait()
		seedHandlingBrowser(t, a, s.record.ID, own.ID)
	}
	handler := a.Handler(http.FileServer(http.FS(assets)))
	server := httptest.NewServer(handler)
	defer server.Close()
	remote := httptest.NewServer(b.Handler(http.FileServer(http.FS(assets))))
	defer remote.Close()
	manifest := map[string]string{"url": server.URL + "/#/collaborations/" + s.record.ID, "global": server.URL + "/#/activity", "api": server.URL, "remote": remote.URL + "/#/collaborations/" + s.record.ID, "id": s.record.ID, "annotationId": own.ID, "materialId": mid, "dataDir": a.Config.DataDir}
	if err = writeJSONFile(filepath.Join(dir, "fixture.json"), manifest); err != nil {
		t.Fatal(err)
	}
	t.Log("activity browser fixture ready", dir)
	timer := time.NewTimer(30 * time.Minute)
	defer timer.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-timer.C:
			t.Fatal("fixture timed out")
		case <-ticker.C:
			if _, err = os.Stat(filepath.Join(dir, "finish")); err == nil {
				return
			}
		}
	}
}
