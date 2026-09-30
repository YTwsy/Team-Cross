package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestEnsureCancellationReapsOnlyItsStartingProcess(t *testing.T) {
	directory := t.TempDir()
	helper := filepath.Join(directory, "delayed-core")
	// The shell is replaced by sleep, preserving the owned PID. No ready Core
	// or user provider is involved; the parent cancels only after spawn.
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nprintf '%s' \"$$\" > started-pid\nexec /bin/sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := Ensure(ctx, directory, helper, nil); done <- err }()
	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(filepath.Join(directory, "started-pid"))
		pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		if pid > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		cancel()
		<-done
		t.Fatal("helper never started")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled startup succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("startup cancellation did not settle")
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("starting process remains: %v", err)
	}
	lock, err := Lock(directory, "start.lock")
	if err != nil {
		t.Fatal("startup lock not released after process exit", err)
	}
	lock.Close()
}

func TestAlreadyCancelledEnsureDoesNotSpawn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Ensure(ctx, t.TempDir(), "/does-not-exist", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled startup attempted a process: %v", err)
	}
}
