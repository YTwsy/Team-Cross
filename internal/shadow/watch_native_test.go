package shadow

import (
	"context"
	"encoding/json"
	"fmt"
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

// The native model uses the real watch tool and persisted attention lifecycle.
// Business sources are controlled fixtures; host/TLS behavior is tested in collab.
func TestNativeShadowWatch(t *testing.T) {
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
	var follows, reads atomic.Int32
	subject := spacecontext.Subject{Kind: "discussion", ID: "watch-discussion"}
	ref := spacecontext.SourceRef{SpaceID: "watch-native-fixture", AnnotationID: subject.ID}
	cfg := Config{Directory: filepath.Join(root, "engine"), IdleMemoryDelay: time.Hour,
		Start: func(ctx context.Context, h, c, log string) (Runtime, error) {
			p, err := nativecodex.StartWithMode(ctx, binary, h, c, log, runtimeconfig.Trusted)
			if err != nil {
				return nil, err
			}
			return &toolsetAuditRuntime{Runtime: p, audit: audit}, nil
		},
		EnsureFollow: func(_ context.Context, got spacecontext.Subject, id string) (spacecontext.FollowSetting, error) {
			if got != subject || id == "" {
				return spacecontext.FollowSetting{}, fmt.Errorf("fixture subject or stable identity lost")
			}
			follows.Add(1)
			return spacecontext.FollowSetting{Subject: subject, Mode: "following", Revision: 1}, nil
		},
		ReadSource: func(_ context.Context, source spacecontext.SourceRef) (SourceSnapshot, error) {
			reads.Add(1)
			return SourceSnapshot{Source: source, ObservedAt: time.Now(), Data: json.RawMessage(`{"text":"离线升级恢复验证已经通过，断线后的检查点能够恢复。这是事实更新，暂无未决问题，也没有待执行工作。"}`)}, nil
		},
		ReadFollowSource: func(_ context.Context, signal spacecontext.Signal) (SourceSnapshot, error) {
			reads.Add(1)
			return SourceSnapshot{Source: signal.Source, ObservedAt: time.Now(), Data: json.RawMessage(`{"text":"当前已确认旧客户端 v3 兼容；离线升级恢复尚无新进展。这是跟进事实，暂无问题或行动。"}`), References: []spacecontext.SourceRef{signal.Source}}, nil
		},
	}
	e, err := New(cfg, uuid.NewString(), ref.SpaceID, "Watch fixture", home, cwd, Actor{ID: "owner", Name: "Owner"}, ModelSettings{Model: "gpt-5.6-luna", ModelProvider: "openai", ReasoningEffort: "low"})
	if err != nil {
		t.Fatal(err)
	}
	attentionDir := filepath.Join(root, "attention")
	store, err := OpenAttention(attentionDir, ref.SpaceID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetRoute(AttentionRoute{ShadowID: e.record.ID, Revision: 1, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := e.AttachAttention(store); err != nil {
		t.Fatal(err)
	}
	engines := []*Engine{e}
	defer func() {
		for _, engine := range engines {
			engine.Close()
		}
		audit.mu.Lock()
		defer audit.mu.Unlock()
		_ = writeAtomic(filepath.Join(root, "report.json"), map[string]any{"passed": !t.Failed(), "audit": audit, "finalView": e.View(), "attention": store.Snapshot(), "followCalls": follows.Load(), "sourceReads": reads.Load()})
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
	message := func(text, marker string) {
		t.Helper()
		m := send(t, e, "owner", text+" 使用 shadow_watch 实际保存后，简短回答 "+marker+"。这是独立测试，不调用 shell、网络或其他 MCP，不创建任务，不联系其他人。")
		wait(marker, func() bool {
			for _, reply := range e.View().Messages {
				if reply.ReplyTo == m.ID && reply.Role == "assistant" && reply.State == "completed" && strings.Contains(reply.Text, marker) {
					return true
				}
			}
			return false
		})
	}
	focus := func() Focus { return store.Snapshot().Focuses[attentionHash(subject)] }
	deliver := func(seq uint64, follow bool) {
		t.Helper()
		signal := spacecontext.Signal{ID: fmt.Sprint("native-follow-event-", seq), Seq: seq, Kind: "reply", Subject: subject, Source: ref, Reasons: []string{"follow_discussion"}, CreatedAt: time.Now()}
		if follow {
			signal.Kind = "follow_changed"
			signal.Follow = &spacecontext.FollowChange{Mode: "following", Revision: 1, Baseline: []spacecontext.SourceRef{ref}}
		} else {
			signal.Source.ReplyID = "offline-recovery-result"
		}
		r := AttentionReceipt{Token: fmt.Sprint("native-receipt-", seq), Route: store.Snapshot().Route, Through: seq}
		if err := store.Accept(r, []spacecontext.Signal{signal}); err != nil {
			t.Fatal(err)
		}
		if err := store.ConfirmReceipt(r); err != nil {
			t.Fatal(err)
		}
		e.notify()
	}
	e.Start()
	message("请关注准确对象 {kind:discussion,id:watch-discussion}，目标是持续了解旧客户端 v3 兼容性，只了解事实，暂无行动。", "WATCH_STARTED")
	if focus().State != "active" || focus().GoalVersion != 1 || follows.Load() != 1 {
		t.Fatal("native watch start did not persist one real follow", focus())
	}
	deliver(1, true)
	wait("fixed follow baseline understood privately", func() bool {
		return focus().ProcessedSeq == 1 && len(focus().Evidence) > 0 && strings.Contains(focus().Summary, "v3")
	})
	message("对同一对象使用 shadow_watch update，把关注目标改为持续了解离线升级恢复，只了解事实。先 read 取得当前目标版本。", "WATCH_UPDATED")
	message("对同一对象使用 shadow_watch pause，暂时暂停分析并保留排队动态。先 read 取得当前目标版本。", "WATCH_PAUSED")
	if focus().State != "paused" || focus().GoalVersion != 3 {
		t.Fatal("native goal update or pause missing", focus())
	}
	deliver(2, false)
	beforeReads := reads.Load()
	time.Sleep(3 * time.Second)
	if reads.Load() != beforeReads || focus().ProcessedSeq != 1 {
		t.Fatal("paused watch consumed queued work")
	}
	main := e.View().Current
	e.Close()
	store, err = OpenAttention(attentionDir, ref.SpaceID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	e, err = Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	engines = append(engines, e)
	if err := e.AttachAttention(store); err != nil {
		t.Fatal(err)
	}
	e.Start()
	message("对同一对象使用 shadow_watch resume，按原有离线升级恢复目标继续了解排队动态。先 read 取得当前目标版本。", "WATCH_RESUMED")
	wait("queued update understood after native restart", func() bool { return focus().ProcessedSeq == 2 && strings.Contains(focus().Summary, "离线") })
	if e.View().Current != main || focus().GoalVersion != 4 {
		t.Fatal("watch recovery changed native identity or lost goal version")
	}
	message("对同一对象使用 shadow_watch complete，结束当前关注目标并保留已有认识。先 read 取得当前目标版本。", "WATCH_COMPLETED")
	if focus().State != "completed" || focus().GoalVersion != 5 || focus().Summary == "" || follows.Load() != 1 || len(e.View().Jobs) != 0 || len(e.View().Messages) != 10 {
		t.Fatal("watch completion lost understanding, repeated follow or leaked observation", focus(), e.View())
	}
}
