package shadow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/nativecodex"
	"teamcross/internal/runtimeconfig"
	"teamcross/internal/spacecontext"
)

// Actual native tools and worker recovery, with explicitly controlled source
// facts. The collab suite separately exercises authenticated multi-Core reads.
func TestNativeShadowTaskSources(t *testing.T) {
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
	var changed, readSnapshot atomic.Bool
	cfg := Config{Directory: filepath.Join(root, "engine"), IdleMemoryDelay: time.Hour,
		ReadTaskSource: func(_ context.Context, q TaskSourceQuery) (TaskSourceUpdate, error) {
			if q.Subject.ID != "interface" {
				readSnapshot.Store(true)
			}
			out := TaskSourceUpdate{Cursor: q.Cursor}
			out.Cursor.Initialized = true
			if changed.Load() && q.Cursor.Seq < 2 {
				out.Cursor.Seq = 2
				out.Signals = []DependencySignal{{ID: "actual-interface-v2", State: "material_updated", Source: spacecontext.SourceRef{SpaceID: "source-native", MaterialID: "interface", Version: 2}, Preview: "ACTUAL_SOURCE_V2：本夹具实际确认超时后请求可能已经写入，需要核对请求结果再决定是否重试。OIDC 登录不变。"}}
			}
			return out, nil
		},
		Start: func(ctx context.Context, h, c, log string) (Runtime, error) {
			p, err := nativecodex.StartWithMode(ctx, binary, h, c, log, runtimeconfig.Trusted)
			if err != nil {
				return nil, err
			}
			return &toolsetAuditRuntime{Runtime: p, audit: audit}, nil
		},
	}
	e, err := New(cfg, uuid.NewString(), "source-native", "Task source fixture", home, cwd, Actor{ID: "owner", Name: "Owner"}, ModelSettings{Model: "gpt-5.6-luna", ModelProvider: "openai", ReasoningEffort: "low"})
	if err != nil {
		t.Fatal(err)
	}
	engines := []*Engine{e}
	store, err := OpenAttention(filepath.Join(root, "attention"), "source-native", "owner")
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
		_ = writeAtomic(filepath.Join(root, "report.json"), map[string]any{"passed": !t.Failed(), "audit": audit, "jobs": jobs, "runs": runs, "attention": store.Snapshot(), "snapshotPolled": readSnapshot.Load(), "view": e.View()})
	}()
	if err = e.AttachAttention(store); err != nil {
		t.Fatal(err)
	}
	saveTaskContextFacts(t, store, "interface", "INTERFACE_V1：登录采用 OIDC，超时后写入语义未明确，等待新版说明。", 1)
	saveTaskContextFacts(t, store, "validation", "FIXED_VALIDATION：本夹具在此固定版本已验证客户端 v3。", 1)
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
	_, err = e.Receive(Message{ID: uuid.NewString(), Actor: Actor{ID: "owner", Name: "Owner"}, Text: `这是专用任务来源变化验证。用 shadow_task start 创建唯一 kind=work 任务，title=Source continuation，relatedFocuses=[{"id":"interface","reason":"迁移需要最新超时写入事实","mode":"live"},{"id":"validation","reason":"沿用此固定版本的客户端验证","mode":"snapshot"}]，completionCriteria 为“用实际接口 v2 说明确认超时写入语义并写回认识”和“保留 OIDC 与固定客户端 v3 验证”。prompt 完整转述以下要求：首次只看到 INTERFACE_V1 时，对两个 contexts 的准确 current.revision 提交 contextReviews，并用 wait 保存检查点与等待源变化后的下一步，dependencies 可省略。不要自己造新版、另行发问或另建任务。系统重启后会通过本任务 sourceChanges.signals 提供 ACTUAL_SOURCE_V2，preview 是这个专用夹具提供的完整实际事实，引用其 source 即可，无需外部工具。收到后读取 status，在 understandings 的准确目标和 answer IDs 上调用 understand，合并 v2 与原有 OIDC 事实并清除已解决问题；固定 validation 快照继续使用客户端 v3，不跟随外部新版本。再次读取 status，提交变化项 contextReviews，finish completed 保存 checkpoint 和逐项 criteriaChecks，result 包含 SOURCE_RESUMED、超时可能已经写入、OIDC、客户端 v3。所有操作使用准确 baseVersion/baseRevision；若状态有并发更新，重新读取 status。主轮派发后回复 SOURCE_REGISTERED 并结束，不等待后台任务。`})
	if err != nil {
		t.Fatal(err)
	}
	var before Job
	wait("native worker saved live and fixed source contexts", func() bool {
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
	e.Close()
	store.mu.Lock()
	next := clone(store.record)
	next.Follows = map[string]spacecontext.FollowSetting{attentionHash(before.Contexts[0].Subject): {Subject: before.Contexts[0].Subject, Mode: "muted", Revision: 2}}
	if err = store.saveLocked(next); err != nil {
		t.Fatal(err)
	}
	store.mu.Unlock()
	saveTaskContextFacts(t, store, "validation", "LATER_VALIDATION：后续版本已改为客户端 v4；此变化不属于原固定任务范围。", 2)
	changed.Store(true)
	e, err = Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	engines = append(engines, e)
	if err = e.AttachAttention(store); err != nil {
		t.Fatal(err)
	}
	e.Start()
	var after Job
	wait("native worker used new source facts and retained fixed evidence", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		after = clone(e.record.Jobs[before.ID])
		return after.State == "completed" && !e.taskRunActiveLocked(before.ID)
	})
	wait("native fixture delivered its result and settled all started turns", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		job := e.record.Jobs[before.ID]
		if job.ResultEventID == "" || job.DeliveredEventID != job.ResultEventID {
			return false
		}
		for _, run := range e.record.Runs {
			if activeRun(run) {
				return false
			}
		}
		return true
	})
	current := store.Snapshot().Focuses["interface"]
	state := store.Snapshot()
	if after.ThreadID != before.ThreadID || after.Version != before.Version || len(e.View().Jobs) != 1 || readSnapshot.Load() || len(state.UnderstandingCommands) != 1 || len(state.UnderstoodAnswers) != 1 || !strings.Contains(current.Summary, "OIDC") || !strings.Contains(after.Result, "SOURCE_RESUMED") || !strings.Contains(after.Result, "v3") || after.Contexts[1].Current.Evidence[0].Version != 1 || len(after.Contexts[0].Source.Signals) != 0 {
		t.Fatal("native source continuation changed task identity, lost facts or expanded the snapshot scope", after, current)
	}
}
