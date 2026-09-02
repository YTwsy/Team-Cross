package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"teamcross/internal/domain"
)

func (s *Store) CreateThread(ctx context.Context, thread domain.Thread) (domain.Thread, error) {
	if thread.ID == "" {
		thread.ID = newID()
	}
	if thread.Title == "" {
		thread.Title = "Untitled handoff"
	}
	now := time.Now().UTC()
	if thread.CreatedAt.IsZero() {
		thread.CreatedAt = now
	}
	thread.UpdatedAt = thread.CreatedAt
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO threads(
		  id, title, repo_root, baseline_commit, branch, worktree_path,
		  read_only, revision, created_at, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		thread.ID, thread.Title, thread.RepoRoot, thread.BaselineCommit,
		thread.Branch, thread.WorktreePath, thread.ReadOnly, thread.Revision,
		millis(thread.CreatedAt), millis(thread.UpdatedAt))
	if err != nil {
		return domain.Thread{}, fmt.Errorf("create thread: %w", err)
	}
	return thread, nil
}

func (s *Store) GetThread(ctx context.Context, id string) (domain.Thread, error) {
	return scanThread(s.db.QueryRowContext(ctx, `
		SELECT id, title, repo_root, baseline_commit, branch, worktree_path,
		       read_only, revision, created_at, updated_at
		FROM threads WHERE id = ?`, id))
}

