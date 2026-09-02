package bridgeclient

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCallAndEvent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	directory := t.TempDir()
	script := filepath.Join(directory, "bridge.sh")
	content := "#!/bin/sh\nwhile IFS= read -r line; do\n  printf '%s\\n' '{\"jsonrpc\":\"2.0\",\"method\":\"event\",\"params\":{\"type\":\"run.started\"}}'\n  id=$(printf '%s' \"$line\" | sed -E 's/.*\"id\":\"([^\"]+)\".*/\\1/')\n  printf '{\"jsonrpc\":\"2.0\",\"id\":\"%s\",\"result\":{\"ok\":true}}\\n' \"$id\"\ndone\n"
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	client, err := Start(context.Background(), StartOptions{Command: script})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	// Process startup can take longer than a second on a cold or highly parallel
	// CI worker. Keep the assertion bounded without making it timing-sensitive.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var result struct {
		OK bool `json:"ok"`
	}
	if err := client.Call(ctx, "ping", map[string]any{}, &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatal("missing result")
	}
	select {
	case event := <-client.Events():
		if event.Type != "run.started" {
			t.Fatalf("unexpected event %q", event.Type)
		}
	case <-ctx.Done():
		t.Fatal("event not received")
	}
}

func TestProbeChecksProtocolAndDetectsStoppedBridge(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	directory := t.TempDir()
	script := filepath.Join(directory, "bridge.sh")
	content := "#!/bin/sh\nwhile IFS= read -r line; do\n  id=$(printf '%s' \"$line\" | sed -E 's/.*\"id\":\"([^\"]+)\".*/\\1/')\n  printf '{\"jsonrpc\":\"2.0\",\"id\":\"%s\",\"result\":{\"ok\":true,\"name\":\"teamcross-agent-bridge\",\"version\":\"test\",\"protocolVersion\":1}}\\n' \"$id\"\n  sleep 0.1\n  exit 0\ndone\n"
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	client, err := Start(context.Background(), StartOptions{Command: script})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	readiness, err := client.Probe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if readiness.Version != "test" {
		t.Fatalf("unexpected readiness: %+v", readiness)
	}
	select {
	case <-client.Done():
	case <-ctx.Done():
		t.Fatal("bridge did not stop")
	}
	if _, err := client.Probe(ctx); err == nil || !strings.Contains(err.Error(), "bridge stopped") {
		t.Fatalf("expected stopped bridge error, got %v", err)
	}
}

func TestProbeRejectsIncompatibleProtocol(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	directory := t.TempDir()
	script := filepath.Join(directory, "bridge.sh")
	content := "#!/bin/sh\nwhile IFS= read -r line; do\n  id=$(printf '%s' \"$line\" | sed -E 's/.*\"id\":\"([^\"]+)\".*/\\1/')\n  printf '{\"jsonrpc\":\"2.0\",\"id\":\"%s\",\"result\":{\"ok\":true,\"name\":\"teamcross-agent-bridge\",\"protocolVersion\":99}}\\n' \"$id\"\ndone\n"
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	client, err := Start(context.Background(), StartOptions{Command: script})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.Probe(ctx); err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("expected protocol error, got %v", err)
	}
}

func TestResolveDefaultCommandOrderAndLayouts(t *testing.T) {
	root := t.TempDir()
	explicitRoot := filepath.Join(root, "explicit")
	explicitScript := createBridgeFixture(t, filepath.Join(explicitRoot, "packages", "agent-bridge", "dist", "index.js"))
	installRoot := filepath.Join(root, "install")
	installScript := createBridgeFixture(t, filepath.Join(installRoot, "packages", "agent-bridge", "dist", "index.js"))
	executable := filepath.Join(installRoot, "bin", "teamcross")
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatal(err)
	}

	resolved, err := resolveDefaultCommand(explicitRoot, executable, filepath.Join(root, "target-repo"), "/usr/local/bin/teamcross-agent-bridge")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resolved.Args, []string{explicitScript}) {
		t.Fatalf("explicit source root must win, got %+v", resolved)
	}

	resolved, err = resolveDefaultCommand("", executable, filepath.Join(root, "target-repo"), "/usr/local/bin/teamcross-agent-bridge")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resolved.Args, []string{installScript}) {
		t.Fatalf("executable-relative install must win, got %+v", resolved)
	}

	appExecutable := filepath.Join(root, "Team Cross.app", "Contents", "MacOS", "teamcross")
	appScript := createBridgeFixture(t, filepath.Join(root, "Team Cross.app", "Contents", "Resources", "agent-bridge", "dist", "index.js"))
	resolved, err = resolveDefaultCommand("", appExecutable, filepath.Join(root, "target-repo"), "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resolved.Args, []string{appScript}) {
		t.Fatalf("macOS bundle bridge was not resolved, got %+v", resolved)
	}
}

func TestResolveDefaultCommandFallsBackToPathAndDevAncestor(t *testing.T) {
	root := t.TempDir()
	resolved, err := resolveDefaultCommand("", filepath.Join(root, "missing", "teamcross"), filepath.Join(root, "elsewhere"), "/opt/teamcross-agent-bridge")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Command != "/opt/teamcross-agent-bridge" || len(resolved.Args) != 0 {
		t.Fatalf("unexpected PATH resolution: %+v", resolved)
	}

	devScript := createBridgeFixture(t, filepath.Join(root, "packages", "agent-bridge", "dist", "index.js"))
	createBridgeFixture(t, filepath.Join(root, "go.mod"))
	createBridgeFixture(t, filepath.Join(root, "pnpm-workspace.yaml"))
	working := filepath.Join(root, "internal", "server", "fixture")
	if err := os.MkdirAll(working, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err = resolveDefaultCommand("", filepath.Join(root, "missing", "teamcross"), working, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resolved.Args, []string{devScript}) {
		t.Fatalf("dev ancestor was not resolved, got %+v", resolved)
	}
}

func createBridgeFixture(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return absolute
}
