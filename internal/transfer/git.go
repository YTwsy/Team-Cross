package transfer

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Git ignores inherited Git routing/configuration, hooks, filters configured in
// global files, and object alternates. The receiving repository is always new.
func git(ctx context.Context, repo string, input []byte, limit int, args ...string) ([]byte, error) {
	commandArgs := []string{"--no-pager", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "submodule.recurse=false", "-C", repo}
	commandArgs = append(commandArgs, args...)
	cmd := exec.CommandContext(ctx, "git", commandArgs...)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "GIT_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_NO_REPLACE_OBJECTS=1")
	cmd.Stdin = bytes.NewReader(input)
	stdout := &limitedBuffer{remaining: limit}
	stderr := &limitedBuffer{remaining: 64 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, stderr.String())
	}
	return stdout.Bytes(), nil
}

type limitedBuffer struct {
	bytes.Buffer
	remaining int
}

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	if len(data) > buffer.remaining {
		return 0, errors.New("Git output exceeds transfer limit")
	}
	buffer.remaining -= len(data)
	return buffer.Buffer.Write(data)
}

// CaptureBaseline exports the complete reachable object closure, not a thin
// pack or repository URL requiring the sender to remain online.
func CaptureBaseline(ctx context.Context, repo, baseline string) (string, []GitObject, error) {
	format, err := git(ctx, repo, nil, 128, "rev-parse", "--show-object-format")
	if err != nil {
		return "", nil, err
	}
	objectFormat := strings.TrimSpace(string(format))
	if objectFormat != "sha1" && objectFormat != "sha256" {
		return "", nil, errors.New("unsupported Git object format")
	}
	if (objectFormat == "sha1" && len(baseline) != 40) || (objectFormat == "sha256" && len(baseline) != 64) {
		return "", nil, errors.New("baseline must be a complete object ID")
	}
	if _, err := strconv.ParseUint(baseline[:16], 16, 64); err != nil {
		return "", nil, errors.New("invalid baseline object ID")
	}
	ids, err := git(ctx, repo, nil, MaxObjects*66, "rev-list", "--objects", "--no-object-names", baseline, "--")
	if err != nil {
		return "", nil, err
	}
	if len(strings.Fields(string(ids))) > MaxObjects {
		return "", nil, errors.New("baseline history exceeds 10000 object limit")
	}
	data, err := git(ctx, repo, ids, MaxTotalBytes+MaxObjects*128, "cat-file", "--batch")
	if err != nil {
		return "", nil, err
	}
	reader := bufio.NewReader(bytes.NewReader(data))
	objects := make([]GitObject, 0)
	total := 0
	for {
		header, err := reader.ReadString('\n')
		if errors.Is(err, io.EOF) && header == "" {
			break
		}
		if err != nil {
			return "", nil, fmt.Errorf("read Git object header: %w", err)
		}
		fields := strings.Fields(header)
		if len(fields) != 3 {
			return "", nil, errors.New("invalid Git batch header")
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil || size < 0 || size > MaxObjectBytes || total+size > MaxTotalBytes {
			return "", nil, errors.New("baseline history exceeds content size limits")
		}
		object := GitObject{ID: fields[0], Type: fields[1], Data: make([]byte, size)}
		if _, err := io.ReadFull(reader, object.Data); err != nil {
			return "", nil, err
		}
		if terminator, err := reader.ReadByte(); err != nil || terminator != '\n' {
			return "", nil, errors.New("invalid Git batch terminator")
		}
		objects = append(objects, object)
		total += size
	}
	return objectFormat, objects, nil
}

type Materialized struct {
	Root     string
	Repo     string
	Worktree string
}

// Cleanup only removes the freshly allocated, application-owned staging tree.
func (value Materialized) Cleanup() error {
	if value.Root == "" || filepath.Base(value.Root) == "." || !strings.HasPrefix(filepath.Base(value.Root), "fork-") {
		return errors.New("refusing cleanup of an unowned materialization path")
	}
	return os.RemoveAll(value.Root)
}

// Materialize verifies objects and restores the sealed state in a fresh,
// isolated repository. It never uses an existing local checkout or credentials.
func Materialize(ctx context.Context, bundle Bundle, parent string) (result Materialized, err error) {
	if err := bundle.Validate(); err != nil {
		return result, err
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return result, err
	}
	root, err := os.MkdirTemp(parent, "fork-")
	if err != nil {
		return result, err
	}
	result = Materialized{Root: root, Repo: filepath.Join(root, "repository.git"), Worktree: filepath.Join(root, "worktree")}
	defer func() {
		if err != nil {
			_ = result.Cleanup()
		}
	}()
	if _, err = git(ctx, root, nil, 64<<10, "init", "--bare", "--template=", "--object-format="+bundle.ObjectFormat, result.Repo); err != nil {
		return result, err
	}
	for _, object := range bundle.GitObjects {
		var written []byte
		written, err = git(ctx, result.Repo, object.Data, 256, "hash-object", "-w", "-t", object.Type, "--stdin")
		if err != nil {
			return result, err
		}
		if strings.TrimSpace(string(written)) != object.ID {
			return result, errors.New("Git rejected object identity")
		}
	}
	if _, err = git(ctx, result.Repo, nil, 64<<10, "fsck", "--strict", "--no-reflogs", bundle.Baseline); err != nil {
		return result, fmt.Errorf("baseline object closure is invalid: %w", err)
	}
	var closure []byte
	closure, err = git(ctx, result.Repo, nil, MaxObjects*66, "rev-list", "--objects", "--no-object-names", bundle.Baseline, "--")
	if err != nil {
		return result, err
	}
	if len(strings.Fields(string(closure))) != len(bundle.GitObjects) {
		return result, errors.New("bundle contains Git objects outside baseline history")
	}
	if err = validateBaselineTree(ctx, result.Repo, bundle.Baseline); err != nil {
		return result, err
	}
	// Apply only to the fresh bare repository's index first. This validates the
	// complete final tree (including names, link targets, gitlinks and aggregate
	// checkout size) before any patch can write a worktree on a normalizing FS.
	if _, err = git(ctx, result.Repo, nil, 64<<10, "read-tree", bundle.Baseline); err != nil {
		return result, err
	}
	for _, patch := range [][]byte{bundle.Snapshot.StagedPatch, bundle.Snapshot.UnstagedPatch} {
		if len(patch) == 0 {
			continue
		}
		if _, err = git(ctx, result.Repo, patch, 64<<10, "apply", "--cached", "--binary", "--whitespace=nowarn", "-"); err != nil {
			return result, fmt.Errorf("validate sealed patch index: %w", err)
		}
		var intermediateTree []byte
		intermediateTree, err = git(ctx, result.Repo, nil, 128, "write-tree")
		if err != nil {
			return result, err
		}
		if err = validateBaselineTree(ctx, result.Repo, strings.TrimSpace(string(intermediateTree))); err != nil {
			return result, err
		}
	}
	if _, err = git(ctx, result.Repo, nil, 64<<10, "worktree", "add", "--detach", result.Worktree, bundle.Baseline); err != nil {
		return result, err
	}
	for index, patch := range [][]byte{bundle.Snapshot.StagedPatch, bundle.Snapshot.UnstagedPatch} {
		if len(patch) == 0 {
			continue
		}
		args := []string{"apply", "--binary", "--whitespace=nowarn"}
		if index == 0 {
			args = append(args, "--index")
		}
		args = append(args, "-")
		if _, err = git(ctx, result.Worktree, patch, 64<<10, args...); err != nil {
			return result, fmt.Errorf("restore sealed patch: %w", err)
		}
		if err = AuditTree(result.Worktree); err != nil {
			return result, err
		}
	}
	for _, file := range bundle.Snapshot.Untracked {
		if !file.Included {
			continue
		}
		if err = validateAdditionPath(result.Worktree, file.Path); err != nil {
			return result, err
		}
		if err = safeParents(result.Worktree, file.Path); err != nil {
			return result, err
		}
		filename := filepath.Join(result.Worktree, filepath.FromSlash(file.Path))
		if file.Symlink {
			err = os.Symlink(string(file.Content), filename)
		} else {
			var output *os.File
			mode := file.Mode.Perm()
			if mode == 0 {
				mode = 0o600
			}
			output, err = os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
			if err == nil {
				_, err = output.Write(file.Content)
				closeErr := output.Close()
				if err == nil {
					err = closeErr
				}
			}
		}
		if err != nil {
			return result, fmt.Errorf("restore untracked file: %w", err)
		}
	}
	if err = AuditTree(result.Worktree); err != nil {
		return result, err
	}
	return result, nil
}

func validateBaselineTree(ctx context.Context, repo, baseline string) error {
	data, err := git(ctx, repo, nil, 8<<20, "ls-tree", "-r", "--long", "-z", baseline)
	if err != nil {
		return err
	}
	paths := map[string]string{}
	count := 0
	var checkoutBytes int64
	for _, entry := range bytes.Split(data, []byte{0}) {
		if len(entry) == 0 {
			continue
		}
		count++
		if count > MaxObjects {
			return errors.New("baseline tree exceeds file count limit")
		}
		metadata, filename, ok := strings.Cut(string(entry), "\t")
		parts := strings.Fields(metadata)
		if !ok || len(parts) != 4 {
			return errors.New("invalid baseline tree entry")
		}
		if err := ValidatePath(filename); err != nil {
			return err
		}
		if err := registerPortablePath(paths, filename, false); err != nil {
			return err
		}
		if parts[0] == "160000" {
			return errors.New("offline fork does not support Git submodule dependencies")
		}
		size, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil || size < 0 || size > MaxObjectBytes {
			return errors.New("baseline file exceeds content limit")
		}
		checkoutBytes += size
		if checkoutBytes > MaxTotalBytes {
			return errors.New("baseline checkout expands beyond 64 MiB limit")
		}
		if parts[0] == "120000" {
			target, err := git(ctx, repo, nil, 64<<10, "cat-file", "blob", parts[2])
			if err != nil {
				return err
			}
			if err := ValidateSymlink(filename, string(target)); err != nil {
				return err
			}
		}
	}
	return nil
}

func safeParents(root, name string) error {
	if err := ValidatePath(name); err != nil {
		return err
	}
	current := root
	parts := strings.Split(name, "/")
	for _, component := range parts[:len(parts)-1] {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err := os.Mkdir(current, 0o700); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("untracked path traverses a non-directory or symlink")
		}
	}
	return nil
}