func (s *Store) ListThreads(ctx context.Context) ([]domain.Thread, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, repo_root, baseline_commit, branch, worktree_path,
		       read_only, revision, created_at, updated_at
		FROM threads ORDER BY updated_at DESC, id`)
	if err != nil {
		return nil, fmt.Errorf("list threads: %w", err)
	}
	defer rows.Close()
	var out []domain.Thread
	for rows.Next() {
		thread, err := scanThread(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, thread)
	}
	return out, rows.Err()
}

// SetThreadWorktree performs an optimistic update and advances Revision.
func (s *Store) SetThreadWorktree(ctx context.Context, id, path string, expectedRevision int64) (domain.Thread, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
		UPDATE threads SET worktree_path = ?, revision = revision + 1, updated_at = ?
		WHERE id = ? AND revision = ?`, path, millis(now), id, expectedRevision)
	if err != nil {
		return domain.Thread{}, fmt.Errorf("set worktree: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return domain.Thread{}, err
	}
	if changed == 0 {
		if _, getErr := s.GetThread(ctx, id); errors.Is(getErr, domain.ErrNotFound) {
			return domain.Thread{}, domain.ErrNotFound
		}
		return domain.Thread{}, domain.ErrRevisionConflict
	}
	return s.GetThread(ctx, id)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanThread(row rowScanner) (domain.Thread, error) {
	var t domain.Thread
	var readOnly bool
	var created, updated int64
	if err := row.Scan(&t.ID, &t.Title, &t.RepoRoot, &t.BaselineCommit,
		&t.Branch, &t.WorktreePath, &readOnly, &t.Revision, &created, &updated); err != nil {
		return domain.Thread{}, mapNotFound(err)
	}
	t.ReadOnly = readOnly
	t.CreatedAt = fromMillis(created)
	t.UpdatedAt = fromMillis(updated)
	return t, nil
}

// CreateGitSnapshot records object references for a captured Git state.
func (s *Store) CreateGitSnapshot(ctx context.Context, snap domain.GitSnapshot) (domain.GitSnapshot, error) {
	if snap.ID == "" {
		snap.ID = newID()
	}
	if snap.CreatedAt.IsZero() {
		snap.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO git_snapshots(
		  id, thread_id, head, branch, unborn, status_object,
		  staged_patch_object, unstaged_patch_object, untracked_manifest_object, created_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snap.ID, snap.ThreadID, snap.Head, snap.Branch, snap.Unborn,
		nullString(snap.StatusObject), nullString(snap.StagedPatchObject),
		nullString(snap.UnstagedPatchObject), nullString(snap.UntrackedManifestObject),
		millis(snap.CreatedAt))
	if err != nil {
		return domain.GitSnapshot{}, fmt.Errorf("create git snapshot: %w", err)
	}
	return snap, nil
}

func (s *Store) GetGitSnapshot(ctx context.Context, id string) (domain.GitSnapshot, error) {
	var snap domain.GitSnapshot
	var unborn bool
	var status, staged, unstaged, untracked sql.NullString
	var created int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, thread_id, head, branch, unborn, status_object,
		       staged_patch_object, unstaged_patch_object, untracked_manifest_object, created_at
		FROM git_snapshots WHERE id = ?`, id).Scan(
		&snap.ID, &snap.ThreadID, &snap.Head, &snap.Branch, &unborn,
		&status, &staged, &unstaged, &untracked, &created)
	if err != nil {
		return domain.GitSnapshot{}, mapNotFound(err)
	}
	snap.Unborn = unborn
	snap.StatusObject = status.String
	snap.StagedPatchObject = staged.String
	snap.UnstagedPatchObject = unstaged.String
	snap.UntrackedManifestObject = untracked.String
	snap.CreatedAt = fromMillis(created)
	return snap, nil
}

func (s *Store) CreateRound(ctx context.Context, round domain.Round) (domain.Round, error) {
	if round.ID == "" {
		round.ID = newID()
	}
	if round.Kind == "" {
		round.Kind = "checkpoint"
	}
	if round.CreatedAt.IsZero() {
		round.CreatedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Round{}, err
	}
	defer tx.Rollback()
	var next int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(round_number), -1) + 1 FROM rounds WHERE thread_id = ?`,
		round.ThreadID).Scan(&next); err != nil {
		return domain.Round{}, fmt.Errorf("read round sequence: %w", err)
	}
	if round.Number < next {
		return domain.Round{}, domain.ErrRoundExists
	}
	if round.Number > next {
		return domain.Round{}, domain.ErrRoundSequence
	}
	if round.Number == 0 && round.ParentRoundID != "" {
		return domain.Round{}, domain.ErrRoundSequence
	}
	if round.Number > 0 {
		var previousID string
		if err := tx.QueryRowContext(ctx,
			`SELECT id FROM rounds WHERE thread_id = ? AND round_number = ?`,
			round.ThreadID, round.Number-1).Scan(&previousID); err != nil {
			return domain.Round{}, mapNotFound(err)
		}
		if round.ParentRoundID == "" {
			round.ParentRoundID = previousID
		} else if round.ParentRoundID != previousID {
			return domain.Round{}, domain.ErrRoundSequence
		}
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO rounds(
		  id, thread_id, round_number, parent_round_id, kind, summary,
		  snapshot_id, agent_run_id, event_from_seq, event_to_seq,
		  manifest_object, created_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		round.ID, round.ThreadID, round.Number, nullString(round.ParentRoundID),
		round.Kind, round.Summary, nullString(round.SnapshotID),
		nullString(round.AgentRunID), round.EventFromSeq, round.EventToSeq,
		nullString(round.ManifestObject), millis(round.CreatedAt))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return domain.Round{}, domain.ErrRoundExists
		}
		return domain.Round{}, fmt.Errorf("create round: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Round{}, fmt.Errorf("commit round: %w", err)
	}
	return round, nil
}

func (s *Store) GetRound(ctx context.Context, id string) (domain.Round, error) {
	return scanRound(s.db.QueryRowContext(ctx, `
		SELECT id, thread_id, round_number, parent_round_id, kind, summary,
		       snapshot_id, agent_run_id, event_from_seq, event_to_seq,
		       manifest_object, created_at
		FROM rounds WHERE id = ?`, id))
}

func (s *Store) ListRounds(ctx context.Context, threadID string) ([]domain.Round, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, thread_id, round_number, parent_round_id, kind, summary,
		       snapshot_id, agent_run_id, event_from_seq, event_to_seq,
		       manifest_object, created_at
		FROM rounds WHERE thread_id = ? ORDER BY round_number`, threadID)
	if err != nil {
		return nil, fmt.Errorf("list rounds: %w", err)
	}
	defer rows.Close()
	var rounds []domain.Round
	for rows.Next() {
		round, err := scanRound(rows)
		if err != nil {
			return nil, err
		}
		rounds = append(rounds, round)
	}
	return rounds, rows.Err()
}

func scanRound(row rowScanner) (domain.Round, error) {
	var r domain.Round
	var parent, snapshot, run, manifest sql.NullString
	var created int64
	err := row.Scan(&r.ID, &r.ThreadID, &r.Number, &parent, &r.Kind, &r.Summary,
		&snapshot, &run, &r.EventFromSeq, &r.EventToSeq, &manifest, &created)
	if err != nil {
		return domain.Round{}, mapNotFound(err)
	}
	r.ParentRoundID = parent.String
	r.SnapshotID = snapshot.String
	r.AgentRunID = run.String
	r.ManifestObject = manifest.String
	r.CreatedAt = fromMillis(created)
	return r, nil
}
