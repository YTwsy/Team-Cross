package collab

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"teamcross/internal/shadow"
	"teamcross/internal/spacecontext"
)

func TestShadowQuestionsLocalUIUsesOwnedEngineAndDoesNotStartModel(t *testing.T) {
	_, _, b, joined, _ := consumerFixture(t)
	v := consumerRegister(t, b, joined.ID)
	root := t.TempDir()
	cfg := shadow.Config{Directory: filepath.Join(root, "record")}
	e, err := shadow.New(cfg, v.ID, joined.ID, v.Name, root, root, v.Owner, shadow.ModelSettings{Model: "fixture-model", ModelProvider: "fixture-provider", ReasoningEffort: "low"})
	if err != nil {
		t.Fatal(err)
	}
	e.Close()
	path := filepath.Join(cfg.Directory, "shadow.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record shadow.Record
	if err = json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	ref := spacecontext.SourceRef{SpaceID: joined.ID, AnnotationID: "question-discussion", ReplyID: "actual-question"}
	record.Questions = map[string]shadow.TaskQuestion{"question": {ID: "question", Text: "Which version remains supported?", Delivery: "saved", Source: ref}}
	record.Jobs = map[string]shadow.Job{"task": {ID: "task", State: "waiting", Prompt: "PRIVATE PROMPT", ThreadID: "PRIVATE SESSION", Dependencies: []shadow.TaskDependency{{ID: "version", QuestionID: "question", Condition: "Need a version", State: "waiting"}}}}
	data, _ = json.Marshal(record)
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	e, err = shadow.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	b.mu.Lock()
	b.shadows[v.ID] = e
	b.mu.Unlock()
	base := "collaborations/" + joined.ID + "/attention/"
	for _, route := range []string{"questions?shadowId=" + v.ID, "question?shadowId=" + v.ID + "&id=question"} {
		w := handlingHTTP(t, b, base+route, nil)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "Which version remains supported?") || strings.Contains(w.Body.String(), "PRIVATE") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, route := range []string{"questions?shadowId=another-members-shadow", "question?shadowId=" + v.ID + "&id=missing", "question?shadowId=" + v.ID} {
		if w := handlingHTTP(t, b, base+route, nil); w.Code == 200 {
			t.Fatal("invalid question route accepted", route)
		}
	}
	if w := handlingHTTP(t, b, base+"questions?shadowId="+v.ID, map[string]any{}); w.Code == 200 {
		t.Fatal("index accepted a write")
	}
	if e.View().Current != "" {
		t.Fatal("reading questions started a native conversation")
	}
}
