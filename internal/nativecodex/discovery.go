package nativecodex

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"teamcross/internal/problem"
	"time"
)

// Installation describes the executable independently of saved settings.
type Installation struct {
	Binary        string `json:"binary"`
	Source        string `json:"source"`
	DesktopApp    string `json:"desktopApp,omitempty"`
	RecoveredFrom string `json:"recoveredFrom,omitempty"`
}

var bundledCLIRelatives = []string{
	"Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex",
	"Contents/Resources/codex",
}

type discoveryEnvironment struct {
	home, path, override string
	apps, common         []string
	shellPath            func() string
}

func installedApps(home string) []string {
	var apps []string
	for _, base := range []string{"/Applications", filepath.Join(home, "Applications")} {
		for _, name := range []string{"ChatGPT.app", "Codex.app"} {
			apps = append(apps, filepath.Join(base, name))
		}
	}
	return apps
}

func expandClientPath(value, home string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "~/") {
		return filepath.Join(home, value[2:])
	}
	return value
}

func executableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0
}

// BundledBinary searches known layouts only, never arbitrary app resources.
func BundledBinary(app string) string {
	if app == "" {
		return ""
	}
	for _, relative := range bundledCLIRelatives {
		path := filepath.Join(app, filepath.FromSlash(relative))
		if executableFile(path) {
			return path
		}
	}
	return ""
}

func DefaultDesktop() string {
	home, _ := os.UserHomeDir()
	for _, app := range installedApps(home) {
		if BundledBinary(app) != "" {
			return app
		}
	}
	return ""
}

func bundledApp(binary string) string {
	for _, relative := range bundledCLIRelatives {
		suffix := string(filepath.Separator) + filepath.FromSlash(relative)
		if app, ok := strings.CutSuffix(filepath.Clean(binary), suffix); ok && strings.HasSuffix(app, ".app") {
			return app
		}
	}
	return ""
}

func lookupCLI(command, path string) string {
	if strings.ContainsRune(command, filepath.Separator) {
		if executableFile(command) {
			absolute, _ := filepath.Abs(command)
			return absolute
		}
		return ""
	}
	for _, directory := range filepath.SplitList(path) {
		// Do not implicitly execute a command from the current directory.
		if !filepath.IsAbs(directory) {
			continue
		}
		candidate := filepath.Join(directory, command)
		if executableFile(candidate) {
			return candidate
		}
	}
	return ""
}

func discover(binary, desktop string, env discoveryEnvironment) (Installation, error) {
	binary, desktop = expandClientPath(binary, env.home), expandClientPath(desktop, env.home)
	source := "custom"
	if binary == "" && env.override != "" {
		binary, source = expandClientPath(env.override, env.home), "environment"
	}
	if binary != "" {
		if found := lookupCLI(binary, env.path); found != "" {
			return Installation{Binary: found, Source: source}, nil
		}
		if !strings.ContainsRune(binary, filepath.Separator) {
			if found := lookupCLI(binary, strings.Join(env.common, string(os.PathListSeparator))); found != "" {
				return Installation{Binary: found, Source: source}, nil
			}
			if env.shellPath != nil {
				if found := lookupCLI(binary, env.shellPath()); found != "" {
					return Installation{Binary: found, Source: source}, nil
				}
			}
		}
		// Recover a moved bundled CLI within the same selected installation.
		// Arbitrary custom pins and the saved settings remain untouched.
		if app := bundledApp(binary); app != "" && source == "custom" && (desktop == "" || filepath.Clean(desktop) == app) {
			if found := BundledBinary(app); found != "" {
				return Installation{Binary: found, Source: "app", DesktopApp: app, RecoveredFrom: binary}, nil
			}
		}
		message := "Codex CLI 文件不存在"
		if _, err := os.Stat(binary); err == nil {
			message = "Codex CLI 文件不可执行"
		}
		recovery := "请修正保存的路径，或恢复自动发现"
		if source == "environment" {
			recovery = "请修正 TEAMCROSS_CODEX_BIN，或在设置中指定可用的 CLI"
		}
		return Installation{Source: source}, problem.New("client_missing", message, recovery)
	}
	if desktop != "" {
		if found := BundledBinary(desktop); found != "" {
			return Installation{Binary: found, Source: "app", DesktopApp: desktop}, nil
		}
	}
	if found := lookupCLI("codex", env.path); found != "" {
		return Installation{Binary: found, Source: "path"}, nil
	}
	for _, app := range env.apps {
		if found := BundledBinary(app); found != "" {
			return Installation{Binary: found, Source: "app", DesktopApp: app}, nil
		}
	}
	if found := lookupCLI("codex", strings.Join(env.common, string(os.PathListSeparator))); found != "" {
		return Installation{Binary: found, Source: "common"}, nil
	}
	if env.shellPath != nil {
		if found := lookupCLI("codex", env.shellPath()); found != "" {
			return Installation{Binary: found, Source: "shell"}, nil
		}
	}
	return Installation{}, problem.New("client_missing", "未找到可用的 Codex CLI", "请安装 Codex CLI 或选择已安装的应用；只查看共享材料无需安装")
}

