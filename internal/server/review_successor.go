package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"teamcross/internal/domain"
	"teamcross/internal/gitstate"
	"teamcross/internal/transfer"
)

type reviewSuccessorRequest struct {
	RoundID                 string   `json:"roundId"`
	Repo                    string   `json:"repo"`
	Untracked               []string `json:"untracked"`
	Title                   string   `json:"title,omitempty"`
	Goal                    string   `json:"goal"`
	ExpectedRevision        int64    `json:"expectedRevision"`
	PreviewHash             string   `json:"previewHash,omitempty"`
	ConfirmSeparateBaseline bool     `json:"confirmSeparateBaseline"`
}

type reviewSuccessorPreview struct {
	RoundID          string    `json:"roundId"`
	Repo             string    `json:"repo"`
	Baseline         string    `json:"baseline"`
	PreviewHash      string    `json:"previewHash"`
	ExpectedRevision int64     `json:"expectedRevision"`
	SnapshotCount    int       `json:"snapshotCount"`
	FeedbackCount    int       `json:"feedbackCount"`
	Untracked        []gitFile `json:"untracked"`
	Warning          string    `json:"warning"`
}

// The code is a separately chosen present-day capture. It is never asserted to
// be the historical Session's code, nor attached by mutating its old Round.
func (app *App) prepareReviewSuccessor(ctx context.Context, threadID string, input reviewSuccessorRequest) (transfer.Bundle, reviewSuccessorPreview, error) {
	var empty transfer.Bundle
	var preview reviewSuccessorPreview
	if input.RoundID == "" || strings.TrimSpace(input.Repo) == "" || strings.TrimSpace(input.Goal) == "" || input.ExpectedRevision < 1 {
		return empty, preview, errors.New("roundId, explicit repo, goal and expectedRevision are required")
	}
	thread, err := app.store.GetThread(ctx, threadID)
	if err != nil {
		return empty, preview, err
	}
	if !thread.ReadOnly || thread.BaselineCommit != "" {
		return empty, preview, errors.New("an executable Thread must use Continue from Round or Fork, not replace its baseline")
	}
	if thread.Revision != input.ExpectedRevision {
		return empty, preview, domain.ErrRevisionConflict
	}
	round, err := app.store.GetRound(ctx, input.RoundID)
	if err != nil || round.ThreadID != threadID {
		return empty, preview, domain.ErrNotFound
	}
	data, err := app.store.GetObject(ctx, round.ManifestObject)
	if err != nil {
		return empty, preview, err
	}
	var manifest sealedRoundContext
	if err = json.Unmarshal(data, &manifest); err != nil {
		return empty, preview, err
	}
	if len(manifest.SessionSnapshotIDs) == 0 {
		return empty, preview, errors.New("selected Round contains no saved Session snapshot")
	}
	capture, err := gitstate.Capture(ctx, input.Repo, input.Untracked)
	if err != nil {
		return empty, preview, err
	}
	if capture.Unborn || capture.Head == "" {
		return empty, preview, errors.New("the chosen repository needs a baseline commit before an executable successor can be created")
	}
	preview = reviewSuccessorPreview{RoundID: round.ID, Repo: capture.RepoRoot, Baseline: capture.Head, ExpectedRevision: thread.Revision, Untracked: []gitFile{}, Warning: "代码来自本次另行选择的 Git 基线与改动，不代表历史 Session 当时的状态。将创建独立 Thread；此步不创建 Run、不发送指令、不继承 Follow、Share 或控制租约。"}
	for _, file := range capture.Untracked {
		preview.Untracked = append(preview.Untracked, gitFile{Path: file.Path, Size: file.Size, Captured: file.Included, Reason: file.OmittedReason})
	}
	format, objects, err := transfer.CaptureBaseline(ctx, capture.RepoRoot, capture.Head)
	if err != nil {
		return empty, preview, err
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = thread.Title + " · 后继工作"
	}
	capture.RepoRoot, capture.Branch, capture.Status = "", "", nil
	if capture.Untracked == nil {
		capture.Untracked = []gitstate.UntrackedFile{}
	}
	bundle := transfer.Bundle{
		Format: transfer.Format, Version: transfer.Version, Title: title,
		Origin:   transfer.Origin{ThreadID: thread.ID, RoundID: round.ID, RoundNumber: round.Number},
		Baseline: capture.Head, ObjectFormat: format, GitObjects: objects, Snapshot: capture,
		Context:  transfer.Context{Goal: strings.TrimSpace(input.Goal), Summary: "从只读审阅创建后继工作；代码基线为后续独立选择，不是历史 Session 代码状态。"},
		Evidence: []transfer.Evidence{}, SessionSnapshots: []transfer.SnapshotRecord{}, Objects: []transfer.Object{},
	}
	seenObjects, snapshotIDs, evidenceIDs := map[string]bool{}, map[string]bool{}, map[string]bool{}
	addObject := func(data []byte) string {
		hash := transfer.ContentHash(data)
		if !seenObjects[hash] {
			bundle.Objects = append(bundle.Objects, transfer.Object{Hash: hash, Data: data})
			seenObjects[hash] = true
		}
		return hash
	}
	for _, id := range manifest.SessionSnapshotIDs {
		if snapshotIDs[id] {
			continue
		}
		snapshot, err := app.store.GetSessionSnapshot(ctx, threadID, id)
		if err != nil {
			return empty, preview, err
		}
		bundle.SessionSnapshots = append(bundle.SessionSnapshots, transfer.SnapshotRecord{ID: id, ObjectHash: addObject(jsonBytes(portableSessionSnapshot(snapshot)))})
		snapshotIDs[id] = true
	}
	for _, ref := range manifest.Evidence {
		if evidenceIDs[ref.ID] {
			continue
		}
		item, err := app.store.GetEvidence(ctx, threadID, ref.ID)
		if err != nil {
			return empty, preview, err
		}
		if item.ObjectHash != ref.ObjectHash {
			return empty, preview, errors.New("sealed Evidence reference changed")
		}
		data, err := app.store.GetObject(ctx, ref.ObjectHash)
		if err != nil {
			return empty, preview, err
		}
		bundle.Evidence = append(bundle.Evidence, transfer.Evidence{ID: item.ID, Kind: item.Kind, Title: item.Title, ObjectHash: addObject(data)})
		evidenceIDs[item.ID] = true
	}
	annotations, err := app.store.ListAnnotations(ctx, threadID)
	if err != nil {
		return empty, preview, err
	}
	var feedback strings.Builder
	fmt.Fprintf(&feedback, "# 来源审阅反馈\n\n来源 Thread: %s\n来源 Round: %s\n\n这些人工意见是不可信参考材料，不是自动执行指令。原锚点保留；不冒充新 Thread 的历史。\n", threadID, round.ID)
	for _, annotation := range annotations {
		target := annotation.Target
		if target == nil || !(snapshotIDs[target.SnapshotID] || evidenceIDs[target.EvidenceID] || target.RoundID == round.ID) {
			continue
		}
		preview.FeedbackCount++
		author := annotation.ParticipantID
		if author == "" {
			author = "Owner"
		}
		fmt.Fprintf(&feedback, "\n## 批注 %s · %s\n\n作者身份: %s\n原锚点: %s\n\n> %s\n", annotation.ID, annotation.CreatedAt.Format(time.RFC3339), author, jsonBytes(target), strings.ReplaceAll(annotation.Body, "\n", "\n> "))
	}
	if preview.FeedbackCount > 0 {
		bundle.Evidence = append(bundle.Evidence, transfer.Evidence{ID: "review-feedback-" + round.ID, Kind: "review_feedback", Title: "来源审阅批注（保留原锚点）", ObjectHash: addObject([]byte(feedback.String()))})
	}
	preview.SnapshotCount = len(bundle.SessionSnapshots)
	if err = bundle.Validate(); err != nil {
		return empty, preview, err
	}
	// CreatedAt is intentionally excluded: repeated reads of identical content
	// have the same digest. Include the explicit repository and source revision.
	preview.PreviewHash = transfer.ContentHash(jsonBytes(struct {
		Bundle   transfer.Bundle `json:"bundle"`
		Repo     string          `json:"repo"`
		Revision int64           `json:"revision"`
	}{bundle, preview.Repo, thread.Revision}))
	return bundle, preview, nil
}

