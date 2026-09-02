package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"teamcross/internal/domain"
	"teamcross/internal/gitstate"
	"teamcross/internal/transfer"
)

type exportBundleRequest struct {
	RoundID       string   `json:"roundId"`
	EvidenceIDs   []string `json:"evidenceIds"`
	SnapshotIDs   []string `json:"snapshotIds"`
	ConfirmExport bool     `json:"confirmExport"`
}

type sealedRoundContext struct {
	transfer.Context
	PatchObject        string              `json:"patchObject,omitempty"`
	Evidence           []evidenceReference `json:"evidence,omitempty"`
	SessionSnapshotIDs []string            `json:"sessionSnapshotIds,omitempty"`
	SourceSnapshotIDs  map[string]string   `json:"sourceSnapshotIds,omitempty"`
	Origin             *transfer.Origin    `json:"origin,omitempty"`
}

func (app *App) handleExportBundle(response http.ResponseWriter, request *http.Request) {
	if accessFrom(request).Mode != "host" {
		writeError(response, http.StatusForbidden, "owner_only", "Only the host Owner may export an offline copy")
		return
	}
	threadID, ok := app.authorizeThread(response, request)
	if !ok {
		return
	}
	var input exportBundleRequest
	if !decodeJSON(response, request, &input) {
		return
	}
	if !input.ConfirmExport {
		writeError(response, http.StatusBadRequest, "export_confirmation", "Confirm export of baseline Git history and selected context; copies cannot be revoked")
		return
	}
	bundle, err := app.buildOfflineBundle(request.Context(), threadID, input.RoundID, input.EvidenceIDs, input.SnapshotIDs)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, "bundle_export", err.Error())
		return
	}
	// Validate restoration before handing over an apparently usable bundle.
	materialized, err := transfer.Materialize(request.Context(), bundle, app.paths.Worktrees)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, "bundle_restore", err.Error())
		return
	}
	defer materialized.Cleanup()
	response.Header().Set("Content-Type", "application/vnd.teamcross.bundle+json")
	response.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", "teamcross-"+threadID+".tcx.json"))
	_ = json.NewEncoder(response).Encode(bundle)
}

func (app *App) handleImportBundle(response http.ResponseWriter, request *http.Request) {
	if accessFrom(request).Mode != "host" {
		writeError(response, http.StatusForbidden, "owner_only", "Only the host Owner may import an offline copy")
		return
	}
	bundle, err := transfer.Decode(request.Body)
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_bundle", err.Error())
		return
	}
	thread, err := app.importOfflineBundle(request.Context(), bundle, "")
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, "bundle_import", err.Error())
		return
	}
	detail, err := app.buildThreadDetail(request.Context(), thread.ID, accessFrom(request))
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, detail)
}

// readSealedRound never reads the mutable worktree or fixed context alias.
func (app *App) readSealedRound(ctx context.Context, thread domain.Thread, round domain.Round) (gitstate.Snapshot, sealedRoundContext, error) {
	if round.ThreadID != thread.ID {
		return gitstate.Snapshot{}, sealedRoundContext{}, domain.ErrNotFound
	}
	if thread.BaselineCommit == "" || thread.ReadOnly {
		return gitstate.Snapshot{}, sealedRoundContext{}, errors.New("selected Round has no executable Git baseline")
	}
	manifest := sealedRoundContext{}
	if round.ManifestObject != "" {
		data, err := app.store.GetObject(ctx, round.ManifestObject)
		if err != nil {
			return gitstate.Snapshot{}, manifest, err
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			return gitstate.Snapshot{}, manifest, fmt.Errorf("read sealed Round manifest: %w", err)
		}
	}
	manifest.Summary = round.Summary
	snapshot := gitstate.Snapshot{Head: thread.BaselineCommit, Untracked: []gitstate.UntrackedFile{}}
	if manifest.PatchObject != "" {
		patch, err := app.store.GetObject(ctx, manifest.PatchObject)
		if err != nil {
			return snapshot, manifest, err
		}
		snapshot.UnstagedPatch = patch
		return snapshot, manifest, nil
	}
	if round.SnapshotID == "" {
		return snapshot, manifest, errors.New("selected Round has no immutable Git snapshot; current worktree is not a substitute")
	}
	stored, err := app.store.GetGitSnapshot(ctx, round.SnapshotID)
	if err != nil {
		return snapshot, manifest, err
	}
	if stored.ThreadID != thread.ID || stored.Head != thread.BaselineCommit || stored.Unborn {
		return snapshot, manifest, errors.New("sealed snapshot baseline mismatch")
	}
	for _, item := range []struct {
		hash        string
		destination *[]byte
	}{{stored.StagedPatchObject, &snapshot.StagedPatch}, {stored.UnstagedPatchObject, &snapshot.UnstagedPatch}} {
		if item.hash == "" {
			return snapshot, manifest, errors.New("sealed snapshot patch object is missing")
		}
		data, err := app.store.GetObject(ctx, item.hash)
		if err != nil {
			return snapshot, manifest, err
		}
		*item.destination = data
	}
	if stored.UntrackedManifestObject != "" {
		data, err := app.store.GetObject(ctx, stored.UntrackedManifestObject)
		if err != nil {
			return snapshot, manifest, err
		}
		if err := json.Unmarshal(data, &snapshot.Untracked); err != nil {
			return snapshot, manifest, err
		}
	}
	return snapshot, manifest, nil
}

