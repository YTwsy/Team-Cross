package gitstate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"teamcross/internal/domain"
)

// CreateWorktree creates a detached worktree from Snapshot.Head and applies the
// captured index/worktree state there. It never checks out or applies anything
// in Snapshot.RepoRoot.
func CreateWorktree(ctx context.Context, snapshot Snapshot, destination string) (err error) {
	if snapshot.Unborn || snapshot.Head == "" {
		return domain.ErrUnbornRepository
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("resolve worktree path: %w", err)
	}
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("worktree destination already exists: %s", destination)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect worktree destination: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return fmt.Errorf("create worktree parent: %w", err)
	}
	if _, err := gitOutput(ctx, snapshot.RepoRoot, nil, "worktree", "add", "--detach", destination, snapshot.Head); err != nil {
		return fmt.Errorf("create isolated worktree: %w", err)
	}
	created := true
	defer func() {
		if err == nil || !created {
			return
		}
		_, _ = gitOutput(context.Background(), snapshot.RepoRoot, nil, "worktree", "remove", "--force", destination)
		_ = os.RemoveAll(destination)
	}()
	if len(snapshot.StagedPatch) > 0 {
		if _, err := gitOutput(ctx, destination, snapshot.StagedPatch,
			"apply", "--binary", "--index", "--whitespace=nowarn", "-"); err != nil {
			return fmt.Errorf("apply staged patch in worktree: %w", err)
		}
	}
	if len(snapshot.UnstagedPatch) > 0 {
		if _, err := gitOutput(ctx, destination, snapshot.UnstagedPatch,
			"apply", "--binary", "--whitespace=nowarn", "-"); err != nil {
			return fmt.Errorf("apply unstaged patch in worktree: %w", err)
		}
	}
	for _, file := range snapshot.Untracked {
		if !file.Included {
			continue
		}
		rel, normalizeErr := normalizeSelectedPath(destination, file.Path)
		if normalizeErr != nil {
			return normalizeErr
		}
		path := filepath.Join(destination, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return fmt.Errorf("create untracked parent %s: %w", rel, err)
		}
		if file.Symlink {
			if err := os.Symlink(string(file.Content), path); err != nil {
				return fmt.Errorf("restore untracked symlink %s: %w", rel, err)
			}
			continue
		}
		mode := file.Mode.Perm()
		if mode == 0 {
			mode = 0o600
		}
		if err := os.WriteFile(path, file.Content, mode); err != nil {
			return fmt.Errorf("restore untracked file %s: %w", rel, err)
		}
	}
	created = false
	return nil
}

// ExportBinaryPatch returns the entire tracked diff from baseline plus binary
// patches for untracked regular files. The output is suitable for git apply.
func ExportBinaryPatch(ctx context.Context, worktree, baseline string) ([]byte, error) {
	if baseline == "" {
		return nil, domain.ErrUnbornRepository
	}
	tracked, err := gitOutput(ctx, worktree, nil,
		"diff", "--binary", "--full-index", "--no-ext-diff", "--no-textconv", baseline, "--")
	if err != nil {
		return nil, fmt.Errorf("export tracked patch: %w", err)
	}
	untracked, err := gitOutput(ctx, worktree, nil, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("list untracked files: %w", err)
	}
	var patch bytes.Buffer
	patch.Write(tracked)
	for _, raw := range bytes.Split(untracked, []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		rel := string(raw)
		piece, diffErr := gitOutput(ctx, worktree, nil,
			"diff", "--no-index", "--binary", "--full-index", "--no-ext-diff", "--no-textconv", "--", "/dev/null", rel)
		if diffErr != nil && !isExitCode(diffErr, 1) {
			return nil, fmt.Errorf("export untracked file %s: %w", rel, diffErr)
		}
		if patch.Len() > 0 && !strings.HasSuffix(patch.String(), "\n") {
			patch.WriteByte('\n')
		}
		patch.Write(piece)
	}
	return patch.Bytes(), nil
}
