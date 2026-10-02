package collab

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"teamcross/internal/buildinfo"
	"teamcross/internal/service"
	"teamcross/internal/webassets"
	"teamcross/internal/workspace"
	"testing"
	"time"
	"unicode/utf16"
)

// Only native history is synthetic. Publishing, versioning, access, annotations,
// fixed reading codes, authenticated HTTP, and disk persistence use real Core.
func pluginFixtureTurn(id, text string) MaterialTurn {
	return MaterialTurn{ID: id, Status: "completed", Items: []MaterialItem{{ID: "answer", Type: "agentMessage", Phase: "final_answer", Text: text}}}
}
func TestChatGPTPluginFixture(t *testing.T) {
	dir := os.Getenv("TEAMCROSS_PLUGIN_FIXTURE_DIR")
	if dir == "" {
		t.Skip("explicit fresh fixture directory required")
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(dir, "workspace")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := workspace.Git(ctx, repo, "init", "-b", "codex/plugin-fixture"); err != nil {
		t.Fatal(err)
	}
	first := "## 固定版本调查\n\n本次样本标记 **PLUGIN-V1-20261002**。连接池等待从 420 ms 降到 38 ms，服务端处理时间保持 31 ms。\n\n| 检查 | 结果 |\n| --- | --- |\n| 同一并发条件 | 已核对 |\n| 真实跨设备网络 | 待复测 |\n\n```go\ntransport.MaxConnsPerHost = 20\n```\n\n中文与 emoji 🧭 用于验证原文坐标。"
	f := &fakeRuntime{source: Source{ID: uuid.NewString(), Cwd: repo, Name: "合成调查来源"}, alive: true, history: []MaterialTurn{pluginFixtureTurn("summary", first), pluginFixtureTurn("private", "UNPUBLISHED-SECRET-NOT-SELECTED")}}
	a, err := Open(Config{DataDir: filepath.Join(dir, "state"), Repo: repo, Binary: "/usr/bin/true", Loopback: true, StartProcess: func(string, string, string, string) (Runtime, error) { return f, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Host = "Team Cross 插件 · 隔离本机"
	if err := a.SetUILanguage("zh-CN"); err != nil {
		t.Fatal(err)
	}
	publish := func(space *Session, title, start, end, mid string, base int) string {
		t.Helper()
		d, e := a.FreezeSource(ctx, "codex", f.source.ID)
		if e != nil {
			t.Fatal(e)
		}
		d, e = a.PreviewPublication(PublicationSelection{DraftID: d.ID, Title: title, StartTurnID: start, EndTurnID: end})
		if e != nil {
			t.Fatal(e)
		}
		p, e := a.Publish(ctx, space.record.ID, PublishInput{PreviewID: d.ID, PreviewHash: d.Hash, RequestID: uuid.NewString(), MaterialID: mid, BaseVersion: base})
		if e != nil {
			t.Fatal(e)
		}
		return p.(map[string]any)["materialId"].(string)
	}
	space, err := a.CreateSpace(ctx, SpaceInput{RequestID: uuid.NewString(), Title: "连接池调查与材料版本"})
	if err != nil {
		t.Fatal(err)
	}
	mid := publish(space, "连接池调查", "summary", "summary", "", 0)
	f.mu.Lock()
	f.history = append([]MaterialTurn{pluginFixtureTurn("revision", "## 后续复测\n\n新版本专属标记 PLUGIN-V2-20261002。\n\n新版补充结果不能混入版本 1 的分析。")}, f.history...)
	f.mu.Unlock()
	publish(space, "连接池调查（补充复测）", "summary", "revision", mid, 1)
	quote := "本次样本标记"
	start := strings.Index(first, quote)
	offset := len(utf16.Encode([]rune(first[:start])))
	note, err := space.annotate(Annotation{Text: "请核对 v1 的样本条件，保留待复测事项。ANNOTATION-SELECTED-20261002", Target: &AnnotationTarget{Kind: "material", MaterialID: mid, Version: 1, TurnID: "summary", ItemID: "answer", Quote: quote, StartOffset: offset, EndOffset: offset + len(utf16.Encode([]rune(quote)))}}, "测试发起者")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 24; i++ {
		if _, err := space.annotate(Annotation{Text: "分页批注 " + strings.Repeat("记录 ", i+1)}, "测试发起者"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 12; i++ {
		if _, err := space.replyAnnotation(ctx, AnnotationReplyInput{AnnotationID: note.ID, Text: strings.Repeat("分页回复用于核对原请求编号与保存状态。", 100), RequestID: uuid.NewString()}, "测试协作者"); err != nil {
			t.Fatal(err)
		}
	}
	f.mu.Lock()
	f.source.ID = uuid.NewString()
	f.source.Name = "未选中的发布检查"
	f.history = []MaterialTurn{pluginFixtureTurn("other", "UNSELECTED-SPACE-20261002：另一空间的材料")}
	f.mu.Unlock()
	other, err := a.CreateSpace(ctx, SpaceInput{RequestID: uuid.NewString(), Title: "发布检查（不进入所选分析）"})
	if err != nil {
		t.Fatal(err)
	}
	otherMid := publish(other, "独立发布检查", "other", "other", "", 0)
	var mu sync.Mutex
	var requests []map[string]string
	var connection service.Connection
	web, err := fs.Sub(webassets.Dist, "dist")
	if err != nil {
		t.Fatal(err)
	}
	handler := a.Handler(http.FileServer(http.FS(web)))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, map[string]string{"method": r.Method, "path": r.URL.Path})
		mu.Unlock()
		if r.URL.Path == "/api/control/status" {
			if r.Header.Get("Authorization") != "Bearer "+a.Token {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			s := service.Status{Connection: connection, Running: true}
			s.Token = ""
			json.NewEncoder(w).Encode(s)
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
	if err := service.Save(data, connection); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{"api": server.URL, "pid": os.Getpid(), "dataDir": data, "spaceId": space.record.ID, "materialId": mid, "annotationId": note.ID, "otherSpaceId": other.record.ID, "otherMaterialId": otherMid, "version": 1, "executionEnabled": false, "nativeHistory": "synthetic", "coreHTTPAndStorage": "real"}
	if err := writeJSONFile(filepath.Join(dir, "fixture.json"), manifest); err != nil {
		t.Fatal(err)
	}
	t.Log("local plugin fixture ready", dir)
	timer := time.NewTimer(90 * time.Minute)
	defer timer.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-timer.C:
			t.Fatal("plugin fixture expired")
		case <-tick.C:
			mu.Lock()
			observed := append([]map[string]string(nil), requests...)
			mu.Unlock()
			f.mu.Lock()
			calls := append([]string(nil), f.calls...)
			forks := f.forks
			f.mu.Unlock()
			space.mu.Lock()
			notes := append([]Annotation(nil), space.record.Annotations...)
			execution := space.record.ExecutionRecord != nil
			space.mu.Unlock()
			if err := writeJSONFile(filepath.Join(dir, "observations.json"), map[string]any{"requests": observed, "nativeCalls": calls, "forks": forks, "executionEnabled": execution, "annotations": notes}); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(dir, "finish")); err != nil {
				continue
			}
			if execution || forks != 0 {
				t.Fatal("plugin enabled execution")
			}
			for _, call := range calls {
				if strings.HasPrefix(call, "turn/") || call == "thread/fork" {
					t.Fatal("plugin sent shared model input", call)
				}
			}
			var record Record
			b, err := os.ReadFile(filepath.Join(data, "collaborations", space.record.ID, "collaboration.json"))
			if err != nil || json.Unmarshal(b, &record) != nil {
				t.Fatal("persistence unavailable", err)
			}
			if len(record.Annotations) != len(notes) || len(record.Annotations[0].Replies) != len(notes[0].Replies) {
				t.Fatal("reply persistence mismatch")
			}
			return
		}
	}
}
