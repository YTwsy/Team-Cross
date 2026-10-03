package nativecodex

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"teamcross/internal/problem"
)

func discoveryCLI(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDiscoverBundledLayoutsAndUpgrade(t *testing.T) {
	for _, layout := range bundledCLIRelatives {
		t.Run(layout, func(t *testing.T) {
			app := filepath.Join(t.TempDir(), "ChatGPT.app")
			cli := discoveryCLI(t, filepath.Join(app, layout))
			for _, selected := range []string{"", app} {
				found, err := discover("", selected, discoveryEnvironment{apps: []string{app}})
				if err != nil || found.Binary != cli || found.Source != "app" || found.DesktopApp != app {
					t.Fatal(found, err)
				}
			}
		})
	}
	app := filepath.Join(t.TempDir(), "Codex.app")
	old := filepath.Join(app, "Contents/Resources/codex")
	newCLI := discoveryCLI(t, filepath.Join(app, bundledCLIRelatives[0]))
	for _, desktop := range []string{"", app} {
		found, err := discover(old, desktop, discoveryEnvironment{})
		if err != nil || found.Binary != newCLI || found.RecoveredFrom != old || found.DesktopApp != app {
			t.Fatal(found, err)
		}
	}
	// When both layouts exist, automatic detection prefers the current layout;
	// a still-working explicit path retains the user's choice.
	discoveryCLI(t, old)
	if found, err := discover("", app, discoveryEnvironment{}); err != nil || found.Binary != newCLI {
		t.Fatal(found, err)
	}
	if found, err := discover(old, app, discoveryEnvironment{}); err != nil || found.Binary != old || found.Source != "custom" || found.RecoveredFrom != "" {
		t.Fatal(found, err)
	}
}

func TestDiscoverHonorsExplicitPinsAndEnvironment(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "ChatGPT.app")
	appCLI := discoveryCLI(t, filepath.Join(app, bundledCLIRelatives[0]))
	custom := discoveryCLI(t, filepath.Join(dir, "custom", "codex"))
	fromEnv := discoveryCLI(t, filepath.Join(dir, "env", "codex"))
	missing := filepath.Join(dir, "custom", "missing")
	nonExecutable := filepath.Join(dir, "custom", "text")
	if err := os.WriteFile(nonExecutable, []byte("not an executable"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, binary, desktop, override, want, source, message string
	}{
		{"manual before env", custom, app, fromEnv, custom, "custom", ""},
		{"environment", "", app, fromEnv, fromEnv, "environment", ""},
		{"custom missing", missing, app, "", "", "custom", "Codex CLI 文件不存在"},
		{"custom permissions", nonExecutable, app, "", "", "custom", "Codex CLI 文件不可执行"},
		{"env missing", "", app, missing, "", "environment", "Codex CLI 文件不存在"},
		{"another selected app", filepath.Join(app, "Contents/Resources/codex"), filepath.Join(dir, "Other.app"), "", "", "custom", "Codex CLI 文件不存在"},
	} {
		t.Run(test.name, func(t *testing.T) {
			found, err := discover(test.binary, test.desktop, discoveryEnvironment{path: filepath.Dir(appCLI), apps: []string{app}, override: test.override})
			if found.Binary != test.want || found.Source != test.source {
				t.Fatal(found, err)
			}
			if test.message == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || err.Error() != test.message || problem.Describe(err).Recovery == "" {
				t.Fatal("missing actionable pin error", found, err)
			}
		})
	}
	// Home expansion is separate from the stored raw setting.
	if found, err := discover("~/custom/codex", "", discoveryEnvironment{home: dir}); err != nil || found.Binary != custom {
		t.Fatal(found, err)
	}
}

func TestDiscoverAutomaticOrderAndShellFallback(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "ChatGPT.app")
	appCLI := discoveryCLI(t, filepath.Join(app, bundledCLIRelatives[0]))
	pathCLI := discoveryCLI(t, filepath.Join(dir, "path", "codex"))
	commonCLI := discoveryCLI(t, filepath.Join(dir, "common", "codex"))
	shellCLI := discoveryCLI(t, filepath.Join(dir, "nvm", "bin", "codex"))
	shellCalls := 0
	env := discoveryEnvironment{path: filepath.Dir(pathCLI), apps: []string{app}, common: []string{filepath.Dir(commonCLI)}, shellPath: func() string {
		shellCalls++
		return filepath.Dir(shellCLI)
	}}
	for _, test := range []struct {
		name, desktop, want, source string
		path, apps, common          bool
	}{
		{"selected application", app, appCLI, "app", true, true, true},
		{"inherited path", "", pathCLI, "path", true, true, true},
		{"known applications", "", appCLI, "app", false, true, true},
		{"common directories", "", commonCLI, "common", false, false, true},
		{"login shell", "", shellCLI, "shell", false, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			probe := env
			if !test.path {
				probe.path = ""
			}
			if !test.apps {
				probe.apps = nil
			}
			if !test.common {
				probe.common = nil
			}
			found, err := discover("", test.desktop, probe)
			if err != nil || found.Binary != test.want || found.Source != test.source {
				t.Fatal(found, err)
			}
		})
	}
	if shellCalls != 1 {
		t.Fatal("shell was started before cheaper candidates were exhausted", shellCalls)
	}
	if found, err := discover("codex", "", discoveryEnvironment{shellPath: env.shellPath}); err != nil || found.Binary != shellCLI || found.Source != "custom" {
		t.Fatal("manual command names must work after a GUI launch", found, err)
	}
	if _, err := discover("", "", discoveryEnvironment{}); err == nil || problem.Describe(err).Code != "client_missing" {
		t.Fatal(err)
	}
}

