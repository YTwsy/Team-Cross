package shadow

import (
	"context"
	"encoding/json"
	"fmt"
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

// Real structured output must select Core-issued references even when the
// source body contains tempting business request/native turn identifiers.
func TestNativeShadowObservationEvidence(t *testing.T) {
	root, binary := os.Getenv("TEAMCROSS_SHADOW_NATIVE_DIR"), os.Getenv("TEAMCROSS_SHADOW_CODEX_BIN")
	if root == "" || binary == "" {
		t.Skip("requires a fresh dedicated native fixture")
	}
	home, cwd := filepath.Join(root, "codex-home"), filepath.Join(root, "repo")
	config, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil || !strings.Contains(string(config), `model = "gpt-5.6-luna"`) {
		t.Fatal("fixture must explicitly select gpt-5.6-luna")
	}
	cfg := Config{Directory: filepath.Join(root, "engine"), IdleMemoryDelay: time.Hour,
		Start: func(ctx context.Context, h, c, log string) (Runtime, error) {
			return nativecodex.StartWithMode(ctx, binary, h, c, log, runtimeconfig.Trusted)
		},
		ReadSource: func(_ context.Context, ref spacecontext.SourceRef) (SourceSnapshot, error) {
			source := SourceSnapshot{Source: ref, ObservedAt: time.Now()}
			switch {
			case ref.AnnotationID != "":
				source.Data = json.RawMessage(`{"author":"成员乙","text":"发布状态已经核实为已批准，当前没有未决问题或待办行动。","requestId":"business-command-not-a-source","target":{"turnId":"body-turn","itemIds":["body-item"]}}`)
			case ref.Version == 1:
				source.Data = json.RawMessage(`{"version":1,"status":"待审核","quota":20,"text":"只需了解事实，当前无问题或行动。","requestId":"publish-command-v1"}`)
			case ref.Version == 2:
				source.Data = json.RawMessage(`{"version":2,"status":"已批准","quota":35,"text":"v2 替换 v1，当前无问题或行动。","requestId":"publish-command-v2","turnId":"source-turn","itemIds":["source-item"]}`)
			default:
				source.Unavailable = "material withdrawn"
				source.Data = json.RawMessage(`{"status":"withdrawn"}`)
			}
			return source, nil
		},
	}
	e, err := New(cfg, uuid.NewString(), "observation-evidence-fixture", "Observation fixture", home, cwd, Actor{ID: "owner", Name: "Owner"}, ModelSettings{Model: "gpt-5.6-luna", ModelProvider: "openai", ReasoningEffort: "low"})
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenAttention(filepath.Join(root, "attention"), e.record.SpaceID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetRoute(AttentionRoute{ShadowID: e.record.ID, Revision: 1, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := e.AttachAttention(store); err != nil {
		t.Fatal(err)
	}
	defer func() {
		e.Close()
		_ = writeAtomic(filepath.Join(root, "report.json"), map[string]any{"passed": !t.Failed(), "finalView": e.View(), "attention": store.Snapshot()})
	}()
	e.Start()
	deliver := func(seq uint64, subject spacecontext.Subject, ref spacecontext.SourceRef) Focus {
		t.Helper()
		signal := spacecontext.Signal{ID: fmt.Sprint("observation-event-", seq), Seq: seq, Kind: "reply", Subject: subject, Source: ref, Reasons: []string{"follow_discussion"}, CreatedAt: time.Now()}
		if subject.Kind == "material" {
			signal.Kind, signal.Reasons = "material_updated", []string{"follow_material"}
		}
		acceptAttentionSignals(t, store, signal)
		e.notify()
		deadline := time.Now().Add(180 * time.Second)
		for time.Now().Before(deadline) {
			snapshot := store.Snapshot()
			focus := snapshot.Focuses[attentionHash(subject)]
			if focus.Error != "" || e.DiagnosticError() != "" {
				t.Fatalf("observation %d failed: %s / %s", seq, focus.Error, e.DiagnosticError())
			}
			if snapshot.Events[signal.ID].State == "processed" {
				t.Logf("observation %d persisted: %s; evidence=%s", seq, focus.Summary, jsonText(focus.Evidence))
				return focus
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("observation %d timed out: %s", seq, jsonText(e.View()))
		return Focus{}
	}
	ref := spacecontext.SourceRef{SpaceID: e.record.SpaceID, ExecutionID: "execution", AnnotationID: "discussion", ReplyID: "reply"}
	discussion := deliver(1, spacecontext.Subject{Kind: "discussion", ID: "discussion", ExecutionID: "execution"}, ref)
	if discussion.Summary == "" || len(discussion.Evidence) != 1 || attentionHash(discussion.Evidence[0]) != attentionHash(ref) {
		t.Fatal("discussion reference gained body fields or lost attribution", discussion)
	}
	material := spacecontext.Subject{Kind: "material", ID: "material"}
	ref = spacecontext.SourceRef{SpaceID: e.record.SpaceID, MaterialID: material.ID, Version: 1}
	first := deliver(2, material, ref)
	if len(first.Evidence) != 1 || attentionHash(first.Evidence[0]) != attentionHash(ref) {
		t.Fatal("initial material source missing", first)
	}
	ref.Version = 2
	second := deliver(3, material, ref)
	if !strings.Contains(second.Summary, "35") {
		t.Fatal("latest material facts not retained", second)
	}
	current := false
	for _, evidence := range second.Evidence {
		current = current || attentionHash(evidence) == attentionHash(ref)
		if evidence.RequestID != "" || evidence.TurnID != "" || len(evidence.ItemIDs) > 0 {
			t.Fatal("material evidence gained body metadata", evidence)
		}
	}
	if !current {
		t.Fatal("current material version not cited", second)
	}
	ref.Version = 3
	withdrawn := deliver(4, material, ref)
	if len(withdrawn.Evidence) != 0 || withdrawn.Summary == "" {
		t.Fatal("withdrawal retained unavailable evidence", withdrawn)
	}
	if len(e.View().Messages) != 0 || len(e.View().Jobs) != 0 || len(store.Snapshot().Continuations) != 0 {
		t.Fatal("ordinary facts created a public reply or duplicate task")
	}
}
