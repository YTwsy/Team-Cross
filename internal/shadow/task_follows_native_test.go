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
	"teamcross/internal/spacecontext"
)

type taskFollowAuditRuntime struct {
	*toolsetAuditRuntime
	mu         *sync.Mutex
	interrupts *[]map[string]any
}

func (p *taskFollowAuditRuntime) Call(ctx context.Context, method string, params, out any) error {
	err := p.toolsetAuditRuntime.Call(ctx, method, params, out)
	if method == "turn/interrupt" {
		p.mu.Lock()
		entry := cloneMap(params)
		entry["acknowledged"] = err == nil
		*p.interrupts = append(*p.interrupts, entry)
		p.mu.Unlock()
	}
	return err
}

// Uses real Luna tools and native stop/resume. The followed object and the
// inherited blocking evidence tool are dedicated local fixtures, not a claim
// of end-to-end multi-Core collaboration acceptance.
func TestNativeShadowTaskFollow(t *testing.T) {
	root, binary := os.Getenv("TEAMCROSS_SHADOW_NATIVE_DIR"), os.Getenv("TEAMCROSS_SHADOW_CODEX_BIN")
	if root == "" || binary == "" {
		t.Skip("requires a fresh dedicated native fixture")
	}
	home, cwd := filepath.Join(root, "codex-home"), filepath.Join(root, "repo")
	config, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil || !strings.Contains(string(config), `model = "gpt-5.6-luna"`) {
		t.Fatal("fixture must explicitly select gpt-5.6-luna")
	}
	audit := &toolsetNativeAudit{}
	var interruptMu sync.Mutex
	interrupts := []map[string]any{}
	subject := spacecontext.Subject{Kind: "discussion", ID: "task-follow-discussion"}
	cfg := Config{Directory: filepath.Join(root, "engine"), IdleMemoryDelay: time.Hour,
		Start: func(ctx context.Context, h, c, log string) (Runtime, error) {
			p, err := nativecodex.StartWithMode(ctx, binary, h, c, log, runtimeconfig.Trusted)
			if err != nil {
				return nil, err
			}
			return &taskFollowAuditRuntime{toolsetAuditRuntime: &toolsetAuditRuntime{Runtime: p, audit: audit}, mu: &interruptMu, interrupts: &interrupts}, nil
		},
		EnsureFollow: func(_ context.Context, got spacecontext.Subject, id string) (spacecontext.FollowSetting, error) {
			if got != subject || id == "" {
				return spacecontext.FollowSetting{}, fmt.Errorf("fixture follow identity lost")
			}
			return spacecontext.FollowSetting{Subject: subject, Mode: "following", Revision: 1}, nil
		},
	}
	e, err := New(cfg, uuid.NewString(), "task-follow-native", "Task follow fixture", home, cwd, Actor{ID: "owner", Name: "Owner"}, ModelSettings{Model: "gpt-5.6-luna", ModelProvider: "openai", ReasoningEffort: "low"})
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenAttention(filepath.Join(root, "attention"), "task-follow-native", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.AttachAttention(store); err != nil {
		t.Fatal(err)
	}
	engines := []*Engine{e}
	approved := map[string]bool{}
	defer func() {
		// Release only this fixture's native MCP gate, including failed runs.
		_ = os.WriteFile(filepath.Join(cwd, "allow-worker-complete"), []byte("fixture cleanup\n"), 0600)
		for _, engine := range engines {
			engine.Close()
		}
		audit.mu.Lock()
		defer audit.mu.Unlock()
		interruptMu.Lock()
		defer interruptMu.Unlock()
		e.mu.Lock()
		jobs := clone(e.record.Jobs)
		e.mu.Unlock()
		_ = writeAtomic(filepath.Join(root, "report.json"), map[string]any{"passed": !t.Failed(), "audit": audit, "interrupts": interrupts, "jobs": jobs, "view": e.View(), "attention": store.Snapshot(), "fixtureToolApprovals": len(approved)})
	}()
	wait := func(label string, check func() bool) {
		t.Helper()
		deadline := time.Now().Add(180 * time.Second)
		for time.Now().Before(deadline) {
			// Same bounded fixture consent as native_acceptance_test.go. This
			// exercises inherited native policy without changing product policy.
			for _, approval := range e.Approvals() {
				var p struct {
					ServerName string `json:"serverName"`
					Message    string `json:"message"`
				}
				_ = json.Unmarshal(approval.Params, &p)
				// Server request IDs are scoped to one native runtime and can be
				// reused after restart. The fixture must not suppress new consent.
				key := fmt.Sprintf("%p:%s", e, approval.ID)
				if approval.Method == "mcpServer/elicitation/request" && p.ServerName == "shadow_fixture" && (strings.Contains(p.Message, "fixture_probe") || strings.Contains(p.Message, "fixture_wait_and_read")) && !approved[key] {
					if err := e.Respond(context.Background(), approval.ID, map[string]any{"action": "accept", "content": map[string]any{}}); err != nil {
						t.Fatal(err)
					}
					approved[key] = true
				}
			}
			if check() {
				t.Log(label)
				return
			}
			if reason := e.DiagnosticError(); reason != "" {
				t.Fatalf("%s: %s", label, reason)
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("timeout %s; view=%s", label, jsonText(e.View()))
	}
	message := func(text, marker string) {
		t.Helper()
		_, err := e.Receive(Message{ID: uuid.NewString(), Actor: Actor{ID: "owner", Name: "Owner"}, Text: text})
		if err != nil {
			t.Fatal(err)
		}
		wait(marker, func() bool {
			for _, m := range e.View().Messages {
				if m.Role == "assistant" && strings.Contains(m.Text, marker) {
					return true
				}
			}
			return false
		})
	}
	e.Start()
	message(`这是专用验证，请直接执行这些工具操作：用 shadow_watch start 关注 discussion task-follow-discussion，goal 为“等待隔离夹具证据，然后核对原目标”，requestId 为 native-follow-start，baseGoalVersion 为 0；再 shadow_task start 创建一个 work 任务，followId 使用返回的认识 ID。任务标题为“Follow lifecycle fixture”，完成条件为“取得原目标所需证据”。任务 prompt 必须原样包含：“先用 shadow_task status 和 checkpoint 保存原目标进展，再调用继承的 shadow_fixture MCP fixture_wait_and_read 等待测试证据。这个调用只读取专用夹具。若目标尚未变化，取得证据后按当前完成条件 finish。若后续收到目标重核指示，优先遵循最新私有 follow goal；不要重复旧的等待调用。” 主轮派发后立即结束，回复 FOLLOW_TASK_STARTED，不等待工作结果，也不代替工作轮调用夹具。`, "FOLLOW_TASK_STARTED")
	var jobID string
	wait("worker entered native evidence tool", func() bool {
		_, err := os.Stat(filepath.Join(cwd, "worker-gate-entered"))
		if err != nil {
			return false
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		for _, j := range e.record.Jobs {
			if j.Follow != nil && j.State == "running" && j.Checkpoint != nil {
				jobID = j.ID
				return true
			}
		}
		return false
	})
	first := currentJob(t, e, jobID)
	message(fmt.Sprintf("用 shadow_watch read 读取 %s，然后 pause 同一目标，requestId=native-follow-pause，使用实际 baseGoalVersion。回复 FOLLOW_TASK_PAUSED。", first.Follow.FocusID), "FOLLOW_TASK_PAUSED")
	wait("native worker actually stopped", func() bool { return currentJob(t, e, jobID).State == "paused" })
	interruptMu.Lock()
	stopped := len(interrupts) == 1 && interrupts[0]["threadId"] == first.ThreadID && interrupts[0]["acknowledged"] == true
	interruptMu.Unlock()
	if !stopped {
		t.Fatal("missing exact acknowledged native interruption")
	}
	message(fmt.Sprintf("用 shadow_watch read 读取 %s，再 update，requestId=native-follow-update，goal 改为：\"新目标：核对继承的 MCP 只读 fixture_probe 返回 INHERITED_MCP_MARKER。必须先在原任务中调用 shadow_task replan，以当前 baseVersion/baseRevision 保存新 prompt、完成条件、checkpoint，并传 retainDependencyIds 空数组；保留已做记录。然后调用 fixture_probe，核对标记并 shadow_task finish completed，result 中包含 FOLLOW_REPLAN_DONE。旧的等待证据不再是当前要求。\" 保持暂停，回复 FOLLOW_GOAL_UPDATED。", first.Follow.FocusID), "FOLLOW_GOAL_UPDATED")
	wait("paused latest goal retained", func() bool { j := currentJob(t, e, jobID); return j.State == "paused" && j.Version == 3 })
	if err = os.WriteFile(filepath.Join(cwd, "allow-worker-complete"), []byte("release interrupted fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	e.Close()
	e, err = Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	engines = append(engines, e)
	store, err = OpenAttention(store.directory, "task-follow-native", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.AttachAttention(store); err != nil {
		t.Fatal(err)
	}
	e.Start()
	message(fmt.Sprintf("用 shadow_watch read 读取 %s，再 resume，requestId=native-follow-resume，使用实际 baseGoalVersion。主轮只恢复关注，原工作会话负责目标重核；回复 FOLLOW_TASK_RESUMED。", first.Follow.FocusID), "FOLLOW_TASK_RESUMED")
	wait("same worker replanned and completed revised goal", func() bool {
		j := currentJob(t, e, jobID)
		return j.State == "completed" && j.ResultEventID != "" && strings.Contains(j.Result, "FOLLOW_REPLAN_DONE")
	})
	last := currentJob(t, e, jobID)
	if last.ThreadID != first.ThreadID || last.Version != 4 || last.ResultVersion != 4 || last.Follow.NeedsReplan || len(last.CompletionChecks) == 0 {
		t.Fatal(last)
	}
	audit.mu.Lock()
	replanned, resumedWorker := false, false
	for _, call := range audit.Calls {
		if call.ThreadID == first.ThreadID && call.Tool == "shadow_task" && strings.Contains(string(call.Arguments), `"replan"`) {
			replanned = true
		}
	}
	for _, id := range audit.Resumes {
		resumedWorker = resumedWorker || id == first.ThreadID
	}
	count := len(audit.Starts)
	audit.mu.Unlock()
	if !replanned || !resumedWorker || count != 2 {
		t.Fatal("replan or exact resume missing; unexpected replacement thread", replanned, resumedWorker, count)
	}
}
