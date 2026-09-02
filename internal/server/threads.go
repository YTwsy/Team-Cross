package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"teamcross/internal/domain"
	"teamcross/internal/gitstate"
)

func (app *App) handleCapturePreview(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Repo string `json:"repo"`
	}
	if !decodeJSON(response, request, &input) {
		return
	}
	if strings.TrimSpace(input.Repo) == "" {
		input.Repo = app.config.Repo
	}
	snapshot, err := gitstate.Capture(request.Context(), input.Repo, nil)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, "git_capture", err.Error())
		return
	}
	files := untrackedMetadata(snapshot.RepoRoot, snapshot.Status)
	writeJSON(response, http.StatusOK, capturePreview{
		Repo: snapshot.RepoRoot, Branch: snapshot.Branch, Head: snapshot.Head,
		Unborn: snapshot.Unborn, Status: printableStatus(snapshot.Status), Untracked: files,
	})
}

func untrackedMetadata(root string, status []byte) []gitFile {
	files := make([]gitFile, 0)
	for _, record := range bytes.Split(status, []byte{0}) {
		if !bytes.HasPrefix(record, []byte("? ")) {
			continue
		}
		path := string(record[2:])
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			continue
		}
		files = append(files, gitFile{Path: path, Size: info.Size()})
	}
	return files
}

func printableStatus(status []byte) string {
	return strings.TrimSpace(strings.ReplaceAll(string(status), "\x00", "\n"))
}

func (app *App) handleCreateThread(response http.ResponseWriter, request *http.Request) {
	var input createThreadRequest
	if !decodeJSON(response, request, &input) {
		return
	}
	if err := requiredString(input.Title, "title"); err != nil {
		writeError(response, http.StatusBadRequest, "missing_field", err.Error())
		return
	}
	if err := requiredString(input.Goal, "goal"); err != nil {
		writeError(response, http.StatusBadRequest, "missing_field", err.Error())
		return
	}
	if strings.TrimSpace(input.Repo) == "" {
		input.Repo = app.config.Repo
	}
	snapshot, err := gitstate.Capture(request.Context(), input.Repo, input.Untracked)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, "git_capture", err.Error())
		return
	}
	thread, err := app.store.CreateThread(request.Context(), domain.Thread{
		Title:          strings.TrimSpace(input.Title),
		RepoRoot:       snapshot.RepoRoot,
		BaselineCommit: snapshot.Head,
		Branch:         snapshot.Branch,
		ReadOnly:       snapshot.Unborn,
	})
	if err != nil {
		writeDomainError(response, err)
		return
	}
	statusObject, err := app.store.PutObject(request.Context(), snapshot.Status, "application/x-git-status")
	if err != nil {
		writeDomainError(response, err)
		return
	}
	stagedObject, err := app.store.PutObject(request.Context(), snapshot.StagedPatch, "application/x-git-diff")
	if err != nil {
		writeDomainError(response, err)
		return
	}
	unstagedObject, err := app.store.PutObject(request.Context(), snapshot.UnstagedPatch, "application/x-git-diff")
	if err != nil {
		writeDomainError(response, err)
		return
	}
	untrackedBytes, _ := json.Marshal(snapshot.Untracked)
	untrackedObject, err := app.store.PutObject(request.Context(), untrackedBytes, "application/vnd.teamcross.untracked+json")
	if err != nil {
		writeDomainError(response, err)
		return
	}
	gitSnapshot, err := app.store.CreateGitSnapshot(request.Context(), domain.GitSnapshot{
		ThreadID:                thread.ID,
		Head:                    snapshot.Head,
		Branch:                  snapshot.Branch,
		Unborn:                  snapshot.Unborn,
		StatusObject:            statusObject.Hash,
		StagedPatchObject:       stagedObject.Hash,
		UnstagedPatchObject:     unstagedObject.Hash,
		UntrackedManifestObject: untrackedObject.Hash,
	})
	if err != nil {
		writeDomainError(response, err)
		return
	}
	manifest := handoffManifest{Goal: input.Goal, Progress: input.Progress, Blocker: input.Blocker, Tried: input.Tried, Questions: input.Questions}
	manifestObject, err := app.store.PutObject(request.Context(), jsonBytes(manifest), "application/vnd.teamcross.handoff+json")
	if err != nil {
		writeDomainError(response, err)
		return
	}
	if _, err := app.store.CreateRound(request.Context(), domain.Round{
		ThreadID: thread.ID, Number: 0, Kind: "capture", Summary: "Initial Git and handoff capture",
		SnapshotID: gitSnapshot.ID, ManifestObject: manifestObject.Hash,
	}); err != nil {
		writeDomainError(response, err)
		return
	}
	if !snapshot.Unborn {
		worktree := filepath.Join(app.paths.Worktrees, thread.ID)
		if err := gitstate.CreateWorktree(request.Context(), snapshot, worktree); err != nil {
			writeError(response, http.StatusInternalServerError, "worktree", err.Error())
			return
		}
		thread, err = app.store.SetThreadWorktree(request.Context(), thread.ID, worktree, thread.Revision)
		if err != nil {
			writeDomainError(response, err)
			return
		}
	}
	if _, err := app.store.AppendEvent(request.Context(), thread.ID, "thread.created", jsonBytes(map[string]any{"actor": "Owner", "revision": thread.Revision + 1})); err != nil {
		writeDomainError(response, err)
		return
	}
	detail, err := app.buildThreadDetail(request.Context(), thread.ID, accessFrom(request))
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, detail)
}

