package collab

import (
	"context"
	"encoding/json"
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

// One Mac, two real Core endpoints and one isolated native shadow. The space
// and discussion are test data; writes still cross the actual authenticated API.
func TestNativeShadowBrief(t *testing.T) {
	root, binary := os.Getenv("TEAMCROSS_SHADOW_NATIVE_DIR"), os.Getenv("TEAMCROSS_SHADOW_CODEX_BIN")
	if root == "" || binary == "" {
		t.Skip("requires a fresh dedicated native fixture")
	}
	home, cwd := filepath.Join(root, "codex-home"), filepath.Join(root, "repo")
	config, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil || !strings.Contains(string(config), `model = "gpt-5.6-luna"`) {
		t.Fatal("fixture must explicitly select gpt-5.6-luna")
	}
	a, host, b, joined, note := consumerFixture(t)
	v := consumerRegister(t, b, joined.ID)
	briefPatch(t, a, host.record.ID, workbenchInput{Changes: []BriefChange{
		briefPut("decisions", "protocol", "ORIGINAL_PROTOCOL：超时后是否写入尚待核对。", "proposed"),
		briefPut("questions", "owner-question", "KEEP_OWNER_QUESTION：另行确认客户端支持期限。", "open"),
	}})
	answer, err := host.replyAnnotationResult(context.Background(), AnnotationReplyInput{AnnotationID: note.ID, RequestID: uuid.NewString(), Text: "ACTUAL_DISCUSSION：接口仍使用 OIDC。超时后请求可能已经写入，必须先核对原请求结果再决定是否重试。这是已核对的接口事实。"}, "Owner", true)
	if err != nil {
		t.Fatal(err)
	}
	reply := workbenchDecode[struct {
		Reply AnnotationReply `json:"reply"`
	}](answer).Reply
	var mu sync.Mutex
	calls := []map[string]any{}
	injected := false
	cfg := shadow.Config{Directory: filepath.Join(root, "engine"), IdleMemoryDelay: time.Hour, CollaborationTools: mcp.ShadowTools(),
		Start: func(ctx context.Context, h, c, log string) (shadow.Runtime, error) {
			return nativecodex.StartWithMode(ctx, binary, h, c, log, runtimeconfig.Trusted)
		},
		CallTool: func(ctx context.Context, name string, args map[string]any) (any, error) {
			mu.Lock()
			inject := name == "update_space_brief" && !injected
			if inject {
				injected = true
			}
			mu.Unlock()
			if inject {
				// A real host-side write wins between the model's read and update.
				out, err := a.workbenchCall(ctx, host.record.ID, "brief-update", workbenchInput{RequestID: uuid.NewString(), BaseRevision: 1, Changes: []BriefChange{briefPut("progress", "peer-progress", "KEEP_PEER_PROGRESS：同事已开始核对客户端。", "in_progress")}})
				if err != nil || workbenchDecode[BriefResult](out).State != "saved" {
					return nil, fmt.Errorf("fixture concurrent update failed: %v", err)
				}
			}
			out, err := b.shadowToolCall(ctx, v.ID, name, args)
			mu.Lock()
			entry := map[string]any{"tool": name, "arguments": libraryClone(args), "result": libraryClone(out)}
			if err != nil {
				entry["error"] = err.Error()
			}
			calls = append(calls, entry)
			mu.Unlock()
			return out, err
		},
	}
	e, err := shadow.New(cfg, v.ID, joined.ID, v.Name, home, cwd, v.Owner, shadow.ModelSettings{Model: "gpt-5.6-luna", ModelProvider: "openai", ReasoningEffort: "low"})
	if err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	b.shadows[v.ID] = e
	b.mu.Unlock()
	defer func() {
		e.Close()
		host.mu.Lock()
		brief := libraryClone(host.record.Workbench.Brief)
		commands := libraryClone(host.record.Workbench.BriefCommands)
		host.mu.Unlock()
		mu.Lock()
		defer mu.Unlock()
		_ = writeJSONFile(filepath.Join(root, "report.json"), map[string]any{"passed": !t.Failed(), "calls": calls, "brief": brief, "commands": commands, "view": e.View(), "concurrentWriteInjected": injected, "scope": "One Mac, two authenticated Core endpoints, one dedicated Luna shadow, actual public discussion and brief operations."})
	}()
	e.Start()
	messageID := uuid.NewString()
	_, err = e.Receive(shadow.Message{ID: messageID, Actor: v.Owner, Text: fmt.Sprintf(`这是专用空间简报验证。直接完成本轮，不另建后台任务。先用 read_space_brief 取得当前版本，再用 read_annotations 读取讨论 %s 的具体回复 %s（sourceOnly=true）。依据 ACTUAL_DISCUSSION，只更新 decisions 的 protocol 条目：写明 OIDC、超时可能已经写入、先核对原请求结果再决定重试，状态 confirmed，并引用实际回复的准确 source。可通过读取返回的 spaceId 组织来源。保留其他人的全部条目与状态。实际会有同事并发更新：update_space_brief 返回 state=conflict 时尚未写入，重读并合并，使用新 requestId 和新 baseRevision；不询问发送许可。成功后按保存回执 readBack 调用 read_space_brief(requestId) 核对，并用 read_space_brief revision=1、itemId=protocol 核对旧版本仍保留 ORIGINAL_PROTOCOL。确认当前版本同时保留 KEEP_OWNER_QUESTION 和 KEEP_PEER_PROGRESS。最后简短回复 BRIEF_MERGED，说明新版本与旧版本分别是什么。`, note.ID, reply.ID)})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(180 * time.Second)
	finished := false
	for time.Now().Before(deadline) {
		view := e.View()
		for _, message := range view.Messages {
			if message.ID == messageID && message.State == "completed" && !view.Busy {
				finished = true
			}
		}
		if finished {
			break
		}
		if reason := e.DiagnosticError(); reason != "" {
			t.Fatal(reason)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !finished {
		t.Fatal("native brief work did not settle", e.View())
	}
	host.mu.Lock()
	final := libraryClone(host.record.Workbench)
	host.mu.Unlock()
	if final.Brief.Revision != 3 || len(final.Brief.Decisions) != 1 || len(final.Brief.Questions) != 1 || len(final.Brief.Progress) != 1 {
		t.Fatal("native merge lost or duplicated items", final.Brief)
	}
	decision := final.Brief.Decisions[0]
	if decision.ID != "protocol" || decision.Status != "confirmed" || decision.UpdatedBy.ShadowID != v.ID || !strings.Contains(decision.Text, "OIDC") || len(decision.Sources) != 1 || decision.Sources[0].ReplyID != reply.ID || final.Brief.Progress[0].ID != "peer-progress" || final.Brief.Questions[0].ID != "owner-question" {
		t.Fatal("native update lost facts, provenance or another writer", final.Brief)
	}
	mu.Lock()
	recorded := libraryClone(calls)
	mu.Unlock()
	conflicts, saved, oldReads, receiptReads := 0, 0, 0, 0
	for _, call := range recorded {
		result := workbenchDecode[BriefResult](call["result"])
		if call["tool"] == "update_space_brief" {
			if result.State == "conflict" {
				conflicts++
			}
			if result.State == "saved" {
				saved++
			}
		}
		if call["tool"] == "read_space_brief" && result.Revision == 1 && result.Item != nil && strings.Contains(result.Item.Text, "ORIGINAL_PROTOCOL") {
			oldReads++
		}
		if call["tool"] == "read_space_brief" && result.RequestID != "" && result.Revision == 3 {
			receiptReads++
		}
	}
	if conflicts != 1 || saved != 1 || oldReads < 1 || receiptReads < 1 {
		data, _ := json.Marshal(recorded)
		t.Fatal("missing real conflict, merge, receipt or historical read", string(data))
	}
}
