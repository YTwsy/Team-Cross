package gitstate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"teamcross/internal/domain"
)

const (
	DefaultMaxUntrackedFileBytes  int64 = 5 << 20
	DefaultMaxUntrackedTotalBytes int64 = 20 << 20
)

type CaptureOptions struct {
	MaxUntrackedFileBytes  int64
	MaxUntrackedTotalBytes int64
}

type Snapshot struct {
	RepoRoot      string          `json:"repoRoot"`
	Head          string          `json:"head,omitempty"`
	Branch        string          `json:"branch,omitempty"`
	Unborn        bool            `json:"unborn"`
	Status        []byte          `json:"status"`
	StagedPatch   []byte          `json:"stagedPatch"`
	UnstagedPatch []byte          `json:"unstagedPatch"`
	Untracked     []UntrackedFile `json:"untracked"`
}

type UntrackedFile struct {
	Path          string      `json:"path"`
	Size          int64       `json:"size"`
	Mode          os.FileMode `json:"mode"`
	SHA256        string      `json:"sha256,omitempty"`
	Content       []byte      `json:"content,omitempty"`
	Included      bool        `json:"included"`
	OmittedReason string      `json:"omittedReason,omitempty"`
	Symlink       bool        `json:"symlink,omitempty"`
}

func Capture(ctx context.Context, repo string, selectedUntracked []string) (Snapshot, error) {
	return CaptureWithOptions(ctx, repo, selectedUntracked, CaptureOptions{})
}

func CaptureWithOptions(ctx context.Context, repo string, selectedUntracked []string, opts CaptureOptions) (Snapshot, error) {
	if opts.MaxUntrackedFileBytes <= 0 {
		opts.MaxUntrackedFileBytes = DefaultMaxUntrackedFileBytes
	}
	if opts.MaxUntrackedTotalBytes <= 0 {
		opts.MaxUntrackedTotalBytes = DefaultMaxUntrackedTotalBytes
	}
	rootBytes, err := gitOutput(ctx, repo, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return Snapshot{}, fmt.Errorf("resolve git repository: %w", err)
	}
	root := strings.TrimSpace(string(rootBytes))
	root, err = filepath.Abs(root)
	if err != nil {
		return Snapshot{}, fmt.Errorf("resolve repository path: %w", err)
	}
	snapshot := Snapshot{RepoRoot: root}
	head, err := gitOutput(ctx, root, nil, "rev-parse", "--verify", "HEAD")
	if err != nil {
		snapshot.Unborn = true
	} else {
		snapshot.Head = strings.TrimSpace(string(head))
	}
	branch, branchErr := gitOutput(ctx, root, nil, "symbolic-ref", "--quiet", "--short", "HEAD")
	if branchErr == nil {
		snapshot.Branch = strings.TrimSpace(string(branch))
	}
	if snapshot.Branch == "" && !snapshot.Unborn {
		if name, nameErr := gitOutput(ctx, root, nil, "rev-parse", "--abbrev-ref", "HEAD"); nameErr == nil {
			snapshot.Branch = strings.TrimSpace(string(name))
		}
	}
	snapshot.Status, err = gitOutput(ctx, root, nil, "status", "--porcelain=v2", "-z", "--untracked-files=all")
	if err != nil {
		return Snapshot{}, fmt.Errorf("capture status: %w", err)
	}
	snapshot.StagedPatch, err = gitOutput(ctx, root, nil, "diff", "--cached", "--binary", "--full-index", "--no-ext-diff", "--no-textconv", "--")
	if err != nil {
		return Snapshot{}, fmt.Errorf("capture staged diff: %w", err)
	}
	snapshot.UnstagedPatch, err = gitOutput(ctx, root, nil, "diff", "--binary", "--full-index", "--no-ext-diff", "--no-textconv", "--")
	if err != nil {
		return Snapshot{}, fmt.Errorf("capture unstaged diff: %w", err)
	}
	untracked := parseUntracked(snapshot.Status)
	selected := make(map[string]struct{}, len(selectedUntracked))
	for _, raw := range selectedUntracked {
		rel, normalizeErr := normalizeSelectedPath(root, raw)
		if normalizeErr != nil {
			return Snapshot{}, normalizeErr
		}
		if _, ok := untracked[rel]; !ok {
			return Snapshot{}, fmt.Errorf("%w: %s", domain.ErrNotUntracked, rel)
		}
		selected[rel] = struct{}{}
	}
	paths := make([]string, 0, len(selected))
	for path := range selected {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var includedTotal int64
	for _, path := range paths {
		file, fileErr := captureUntracked(root, path, opts, includedTotal)
		if fileErr != nil {
			return Snapshot{}, fileErr
		}
		if file.Included {
			includedTotal += file.Size
		}
		snapshot.Untracked = append(snapshot.Untracked, file)
	}
	return snapshot, nil
}

func parseUntracked(status []byte) map[string]struct{} {
	result := make(map[string]struct{})
	for _, record := range bytes.Split(status, []byte{0}) {
		if bytes.HasPrefix(record, []byte("? ")) {
			result[string(record[2:])] = struct{}{}
		}
	}
	return result
}

func normalizeSelectedPath(root, raw string) (string, error) {
	if raw == "" {
		return "", domain.ErrUnsafePath
	}
	path := raw
	if filepath.IsAbs(path) {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return "", fmt.Errorf("%w: %s", domain.ErrUnsafePath, raw)
		}
		path = rel
	}
	path = filepath.Clean(filepath.FromSlash(path))
	if path == "." || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) || filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: %s", domain.ErrUnsafePath, raw)
	}
	abs := filepath.Join(root, path)
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %s", domain.ErrUnsafePath, raw)
	}
	return filepath.ToSlash(path), nil
}