func (app *App) handleListThreads(response http.ResponseWriter, request *http.Request) {
	identity := accessFrom(request)
	threads, err := app.store.ListThreads(request.Context())
	if err != nil {
		writeDomainError(response, err)
		return
	}
	result := make([]threadSummary, 0, len(threads))
	for _, thread := range threads {
		if identity.Mode == "share" && thread.ID != identity.ThreadID {
			continue
		}
		result = append(result, app.summarizeThread(thread, identity))
	}
	writeJSON(response, http.StatusOK, result)
}

func (app *App) summarizeThread(thread domain.Thread, identity access) threadSummary {
	repo := thread.RepoRoot
	if identity.Mode == "share" {
		repo = filepath.Base(repo)
	}
	status := "ready"
	provider := ""
	app.mu.RLock()
	run := app.runs[thread.ID]
	shared := app.shares[thread.ID]
	if run != nil {
		provider = run.Provider
		if run.Status == "running" || run.Status == "waiting" {
			status = "running"
		}
	}
	if shared != nil && status == "ready" {
		status = "shared"
	}
	app.mu.RUnlock()
	if thread.ReadOnly {
		status = "read-only"
	}
	return threadSummary{ID: thread.ID, Title: thread.Title, Repo: repo, Branch: thread.Branch, Status: status, UpdatedAt: thread.UpdatedAt, Provider: provider}
}

func (app *App) handleGetThread(response http.ResponseWriter, request *http.Request) {
	threadID, ok := app.authorizeThread(response, request)
	if !ok {
		return
	}
	detail, err := app.buildThreadDetail(request.Context(), threadID, accessFrom(request))
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, detail)
}

