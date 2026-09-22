package collab

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
	"teamcross/internal/mcp"
)

func libraryFixture(t *testing.T) (*App, *Session, LibraryReference, LibraryReference) {
	t.Helper()
	ctx := context.Background()
	a, f, _ := fixture(t)
	d := materialDraft(t, a, f)
	s, err := a.CreateSpace(ctx, SpaceInput{RequestID: uuid.NewString(), Title: "资源库验证"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := a.Publish(ctx, s.record.ID, PublishInput{PreviewID: d.ID, PreviewHash: d.Hash, RequestID: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	mat := LibraryReference{SpaceID: s.record.ID, Kind: "material", MaterialID: out.(map[string]any)["materialId"].(string), Version: 1}
	note, err := s.annotate(Annotation{Text: "核对公开范围", Target: &AnnotationTarget{Kind: "material", MaterialID: mat.MaterialID, Version: 1, TurnID: "public", ItemID: "answer", Quote: "答案", EndOffset: 2}}, "发起者")
	if err != nil {
		t.Fatal(err)
	}
	return a, s, mat, LibraryReference{SpaceID: s.record.ID, Kind: "annotation", AnnotationID: note.ID}
}
func TestLibrarySelectionPersistsAndBundlesAreImmutable(t *testing.T) {
	a, _, mat, note := libraryFixture(t)
	ctx := context.Background()
	yes := true
	for _, ref := range []LibraryReference{mat, note} {
		if err := a.updateLibrary(ctx, libraryUpdate{Action: "select", Reference: ref, Enabled: &yes}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.updateLibrary(ctx, libraryUpdate{Action: "favorite", Reference: mat, Enabled: &yes}); err != nil {
		t.Fatal(err)
	}
	b, err := a.createLibraryBundle(ctx, []LibraryReference{mat, note}, "bundle-one")
	if err != nil {
		t.Fatal(err)
	}
	again, err := a.createLibraryBundle(ctx, []LibraryReference{mat, note}, "bundle-one")
	if err != nil || again.Code != b.Code {
		t.Fatal(again, err)
	}
	if _, err = a.createLibraryBundle(ctx, []LibraryReference{note}, "bundle-one"); err == nil {
		t.Fatal("request identity reused for another selection")
	}
	if err = a.updateLibrary(ctx, libraryUpdate{Action: "clear"}); err != nil {
		t.Fatal(err)
	}
	a.Close()
	reopened, err := Open(Config{DataDir: a.Config.DataDir})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	view := reopened.Library(ctx)
	if len(view.Selection) != 0 {
		t.Fatal(view.Selection)
	}
	favorite := false
	for _, r := range view.Resources {
		if r.Reference == mat {
			favorite = r.Favorite
		}
	}
	if !favorite {
		t.Fatal("favorite was not persisted")
	}
	result, err := reopened.readLibraryBundle(ctx, b.Code, 0)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result)
	for _, want := range []string{"已选的验证结果", "核对公开范围", b.Code} {
		if !strings.Contains(string(raw), want) {
			t.Fatal(string(raw))
		}
	}
	if strings.Contains(string(raw), "未公开") {
		t.Fatal("read outside published version", string(raw))
	}
	stored, err := os.ReadFile(filepath.Join(a.Config.DataDir, "library.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), "已选的验证结果") {
		t.Fatal("material body copied into personal index")
	}
}
func TestLibraryChecksRevocationAndWithdrawnMaterialOnEveryRead(t *testing.T) {
	_, s, mat, note := libraryFixture(t)
	ctx := context.Background()
	if err := s.Share(ctx, "lan"); err != nil {
		t.Fatal(err)
	}
	b, _, _ := fixture(t)
	j, err := b.Join(ctx, s.share.Token())
	if err != nil {
		t.Fatal(err)
	}
	mat.SpaceID = j.ID
	note.SpaceID = j.ID
	bundle, err := b.createLibraryBundle(ctx, []LibraryReference{mat, note}, "guest-selection")
	if err != nil {
		t.Fatal(err)
	}
	yes := true
	if err = b.updateLibrary(ctx, libraryUpdate{Action: "select", Reference: note, Enabled: &yes}); err != nil {
		t.Fatal(err)
	}
	// A peer's unprivileged sharing listener never provides personal library data.
	for _, path := range []string{"/v2/library", "/v2/library/read-selection"} {
		if err = j.request(ctx, "POST", path, map[string]string{"code": bundle.Code}, nil); err == nil {
			t.Fatal("library exposed to peer")
		}
	}
	if err = mcp.ValidateRuntimeCall("read_selection", map[string]any{"code": bundle.Code}); err == nil {
		t.Fatal("shared runtime gained personal library access")
	}
	if _, err = s.withdrawMaterial(ctx, mat.MaterialID); err != nil {
		t.Fatal(err)
	}
	result, err := b.readLibraryBundle(ctx, bundle.Code, 0)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), "已选的验证结果") || !strings.Contains(string(raw), "error") || !strings.Contains(string(raw), "核对公开范围") {
		t.Fatal(string(raw))
	}
	member := j.view(ctx)["selfId"].(string)
	if err = s.RevokeMember(member); err != nil {
		t.Fatal(err)
	}
	result, err = b.readLibraryBundle(ctx, bundle.Code, 0)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(result)
	if strings.Contains(string(raw), "核对公开范围") || strings.Contains(string(raw), `"quote"`) {
		t.Fatal("revoked snapshot returned", string(raw))
	}
	view := b.Library(ctx)
	raw, _ = json.Marshal(view)
	if strings.Contains(string(raw), "核对公开范围") {
		t.Fatal("cached annotation text surfaced after revocation", string(raw))
	}
	no := false
	if err = b.updateLibrary(ctx, libraryUpdate{Action: "select", Key: libraryKey(note), Enabled: &no}); err != nil {
		t.Fatal(err)
	}
	if len(b.Library(ctx).Selection) != 0 {
		t.Fatal("could not remove inaccessible item")
	}
}
func TestLibraryPersonalFilterUsesIdentityAndReplies(t *testing.T) {
	a, s, mat, note := libraryFixture(t)
	ctx := context.Background()
	s.mu.Lock()
	s.record.Annotations[0].AuthorID = "another-member"
	s.record.Annotations[0].Author = "发起者" // Same display name is not the local user.
	s.mu.Unlock()
	check := func(want bool) {
		t.Helper()
		for _, r := range a.Library(ctx).Resources {
			if (r.Reference == mat || r.Reference == note) && r.Annotated != want {
				t.Fatal(r)
			}
		}
	}
	check(false)
	if _, err := s.replyAnnotation(ctx, AnnotationReplyInput{AnnotationID: note.AnnotationID, Text: "我来核对", RequestID: "own-reply"}, "本机用户"); err != nil {
		t.Fatal(err)
	}
	check(true)
}
func TestLibraryConcurrentUpdatesAndVersionPins(t *testing.T) {
	a, s, mat, note := libraryFixture(t)
	ctx := context.Background()
	yes := true
	var wg sync.WaitGroup
	for _, ref := range []LibraryReference{mat, note} {
		for _, action := range []string{"favorite", "select"} {
			wg.Add(1)
			go func(ref LibraryReference, action string) {
				defer wg.Done()
				if err := a.updateLibrary(ctx, libraryUpdate{Action: action, Reference: ref, Enabled: &yes}); err != nil {
					t.Error(err)
				}
			}(ref, action)
		}
	}
	wg.Wait()
	s.mu.Lock()
	v := s.record.Materials[0].Versions[0]
	v.Version = 2
	v.Title = "新版标题"
	s.record.Materials[0].Versions = append(s.record.Materials[0].Versions, v)
	s.mu.Unlock()
	view := a.Library(ctx)
	if len(view.Selection) != 2 {
		t.Fatal(view)
	}
	old, newer := false, false
	for _, r := range view.Resources {
		if r.Reference.Kind == "material" {
			if r.Reference.Version == 1 {
				old = r.Selected && r.Favorite
			} else if r.Reference.Version == 2 {
				newer = !r.Selected && !r.Favorite
			}
		}
	}
	if !old || !newer {
		t.Fatal("selection drifted to new version", view)
	}
	if _, err := a.createLibraryBundle(ctx, []LibraryReference{mat, mat}, "duplicate"); err == nil {
		t.Fatal("duplicate refs accepted")
	}
}
func TestLibraryBundlePagingExpiryAndContextValidation(t *testing.T) {
	a, s, mat, _ := libraryFixture(t)
	ctx := context.Background()
	refs := []LibraryReference{mat}
	for i := 0; i < 5; i++ {
		n, err := s.annotate(Annotation{Text: "分页意见"}, "发起者")
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, LibraryReference{SpaceID: s.record.ID, Kind: "annotation", AnnotationID: n.ID})
	}
	b, err := a.createLibraryBundle(ctx, refs, "paged")
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.readLibraryBundle(ctx, b.Code, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := result.(map[string]any)
	if out["nextOffset"] != 4 || len(out["items"].([]any)) != 4 {
		t.Fatal(out)
	}
	result, err = a.readLibraryBundle(ctx, b.Code, 4)
	if err != nil {
		t.Fatal(err)
	}
	out = result.(map[string]any)
	if out["nextOffset"] != nil || len(out["items"].([]any)) != 2 {
		t.Fatal(out)
	}
	if _, err = a.readLibraryBundle(ctx, b.Code, -1); err == nil {
		t.Fatal("negative offset")
	}
	a.libraryMu.Lock()
	b.ExpiresAt = time.Now().Add(-time.Minute)
	a.library.Bundles[b.Code] = b
	a.libraryMu.Unlock()
	if _, err = a.readLibraryBundle(ctx, b.Code, 0); err == nil {
		t.Fatal("expired bundle")
	}
	for _, ref := range []LibraryReference{{SpaceID: s.record.ID, Kind: "context"}, {SpaceID: "../elsewhere", Kind: "context"}, {SpaceID: s.record.ID, Kind: "material", MaterialID: mat.MaterialID, Version: 0}, {SpaceID: s.record.ID, Kind: "context", Target: &AnnotationTarget{Kind: "file", Path: "../secret", StartLine: 1, EndLine: 1, Quote: "x", ContentHash: strings.Repeat("0", 64)}}} {
		if _, err = a.libraryResource(ctx, ref); err == nil {
			t.Fatal("invalid/private resource accepted", ref)
		}
	}
}

func TestLibrarySharedSelectionStaysInOneSpaceWithoutCreatingBundle(t *testing.T) {
	a, _, mat, note := libraryFixture(t)
	ctx := context.Background()
	s, err := a.CreateSpace(ctx, SpaceInput{RequestID: uuid.NewString(), Title: "另一个空间"})
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.annotate(Annotation{Text: "另一份意见"}, "发起者")
	if err != nil {
		t.Fatal(err)
	}
	other := LibraryReference{SpaceID: s.record.ID, Kind: "annotation", AnnotationID: n.ID}
	if err = a.validateLibrarySelection(ctx, []LibraryReference{mat, note}, true); err != nil {
		t.Fatal(err)
	}
	if err = a.validateLibrarySelection(ctx, []LibraryReference{mat, other}, true); err == nil {
		t.Fatal("shared input accepted references from another space")
	}
	if err = a.validateLibrarySelection(ctx, []LibraryReference{mat, other}, false); err != nil {
		t.Fatal("personal selection rejected an accessible second space", err)
	}
	a.libraryMu.Lock()
	defer a.libraryMu.Unlock()
	if len(a.library.Bundles) != 0 {
		t.Fatal("preview created a personal read entry")
	}
}
