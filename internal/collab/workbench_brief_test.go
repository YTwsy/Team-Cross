package collab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func briefPatch(t *testing.T, a *App, space string, in workbenchInput) BriefResult {
	t.Helper()
	if in.RequestID == "" {
		in.RequestID = uuid.NewString()
	}
	out, err := a.workbenchCall(context.Background(), space, "brief-update", in)
	if err != nil {
		t.Fatal(err)
	}
	return workbenchDecode[BriefResult](out)
}

func briefPut(section, id, text, status string, sources ...SpaceSourceRef) BriefChange {
	return BriefChange{Operation: "upsert", Section: section, Item: &BriefItem{ID: id, Text: text, Status: status, Sources: sources}}
}

func TestBriefRemoteShadowPatchKeepsOtherItemsAndExactRevisions(t *testing.T) {
	a, s, b, joined, note := consumerFixture(t)
	v := registeredToolShadow(t, b, joined.ID)
	first := briefPatch(t, a, s.record.ID, workbenchInput{Changes: []BriefChange{
		briefPut("decisions", "protocol", "Original decision", "confirmed"),
		briefPut("questions", "owner-question", "Keep the owner's question", "open"),
	}})
	if first.Revision != 1 || first.State != "saved" {
		t.Fatal(first)
	}
	requestID := uuid.NewString()
	args := map[string]any{"requestId": requestID, "baseRevision": 1, "changes": []BriefChange{briefPut("decisions", "protocol", "A proposal with evidence", "proposed", SpaceSourceRef{SpaceID: joined.ID, AnnotationID: note.ID})}}
	out, err := b.shadowToolCall(context.Background(), v.ID, "update_space_brief", args)
	if err != nil {
		t.Fatal(err)
	}
	second := workbenchDecode[BriefResult](out)
	if second.Revision != 2 || second.Source.SpaceID != joined.ID || second.Source.BriefRevision != 2 || second.Brief.UpdatedBy.Kind != "shadow" || second.Brief.UpdatedBy.ShadowID != v.ID || second.Brief.UpdatedBy.MemberID != v.Owner.ID {
		t.Fatal("lost actual writer or source", second)
	}
	if second.ReadBack == nil || second.ReadBack.Tool != "read_space_brief" || second.ReadBack.RequestID != requestID {
		t.Fatal("saved update lacks an unambiguous receipt lookup", second)
	}
	if !reflect.DeepEqual(second.Brief.Questions, first.Brief.Questions) || second.Brief.Decisions[0].ID != "protocol" || second.Brief.Decisions[0].Status != "proposed" || second.Brief.Decisions[0].Sources[0].SpaceID != joined.ID {
		t.Fatal("patch changed unrelated content or confirmation", second.Brief)
	}
	if first.Brief.Questions[0].UpdatedBy.Kind != "human" || first.Brief.Questions[0].UpdatedAt != second.Brief.Questions[0].UpdatedAt {
		t.Fatal("unmodified item lost attribution")
	}
	read, err := b.shadowToolCall(context.Background(), v.ID, "read_space_brief", map[string]any{"revision": 1, "itemId": "protocol"})
	old := workbenchDecode[BriefResult](read)
	if err != nil || old.Item == nil || old.Item.Text != "Original decision" || old.Brief != nil || old.Source.BriefItemID != "protocol" || old.Source.BriefRevision != 1 {
		t.Fatal("historical read returned current content", read, err)
	}
	current := briefPatch(t, a, s.record.ID, workbenchInput{BaseRevision: 2, Changes: []BriefChange{briefPut("progress", "delivery", "Tests are in progress", "in_progress")}})
	if current.Revision != 3 {
		t.Fatal(current)
	}
	// Reload the actual saved workbench, including command receipts and history.
	var disk Record
	if err := readJSON(filepath.Join(s.runtimeDirectory(), "collaboration.json"), &disk); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.record.Workbench = libraryClone(disk.Workbench)
	s.mu.Unlock()
	out, err = b.shadowToolCall(context.Background(), v.ID, "update_space_brief", args)
	if err != nil || workbenchDecode[BriefResult](out).Revision != 2 {
		t.Fatal("retry created a new revision", out, err)
	}
	receipt, err := b.shadowToolCall(context.Background(), v.ID, "read_space_brief", map[string]any{"requestId": requestID})
	saved := workbenchDecode[BriefResult](receipt)
	if err != nil || saved.Revision != 2 || saved.State != "saved" || saved.ReadBack == nil || saved.ReadBack.RequestID != requestID {
		t.Fatal("lost receipt after reload", receipt, err)
	}
	args["baseRevision"] = 3
	if _, err = b.shadowToolCall(context.Background(), v.ID, "update_space_brief", args); err == nil {
		t.Fatal("accepted changed command under old ID")
	}
	if _, err = a.workbenchCall(context.Background(), s.record.ID, "brief-update", workbenchInput{RequestID: requestID, BaseRevision: 1, Changes: []BriefChange{briefPut("decisions", "protocol", "A proposal with evidence", "proposed", SpaceSourceRef{SpaceID: s.record.ID, AnnotationID: note.ID})}}); err == nil {
		t.Fatal("another writer reused a shadow command")
	}
	snapshot, err := b.readAttentionSource(context.Background(), SpaceSourceRef{SpaceID: joined.ID, BriefRevision: 1, BriefItemID: "protocol"})
	if err != nil || snapshot.Latest || !strings.Contains(string(snapshot.Data), "Original decision") || strings.Contains(string(snapshot.Data), "A proposal") {
		t.Fatal("source reader lost exact revision", snapshot, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.Workbench.Brief.Revision != 3 {
		t.Fatal("retry altered current revision")
	}
	for _, event := range s.activityLog.Events {
		if event.Kind == "brief_updated" && event.Source.BriefRevision <= 0 {
			t.Fatal("brief event lacks fixed revision", event)
		}
	}
}

func TestBriefHTTPDoesNotSubstituteLatestForInvalidRevision(t *testing.T) {
	a, _, s := spaceFixture(t)
	briefPatch(t, a, s.record.ID, workbenchInput{Changes: []BriefChange{briefPut("decisions", "current", "CURRENT_BRIEF_BODY", "confirmed")}})
	path := "http://127.0.0.1/api/collaborations/" + s.record.ID + "/workbench/brief-read?revision="
	valid := httptest.NewRecorder()
	a.Handler(http.NotFoundHandler()).ServeHTTP(valid, httptest.NewRequest(http.MethodGet, path+"1", nil))
	if valid.Code != http.StatusOK || !strings.Contains(valid.Body.String(), "CURRENT_BRIEF_BODY") {
		t.Fatal("valid historical source was not readable", valid.Code, valid.Body.String())
	}
	for _, revision := range []string{"invalid", "0", "-1", "1.5", "999999999999999999999999"} {
		t.Run(revision, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path+revision, nil)
			res := httptest.NewRecorder()
			a.Handler(http.NotFoundHandler()).ServeHTTP(res, req)
			if res.Code != http.StatusBadRequest || strings.Contains(res.Body.String(), "CURRENT_BRIEF_BODY") {
				t.Fatal("invalid historical reference returned current content", res.Code, res.Body.String())
			}
		})
	}
}

