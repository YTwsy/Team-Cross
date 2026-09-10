package cliinstall

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"teamcross/internal/problem"
	"testing"
)

func fixture(t *testing.T) (string, string) {
	t.Helper()
	root := resolved(t.TempDir())
	source := filepath.Join(root, "Application's $(false) space", "Team Cross.app", "Contents/Resources/teamcross")
	if err := os.MkdirAll(filepath.Dir(source), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return source, filepath.Join(root, "terminal bin")
}

func TestLauncherUsesAppAndPreservesLiteralArguments(t *testing.T) {
	source, dir := fixture(t)
	status, err := Install(source, dir, dir)
	if err != nil || !status.Installed || !status.PathReady {
		t.Fatal(status, err)
	}
	args := []string{"argument with spaces", "$(touch should-not-exist)", "quote'\"", "--json"}
	out, err := exec.Command(status.Target, args...).Output()
	if err != nil || string(out) != strings.Join(args, "\n")+"\n" {
		t.Fatal(string(out), err)
	}
	// Replacing the App at its stable location must update the command immediately.
	if err := os.WriteFile(source, []byte("#!/bin/sh\nprintf 'updated\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	out, err = exec.Command(status.Target).Output()
	if err != nil || string(out) != "updated\n" {
		t.Fatal(string(out), err)
	}
	if _, err = Uninstall(source, dir, dir); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Lstat(status.Target); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err = os.Stat(source); err != nil {
		t.Fatal("uninstall changed App", err)
	}
}

func TestForeignFilesAndSymlinksArePreserved(t *testing.T) {
	for _, kind := range []string{"file", "symlink", "forged-marker"} {
		t.Run(kind, func(t *testing.T) {
			source, dir := fixture(t)
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(dir, "teamcross")
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(source, target)
			case "forged-marker":
				err = os.WriteFile(target, append(launcher(source), []byte("# user modification\n")...), 0755)
			default:
				err = os.WriteFile(target, []byte("#!/bin/sh\nexit 0\n"), 0755)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(target)
			for _, change := range []func(string, string, string) (Status, error){Install, Uninstall} {
				_, err = change(source, dir, dir)
				if err == nil || problem.Describe(err).Code != "cli_conflict" {
					t.Fatal(err)
				}
				after, _ := os.ReadFile(target)
				if string(after) != string(before) {
					t.Fatal("foreign command changed")
				}
			}
		})
	}
}

func TestHomebrewCommandIsReusedOrReportedAsConflict(t *testing.T) {
	source, dir := fixture(t)
	brewBin := filepath.Join(filepath.Dir(dir), "brew/bin")
	if err := os.MkdirAll(brewBin, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(brewBin, "teamcross")
	if err := os.Symlink(source, link); err != nil {
		t.Fatal(err)
	}
	s, err := Install(source, dir, brewBin)
	if err != nil || s.Command != link || s.CanInstall {
		t.Fatal(s, err)
	}
	if _, err := os.Lstat(s.Target); !os.IsNotExist(err) {
		t.Fatal("created competing command", err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(link, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	_, err = Install(source, dir, brewBin)
	if err == nil || problem.Describe(err).Code != "cli_conflict" {
		t.Fatal(err)
	}
}

func TestConcurrentInstallAndRemove(t *testing.T) {
	source, dir := fixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Install(source, dir, dir); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if sourceOnDisk, ours := owned(filepath.Join(dir, "teamcross")); !ours || sourceOnDisk != source {
		t.Fatal(sourceOnDisk, ours)
	}
	if _, err := Uninstall(source, dir, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(source, dir, dir); err != nil {
		t.Fatal("remove must be idempotent", err)
	}
}

func TestStandaloneCannotCreateAppLauncher(t *testing.T) {
	_, dir := fixture(t)
	_, err := Install(filepath.Join(filepath.Dir(dir), "teamcross"), dir, dir)
	if err == nil || problem.Describe(err).Code != "cli_requires_app" {
		t.Fatal(err)
	}
}

func TestPathReadinessUsesActualEnvironment(t *testing.T) {
	source, dir := fixture(t)
	s, err := Install(source, dir, "/usr/bin:/bin")
	if err != nil || !s.Installed || s.PathReady {
		t.Fatal(s, err)
	}
	// Finder's fallback scan must not imply that a shell has this PATH entry.
	if Inspect(source, DefaultDir, "/usr/bin:/bin").PathReady {
		t.Fatal("fallback search directories were treated as shell PATH")
	}
}
