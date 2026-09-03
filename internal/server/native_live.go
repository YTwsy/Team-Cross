package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"teamcross/internal/domain"
)

// This is a disclosure status, not a native reader or a Writer capability.
type nativeLiveView struct {
	FollowID         string   `json:"followId"`
	State            string   `json:"state"`
	LatestSnapshotID string   `json:"latestSnapshotId"`
	EntryKinds       []string `json:"entryKinds"`
	Reason           string   `json:"reason,omitempty"`
}

func (app *App) prepareNativeLive(ctx context.Context, threadID string, scope *domain.NativeLiveScope) (*domain.NativeLiveBinding, error) {
	if scope == nil {
		return nil, nil
	}
	if !scope.ConfirmCurrentAndFuture || scope.FollowID == "" || scope.ExpectedSnapshotID == "" {
		return nil, fmt.Errorf("原生实时分享需要确认当前预览窗口及后续窗口的内容类别")
	}
	follow, err := app.store.GetSessionFollow(ctx, threadID, scope.FollowID)
	if err != nil {
		return nil, fmt.Errorf("选择的只读 Follow 不属于此 Thread")
	}
	if follow.State != "active" || follow.LastPolledAt == nil {
		return nil, fmt.Errorf("仅已成功读取且仍在运行的 Follow 可以启用原生实时分享")
	}
	if follow.CurrentSnapshotID != scope.ExpectedSnapshotID {
		return nil, domain.ErrLiveShareStale
	}
	snapshot, err := app.store.GetSessionSnapshot(ctx, threadID, scope.ExpectedSnapshotID)
	if err != nil {
		return nil, err
	}
	if !sameFollowSource(snapshot.Source, follow.Source) {
		return nil, domain.ErrLiveShareFence
	}
	if _, _, err = domain.ProjectNativeSnapshot(snapshot, scope.EntryKinds); err != nil {
		return nil, err
	}
	return &domain.NativeLiveBinding{FollowID: follow.ID, FollowEpoch: follow.Epoch, Source: follow.Source, EntryKinds: append([]string(nil), scope.EntryKinds...), ExpectedSnapshotID: scope.ExpectedSnapshotID}, nil
}

func (app *App) requireCurrentShare(ctx context.Context, shareID, threadID string) error {
	if !app.isPublishedShare(threadID, shareID) {
		return domain.ErrNotFound
	}
	share, err := app.store.GetShare(ctx, shareID)
	if err != nil {
		return err
	}
	if share.ThreadID != threadID || !share.RevokedAt.IsZero() || !share.ExpiresAt.After(time.Now()) {
		return domain.ErrNotFound
	}
	return nil
}

// All exact-snapshot reads and annotation targets resolve through one boundary.
// Stable entry IDs alone never grant access to another immutable window.
func (app *App) resolveSharedSnapshot(ctx context.Context, shareID, threadID, snapshotID string, p shareProjection) (domain.SessionSnapshot, bool, error) {
	if err := app.requireCurrentShare(ctx, shareID, threadID); err != nil {
		return domain.SessionSnapshot{}, false, err
	}
	var out domain.SessionSnapshot
	fully, found := false, false
	for _, snapshot := range p.Detail.SessionSnapshots {
		if snapshot.ID == snapshotID && snapshot.ThreadID == threadID {
			out, fully, found = snapshot, p.SnapshotFullyShared, true
			out.Entries = append([]domain.SessionEntry{}, snapshot.Entries...)
			break
		}
	}
	if p.Scope.NativeLive != nil {
		live, all, err := app.store.GetLiveShareSnapshot(ctx, shareID, snapshotID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return domain.SessionSnapshot{}, false, err
		}
		if err == nil {
			if live.ThreadID != threadID || live.ID != snapshotID {
				return domain.SessionSnapshot{}, false, domain.ErrNotFound
			}
			if !found {
				out, fully, found = live, all, true
			} else {
				// Static entry selection and native categories can independently
				// authorize different records in the same immutable snapshot.
				ids := map[string]bool{}
				for _, entry := range out.Entries {
					ids[entry.ID] = true
				}
				for _, entry := range live.Entries {
					if !ids[entry.ID] {
						out.Entries = append(out.Entries, entry)
					}
				}
				fully = fully || all
				if p.Scope.SnapshotID == snapshotID && p.SnapshotEntryCount > 0 && len(out.Entries) == p.SnapshotEntryCount {
					fully = true
				}
			}
		}
	}
	if !found {
		return out, false, domain.ErrNotFound
	}
	return out, fully, nil
}

func (app *App) handleGetSessionSnapshot(w http.ResponseWriter, r *http.Request) {
	threadID, ok := app.authorizeThread(w, r)
	if !ok {
		return
	}
	var snapshot domain.SessionSnapshot
	var err error
	identity := accessFrom(r)
	if identity.Mode == "share" {
		var p shareProjection
		p, err = app.loadShareProjection(r.Context(), identity.ShareID)
		if err == nil {
			snapshot, _, err = app.resolveSharedSnapshot(r.Context(), identity.ShareID, threadID, r.PathValue("snapshotID"), p)
		}
	} else {
		snapshot, err = app.store.GetSessionSnapshot(r.Context(), threadID, r.PathValue("snapshotID"))
	}
	if err != nil {
		// Do not disclose whether a private snapshot exists or why it is absent.
		writeError(w, http.StatusNotFound, "snapshot_unavailable", "此快照不存在，或不在当前 Share 的授权范围内。")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, snapshot)
}

func (app *App) sharedAnnotationAllowed(ctx context.Context, shareID, threadID string, p shareProjection, a annotationView) (bool, error) {
	return app.sharedAnnotationChecker(ctx, shareID, threadID, p)(a)
}

