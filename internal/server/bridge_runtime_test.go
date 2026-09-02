package server

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestOpenRestartsBridgeOnceWhenInitialReadinessFails(t *testing.T) {
	directory := t.TempDir()
	counter := filepath.Join(directory, "starts")
	script := writeBridgeFixture(t, directory, `#!/bin/sh
counter=$1
count=0
if [ -f "$counter" ]; then count=$(cat "$counter"); fi
count=$((count + 1))
printf '%s' "$count" > "$counter"
if [ "$count" -eq 1 ]; then exit 1; fi
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -E 's/.*"id":"([^"]+)".*/\1/')
  printf '{"jsonrpc":"2.0","id":"%s","result":{"ok":true,"name":"teamcross-agent-bridge","version":"fixture","protocolVersion":1}}\n' "$id"
done
`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app, err := Open(ctx, Config{
		Repo:          directory,
		DataDir:       filepath.Join(directory, "data"),
		BridgeCommand: script,
		BridgeArgs:    []string{counter},
		Logger:        testLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	value, err := os.ReadFile(counter)
	if err != nil {
		t.Fatal(err)
	}
	starts, err := strconv.Atoi(string(value))
	if err != nil {
		t.Fatal(err)
	}
	if starts != 2 {
		t.Fatalf("expected exactly one bounded restart, got %d starts", starts)
	}
	check := app.bridgeDoctorCheck(context.Background())
	if check.Status != "ok" || !strings.Contains(check.Detail, "protocol 1") {
		t.Fatalf("unexpected bridge doctor result: %+v", check)
	}
}

func TestBridgeDoctorCheckDetectsProcessThatStoppedAfterStartup(t *testing.T) {
	directory := t.TempDir()
	script := writeBridgeFixture(t, directory, `#!/bin/sh
IFS= read -r line
id=$(printf '%s' "$line" | sed -E 's/.*"id":"([^"]+)".*/\1/')
printf '{"jsonrpc":"2.0","id":"%s","result":{"ok":true,"name":"teamcross-agent-bridge","version":"fixture","protocolVersion":1}}\n' "$id"
sleep 0.1
exit 0
`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app, err := Open(ctx, Config{
		Repo:          directory,
		DataDir:       filepath.Join(directory, "data"),
		BridgeCommand: script,
		Logger:        testLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	select {
	case <-app.bridge.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("fixture bridge did not stop")
	}
	check := app.bridgeDoctorCheck(context.Background())
	if check.Status != "error" || !strings.Contains(check.Detail, "bridge stopped") {
		t.Fatalf("stopped bridge was not detected: %+v", check)
	}
}

func writeBridgeFixture(t *testing.T, directory, content string) string {
	t.Helper()
	path := filepath.Join(directory, "bridge.sh")
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
