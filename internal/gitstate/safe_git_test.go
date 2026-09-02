package gitstate

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCaptureAndExportNeverExecuteAttributeSelectedFilters(t *testing.T) {
	for _, scope := range []string{"repository", "global"} {
		t.Run(scope, func(t *testing.T) {
			repo := initializedRepository(t)
			if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("*.txt filter=host diff=host\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			runGit(t, repo, "add", ".gitattributes")
			runGit(t, repo, "commit", "-m", "attributes")
			sentinel := filepath.Join(t.TempDir(), "executed")
			script := filepath.Join(t.TempDir(), "filter")
			if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf unsafe > '"+sentinel+"'\ncat\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			if scope == "repository" {
				for _, key := range []string{"filter.host.clean", "filter.host.smudge", "filter.host.process", "diff.host.textconv"} {
					runGit(t, repo, "config", key, script)
				}
				runGit(t, repo, "config", "filter.host.required", "true")
			} else {
				config := filepath.Join(t.TempDir(), "gitconfig")
				if err := os.WriteFile(config, []byte("[filter \"host\"]\n clean = "+script+"\n smudge = "+script+"\n process = "+script+"\n required = true\n[diff \"host\"]\n textconv = "+script+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("GIT_CONFIG_GLOBAL", config)
			}
			if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("literal modified bytes\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			snapshot, err := Capture(context.Background(), repo, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(snapshot.UnstagedPatch, []byte("literal modified bytes")) {
				t.Fatal("literal filesystem bytes missing")
			}
			worktree := filepath.Join(t.TempDir(), "isolated")
			if err := CreateWorktree(context.Background(), snapshot, worktree); err != nil {
				t.Fatal(err)
			}
			patch, err := ExportBinaryPatch(context.Background(), worktree, snapshot.Head)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(patch, []byte("literal modified bytes")) {
				t.Fatal("patch lost literal filesystem bytes")
			}
			if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
				t.Fatal("attribute-selected host command executed")
			}
		})
	}
}
