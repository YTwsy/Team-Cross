package shadow

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
	"teamcross/internal/nativecodex"
	"teamcross/internal/runtimeconfig"
)

type inputNativeAudit struct {
	mu    sync.Mutex
	Calls []map[string]any `json:"calls"`
}

type inputAuditRuntime struct {
	Runtime
	audit *inputNativeAudit
}

func (r *inputAuditRuntime) SetHandler(handler func(nativecodex.Message)) {
	r.Runtime.SetHandler(func(m nativecodex.Message) {
		if m.Method == "item/tool/call" {
			var call toolCall
			if json.Unmarshal(m.Params, &call) == nil && call.Tool == "shadow_task" {
				var args map[string]any
				_ = json.Unmarshal(call.Arguments, &args)
				r.audit.mu.Lock()
				r.audit.Calls = append(r.audit.Calls, map[string]any{"method": "shadow_task", "threadId": call.ThreadID, "operation": args["operation"], "state": args["state"], "suppliedId": args["id"], "suppliedVersion": args["baseVersion"], "at": time.Now()})
				r.audit.mu.Unlock()
			}
		}
		handler(m)
	})
}

func (r *inputAuditRuntime) Call(ctx context.Context, method string, params, out any) error {
	started := time.Now()
	err := r.Runtime.Call(ctx, method, params, out)
	if method == "turn/start" || method == "turn/steer" || method == "thread/start" || method == "thread/resume" {
		p := cloneMap(params)
		entry := map[string]any{"method": method, "threadId": p["threadId"], "expectedTurnId": p["expectedTurnId"], "model": p["model"], "startedAt": started, "returnedAt": time.Now(), "durationMs": time.Since(started).Milliseconds()}
		if err != nil {
			entry["error"] = err.Error()
		}
		r.audit.mu.Lock()
		r.audit.Calls = append(r.audit.Calls, entry)
		r.audit.mu.Unlock()
	}
	return err
}

