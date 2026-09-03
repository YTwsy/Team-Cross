package gitstate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"teamcross/internal/domain"
)

// ExportBinaryPatch returns the baseline-relative tracked patch and currently
// visible untracked files. Previously captured paths require the explicit
// ExportBinaryPatchWithCaptured scope; ignored files are never enumerated.
func ExportBinaryPatch(ctx context.Context, worktree, baseline string) ([]byte, error) {
	return ExportBinaryPatchWithCaptured(ctx, worktree, baseline, nil)
}

// ExportBinaryPatchWithCaptured preserves explicitly captured files even after
// ignore rules change. The caller must derive capturedPaths from an immutable
// snapshot belonging to this Thread, never from a recursive ignored-file scan.
// It reads current content only: missing files are not restored from history.
func ExportBinaryPatchWithCaptured(ctx context.Context, worktree, baseline string, capturedPaths []string) ([]byte, error) {
	if baseline == "" {
		return nil, domain.ErrUnbornRepository
	}
	paths := make(map[string]bool, len(capturedPaths))
	for _, path := range capturedPaths {
		if err := validateExportPath(path); err != nil {
			return nil, err
		}
		paths[path] = true
	}
	untracked, err := gitOutput(ctx, worktree, nil, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("list untracked files: %w", err)
	}
	for _, raw := range bytes.Split(untracked, []byte{0}) {
		if len(raw) > 0 {
			paths[string(raw)] = true
		}
	}
	indexed, err := gitOutput(ctx, worktree, nil, "ls-files", "--cached", "-z")
	if err != nil {
		return nil, fmt.Errorf("list tracked files: %w", err)
	}
	indexedPaths := map[string]bool{}
	for _, raw := range bytes.Split(indexed, []byte{0}) {
		indexedPaths[string(raw)] = true
		delete(paths, string(raw)) // Already included in the baseline-relative diff.
	}
	indexPath, restored, cleanup, err := exportBaselineIndex(ctx, worktree, baseline, indexedPaths)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	for _, path := range restored {
		delete(paths, path) // Restored only in the temporary export index.
	}
	tracked, err := exportGitWithIndex(ctx, worktree, indexPath, nil,
		"diff", "--binary", "--full-index", "--no-ext-diff", "--no-textconv", baseline, "--")
	if err != nil {
		return nil, fmt.Errorf("export tracked patch: %w", err)
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	var patch bytes.Buffer
	patch.Write(tracked)
	for _, rel := range ordered {
		present, err := inspectExportPath(worktree, rel)
		if err != nil {
			return nil, err
		}
		if !present {
			continue
		}
		piece, diffErr := gitOutput(ctx, worktree, nil,
			"diff", "--no-index", "--binary", "--full-index", "--no-ext-diff", "--no-textconv", "--", "/dev/null", rel)
		if diffErr != nil && !isExitCode(diffErr, 1) {
			return nil, fmt.Errorf("export untracked file %s: %w", rel, diffErr)
		}
		if patch.Len() > 0 && patch.Bytes()[patch.Len()-1] != '\n' {
			patch.WriteByte('\n')
		}
		patch.Write(piece)
	}
	return patch.Bytes(), nil
}

// Removing a baseline file from the index does not remove its working bytes.
// Diffing the real index would emit a delete followed by a duplicate no-index
// add (or silently lose it after ignore). A temporary index restores only those
// still-existing baseline entries. No Git objects or original index are written.
func exportBaselineIndex(ctx context.Context, worktree, baseline string, indexed map[string]bool) (string, []string, func(), error) {
	noop := func() {}
	tree, err := gitOutput(ctx, worktree, nil, "ls-tree", "-r", "-z", baseline)
	if err != nil {
		return "", nil, noop, fmt.Errorf("read export baseline paths: %w", err)
	}
	restored := []string{}
	var entries bytes.Buffer
	for _, record := range bytes.Split(tree, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		pair := bytes.SplitN(record, []byte{'\t'}, 2)
		if len(pair) != 2 {
			return "", nil, noop, fmt.Errorf("invalid baseline tree entry")
		}
		path := string(pair[1])
		if indexed[path] {
			continue
		}
		fields := strings.Fields(string(pair[0]))
		if len(fields) != 3 {
			return "", nil, noop, fmt.Errorf("invalid baseline tree metadata")
		}
		if fields[1] != "blob" {
			continue // Gitlinks are not read as ordinary captured file content.
		}
		present, err := inspectExportPath(worktree, path)
		if err != nil {
			return "", nil, noop, err
		}
		if !present {
			continue
		}
		fmt.Fprintf(&entries, "%s %s\t%s%c", fields[0], fields[2], path, byte(0))
		restored = append(restored, path)
	}
	if len(restored) == 0 {
		return "", nil, noop, nil
	}
	temporary, err := os.MkdirTemp("", "teamcross-export-index-")
	if err != nil {
		return "", nil, noop, err
	}
	cleanup := func() { _ = os.RemoveAll(temporary) }
	indexPath := filepath.Join(temporary, "index")
	originalPath, err := gitOutput(ctx, worktree, nil, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		cleanup()
		return "", nil, noop, err
	}
	data, err := os.ReadFile(strings.TrimSpace(string(originalPath)))
	if os.IsNotExist(err) {
		_, err = exportGitWithIndex(ctx, worktree, indexPath, nil, "read-tree", "--empty")
	} else if err == nil {
		err = os.WriteFile(indexPath, data, 0o600)
	}
	if err == nil {
		_, err = exportGitWithIndex(ctx, worktree, indexPath, entries.Bytes(), "update-index", "-z", "--index-info")
	}
	if err != nil {
		cleanup()
		return "", nil, noop, fmt.Errorf("prepare temporary export index: %w", err)
	}
	return indexPath, restored, cleanup, nil
}

func exportGitWithIndex(ctx context.Context, repo, indexPath string, stdin []byte, args ...string) ([]byte, error) {
	if indexPath == "" {
		return gitOutput(ctx, repo, stdin, args...)
	}
	filterArgs, err := disabledFilterArgs(ctx, repo)
	if err != nil {
		return nil, err
	}
	cmd := isolatedGitCommand(ctx, repo, append(filterArgs, args...)...)
	cmd.Env = append(cmd.Env, "GIT_INDEX_FILE="+indexPath)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), &gitCommandError{Args: args, Output: stderr.String(), Err: err}
	}
	return stdout.Bytes(), nil
}

