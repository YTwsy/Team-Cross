package shadow

import (
	"context"
	"encoding/json"
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

// Explicit opt-in. The fixture home must select only the repository-approved
// test model; it never resumes a personal session or writes personal config.
func TestNativeShadowAcceptance(t *testing.T) {
	root, binary := os.Getenv("TEAMCROSS_SHADOW_NATIVE_DIR"), os.Getenv("TEAMCROSS_SHADOW_CODEX_BIN")
	if root == "" || binary == "" {
		t.Skip("requires a fresh dedicated native fixture")
	}
	home, cwd := filepath.Join(root, "codex-home"), filepath.Join(root, "repo")
	config, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil || !strings.Contains(string(config), `model = "gpt-5.6-luna"`) {
		t.Fatal("fixture must explicitly select gpt-5.6-luna")
	}
	var confirmationsMu sync.Mutex
	confirmations := []map[string]any{}
	cfg := Config{Directory: filepath.Join(root, "engine"), IdleMemoryDelay: time.Hour, Start: func(ctx context.Context, h, c, l string) (Runtime, error) {
		p, err := nativecodex.StartWithMode(ctx, binary, h, c, l, runtimeconfig.Trusted)
		if err != nil {
			return nil, err
		}
		return &settingsAuditRuntime{Runtime: p, record: func(value map[string]any) {
			confirmationsMu.Lock()
			defer confirmationsMu.Unlock()
			confirmations = append(confirmations, value)
		}}, nil
	}}
	e, err := New(cfg, uuid.NewString(), "native-acceptance", "Fixture shadow", home, cwd, Actor{ID: "owner", Name: "Owner"}, ModelSettings{Model: "gpt-5.6-luna", ModelProvider: "openai", ReasoningEffort: "low"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { e.Close(); _ = writeAtomic(filepath.Join(root, "final-view.json"), e.View()) }()
	e.Start()
	approved := map[string]bool{}
	wait := func(label string, fn func() bool) {
		t.Helper()
		deadline := time.Now().Add(180 * time.Second)
		for time.Now().Before(deadline) {
			for _, approval := range e.Approvals() {
				var p struct {
					ServerName string `json:"serverName"`
					Message    string `json:"message"`
				}
				_ = json.Unmarshal(approval.Params, &p)
				key := string(approval.ID)
				if approval.Method == "mcpServer/elicitation/request" && p.ServerName == "shadow_fixture" && (strings.Contains(p.Message, "fixture_probe") || strings.Contains(p.Message, "fixture_wait_and_read")) && !approved[key] {
					if err := e.Respond(context.Background(), approval.ID, map[string]any{"action": "accept", "content": map[string]any{}}); err != nil {
						t.Fatal(err)
					}
					approved[key] = true
				}
			}
			if fn() {
				t.Log(label)
				return
			}
			v := e.View()
			if v.Error != "" {
				t.Fatalf("%s: %s", label, e.DiagnosticError())
			}
			time.Sleep(150 * time.Millisecond)
		}
		t.Fatalf("timeout: %s; view=%s approvals=%s", label, jsonText(e.View()), jsonText(e.Approvals()))
	}
	message := func(actor, text string) Message {
		t.Helper()
		m, err := e.Receive(Message{ID: uuid.NewString(), Actor: Actor{ID: actor, Name: actor}, Text: text})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	answer := func(m Message) string {
		t.Helper()
		var result string
		wait("answered "+m.ID, func() bool {
			for _, v := range e.View().Messages {
				if v.ReplyTo == m.ID && v.Role == "assistant" {
					result = v.Text
					return true
				}
			}
			return false
		})
		return result
	}
	m := message("B", "Please explicitly remember my preferred response language is Chinese, using shadow_remember with member scope. Also call the inherited fixture_probe MCP tool once and include its returned marker in your brief final reply.")
	a := answer(m)
	if !strings.Contains(a, "INHERITED_MCP_MARKER") || !strings.Contains(a, "INHERITED_CONFIG_MARKER") {
		t.Fatal("native configuration/MCP marker missing", a)
	}
	v := e.View()
	if v.Model != "gpt-5.6-luna" || len(v.Memories) != 1 || v.Memories[0].SubjectID != "B" {
		t.Fatal(v)
	}
	if v.ReasoningEffort != "low" || v.Settings.ReasoningEffort != "low" {
		t.Fatal("launch effort not confirmed", v)
	}
	if err = os.WriteFile(filepath.Join(home, "config.toml"), []byte(strings.ReplaceAll(string(config), `model_reasoning_effort = "medium"`, `model_reasoning_effort = "high"`)), 0600); err != nil {
		t.Fatal(err)
	}
	first := v.Current
	// The worker stays alive through a main turn and handoff, then the fixture
	// releases its bounded file wait. No personal files or external messages.
	jobRequest := message("B", "Start exactly one background task with shadow_task. Task: call the inherited fixture_wait_and_read MCP tool exactly once. This read-only fixture tool waits up to 180 seconds for allow-worker-complete and then returns worker-evidence.txt; return its exact content. Do not use shell commands or other tools for this task. This is explicitly authorized fixture work. Do not wait in the main conversation. Preserve the requirement that the result must contain the evidence file text.")
	answer(jobRequest)
	var worker Job
	wait("background running", func() bool {
		for _, j := range e.View().Jobs {
			if j.State == "running" {
				worker = j
				return true
			}
		}
		return false
	})
	short := answer(message("C", "While that background task is running, what is 2+3? Answer briefly. My preferences are separate from B's."))
	if !strings.Contains(short, "5") {
		t.Fatal(short)
	}
	if e.View().Jobs[0].State != "running" {
		t.Fatal("worker did not overlap main reply")
	}
	// Lower only the Core budget trigger in this dedicated replay. The native
	// model and reported context window stay unchanged.
	e.mu.Lock()
	e.cfg.CheckRatio = .00001
	e.lastMaintenance = time.Time{}
	e.mu.Unlock()
	e.notify()
	wait("native context steward completed", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		for _, r := range e.record.Runs {
			if r.Kind == "steward" && r.State == "completed" {
				return true
			}
		}
		return false
	})
	e.mu.Lock()
	e.cfg.CheckRatio = .8
	e.mu.Unlock()
	wait("native compact or automatic handoff", func() bool { v := e.View(); return len(v.Segments) > 1 || v.Segments[0].Compactions > 0 })
	if len(e.View().Segments) == 1 {
		if err := e.Rollover(); err != nil {
			t.Fatal(err)
		}
	}
	wait("S2 takes over", func() bool { return e.View().Current != first && len(e.View().Segments) == 2 })
	if e.View().Jobs[0].ThreadID != worker.ThreadID {
		t.Fatal("worker thread changed across handoff")
	}
	if err := os.WriteFile(filepath.Join(cwd, "allow-worker-complete"), []byte("go"), 0600); err != nil {
		t.Fatal(err)
	}
	wait("worker result delivered to S2", func() bool {
		v := e.View()
		for _, j := range v.Jobs {
			if j.State == "completed" && j.ResultEventID != "" && j.DeliveredEventID == j.ResultEventID {
				for _, m := range v.Messages {
					if m.Role == "assistant" && m.SegmentID == v.Current && strings.Contains(m.Text, "WORKER_EVIDENCE_MARKER") {
						return true
					}
				}
			}
		}
		return false
	})
	answer(message("B", "Correct my remembered response language to English; store the text exactly 'Response language: English'. Use shadow_recall to get the memory ID and version, then shadow_remember to update that existing member memory. Do not create a second preference."))
	v = e.View()
	if len(v.Memories) != 1 || !strings.Contains(strings.ToLower(v.Memories[0].Text), "english") {
		t.Fatal(v.Memories)
	}
	answer(message("B", "Forget my remembered response language. Use shadow_recall and shadow_remember forget, then confirm briefly."))
	v = e.View()
	if len(v.Memories) != 1 || v.Memories[0].State != "forgotten" || v.Memories[0].Text != "" {
		t.Fatal(v.Memories)
	}
	before := v.Current
	e.Close()
	e, err = Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e.Start()
	answer(message("C", "We have resumed the same shared conversation. What did the completed background task report? Retrieve the existing task result if needed; do not launch another task."))
	v = e.View()
	if v.Current != before || len(v.Jobs) != 1 || v.Memories[0].State != "forgotten" {
		t.Fatal(v)
	}
	// Idle memory consolidation is advisory. An empty candidates array is valid.
	e.mu.Lock()
	e.cfg.IdleMemoryDelay = time.Millisecond
	e.lastMaintenance = time.Time{}
	e.mu.Unlock()
	e.notify()
	wait("idle memory watermark advanced", func() bool { e.mu.Lock(); defer e.mu.Unlock(); return e.record.MemoryWatermark > 0 })
	confirmationsMu.Lock()
	audit := clone(confirmations)
	confirmationsMu.Unlock()
	starts, resumes := map[string]bool{}, map[string]bool{}
	for _, got := range audit {
		if got["method"] == "thread/start" {
			starts[got["thread"].(string)] = true
		} else {
			resumes[got["thread"].(string)] = true
		}
	}
	kinds := map[string]bool{}
	e.mu.Lock()
	runs := clone(e.record.Runs)
	e.mu.Unlock()
	for _, run := range runs {
		if starts[run.ThreadID] {
			kinds[run.Kind] = true
		}
	}
	for _, kind := range []string{"main", "job", "steward", "memory", "bootstrap"} {
		if !kinds[kind] {
			t.Fatal("missing setting confirmation for", kind, audit)
		}
	}
	if !resumes[before] {
		t.Fatal("missing exact S2 resume confirmation", audit)
	}
	for _, got := range audit {
		if got["model"] != "gpt-5.6-luna" || got["modelProvider"] != "openai" || got["reasoningEffort"] != "low" {
			t.Fatal("settings drifted", got)
		}
	}
	if err := writeAtomic(filepath.Join(root, "settings-confirmations.json"), audit); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(filepath.Join(root, "report.json"), map[string]any{"launchSettingsPinnedAcrossAllThreadsAndRestart": true, "settingsConfirmations": len(audit), "model": v.Model, "provider": v.ModelProvider, "reasoningEffort": v.ReasoningEffort, "firstSegment": first, "currentSegment": v.Current, "workerThread": worker.ThreadID, "configurationAndMCPInherited": true, "mainResponsiveWhileWorkerRan": true, "nativeContextSteward": true, "segmentHandoff": true, "lateWorkerResult": true, "memoryCorrectionAndForget": true, "exactThreadResume": true, "idleMemoryConsolidation": true}); err != nil {
		t.Fatal(err)
	}
}

// Record native responses, not just our requested parameters.
type settingsAuditRuntime struct {
	Runtime
	record func(map[string]any)
}

func (p *settingsAuditRuntime) Call(ctx context.Context, method string, params, out any) error {
	err := p.Runtime.Call(ctx, method, params, out)
	if err == nil && (method == "thread/start" || method == "thread/resume") {
		value := cloneMap(out)
		p.record(map[string]any{"method": method, "thread": value["thread"].(map[string]any)["id"], "model": value["model"], "modelProvider": value["modelProvider"], "reasoningEffort": value["reasoningEffort"]})
	}
	return err
}
