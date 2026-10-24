package collab

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"teamcross/internal/shadow"
)

func TestShadowResultExactSourcesAcrossAuthenticatedCores(t *testing.T) {
	_, host, member, joined, _ := consumerFixture(t)
	member.shadowCancel()
	member.shadowWG.Wait()
	view := consumerRegister(t, member, joined.ID)
	now := time.Now()
	message := shadow.Message{ID: "reply-fixture", Actor: shadow.Actor{ID: view.ID, Name: view.Name}, Role: "assistant", Text: "The original answer.", State: "completed", CreatedAt: now}
	result := shadow.TaskResult{ID: "result-fixture-v1", TaskID: "task-fixture", TaskVersion: 1, Title: "Compatibility", SourceID: "original-request", State: "completed", Outcome: "completed", Text: "Version one evidence.", CompletionCriteria: []string{"Check v1"}, CompletionChecks: []string{"v1 checked"}, CreatedAt: now}
	view.Messages, view.TaskResults = []shadow.Message{message}, []shadow.TaskResult{result}
	view.Revision++
	shadowOp(t, member, joined.ID, "sync", shadowInput{ID: view.ID, Snapshot: &view})
	ref := SpaceSourceRef{SpaceID: joined.ID, ShadowID: view.ID, ShadowResultID: result.ID}
	read, err := member.readAttentionSource(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	var source ShadowSourceResult
	if err := json.Unmarshal(read.Data, &source); err != nil || source.Result == nil || source.Result.Text != result.Text || source.Source.SpaceID != joined.ID || read.Latest {
		t.Fatal("exact result source or local alias lost", string(read.Data), err)
	}
	// The current job and newer result may change; the cited result stays fixed.
	second := result
	second.ID, second.TaskVersion, second.Text = "result-fixture-v2", 2, "Version two evidence."
	view.TaskResults = append(view.TaskResults, second)
	view.Jobs = []shadow.Job{{ID: result.TaskID, Version: 2, State: "completed", Result: second.Text}}
	view.Revision++
	shadowOp(t, member, joined.ID, "sync", shadowInput{ID: view.ID, Snapshot: &view})
	out, err := member.invokeWorkbenchTool(context.Background(), "read_space_shadow", map[string]any{"spaceId": joined.ID, "shadowId": view.ID, "resultId": result.ID}, agentCaller{}, "")
	if err != nil || workbenchDecode[ShadowSourceResult](out).Result.Text != result.Text {
		t.Fatal("model tool substituted current result", out, err)
	}
	directory := workbenchDecode[shadowSpaceView](shadowOp(t, member, joined.ID, "view", shadowInput{ID: view.ID}))
	if directory.Shadows[0].TaskResultCount != 2 || directory.Shadows[0].TaskResults[0].Text != "" {
		t.Fatal("ordinary directory eagerly included full result bodies")
	}
	ref.ShadowResultID, ref.ShadowMessageID = "", message.ID
	read, err = member.readAttentionSource(context.Background(), ref)
	if err != nil || !strings.Contains(string(read.Data), message.Text) {
		t.Fatal("exact message source unavailable", err, string(read.Data))
	}
	for _, invalid := range []SpaceSourceRef{
		{SpaceID: joined.ID, ShadowMessageID: message.ID},
		{SpaceID: joined.ID, ShadowID: view.ID, ShadowMessageID: message.ID, ShadowResultID: result.ID},
		{SpaceID: joined.ID, ShadowID: view.ID, ShadowResultID: result.ID, RequestID: "another"},
		{SpaceID: joined.ID, ShadowID: view.ID, ShadowResultID: result.ID, BriefRevision: 1},
	} {
		if _, err := member.readAttentionSource(context.Background(), invalid); err == nil {
			t.Fatal("ambiguous source accepted", invalid)
		}
	}
	// Reject an owner's attempt to overwrite a stable source, preserving disk.
	changed := libraryClone(view)
	changed.Revision++
	changed.TaskResults[0].Text = "Rewritten historical result"
	if _, err := member.shadowCall(context.Background(), joined.ID, "sync", shadowInput{ID: view.ID, Snapshot: &changed}); err == nil {
		t.Fatal("historical result overwritten")
	}
	changed = libraryClone(view)
	changed.Revision++
	changed.Messages[0].Text = "Rewritten message"
	if _, err := member.shadowCall(context.Background(), joined.ID, "sync", shadowInput{ID: view.ID, Snapshot: &changed}); err == nil {
		t.Fatal("historical message overwritten")
	}
	var disk Record
	if err := readJSON(filepath.Join(host.runtimeDirectory(), "collaboration.json"), &disk); err != nil {
		t.Fatal(err)
	}
	if disk.Shadows[view.ID].View.TaskResults[0].Text != result.Text {
		t.Fatal("failed synchronization changed disk")
	}
	// A retention gap is explicit. Never substitute v2 for the requested v1.
	view.TaskResults = []shadow.TaskResult{second}
	view.Revision++
	shadowOp(t, member, joined.ID, "sync", shadowInput{ID: view.ID, Snapshot: &view})
	missing := workbenchDecode[ShadowSourceResult](shadowOp(t, member, joined.ID, "source", shadowInput{ID: view.ID, ResultID: result.ID}))
	if missing.Unavailable != "shadow_result_unavailable" || missing.Result != nil {
		t.Fatal("expired result fell back to latest", missing)
	}
	host.mu.Lock()
	host.record.State = "ended"
	host.mu.Unlock()
	if _, err := member.readAttentionSource(context.Background(), ref); err == nil {
		t.Fatal("closed space still returned source body")
	}
}
