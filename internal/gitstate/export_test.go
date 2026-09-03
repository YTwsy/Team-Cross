package gitstate

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestExportCapturedFilesRemainIncludedAfterIgnore(t *testing.T) {
	ctx := context.Background()
	repo := initializedRepository(t)
	files := map[string][]byte{
		"captured.bin":     {0, 255, 1, 0, 42},
		"space 引用\"\t.txt": []byte("selected unusual name\n"),
		"removed.bin":      []byte("must not resurrect\n"),
		"now-tracked.txt":  []byte("staged once\n"),
	}
	selected := []string{}
	for path, data := range files {
		writeExportFixture(t, filepath.Join(repo, path), data)
		selected = append(selected, path)
	}
	if err := os.Symlink("tracked.txt", filepath.Join(repo, "captured-link")); err != nil {
		t.Fatal(err)
	}
	selected = append(selected, "captured-link")
	snapshot, err := Capture(ctx, repo, selected)
	if err != nil {
		t.Fatal(err)
	}
	before := runGit(t, repo, "status", "--porcelain=v2", "-z", "--untracked-files=all")
	worktree := filepath.Join(t.TempDir(), "worktree")
	if err := CreateWorktree(ctx, snapshot, worktree); err != nil {
		t.Fatal(err)
	}
	writeExportFixture(t, filepath.Join(worktree, ".gitignore"), []byte("*\n!.gitignore\n"))
	writeExportFixture(t, filepath.Join(worktree, "private-secret.txt"), []byte("UNSELECTED SECRET\n"))
	if err := os.Remove(filepath.Join(worktree, "removed.bin")); err != nil {
		t.Fatal(err)
	}
	runGit(t, worktree, "add", "-f", "now-tracked.txt")
	selected = append(selected, "captured.bin") // Duplicate grants do not duplicate the patch.
	patch, err := ExportBinaryPatchWithCaptured(ctx, worktree, snapshot.Head, selected)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := PatchPaths(ctx, worktree, patch)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"captured.bin", "space 引用\"\t.txt", "now-tracked.txt", "captured-link", ".gitignore"} {
		if !slices.Contains(paths, path) {
			t.Fatalf("captured path %q absent from %q", path, paths)
		}
	}
	if slices.Contains(paths, "removed.bin") || slices.Contains(paths, "private-secret.txt") || bytes.Contains(patch, []byte("UNSELECTED SECRET")) {
		t.Fatalf("deleted or unauthorized ignored file exported: %q", paths)
	}
	unique := map[string]bool{}
	for _, path := range paths {
		if unique[path] {
			t.Fatalf("duplicate patch for captured/tracked path %q", path)
		}
		unique[path] = true
	}
	plain, err := ExportBinaryPatch(ctx, worktree, snapshot.Head)
	if err != nil {
		t.Fatal(err)
	}
	plainPaths, err := PatchPaths(ctx, worktree, plain)
	if err != nil || slices.Contains(plainPaths, "captured.bin") {
		t.Fatalf("unscoped exporter enumerated ignored files: %q %v", plainPaths, err)
	}
	fresh := filepath.Join(t.TempDir(), "fresh")
	runGit(t, repo, "worktree", "add", "--detach", fresh, snapshot.Head)
	applyExportFixture(t, fresh, patch)
	for path, want := range files {
		if path == "removed.bin" {
			continue
		}
		got, err := os.ReadFile(filepath.Join(fresh, path))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("applied %q = %q, %v", path, got, err)
		}
	}
	if target, err := os.Readlink(filepath.Join(fresh, "captured-link")); err != nil || target != "tracked.txt" {
		t.Fatalf("symlink = %q %v", target, err)
	}
	// A flattened immutable patch also grants its new, now ignored files.
	reexport, err := ExportSnapshotPatch(ctx, fresh, Snapshot{Head: snapshot.Head, UnstagedPatch: patch})
	if err != nil {
		t.Fatal(err)
	}
	reexportPaths, err := PatchPaths(ctx, fresh, reexport)
	if err != nil || !slices.Contains(reexportPaths, "captured.bin") {
		t.Fatalf("flattened snapshot lost captured path: %q %v", reexportPaths, err)
	}
	if !bytes.Equal(before, runGit(t, repo, "status", "--porcelain=v2", "-z", "--untracked-files=all")) {
		t.Fatal("export altered source checkout")
	}
}