// Memoize only IDs for one response, never authorization across requests. Many
// comments on a large window must not repeatedly hash/decode the same CAS body.
func (app *App) sharedAnnotationChecker(ctx context.Context, shareID, threadID string, p shareProjection) func(annotationView) (bool, error) {
	type targets struct {
		entries map[string]bool
		fully   bool
	}
	cache := map[string]targets{}
	return func(a annotationView) (bool, error) {
		if a.Target == nil || a.Target.SnapshotID == "" {
			return p.allowsAnnotation(a), nil
		}
		if a.File != "" {
			return false, nil
		}
		allowed, ok := cache[a.Target.SnapshotID]
		if !ok {
			snapshot, fully, err := app.resolveSharedSnapshot(ctx, shareID, threadID, a.Target.SnapshotID, p)
			if err != nil && !errors.Is(err, domain.ErrNotFound) {
				return false, err
			}
			allowed = targets{entries: map[string]bool{}, fully: err == nil && fully}
			for _, entry := range snapshot.Entries {
				allowed.entries[entry.ID] = true
			}
			cache[a.Target.SnapshotID] = allowed
		}
		if a.Target.EntryID == "" {
			return allowed.fully, nil
		}
		return allowed.entries[a.Target.EntryID], nil
	}
}

func (app *App) nativeLiveStatus(ctx context.Context, shareID string) (*nativeLiveView, error) {
	state, err := app.store.GetLiveShareState(ctx, shareID)
	if err != nil {
		return nil, err
	}
	view := &nativeLiveView{FollowID: state.Binding.FollowID, State: state.State, LatestSnapshotID: state.LatestSnapshotID, EntryKinds: state.Binding.EntryKinds}
	// No raw Provider errors, paths, cursor or hidden snapshot IDs in the view.
	switch state.State {
	case "active":
	case "retrying":
		view.Reason = "只读来源暂时不可用；保留最后已公开窗口。"
	case "stopped":
		view.Reason = "此 Follow 已停止；新 Follow 不会继承本 Share 的实时授权。"
	case "limited":
		view.Reason = "实时分享达到 128 个窗口或 64 MiB 上限；保留已公开窗口，后续内容未分享。请撤销并重新预览分享。"
	default:
		return nil, domain.ErrNotFound
	}
	return view, nil
}

func (app *App) attachNativeLiveDetail(ctx context.Context, shareID, threadID string, p shareProjection, out *threadDetail) error {
	if p.Scope.NativeLive == nil {
		return nil
	}
	view, err := app.nativeLiveStatus(ctx, shareID)
	if err != nil {
		return err
	}
	out.NativeLive = view
	latestIncluded := false
	for i, item := range out.SessionSnapshots {
		// A selected static window can also have persistent live membership.
		// Resolve that same union on every detail read, even after it is no
		// longer latest; its exact anchor must not shrink to the static subset.
		snapshot, _, err := app.resolveSharedSnapshot(ctx, shareID, threadID, item.ID, p)
		if err != nil {
			return err
		}
		out.SessionSnapshots[i] = snapshot
		latestIncluded = latestIncluded || item.ID == view.LatestSnapshotID
	}
	if latestIncluded || view.LatestSnapshotID == "" {
		return nil
	}
	snapshot, _, err := app.resolveSharedSnapshot(ctx, shareID, threadID, view.LatestSnapshotID, p)
	if err != nil {
		return err
	}
	out.SessionSnapshots = append(out.SessionSnapshots, snapshot)
	return nil
}

// Membership is checked on every notification, including on an existing SSE
// connection. Only fixed reference fields are emitted; text uses the read API.
func (app *App) projectSharedEvent(ctx context.Context, shareID, threadID string, p shareProjection, event eventView) (eventView, error) {
	project, err := app.sharedEventProjector(ctx, shareID, threadID, p)
	if err != nil {
		return eventView{}, err
	}
	return project(event)
}

// A projector lives only for one response or one fetched SSE batch. It never
// reads snapshot bodies and never caches capability across streaming batches.
func (app *App) sharedEventProjector(ctx context.Context, shareID, threadID string, p shareProjection) (func(eventView) (eventView, error), error) {
	if p.Scope.NativeLive == nil {
		return func(event eventView) (eventView, error) { return p.projectEvent(event), nil }, nil
	}
	if err := app.requireCurrentShare(ctx, shareID, threadID); err != nil {
		return nil, err
	}
	state, err := app.store.GetLiveShareState(ctx, shareID)
	if err != nil {
		return nil, err
	}
	cache := map[string]bool{}
	return func(event eventView) (eventView, error) {
		base := p.projectEvent(event)
		if event.Seq <= state.StartSeq || !strings.HasPrefix(event.Type, "session.follow.") || event.Payload["followId"] != p.Scope.NativeLive.FollowID {
			return base, nil
		}
		snapshotID, _ := event.Payload["snapshotId"].(string)
		if snapshotID == "" {
			return base, nil
		}
		member, seen := cache[snapshotID]
		if !seen {
			var err error
			member, err = app.store.HasLiveShareSnapshot(ctx, shareID, snapshotID)
			if err != nil {
				return eventView{}, err
			}
			cache[snapshotID] = member
		}
		if !member {
			if state.State != "limited" {
				return base, nil
			}
			snapshotID = state.LatestSnapshotID
		}
		return eventView{Seq: event.Seq, Type: "shared.session.updated", CreatedAt: event.CreatedAt, Payload: map[string]any{"followId": state.Binding.FollowID, "snapshotId": snapshotID, "state": state.State, "revision": event.Payload["revision"]}}, nil
	}, nil
}