func captureUntracked(root, path string, opts CaptureOptions, currentTotal int64) (UntrackedFile, error) {
	abs := filepath.Join(root, filepath.FromSlash(path))
	info, err := os.Lstat(abs)
	if err != nil {
		return UntrackedFile{}, fmt.Errorf("stat untracked file %s: %w", path, err)
	}
	file := UntrackedFile{Path: path, Size: info.Size(), Mode: info.Mode()}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(abs)
		if err != nil {
			return UntrackedFile{}, fmt.Errorf("read untracked symlink %s: %w", path, err)
		}
		file.Symlink = true
		file.Size = int64(len(target))
		if file.Size > opts.MaxUntrackedFileBytes {
			file.OmittedReason = "file_limit"
			return file, nil
		}
		if currentTotal+file.Size > opts.MaxUntrackedTotalBytes {
			file.OmittedReason = "round_limit"
			return file, nil
		}
		file.Content = []byte(target)
	} else if !info.Mode().IsRegular() {
		file.OmittedReason = "unsupported_file_type"
		return file, nil
	} else {
		if file.Size > opts.MaxUntrackedFileBytes {
			file.OmittedReason = "file_limit"
			return file, nil
		}
		if currentTotal+file.Size > opts.MaxUntrackedTotalBytes {
			file.OmittedReason = "round_limit"
			return file, nil
		}
		input, openErr := os.Open(abs)
		if openErr != nil {
			return UntrackedFile{}, fmt.Errorf("open untracked file %s: %w", path, openErr)
		}
		file.Content, err = io.ReadAll(io.LimitReader(input, opts.MaxUntrackedFileBytes+1))
		closeErr := input.Close()
		if err != nil {
			return UntrackedFile{}, fmt.Errorf("read untracked file %s: %w", path, err)
		}
		if closeErr != nil {
			return UntrackedFile{}, fmt.Errorf("close untracked file %s: %w", path, closeErr)
		}
		file.Size = int64(len(file.Content))
		if file.Size > opts.MaxUntrackedFileBytes {
			file.Content = nil
			file.OmittedReason = "file_limit"
			return file, nil
		}
		if currentTotal+file.Size > opts.MaxUntrackedTotalBytes {
			file.Content = nil
			file.OmittedReason = "round_limit"
			return file, nil
		}
	}
	sum := sha256.Sum256(file.Content)
	file.SHA256 = hex.EncodeToString(sum[:])
	file.Included = true
	return file, nil
}

type gitCommandError struct {
	Args   []string
	Output string
	Err    error
}

func (e *gitCommandError) Error() string {
	message := strings.TrimSpace(e.Output)
	if message == "" {
		return fmt.Sprintf("git %s: %v", strings.Join(e.Args, " "), e.Err)
	}
	return fmt.Sprintf("git %s: %v: %s", strings.Join(e.Args, " "), e.Err, message)
}

func (e *gitCommandError) Unwrap() error { return e.Err }

func gitOutput(ctx context.Context, repo string, stdin []byte, args ...string) ([]byte, error) {
	filterArgs, err := disabledFilterArgs(ctx, repo)
	if err != nil {
		return nil, err
	}
	cmd := isolatedGitCommand(ctx, repo, append(filterArgs, args...)...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		return stdout.Bytes(), &gitCommandError{Args: args, Output: stderr.String(), Err: err}
	}
	return stdout.Bytes(), nil
}

// Core Git operations consume literal code bytes and must not execute commands
// selected by an imported .gitattributes file or inherited host configuration.
func isolatedGitCommand(ctx context.Context, repo string, args ...string) *exec.Cmd {
	commandArgs := []string{"--no-pager", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "submodule.recurse=false", "-c", "core.attributesFile=/dev/null", "-C", repo}
	commandArgs = append(commandArgs, args...)
	cmd := exec.CommandContext(ctx, "git", commandArgs...)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "GIT_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_ATTR_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_NO_REPLACE_OBJECTS=1")
	return cmd
}

func disabledFilterArgs(ctx context.Context, repo string) ([]string, error) {
	command := isolatedGitCommand(ctx, repo, "config", "--includes", "--null", "--name-only", "--get-regexp", `^filter\..*\.(clean|smudge|process|required)$`)
	keys, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("inspect Git content filters: %w", err)
	}
	args := []string{}
	seen := map[string]bool{}
	for _, key := range bytes.Split(keys, []byte{0}) {
		if len(key) == 0 {
			continue
		}
		name := string(key)
		index := strings.LastIndexByte(name, '.')
		if index < len("filter.") {
			return nil, errors.New("invalid Git filter configuration")
		}
		driver := name[:index]
		if seen[driver] {
			continue
		}
		seen[driver] = true
		for _, suffix := range []string{"clean=", "smudge=", "process=", "required=false"} {
			args = append(args, "-c", driver+"."+suffix)
		}
	}
	return args, nil
}

func isExitCode(err error, code int) bool {
	var commandErr *gitCommandError
	if !errors.As(err, &commandErr) {
		return false
	}
	var exitErr *exec.ExitError
	return errors.As(commandErr.Err, &exitErr) && exitErr.ExitCode() == code
}