// Opt-in, isolated home/repository and Luna only. The gated tools are local
// fixture callbacks; this proves native multiplexing, not cross-Mac transport.
func TestNativeShadowMessageInputs(t *testing.T) {
	root, binary := os.Getenv("TEAMCROSS_SHADOW_NATIVE_DIR"), os.Getenv("TEAMCROSS_SHADOW_CODEX_BIN")
	if root == "" || binary == "" {
		t.Skip("requires a fresh dedicated native fixture")
	}
	home, cwd := filepath.Join(root, "codex-home"), filepath.Join(root, "repo")
	config, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil || !strings.Contains(string(config), `model = "gpt-5.6-luna"`) {
		t.Fatal("fixture must select gpt-5.6-luna")
	}
	mainEntered, workEntered := make(chan struct{}), make(chan struct{})
	mainRelease, workRelease := make(chan struct{}), make(chan struct{})
	var mainOnce, workOnce sync.Once
	audit := &inputNativeAudit{}
	cfg := Config{Directory: filepath.Join(root, "engine"), IdleMemoryDelay: time.Hour,
		CollaborationTools: []map[string]any{{"name": "fixture_input_gate", "description": "Read-only test evidence. phase=main waits for the main-input gate; phase=work waits for the background gate. This is the only evidence tool required by this isolated fixture.", "annotations": map[string]any{"readOnlyHint": true}, "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"phase": map[string]any{"type": "string", "enum": []string{"main", "work"}}}, "required": []string{"phase"}, "additionalProperties": false}}},
		Start: func(ctx context.Context, h, c, l string) (Runtime, error) {
			p, err := nativecodex.StartWithMode(ctx, binary, h, c, l, runtimeconfig.Trusted)
			if err != nil {
				return nil, err
			}
			return &inputAuditRuntime{Runtime: p, audit: audit}, nil
		},
		CallTool: func(ctx context.Context, name string, args map[string]any) (any, error) {
			if name != "fixture_input_gate" {
				return nil, fmt.Errorf("only the isolated fixture tool is available")
			}
			var release <-chan struct{}
			switch args["phase"] {
			case "main":
				mainOnce.Do(func() { close(mainEntered) })
				release = mainRelease
			case "work":
				workOnce.Do(func() { close(workEntered) })
				release = workRelease
			default:
				return nil, fmt.Errorf("invalid fixture phase")
			}
			select {
			case <-release:
				return map[string]any{"evidence": "INPUT_WORK_EVIDENCE_7351"}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(180 * time.Second):
				return nil, fmt.Errorf("fixture gate timeout")
			}
		},
	}
	e, err := New(cfg, uuid.NewString(), "input-native-fixture", "Input fixture", home, cwd, Actor{ID: "owner", Name: "Owner"}, ModelSettings{Model: "gpt-5.6-luna", ModelProvider: "openai", ReasoningEffort: "low"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		e.Close()
		e.mu.Lock()
		runs := clone(e.record.Runs)
		e.mu.Unlock()
		audit.mu.Lock()
		calls := clone(audit.Calls)
		audit.mu.Unlock()
		_ = writeAtomic(filepath.Join(root, "report.json"), map[string]any{"passed": !t.Failed(), "scope": "single Mac native Luna, synthetic members and gated local tools; no cross-Mac or UI claim", "calls": calls, "runs": runs, "view": e.View()})
	}()
	wait := func(label string, check func() bool) {
		t.Helper()
		deadline := time.Now().Add(180 * time.Second)
		for time.Now().Before(deadline) {
			if check() {
				t.Log(label)
				return
			}
			if why := e.DiagnosticError(); why != "" {
				t.Fatalf("%s: %s", label, why)
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("timeout: %s; approvals=%s", label, jsonText(e.Approvals()))
	}
	receive := func(actor, text string) Message {
		t.Helper()
		m, err := e.Receive(Message{ID: uuid.NewString(), Actor: Actor{ID: actor, Name: actor}, Text: text})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	reply := func(m Message, marker string) {
		t.Helper()
		wait("reply bound to "+m.Actor.ID+": "+marker, func() bool {
			for _, a := range e.View().Messages {
				if a.Role == "assistant" && a.ReplyTo == m.ID && strings.Contains(a.Text, marker) {
					return true
				}
			}
			return false
		})
	}
	idle := func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		for _, r := range e.record.Runs {
			if activeRun(r) {
				return false
			}
		}
		return true
	}
	a := receive("A", "这是隔离验证。先调用只读 fixture_input_gate，phase=main；工具返回后，明确记住我个人的颜色偏好，文本为 Preference A: teal，scope=member。用 shadow_reply 对本条输入回复且包含 A_REPLY_913。不要创建后台任务，不用 shell、浏览器或其他工具。其他成员的消息保持独立来源。")
	b := receive("B", "请明确记住我的个人颜色偏好，文本为 Preference B: amber，scope=member。这是我的独立偏好。用 shadow_reply 回复我这条消息，包含 B_REPLY_284。不要创建任务，不要把我的回复合并到 A 的回复。")
	e.Start()
	wait("main read tool entered", func() bool {
		select {
		case <-mainEntered:
			return true
		default:
			return false
		}
	})
	c := receive("C", "运行中的新增问题：2+3 等于多少？用 shadow_reply 回复本条输入，包含 C_REPLY_527 和答案。不要修改 A 或 B 的偏好。")
	wait("native steer accepted on existing turn", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		for _, r := range e.record.Runs {
			if len(runInputs(r)) == 3 && len(r.InputBatches) >= 2 && r.InputBatches[1].State == "accepted" {
				return true
			}
		}
		return false
	})
	close(mainRelease)
	reply(a, "A_REPLY_913")
	reply(b, "B_REPLY_284")
	reply(c, "C_REPLY_527")
	wait("first native turn completed", idle)
	view := e.View()
	seen := map[string]bool{}
	for _, m := range view.Memories {
		if m.SubjectID == "A" && strings.Contains(m.Text, "teal") {
			seen["A"] = true
		}
		if m.SubjectID == "B" && strings.Contains(m.Text, "amber") {
			seen["B"] = true
		}
	}
	if !seen["A"] || !seen["B"] || len(view.Jobs) != 0 {
		t.Fatal("native batch crossed authors or created ordinary jobs", view.Memories, view.Jobs)
	}
	for _, m := range view.Messages {
		if m.Role != "assistant" {
			continue
		}
		for id, marker := range map[string]string{a.ID: "A_REPLY_913", b.ID: "B_REPLY_284", c.ID: "C_REPLY_527"} {
			if strings.Contains(m.Text, marker) && m.ReplyTo != id {
				t.Fatal("native reply went to wrong source", m)
			}
		}
	}
	request := receive("A", "创建且只创建一个独立 Work。目标：先用 shadow_task report state=continue 报告开始工作，正文包含 WORK_PROGRESS_190；再调用 fixture_input_gate phase=work 一次，读取其返回证据；最后用 report state=complete 提交结果和 criteriaChecks，结果包含原样证据 INPUT_WORK_EVIDENCE_7351。Work 的 report 不传 id/baseVersion/baseRevision，由 Core 绑定。不要在主会话调用这个慢工具，不用 shell 或其他工具。派发后用 shadow_reply 回复本条消息 ACK_WORK_648，然后结束主轮，不等 Work。")
	reply(request, "ACK_WORK_648")
	wait("independent Work tool entered", func() bool {
		select {
		case <-workEntered:
			return true
		default:
			return false
		}
	})
	quick := receive("B", "在 Work 执行时回答一个独立短问题：7+8 等于多少？回复本条消息，包含 QUICK_REPLY_486 和答案，不等待 Work，不建立新任务。")
	reply(request, "WORK_PROGRESS_190")
	reply(quick, "QUICK_REPLY_486")
	view = e.View()
	if len(view.Jobs) != 1 || view.Jobs[0].State != "running" || view.Jobs[0].SourceID != request.ID {
		t.Fatal("short reply waited for Work or changed its origin", view.Jobs)
	}
	worker := view.Jobs[0].ThreadID
	if worker == view.Current {
		t.Fatal("Work reused main thread")
	}
	close(workRelease)
	wait("Work completed with actual evidence", func() bool {
		v := e.View()
		return len(v.Jobs) == 1 && v.Jobs[0].State == "completed" && strings.Contains(v.Jobs[0].Result, "INPUT_WORK_EVIDENCE_7351")
	})
	wait("all native turns finished", idle)
	reply(request, "INPUT_WORK_EVIDENCE_7351")
	e.mu.Lock()
	for _, r := range e.record.Runs {
		if r.Kind == "result" {
			t.Error("Work report created a mandatory main-session rewrite")
		}
	}
	e.mu.Unlock()
	audit.mu.Lock()
	reports := 0
	for _, call := range audit.Calls {
		if call["method"] == "shadow_task" && call["operation"] == "report" {
			reports++
			if call["suppliedId"] != nil || call["suppliedVersion"] != nil {
				t.Error("native W repeated host-owned task bookkeeping")
			}
		}
	}
	audit.mu.Unlock()
	if reports < 2 {
		t.Error("missing real native progress and completion reports")
	}
	if e.View().Model != "gpt-5.6-luna" {
		t.Fatal("unexpected test model")
	}
}
