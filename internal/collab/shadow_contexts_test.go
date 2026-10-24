package collab

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"teamcross/internal/shadow"
)

func TestShadowContextUsesIncludesOnlySameMemberAndSpace(t *testing.T) {
	a := &App{shadows: map[string]*shadow.Engine{}}
	makeEngine := func(id, space, member, state string) *shadow.Engine {
		t.Helper()
		root := t.TempDir()
		cfg := shadow.Config{Directory: filepath.Join(root, "record")}
		identity := uuid.NewString()
		e, err := shadow.New(cfg, identity, space, id, root, root, shadow.Actor{ID: member, Name: member}, shadow.ModelSettings{Model: "fixture-model", ModelProvider: "fixture-provider", ReasoningEffort: "low"})
		if err != nil {
			t.Fatal(err)
		}
		e.Close()
		path := filepath.Join(cfg.Directory, "shadow.json")
		var record shadow.Record
		if err := readJSON(path, &record); err != nil {
			t.Fatal(err)
		}
		if state != "" {
			record.Jobs[id+"-job"] = shadow.Job{ID: id + "-job", Title: id + " task", Version: 1, Revision: 1, State: state, Contexts: []shadow.TaskContext{{ID: "same-focus-id", Reason: "Exact material facts"}}}
		}
		if err := writeJSONFile(path, record); err != nil {
			t.Fatal(err)
		}
		e, err = shadow.Open(cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(e.Close)
		a.shadows[identity] = e
		return e
	}
	current := makeEngine("new-default", "space", "member", "")
	old := makeEngine("old-default", "space", "member", "waiting")
	makeEngine("another-member", "space", "other-member", "waiting")
	makeEngine("another-space", "other-space", "member", "waiting")
	makeEngine("completed", "space", "member", "completed")
	currentID, _, _, _ := current.Identity()
	oldID, _, _, _ := old.Identity()
	uses := a.shadowContextUses(currentID, "same-focus-id")
	if uses.Total != 1 || len(uses.Items) != 1 || uses.Items[0].ShadowID != oldID || uses.Items[0].JobID != "old-default-job" {
		t.Fatal("related-task index lost the old shadow or crossed identity", uses)
	}
	if len(old.View().Jobs[0].Contexts) != 0 {
		t.Fatal("private context was exposed in the shared task")
	}
	if uses = a.shadowContextUses("missing", "same-focus-id"); uses.Total != 0 || len(uses.Items) != 0 {
		t.Fatal("unknown caller received private contexts", uses)
	}
}
