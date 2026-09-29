package appinstance

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Exercise the actual Swift implementation so Foundation path aliases, JSON
// encoding and CFMessagePort framing cannot silently drift between shells.
func TestSwiftShellProtocolInterop(t *testing.T) {
	repo, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	output := t.TempDir()
	compile := func(name, source string) string {
		t.Helper()
		destination := filepath.Join(output, name)
		command := exec.Command("xcrun", "swiftc", "-module-cache-path", filepath.Join(output, "modules"),
			filepath.Join(repo, "apps/macos/AppLanguage.swift"), filepath.Join(repo, "apps/macos/AppInstance.swift"), source, "-o", destination)
		if data, err := command.CombinedOutput(); err != nil {
			t.Fatalf("compile legacy fixture: %v\n%s", err, data)
		}
		return destination
	}
	probe := compile("probe", filepath.Join(repo, "scripts/fixtures/app-instance-probe.swift"))
	receiver := compile("receiver", filepath.Join(repo, "apps/desktop/internal/appinstance/testdata/receiver.swift"))
	directory := t.TempDir()
	alias := filepath.Join(t.TempDir(), "directory alias")
	if err := os.Symlink(directory, alias); err != nil {
		t.Fatal(err)
	}
	owner, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	var count atomic.Int32
	if claimed, err := owner.Claim(func(Request) bool { count.Add(1); return true }); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	request := NewRequest(nil)
	for range 2 {
		data, err := exec.Command(probe, alias, request.ID).CombinedOutput()
		if err != nil || strings.TrimSpace(string(data)) != "true" {
			t.Fatalf("Swift to Go: %v %s", err, data)
		}
	}
	if count.Load() != 1 {
		t.Fatal("legacy receipt retry duplicated")
	}

	legacyDirectory := t.TempDir()
	command := exec.Command(receiver, legacyDirectory)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(legacyDirectory, "ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("legacy receiver did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	secondary, err := New(legacyDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer secondary.Close()
	if claimed, err := secondary.Claim(func(Request) bool { return false }); err != nil || claimed {
		t.Fatal("legacy lock not shared", claimed, err)
	}
	request = NewRequest(nil)
	if !secondary.Forward(request) || !secondary.Forward(request) {
		t.Fatal("Go to Swift receipt failed")
	}
	if data, err := os.ReadFile(filepath.Join(legacyDirectory, "count")); err != nil || string(data) != "1" {
		t.Fatal("legacy queue duplicated", string(data), err)
	}
}
