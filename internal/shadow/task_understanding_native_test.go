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
	"teamcross/internal/spacecontext"
)

// Real native tools and exact worker recovery, with controlled source facts.
// The complete multi-Core discussion/inflow path has separate acceptance.
func TestNativeShadowTaskUnderstanding(t *testing.T) {
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
		ReadDependency: func(_ context.Context, d TaskDependency) (DependencyUpdate, error) {
			return DependencyUpdate{Cursor: d.Cursor}, nil
		},
		ResolveDependency: func(_ context.Context, d TaskDependency, signal spacecontext.Signal) (*DependencySignal, error) {
			return &DependencySignal{ID: "reply:fixture-answer", Source: signal.Source, State: "answered", Preview: "ACTUAL_ANSWER：兼容客户端 v3。既有 OIDC 登录方案保持不变。"}, nil
		},
		Start: func(ctx context.Context, h, c, log string) (Runtime, error) {
			p, err := nativecodex.StartWithMode(ctx, binary, h, c, log, runtimeconfig.Trusted)
			if err != nil {
				return nil, err
			}
			return &toolsetAuditRuntime{Runtime: p, audit: audit}, nil
		},
	}
	e, err := New(cfg, uuid.NewString(), "understanding-native", "Understanding fixture", home, cwd, Actor{ID: "owner", Name: "Owner"}, ModelSettings{Model: "gpt-5.6-luna", ModelProvider: "openai", ReasoningEffort: "low"})
	if err != nil {
		t.Fatal(err)
	}
	engines := []*Engine{e}
	store, err := OpenAttention(filepath.Join(root, "attention"), "understanding-native", "owner")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, engine := range engines {
			engine.Close()
		}
		e.mu.Lock()
		jobs, runs := clone(e.record.Jobs), clone(e.record.Runs)
		e.mu.Unlock()
		audit.mu.Lock()
		defer audit.mu.Unlock()
		_ = writeAtomic(filepath.Join(root, "report.json"), map[string]any{"passed": !t.Failed(), "audit": audit, "jobs": jobs, "runs": runs, "attention": store.Snapshot(), "view": e.View()})
	}()
	if err = e.AttachAttention(store); err != nil {
		t.Fatal(err)
	}
	signal := spacecontext.Signal{ID: "fixture-event", Seq: 1, Kind: "reply", Subject: spacecontext.Subject{Kind: "discussion", ID: "discussion"}, Source: spacecontext.SourceRef{SpaceID: "understanding-native", AnnotationID: "discussion", ReplyID: "fixture-answer"}}
	focus := seedDiscussionUnderstanding(t, store, signal)
	store.mu.Lock()
	initial := clone(store.record)
	focus.Summary = "既有事实：登录采用 OIDC，保持不变。缺少旧客户端兼容版本。"
	initial.Focuses[focus.ID] = focus
	err = store.saveLocked(initial)
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
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
	_, err = e.Receive(Message{ID: uuid.NewString(), Actor: Actor{ID: "owner", Name: "Owner"}, Text: `这是专用答复认识回写验证。请用 shadow_task start 创建唯一 kind=work 任务，title=Answer understanding，completionCriteria 为“根据实际答复确认客户端兼容版本”和“将答案并入讨论认识并保留原有 OIDC 事实”。prompt 完整包含这些要求：首次使用 shadow_task wait，dependencies 为 [{"id":"client-version","kind":"discussion","condition":"明确兼容客户端版本","source":{"spaceId":"understanding-native","annotationId":"discussion","replyId":"question"}}]，保存检查点和答案到达后继续的下一步。首次没有答案，必须等待，不能编造事实、另行发问或启动其他任务。系统会在重启后向同一任务给出 ACTUAL_ANSWER；本夹具的 signal.preview 是测试源提供的完整实际答复，无需外部工具。接续时读取任务 status，按 understandings 中准确 ID/current.revision/answers，用 understand 操作及稳定 requestId 将答案与既有 OIDC 事实合并，引用实际 signal.source，清除已解决的问题。再读取 status，用 finish completed 保存 checkpoint、逐项 criteriaChecks，并用 resolutions 引用实际信号解除 client-version 等待；result 包含 ANSWER_RECONCILED 和实际版本。主轮派发后回复 ANSWER_REGISTERED 并结束，不等待后台任务。`})
	if err != nil {
		t.Fatal(err)
	}
	var before Job
	wait("native worker saved the exact question wait", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		for _, job := range e.record.Jobs {
			if job.State == "waiting" && len(job.Dependencies) == 1 && !e.taskRunActiveLocked(job.ID) {
				before = clone(job)
				return true
			}
		}
		return false
	})
	e.Close()
	store, err = OpenAttention(store.directory, "understanding-native", "owner")
	if err != nil {
		t.Fatal(err)
	}
	e, err = Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	engines = append(engines, e)
	if err = e.AttachAttention(store); err != nil {
		t.Fatal(err)
	}
	if _, err = e.RouteDependencyActivity(context.Background(), signal); err != nil {
		t.Fatal(err)
	}
	e.Start()
	var after Job
	wait("native worker reconciled the answer and completed the same task", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		after = clone(e.record.Jobs[before.ID])
		return after.State == "completed" && !e.taskRunActiveLocked(before.ID)
	})
	state := store.Snapshot()
	current := state.Focuses[focus.ID]
	if after.ThreadID != before.ThreadID || after.Version != before.Version || len(e.View().Jobs) != 1 || len(state.UnderstandingCommands) != 1 || len(state.UnderstoodAnswers) != 1 || len(current.OpenQuestions) != 0 || !strings.Contains(current.Summary, "OIDC") || !strings.Contains(current.Summary, "v3") || !strings.Contains(after.Result, "ANSWER_RECONCILED") {
		t.Fatal("native answer reconciliation lost facts, repeated work or changed task identity", after, state)
	}
}
