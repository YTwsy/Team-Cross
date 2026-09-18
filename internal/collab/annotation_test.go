package collab

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"teamcross/internal/workspace"
	"testing"
	"time"
)

func TestParticipantAnnotationRoundTripAndRevokedAccess(t *testing.T) {
	a, runtime, _ := fixture(t)
	s := createFixture(t, a, runtime, "existing")
	b, _, _ := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Action(ctx, "share"); err != nil {
		t.Fatal(err)
	}
	j, err := b.Join(ctx, s.share.Token())
	if err != nil {
		t.Fatal(err)
	}
	var result Annotation
	in := Annotation{Text: "针对原消息的意见", Target: &AnnotationTarget{Kind: "history", TurnID: "turn", ItemID: "item", Quote: "中文🙂", StartOffset: 5, EndOffset: 9}}
	if err := j.request(ctx, "POST", "/v2/annotations", in, &result); err != nil {
		t.Fatal(err)
	}
	if result.Author != b.Host || result.AuthorID != firstRemote(s) || result.Target.SessionID != s.record.SessionID || result.Target.EndOffset != 9 {
		t.Fatal(result)
	}
	var read struct {
		Annotations  []Annotation `json:"annotations"`
		ExecutionCwd string       `json:"executionCwd"`
	}
	if err := j.request(ctx, "GET", "/v2/context?kind=annotations", nil, &read); err != nil {
		t.Fatal(err)
	}
	if len(read.Annotations) != 1 || read.Annotations[0].Target.ItemID != "item" || read.ExecutionCwd != s.record.ExecutionCwd {
		t.Fatal(read)
	}
	if s.writer != "owner" {
		t.Fatal("annotation changed input ownership")
	}
	runtime.mu.Lock()
	for _, method := range runtime.calls {
		if method == "turn/start" || method == "turn/steer" {
			t.Error("annotation started a turn")
		}
	}
	runtime.mu.Unlock()
	if err := s.Action(ctx, "end"); err != nil {
		t.Fatal(err)
	}
	if err := j.request(ctx, "POST", "/v2/annotations", in, &result); err == nil {
		t.Fatal("ended participant still wrote an annotation")
	}
}

