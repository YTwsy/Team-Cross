package collab

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/buildinfo"
	"teamcross/internal/service"
	"teamcross/internal/webassets"
)

type readerBrowserRuntime struct {
	*fakeRuntime
	raw []map[string]any
}

func (f *readerBrowserRuntime) Call(ctx context.Context, method string, in, out any) error {
	if method != "thread/turns/list" {
		return f.fakeRuntime.Call(ctx, method, in, out)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p := in.(map[string]any)
	start, _ := strconv.Atoi(fmt.Sprint(p["cursor"]))
	limit, _ := p["limit"].(int)
	if limit <= 0 {
		limit = 8
	}
	end := min(start+limit, len(f.raw))
	if start < 0 || start > len(f.raw) {
		return fmt.Errorf("invalid fixture cursor")
	}
	next := ""
	if end < len(f.raw) {
		next = strconv.Itoa(end)
	}
	b, _ := json.Marshal(map[string]any{"data": f.raw[start:end], "nextCursor": next})
	return json.Unmarshal(b, out)
}

func TestReaderBrowserFixture(t *testing.T) {
	dir := os.Getenv("TEAMCROSS_READER_BROWSER_DIR")
	if dir == "" {
		t.Skip("set a fresh TEAMCROSS_READER_BROWSER_DIR")
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a, base, _ := fixture(t)
	a.Host = "阅读器测试 Mac"
	f := &readerBrowserRuntime{fakeRuntime: base}
	a.Config.StartProcess = func(string, string, string, string) (Runtime, error) { return f, nil }
	ctx := context.Background()
	im := image.NewRGBA(image.Rect(0, 0, 640, 360))
	draw.Draw(im, im.Bounds(), image.NewUniform(color.RGBA{R: 235, G: 242, B: 255, A: 255}), image.Point{}, draw.Src)
	for i := 0; i < 5; i++ {
		draw.Draw(im, image.Rect(60+i*110, 270-i*35, 125+i*110, 310), image.NewUniform(color.RGBA{R: uint8(35 + i*15), G: 99, B: 217, A: 255}), image.Point{}, draw.Src)
	}
	var pngData bytes.Buffer
	_ = png.Encode(&pngData, im)
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngData.Bytes())
	rich := "## 阅读体验验证\n\n固定材料版本、可追溯源码与完整搜索。中文与 emoji 🙂 用于校验批注位置。\n\n```mermaid\nflowchart LR\n A[读取来源] --> B{内容完成?}\n B -->|是| C[显示图形]\n B -->|否| D[保留源码]\n C --> E[批注与讨论]\n```\n\n行内公式 $E=mc^2$ 与独立公式：\n\n$$\n\\int_0^1 x^2 \\, dx = \\frac{1}{3}\n$$\n\n| 检查项 | 当前版本 | 结果 | 说明 |\n| --- | --- | --- | --- |\n| 图形与源码 | v1 | 支持 | 保留源码位置，支持整块批注 |\n| 图片 | v1 | 已固定 | 发布后来源变动不改变这一版本 |\n\n### 长代码与工具输出\n\n```typescript\n" + strings.Repeat("const result = await readVersion({ materialId: 'sample', version: 1 });\n", 32) + "```\n\n![外链示例](https://example.invalid/external.png)\n\n最后一段用于检查回到底部与跟随。"
	for n := 20; n >= 1; n-- {
		answer := fmt.Sprintf("第 %d 轮合成内容。SEARCH-OLDER-%02d", n, n)
		blocks := []any{map[string]any{"type": "text", "text": fmt.Sprintf("请检查第 %d 轮内容。", n)}}
		if n == 20 {
			answer = rich
			blocks = append(blocks, map[string]any{"type": "image", "url": dataURL}, map[string]any{"type": "image", "url": dataURL})
		}
		f.raw = append(f.raw, map[string]any{"id": fmt.Sprintf("turn-%d", n), "status": "completed", "items": []any{map[string]any{"id": "question", "type": "userMessage", "content": blocks}, map[string]any{"id": "answer", "type": "agentMessage", "text": answer}, map[string]any{"id": "tool", "type": "toolResult", "text": strings.Repeat("output line\n", 180) + "TOOL-ONLY-NEEDLE"}, map[string]any{"id": "tool-2", "type": "toolResult", "text": "second process result"}}})
	}
	space, err := a.CreateSpace(ctx, SpaceInput{RequestID: uuid.NewString(), Title: "阅读器 · 六项体验验证"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := a.FreezeSource(ctx, "codex", f.source.ID)
	if err != nil {
		t.Fatal(err)
	}
	d, err = a.PreviewPublication(PublicationSelection{DraftID: d.ID, Title: "图形、图片与公式 · 固定版本", StartTurnID: "turn-20", EndTurnID: "turn-20"})
	if err != nil {
		t.Fatal(err)
	}
	pub, err := a.Publish(ctx, space.record.ID, PublishInput{PreviewID: d.ID, PreviewHash: d.Hash, RequestID: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	in := CreateInput{SpaceID: space.record.ID, RequestID: uuid.NewString(), SourceID: f.source.ID, WorkspaceMode: "existing"}
	preview, err := a.Preview(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	in.PreviewHash = preview.Hash
	execution, err := a.Create(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	web, err := fs.Sub(webassets.Dist, "dist")
	if err != nil {
		t.Fatal(err)
	}
	handler := a.Handler(http.FileServer(http.FS(web)))
	var connection service.Connection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Plugin installation is outside this fixture. Never invoke the user's
		// installed management helper while testing the reader's MCP transport.
		if r.URL.Path == "/api/plugin/connection" {
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "unavailable", "available": false, "autoUpdate": false, "installed": false})
			return
		}
		if r.URL.Path == "/api/control/status" {
			if r.Header.Get("Authorization") != "Bearer "+a.Token {
				w.WriteHeader(403)
				return
			}
			status := service.Status{Connection: connection, Running: true}
			status.Token = ""
			_ = json.NewEncoder(w).Encode(status)
			return
		}
		if r.URL.Path == "/fixture/advance" && r.Method == "POST" {
			f.mu.Lock()
			items := f.raw[0]["items"].([]any)
			answer := items[1].(map[string]any)
			answer["text"] = answer["text"].(string) + "\n\n追加进展：" + time.Now().Format("15:04:05")
			f.raw[0]["status"] = "inProgress"
			if r.URL.Query().Get("done") == "true" {
				f.raw[0]["status"] = "completed"
			}
			f.mu.Unlock()
			execution.mu.Lock()
			execution.sequence++
			execution.mu.Unlock()
			w.WriteHeader(204)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	data, err := service.Normalize(a.Config.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	connection = service.Connection{URL: server.URL, PID: os.Getpid(), Instance: uuid.NewString(), Token: a.Token, Version: buildinfo.Version, Commit: buildinfo.Commit, Protocol: buildinfo.ControlProtocol, DataDir: data}
	if err = service.Save(data, connection); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{"api": server.URL, "url": server.URL + "/#/collaborations/" + space.record.ID, "dataDir": data, "spaceId": space.record.ID, "executionId": execution.record.ID, "materialId": pub.(map[string]any)["materialId"], "nativeHistory": "synthetic"}
	if err = writeJSONFile(filepath.Join(dir, "fixture.json"), manifest); err != nil {
		t.Fatal(err)
	}
	t.Log("reader fixture ready", dir)
	deadline := time.NewTimer(45 * time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("reader fixture expired")
		case <-tick.C:
			if _, err = os.Stat(filepath.Join(dir, "finish")); err == nil {
				return
			}
		}
	}
}
