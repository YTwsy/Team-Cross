package transfer

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"teamcross/internal/gitstate"
)

func gitTest(t *testing.T, repo string, args ...string) []byte {
	t.Helper()
	output, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return output
}

func testBundle(t *testing.T) (Bundle, string) {
	t.Helper()
	repo := t.TempDir()
	gitTest(t, repo, "init", "-b", "main")
	gitTest(t, repo, "config", "user.name", "Transfer Test")
	gitTest(t, repo, "config", "user.email", "transfer@example.invalid")
	writeTestFile(t, repo, "tracked.txt", []byte("baseline\n"))
	writeTestFile(t, repo, "binary.bin", []byte{0, 1, 2, 3})
	gitTest(t, repo, "add", ".")
	gitTest(t, repo, "commit", "-m", "baseline")
	writeTestFile(t, repo, "tracked.txt", []byte("staged\n"))
	gitTest(t, repo, "add", "tracked.txt")
	writeTestFile(t, repo, "tracked.txt", []byte("final\n"))
	writeTestFile(t, repo, "binary.bin", []byte{0, 9, 255, 3})
	writeTestFile(t, repo, "new.bin", []byte{0, 4, 0, 5})
	if err := os.Symlink("tracked.txt", filepath.Join(repo, "local-link")); err != nil {
		t.Fatal(err)
	}
	snapshot, err := gitstate.Capture(context.Background(), repo, []string{"new.bin", "local-link"})
	if err != nil {
		t.Fatal(err)
	}
	format, objects, err := CaptureBaseline(context.Background(), repo, snapshot.Head)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.RepoRoot, snapshot.Branch, snapshot.Status = "", "", nil
	bundle := Bundle{Format: Format, Version: Version, CreatedAt: time.Now().UTC(), Title: "Review fork", Origin: Origin{ThreadID: "source-thread", RoundID: "source-round"}, Baseline: snapshot.Head, ObjectFormat: format, GitObjects: objects, Snapshot: snapshot}
	if err := bundle.Validate(); err != nil {
		t.Fatal(err)
	}
	return bundle, repo
}