func TestLookupCLISkipsBrokenLinksNonExecutablesAndRelativePATH(t *testing.T) {
	dir := t.TempDir()
	bad, link, good := filepath.Join(dir, "bad"), filepath.Join(dir, "link"), filepath.Join(dir, "good")
	for _, path := range []string{bad, link} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(bad, "codex"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "absent"), filepath.Join(link, "codex")); err != nil {
		t.Fatal(err)
	}
	cli := discoveryCLI(t, filepath.Join(good, "codex"))
	if got := lookupCLI("codex", strings.Join([]string{"", ".", bad, link, good}, string(os.PathListSeparator))); got != cli {
		t.Fatal(got)
	}
	if got := lookupCLI("codex", "."); got != "" {
		t.Fatal("current directory is not an automatic install location", got)
	}
}

func TestShellPathMarkersAndExplicitRefresh(t *testing.T) {
	if got := parseShellPath("welcome\n__TEAMCROSS_PATH_START__\n/a:/b\n__TEAMCROSS_PATH_END__\nbye"); got != "/a:/b" {
		t.Fatal(got)
	}
	for _, bad := range []string{"/a:/b", "__TEAMCROSS_PATH_START__\n/a:/b", "__TEAMCROSS_PATH_START__\n/a\n/b\n__TEAMCROSS_PATH_END__"} {
		if got := parseShellPath(bad); got != "" {
			t.Fatal("unmarked or mixed shell output accepted", got)
		}
	}
	dir := t.TempDir()
	shell := filepath.Join(dir, "shell")
	value := filepath.Join(dir, "path")
	write := func(path string) {
		t.Helper()
		if err := os.WriteFile(value, []byte(path), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Execute the supplied command with the chosen PATH. This tests the real
	// probe quoting rather than only a canned marker response.
	script := "#!/bin/sh\nPATH=$(/bin/cat '" + value + "')\nexport PATH\nexec /bin/sh -c \"$2\"\n"
	if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", shell)
	t.Setenv("HOME", dir)
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Cleanup(RefreshDiscovery)
	write("/usr/bin:/first")
	if got := loginShellPath(); got != "/usr/bin:/first" {
		t.Fatal("login shell PATH not extracted", got)
	}
	write("/usr/bin:/second")
	if got := loginShellPath(); got != "/usr/bin:/first" {
		t.Fatal("probe was not cached", got)
	}
	RefreshDiscovery()
	if got := loginShellPath(); got != "/usr/bin:/second" {
		t.Fatal("explicit recheck reused stale shell PATH", got)
	}
	// A plain system shell also honors the marker command.
	if out, err := exec.Command("/bin/sh", "-c", "printf '\\n__TEAMCROSS_PATH_START__\\n'; printf '/usr/bin\\n'; printf '__TEAMCROSS_PATH_END__\\n'").Output(); err != nil || parseShellPath(string(out)) != "/usr/bin" {
		t.Fatal(string(out), err)
	}
}
