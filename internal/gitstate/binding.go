package gitstate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// VerifyWorktreeBinding rejects a replaced execution root or .git pointer that
// would cause later Agent Git operations to use the original checkout's index.
func VerifyWorktreeBinding(ctx context.Context, worktree, repository string) error {
	info, err := os.Lstat(worktree)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("execution root is not a real isolated directory")
	}
	marker := filepath.Join(worktree, ".git")
	info, err = os.Lstat(marker)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("isolated worktree Git marker was replaced")
	}
	gitDir, err := gitOutput(ctx, worktree, nil, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return err
	}
	common, err := gitOutput(ctx, worktree, nil, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	repoCommon, err := gitOutput(ctx, repository, nil, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	canonical := func(value string) (string, error) { return filepath.EvalSymlinks(strings.TrimSpace(value)) }
	gitPath, err := canonical(string(gitDir))
	if err != nil {
		return err
	}
	commonPath, err := canonical(string(common))
	if err != nil {
		return err
	}
	repoPath, err := canonical(string(repoCommon))
	if err != nil {
		return err
	}
	if gitPath == commonPath || commonPath != repoPath {
		return errors.New("execution root is no longer bound to the registered isolated worktree")
	}
	backlink, err := os.ReadFile(filepath.Join(gitPath, "gitdir"))
	if err != nil {
		return err
	}
	backlinkPath, err := canonical(string(backlink))
	if err != nil {
		return err
	}
	markerPath, err := canonical(marker)
	if err != nil {
		return err
	}
	if backlinkPath != markerPath {
		return errors.New("isolated Git worktree points to another checkout")
	}
	return nil
}