func TestExportCapturedPathsRejectUnsafeParentsAndDirectories(t *testing.T) {
	repo := initializedRepository(t)
	baseline := strings.TrimSpace(string(runGit(t, repo, "rev-parse", "HEAD")))
	outside := t.TempDir()
	writeExportFixture(t, filepath.Join(outside, "private"), []byte("OUTSIDE SECRET"))
	if err := os.Symlink(outside, filepath.Join(repo, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repo, "was-a-file"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeExportFixture(t, filepath.Join(repo, "was-a-file", "new-secret"), []byte("DIRECTORY SECRET"))
	for _, path := range []string{"../private", "/private", "a/../../private", "a//b", "a/./b", "a\\b", "a\x00b", ".git/config", ".GIT/config", "escape/private", "was-a-file"} {
		t.Run(path, func(t *testing.T) {
			if patch, err := ExportBinaryPatchWithCaptured(context.Background(), repo, baseline, []string{path}); err == nil {
				t.Fatalf("unsafe captured path accepted: %q patch=%q", path, patch)
			}
		})
	}
	// A removed parent is absence, not authorization to resurrect old content.
	if _, err := ExportBinaryPatchWithCaptured(context.Background(), repo, baseline, []string{"removed-parent/file"}); err != nil {
		t.Fatal(err)
	}
}

func TestPatchPathsRetainsRenameTargetAndRejectsUnsafeName(t *testing.T) {
	repo := initializedRepository(t)
	patch := []byte("diff --git a/old.txt b/new name.txt\nsimilarity index 100%\nrename from old.txt\nrename to new name.txt\n")
	paths, err := PatchPaths(context.Background(), repo, patch)
	if err != nil || !slices.Equal(paths, []string{"new name.txt"}) {
		t.Fatalf("rename target=%q err=%v", paths, err)
	}
	unsafe := []byte("diff --git a/../escape b/../escape\nnew file mode 100644\n--- /dev/null\n+++ b/../escape\n@@ -0,0 +1 @@\n+secret\n")
	if paths, err := PatchPaths(context.Background(), repo, unsafe); err == nil {
		t.Fatalf("unsafe patch accepted: %q", paths)
	}
	for _, target := range []string{"rename space.txt", "rename\ttab.txt", "重命名.txt", "space 中文\ttab.txt"} {
		t.Run(target, func(t *testing.T) {
			repo := initializedRepository(t)
			runGit(t, repo, "mv", "tracked.txt", target)
			snapshot, err := Capture(context.Background(), repo, nil)
			if err != nil {
				t.Fatal(err)
			}
			paths, err := PatchPaths(context.Background(), repo, snapshot.StagedPatch)
			if err != nil || !slices.Equal(paths, []string{target}) {
				t.Fatalf("real Git rename target=%q err=%v", paths, err)
			}
		})
	}
}

func TestExportBaselineFileRemovedFromIndexRemainsCodeWithoutDuplicatePatch(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "changed"}[changed], func(t *testing.T) {
			ctx := context.Background()
			repo := initializedRepository(t)
			head := strings.TrimSpace(string(runGit(t, repo, "rev-parse", "HEAD")))
			runGit(t, repo, "rm", "--cached", "binary.bin")
			writeExportFixture(t, filepath.Join(repo, ".gitignore"), []byte("*.bin\n"))
			want := []byte{0, 1, 2, 3}
			if changed {
				want = []byte{0, 255, 42, 0, 5}
				writeExportFixture(t, filepath.Join(repo, "binary.bin"), want)
			}
			writeExportFixture(t, filepath.Join(repo, "private.bin"), []byte("PRIVATE UNCAPTURED"))
			indexBefore, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
			if err != nil {
				t.Fatal(err)
			}
			statusBefore := runGit(t, repo, "status", "--porcelain=v2", "-z")
			objectsBefore := runGit(t, repo, "count-objects", "-v")
			// Baseline itself grants this file, even if no prior change named it.
			patch, err := ExportBinaryPatchWithCaptured(ctx, repo, head, nil)
			if err != nil {
				t.Fatal(err)
			}
			paths, err := PatchPaths(ctx, repo, patch)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, path := range paths {
				if path == "binary.bin" {
					count++
				}
				if path == "private.bin" {
					t.Fatal("temporary export index widened ignored scope")
				}
			}
			if count != map[bool]int{false: 0, true: 1}[changed] {
				t.Fatalf("duplicate or incorrect baseline file patch count=%d paths=%q", count, paths)
			}
			indexAfter, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
			if err != nil || !bytes.Equal(indexBefore, indexAfter) || !bytes.Equal(objectsBefore, runGit(t, repo, "count-objects", "-v")) || !bytes.Equal(statusBefore, runGit(t, repo, "status", "--porcelain=v2", "-z")) || head != strings.TrimSpace(string(runGit(t, repo, "rev-parse", "HEAD"))) {
				t.Fatal("export changed original index, objects, status, or HEAD")
			}
			fresh := filepath.Join(t.TempDir(), "fresh")
			runGit(t, repo, "worktree", "add", "--detach", fresh, head)
			applyExportFixture(t, fresh, patch)
			data, err := os.ReadFile(filepath.Join(fresh, "binary.bin"))
			if err != nil || !bytes.Equal(data, want) {
				t.Fatalf("baseline code not preserved: %v %v", data, err)
			}
		})
	}
}

func writeExportFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func applyExportFixture(t *testing.T, repo string, patch []byte) {
	t.Helper()
	for _, args := range [][]string{{"apply", "--binary", "--check", "-"}, {"apply", "--binary", "-"}} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Stdin = bytes.NewReader(patch)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("apply patch: %v %s", err, output)
		}
	}
}