// Discover uses existing installations without writes or runtime downloads.
func Discover(binary, desktop string) (Installation, error) {
	home, _ := os.UserHomeDir()
	return discover(binary, desktop, discoveryEnvironment{
		home: home, path: os.Getenv("PATH"), override: os.Getenv("TEAMCROSS_CODEX_BIN"),
		apps: installedApps(home), shellPath: loginShellPath,
		common: []string{filepath.Join(home, ".local", "bin"), filepath.Join(home, "bin"), "/opt/homebrew/bin", "/usr/local/bin"},
	})
}

type shellPathEntry struct {
	path    string
	expires time.Time
}

var shellPathCache sync.Map

// RefreshDiscovery lets an explicit recheck observe changed shell setup now.
func RefreshDiscovery() {
	shellPathCache.Delete([3]string{os.Getenv("SHELL"), os.Getenv("HOME"), os.Getenv("PATH")})
}

func loginShellPath() string {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return ""
	}
	key := [3]string{os.Getenv("SHELL"), os.Getenv("HOME"), os.Getenv("PATH")}
	if cached, ok := shellPathCache.Load(key); ok && time.Now().Before(cached.(shellPathEntry).expires) {
		return cached.(shellPathEntry).path
	}
	// GUI launches may omit version-manager paths. Bound shell startup and
	// exclude greeting text; cache both successful and failed probes briefly.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	path := ""
	for _, shell := range []string{key[0], "/bin/zsh", "/bin/bash"} {
		if !filepath.IsAbs(shell) {
			continue
		}
		cmd := exec.CommandContext(ctx, shell, "-ilc", "printf '\\n__TEAMCROSS_PATH_START__\\n'; printenv PATH; printf '__TEAMCROSS_PATH_END__\\n'")
		cmd.WaitDelay = 200 * time.Millisecond
		data, err := cmd.Output()
		if err == nil {
			path = parseShellPath(string(data))
			if path != "" {
				break
			}
		}
	}
	if path == "" && runtime.GOOS == "darwin" && ctx.Err() == nil {
		data, err := exec.CommandContext(ctx, "/bin/launchctl", "getenv", "PATH").Output()
		if err == nil {
			path = strings.TrimSpace(string(data))
		}
	}
	shellPathCache.Store(key, shellPathEntry{path: path, expires: time.Now().Add(time.Minute)})
	return path
}

func parseShellPath(output string) string {
	_, tail, ok := strings.Cut(output, "__TEAMCROSS_PATH_START__\n")
	if !ok {
		return ""
	}
	path, _, ok := strings.Cut(tail, "__TEAMCROSS_PATH_END__")
	if !ok {
		return ""
	}
	path = strings.TrimSpace(path)
	if strings.ContainsAny(path, "\r\n") {
		return ""
	}
	return path
}
