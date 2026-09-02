package gitstate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"teamcross/internal/domain"
)

// ObjectWriter is satisfied by storage.Store and keeps gitstate independent of
// the SQLite implementation.
type ObjectWriter interface {
	PutObject(context.Context, []byte, string) (domain.Object, error)
}

type ObjectReader interface {
	GetObject(context.Context, string) ([]byte, error)
}

type UntrackedManifest struct {
	Files []UntrackedManifestFile `json:"files"`
}

type UntrackedManifestFile struct {
	Path          string `json:"path"`
	Size          int64  `json:"size"`
	Mode          uint32 `json:"mode"`
	SHA256        string `json:"sha256,omitempty"`
	ObjectHash    string `json:"objectHash,omitempty"`
	Included      bool   `json:"included"`
	OmittedReason string `json:"omittedReason,omitempty"`
	Symlink       bool   `json:"symlink,omitempty"`
}

// PersistObjects writes every captured blob into the content-addressed object
// store and returns a database-ready GitSnapshot. Untracked file bodies are
// referenced from the manifest rather than duplicated inline.
func (snapshot Snapshot) PersistObjects(ctx context.Context, writer ObjectWriter, threadID string) (domain.GitSnapshot, error) {
	status, err := writer.PutObject(ctx, snapshot.Status, "application/vnd.teamcross.git-status")
	if err != nil {
		return domain.GitSnapshot{}, fmt.Errorf("persist git status: %w", err)
	}
	staged, err := writer.PutObject(ctx, snapshot.StagedPatch, "text/x-diff")
	if err != nil {
		return domain.GitSnapshot{}, fmt.Errorf("persist staged patch: %w", err)
	}
	unstaged, err := writer.PutObject(ctx, snapshot.UnstagedPatch, "text/x-diff")
	if err != nil {
		return domain.GitSnapshot{}, fmt.Errorf("persist unstaged patch: %w", err)
	}
	manifest := UntrackedManifest{Files: make([]UntrackedManifestFile, 0, len(snapshot.Untracked))}
	for _, file := range snapshot.Untracked {
		record := UntrackedManifestFile{
			Path:          file.Path,
			Size:          file.Size,
			Mode:          uint32(file.Mode),
			SHA256:        file.SHA256,
			Included:      file.Included,
			OmittedReason: file.OmittedReason,
			Symlink:       file.Symlink,
		}
		if file.Included {
			object, putErr := writer.PutObject(ctx, file.Content, "application/octet-stream")
			if putErr != nil {
				return domain.GitSnapshot{}, fmt.Errorf("persist untracked file %s: %w", file.Path, putErr)
			}
			record.ObjectHash = object.Hash
		}
		manifest.Files = append(manifest.Files, record)
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return domain.GitSnapshot{}, fmt.Errorf("encode untracked manifest: %w", err)
	}
	manifestObject, err := writer.PutObject(ctx, manifestJSON, "application/vnd.teamcross.untracked+json")
	if err != nil {
		return domain.GitSnapshot{}, fmt.Errorf("persist untracked manifest: %w", err)
	}
	return domain.GitSnapshot{
		ThreadID:                threadID,
		Head:                    snapshot.Head,
		Branch:                  snapshot.Branch,
		Unborn:                  snapshot.Unborn,
		StatusObject:            status.Hash,
		StagedPatchObject:       staged.Hash,
		UnstagedPatchObject:     unstaged.Hash,
		UntrackedManifestObject: manifestObject.Hash,
		CreatedAt:               time.Now().UTC(),
	}, nil
}

// LoadSnapshot reconstructs the capture needed to recreate an isolated
// worktree after a host restart.
func LoadSnapshot(ctx context.Context, reader ObjectReader, stored domain.GitSnapshot, repoRoot string) (Snapshot, error) {
	status, err := reader.GetObject(ctx, stored.StatusObject)
	if err != nil {
		return Snapshot{}, fmt.Errorf("load git status: %w", err)
	}
	staged, err := reader.GetObject(ctx, stored.StagedPatchObject)
	if err != nil {
		return Snapshot{}, fmt.Errorf("load staged patch: %w", err)
	}
	unstaged, err := reader.GetObject(ctx, stored.UnstagedPatchObject)
	if err != nil {
		return Snapshot{}, fmt.Errorf("load unstaged patch: %w", err)
	}
	manifestJSON, err := reader.GetObject(ctx, stored.UntrackedManifestObject)
	if err != nil {
		return Snapshot{}, fmt.Errorf("load untracked manifest: %w", err)
	}
	var manifest UntrackedManifest
	if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
		return Snapshot{}, fmt.Errorf("decode untracked manifest: %w", err)
	}
	snapshot := Snapshot{
		RepoRoot:      repoRoot,
		Head:          stored.Head,
		Branch:        stored.Branch,
		Unborn:        stored.Unborn,
		Status:        status,
		StagedPatch:   staged,
		UnstagedPatch: unstaged,
		Untracked:     make([]UntrackedFile, 0, len(manifest.Files)),
	}
	for _, record := range manifest.Files {
		file := UntrackedFile{
			Path:          record.Path,
			Size:          record.Size,
			Mode:          os.FileMode(record.Mode),
			SHA256:        record.SHA256,
			Included:      record.Included,
			OmittedReason: record.OmittedReason,
			Symlink:       record.Symlink,
		}
		if record.Included {
			file.Content, err = reader.GetObject(ctx, record.ObjectHash)
			if err != nil {
				return Snapshot{}, fmt.Errorf("load untracked file %s: %w", record.Path, err)
			}
		}
		snapshot.Untracked = append(snapshot.Untracked, file)
	}
	return snapshot, nil
}
