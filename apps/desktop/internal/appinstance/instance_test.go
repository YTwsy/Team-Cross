package appinstance

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func decodeReceipt(t *testing.T, data []byte) receipt {
	t.Helper()
	var out receipt
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestReceiptOnlyAcknowledgesEnqueueAndDeduplicates(t *testing.T) {
	calls := 0
	now := time.Now()
	i := &Instance{accepted: make(map[string]time.Time), now: func() time.Time { return now }, receive: func(Request) bool { calls++; return true }}
	request := NewRequest([]string{"teamcross://join?invite=fixture"})
	data, _ := json.Marshal(request)
	for range 2 {
		out := decodeReceipt(t, i.reply(data))
		if out.ID != request.ID || !out.Accepted {
			t.Fatal(out)
		}
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	now = now.Add(11 * time.Minute)
	if !decodeReceipt(t, i.reply(data)).Accepted || calls != 2 {
		t.Fatal(calls)
	}
}

func TestQueueAndProtocolBounds(t *testing.T) {
	accept := false
	i := &Instance{accepted: make(map[string]time.Time), now: time.Now, receive: func(Request) bool { return accept }}
	request := NewRequest(nil)
	data, _ := json.Marshal(request)
	if decodeReceipt(t, i.reply(data)).Accepted || len(i.accepted) != 0 {
		t.Fatal("busy queue acknowledged")
	}
	accept = true
	if !decodeReceipt(t, i.reply(data)).Accepted {
		t.Fatal("retry not accepted")
	}
	for _, invalid := range []Request{
		{2, request.ID, nil}, {1, "not-a-uuid", nil}, NewRequest([]string{"https://example.com"}),
		NewRequest([]string{"teamcross://user@join?invite=x"}), NewRequest(make([]string, 33)),
	} {
		data, _ := json.Marshal(invalid)
		if i.reply(data) != nil {
			t.Fatal("accepted invalid protocol")
		}
	}
	if i.reply(make([]byte, (1<<20)+1)) != nil {
		t.Fatal("accepted oversized message")
	}
	for len(i.accepted) < 512 {
		i.accepted[NewRequest(nil).ID] = time.Now()
	}
	data, _ = json.Marshal(NewRequest(nil))
	if decodeReceipt(t, i.reply(data)).Accepted {
		t.Fatal("unbounded receipt cache")
	}
}

func TestCanonicalDirectoryPortAndIndependentOwnership(t *testing.T) {
	directory := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
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
	other, err := New(alias)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if other.name != owner.name {
		t.Fatal("path alias split identity")
	}
	if claimed, err := other.Claim(func(Request) bool { t.Error("secondary received"); return false }); err != nil || claimed {
		t.Fatal(claimed, err)
	}
	request := NewRequest(nil)
	if !other.Forward(request) || !other.Forward(request) || count.Load() != 1 {
		t.Fatal("receipt retry failed", count.Load())
	}
	separate, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer separate.Close()
	if claimed, err := separate.Claim(func(Request) bool { return true }); err != nil || !claimed || separate.name == owner.name {
		t.Fatal(claimed, err)
	}
	owner.Close()
	if claimed, err := other.Claim(func(Request) bool { return true }); err != nil || !claimed {
		t.Fatal("failed to reclaim", claimed, err)
	}
	if _, err := os.Stat(filepath.Join(directory, "app.lock")); err != nil {
		t.Fatal("lock file was removed", err)
	}
}

func TestRejectSymlinkLock(t *testing.T) {
	directory := t.TempDir()
	if err := os.Symlink(filepath.Join(t.TempDir(), "unrelated"), filepath.Join(directory, "app.lock")); err != nil {
		t.Fatal(err)
	}
	if instance, err := New(directory); err == nil {
		instance.Close()
		t.Fatal("followed lock symlink")
	}
}

func TestCrashReleasesOnlyShellOwnership(t *testing.T) {
	directory := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestInstanceHelper$")
	cmd.Env = append(os.Environ(), "TEAMCROSS_INSTANCE_HELPER="+directory)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(directory, "ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := os.WriteFile(filepath.Join(directory, "preserved"), []byte("Core and user data"), 0600); err != nil {
		t.Fatal(err)
	}
	secondary, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer secondary.Close()
	if claimed, _ := secondary.Claim(func(Request) bool { return true }); claimed {
		t.Fatal("duplicate owner")
	}
	// Suspend exactly this helper. A timed-out receipt may already have queued
	// the request in the kernel; retrying the same ID must enqueue it only once.
	request := NewRequest(nil)
	if err := cmd.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	if secondary.Forward(request) {
		t.Fatal("stopped owner returned a receipt")
	}
	if claimed, _ := secondary.Claim(func(Request) bool { return true }); claimed {
		t.Fatal("stole stopped owner's lock")
	}
	if err := cmd.Process.Signal(syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	if !secondary.Forward(request) || !secondary.Forward(request) {
		t.Fatal("cannot retry across processes")
	}
	if value, err := os.ReadFile(filepath.Join(directory, "count")); err != nil || string(value) != "1" {
		t.Fatal("replayed request", string(value), err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if claimed, err := secondary.Claim(func(Request) bool { return true }); err != nil || !claimed {
		t.Fatal("crashed lock not recovered", claimed, err)
	}
	if value, err := os.ReadFile(filepath.Join(directory, "preserved")); err != nil || string(value) != "Core and user data" {
		t.Fatal("data changed")
	}
}

func TestInstanceHelper(t *testing.T) {
	directory := os.Getenv("TEAMCROSS_INSTANCE_HELPER")
	if directory == "" {
		t.Skip("subprocess helper")
	}
	instance, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	count := 0
	if claimed, err := instance.Claim(func(Request) bool {
		count++
		return os.WriteFile(filepath.Join(directory, "count"), []byte(strconv.Itoa(count)), 0600) == nil
	}); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	if err := os.WriteFile(filepath.Join(directory, "ready"), []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	select {}
}
