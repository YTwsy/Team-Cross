package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"teamcross/internal/domain"
	"teamcross/internal/gitstate"
)

// capturedThreadPaths reads only immutable, same-Thread capture sources. A
// cumulative manifest bounds the history walk after the first new checkpoint;
// older checkpoints are supported through their actual patch/snapshot objects.
// Paths are grants to read current files, never grants to restore old content.
func (app *App) capturedThreadPaths(ctx context.Context, thread domain.Thread) ([]string, error) {
	rounds, err := app.store.ListRounds(ctx, thread.ID)
	if err != nil {
		return nil, err
	}
	paths := map[string]bool{}
	addPatch := func(hash string) error {
		if hash == "" {
			return nil
		}
		patch, err := app.store.GetObject(ctx, hash)
		if err != nil {
			return err
		}
		added, err := gitstate.PatchPaths(ctx, thread.WorktreePath, patch)
		if err != nil {
			return err
		}
		for _, path := range added {
			paths[path] = true
		}
		return nil
	}
	for i := len(rounds) - 1; i >= 0; i-- {
		round := rounds[i]
		var manifest struct {
			CapturedPaths *[]string `json:"capturedPaths"`
			PatchObject   string    `json:"patchObject"`
		}
		if round.ManifestObject != "" {
			data, err := app.store.GetObject(ctx, round.ManifestObject)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(data, &manifest); err != nil {
				return nil, fmt.Errorf("read immutable captured-path manifest: %w", err)
			}
		}
		if manifest.CapturedPaths != nil {
			for _, path := range *manifest.CapturedPaths {
				paths[path] = true
			}
			break
		}
		if err := addPatch(manifest.PatchObject); err != nil {
			return nil, err
		}
		if round.SnapshotID == "" {
			continue
		}
		stored, err := app.store.GetGitSnapshot(ctx, round.SnapshotID)
		if err != nil {
			return nil, err
		}
		if stored.ThreadID != thread.ID || stored.Head != thread.BaselineCommit || stored.Unborn {
			return nil, fmt.Errorf("captured-path snapshot baseline or Thread mismatch")
		}
		for _, hash := range []string{stored.StagedPatchObject, stored.UnstagedPatchObject} {
			if err := addPatch(hash); err != nil {
				return nil, err
			}
		}
		if stored.UntrackedManifestObject != "" {
			data, err := app.store.GetObject(ctx, stored.UntrackedManifestObject)
			if err != nil {
				return nil, err
			}
			var files []gitstate.UntrackedFile
			if err := json.Unmarshal(data, &files); err != nil {
				return nil, fmt.Errorf("read captured untracked paths: %w", err)
			}
			for _, file := range files {
				if file.Included {
					paths[file.Path] = true
				}
			}
		}
	}
	return sortedCapturedPaths(paths), nil
}

func (app *App) exportThreadPatch(ctx context.Context, thread domain.Thread) ([]byte, error) {
	paths, err := app.capturedThreadPaths(ctx, thread)
	if err != nil {
		return nil, err
	}
	return gitstate.ExportBinaryPatchWithCaptured(ctx, thread.WorktreePath, thread.BaselineCommit, paths)
}

func (app *App) captureThreadPatch(ctx context.Context, thread domain.Thread) ([]byte, []string, error) {
	previous, err := app.capturedThreadPaths(ctx, thread)
	if err != nil {
		return nil, nil, err
	}
	patch, err := gitstate.ExportBinaryPatchWithCaptured(ctx, thread.WorktreePath, thread.BaselineCommit, previous)
	if err != nil {
		return nil, nil, err
	}
	current, err := gitstate.PatchPaths(ctx, thread.WorktreePath, patch)
	if err != nil {
		return nil, nil, err
	}
	paths := make(map[string]bool, len(previous)+len(current))
	for _, path := range append(previous, current...) {
		paths[path] = true
	}
	return patch, sortedCapturedPaths(paths), nil
}

func sortedCapturedPaths(paths map[string]bool) []string {
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}