func (app *App) buildOfflineBundle(ctx context.Context, threadID, roundID string, evidenceIDs, snapshotIDs []string) (transfer.Bundle, error) {
	thread, err := app.store.GetThread(ctx, threadID)
	if err != nil {
		return transfer.Bundle{}, err
	}
	round, err := app.store.GetRound(ctx, roundID)
	if err != nil {
		return transfer.Bundle{}, err
	}
	snapshot, manifest, err := app.readSealedRound(ctx, thread, round)
	if err != nil {
		return transfer.Bundle{}, err
	}
	format, objects, err := transfer.CaptureBaseline(ctx, thread.RepoRoot, thread.BaselineCommit)
	if err != nil {
		return transfer.Bundle{}, err
	}
	bundle := transfer.Bundle{
		Format: transfer.Format, Version: transfer.Version, CreatedAt: time.Now().UTC(), Title: thread.Title,
		Origin:   transfer.Origin{ThreadID: thread.ID, RoundID: round.ID, RoundNumber: round.Number},
		Baseline: thread.BaselineCommit, ObjectFormat: format, GitObjects: objects, Snapshot: snapshot, Context: manifest.Context,
		Evidence: []transfer.Evidence{}, SessionSnapshots: []transfer.SnapshotRecord{}, Objects: []transfer.Object{},
	}
	seenIDs, seenObjects := map[string]bool{}, map[string]bool{}
	addObject := func(data []byte) string {
		hash := transfer.ContentHash(data)
		if !seenObjects[hash] {
			bundle.Objects = append(bundle.Objects, transfer.Object{Hash: hash, Data: data})
			seenObjects[hash] = true
		}
		return hash
	}
	for _, id := range evidenceIDs {
		if seenIDs["e:"+id] {
			return bundle, errors.New("duplicate selected Evidence")
		}
		seenIDs["e:"+id] = true
		item, err := app.store.GetEvidence(ctx, thread.ID, id)
		if err != nil {
			return bundle, err
		}
		data, err := app.store.GetObject(ctx, item.ObjectHash)
		if err != nil {
			return bundle, err
		}
		bundle.Evidence = append(bundle.Evidence, transfer.Evidence{ID: item.ID, Kind: item.Kind, Title: item.Title, ObjectHash: addObject(data)})
	}
	for _, id := range snapshotIDs {
		if seenIDs["s:"+id] {
			return bundle, errors.New("duplicate selected Session snapshot")
		}
		seenIDs["s:"+id] = true
		item, err := app.store.GetSessionSnapshot(ctx, thread.ID, id)
		if err != nil {
			return bundle, err
		}
		item = portableSessionSnapshot(item)
		data, err := json.Marshal(item)
		if err != nil {
			return bundle, err
		}
		bundle.SessionSnapshots = append(bundle.SessionSnapshots, transfer.SnapshotRecord{ID: item.ID, ObjectHash: addObject(data)})
	}
	return bundle, bundle.Validate()
}

func portableSessionSnapshot(snapshot domain.SessionSnapshot) domain.SessionSnapshot {
	snapshot.ThreadID = ""
	snapshot.Source.Cwd = ""
	snapshot.Capabilities = domain.SessionCapabilities{Read: true, Reason: "Offline source evidence; native execution capabilities must be verified on the receiving host"}
	return snapshot
}

func decodePortableSessions(bundle transfer.Bundle) ([]domain.SessionSnapshot, error) {
	objects := map[string][]byte{}
	for _, object := range bundle.Objects {
		objects[object.Hash] = object.Data
	}
	sessions := make([]domain.SessionSnapshot, 0, len(bundle.SessionSnapshots))
	for _, reference := range bundle.SessionSnapshots {
		var snapshot domain.SessionSnapshot
		decoder := json.NewDecoder(bytes.NewReader(objects[reference.ObjectHash]))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&snapshot); err != nil {
			return nil, fmt.Errorf("decode portable Session snapshot: %w", err)
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return nil, errors.New("portable Session snapshot must contain one JSON value")
		}
		if snapshot.ID != reference.ID || snapshot.ThreadID != "" || snapshot.Source.Cwd != "" || snapshot.Source.SessionID == "" || snapshot.CapturedAt.IsZero() || len(snapshot.Entries) > 10000 {
			return nil, errors.New("invalid portable Session snapshot identity or size")
		}
		if snapshot.Source.Provider != "codex" && snapshot.Source.Provider != "claude" && snapshot.Source.Provider != "mock" {
			return nil, errors.New("unsupported source Provider")
		}
		ids := map[string]bool{}
		for _, entry := range snapshot.Entries {
			if entry.ID == "" || ids[entry.ID] || len(entry.Text) > 1<<20 {
				return nil, errors.New("invalid portable Session entry")
			}
			ids[entry.ID] = true
		}
		sessions = append(sessions, portableSessionSnapshot(snapshot))
	}
	return sessions, nil
}

