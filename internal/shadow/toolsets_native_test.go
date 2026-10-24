package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/nativecodex"
	"teamcross/internal/runtimeconfig"
)

type toolsetNativeAudit struct {
	mu      sync.Mutex
	Starts  []map[string]any `json:"starts"`
	Resumes []string         `json:"resumes"`
	Calls   []toolCall       `json:"calls"`
}

type toolsetAuditRuntime struct {
	Runtime
	audit *toolsetNativeAudit
}

func (p *toolsetAuditRuntime) Call(ctx context.Context, method string, params, out any) error {
	if err := p.Runtime.Call(ctx, method, params, out); err != nil {
		return err
	}
	input := cloneMap(params)
	p.audit.mu.Lock()
	defer p.audit.mu.Unlock()
	if method == "thread/start" {
		var result struct {
			Thread struct{ ID string } `json:"thread"`
		}
		_ = assign(out, &result)
		p.audit.Starts = append(p.audit.Starts, map[string]any{"threadId": result.Thread.ID, "dynamicTools": input["dynamicTools"], "model": input["model"], "modelProvider": input["modelProvider"]})
	}
	if method == "thread/resume" {
		if input["dynamicTools"] != nil {
			return fmt.Errorf("thread/resume received unsupported dynamicTools")
		}
		p.audit.Resumes = append(p.audit.Resumes, fmt.Sprint(input["threadId"]))
	}
	return nil
}

func (p *toolsetAuditRuntime) SetHandler(h func(nativecodex.Message)) {
	p.Runtime.SetHandler(func(message nativecodex.Message) {
		if message.Method == "item/tool/call" {
			var call toolCall
			if json.Unmarshal(message.Params, &call) == nil {
				p.audit.mu.Lock()
				p.audit.Calls = append(p.audit.Calls, call)
				p.audit.mu.Unlock()
			}
		}
		h(message)
	})
}

