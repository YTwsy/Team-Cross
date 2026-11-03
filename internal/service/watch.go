package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// WatchInstalled also covers resident MCP processes with older launcher code.
// The installed helper owns the lifecycle lock and outlives the Core it replaces.
// No UI state or business request is passed to it.
func WatchInstalled(ctx context.Context, c Connection) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var inspected os.FileInfo
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		info, err := os.Stat(c.Executable)
		if err != nil {
			continue
		}
		if inspected != nil && os.SameFile(info, inspected) && info.Size() == inspected.Size() && info.ModTime().Equal(inspected.ModTime()) {
			continue
		}
		build, err := InstalledBuild(ctx, c.Executable)
		if err != nil {
			continue
		}
		inspected = info
		if build.Protocol != c.Protocol || build.Matches(c.Version, c.Commit) {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		inspected = nil // Retry a failed handoff after the backoff, even if bytes stay the same.
		log, err := os.OpenFile(filepath.Join(c.DataDir, "core-upgrade.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			continue
		}
		cmd := exec.Command(c.Executable, "serve", "--no-open", "--json", "--data-dir", c.DataDir)
		cmd.Stdout, cmd.Stderr = log, log
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err = cmd.Start(); err != nil {
			log.Close()
			continue
		}
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); _ = log.Close(); close(done) }()
		select {
		case <-ctx.Done():
			return
		case <-done:
		}
		// A failed replacement must not create a rapid restart loop.
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}
