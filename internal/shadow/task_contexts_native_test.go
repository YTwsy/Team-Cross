package shadow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/nativecodex"
	"teamcross/internal/runtimeconfig"
)

// The native worker uses real dynamic tools. Facts are deliberately isolated
// fixture observations; this does not claim a live team's material transport.
func TestNativeShadowTaskContexts(t *testing.T) {
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
	cfg := Config{Directory: filepath.Join(root, "engine"), IdleMemoryDelay: time.Hour,
		Start: func(ctx context.Context, h, c, log string) (Runtime, error) {
			p, err := nativecodex.StartWithMode(ctx, binary, h, c, log, runtimeconfig.Trusted)
			if err != nil {
				return nil, err
			}
			return &toolsetAuditRuntime{Runtime: p, audit: audit}, nil
		},
	}
	e, err := New(cfg, uuid.NewString(), "context-native", "Context fixture", home, cwd, Actor{ID: "owner", Name: "Owner"}, ModelSettings{Model: "gpt-5.6-luna", ModelProvider: "openai", ReasoningEffort: "low"})
	if err != nil {
		t.Fatal(err)
	}
	engines := []*Engine{e}
	defer func() {
		for _, engine := range engines {
			engine.Close()
		}
		e.mu.Lock()
		jobs, runs := clone(e.record.Jobs), clone(e.record.Runs)
		e.mu.Unlock()
		audit.mu.Lock()
		defer audit.mu.Unlock()
		_ = writeAtomic(filepath.Join(root, "report.json"), map[string]any{"passed": !t.Failed(), "audit": audit, "jobs": jobs, "runs": runs, "view": e.View()})
	}()
	store, err := OpenAttention(filepath.Join(root, "attention"), "context-native", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.AttachAttention(store); err != nil {
		t.Fatal(err)
	}
	saveTaskContextFacts(t, store, "interface", "INTERFACE_V1：超时后是否已经写入尚未明确，需要等这份认识的新版本。", 1)
	saveTaskContextFacts(t, store, "validation", "VALIDATION_FIXED：本夹具已验证客户端 v3 兼容；这份验证结论保持有效。", 1)
	wait := func(label string, check func() bool) {
		t.Helper()
		deadline := time.Now().Add(180 * time.Second)
		for time.Now().Before(deadline) {
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
	e.Start()
	_, err = e.Receive(Message{ID: uuid.NewString(), Actor: Actor{ID: "owner", Name: "Owner"}, Text: `这是专用多来源接续验证。请用 shadow_task start 创建一个 kind=work 的任务，title=Context continuation。relatedFocuses 传 [{"id":"interface","reason":"迁移依赖超时写入语义"},{"id":"validation","reason":"确认既有客户端兼容验证仍有效"}]，completionCriteria 为两项：“根据接口 v2 的实际认识确认超时写入语义”和“保留客户端 v3 的既有验证结论”。prompt 请完整包含以下要求：读取 shadow_task status 中本任务的 contexts，认识是本夹具提供的实际证据，无需调用外部工具。首次只看到 INTERFACE_V1 时，使用 contextReviews 核对两个 current.revision，说明当前缺少什么，调用 wait 保存检查点和等待接口认识更新后的下一步，dependencies 可以省略；不得自己改认识、再建任务或伪造 v2。后来系统在同一任务接续、实际收到 INTERFACE_V2 后，核对 current 与 reviewed 的变化，对变化的对象提交准确 revision 的 contextReviews，保留验证认识；用 finish completed 提交逐项 criteriaChecks，checkpoint.openQuestions 为空，result 包含 CONTEXT_RESUMED、超时可能已经写入和客户端 v3。所有检查点操作用 status 返回的当前 baseVersion/baseRevision。主轮派发后回复 CONTEXT_REGISTERED 并结束，不等待后台任务。`})
	if err != nil {
		t.Fatal(err)
	}
	var before Job
	wait("native worker reviewed two sources and saved a context wait", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		for _, job := range e.record.Jobs {
			if job.State == "waiting" && len(job.Contexts) == 2 && !pendingTaskContexts(job) && !e.taskRunActiveLocked(job.ID) {
				before = clone(job)
				return true
			}
		}
		return false
	})
	if before.ThreadID == "" || before.Follow != nil || len(before.Dependencies) != 0 {
		t.Fatal("context wait changed goal ownership or invented an answer dependency", before)
	}
	e.Close()
	saveTaskContextFacts(t, store, "interface", "INTERFACE_V2：接口 v2 已确认超时可能已经写入，重试必须带幂等键；继续保留客户端 v3 兼容。", 2)
	restored, err := OpenAttention(store.directory, "context-native", "owner")
	if err != nil {
		t.Fatal(err)
	}
	e, err = Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	engines = append(engines, e)
	if err = e.AttachAttention(restored); err != nil {
		t.Fatal(err)
	}
	e.Start()
	var after Job
	wait("same native worker reviewed changed facts and completed the original task", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		after = clone(e.record.Jobs[before.ID])
		return after.State == "completed" && !e.taskRunActiveLocked(before.ID)
	})
	if after.ThreadID != before.ThreadID || after.Version != before.Version || pendingTaskContexts(after) || len(after.CompletionChecks) != 2 || !strings.Contains(after.Result, "CONTEXT_RESUMED") || !strings.Contains(after.Result, "v3") {
		t.Fatal("native continuation lost the task, evidence review or criteria", after)
	}
	if len(e.View().Jobs) != 1 || len(e.View().Jobs[0].Contexts) != 0 {
		t.Fatal("native continuation duplicated work or published private contexts")
	}
}