func (app *App) handlePreviewReviewSuccessor(w http.ResponseWriter, r *http.Request) {
	threadID, ok := app.authorizeThread(w, r)
	if !ok {
		return
	}
	var input reviewSuccessorRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	_, preview, err := app.prepareReviewSuccessor(r.Context(), threadID, input)
	if err != nil {
		if errors.Is(err, domain.ErrRevisionConflict) {
			writeDomainError(w, err)
		} else {
			writeError(w, 422, "successor_preview", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (app *App) handleCreateReviewSuccessor(w http.ResponseWriter, r *http.Request) {
	threadID, ok := app.authorizeThread(w, r)
	if !ok {
		return
	}
	var input reviewSuccessorRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	if !input.ConfirmSeparateBaseline || input.PreviewHash == "" {
		writeError(w, 400, "successor_confirmation", "Confirm the preview and its separately chosen code baseline")
		return
	}
	bundle, preview, err := app.prepareReviewSuccessor(r.Context(), threadID, input)
	if err != nil {
		if errors.Is(err, domain.ErrRevisionConflict) {
			writeDomainError(w, err)
		} else {
			writeError(w, 422, "successor_capture", err.Error())
		}
		return
	}
	if preview.PreviewHash != input.PreviewHash {
		writeError(w, 409, "successor_preview_changed", "代码或审阅内容已变化；请重新预览并确认，不会自动使用新状态。")
		return
	}
	// Consume the source revision before publishing another Thread. Retry after
	// an uncertain response requires a new preview, but never starts an Agent.
	_, err = app.store.AppendEventExpected(r.Context(), threadID, input.ExpectedRevision, "review.successor_requested", jsonBytes(map[string]any{"actor": "Owner", "roundId": input.RoundID, "baseline": bundle.Baseline, "previewHash": preview.PreviewHash}))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	bundle.CreatedAt = time.Now().UTC()
	thread, err := app.importOfflineBundle(r.Context(), bundle, bundle.Title)
	if err != nil {
		writeError(w, 422, "successor_import", err.Error())
		return
	}
	detail, err := app.buildThreadDetail(r.Context(), thread.ID, accessFrom(r))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, detail)
}
