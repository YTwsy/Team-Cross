package commandtools

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAdminCommandKeepsPathsLiteralAndOnlySupportsLauncherOperations(t *testing.T) {
	root := t.TempDir()
	helper := filepath.Join(root, "Team Cross ' $HOME `id` 🧪.app/Contents/Resources/teamcross")
	directory := filepath.Join(root, "commands ' $HOME `id`")
	if err := os.MkdirAll(filepath.Dir(helper), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nprintf '%s\\000' \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	m := Manager{Helper: helper, Directory: directory}
	for _, remove := range []bool{false, true} {
		command, err := m.adminCommand(remove)
		if err != nil {
			t.Fatal(err)
		}
		result, err := exec.Command("/bin/sh", "-c", command).Output()
		if err != nil {
			t.Fatal(err)
		}
		operation := "install-cli"
		if remove {
			operation = "uninstall-cli"
		}
		expected := bytes.Join([][]byte{[]byte(operation), []byte("--json"), []byte("--cli-dir"), []byte(directory), nil}, []byte{0})
		if !bytes.Equal(result, expected) {
			t.Fatalf("arguments changed: %q", result)
		}
	}
	m.Helper = "/usr/bin/true"
	if _, err := m.adminCommand(false); err == nil {
		t.Fatal("accepted a non-App command")
	}
}

func TestUnprivilegedLauncherInstallConflictAndRemoval(t *testing.T) {
	root := t.TempDir()
	helper := filepath.Join(root, "Team Cross.app/Contents/Resources/teamcross")
	if err := os.MkdirAll(filepath.Dir(helper), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	m := Manager{Helper: helper, Directory: filepath.Join(root, "bin"), SearchPath: "/usr/bin:/bin"}
	if status, err := m.Change(context.Background(), false, false); err != nil || !status.Installed {
		t.Fatal(status, err)
	}
	if _, err := m.Change(context.Background(), true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(helper); err != nil {
		t.Fatal("removed App helper", err)
	}
	target := filepath.Join(m.Directory, "teamcross")
	if err := os.WriteFile(target, []byte("user-owned"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, remove := range []bool{false, true} {
		if _, err := m.Change(context.Background(), remove, false); err == nil {
			t.Fatal("overwrote or removed a foreign command")
		}
	}
	if data, _ := os.ReadFile(target); string(data) != "user-owned" {
		t.Fatal("foreign command changed")
	}
}