func TestAnnotationSnapshotPersistsWithoutStartingTurn(t *testing.T) {
	a, runtime, repo := fixture(t)
	s := createFixture(t, a, runtime, "existing")
	ctx := context.Background()
	value, err := s.Context(ctx, "file", "file.txt", 0)
	if err != nil {
		t.Fatal(err)
	}
	file := value.(map[string]any)
	hash := sha256.Sum256([]byte("baseline"))
	if file["contentHash"] != hex.EncodeToString(hash[:]) {
		t.Fatal(file)
	}
	target := &AnnotationTarget{Kind: "file", Path: "file.txt", StartLine: 1, EndLine: 1, Quote: "baseline", ContentHash: file["contentHash"].(string)}
	// A file can change while a user is writing a comment. Preserve the selected
	// snapshot rather than silently binding the comment to the replacement text.
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	runtime.mu.Lock()
	before := len(runtime.calls)
	runtime.mu.Unlock()
	annotation, err := s.annotate(Annotation{Text: "请保留原来的行为", Target: target}, "发起者")
	if err != nil {
		t.Fatal(err)
	}
	if target.SessionID != "" || annotation.Target.SessionID != s.record.SessionID || annotation.Target.Quote != "baseline" {
		t.Fatal(annotation)
	}
	got, err := s.Context(ctx, "annotations", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if list := got.(map[string]any)["annotations"].([]Annotation); len(list) != 1 || list[0].ID != annotation.ID {
		t.Fatal(got)
	}
	runtime.mu.Lock()
	after := len(runtime.calls)
	runtime.mu.Unlock()
	if after != before {
		t.Fatal("saving or reading annotations called the native runtime")
	}
	var record Record
	if err := readJSON(filepath.Join(a.Config.DataDir, "collaborations", s.record.ID, "collaboration.json"), &record); err != nil {
		t.Fatal(err)
	}
	if len(record.Annotations) != 1 || record.Annotations[0].Target.ContentHash != file["contentHash"] {
		t.Fatal(record.Annotations)
	}
	current, err := s.Context(ctx, "file", "file.txt", 0)
	if err != nil || current.(map[string]any)["contentHash"] == file["contentHash"] {
		t.Fatal("changed file kept the old hash", current, err)
	}
	wrong := *target
	wrong.SessionID = "different-fork"
	if _, err := s.annotate(Annotation{Text: "wrong", Target: &wrong}, "发起者"); err == nil {
		t.Fatal("foreign session anchor accepted")
	}
}

func TestAnnotationTargetValidation(t *testing.T) {
	valid := AnnotationTarget{Kind: "file", Path: "src/file.ts", StartLine: 2, EndLine: 3, Quote: "first\nsecond", ContentHash: strings.Repeat("a", 64)}
	for name, mutate := range map[string]func(*AnnotationTarget){
		"absolute":         func(v *AnnotationTarget) { v.Path = "/tmp/outside" },
		"traversal":        func(v *AnnotationTarget) { v.Path = "../../outside" },
		"empty path":       func(v *AnnotationTarget) { v.Path = "" },
		"reversed lines":   func(v *AnnotationTarget) { v.EndLine = 1 },
		"wrong line count": func(v *AnnotationTarget) { v.EndLine = 4 },
		"bad hash":         func(v *AnnotationTarget) { v.ContentHash = "HEAD" },
		"empty quote":      func(v *AnnotationTarget) { v.Quote = "" },
		"oversize quote":   func(v *AnnotationTarget) { v.Quote = strings.Repeat("字", 8001) },
		"missing side":     func(v *AnnotationTarget) { v.Kind = "changes"; v.BaseRevision = strings.Repeat("b", 40) },
		"missing base":     func(v *AnnotationTarget) { v.Kind = "changes"; v.Side = "old" },
		"mixed identity":   func(v *AnnotationTarget) { v.ItemID = "message" },
	} {
		t.Run(name, func(t *testing.T) {
			target := valid
			mutate(&target)
			if target.validate() == nil {
				t.Fatal("invalid target accepted", target)
			}
		})
	}
	if err := valid.validate(); err != nil {
		t.Fatal(err)
	}
	history := AnnotationTarget{Kind: "history", TurnID: "turn", ItemID: "item", Quote: "中文🙂", StartOffset: 10, EndOffset: 14}
	if err := history.validate(); err != nil {
		t.Fatal(err)
	}
	history.EndOffset = 13
	if history.validate() == nil {
		t.Fatal("accepted UTF-8/rune offsets instead of UTF-16")
	}
}

func TestChangesAnchorsAreRelativeToExecutionDirectory(t *testing.T) {
	a, runtime, repo := fixture(t)
	dir := filepath.Join(repo, "nested")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "file.ts")
	os.WriteFile(path, []byte("old\n"), 0600)
	ctx := context.Background()
	if _, err := workspace.Git(ctx, repo, "add", "nested/file.ts"); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Git(ctx, repo, "commit", "-m", "nested fixture"); err != nil {
		t.Fatal(err)
	}
	runtime.source.Cwd = dir
	s := createFixture(t, a, runtime, "existing")
	os.WriteFile(path, []byte("new\n"), 0600)
	value, err := s.Context(ctx, "changes", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	changes := value.(map[string]any)
	diff := changes["diff"].(string)
	if !strings.Contains(diff, "--- a/file.ts\n+++ b/file.ts\n") || strings.Contains(diff, "a/nested/") {
		t.Fatal(diff)
	}
	hash := sha256.Sum256([]byte(diff))
	if changes["contentHash"] != hex.EncodeToString(hash[:]) || len(changes["baseRevision"].(string)) != 40 {
		t.Fatal(changes)
	}
	old := AnnotationTarget{Kind: "changes", Path: "file.ts", Side: "old", StartLine: 1, EndLine: 1, Quote: "old", ContentHash: changes["contentHash"].(string), BaseRevision: changes["baseRevision"].(string)}
	if err := old.validate(); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(old)
	if !strings.Contains(string(encoded), `"side":"old"`) {
		t.Fatal(string(encoded))
	}
}