func writeTestFile(t *testing.T, root, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOfflineBundleRestoresBinaryStagedUntrackedAfterSenderGone(t *testing.T) {
	bundle, repo := testBundle(t)
	before := gitTest(t, repo, "status", "--porcelain=v2", "-z")
	want, err := TreeDigest(repo)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, gitTest(t, repo, "status", "--porcelain=v2", "-z")) {
		t.Fatal("export changed source checkout")
	}
	// This temporary fixture is the entire source machine in the test. No
	// repository path, remote or alternates survive in the decoded envelope.
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	restored, err := Materialize(context.Background(), decoded, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Cleanup()
	got, err := TreeDigest(restored.Worktree)
	if err != nil || got != want {
		t.Fatalf("restored tree digest=%s want=%s err=%v", got, want, err)
	}
	if string(gitTest(t, restored.Worktree, "show", ":tracked.txt")) != "staged\n" {
		t.Fatal("staged state was lost")
	}
	if len(gitTest(t, restored.Repo, "remote")) != 0 {
		t.Fatal("imported repository unexpectedly has remotes")
	}
	patch, err := gitstate.ExportBinaryPatch(context.Background(), restored.Worktree, bundle.Baseline)
	if err != nil || !bytes.Contains(patch, []byte("GIT binary patch")) || !bytes.Contains(patch, []byte("new.bin")) {
		t.Fatalf("binary/untracked export failed: %v %s", err, patch)
	}
}

func cloneBundle(t *testing.T, bundle Bundle) Bundle {
	t.Helper()
	data, _ := json.Marshal(bundle)
	var copy Bundle
	if err := json.Unmarshal(data, &copy); err != nil {
		t.Fatal(err)
	}
	return copy
}

func TestBundleRejectsCorruptionMissingObjectsAndUnsafePaths(t *testing.T) {
	fixture, _ := testBundle(t)
	cases := map[string]func(*Bundle){
		"unknown version":      func(b *Bundle) { b.Version++ },
		"Git hash mismatch":    func(b *Bundle) { b.GitObjects[0].Data = append(b.GitObjects[0].Data, 'x') },
		"duplicate Git object": func(b *Bundle) { b.GitObjects = append(b.GitObjects, b.GitObjects[0]) },
		"traversal":            func(b *Bundle) { b.Snapshot.Untracked[0].Path = "../escape" },
		"absolute path":        func(b *Bundle) { b.Snapshot.Untracked[0].Path = "/private/tmp/escape" },
		"Git metadata":         func(b *Bundle) { b.Snapshot.Untracked[0].Path = ".Git/config" },
		"wrong untracked hash": func(b *Bundle) { b.Snapshot.Untracked[0].SHA256 = "bad" },
		"missing evidence":     func(b *Bundle) { b.Evidence = []Evidence{{ID: "private", ObjectHash: ContentHash(nil)}} },
		"unselected object":    func(b *Bundle) { b.Objects = []Object{{Hash: ContentHash([]byte("private")), Data: []byte("private")}} },
		"host path":            func(b *Bundle) { b.Snapshot.RepoRoot = "/Users/sender/private" },
		"escaping symlink": func(b *Bundle) {
			for i := range b.Snapshot.Untracked {
				if b.Snapshot.Untracked[i].Symlink {
					f := &b.Snapshot.Untracked[i]
					f.Content = []byte("../../outside")
					f.Size = int64(len(f.Content))
					f.SHA256 = ContentHash(f.Content)
				}
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			bundle := cloneBundle(t, fixture)
			mutate(&bundle)
			if err := bundle.Validate(); err == nil {
				t.Fatal("invalid bundle accepted")
			}
		})
	}
	t.Run("missing Git closure", func(t *testing.T) {
		bundle := cloneBundle(t, fixture)
		for index, obj := range bundle.GitObjects {
			if obj.Type == "blob" {
				bundle.GitObjects = append(bundle.GitObjects[:index], bundle.GitObjects[index+1:]...)
				break
			}
		}
		parent := t.TempDir()
		if _, err := Materialize(context.Background(), bundle, parent); err == nil {
			t.Fatal("missing baseline blob accepted")
		}
		entries, _ := os.ReadDir(parent)
		if len(entries) != 0 {
			t.Fatal("failed restore left a staging repository")
		}
	})
}

func TestBundleRejectsSymlinkParentAndUnsafePatchWithoutOutsideWrites(t *testing.T) {
	fixture, _ := testBundle(t)
	parent := t.TempDir()
	sentinel := filepath.Join(parent, "sentinel")
	if err := os.WriteFile(sentinel, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Run("symlink parent", func(t *testing.T) {
		bundle := cloneBundle(t, fixture)
		file := gitstate.UntrackedFile{Path: "local-link/child", Content: []byte("bad"), Size: 3, SHA256: ContentHash([]byte("bad")), Included: true}
		bundle.Snapshot.Untracked = append(bundle.Snapshot.Untracked, file)
		if _, err := Materialize(context.Background(), bundle, parent); err == nil {
			t.Fatal("write through symlink accepted")
		}
	})
	t.Run("patch escape", func(t *testing.T) {
		bundle := cloneBundle(t, fixture)
		bundle.Snapshot.StagedPatch = nil
		bundle.Snapshot.UnstagedPatch = []byte("diff --git a/../sentinel b/../sentinel\nnew file mode 100644\n--- /dev/null\n+++ b/../sentinel\n@@ -0,0 +1 @@\n+changed\n")
		if _, err := Materialize(context.Background(), bundle, parent); err == nil {
			t.Fatal("escaping patch accepted")
		}
	})
	data, _ := os.ReadFile(sentinel)
	if string(data) != "unchanged" {
		t.Fatal("outside sentinel changed")
	}
}

func TestPortableRestoreDoesNotRunInheritedCheckoutFilters(t *testing.T) {
	bundle, repo := testBundle(t)
	writeTestFile(t, repo, ".gitattributes", []byte("*.txt filter=outside\n"))
	gitTest(t, repo, "add", ".gitattributes")
	gitTest(t, repo, "commit", "-m", "attributes")
	baseline := strings.TrimSpace(string(gitTest(t, repo, "rev-parse", "HEAD")))
	format, objects, err := CaptureBaseline(context.Background(), repo, baseline)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Baseline, bundle.ObjectFormat, bundle.GitObjects = baseline, format, objects
	bundle.Snapshot = gitstate.Snapshot{Head: baseline}
	sentinel := filepath.Join(t.TempDir(), "filter-ran")
	config := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(config, []byte("[filter \"outside\"]\n\tsmudge = touch "+sentinel+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	restored, err := Materialize(context.Background(), bundle, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Cleanup()
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("inherited checkout filter executed")
	}
}