// PatchPaths extracts exact target names through Git's NUL-delimited parser.
// --numstat only parses the patch; it does not apply it or alter the index.
func PatchPaths(ctx context.Context, repo string, patch []byte) ([]string, error) {
	paths := []string{}
	if len(bytes.TrimSpace(patch)) == 0 {
		return paths, nil
	}
	stat, err := gitOutput(ctx, repo, patch, "apply", "--numstat", "-z", "-")
	if err != nil {
		return nil, fmt.Errorf("read captured patch paths: %w", err)
	}
	for _, record := range bytes.Split(stat, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		fields := bytes.SplitN(record, []byte{'\t'}, 3)
		if len(fields) != 3 {
			return nil, fmt.Errorf("invalid captured patch path record")
		}
		path := string(fields[2])
		if err := validateExportPath(path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// ExportSnapshotPatch is for an already materialized immutable snapshot. New
// files may be represented inside patches, not only in its Untracked manifest.
func ExportSnapshotPatch(ctx context.Context, worktree string, snapshot Snapshot) ([]byte, error) {
	paths := []string{}
	for _, file := range snapshot.Untracked {
		if file.Included {
			paths = append(paths, file.Path)
		}
	}
	for _, patch := range [][]byte{snapshot.StagedPatch, snapshot.UnstagedPatch} {
		added, err := PatchPaths(ctx, worktree, patch)
		if err != nil {
			return nil, err
		}
		paths = append(paths, added...)
	}
	return ExportBinaryPatchWithCaptured(ctx, worktree, snapshot.Head, paths)
}

func validateExportPath(path string) error {
	if path == "" || filepath.IsAbs(path) || strings.ContainsAny(path, "\\\x00") {
		return fmt.Errorf("unsafe captured export path %q", path)
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." || strings.EqualFold(part, ".git") {
			return fmt.Errorf("unsafe captured export path %q", path)
		}
	}
	return nil
}

func inspectExportPath(root, path string) (bool, error) {
	if err := validateExportPath(path); err != nil {
		return false, err
	}
	parts := strings.Split(path, "/")
	current := root
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("inspect captured export path %q: %w", path, err)
		}
		if i < len(parts)-1 {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return false, fmt.Errorf("unsafe captured export parent %q", path)
			}
		} else if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			// In particular never recurse into a directory replacing a captured file.
			return false, fmt.Errorf("captured export path is not a file: %q", path)
		}
	}
	return true, nil
}