// AuditTree does not follow symlinks. Safe local links remain usable; escaping
// links and unsupported filesystem entries fail closed before Agent execution.
func AuditTree(root string) error {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("isolated execution root must be a real directory, not a symlink")
	}
	paths := map[string]string{}
	var totalBytes int64
	count := 0
	return filepath.WalkDir(root, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filename == root {
			return nil
		}
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if err := ValidatePath(relative); err != nil {
			return err
		}
		if err := registerPortablePath(paths, relative, entry.IsDir()); err != nil {
			return err
		}
		count++
		if count > MaxObjects {
			return errors.New("materialized snapshot exceeds file count limit")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			totalBytes += info.Size()
			if info.Size() > MaxObjectBytes || totalBytes > MaxTotalBytes {
				return errors.New("materialized snapshot exceeds content limit")
			}
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(filename)
			if err != nil {
				return err
			}
			return ValidateSymlink(relative, target)
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			return errors.New("unsupported file type in isolated snapshot")
		}
		return nil
	})
}

func validateAdditionPath(root, name string) error {
	paths := map[string]string{}
	if err := filepath.WalkDir(root, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filename == root {
			return nil
		}
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			return err
		}
		if relative == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		return registerPortablePath(paths, filepath.ToSlash(relative), entry.IsDir())
	}); err != nil {
		return err
	}
	return registerPortablePath(paths, name, false)
}