func (app *App) buildThreadDetail(ctx context.Context, threadID string, identity access) (threadDetail, error) {
	thread, err := app.store.GetThread(ctx, threadID)
	if err != nil {
		return threadDetail{}, err
	}
	rounds, err := app.store.ListRounds(ctx, threadID)
	if err != nil {
		return threadDetail{}, err
	}
	manifest := handoffManifest{}
	git := gitView{Head: thread.BaselineCommit, Branch: thread.Branch, Unborn: thread.ReadOnly, Untracked: make([]gitFile, 0)}
	if len(rounds) > 0 {
		if rounds[0].ManifestObject != "" {
			if value, objectErr := app.store.GetObject(ctx, rounds[0].ManifestObject); objectErr == nil {
				_ = json.Unmarshal(value, &manifest)
			}
		}
		if rounds[0].SnapshotID != "" {
			if snapshot, snapshotErr := app.store.GetGitSnapshot(ctx, rounds[0].SnapshotID); snapshotErr == nil {
				git.Head, git.Branch, git.Unborn = snapshot.Head, snapshot.Branch, snapshot.Unborn
				git.Status = string(app.objectOrEmpty(ctx, snapshot.StatusObject))
				git.StagedPatch = string(app.objectOrEmpty(ctx, snapshot.StagedPatchObject))
				git.UnstagedPatch = string(app.objectOrEmpty(ctx, snapshot.UnstagedPatchObject))
				var files []gitstate.UntrackedFile
				_ = json.Unmarshal(app.objectOrEmpty(ctx, snapshot.UntrackedManifestObject), &files)
				for _, file := range files {
					git.Untracked = append(git.Untracked, gitFile{Path: file.Path, Size: file.Size, Captured: file.Included, Reason: file.OmittedReason})
				}
			}
		}
	}
	if !thread.ReadOnly && thread.WorktreePath != "" {
		if patch, patchErr := gitstate.ExportBinaryPatch(ctx, thread.WorktreePath, thread.BaselineCommit); patchErr == nil {
			git.FinalPatch = string(patch)
		}
	}
	detail := threadDetail{
		threadSummary: app.summarizeThread(thread, identity), Revision: thread.Revision,
		Worktree: thread.WorktreePath, Goal: manifest.Goal, Progress: manifest.Progress,
		Blocker: manifest.Blocker, Tried: manifest.Tried, Questions: manifest.Questions, Git: git,
		Rounds: make([]roundView, 0), Events: make([]eventView, 0),
		Annotations: make([]annotationView, 0), Evidence: make([]evidenceView, 0),
		Participants: make([]participantView, 0),
	}
	if identity.Mode == "share" {
		detail.Worktree = "Isolated worktree on host"
	}
	for _, round := range rounds {
		view := roundView{ID: round.ID, Sequence: round.Number, Summary: round.Summary, CreatedAt: round.CreatedAt}
		if round.AgentRunID != "" {
			view.Provider = app.providerForRun(round.AgentRunID)
		}
		detail.Rounds = append(detail.Rounds, view)
	}
	events, err := app.store.EventsAfter(ctx, threadID, 0, 10_000)
	if err != nil {
		return threadDetail{}, err
	}
	for _, event := range events {
		detail.Events = append(detail.Events, convertEvent(event))
	}
	annotations, err := app.store.ListAnnotations(ctx, threadID)
	if err != nil {
		return threadDetail{}, err
	}
	participantNames := app.participantNames(ctx, threadID)
	for _, annotation := range annotations {
		author := "Owner"
		if name := participantNames[annotation.ParticipantID]; name != "" {
			author = name
		}
		detail.Annotations = append(detail.Annotations, annotationView{ID: annotation.ID, Author: author, Body: annotation.Body, File: annotation.Path, Line: annotation.StartLine, CreatedAt: annotation.CreatedAt})
	}
	evidence, err := app.store.ListEvidence(ctx, threadID)
	if err != nil {
		return threadDetail{}, err
	}
	for _, item := range evidence {
		var size int64
		var metadata struct {
			MIMEType string `json:"mimeType"`
		}
		_ = json.Unmarshal(item.Metadata, &metadata)
		if item.ObjectHash != "" {
			if value, objectErr := app.store.GetObject(ctx, item.ObjectHash); objectErr == nil {
				size = int64(len(value))
			}
		}
		detail.Evidence = append(detail.Evidence, evidenceView{
			ID: item.ID, Kind: item.Kind, Name: item.Title, Size: size,
			MIMEType: metadata.MIMEType, Source: item.Source, CreatedAt: item.CreatedAt,
		})
	}
	app.attachEphemeralState(ctx, &detail, identity)
	return detail, nil
}

func (app *App) objectOrEmpty(ctx context.Context, hash string) []byte {
	if hash == "" {
		return nil
	}
	value, _ := app.store.GetObject(ctx, hash)
	return value
}

