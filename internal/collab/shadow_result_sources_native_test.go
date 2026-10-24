package collab

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/mcp"
	"teamcross/internal/nativecodex"
	"teamcross/internal/runtimeconfig"
	"teamcross/internal/shadow"
)

// The historical result is controlled fixture data. The reading shadow is a
// real isolated Luna session using two authenticated local Core endpoints.
func TestNativeShadowResultSources(t *testing.T) {
	root, binary := os.Getenv("TEAMCROSS_SHADOW_NATIVE_DIR"), os.Getenv("TEAMCROSS_SHADOW_CODEX_BIN")
	if root == "" || binary == "" {
		t.Skip("requires a dedicated native fixture")
	}
	home, cwd := filepath.Join(root, "codex-home"), filepath.Join(root, "repo")
	config, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil || !strings.Contains(string(config), `model = "gpt-5.6-luna"`) {
		t.Fatal("fixture must explicitly select gpt-5.6-luna")
	}
	_, host, member, joined, _ := consumerFixture(t)
	member.shadowCancel()
	member.shadowWG.Wait()
	sourceView := consumerRegister(t, member, joined.ID)
	old := shadow.TaskResult{ID: "result-compatibility-v1", TaskID: "compatibility-task", TaskVersion: 1, Title: "旧版本兼容性", SourceID: "fixture-request", State: "completed", Outcome: "completed", Text: "HISTORICAL_RESULT_V1：旧客户端必须保留适配层。", CompletionCriteria: []string{"核对旧客户端"}, CompletionChecks: []string{"OLD_CLIENT_CHECKED"}, CreatedAt: time.Now()}
	latest := old
	latest.ID, latest.TaskVersion, latest.Text = "result-compatibility-v2", 2, "LATEST_RESULT_V2：新客户端不再需要适配层。"
	sourceView.TaskResults = []shadow.TaskResult{old, latest}
	sourceView.Jobs = []shadow.Job{{ID: old.TaskID, Version: 2, State: "completed", Result: latest.Text}}
	sourceView.Messages = []shadow.Message{{ID: "reply-historical", Actor: shadow.Actor{ID: sourceView.ID, Name: sourceView.Name}, Role: "assistant", State: "completed", Text: "HISTORICAL_MESSAGE：这项原答复仅讨论旧客户端。", CreatedAt: time.Now()}}
	sourceView.Revision++
	shadowOp(t, member, joined.ID, "sync", shadowInput{ID: sourceView.ID, Snapshot: &sourceView})
	v := consumerRegister(t, member, joined.ID)
	var mu sync.Mutex
	calls := []map[string]any{}
	cfg := shadow.Config{Directory: filepath.Join(root, "engine"), IdleMemoryDelay: time.Hour, CollaborationTools: mcp.ShadowTools(),
		Start: func(ctx context.Context, h, c, log string) (shadow.Runtime, error) {
			return nativecodex.StartWithMode(ctx, binary, h, c, log, runtimeconfig.Trusted)
		},
		CallTool: func(ctx context.Context, name string, args map[string]any) (any, error) {
			out, err := member.shadowToolCall(ctx, v.ID, name, args)
			entry := map[string]any{"tool": name, "arguments": libraryClone(args), "result": libraryClone(out)}
			if err != nil {
				entry["error"] = err.Error()
			}
			mu.Lock()
			calls = append(calls, entry)
			mu.Unlock()
			return out, err
		},
	}
	e, err := shadow.New(cfg, v.ID, joined.ID, v.Name, home, cwd, v.Owner, shadow.ModelSettings{Model: "gpt-5.6-luna", ModelProvider: "openai", ReasoningEffort: "low"})
	if err != nil {
		t.Fatal(err)
	}
	member.mu.Lock()
	member.shadows[v.ID] = e
	member.mu.Unlock()
	defer func() {
		e.Close()
		host.mu.Lock()
		brief := libraryClone(host.record.Workbench.Brief)
		host.mu.Unlock()
		mu.Lock()
		defer mu.Unlock()
		_ = writeJSONFile(filepath.Join(root, "report.json"), map[string]any{"passed": !t.Failed(), "calls": calls, "brief": brief, "view": e.View(), "scope": "One Mac; two authenticated Core endpoints; one native Luna reader; synthetic immutable task-result and message sources."})
	}()
	e.Start()
	id := uuid.NewString()
	_, err = e.Receive(shadow.Message{ID: id, Actor: v.Owner, Text: fmt.Sprintf(`这是准确来源验证，直接完成，不建立后台任务。请用 read_space_shadow 读取分身 %s 的 resultId=%s，然后单独用 messageId=reply-historical 读取原消息。当前任务已有 v2，你必须以指定 v1 结果为旧客户端结论依据。读取空间简报后，用 update_space_brief 在 decisions 新增 historical-compatibility，写明旧客户端仍需适配层，状态 confirmed，sources 同时引用这两次读取返回的准确 source。保留其他条目。不需要询问发送许可。最后回复 SOURCE_V1_CONFIRMED 并简短解释为什么当前 v2 不能替换旧引用。`, sourceView.ID, old.ID)})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(180 * time.Second)
	finished := false
	for time.Now().Before(deadline) {
		view := e.View()
		for _, message := range view.Messages {
			finished = finished || message.ID == id && message.State == "completed" && !view.Busy
		}
		if finished {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !finished {
		t.Fatal("native exact-source turn did not complete")
	}
	host.mu.Lock()
	brief := libraryClone(host.record.Workbench.Brief)
	host.mu.Unlock()
	if len(brief.Decisions) != 1 || brief.Decisions[0].ID != "historical-compatibility" || len(brief.Decisions[0].Sources) != 2 {
		t.Fatal("native reader did not save exact historical evidence", brief)
	}
	resultSeen, messageSeen := false, false
	for _, ref := range brief.Decisions[0].Sources {
		resultSeen = resultSeen || ref.ShadowID == sourceView.ID && ref.ShadowResultID == old.ID
		messageSeen = messageSeen || ref.ShadowID == sourceView.ID && ref.ShadowMessageID == "reply-historical"
	}
	if !resultSeen || !messageSeen || strings.Contains(brief.Decisions[0].Text, "不再需要") {
		t.Fatal("latest result replaced the cited historical source", brief)
	}
}
