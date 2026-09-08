package workspace

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repository(t *testing.T) string {
	t.Helper()
	r := t.TempDir()
	ctx := context.Background()
	run := func(args ...string) {
		if _, e := Git(ctx, r, args...); e != nil {
			t.Fatal(e)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "fixture@example.invalid")
	run("config", "user.name", "Fixture")
	if e := os.Mkdir(filepath.Join(r, "src"), 0700); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(r, "src", "main.txt"), []byte("committed\n"), 0600)
	os.WriteFile(filepath.Join(r, ".gitignore"), []byte("ignored.txt\n"), 0600)
	run("add", "src/main.txt", ".gitignore")
	run("commit", "-m", "fixture")
	os.WriteFile(filepath.Join(r, "src", "main.txt"), []byte("staged\n"), 0600)
	run("add", "src/main.txt")
	os.WriteFile(filepath.Join(r, "src", "main.txt"), []byte("unstaged\n"), 0600)
	os.WriteFile(filepath.Join(r, "untracked.txt"), []byte("untracked\n"), 0600)
	os.WriteFile(filepath.Join(r, "ignored.txt"), []byte("ignored\n"), 0600)
	return r
}
func TestWorkspaceModesPreserveSource(t *testing.T) {
	for _, mode := range []string{"existing", "worktree"} {
		t.Run(mode, func(t *testing.T) {
			r := repository(t)
			ctx := context.Background()
			before, _ := Git(ctx, r, "status", "--porcelain=v1", "--ignored")
			index, _ := os.ReadFile(filepath.Join(r, ".git", "index"))
			p, e := Inspect(ctx, filepath.Join(r, "src"), mode)
			if e != nil {
				t.Fatal(e)
			}
			dest := filepath.Join(t.TempDir(), "separate")
			cwd, e := Prepare(ctx, p, dest, "codex/collab-test1234")
			if e != nil {
				t.Fatal(e)
			}
			if !p.Dirty {
				t.Fatal("dirty source omitted")
			}
			after, _ := Git(ctx, r, "status", "--porcelain=v1", "--ignored")
			indexAfter, _ := os.ReadFile(filepath.Join(r, ".git", "index"))
			branch, _ := Git(ctx, r, "branch", "--show-current")
			if !bytes.Equal(before, after) || !bytes.Equal(index, indexAfter) || strings.TrimSpace(string(branch)) != "main" {
				t.Fatal("source Git state changed")
			}
			if mode == "existing" {
				resolved, _ := filepath.EvalSymlinks(filepath.Join(r, "src"))
				if cwd != resolved {
					t.Fatal(cwd)
				}
				data, _ := os.ReadFile(filepath.Join(cwd, "main.txt"))
				if string(data) != "unstaged\n" {
					t.Fatal("existing content lost")
				}
			} else {
				if cwd != filepath.Join(dest, "src") {
					t.Fatal("subdirectory not mapped", cwd)
				}
				data, _ := os.ReadFile(filepath.Join(cwd, "main.txt"))
				if string(data) != "committed\n" {
					t.Fatal("dirty content copied")
				}
				for _, name := range []string{"untracked.txt", "ignored.txt"} {
					if _, e := os.Stat(filepath.Join(dest, name)); !os.IsNotExist(e) {
						t.Fatalf("unexpected copied file %s", name)
					}
				}
				head, _ := Git(ctx, dest, "rev-parse", "HEAD")
				if strings.TrimSpace(string(head)) != p.Head {
					t.Fatal("wrong HEAD")
				}
				newBranch, _ := Git(ctx, dest, "branch", "--show-current")
				if strings.TrimSpace(string(newBranch)) != "codex/collab-test1234" {
					t.Fatal("wrong new branch")
				}
			}
		})
	}
}
func TestReadFileCannotEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	os.WriteFile(outside, []byte("outside"), 0600)
	os.Symlink(outside, filepath.Join(root, "escape"))
	if _, e := ReadFile(root, "escape"); e == nil {
		t.Fatal("symlink escaped")
	}
	if _, e := ReadFile(root, outside); e == nil {
		t.Fatal("absolute path allowed")
	}
}
