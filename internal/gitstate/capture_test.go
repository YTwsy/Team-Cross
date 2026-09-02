package gitstate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"teamcross/internal/domain"
)

type memoryObjects map[string][]byte

func (m memoryObjects) PutObject(_ context.Context, data []byte, mime string) (domain.Object, error) {
	digest := sha256.Sum256(data)
	hash := hex.EncodeToString(digest[:])
	m[hash] = bytes.Clone(data)
	return domain.Object{Hash: hash, Size: int64(len(data)), MIME: mime}, nil
}

func (m memoryObjects) GetObject(_ context.Context, hash string) ([]byte, error) {
	data, ok := m[hash]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return bytes.Clone(data), nil
}

func runGit(t *testing.T, repo string, args ...string) []byte {
	t.Helper()
	commandArgs := append([]string{"-C", repo}, args...)
	cmd := exec.Command("git", commandArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return out
}

func initializedRepository(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Team Cross Test")
	runGit(t, repo, "config", "user.email", "teamcross@example.invalid")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "binary.bin"), []byte{0, 1, 2, 3}, 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")
	return repo
}

func TestCaptureAndCreateWorktreePreserveSource(t *testing.T) {
	ctx := context.Background()
	repo := initializedRepository(t)
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("staged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "tracked.txt")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("staged\nunstaged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "binary.bin"), []byte{0, 9, 8, 7, 6}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "notes"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "notes", "handoff.txt"), []byte("blocked on parser\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	before := runGit(t, repo, "status", "--porcelain=v2", "-z", "--untracked-files=all")
	snapshot, err := Capture(ctx, repo, []string{"notes/handoff.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Unborn || snapshot.Head == "" || len(snapshot.StagedPatch) == 0 || len(snapshot.UnstagedPatch) == 0 {
		t.Fatalf("incomplete snapshot: %#v", snapshot)
	}
	if len(snapshot.Untracked) != 1 || !snapshot.Untracked[0].Included {
		t.Fatalf("untracked capture = %#v", snapshot.Untracked)
	}
	worktree := filepath.Join(filepath.Dir(repo), "worktree")
	if err := CreateWorktree(ctx, snapshot, worktree); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(worktree, "tracked.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "staged\nunstaged\n" {
		t.Fatalf("worktree tracked content = %q", got)
	}
	got, err = os.ReadFile(filepath.Join(worktree, "notes", "handoff.txt"))
	if err != nil || string(got) != "blocked on parser\n" {
		t.Fatalf("worktree untracked content = %q, err=%v", got, err)
	}
	patch, err := ExportBinaryPatch(ctx, worktree, snapshot.Head)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(patch, []byte("tracked.txt")) || !bytes.Contains(patch, []byte("binary.bin")) || !bytes.Contains(patch, []byte("notes/handoff.txt")) {
		t.Fatalf("exported patch missing files:\n%s", patch)
	}
	exportTarget := filepath.Join(filepath.Dir(repo), "export-target")
	runGit(t, repo, "worktree", "add", "--detach", exportTarget, snapshot.Head)
	apply := exec.Command("git", "-C", exportTarget, "apply", "--binary", "-")
	apply.Stdin = bytes.NewReader(patch)
	if output, err := apply.CombinedOutput(); err != nil {
		t.Fatalf("apply exported patch: %v: %s", err, output)
	}
	for _, path := range []string{"tracked.txt", "binary.bin", "notes/handoff.txt"} {
		want, err := os.ReadFile(filepath.Join(worktree, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(exportTarget, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("exported %s differs: got %q want %q", path, got, want)
		}
	}
	after := runGit(t, repo, "status", "--porcelain=v2", "-z", "--untracked-files=all")
	if !bytes.Equal(before, after) {
		t.Fatalf("source worktree changed\nbefore: %q\nafter:  %q", before, after)
	}
}

func TestCaptureUntrackedLimitsAndSafePaths(t *testing.T) {
	repo := initializedRepository(t)
	if err := os.WriteFile(filepath.Join(repo, "small.txt"), []byte("1234"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "large.txt"), []byte("123456"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureWithOptions(context.Background(), repo, []string{"small.txt", "large.txt"}, CaptureOptions{
		MaxUntrackedFileBytes:  5,
		MaxUntrackedTotalBytes: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Untracked) != 2 {
		t.Fatalf("files = %#v", snapshot.Untracked)
	}
	var included, omitted int
	for _, file := range snapshot.Untracked {
		if file.Included {
			included++
		} else if file.OmittedReason != "" {
			omitted++
		}
	}
	if included != 1 || omitted != 1 {
		t.Fatalf("limit result = %#v", snapshot.Untracked)
	}
	if _, err := Capture(context.Background(), repo, []string{"../outside"}); !errors.Is(err, domain.ErrUnsafePath) {
		t.Fatalf("unsafe path error = %v", err)
	}
	if _, err := Capture(context.Background(), repo, []string{"tracked.txt"}); !errors.Is(err, domain.ErrNotUntracked) {
		t.Fatalf("tracked selection error = %v", err)
	}
}

func TestUnbornRepositoryIsReadOnlyForManagedWorktree(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "unborn")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "draft.txt"), []byte("draft"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Capture(context.Background(), repo, []string{"draft.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Unborn || snapshot.Branch != "main" {
		t.Fatalf("unborn snapshot = %#v", snapshot)
	}
	if err := CreateWorktree(context.Background(), snapshot, filepath.Join(filepath.Dir(repo), "worktree")); !errors.Is(err, domain.ErrUnbornRepository) {
		t.Fatalf("CreateWorktree error = %v", err)
	}
}

func TestPersistAndLoadSnapshotUsesObjectReferences(t *testing.T) {
	ctx := context.Background()
	original := Snapshot{
		RepoRoot:      "/source/repo",
		Head:          "0123456789abcdef",
		Branch:        "main",
		Status:        []byte("? note.txt\x00"),
		StagedPatch:   []byte("staged"),
		UnstagedPatch: []byte("unstaged"),
		Untracked: []UntrackedFile{{
			Path: "note.txt", Size: 4, Mode: 0o640, SHA256: "digest",
			Content: []byte("note"), Included: true,
		}, {
			Path: "huge.log", Size: 30 << 20, Mode: 0o600,
			OmittedReason: "file_limit",
		}},
	}
	objects := memoryObjects{}
	stored, err := original.PersistObjects(ctx, objects, "thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if stored.StatusObject == "" || stored.UntrackedManifestObject == "" {
		t.Fatalf("stored refs = %#v", stored)
	}
	restored, err := LoadSnapshot(ctx, objects, stored, original.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored.Status, original.Status) ||
		!bytes.Equal(restored.StagedPatch, original.StagedPatch) ||
		!bytes.Equal(restored.Untracked[0].Content, original.Untracked[0].Content) {
		t.Fatalf("restored snapshot = %#v", restored)
	}
	if restored.Untracked[1].Included || restored.Untracked[1].OmittedReason != "file_limit" {
		t.Fatalf("omitted metadata lost: %#v", restored.Untracked[1])
	}
}
