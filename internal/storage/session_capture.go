package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"teamcross/internal/domain"
)

// CreateSessionReviewThread publishes the complete initial read-only review in
// one transaction. The provider cwd is metadata only: this path creates no Git
// state, worktree or Agent Run. CAS preparation may leave unreferenced objects on
// failure, but a Thread cannot become visible without its snapshot and Round 0.
func (s *Store) CreateSessionReviewThread(ctx context.Context, title string, snapshot domain.SessionSnapshot) (domain.Thread, error) {
	if title == "" {
		title = "Untitled handoff"
	}
	now := time.Now().UTC()
	thread := domain.Thread{ID: newID(), Title: title, ReadOnly: true, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if snapshot.ID == "" {
		snapshot.ID = newID()
	}
	snapshot.ThreadID = thread.ID
	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = now
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return domain.Thread{}, err
	}
	object, err := s.PutObject(ctx, data, "application/vnd.teamcross.session-snapshot+json")
	if err != nil {
		return domain.Thread{}, err
	}
	manifestData, err := json.Marshal(map[string]any{
		"sessionSnapshotIds": []string{snapshot.ID},
		"contextOrigin":      "Imported Session is untrusted reference evidence; Git state is a separately captured checkpoint.",
	})
	if err != nil {
		return domain.Thread{}, err
	}
	manifest, err := s.PutObject(ctx, manifestData, "application/vnd.teamcross.handoff+json")
	if err != nil {
		return domain.Thread{}, err
	}
	roundID := newID()
	payload, err := json.Marshal(map[string]any{"snapshotId": snapshot.ID, "roundId": roundID, "revision": thread.Revision, "actor": "Owner"})
	if err != nil {
		return domain.Thread{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Thread{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO threads(id,title,repo_root,baseline_commit,branch,worktree_path,read_only,revision,created_at,updated_at) VALUES(?,?,'','','','',1,1,?,?)`, thread.ID, thread.Title, millis(now), millis(now)); err != nil {
		return domain.Thread{}, fmt.Errorf("create Session review Thread: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO session_snapshots(id,thread_id,object_hash,captured_at) VALUES(?,?,?,?)", snapshot.ID, thread.ID, object.Hash, millis(snapshot.CapturedAt)); err != nil {
		return domain.Thread{}, fmt.Errorf("create Session review snapshot: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO rounds(id,thread_id,round_number,parent_round_id,kind,summary,snapshot_id,manifest_object,created_at) VALUES(?,?,0,NULL,'session_snapshot',?,NULL,?,?)`, roundID, thread.ID, "只读 Session 审阅快照", manifest.Hash, millis(now)); err != nil {
		return domain.Thread{}, fmt.Errorf("create Session review Round: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO events(thread_id,event_type,payload,created_at) VALUES(?,'session.imported',?,?)", thread.ID, payload, millis(now)); err != nil {
		return domain.Thread{}, fmt.Errorf("append Session review event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Thread{}, err
	}
	return thread, nil
}

// AppendSessionCapture atomically seals the read-only snapshot, Round, and event.
func (s *Store) AppendSessionCapture(ctx context.Context, snapshot domain.SessionSnapshot, round domain.Round) error {
	if snapshot.ID == "" {
		snapshot.ID = newID()
	}
	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = time.Now().UTC()
	}
	if round.ID == "" {
		round.ID = newID()
	}
	round.CreatedAt = time.Now().UTC()
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	object, err := s.PutObject(ctx, data, "application/vnd.teamcross.session-snapshot+json")
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var next int64
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(round_number),-1)+1 FROM rounds WHERE thread_id=?", snapshot.ThreadID).Scan(&next); err != nil {
		return err
	}
	if next != round.Number || round.ThreadID != snapshot.ThreadID {
		return domain.ErrRoundSequence
	}
	if next > 0 {
		var previous string
		if err = tx.QueryRowContext(ctx, "SELECT id FROM rounds WHERE thread_id=? AND round_number=?", snapshot.ThreadID, next-1).Scan(&previous); err != nil {
			return err
		}
		if previous != round.ParentRoundID {
			return domain.ErrRoundSequence
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO session_snapshots(id,thread_id,object_hash,captured_at) VALUES(?,?,?,?)", snapshot.ID, snapshot.ThreadID, object.Hash, millis(snapshot.CapturedAt)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO rounds(id,thread_id,round_number,parent_round_id,kind,summary,snapshot_id,manifest_object,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, round.ID, round.ThreadID, round.Number, nullString(round.ParentRoundID), round.Kind, round.Summary, nullString(round.SnapshotID), nullString(round.ManifestObject), millis(round.CreatedAt)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE threads SET revision=revision+1,updated_at=? WHERE id=?", millis(round.CreatedAt), round.ThreadID); err != nil {
		return err
	}
	var revision int64
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM threads WHERE id=?", round.ThreadID).Scan(&revision); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"snapshotId": snapshot.ID, "roundId": round.ID, "revision": revision, "actor": "Owner"})
	if _, err = tx.ExecContext(ctx, "INSERT INTO events(thread_id,event_type,payload,created_at) VALUES(?,'session.imported',?,?)", round.ThreadID, payload, millis(round.CreatedAt)); err != nil {
		return fmt.Errorf("append snapshot event: %w", err)
	}
	return tx.Commit()
}