func TestBriefConcurrentPatchesReturnConflictsAndMergeWithoutLostUpdates(t *testing.T) {
	a, _, s := spaceFixture(t)
	briefPatch(t, a, s.record.ID, workbenchInput{Changes: []BriefChange{briefPut("decisions", "choice", "Initial", "proposed")}})
	inputs := []workbenchInput{
		{RequestID: uuid.NewString(), BaseRevision: 1, Changes: []BriefChange{briefPut("decisions", "choice", "Writer A", "confirmed")}},
		{RequestID: uuid.NewString(), BaseRevision: 1, Changes: []BriefChange{briefPut("decisions", "choice", "Writer B", "disputed")}},
	}
	results := make([]BriefResult, 2)
	errors := make([]error, 2)
	var wg sync.WaitGroup
	for i := range inputs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out, err := a.workbenchCall(context.Background(), s.record.ID, "brief-update", inputs[i])
			results[i], errors[i] = workbenchDecode[BriefResult](out), err
		}(i)
	}
	wg.Wait()
	saved, conflicted := -1, -1
	for i, result := range results {
		if errors[i] != nil {
			t.Fatal(errors[i])
		}
		if result.State == "saved" {
			saved = i
		}
		if result.State == "conflict" {
			conflicted = i
		}
	}
	if saved < 0 || conflicted < 0 || !reflect.DeepEqual(results[conflicted].Conflicts, []string{"choice"}) || results[conflicted].Brief.Decisions[0].Text != inputs[saved].Changes[0].Item.Text {
		t.Fatal("conflict omitted latest content or affected item", results)
	}
	// The losing writer rereads and preserves the winning decision, adding a question.
	merged := briefPatch(t, a, s.record.ID, workbenchInput{BaseRevision: results[conflicted].Revision, Changes: []BriefChange{briefPut("questions", "clarify", "Clarify the remaining disagreement", "open")}})
	if merged.Revision != 3 || merged.Brief.Decisions[0].Text != inputs[saved].Changes[0].Item.Text || len(merged.Brief.Questions) != 1 {
		t.Fatal("merge lost the concurrent update", merged)
	}
	// A new command with identical content has a receipt but creates no new revision.
	noop := briefPatch(t, a, s.record.ID, workbenchInput{BaseRevision: 3, Changes: []BriefChange{briefPut("questions", "clarify", "Clarify the remaining disagreement", "open")}})
	if noop.Revision != 3 {
		t.Fatal("unchanged brief manufactured a revision", noop)
	}
}

