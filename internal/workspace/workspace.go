// Package workspace prepares native Git workspaces without copying dirty state.
package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Preview struct {
	Mode         string `json:"workspaceMode"`
	Repo         string `json:"repo"`
	SourceCwd    string `json:"sourceCwd"`
	ExecutionCwd string `json:"executionCwd"`
	Head         string `json:"head"`
	Branch       string `json:"branch"`
	Dirty        bool   `json:"dirty"`
	RelativeCwd  string `json:"-"`
}

func Git(ctx context.Context, cwd string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false"}, args...)...)
	cmd.Dir = cwd
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GIT_") {
			cmd.Env = append(cmd.Env, e)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return out, nil
}
func Inspect(ctx context.Context, cwd, mode string) (Preview, error) {
	p := Preview{Mode: mode}
	if mode != "existing" && mode != "worktree" {
		return p, fmt.Errorf("请选择原目录或新 worktree")
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return p, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return p, err
	}
	p.SourceCwd = abs
	p.ExecutionCwd = abs
	root, err := Git(ctx, abs, "rev-parse", "--show-toplevel")
	if err != nil {
		return p, fmt.Errorf("来源目录不是可用的 Git 仓库: %w", err)
	}
	p.Repo = strings.TrimSpace(string(root))
	p.RelativeCwd, err = filepath.Rel(p.Repo, abs)
	if err != nil || strings.HasPrefix(p.RelativeCwd, "..") {
		return p, fmt.Errorf("来源目录不在仓库中")
	}
	head, err := Git(ctx, abs, "rev-parse", "--verify", "HEAD")
	if err != nil && mode == "worktree" {
		return p, fmt.Errorf("创建 worktree 需要仓库已有提交")
	}
	p.Head = strings.TrimSpace(string(head))
	if err != nil {
		p.Head = ""
	}
	branch, _ := Git(ctx, abs, "symbolic-ref", "--quiet", "--short", "HEAD")
	p.Branch = strings.TrimSpace(string(branch))
	status, err := Git(ctx, abs, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return p, err
	}
	p.Dirty = len(status) > 0
	if _, err := os.Stat(filepath.Join(p.Repo, ".gitmodules")); err == nil {
		return p, fmt.Errorf("当前暂不支持含 submodule 的仓库")
	}
	return p, nil
}
func Prepare(ctx context.Context, p Preview, destination, branch string) (string, error) {
	if p.Mode == "existing" {
		return p.SourceCwd, nil
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return "", err
	}
	if _, err := Git(ctx, p.Repo, "worktree", "add", "-b", branch, destination, p.Head); err != nil {
		return "", err
	}
	cwd := filepath.Join(destination, p.RelativeCwd)
	if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
		return "", fmt.Errorf("所选提交中没有来源子目录 %s；已创建的 worktree 保留在 %s", p.RelativeCwd, destination)
	}
	return cwd, nil
}
func ReadFile(root, path string) (string, error) {
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("使用相对于协作目录的文件路径")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, path))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("文件不在协作目录中")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 2<<20 {
		return "", fmt.Errorf("仅可读取 2 MiB 以内的普通文件")
	}
	b, err := os.ReadFile(resolved)
	return string(b), err
}
