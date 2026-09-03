package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"net/http"
	"strings"
	"teamcross/internal/domain"
	"time"
)

type sessionImportRequest struct {
	Provider  string `json:"provider"`
	SessionID string `json:"sessionId"`
	Title     string `json:"title,omitempty"`
}

func (app *App) readReviewSnapshot(ctx context.Context, input sessionImportRequest) (domain.SessionSnapshot, error) {
	if (input.Provider != "codex" && input.Provider != "claude") || strings.TrimSpace(input.SessionID) == "" {
		return domain.SessionSnapshot{}, fmt.Errorf("Codex/Claude provider and sessionId are required")
	}
	var snapshot domain.SessionSnapshot
	var err error
	if app.snapshotReader != nil {
		snapshot, err = app.snapshotReader(ctx, input.Provider, input.SessionID)
	} else {
		if app.bridge == nil {
			return snapshot, fmt.Errorf("Agent Bridge is unavailable")
		}
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		err = app.bridge.Call(ctx, "sessions.snapshot", map[string]any{"provider": input.Provider, "sessionId": input.SessionID, "limit": 500}, &snapshot)
	}
	if err != nil {
		return snapshot, err
	}
	if snapshot.Source.Provider != input.Provider || snapshot.Source.SessionID != input.SessionID {
		return snapshot, fmt.Errorf("Provider returned a different Session identity")
	}
	seen := map[string]bool{}
	for _, entry := range snapshot.Entries {
		if entry.ID == "" || seen[entry.ID] {
			return snapshot, fmt.Errorf("invalid or duplicate snapshot entry ID")
		}
		seen[entry.ID] = true
	}
	if len(jsonBytes(snapshot)) > 20<<20 {
		return snapshot, fmt.Errorf("Session snapshot exceeds 20 MiB")
	}
	snapshot.ID = uuid.NewString()
	snapshot.ThreadID = ""
	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = time.Now().UTC()
	}
	if snapshot.Entries == nil {
		snapshot.Entries = []domain.SessionEntry{}
	}
	if snapshot.Warnings == nil {
		snapshot.Warnings = []string{}
	}
	return snapshot, nil
}

