package shadow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/mcp"
	"teamcross/internal/nativecodex"
	"teamcross/internal/runtimeconfig"
	"teamcross/internal/spacecontext"
)

// Real native tool use against isolated collaboration callbacks. The separate
// collab tests cover authenticated Core transport and business question lookup.
func TestNativeShadowTaskQuestion(t *testing.T) {
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
	var ready atomic.Bool
	var sends, reads, sourceReads atomic.Int32
	const actionID = "native-question-action"
	cfg := Config{Directory: filepath.Join(root, "engine"), IdleMemoryDelay: time.Hour, DependencyPollInterval: 200 * time.Millisecond, CollaborationTools: mcp.ShadowTools(), ValidateTool: mcp.ValidateShadowCall,
		Start: func(ctx context.Context, h, c, log string) (Runtime, error) {
			p, err := nativecodex.StartWithMode(ctx, binary, h, c, log, runtimeconfig.Trusted)
			if err != nil {
				return nil, err
			}
			return &toolsetAuditRuntime{Runtime: p, audit: audit}, nil
		},
		CallTool: func(_ context.Context, name string, args map[string]any) (any, error) {
			if name != "reply_to_annotation" || args["requestId"] != actionID || args["annotationId"] != "question-fixture-discussion" {
				return nil, fmt.Errorf("only the dedicated question command is available")
			}
			sends.Add(1)
			return nil, fmt.Errorf("fixture: remote question saved, acknowledgement lost; exact dependency read will reconcile")
		},
		ReadSource: func(_ context.Context, ref spacecontext.SourceRef) (SourceSnapshot, error) {
			if ref.SpaceID != "question-native" || ref.AnnotationID != "original-compatibility-question" || ref.ReplyID != "" {
				return SourceSnapshot{}, fmt.Errorf("only the exact original question is available")
			}
			sourceReads.Add(1)
			return handlingSnapshot(ref, 0), nil
		},
		ReadDependency: func(_ context.Context, d TaskDependency) (DependencyUpdate, error) {
			reads.Add(1)
			if !ready.Load() {
				return DependencyUpdate{Cursor: d.Cursor}, nil
			}
			if d.QuestionActionID != actionID || d.QuestionID == "" {
				return DependencyUpdate{}, fmt.Errorf("durable question identity was lost")
			}
			question := spacecontext.SourceRef{SpaceID: "question-native", AnnotationID: "question-fixture-discussion", ReplyID: "question-fixture-post"}
			answer := question
			answer.ReplyID = "owner-version-answer"
			return DependencyUpdate{QuestionSource: &question, Cursor: "reply:2", Signals: []DependencySignal{{ID: "reply:owner-version-answer", Source: answer, Actor: spacecontext.Actor{Kind: "human", MemberID: "owner"}, State: "answered", Preview: "本夹具已确认需支持客户端 v3；原问题所需事实已齐全。"}}}, nil
		},
	}
	e, err := New(cfg, uuid.NewString(), "question-native", "Question fixture", home, cwd, Actor{ID: "owner", Name: "Owner"}, ModelSettings{Model: "gpt-5.6-luna", ModelProvider: "openai", ReasoningEffort: "low"})
	if err != nil {
		t.Fatal(err)
	}
	engines := []*Engine{e}
	defer func() {
		for _, engine := range engines {
			engine.Close()
		}
		audit.mu.Lock()
		defer audit.mu.Unlock()
		e.mu.Lock()
		jobs, questions, actions := clone(e.record.Jobs), clone(e.record.Questions), clone(e.record.Actions)
		e.mu.Unlock()
		_ = writeAtomic(filepath.Join(root, "report.json"), map[string]any{"passed": !t.Failed(), "audit": audit, "jobs": jobs, "questions": questions, "actions": actions, "questionWrites": sends.Load(), "dependencyReads": reads.Load(), "relationReads": sourceReads.Load(), "view": e.View()})
	}()
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
	_, err = e.Receive(Message{ID: uuid.NewString(), Actor: Actor{ID: "owner", Name: "Owner"}, Text: `这是专用协作验证。请直接调用一次 shadow_task ask 建立一个 kind=work 的等待任务，title=Compatibility question；prompt 为“读取 shadow_task status 中实际到达的依赖信号，根据答案确定支持的客户端版本；用原依赖 ID 和 signalIds 明确解除等待，再以当前版本、检查点和完成条件逐项核对调用 finish completed，result 包含 QUESTION_RESUMED 和具体版本。该专用夹具的精确依赖信号就是证据，无需再调用其他业务工具。” completionCriteria 只有“根据原问题的实际答复确定需要支持的客户端版本”。checkpoint 写明当前缺版本、答复后继续上述工作。question.tool=reply_to_annotation，arguments 只含 annotationId=question-fixture-discussion、requestId=native-question-action、text=需要支持哪个客户端版本？；condition=明确需要支持的客户端版本。question.respondsTo 必须为 [{"spaceId":"question-native","annotationId":"original-compatibility-question"}]，说明这次提问在推进该原问题。如果发送回执待核对，Core 会按原行动读取；不要重发，不要另开重复任务，不要把回执丢失当作不存在。主轮登记后立即结束，回复 QUESTION_REGISTERED，随后等待系统自动继续。`})
	if err != nil {
		t.Fatal(err)
	}
	var jobID, questionID string
	wait("native ask registered the real waiting dependency", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		for _, q := range e.record.Questions {
			j := e.record.Jobs[q.JobID]
			if q.ActionID == actionID && q.Delivery == "unknown" && j.State == "waiting" && j.Kind == "work" && len(j.Dependencies) == 1 && q.RelationsRecorded && len(q.RespondsTo) == 2 && sourceReads.Load() == 1 {
				jobID, questionID = j.ID, q.ID
			}
		}
		for _, m := range e.record.Messages {
			if jobID != "" && m.Role == "assistant" && strings.Contains(m.Text, "QUESTION_REGISTERED") {
				return true
			}
		}
		return false
	})
	e.Close()
	ready.Store(true)
	e, err = Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	engines = append(engines, e)
	e.Start()
	wait("restored task consumed exact answer and completed", func() bool {
		j := currentJob(t, e, jobID)
		return j.State == "completed" && j.ResultEventID != "" && strings.Contains(j.Result, "QUESTION_RESUMED")
	})
	j := currentJob(t, e, jobID)
	e.mu.Lock()
	q, a := e.record.Questions[questionID], e.record.Actions[actionID]
	e.mu.Unlock()
	if sends.Load() != 1 || q.Delivery != "saved" || a.State != "unknown" || j.Dependencies[0].State != "satisfied" || len(j.CompletionChecks) != 1 || !strings.Contains(j.Result, "v3") {
		t.Fatal("native recovery lost task, invented receipt, or resent question", sends.Load(), q, a, j)
	}
	audit.mu.Lock()
	asked, finished := false, false
	for _, call := range audit.Calls {
		if call.Tool != "shadow_task" {
			continue
		}
		asked = asked || taskCallOperation(call.Arguments) == "ask"
		finished = finished || taskCallOperation(call.Arguments) == "finish"
	}
	starts := len(audit.Starts)
	audit.mu.Unlock()
	if !asked || !finished || starts != 2 {
		t.Fatal("expected one native main and one resumed-task worker", asked, finished, starts)
	}
}