func convertEvent(event domain.Event) eventView {
	payload := map[string]any{}
	_ = json.Unmarshal(event.Payload, &payload)
	actor, _ := payload["actor"].(string)
	delete(payload, "actor")
	return eventView{Seq: event.Seq, Type: event.Type, Actor: actor, Payload: payload, CreatedAt: event.CreatedAt}
}

func (app *App) providerForRun(runID string) string {
	app.mu.RLock()
	run := app.runsByID[runID]
	app.mu.RUnlock()
	if run != nil {
		return run.Provider
	}
	if stored, err := app.store.GetAgentRun(context.Background(), runID); err == nil {
		return stored.Provider
	}
	return ""
}

func (app *App) participantNames(ctx context.Context, threadID string) map[string]string {
	result := map[string]string{}
	participants, _ := app.store.ListThreadParticipants(ctx, threadID)
	for _, participant := range participants {
		if result[participant.ID] == "" {
			result[participant.ID] = participant.Name
		}
	}
	return result
}

func (app *App) attachEphemeralState(ctx context.Context, detail *threadDetail, identity access) {
	app.mu.RLock()
	run := app.runs[detail.ID]
	state := app.shares[detail.ID]
	if run != nil {
		copyRun := *run
		detail.AgentRun = &agentRunView{ID: copyRun.ID, Provider: copyRun.Provider, Status: copyRun.Status, SessionID: copyRun.SessionID, TurnID: copyRun.TurnID, NetworkEnabled: copyRun.NetworkEnabled}
	}
	app.mu.RUnlock()
	if state == nil || !time.Now().Before(state.ExpiresAt) {
		return
	}
	detail.Share = &shareView{ID: state.ID, Invite: state.Token, ExpiresAt: state.ExpiresAt, Status: "active", Transports: append([]string(nil), state.Transports...)}
	participants, _ := app.store.ListParticipants(ctx, state.ID)
	lease, _ := app.store.GetControlLease(ctx, state.ID)
	now := time.Now()
	leaseActive := lease.ParticipantID != "" && lease.ExpiresAt.After(now)
	for _, participant := range participants {
		role := "observer"
		if leaseActive && participant.ID == lease.ParticipantID {
			role = "controller"
		}
		detail.Participants = append(detail.Participants, participantView{ID: participant.ID, Name: participant.Name, Role: role, Transport: participantTransport(participant.Role), LastSeenAt: participant.LastSeen})
	}
	if leaseActive {
		detail.ControlLease = &leaseView{ParticipantID: lease.ParticipantID, Epoch: lease.Epoch, ExpiresAt: lease.ExpiresAt}
	}
	if identity.Mode == "share" && detail.Share != nil {
		detail.Share.Invite = ""
	}
}

func persistedParticipantRole(transport string) string {
	transport = strings.TrimSpace(strings.ToLower(transport))
	if transport == "" {
		return "observer"
	}
	return "observer|" + transport
}

func participantTransport(role string) string {
	const prefix = "observer|"
	if strings.HasPrefix(role, prefix) {
		return strings.TrimPrefix(role, prefix)
	}
	return ""
}

func (app *App) handlePatch(response http.ResponseWriter, request *http.Request) {
	threadID, ok := app.authorizeThread(response, request)
	if !ok {
		return
	}
	thread, err := app.store.GetThread(request.Context(), threadID)
	if err != nil {
		writeDomainError(response, err)
		return
	}
	if thread.ReadOnly || thread.WorktreePath == "" {
		writeError(response, http.StatusConflict, "unborn", "A baseline commit is required to export a patch")
		return
	}
	patch, err := gitstate.ExportBinaryPatch(request.Context(), thread.WorktreePath, thread.BaselineCommit)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "patch", err.Error())
		return
	}
	response.Header().Set("Content-Type", "application/octet-stream")
	response.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", "teamcross-"+thread.ID+".patch"))
	_, _ = response.Write(patch)
}
