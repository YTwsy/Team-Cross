package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/domain"
)

const followPollInterval = 3 * time.Second

type startFollowRequest struct {
	SnapshotID      string `json:"snapshotId"`
	ConfirmReadOnly bool   `json:"confirmReadOnly"`
}

func (app *App) handleStartFollow(w http.ResponseWriter, r *http.Request) {
	threadID, ok := app.authorizeThread(w, r)
	if !ok {
		return
	}
	var input startFollowRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.SnapshotID == "" || !input.ConfirmReadOnly {
		writeError(w, 422, "follow_confirmation", "请选择快照，并确认仅只读跟随：不启动 Agent，也不扩大现有 Share。")
		return
	}
	snapshot, err := app.store.GetSessionSnapshot(r.Context(), threadID, input.SnapshotID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if !snapshot.Capabilities.Read || !snapshot.Capabilities.Follow {
		writeError(w, 422, "follow_unsupported", domain.ErrFollowUnsupported.Error())
		return
	}
	// A capability in an old or imported snapshot is evidence, not local authority.
	// Recheck this exact native source without subscribing, resuming, or executing.
	if err = app.verifyFollowSource(r.Context(), snapshot.Source); err != nil {
		writeError(w, 422, "follow_unsupported", err.Error())
		return
	}
	follow, created, err := app.store.CreateSessionFollow(r.Context(), threadID, input.SnapshotID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	app.startFollowWorker(follow, true)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, follow)
}

func (app *App) handleStopFollow(w http.ResponseWriter, r *http.Request) {
	threadID, ok := app.authorizeThread(w, r)
	if !ok {
		return
	}
	// Persist the fence BEFORE cancelling I/O. An uncooperative delayed reader is
	// still prevented from writing after this request returns successfully.
	follow, err := app.store.StopSessionFollow(r.Context(), threadID, r.PathValue("followID"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	app.followMu.Lock()
	if cancel := app.followWorkers[follow.ID]; cancel != nil {
		cancel()
	}
	app.followMu.Unlock()
	writeJSON(w, http.StatusOK, follow)
}

func sameFollowSource(a, b domain.SessionRef) bool {
	return a.Provider == b.Provider && a.SessionID == b.SessionID && a.IdentityKind == b.IdentityKind && a.Surface == b.Surface && a.ProviderVersion == b.ProviderVersion
}

func (app *App) verifyFollowSource(ctx context.Context, source domain.SessionRef) error {
	snapshot, err := app.readReviewSnapshot(ctx, sessionImportRequest{Provider: source.Provider, SessionID: source.SessionID})
	if err != nil {
		return fmt.Errorf("无法验证该原生 Session 的当前只读能力: %w", err)
	}
	if !sameFollowSource(source, snapshot.Source) || !snapshot.Capabilities.Read || !snapshot.Capabilities.Follow {
		return domain.ErrFollowUnsupported
	}
	return nil
}

// Restore only readers explicitly authorized by Owner. Every new process checks
// the currently installed adapter/source capability again before polling.
func (app *App) startSessionFollows(parent context.Context) {
	app.followMu.Lock()
	if app.followCtx == nil && !app.followClosed {
		app.followCtx, app.followCancel = context.WithCancel(parent)
		app.followWorkers = map[string]context.CancelFunc{}
	}
	app.followMu.Unlock()
	follows, err := app.store.ListSessionFollows(parent, "")
	if err != nil {
		app.logger.Warn("Unable to restore read-only Follow readers", "error", err)
		return
	}
	for _, follow := range follows {
		if follow.State != "stopped" {
			app.startFollowWorker(follow, false)
		}
	}
}

func (app *App) startFollowWorker(follow domain.SessionFollow, verified bool) {
	app.followMu.Lock()
	defer app.followMu.Unlock()
	if app.followClosed || app.followWorkers[follow.ID] != nil {
		return
	}
	if app.followCtx == nil {
		app.followCtx, app.followCancel = context.WithCancel(context.Background())
		app.followWorkers = map[string]context.CancelFunc{}
	}
	ctx, cancel := context.WithCancel(app.followCtx)
	app.followWorkers[follow.ID] = cancel
	app.followWG.Add(1)
	go func() {
		defer func() {
			cancel()
			app.followMu.Lock()
			delete(app.followWorkers, follow.ID)
			app.followMu.Unlock()
			app.followWG.Done()
		}()
		failures := 0
		for ctx.Err() == nil {
			current, err := app.store.GetSessionFollow(ctx, follow.ThreadID, follow.ID)
			if err != nil || current.State == "stopped" {
				return
			}
			if !verified {
				err = app.verifyFollowSource(ctx, current.Source)
				verified = err == nil
			}
			if err == nil {
				err = app.pollSessionFollow(ctx, current)
			}
			if ctx.Err() != nil || errors.Is(err, domain.ErrFollowFence) {
				return
			}
			delay := followPollInterval
			if err != nil {
				// The reader may have restarted/upgraded while disconnected. Stored
				// evidence of capability is not authority for a replacement reader.
				verified = false
				failures++
				// Keep diagnostics local; do not place raw Provider output in SSE.
				reason := "只读来源暂时不可用；保留游标并自动重试，不会执行 Agent。"
				if errors.Is(err, domain.ErrFollowUnsupported) {
					reason = "当前来源或版本尚未验证 Follow；未读取增量，也未启动 Agent。"
				}
				if recordErr := app.store.RecordFollowRetry(ctx, current, reason); recordErr != nil && !errors.Is(recordErr, domain.ErrFollowFence) {
					app.logger.Warn("Unable to persist Follow retry", "followId", follow.ID, "error", recordErr)
				}
				delay = time.Duration(min(failures+1, 10)) * followPollInterval
			} else {
				failures = 0
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}

func (app *App) stopFollowWorkers() {
	app.followMu.Lock()
	app.followClosed = true
	if app.followCancel != nil {
		app.followCancel()
	}
	app.followMu.Unlock()
	app.followWG.Wait()
}

func (app *App) readFollowPoll(ctx context.Context, follow domain.SessionFollow) (domain.SessionPoll, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if app.followReader != nil {
		return app.followReader(ctx, follow)
	}
	var result domain.SessionPoll
	if app.bridge == nil {
		return result, fmt.Errorf("Agent Bridge is unavailable")
	}
	err := app.bridge.Call(ctx, "sessions.poll", followPollParams(follow), &result)
	return result, err
}

func followPollParams(follow domain.SessionFollow) map[string]any {
	params := map[string]any{"provider": follow.Source.Provider, "sessionId": follow.Source.SessionID, "limit": 500}
	// An absent cursor means initial read; an explicitly empty opaque cursor is
	// malformed according to the Bridge protocol and must not be sent.
	if follow.Cursor != "" {
		params["cursor"] = follow.Cursor
	}
	return params
}

func (app *App) pollSessionFollow(ctx context.Context, follow domain.SessionFollow) error {
	poll, err := app.readFollowPoll(ctx, follow)
	if err != nil {
		return err
	}
	if !sameFollowSource(follow.Source, poll.Source) || poll.Cursor == "" || len(poll.Cursor) > 128<<10 || len(jsonBytes(poll)) > 20<<20 || (follow.Cursor == "" && !poll.Reset) {
		return fmt.Errorf("invalid Follow identity, cursor, or response size")
	}
	if poll.Reset && follow.Cursor != "" {
		if err := app.verifyFollowSource(ctx, follow.Source); err != nil {
			return err
		}
	}
	app.roundMu.Lock()
	defer app.roundMu.Unlock()
	previous, err := app.store.GetSessionSnapshot(ctx, follow.ThreadID, follow.CurrentSnapshotID)
	if err != nil {
		return err
	}
	snapshot, gaps, changed, err := mergeFollowPoll(previous, follow, poll)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	next := follow
	next.Cursor, next.State, next.Reason, next.UpdatedAt, next.LastPolledAt, next.Gaps = poll.Cursor, "active", "", now, &now, gaps
	if !changed {
		return app.store.CommitFollowPoll(ctx, follow, next, nil, nil)
	}
	snapshot.ID, snapshot.ThreadID = uuid.NewString(), follow.ThreadID
	next.CurrentSnapshotID = snapshot.ID
	round, err := app.prepareFollowRound(ctx, follow, snapshot.ID)
	if err != nil {
		return err
	}
	return app.store.CommitFollowPoll(ctx, follow, next, &snapshot, &round)
}

func appendFollowNotes(notes []string, more ...string) []string {
	for _, value := range more {
		if value != "" && !contains(notes, value) {
			notes = append(notes, value)
		}
	}
	if len(notes) > 64 {
		notes = append([]string{"较早的数据缺口标记见历史快照。"}, notes[len(notes)-63:]...)
	}
	return notes
}

func mergeFollowPoll(previous domain.SessionSnapshot, follow domain.SessionFollow, poll domain.SessionPoll) (domain.SessionSnapshot, []string, bool, error) {
	next := previous
	next.Entries = append([]domain.SessionEntry{}, previous.Entries...)
	next.Warnings = append([]string{}, previous.Warnings...)
	gaps := appendFollowNotes(append([]string{}, follow.Gaps...), poll.Gaps...)
	if poll.Reset {
		next.Entries = []domain.SessionEntry{}
		next.Truncated = poll.Truncated
		next.Warnings = []string{}
		if follow.Cursor != "" {
			gaps = appendFollowNotes(gaps, "来源游标失效或历史改写；当前视图已重新定位，旧快照保留。")
		}
	}
	index := map[string]int{}
	for i, entry := range next.Entries {
		index[entry.ID] = i
	}
	seen := map[string]bool{}
	for _, entry := range poll.Entries {
		if entry.ID == "" || seen[entry.ID] {
			return next, gaps, false, fmt.Errorf("invalid or duplicate Follow entry ID")
		}
		seen[entry.ID] = true
		if i, ok := index[entry.ID]; ok {
			next.Entries[i] = entry
		} else {
			index[entry.ID] = len(next.Entries)
			next.Entries = append(next.Entries, entry)
		}
	}
	next.Truncated = next.Truncated || poll.Truncated
	if len(next.Entries) > 5000 {
		next.Entries = append([]domain.SessionEntry{}, next.Entries[len(next.Entries)-5000:]...)
		next.Truncated = true
		gaps = appendFollowNotes(gaps, "当前 Follow 视图仅保留最近 5000 条记录；更早内容见历史快照。")
	}
	// Bound bytes as well as entry count. A few large tool results must not
	// permanently pin the cursor at the 20 MiB capture limit. Older immutable
	// captures still hold the evicted entries and their annotation anchors.
	entryBytes := make([]int, len(next.Entries))
	var total int
	for i, entry := range next.Entries {
		entryBytes[i] = len(jsonBytes(entry)) + 1
		total += entryBytes[i]
	}
	removed := 0
	for total > 16<<20 && removed < len(next.Entries)-1 {
		total -= entryBytes[removed]
		removed++
	}
	if removed > 0 {
		next.Entries = append([]domain.SessionEntry{}, next.Entries[removed:]...)
		next.Truncated = true
		gaps = appendFollowNotes(gaps, "当前 Follow 视图已按内容大小保留最近记录；更早内容见历史快照。")
	}
	next.Warnings = appendFollowNotes(next.Warnings, poll.Warnings...)
	for _, gap := range gaps {
		next.Warnings = appendFollowNotes(next.Warnings, "数据缺口: "+gap)
	}
	next.CapturedAt = poll.CapturedAt
	if next.CapturedAt.IsZero() {
		next.CapturedAt = time.Now().UTC()
	}
	if len(jsonBytes(next)) > 20<<20 {
		return next, gaps, false, fmt.Errorf("Follow checkpoint exceeds 20 MiB")
	}
	changed := !reflect.DeepEqual(next.Entries, previous.Entries) || !reflect.DeepEqual(next.Warnings, previous.Warnings) || next.Truncated != previous.Truncated
	return next, gaps, changed, nil
}

func (app *App) prepareFollowRound(ctx context.Context, follow domain.SessionFollow, snapshotID string) (domain.Round, error) {
	rounds, err := app.store.ListRounds(ctx, follow.ThreadID)
	if err != nil {
		return domain.Round{}, err
	}
	round := domain.Round{ID: uuid.NewString(), ThreadID: follow.ThreadID, Number: int64(len(rounds)), Kind: "session_follow", Summary: "只读 Follow 检查点（Git 基线独立捕获）", CreatedAt: time.Now().UTC()}
	manifest := map[string]any{}
	if len(rounds) > 0 {
		previous := rounds[len(rounds)-1]
		round.ParentRoundID, round.SnapshotID = previous.ID, previous.SnapshotID
		if previous.ManifestObject != "" {
			data, err := app.store.GetObject(ctx, previous.ManifestObject)
			if err != nil {
				return round, err
			}
			if err := json.Unmarshal(data, &manifest); err != nil {
				return round, err
			}
		}
	}
	var inherited []string
	if value, err := json.Marshal(manifest["sessionSnapshotIds"]); err == nil {
		_ = json.Unmarshal(value, &inherited)
	}
	ids := []string{}
	for _, id := range inherited {
		// Keep the original import, but not every intermediate Follow window in
		// the newest context. Their earlier immutable Rounds retain all anchors.
		if id == follow.CurrentSnapshotID && id != follow.SourceSnapshotID {
			continue
		}
		ids = append(ids, id)
	}
	manifest["sessionSnapshotIds"] = append(ids, snapshotID)
	manifest["contextOrigin"] = "Read-only native Session evidence; Git state is a separately captured checkpoint, not the native Session's historical code state."
	object, err := app.store.PutObject(ctx, jsonBytes(manifest), "application/vnd.teamcross.handoff+json")
	if err != nil {
		return round, err
	}
	round.ManifestObject = object.Hash
	return round, nil
}
