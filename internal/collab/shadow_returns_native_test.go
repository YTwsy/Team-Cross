package collab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/inflow"
	"teamcross/internal/mcp"
	"teamcross/internal/nativecodex"
	"teamcross/internal/runtimeconfig"
	"teamcross/internal/service"
	"teamcross/internal/shadow"
)

// Real Luna creates and finishes the job. The originating conversation identity
// is a controlled transcript fixture; delivery uses the actual Hook HTTP path.
func TestNativeShadowTaskReturns(t *testing.T) {
	root, binary := os.Getenv("TEAMCROSS_SHADOW_NATIVE_DIR"), os.Getenv("TEAMCROSS_SHADOW_CODEX_BIN")
	if root == "" || binary == "" {
		t.Skip("requires a dedicated native fixture")
	}
	home, cwd := filepath.Join(root, "codex-home"), filepath.Join(root, "repo")
	config, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil || !strings.Contains(string(config), `model = "gpt-5.6-luna"`) {
		t.Fatal("fixture must select gpt-5.6-luna")
	}
	a, _, s := spaceFixture(t)
	b, _, _ := fixture(t)
	stopWorkbenchPump(b)
	if err := s.Share(context.Background(), "lan"); err != nil {
		t.Fatal(err)
	}
	j, err := b.Join(context.Background(), s.sharedRuntime().Token())
	if err != nil {
		t.Fatal(err)
	}
	v := consumerRegister(t, a, s.record.ID)
	cfg := a.shadowConfig(v.ID)
	cfg.Directory = filepath.Join(root, "engine")
	cfg.IdleMemoryDelay = time.Hour
	cfg.Start = func(ctx context.Context, h, c, log string) (shadow.Runtime, error) {
		return nativecodex.StartWithMode(ctx, binary, h, c, log, runtimeconfig.Trusted)
	}
	e, err := shadow.New(cfg, v.ID, s.record.ID, v.Name, home, cwd, v.Owner, shadow.ModelSettings{Model: "gpt-5.6-luna", ModelProvider: "openai", ReasoningEffort: "low"})
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.shadows[v.ID] = e
	a.mu.Unlock()
	items := []inflow.Item{}
	var hookOutput string
	defer func() {
		a.mu.Lock()
		delete(a.shadows, v.ID)
		a.mu.Unlock()
		e.Close()
		_ = writeJSONFile(filepath.Join(root, "report.json"), map[string]any{"passed": !t.Failed(), "view": e.View(), "items": items, "hookOutput": hookOutput, "pendingResultExports": len(e.ResultExport()), "pendingReplyExports": len(e.ReplyExport()), "scope": "One Mac; two authenticated Core endpoints; real Luna worker and checked task result; fixture originating conversation; actual Hook attachment and receipt. No active wakeup or second Mac."})
	}()
	ctx := context.Background()
	e.Start()
	wait := func(label string, check func() bool) {
		t.Helper()
		deadline := time.Now().Add(200 * time.Second)
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
		t.Fatal("timeout", label)
	}
	wait("native shadow registered online", func() bool {
		out := workbenchDecode[shadowSpaceView](shadowOp(t, a, s.record.ID, "view", shadowInput{ID: v.ID}))
		return out.Shadows[0].Online
	})
	c := shadowReturnCaller(t, b)
	messageID := uuid.NewString()
	args := map[string]any{"spaceId": j.ID, "shadowId": v.ID, "messageId": messageID, "text": fmt.Sprintf(`这是任务返回验证。请用 shadow_task start 创建一个 kind=work 的后台任务，title=Return evidence，prompt 为“读取文件 %s，核对真实内容含 WORKER_EVIDENCE_MARKER；用 shadow_task status 获取当前任务版本和 revision，然后显式 finish outcome=completed，result 必须包含 RETURN_EVIDENCE_CONFIRMED 及读到的标记，criteriaChecks 逐项说明实际核对结果。不要仅输出 final 代替 finish。” completionCriteria 只有“已读取指定文件并确认 WORKER_EVIDENCE_MARKER”。主轮登记后回复 TASK_DELEGATED 并结束，结果由后台任务完成后返回，不要询问确认。`, filepath.Join(cwd, "worker-evidence.txt"))}
	if _, err = b.invokeWorkbenchTool(ctx, "send_space_shadow", args, c, ""); err != nil {
		t.Fatal(err)
	}
	var result shadow.TaskResult
	wait("real worker finished checked business result", func() bool {
		for _, r := range e.View().TaskResults {
			if r.State == "completed" && r.MessageID == messageID && strings.Contains(r.Text, "RETURN_EVIDENCE_CONFIRMED") && len(r.CompletionChecks) == 1 {
				result = r
				return true
			}
		}
		return false
	})
	wait("host saved result and reply exports", func() bool {
		view := e.View()
		for _, job := range view.Jobs {
			if job.ResultEventID != "" && job.ResultEventID != job.DeliveredEventID {
				return false
			}
		}
		return len(e.ResultExport()) == 0 && len(e.ReplyExport()) == 0 && !view.Busy
	})
	server := httptest.NewServer(b.Handler(http.NotFoundHandler()))
	defer server.Close()
	dir, _ := service.Normalize(t.TempDir())
	connection, _ := json.Marshal(service.Connection{URL: server.URL, PID: os.Getpid(), Instance: "return-fixture", Token: b.Token, DataDir: dir})
	if err = os.WriteFile(filepath.Join(dir, "connection.json"), connection, 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err = mcp.WriteInFlowHook(ctx, dir, "", "", c.Caller, &output); err != nil {
		t.Fatal(err)
	}
	hookOutput = output.String()
	if !strings.Contains(hookOutput, result.ID) || !strings.Contains(hookOutput, "shadow_task_result") || !strings.Contains(hookOutput, messageID) {
		t.Fatal("Hook lost exact completed result", hookOutput)
	}
	// Read the saved receipt from the host, without triggering another model.
	s.mu.Lock()
	for _, route := range s.record.Shadows[v.ID].Returns {
		for key, event := range route.Events {
			if _, delivered := route.Delivered[key]; delivered {
				items = append(items, event.Item)
			}
		}
	}
	s.mu.Unlock()
	if len(items) == 0 || len(inflowBatch(t, b, c.Caller).Items) != 0 {
		t.Fatal("Hook acknowledgement did not drain the return")
	}
	got := workbenchDecode[ShadowSourceResult](shadowOp(t, b, j.ID, "source", shadowInput{ID: v.ID, ResultID: result.ID}))
	if got.Result == nil || got.Result.Text != result.Text {
		t.Fatal("returned result source unavailable")
	}
}
