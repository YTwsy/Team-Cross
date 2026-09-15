// Package cliinstall manages the single terminal launcher installed by the App.
// It never starts a Core, edits shell configuration, or replaces package-manager files.
package cliinstall

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"teamcross/internal/problem"
)

const DefaultDir = "/usr/local/bin"
const marker = "#!/bin/sh\n# Team Cross CLI launcher v1\n# source: "

type Status struct {
	Executable string `json:"executable"`
	Target     string `json:"target"`
	Command    string `json:"command,omitempty"`
	Source     string `json:"source"`
	Installed  bool   `json:"installed"`
	CanInstall bool   `json:"canInstall"`
	CanRemove  bool   `json:"canRemove"`
	Conflict   string `json:"conflict,omitempty"`
	PathReady  bool   `json:"pathReady"`
}

func resolved(path string) string {
	if p, err := filepath.EvalSymlinks(path); err == nil {
		return p
	}
	return filepath.Clean(path)
}

func appExecutable(path string) bool {
	return strings.HasSuffix(path, ".app/Contents/Resources/teamcross")
}

func launcher(source string) []byte {
	metadata, _ := json.Marshal(source)
	quoted := "'" + strings.ReplaceAll(source, "'", "'\"'\"'") + "'"
	return []byte(marker + string(metadata) + "\nexec " + quoted + " \"$@\"\n")
}

// Only a complete, unchanged launcher is ours. A marker alone is not ownership.
func owned(path string) (string, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 32<<10 {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.HasPrefix(data, []byte(marker)) {
		return "", false
	}
	line, _, ok := strings.Cut(string(data[len(marker):]), "\n")
	var source string
	if !ok || json.Unmarshal([]byte(line), &source) != nil || !filepath.IsAbs(source) || !appExecutable(source) {
		return "", false
	}
	return source, bytes.Equal(data, launcher(source))
}

func Inspect(executable, directory, searchPath string) Status {
	s := Status{Executable: resolved(executable), Target: filepath.Join(directory, "teamcross"), Source: "unavailable"}
	_, s.CanRemove = owned(s.Target)
	if source, ours := owned(s.Target); ours {
		s.Installed = resolved(source) == s.Executable
	}
	s.CanInstall = appExecutable(s.Executable)
	if _, err := os.Lstat(s.Target); err == nil && !s.CanRemove {
		s.Conflict = s.Target
		s.CanInstall = false
	}
	dirs := filepath.SplitList(searchPath)
	for _, dir := range dirs {
		if filepath.IsAbs(dir) && resolved(dir) == resolved(directory) {
			s.PathReady = true
		}
	}
	// Finder does not inherit a terminal's Homebrew PATH. Still detect those owners.
	if filepath.Clean(directory) == DefaultDir {
		dirs = append(dirs, "/opt/homebrew/bin", DefaultDir)
	}
	seen := map[string]bool{}
	for _, dir := range dirs {
		if !filepath.IsAbs(dir) || seen[dir] {
			continue
		}
		seen[dir] = true
		path := filepath.Join(dir, "teamcross")
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
			continue
		}
		source := resolved(path)
		if from, ours := owned(path); ours {
			source = resolved(from)
		}
		if s.Command == "" {
			s.Command = path
			s.Source = "standalone"
			if strings.Contains(source, "/Cellar/teamcross/") {
				s.Source = "formula"
			} else if appExecutable(source) {
				s.Source = "app"
			}
		}
		if path != s.Target {
			// A Cask link is already a valid App entry, and remains owned by Homebrew.
			s.CanInstall = false
			if source != s.Executable {
				s.Conflict = path
			}
		}
	}
	return s
}

func change(executable, directory, searchPath string, remove bool) (Status, error) {
	executable = resolved(executable)
	directory = filepath.Clean(directory)
	s := Inspect(executable, directory, searchPath)
	if !filepath.IsAbs(directory) {
		return s, fmt.Errorf("命令目录必须是绝对路径")
	}
	if !appExecutable(executable) {
		return s, problem.New("cli_requires_app", "此操作需要使用 App 内的 Team Cross", "请从菜单栏的“命令行工具…”操作；Formula 由 Homebrew 管理")
	}
	if !remove {
		info, err := os.Stat(executable)
		if err != nil {
			return s, err
		}
		if !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
			return s, fmt.Errorf("App 内的命令不可执行")
		}
		if strings.HasPrefix(executable, "/Volumes/") || strings.Contains(executable, "/AppTranslocation/") {
			return s, problem.New("cli_app_not_installed", "请先把 App 安装到应用程序目录", "关闭安装镜像中的 App，从应用程序目录重新打开")
		}
	}
	if !remove && !s.CanInstall && s.Conflict == "" && s.Command != "" {
		// A matching Cask command is already a valid entry and remains owned by
		// Homebrew; no target-directory lock is needed when nothing changes.
		return s, nil
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return s, permission(err)
	}
	// Keep the lock file: unlinking it would let concurrent installers lock different inodes.
	lock, err := os.OpenFile(filepath.Join(directory, ".teamcross-cli.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return s, permission(err)
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return s, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	// Inspect again while holding the lifecycle lock. The initial status is useful
	// for validation errors, but another installer can atomically create or replace
	// the launcher before this goroutine acquires the lock.
	s = Inspect(executable, directory, searchPath)
	if remove {
		if _, err := os.Lstat(s.Target); errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
	} else if !s.CanInstall {
		if s.Conflict == "" && s.Command != "" {
			return s, nil
		}
		return s, conflict(s.Conflict)
	}
	_, ours := owned(s.Target)
	_, statErr := os.Lstat(s.Target)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return s, permission(statErr)
	}
	if statErr == nil && !ours {
		return s, conflict(s.Target)
	}
	if remove {
		if ours {
			err = os.Remove(s.Target)
		}
	} else {
		var temp *os.File
		temp, err = os.CreateTemp(directory, ".teamcross-cli-")
		if err != nil {
			return s, permission(err)
		}
		name := temp.Name()
		defer os.Remove(name)
		if _, err = temp.Write(launcher(executable)); err == nil {
			err = temp.Chmod(0755)
		}
		if err == nil {
			err = temp.Sync()
		}
		closeErr := temp.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			if ours {
				err = os.Rename(name, s.Target)
			} else {
				err = os.Link(name, s.Target)
			}
		}
	}
	if err != nil {
		return s, permission(err)
	}
	return Inspect(executable, directory, searchPath), nil
}

func conflict(path string) error {
	return problem.New("cli_conflict", "已有命令由其他安装管理："+path, "请先通过原安装渠道移除或切换；Team Cross 不覆盖现有文件")
}
func permission(err error) error {
	if errors.Is(err, os.ErrPermission) {
		return problem.New("cli_permission_denied", "安装命令入口需要系统授权", "请从菜单栏“命令行工具…”完成授权；服务仍以普通用户身份运行")
	}
	return err
}
func Install(executable, directory, searchPath string) (Status, error) {
	return change(executable, directory, searchPath, false)
}
func Uninstall(executable, directory, searchPath string) (Status, error) {
	return change(executable, directory, searchPath, true)
}