// The original and upgraded tools run in real dedicated native sessions. The
// answer source is a controlled fixture, not a claim of a cross-Core user flow.
func TestNativeShadowToolsetUpgrade(t *testing.T) {
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
	var answerReady atomic.Bool
	marker := "UPGRADED_EVIDENCE_" + uuid.NewString()
	cfg := Config{Directory: filepath.Join(root, "engine"), IdleMemoryDelay: time.Hour,
		Start: func(ctx context.Context, h, c, log string) (Runtime, error) {
			p, err := nativecodex.StartWithMode(ctx, binary, h, c, log, runtimeconfig.Trusted)
			if err != nil {
				return nil, err
			}
			return &toolsetAuditRuntime{Runtime: p, audit: audit}, nil
		},
		ReadDependency: func(_ context.Context, dependency TaskDependency) (DependencyUpdate, error) {
			if !answerReady.Load() {
				return DependencyUpdate{}, nil
			}
			ref := dependency.Source
			ref.ReplyID = "upgrade-ready-answer"
			return DependencyUpdate{Cursor: "answer-1", Signals: []DependencySignal{{ID: "upgrade-ready", Source: ref, State: "answered", Preview: "The fixture evidence is now available through read_upgrade_evidence. Read it and finish the original task."}}}, nil
		}}
	e, err := New(cfg, uuid.NewString(), "toolset-native-fixture", "Upgrade fixture", home, cwd, Actor{ID: "owner", Name: "Owner"}, ModelSettings{Model: "gpt-5.6-luna", ModelProvider: "openai", ReasoningEffort: "low"})
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
		_ = writeAtomic(filepath.Join(root, "report.json"), map[string]any{"audit": audit, "finalView": engines[len(engines)-1].View(), "passed": !t.Failed()})
	}()
	wait := func(engine *Engine, label string, ready func() bool) {
		t.Helper()
		deadline := time.Now().Add(180 * time.Second)
		for time.Now().Before(deadline) {
			if ready() {
				t.Log(label)
				return
			}
			if err := engine.DiagnosticError(); err != "" {
				t.Fatalf("%s: %s", label, err)
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("timeout %s; view=%s", label, jsonText(engine.View()))
	}
	answered := func(engine *Engine, message Message, want string) bool {
		for _, m := range engine.View().Messages {
			if m.ReplyTo == message.ID && m.Role == "assistant" && m.State == "completed" && strings.Contains(m.Text, want) {
				return true
			}
		}
		return false
	}
	e.Start()
	first := send(t, e, "owner", "Dedicated continuity test. The fixture will enqueue one controlled background task to wait for evidence, then read it and finish. Do not enqueue work yourself. Reply only LEGACY_NATIVE_READY.")
	wait(e, "original main persisted", func() bool { return answered(e, first, "LEGACY_NATIVE_READY") })
	job := Job{ID: uuid.NewString(), Kind: "work", SourceID: first.ID, Title: "Read evidence after the fixture answer", Version: 1, Revision: 1, State: "queued", CreatedAt: time.Now(), UpdatedAt: time.Now(), CompletionCriteria: []string{"Read the fixture evidence tool after the answer and report its exact returned marker."}}
	job.Prompt = "This is an isolated continuity test. On the first turn there is no answer: call shadow_task wait with dependency id evidence-ready, kind discussion, condition Evidence is available, source {spaceId:toolset-native-fixture, annotationId:fixture-discussion, replyId:fixture-question}. Save a checkpoint explaining that no evidence has been read yet, then end the turn. On continuation, if the dependency contains the actual upgrade-ready signal, call the newly available read_upgrade_evidence tool, use its returned marker as the result, resolve evidence-ready using that signal ID, and finish completed with a check for the completion criterion. Do not create new tasks, ask the owner for permission, or use shell/network tools. Use shadow_task status for the current version and revision."
	e.mu.Lock()
	err = e.mutateLocked(func() error { e.record.Jobs[job.ID] = job; return nil })
	e.notify()
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	wait(e, "original worker persisted a wait and ended its native turn", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		j := e.record.Jobs[job.ID]
		return j.State == "waiting" && j.Checkpoint != nil && !e.threadBusyLocked(j.ThreadID)
	})
	oldMain, oldWorker := e.View().Current, currentJob(t, e, job.ID).ThreadID
	e.Close()
	changeToolsetRecord(t, cfg, func(record *Record) {
		for i := range record.Segments {
			record.Segments[i].ToolsetDigest = ""
		}
		j := record.Jobs[job.ID]
		j.ToolsetDigest = ""
		record.Jobs[j.ID] = j
	})
	cfg.CollaborationTools = []map[string]any{extraFixtureTool()}
	cfg.CallTool = func(_ context.Context, name string, _ map[string]any) (any, error) {
		if name != "read_upgrade_evidence" {
			return nil, fmt.Errorf("unexpected fixture tool")
		}
		return map[string]any{"marker": marker, "evidence": "The exact fixture answer makes v3 support verifiable."}, nil
	}
	e2, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	engines = append(engines, e2)
	second := send(t, e2, "owner", "Read the read_upgrade_evidence tool and include its exact returned marker in your reply. Do not create tasks; let the existing background task continue independently.")
	answerReady.Store(true)
	e2.Start()
	wait(e2, "upgraded main actually called the newly registered tool", func() bool { return answered(e2, second, marker) })
	wait(e2, "upgraded worker resolved its original dependency and finished", func() bool {
		j := currentJob(t, e2, job.ID)
		return j.State == "completed" && strings.Contains(j.Result, marker) && len(j.CompletionChecks) == 1 && len(j.Dependencies) == 1 && j.Dependencies[0].State == "satisfied"
	})
	wait(e2, "result delivery settled", func() bool {
		j := currentJob(t, e2, job.ID)
		return !e2.View().Busy && j.ResultEventID == j.DeliveredEventID
	})
	newMain, migrated := e2.View().Current, currentJob(t, e2, job.ID)
	if newMain == oldMain || migrated.ThreadID == oldWorker || migrated.Version != job.Version || len(migrated.PreviousThreads) != 1 || migrated.PreviousThreads[0] != oldWorker {
		t.Fatal("native upgrade lost lineage or changed the business goal", migrated)
	}
	e2.Close()
	e3, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	engines = append(engines, e3)
	third := send(t, e3, "owner", "After this restart, read read_upgrade_evidence once and return its exact marker. Do not create tasks.")
	e3.Start()
	wait(e3, "restart reused the upgraded main and its tools", func() bool { return answered(e3, third, marker) })
	if e3.View().Current != newMain {
		t.Fatal("unchanged native tools forced another segment after restart")
	}
	audit.mu.Lock()
	defer audit.mu.Unlock()
	mainCalled, workerCalled := false, false
	for _, call := range audit.Calls {
		if call.Tool == "read_upgrade_evidence" {
			mainCalled = mainCalled || call.ThreadID == newMain
			workerCalled = workerCalled || call.ThreadID == migrated.ThreadID
		}
	}
	if !mainCalled || !workerCalled || len(audit.Starts) != 4 {
		t.Fatal("native registration/call evidence is incomplete or creation repeated", mainCalled, workerCalled, len(audit.Starts))
	}
}