func (app *App) importOfflineBundle(ctx context.Context, bundle transfer.Bundle, title string) (domain.Thread, error) {
	if err := bundle.Validate(); err != nil {
		return domain.Thread{}, err
	}
	sessions, err := decodePortableSessions(bundle)
	if err != nil {
		return domain.Thread{}, err
	}
	materialized, err := transfer.Materialize(ctx, bundle, app.paths.Worktrees)
	if err != nil {
		return domain.Thread{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = materialized.Cleanup()
		}
	}()
	if strings.TrimSpace(title) == "" {
		title = bundle.Title + " · Fork"
	}
	thread := domain.Thread{ID: uuid.NewString(), Title: strings.TrimSpace(title), RepoRoot: materialized.Repo, WorktreePath: materialized.Worktree, BaselineCommit: bundle.Baseline}
	round := domain.Round{ID: uuid.NewString(), ThreadID: thread.ID, Kind: "fork", Summary: bundle.Context.Summary}
	snapshot := domain.GitSnapshot{ID: uuid.NewString(), ThreadID: thread.ID, Head: bundle.Baseline}
	round.SnapshotID = snapshot.ID
	for _, item := range []struct {
		data   []byte
		mime   string
		target *string
	}{
		{[]byte{}, "application/x-git-status", &snapshot.StatusObject},
		{bundle.Snapshot.StagedPatch, "application/x-git-diff", &snapshot.StagedPatchObject},
		{bundle.Snapshot.UnstagedPatch, "application/x-git-diff", &snapshot.UnstagedPatchObject},
		{jsonBytes(bundle.Snapshot.Untracked), "application/vnd.teamcross.untracked+json", &snapshot.UntrackedManifestObject},
	} {
		object, err := app.store.PutObject(ctx, item.data, item.mime)
		if err != nil {
			return domain.Thread{}, err
		}
		*item.target = object.Hash
	}
	objectData := map[string][]byte{}
	for _, object := range bundle.Objects {
		if _, err := app.store.PutObject(ctx, object.Data, "application/octet-stream"); err != nil {
			return domain.Thread{}, err
		}
		objectData[object.Hash] = object.Data
	}
	evidence := make([]domain.Evidence, 0, len(bundle.Evidence))
	manifest := sealedRoundContext{Context: bundle.Context, Origin: &bundle.Origin, Evidence: []evidenceReference{}, SessionSnapshotIDs: []string{}}
	for _, item := range bundle.Evidence {
		id := uuid.NewString()
		evidence = append(evidence, domain.Evidence{ID: id, ThreadID: thread.ID, RoundID: round.ID, Kind: item.Kind, Title: item.Title, Source: "Thread " + bundle.Origin.ThreadID + " / Evidence " + item.ID, ObjectHash: item.ObjectHash, Metadata: jsonBytes(map[string]any{"mimeType": http.DetectContentType(objectData[item.ObjectHash]), "originEvidenceId": item.ID})})
		manifest.Evidence = append(manifest.Evidence, evidenceReference{ID: id, Kind: item.Kind, Title: item.Title, ObjectHash: item.ObjectHash})
	}
	for index := range sessions {
		sourceID := sessions[index].ID
		sessions[index].ID, sessions[index].ThreadID = uuid.NewString(), thread.ID
		manifest.SessionSnapshotIDs = append(manifest.SessionSnapshotIDs, sessions[index].ID)
		if manifest.SourceSnapshotIDs == nil {
			manifest.SourceSnapshotIDs = map[string]string{}
		}
		manifest.SourceSnapshotIDs[sessions[index].ID] = sourceID
	}
	object, err := app.store.PutObject(ctx, jsonBytes(manifest), "application/vnd.teamcross.fork-context+json")
	if err != nil {
		return domain.Thread{}, err
	}
	round.ManifestObject = object.Hash
	thread, err = app.store.CreateForkImport(ctx, thread, snapshot, round, evidence, sessions, jsonBytes(map[string]any{"actor": "Owner", "origin": bundle.Origin, "revision": 1, "offline": true}))
	if err != nil {
		return domain.Thread{}, err
	}
	committed = true
	return thread, nil
}
