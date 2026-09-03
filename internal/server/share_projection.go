package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"teamcross/internal/domain"
	"teamcross/internal/gitstate"
	"teamcross/internal/transfer"
	"time"
)

// A Share owns its immutable disclosure projection; later imports cannot widen it.
type shareProjection struct {
	LiveBinding         *domain.NativeLiveBinding `json:"-"`
	SnapshotFullyShared bool                      `json:"snapshotFullyShared"`
	SnapshotEntryCount  int                       `json:"snapshotEntryCount,omitempty"`
	Version             int                       `json:"version"`
	Scope               domain.ShareScope         `json:"scope"`
	AllowControl        bool                      `json:"allowControl"`
	CreatedAt           time.Time                 `json:"createdAt"`
	Detail              threadDetail              `json:"detail"`
	EvidenceHashes      map[string]string         `json:"evidenceHashes"`
	Legacy              bool                      `json:"-"`
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func (app *App) loadShareProjection(ctx context.Context, shareID string) (shareProjection, error) {
	data, err := app.store.GetShareProjection(ctx, shareID)
	if errors.Is(err, domain.ErrNotFound) {
		// Compatibility with v0 records. Production never restores their temporary listeners.
		share, err := app.store.GetShare(ctx, shareID)
		if err != nil {
			return shareProjection{}, err
		}
		var caps []string
		_ = json.Unmarshal(share.Capabilities, &caps)
		return shareProjection{Legacy: true, AllowControl: contains(caps, "send"), Scope: domain.ShareScope{IncludeCode: true, IncludeEvents: true}}, nil
	}
	if err != nil {
		return shareProjection{}, err
	}
	var p shareProjection
	if err = json.Unmarshal(data, &p); err != nil {
		return p, err
	}
	if p.Version != 1 {
		return p, fmt.Errorf("unsupported Share projection")
	}
	return p, nil
}

func (app *App) prepareShareProjection(ctx context.Context, threadID string, scope *domain.ShareScope, allowControl bool) (shareProjection, error) {
	detail, err := app.buildThreadDetail(ctx, threadID, access{Mode: "host", Role: "owner"})
	if err != nil {
		return shareProjection{}, err
	}
	p := shareProjection{Version: 1, CreatedAt: time.Now().UTC(), AllowControl: allowControl, Detail: detail, EvidenceHashes: map[string]string{}}
	if scope != nil {
		p.Scope = *scope
	}
	if p.Scope.EntryIDs == nil {
		p.Scope.EntryIDs = []string{}
	}
	if p.Scope.EvidenceIDs == nil {
		p.Scope.EvidenceIDs = []string{}
	}
	if allowControl && (!p.Scope.IncludeCode || !p.Scope.IncludeEvents) {
		return p, fmt.Errorf("control requires explicit sharing of sealed code and managed Agent live output")
	}
	if p.Scope.SnapshotID == "" && len(p.Scope.EntryIDs) > 0 {
		return p, fmt.Errorf("entryIds require snapshotId")
	}
	p.LiveBinding, err = app.prepareNativeLive(ctx, threadID, p.Scope.NativeLive)
	if err != nil {
		return p, err
	}
	p.Detail.SessionSnapshots = []domain.SessionSnapshot{}
	// Read-only native following does not expand a previously granted disclosure.
	p.Detail.SessionFollows = nil
	p.Detail.NativeLive = nil
	if p.Scope.SnapshotID != "" {
		snapshot, err := app.store.GetSessionSnapshot(ctx, threadID, p.Scope.SnapshotID)
		if err != nil {
			return p, err
		}
		entries := []domain.SessionEntry{}
		selected := map[string]bool{}
		for _, id := range p.Scope.EntryIDs {
			if selected[id] {
				return p, fmt.Errorf("duplicate snapshot entry ID")
			}
			selected[id] = true
			found := false
			for _, entry := range snapshot.Entries {
				if entry.ID == id {
					entries = append(entries, entry)
					found = true
					break
				}
			}
			if !found {
				return p, fmt.Errorf("entry is outside the selected snapshot")
			}
		}
		p.SnapshotFullyShared = len(entries) == len(snapshot.Entries)
		p.SnapshotEntryCount = len(snapshot.Entries)
		snapshot.Entries = entries
		snapshot.Source.Cwd = ""
		snapshot.Source.NativeIDs = nil
		snapshot.Source.Title = ""
		snapshot.Warnings = []string{}
		if snapshot.Truncated {
			snapshot.Warnings = append(snapshot.Warnings, "来源历史不完整；此视图仅包含获准分享的记录。")
		}
		snapshot.Capabilities = domain.SessionCapabilities{Read: true, Reason: "共享的只读快照不授予原生 Session 控制权。"}
		p.Detail.SessionSnapshots = append(p.Detail.SessionSnapshots, snapshot)
	}
	p.Detail.Evidence = []evidenceView{}
	for _, id := range p.Scope.EvidenceIDs {
		item, err := app.store.GetEvidence(ctx, threadID, id)
		if err != nil {
			return p, err
		}
		if item.Kind == "agent_transcript" {
			return p, fmt.Errorf("raw Agent transcripts cannot be shared; import a structured Session snapshot")
		}
		p.EvidenceHashes[id] = item.ObjectHash
		for _, view := range detail.Evidence {
			if view.ID == id {
				view.Source = ""
				p.Detail.Evidence = append(p.Detail.Evidence, view)
			}
		}
	}
	p.Detail.Repo = ""
	p.Detail.Branch = ""
	p.Detail.Provider = ""
	p.Detail.Status = "shared"
	p.Detail.Worktree = ""
	p.Detail.Goal = ""
	p.Detail.Progress = ""
	p.Detail.Blocker = ""
	p.Detail.Tried = ""
	p.Detail.Questions = ""
	p.Detail.Share = nil
	p.Detail.Participants = []participantView{}
	p.Detail.ControlLease = nil
	p.Detail.AgentRun = nil
	if !p.Scope.IncludeCode {
		p.Detail.Git = gitView{Untracked: []gitFile{}, Unborn: true}
		p.Detail.Rounds = []roundView{}
	} else {
		rounds, err := app.store.ListRounds(ctx, threadID)
		if err != nil {
			return p, err
		}
		if len(rounds) == 0 {
			return p, fmt.Errorf("no sealed code checkpoint")
		}
		round := rounds[len(rounds)-1]
		bundle, err := app.buildOfflineBundle(ctx, threadID, round.ID, nil, nil)
		if err != nil {
			return p, err
		}
		restored, err := transfer.Materialize(ctx, bundle, app.paths.Worktrees)
		if err != nil {
			return p, err
		}
		defer restored.Cleanup()
		patch, err := gitstate.ExportSnapshotPatch(ctx, restored.Worktree, bundle.Snapshot)
		if err != nil {
			return p, err
		}
		p.Detail.Git = gitView{Head: bundle.Baseline, FinalPatch: string(patch), StagedPatch: string(bundle.Snapshot.StagedPatch), UnstagedPatch: string(bundle.Snapshot.UnstagedPatch), Untracked: []gitFile{}}
		for _, file := range bundle.Snapshot.Untracked {
			if file.Included {
				p.Detail.Git.Untracked = append(p.Detail.Git.Untracked, gitFile{Path: file.Path, Size: file.Size, Captured: true})
			}
		}
		p.Detail.Rounds = []roundView{{ID: round.ID, Sequence: round.Number, CreatedAt: round.CreatedAt}}
	}
	p.Detail.Annotations = []annotationView{}
	for _, annotation := range detail.Annotations {
		// Old unanchored comments may discuss private context. Never disclose them implicitly.
		if annotation.Target != nil && p.allowsAnnotation(annotation) {
			p.Detail.Annotations = append(p.Detail.Annotations, annotation)
		}
	}
	p.Detail.Events = []eventView{}
	for _, event := range detail.Events {
		p.Detail.Events = append(p.Detail.Events, p.projectEvent(event))
	}
	return p, nil
}

func (p shareProjection) allowsAnnotation(a annotationView) bool {
	if p.Legacy {
		return true
	}
	if a.File != "" && !p.Scope.IncludeCode {
		return false
	}
	target := a.Target
	if target == nil {
		return a.File == "" || p.Scope.IncludeCode
	}
	if target.SnapshotID != "" {
		return target.SnapshotID == p.Scope.SnapshotID && ((target.EntryID == "" && p.SnapshotFullyShared) || (target.EntryID != "" && contains(p.Scope.EntryIDs, target.EntryID)))
	}
	if target.EvidenceID != "" {
		return contains(p.Scope.EvidenceIDs, target.EvidenceID)
	}
	if target.RoundID != "" {
		if !p.Scope.IncludeCode {
			return false
		}
		for _, round := range p.Detail.Rounds {
			if round.ID == target.RoundID {
				return true
			}
		}
	}
	return false
}

func (p shareProjection) projectEvent(event eventView) eventView {
	if p.Legacy {
		return event
	}
	if p.Scope.IncludeEvents {
		for _, prefix := range []string{"message.", "tool.", "turn.", "file.", "input.", "run.", "command."} {
			if strings.HasPrefix(event.Type, prefix) {
				return event
			}
		}
	}
	// Keep the cursor and revision, not private IDs, error details, comments or transcripts.
	return eventView{Seq: event.Seq, Type: "thread.updated", CreatedAt: event.CreatedAt, Payload: map[string]any{"revision": event.Payload["revision"]}}
}

func (app *App) projectThreadDetail(ctx context.Context, detail threadDetail, identity access) (threadDetail, error) {
	p, err := app.loadShareProjection(ctx, identity.ShareID)
	if err != nil {
		return detail, err
	}
	if p.Legacy {
		return detail, nil
	}
	if err = app.requireCurrentShare(ctx, identity.ShareID, detail.ID); err != nil {
		return threadDetail{}, err
	}
	out := p.Detail
	out.SessionSnapshots = append([]domain.SessionSnapshot{}, p.Detail.SessionSnapshots...)
	if err = app.attachNativeLiveDetail(ctx, identity.ShareID, detail.ID, p, &out); err != nil {
		return threadDetail{}, err
	}
	out.Revision = detail.Revision
	out.UpdatedAt = detail.UpdatedAt
	out.Share = detail.Share
	out.Participants = detail.Participants
	if p.AllowControl {
		out.ControlLease = detail.ControlLease
	}
	if p.Scope.IncludeEvents {
		out.AgentRun = detail.AgentRun
	}
	out.Annotations = append([]annotationView{}, p.Detail.Annotations...)
	existing := map[string]bool{}
	for _, a := range out.Annotations {
		existing[a.ID] = true
	}
	allowsAnnotation := app.sharedAnnotationChecker(ctx, identity.ShareID, detail.ID, p)
	for _, a := range detail.Annotations {
		if a.Target == nil && a.SourceShareID != identity.ShareID {
			continue
		}
		allowed, err := allowsAnnotation(a)
		if err != nil {
			return threadDetail{}, err
		}
		// An exact immutable anchor can predate the Share. Its authorized text
		// cannot expand when later captures arrive. Unanchored comments retain
		// the existing same-Share and creation-time boundary.
		if !existing[a.ID] && (a.Target != nil || !a.CreatedAt.Before(p.CreatedAt)) && allowed {
			out.Annotations = append(out.Annotations, a)
		}
	}
	out.Events = []eventView{}
	projectEvent, err := app.sharedEventProjector(ctx, identity.ShareID, detail.ID, p)
	if err != nil {
		return threadDetail{}, err
	}
	for _, event := range detail.Events {
		view, err := projectEvent(event)
		if err != nil {
			return threadDetail{}, err
		}
		out.Events = append(out.Events, view)
	}
	return out, nil
}

func (app *App) authorizeShareCapability(w http.ResponseWriter, r *http.Request, shareID string) bool {
	share, err := app.store.GetShare(r.Context(), shareID)
	if err != nil {
		writeDomainError(w, err)
		return false
	}
	if !share.RevokedAt.IsZero() || !share.ExpiresAt.After(time.Now()) {
		writeError(w, 403, "share_expired", "Share expired or revoked")
		return false
	}
	var caps []string
	if err = json.Unmarshal(share.Capabilities, &caps); err != nil {
		writeError(w, 403, "capability", "Share has no granted capability")
		return false
	}
	capability := "view"
	switch {
	case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/annotations"):
		capability = "annotate"
	case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/control"):
		capability = "send"
	case r.Method == "POST" && strings.Contains(r.URL.Path, "/agent/"):
		capability = r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		if capability == "input" {
			capability = "send"
		}
	}
	if !contains(caps, capability) {
		writeError(w, 403, "capability", "This Share does not grant "+capability)
		return false
	}
	return true
}