func TestBriefAtomicFailureAndSourceValidation(t *testing.T) {
	a, f, s := spaceFixture(t)
	draft := materialDraft(t, a, f)
	published, err := a.Publish(context.Background(), s.record.ID, PublishInput{PreviewID: draft.ID, PreviewHash: draft.Hash, RequestID: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	materialID := published.(map[string]any)["materialId"].(string)
	ref := SpaceSourceRef{SpaceID: s.record.ID, MaterialID: materialID, Version: 1}
	first := briefPatch(t, a, s.record.ID, workbenchInput{Changes: []BriefChange{briefPut("decisions", "kept", "Original public evidence", "confirmed", ref)}})
	s.mu.Lock()
	before := contentHash(s.record.Workbench)
	s.mu.Unlock()
	file := filepath.Join(s.runtimeDirectory(), "collaboration.json")
	if err = os.Rename(file, file+".saved"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(file, 0700); err != nil {
		t.Fatal(err)
	}
	restore := func() { _ = os.Remove(file); _ = os.Rename(file+".saved", file) }
	t.Cleanup(func() {
		if _, err := os.Stat(file + ".saved"); err == nil {
			restore()
		}
	})
	in := workbenchInput{RequestID: uuid.NewString(), BaseRevision: first.Revision, Changes: []BriefChange{briefPut("questions", "new", "A new question", "open")}}
	if _, err = a.workbenchCall(context.Background(), s.record.ID, "brief-update", in); err == nil {
		t.Fatal("obstructed save succeeded")
	}
	s.mu.Lock()
	after := contentHash(s.record.Workbench)
	s.mu.Unlock()
	if after != before {
		t.Fatal("failed save advanced brief, history or receipt")
	}
	restore()
	if got := briefPatch(t, a, s.record.ID, in); got.Revision != 2 {
		t.Fatal(got)
	}
	if _, err = s.withdrawMaterial(context.Background(), materialID); err != nil {
		t.Fatal(err)
	}
	// A change elsewhere retains the existing historical citation; a new citation
	// to a withdrawn source must not be accepted as newly checked evidence.
	kept := briefPatch(t, a, s.record.ID, workbenchInput{BaseRevision: 2, Changes: []BriefChange{briefPut("questions", "new", "Question answered", "resolved")}})
	if !reflect.DeepEqual(kept.Brief.Decisions, first.Brief.Decisions) {
		t.Fatal("unrelated edit rewrote old evidence")
	}
	cases := map[string][]BriefChange{
		"withdrawn":      {briefPut("decisions", "invalid", "Unavailable citation", "confirmed", ref)},
		"foreign":        {briefPut("decisions", "invalid", "Foreign", "proposed", SpaceSourceRef{SpaceID: "other", BriefRevision: 1})},
		"missing-reply":  {briefPut("questions", "invalid", "Missing reply", "open", SpaceSourceRef{SpaceID: s.record.ID, ReplyID: "none"})},
		"mixed-source":   {briefPut("decisions", "invalid", "Mixed", "proposed", SpaceSourceRef{SpaceID: s.record.ID, BriefRevision: 1, MaterialID: materialID, Version: 1})},
		"status":         {briefPut("questions", "invalid", "Wrong status", "confirmed")},
		"duplicate-id":   {briefPut("questions", "new", "First", "open"), briefPut("questions", "new", "Second", "open")},
		"remove-missing": {{Operation: "remove", Section: "questions", ItemID: "absent"}},
		"oversized":      {briefPut("progress", "invalid", strings.Repeat("x", 2001), "in_progress")},
	}
	for name, changes := range cases {
		t.Run(name, func(t *testing.T) {
			s.mu.Lock()
			before := contentHash(s.record.Workbench)
			s.mu.Unlock()
			_, err := a.workbenchCall(context.Background(), s.record.ID, "brief-update", workbenchInput{RequestID: uuid.NewString(), BaseRevision: 3, Changes: changes})
			s.mu.Lock()
			after := contentHash(s.record.Workbench)
			s.mu.Unlock()
			if err == nil || before != after {
				t.Fatal("invalid batch partially applied", err)
			}
		})
	}
}

func TestBriefHistoryRetentionKeepsReceiptsWithoutInventingOldBody(t *testing.T) {
	a, _, s := spaceFixture(t)
	oldText := "ORIGINAL_ONLY"
	firstInput := workbenchInput{RequestID: uuid.NewString(), Topic: &oldText}
	first := briefPatch(t, a, s.record.ID, firstInput)
	for i := 1; i <= briefHistoryLimit+1; i++ {
		topic := strings.Repeat("x", 1+i%20) + uuid.NewString()
		briefPatch(t, a, s.record.ID, workbenchInput{BaseRevision: i, Topic: &topic})
	}
	old, err := a.workbenchCall(context.Background(), s.record.ID, "brief-read", workbenchInput{Revision: first.Revision})
	read := workbenchDecode[BriefResult](old)
	if err != nil || read.Unavailable != "brief_revision_unavailable" || read.Brief != nil || read.Revision != 1 {
		t.Fatal("expired version returned current content", read, err)
	}
	replay := briefPatch(t, a, s.record.ID, firstInput)
	if replay.State != "saved" || replay.Revision != 1 || replay.Unavailable != "brief_revision_unavailable" {
		t.Fatal("old command was reapplied or lost", replay)
	}
	snapshot, err := a.readAttentionSource(context.Background(), first.Source)
	if err != nil || snapshot.Latest || snapshot.Unavailable != "brief_revision_unavailable" || strings.Contains(string(snapshot.Data), "ORIGINAL_ONLY") {
		t.Fatal("missing historical source is not explicit", snapshot, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.record.Workbench.BriefHistory) != briefHistoryLimit || s.record.Workbench.Brief.Revision != briefHistoryLimit+2 {
		t.Fatal("unbounded history or replay changed state")
	}
}
