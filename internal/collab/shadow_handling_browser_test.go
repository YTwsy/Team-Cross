package collab

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/shadow"
	"teamcross/internal/spacecontext"
)

// A paused shadow with actual business questions and fixture-owned task records.
// No native model is used; this fixture verifies the local UI and HTTP paths.
func seedHandlingBrowser(t *testing.T, a *App, spaceID, annotationID string) string {
	t.Helper()
	v := consumerRegister(t, a, spaceID)
	root := t.TempDir()
	cfg := shadow.Config{Directory: filepath.Join(root, "record"), ReadSource: func(ctx context.Context, ref spacecontext.SourceRef) (shadow.SourceSnapshot, error) {
		return a.readShadowSource(ctx, v.ID, ref)
	}}
	e, err := shadow.New(cfg, v.ID, spaceID, v.Name, root, root, v.Owner, shadow.ModelSettings{Model: "fixture-model", ModelProvider: "fixture-provider", ReasoningEffort: "low"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.shadows[v.ID] = e
	a.mu.Unlock()
	actionID := uuid.NewString()
	out, err := a.shadowToolCall(context.Background(), v.ID, "reply_to_annotation", map[string]any{"annotationId": annotationID, "requestId": actionID, "text": "请确认本次发布需要兼容的最低客户端版本；确认后我会同步更新说明和验证清单。"})
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		Reply AnnotationReply `json:"reply"`
	}
	b, _ := json.Marshal(out)
	if err = json.Unmarshal(b, &receipt); err != nil || receipt.Reply.ID == "" {
		t.Fatal(out, err)
	}
	var historical shadow.TaskQuestion
	if os.Getenv("TEAMCROSS_QUESTIONS_BROWSER") == "1" {
		oldAction := uuid.NewString()
		text := "之前的文档更新是否完成？"
		out, err := a.shadowToolCall(context.Background(), v.ID, "reply_to_annotation", map[string]any{"annotationId": annotationID, "requestId": oldAction, "text": text})
		if err != nil {
			t.Fatal(err)
		}
		var saved struct {
			Reply AnnotationReply `json:"reply"`
		}
		raw, _ := json.Marshal(out)
		if err = json.Unmarshal(raw, &saved); err != nil || saved.Reply.ID == "" {
			t.Fatal(out, err)
		}
		historical = shadow.TaskQuestion{ID: "browser-closed-question", ActionID: oldAction, Text: text, Delivery: "saved", Source: spacecontext.SourceRef{SpaceID: spaceID, AnnotationID: annotationID, ReplyID: saved.Reply.ID}, CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now().Add(-time.Hour)}
	}
	store, err := a.memberAttention(spaceID, v.Owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.pumpShadowAttention(context.Background(), spaceID, store); err != nil {
		t.Fatal(err)
	}
	var origin spacecontext.Signal
	for _, event := range store.Snapshot().Events {
		if event.Signal.Kind == "reply" && event.Signal.Source.AnnotationID == annotationID && (origin.ID == "" || event.Signal.Seq < origin.Seq) {
			origin = event.Signal
		}
	}
	e.Close()
	data, err := os.ReadFile(filepath.Join(cfg.Directory, "shadow.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record shadow.Record
	if err = json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	ref := spacecontext.SourceRef{SpaceID: spaceID, AnnotationID: annotationID, ReplyID: receipt.Reply.ID}
	q := shadow.TaskQuestion{ID: "browser-question", ActionID: actionID, Source: ref, Text: receipt.Reply.Text, Delivery: "saved", Recipients: []string{"fixture-colleague"}, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if os.Getenv("TEAMCROSS_QUESTIONS_BROWSER") == "1" {
		q.Condition = "明确最低客户端版本及对应验证范围"
		q.RespondsTo = []shadow.QuestionRelation{{Source: spacecontext.SourceRef{SpaceID: spaceID, AnnotationID: annotationID}, Question: "发布前请核对错误处理与断线恢复。"}, {Source: origin.Source, Question: "已补齐断线恢复测试，可以一起复核最后的验证记录。"}}
		q.RelationsRecorded = true
	}
	record.Questions[q.ID] = q
	if historical.ID != "" {
		record.Questions[historical.ID] = historical
		record.Jobs["browser-closed-task"] = shadow.Job{ID: "browser-closed-task", Title: "先前的文档更新", Kind: "coordination", State: "completed", Version: 1, Revision: 1, Dependencies: []shadow.TaskDependency{{ID: "old-answer", Kind: "discussion", QuestionID: historical.ID, Source: historical.Source, State: "satisfied", Condition: "确认文档更新已完成"}}, UpdatedAt: time.Now().Add(-time.Hour)}
	}
	for i, title := range []string{"更新发布兼容性说明", "补齐最低版本验证清单"} {
		id := uuid.NewString()
		record.Jobs[id] = shadow.Job{ID: id, Title: title, Version: 1, Revision: 1, State: "waiting", Kind: "coordination", Trigger: &shadow.TaskTrigger{Kind: "activity", EventIDs: []string{origin.ID}, Sources: []spacecontext.SourceRef{origin.Source}}, Dependencies: []shadow.TaskDependency{{ID: "version", Kind: "discussion", QuestionID: q.ID, Source: ref, State: "waiting", Condition: []string{"最低版本确认后更新说明", "最低版本确认后确定验证范围"}[i]}}, UpdatedAt: time.Now()}
		if os.Getenv("TEAMCROSS_QUESTIONS_BROWSER") == "1" {
			job := record.Jobs[id]
			job.Checkpoint = &shadow.TaskCheckpoint{Summary: "已取得部分兼容信息", NextStep: []string{"更新兼容说明并附上准确版本依据", "核对最低版本，再补齐断线恢复验证"}[i]}
			if i == 0 {
				job.Dependencies[0].State = "satisfied"
				job.State = "queued"
			} else {
				job.Dependencies[0].Signals = []shadow.DependencySignal{{ID: "fixture-partial-answer", Source: origin.Source, State: "answered", Preview: "收到部分兼容信息，仍需核对验证范围"}}
			}
			record.Jobs[id] = job
		}
		if i == 0 {
			record.Actions[actionID] = shadow.CollaborationAction{ID: actionID, Tool: "reply_to_annotation", JobID: id, JobVersion: 1, QuestionID: q.ID, State: "saved", Result: b, ResponseSources: []spacecontext.SourceRef{ref}, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		}
	}
	if err = writeJSONFile(filepath.Join(cfg.Directory, "shadow.json"), record); err != nil {
		t.Fatal(err)
	}
	e, err = shadow.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	a.mu.Lock()
	a.shadows[v.ID] = e
	a.mu.Unlock()
	snapshot := e.View()
	if _, err = a.shadowCall(context.Background(), spaceID, "sync", shadowInput{ID: v.ID, Snapshot: &snapshot}); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("TEAMCROSS_STATUS_BROWSER") == "1" {
		second := workbenchDecode[shadow.View](shadowOp(t, a, spaceID, "register", shadowInput{ID: uuid.NewString(), Name: "资料分身"}))
		root2 := t.TempDir()
		other, err := shadow.New(shadow.Config{Directory: filepath.Join(root2, "record")}, second.ID, spaceID, second.Name, root2, root2, second.Owner, shadow.ModelSettings{Model: "fixture-model", ModelProvider: "fixture-provider", ReasoningEffort: "low"})
		if err != nil {
			t.Fatal(err)
		}
		if err = other.SetEnabled(false); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(other.Close)
		a.mu.Lock()
		a.shadows[second.ID] = other
		a.mu.Unlock()
		snapshot = other.View()
		if _, err = a.shadowCall(context.Background(), spaceID, "sync", shadowInput{ID: second.ID, Snapshot: &snapshot}); err != nil {
			t.Fatal(err)
		}
	}
	return v.ID
}
