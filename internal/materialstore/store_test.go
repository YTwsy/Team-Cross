package materialstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testBundle(t *testing.T) Bundle {
	t.Helper()
	large := strings.Repeat("工具输出🙂\n", 900)
	body := BlobBody(large)
	manifest := Manifest{
		Schema: ManifestSchema, Title: "存储验证", Provider: "codex", SourceID: "source",
		StartTurnID: "turn", EndTurnID: "turn",
		Turns: []Turn{{
			ID: "turn", Status: "completed", Label: "验证内容寻址存储",
			Items: []Item{{ID: "question", Type: "userMessage", Body: InlineBody("问题")}, {ID: "tool", Type: "toolResult", Body: body}},
		}},
	}
	manifest, _, _, err := Finalize(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return Bundle{Manifest: manifest, Blobs: map[string]string{body.Hash: large}}
}

func TestStoreCommitsBlobsBeforeCanonicalManifest(t *testing.T) {
	store := New(t.TempDir())
	bundle := testBundle(t)
	hash, err := store.PutBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.PutBundle(bundle)
	if err != nil || second != hash {
		t.Fatal("content-addressed retry changed result", second, err)
	}
	manifest, err := store.LoadManifest(hash)
	if err != nil || manifest.Turns[0].Hash == "" {
		t.Fatal(manifest, err)
	}
	text, err := store.ReadBody(manifest.Turns[0].Items[1])
	if err != nil || text != bundle.Blobs[manifest.Turns[0].Items[1].Body.Hash] {
		t.Fatal("blob did not round trip", err)
	}
	if !store.HasBlob(manifest.Turns[0].Items[1].Body.Hash) {
		t.Fatal("committed blob is not addressable inside its authorized store")
	}
}

func TestStoreRejectsMissingExtraAndCorruptBodies(t *testing.T) {
	root := t.TempDir()
	store := New(root)
	bundle := testBundle(t)
	body := bundle.Manifest.Turns[0].Items[1].Body
	missing := bundle
	missing.Blobs = nil
	if _, err := store.PutBundle(missing); err == nil {
		t.Fatal("missing blob accepted")
	}
	extra := testBundle(t)
	extra.Blobs[HashBlob("not referenced")] = "not referenced"
	if _, err := store.PutBundle(extra); err == nil {
		t.Fatal("unreferenced blob accepted")
	}
	if _, err := store.PutBundle(bundle); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "blobs", body.Hash[:2], body.Hash)
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadBody(bundle.Manifest.Turns[0].Items[1]); err == nil {
		t.Fatal("corrupt blob passed hash validation")
	}
}

func TestManifestRejectsConflictingMetadataForOneBlob(t *testing.T) {
	bundle := testBundle(t)
	item := bundle.Manifest.Turns[0].Items[1]
	conflict := item
	conflict.ID = "same-blob-wrong-length"
	conflict.Body.UTF16Length++
	bundle.Manifest.Turns[0].Hash = ""
	bundle.Manifest.Turns[0].Items = append(bundle.Manifest.Turns[0].Items, conflict)
	if _, _, _, err := Finalize(bundle.Manifest); err == nil {
		t.Fatal("one blob hash accepted conflicting length metadata")
	}
}
