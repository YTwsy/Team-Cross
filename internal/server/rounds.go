package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"teamcross/internal/domain"
)

type turnManifest struct {
	SessionSnapshotIDs []string            `json:"sessionSnapshotIds,omitempty"`
	Version            int                 `json:"version"`
	ThreadID           string              `json:"threadId"`
	RunID              string              `json:"runId"`
	Provider           string              `json:"provider"`
	TurnID             string              `json:"turnId,omitempty"`
	Status             string              `json:"status"`
	Summary            string              `json:"summary"`
	PatchObject        string              `json:"patchObject,omitempty"`
	CapturedPaths      []string            `json:"capturedPaths"`
	Files              []string            `json:"files"`
	Evidence           []evidenceReference `json:"evidence"`
	EventFromSeq       int64               `json:"eventFromSeq"`
	EventToSeq         int64               `json:"eventToSeq"`
	CreatedAt          time.Time           `json:"createdAt"`
}

// canSealRunLocked requires app.mu to be held. Only the current Writer can
// attribute the shared worktree to its Turn; delayed historical Run events
// remain events but cannot create a new code checkpoint.
func (app *App) canSealRunLocked(run *managedRun) bool {
	if run == nil {
		return false
	}
	current := app.runs[run.ThreadID]
	return current != nil && current.ID == run.ID && current.Provider == run.Provider &&
		current.Status != "archived" && current.Status != "closed" && !current.IdentityConflict
}

// sealCompletedTurn archives the deterministic state produced by one managed
// Agent turn. It never invokes a model, so completion of the original turn is
// not held open by summary generation.
func (app *App) sealCompletedTurn(run *managedRun, turnID string, completed domain.Event, data map[string]any) {
	app.roundMu.Lock()
	defer app.roundMu.Unlock()
	// A completion can wait behind a switch while roundMu is held. Recheck
	// after acquiring it rather than trusting the earlier event-time snapshot.
	app.mu.RLock()
	eligible := app.canSealRunLocked(run)
	app.mu.RUnlock()
	if !eligible {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	thread, err := app.store.GetThread(ctx, run.ThreadID)
	if err != nil || thread.ReadOnly || thread.WorktreePath == "" {
		return
	}
	rounds, err := app.store.ListRounds(ctx, thread.ID)
	if err != nil {
		app.logger.Warn("list rounds for completed turn", "error", err)
		return
	}
	fromSeq := int64(1)
	if len(rounds) > 0 && rounds[len(rounds)-1].EventToSeq > 0 {
		fromSeq = rounds[len(rounds)-1].EventToSeq + 1
	}
	if completed.Seq < fromSeq {
		return
	}
	summary := app.completedTurnSummary(ctx, thread.ID, fromSeq, completed.Seq, run.ID, turnID)
	status, _ := data["status"].(string)
	if status == "" {
		status = "completed"
	}
	if summary == "" {
		summary = fmt.Sprintf("%s turn %s (%s)", run.Provider, shortID(turnID), status)
	}
	patch, capturedPaths, err := app.captureThreadPatch(ctx, thread)
	if err != nil {
		app.logger.Warn("capture completed turn patch", "error", err)
		return
	}
	patchObject, err := app.store.PutObject(ctx, patch, "application/x-git-diff")
	if err != nil {
		app.logger.Warn("store completed turn patch", "error", err)
		return
	}
	evidence, err := app.store.ListEvidence(ctx, thread.ID)
	if err != nil {
		app.logger.Warn("list completed turn evidence", "error", err)
		return
	}
	references := make([]evidenceReference, 0, len(evidence))
	for _, item := range evidence {
		references = append(references, evidenceReference{ID: item.ID, Kind: item.Kind, Title: item.Title, ObjectHash: item.ObjectHash})
	}
	manifest := turnManifest{
		Version: 1, ThreadID: thread.ID, RunID: run.ID, Provider: run.Provider,
		TurnID: turnID, Status: status, Summary: summary, PatchObject: patchObject.Hash,
		Files: patchFiles(string(patch)), CapturedPaths: capturedPaths, Evidence: references,
		EventFromSeq: fromSeq, EventToSeq: completed.Seq, CreatedAt: time.Now().UTC(),
	}
	manifest.SessionSnapshotIDs, err = app.latestSealedSessionIDs(ctx, rounds)
	if err != nil {
		app.logger.Warn("read sealed Session context", "error", err)
		return
	}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
	object, err := app.store.PutObject(ctx, manifestBytes, "application/vnd.teamcross.context+json")
	if err != nil {
		app.logger.Warn("store completed turn manifest", "error", err)
		return
	}
	if err := os.WriteFile(app.contextManifestPath(thread.ID), manifestBytes, 0o600); err != nil {
		app.logger.Warn("write completed turn manifest", "error", err)
	}
	_, err = app.store.CreateRound(ctx, domain.Round{
		ThreadID: thread.ID, Number: int64(len(rounds)), Kind: "agent_turn",
		Summary: summary, AgentRunID: run.ID, EventFromSeq: fromSeq,
		EventToSeq: completed.Seq, ManifestObject: object.Hash,
	})
	if err != nil {
		app.logger.Warn("seal completed Agent turn", "error", err)
	}
}

func (app *App) latestSealedSessionIDs(ctx context.Context, rounds []domain.Round) ([]string, error) {
	if len(rounds) == 0 || rounds[len(rounds)-1].ManifestObject == "" {
		return nil, nil
	}
	data, err := app.store.GetObject(ctx, rounds[len(rounds)-1].ManifestObject)
	if err != nil {
		return nil, err
	}
	var manifest struct {
		SessionSnapshotIDs []string `json:"sessionSnapshotIds"`
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	return manifest.SessionSnapshotIDs, nil
}

func (app *App) completedTurnSummary(ctx context.Context, threadID string, fromSeq, toSeq int64, runID, turnID string) string {
	cursor := fromSeq - 1
	for cursor < toSeq {
		events, err := app.store.EventsAfter(ctx, threadID, cursor, 10_000)
		if err != nil || len(events) == 0 {
			return ""
		}
		for _, event := range events {
			if event.Seq > toSeq {
				return ""
			}
			cursor = event.Seq
			if event.Type != "message.completed" {
				continue
			}
			var payload map[string]any
			if json.Unmarshal(event.Payload, &payload) != nil || payload["runId"] != runID {
				continue
			}
			if turnID != "" && payload["turnId"] != turnID {
				continue
			}
			if text, ok := payload["text"].(string); ok {
				text = strings.TrimSpace(text)
				if len(text) > 4_000 {
					text = text[:4_000] + "…"
				}
				return text
			}
		}
	}
	return ""
}

func shortID(value string) string {
	if len(value) > 8 {
		return value[:8]
	}
	if value == "" {
		return "unknown"
	}
	return value
}
