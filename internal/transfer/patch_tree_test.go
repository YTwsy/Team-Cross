package transfer

import (
	"context"
	"os"
	"testing"
)

func TestPatchFinalTreeCollisionRejectedBeforeCheckout(t *testing.T) {
	for _, staged := range []bool{false, true} {
		t.Run(map[bool]string{false: "unstaged", true: "staged"}[staged], func(t *testing.T) {
			bundle := treeBundle(t, map[string]GitObject{"foo": {Data: []byte("baseline\n")}})
			patch := []byte("diff --git a/Foo b/Foo\nnew file mode 100644\n--- /dev/null\n+++ b/Foo\n@@ -0,0 +1 @@\n+different\n")
			if staged {
				bundle.Snapshot.StagedPatch = patch
			} else {
				bundle.Snapshot.UnstagedPatch = patch
			}
			parent := t.TempDir()
			if _, err := Materialize(context.Background(), bundle, parent); err == nil {
				t.Fatal("patch creating case collision was accepted")
			}
			entries, _ := os.ReadDir(parent)
			if len(entries) != 0 {
				t.Fatal("failed patch preflight left checkout")
			}
		})
	}
}

func TestIntermediateStagedCollisionRejectedEvenWhenFinalTreeIsSafe(t *testing.T) {
	bundle := treeBundle(t, map[string]GitObject{"foo": {Data: []byte("baseline\n")}})
	bundle.Snapshot.StagedPatch = []byte("diff --git a/Foo b/Foo\nnew file mode 100644\n--- /dev/null\n+++ b/Foo\n@@ -0,0 +1 @@\n+temporary\n")
	bundle.Snapshot.UnstagedPatch = []byte("diff --git a/Foo b/Foo\ndeleted file mode 100644\n--- a/Foo\n+++ /dev/null\n@@ -1 +0,0 @@\n-temporary\n")
	parent := t.TempDir()
	if _, err := Materialize(context.Background(), bundle, parent); err == nil {
		t.Fatal("unsafe staged tree hidden by final tree was accepted")
	}
	entries, _ := os.ReadDir(parent)
	if len(entries) != 0 {
		t.Fatal("failed intermediate preflight left checkout")
	}
}