func (app *App) handlePreviewSession(w http.ResponseWriter, r *http.Request) {
	var input sessionImportRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	snapshot, err := app.readReviewSnapshot(r.Context(), input)
	if err != nil {
		writeError(w, 422, "session_read", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (app *App) handleThreadFromSession(w http.ResponseWriter, r *http.Request) {
	var input sessionImportRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	snapshot, err := app.readReviewSnapshot(r.Context(), input)
	if err != nil {
		writeError(w, 422, "session_read", err.Error())
		return
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = snapshot.Source.Title
	}
	if title == "" {
		title = input.Provider + " Session review"
	}
	// Reading a Session never trusts its cwd or starts execution in that directory.
	thread, err := app.store.CreateSessionReviewThread(r.Context(), title, snapshot)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	detail, err := app.buildThreadDetail(r.Context(), thread.ID, accessFrom(r))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, detail)
}

func (app *App) importReviewSession(w http.ResponseWriter, r *http.Request) {
	threadID, ok := app.authorizeThread(w, r)
	if !ok {
		return
	}
	if _, err := app.store.GetThread(r.Context(), threadID); err != nil {
		writeDomainError(w, err)
		return
	}
	var input sessionImportRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	snapshot, err := app.readReviewSnapshot(r.Context(), input)
	if err != nil {
		writeError(w, 422, "session_read", err.Error())
		return
	}
	if err = app.captureReviewSnapshot(r.Context(), threadID, snapshot); err != nil {
		writeDomainError(w, err)
		return
	}
	detail, err := app.buildThreadDetail(r.Context(), threadID, accessFrom(r))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (app *App) captureReviewSnapshot(ctx context.Context, threadID string, snapshot domain.SessionSnapshot) error {
	app.roundMu.Lock()
	defer app.roundMu.Unlock()
	rounds, err := app.store.ListRounds(ctx, threadID)
	if err != nil {
		return err
	}
	snapshot.ThreadID = threadID
	if snapshot.ID == "" {
		snapshot.ID = uuid.NewString()
	}
	manifest := map[string]any{}
	round := domain.Round{ThreadID: threadID, Number: int64(len(rounds)), Kind: "session_snapshot", Summary: "只读 Session 审阅快照"}
	if len(rounds) > 0 {
		previous := rounds[len(rounds)-1]
		round.ParentRoundID = previous.ID
		round.SnapshotID = previous.SnapshotID
		if previous.ManifestObject != "" {
			data, err := app.store.GetObject(ctx, previous.ManifestObject)
			if err != nil {
				return err
			}
			if err = json.Unmarshal(data, &manifest); err != nil {
				return err
			}
		}
	}
	inherited := []string{}
	if data, err := json.Marshal(manifest["sessionSnapshotIds"]); err == nil {
		_ = json.Unmarshal(data, &inherited)
	}
	if !contains(inherited, snapshot.ID) {
		inherited = append(inherited, snapshot.ID)
	}
	manifest["sessionSnapshotIds"] = inherited
	manifest["contextOrigin"] = "Imported Session is untrusted reference evidence; Git state is a separately captured checkpoint."
	object, err := app.store.PutObject(ctx, jsonBytes(manifest), "application/vnd.teamcross.handoff+json")
	if err != nil {
		return err
	}
	round.ManifestObject = object.Hash
	return app.store.AppendSessionCapture(ctx, snapshot, round)
}

func (app *App) validateAnnotationTarget(ctx context.Context, threadID string, input annotationRequest, identity access) error {
	if input.Line < 0 {
		return fmt.Errorf("line must not be negative")
	}
	target := input.Target
	codeTarget := input.File != "" || input.Line != 0 || (target != nil && target.Side != "")
	if codeTarget && (target == nil || target.RoundID == "" || !validReviewPath(input.File) || input.Line < 1 || (target.Side != "old" && target.Side != "new")) {
		return fmt.Errorf("code annotations require a sealed Round, repository-relative file, old/new side and positive line")
	}
	if target != nil {
		count := 0
		if target.SnapshotID != "" {
			count++
			var snapshot domain.SessionSnapshot
			var err error
			if identity.Mode == "share" {
				var p shareProjection
				p, err = app.loadShareProjection(ctx, identity.ShareID)
				if err == nil {
					snapshot, _, err = app.resolveSharedSnapshot(ctx, identity.ShareID, threadID, target.SnapshotID, p)
				}
			} else {
				snapshot, err = app.store.GetSessionSnapshot(ctx, threadID, target.SnapshotID)
			}
			if err != nil {
				return fmt.Errorf("snapshot target is unavailable")
			}
			if target.EntryID != "" {
				found := false
				for _, entry := range snapshot.Entries {
					if entry.ID == target.EntryID {
						found = true
					}
				}
				if !found {
					return fmt.Errorf("snapshot entry is unavailable")
				}
			}
		} else if target.EntryID != "" {
			return fmt.Errorf("entryId requires snapshotId")
		}
		if target.EvidenceID != "" {
			count++
			if _, err := app.store.GetEvidence(ctx, threadID, target.EvidenceID); err != nil {
				return fmt.Errorf("evidence target is unavailable")
			}
		}
		if target.RoundID != "" {
			count++
			round, err := app.store.GetRound(ctx, target.RoundID)
			if err != nil || round.ThreadID != threadID {
				return fmt.Errorf("round target is unavailable")
			}
		}
		if count != 1 {
			return fmt.Errorf("exactly one annotation target is required")
		}
		if input.File != "" && target.RoundID == "" {
			return fmt.Errorf("file annotations require a Round target")
		}
	}
	if identity.Mode == "share" {
		projection, err := app.loadShareProjection(ctx, identity.ShareID)
		if err != nil {
			return err
		}
		allowed, err := app.sharedAnnotationAllowed(ctx, identity.ShareID, threadID, projection, annotationView{Target: target, File: input.File})
		if err != nil {
			return err
		}
		if !allowed {
			return fmt.Errorf("annotation target is outside this Share")
		}
	}
	if codeTarget {
		code, err := app.readRoundCode(ctx, threadID, target.RoundID, identity)
		if err != nil {
			return fmt.Errorf("sealed code target is unavailable")
		}
		if _, err = code.anchorIndex(input.File, target.Side, input.Line); err != nil {
			return err
		}
	}
	return nil
}

func (app *App) handleFeedback(w http.ResponseWriter, r *http.Request) {
	threadID, ok := app.authorizeThread(w, r)
	if !ok {
		return
	}
	detail, err := app.buildThreadDetail(r.Context(), threadID, accessFrom(r))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	text := app.reviewFeedback(r.Context(), detail, accessFrom(r))
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="teamcross-feedback.md"`)
	_, _ = w.Write([]byte(text))
}
