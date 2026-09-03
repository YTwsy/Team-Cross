package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"teamcross/internal/domain"
)

const followColumns = "payload,cursor"

func scanFollow(row interface{ Scan(...any) error }) (domain.SessionFollow, error) {
	var follow domain.SessionFollow
	var data []byte
	var cursor string
	if err := row.Scan(&data, &cursor); err != nil {
		return follow, mapNotFound(err)
	}
	if err := json.Unmarshal(data, &follow); err != nil {
		return follow, err
	}
	follow.Cursor = cursor
	return follow, nil
}

func (s *Store) GetSessionFollow(ctx context.Context, threadID, id string) (domain.SessionFollow, error) {
	return scanFollow(s.db.QueryRowContext(ctx, "SELECT "+followColumns+" FROM session_follows WHERE thread_id=? AND id=?", threadID, id))
}

func (s *Store) ListSessionFollows(ctx context.Context, threadID string) ([]domain.SessionFollow, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+followColumns+" FROM session_follows WHERE (?='' OR thread_id=?) ORDER BY rowid", threadID, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.SessionFollow{}
	for rows.Next() {
		follow, err := scanFollow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, follow)
	}
	return result, rows.Err()
}

// CreateSessionFollow is idempotent for one active source in a Thread. Starting
// a reader does not create snapshots, Rounds, worktrees, Runs, or model prompts.
func (s *Store) CreateSessionFollow(ctx context.Context, threadID, snapshotID string) (domain.SessionFollow, bool, error) {
	snapshot, err := s.GetSessionSnapshot(ctx, threadID, snapshotID)
	if err != nil {
		return domain.SessionFollow{}, false, err
	}
	if !snapshot.Capabilities.Follow || !snapshot.Capabilities.Read {
		return domain.SessionFollow{}, false, domain.ErrFollowUnsupported
	}
	follow := domain.SessionFollow{ID: newID(), ThreadID: threadID, Source: snapshot.Source, SourceSnapshotID: snapshot.ID, CurrentSnapshotID: snapshot.ID, State: "active", Epoch: 1, UpdatedAt: time.Now().UTC(), Gaps: []string{}}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return follow, false, err
	}
	defer tx.Rollback()
	existing, err := scanFollow(tx.QueryRowContext(ctx, "SELECT "+followColumns+" FROM session_follows WHERE thread_id=? AND provider=? AND session_id=? AND state!='stopped'", threadID, follow.Source.Provider, follow.Source.SessionID))
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return follow, false, err
	}
	data, err := json.Marshal(follow)
	if err != nil {
		return follow, false, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO session_follows(id,thread_id,provider,session_id,state,epoch,cursor,current_snapshot_id,payload) VALUES(?,?,?,?,?,1,'',?,?)", follow.ID, threadID, follow.Source.Provider, follow.Source.SessionID, follow.State, snapshot.ID, data); err != nil {
		return follow, false, err
	}
	if err = appendFollowEvent(ctx, tx, follow, "session.follow.started", ""); err != nil {
		return follow, false, err
	}
	return follow, true, tx.Commit()
}

func appendFollowEvent(ctx context.Context, tx *sql.Tx, follow domain.SessionFollow, kind, roundID string) error {
	if _, err := tx.ExecContext(ctx, "UPDATE threads SET revision=revision+1,updated_at=? WHERE id=?", millis(follow.UpdatedAt), follow.ThreadID); err != nil {
		return err
	}
	var revision int64
	if err := tx.QueryRowContext(ctx, "SELECT revision FROM threads WHERE id=?", follow.ThreadID).Scan(&revision); err != nil {
		return err
	}
	// No Provider text, cursor, paths, or errors in the public event stream.
	payload, err := json.Marshal(map[string]any{"followId": follow.ID, "snapshotId": follow.CurrentSnapshotID, "roundId": roundID, "state": follow.State, "revision": revision, "actor": "Owner"})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO events(thread_id,event_type,payload,created_at) VALUES(?,?,?,?)", follow.ThreadID, kind, payload, millis(follow.UpdatedAt))
	return err
}

func saveFollow(ctx context.Context, tx *sql.Tx, previous, next domain.SessionFollow) error {
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE session_follows SET state=?,epoch=?,cursor=?,current_snapshot_id=?,payload=? WHERE id=? AND thread_id=? AND state!='stopped' AND epoch=? AND cursor=? AND current_snapshot_id=?`, next.State, next.Epoch, next.Cursor, next.CurrentSnapshotID, data, previous.ID, previous.ThreadID, previous.Epoch, previous.Cursor, previous.CurrentSnapshotID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count != 1 {
		err = domain.ErrFollowFence
	}
	return err
}

func (s *Store) StopSessionFollow(ctx context.Context, threadID, id string) (domain.SessionFollow, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.SessionFollow{}, err
	}
	defer tx.Rollback()
	follow, err := scanFollow(tx.QueryRowContext(ctx, "SELECT "+followColumns+" FROM session_follows WHERE thread_id=? AND id=?", threadID, id))
	if err != nil || follow.State == "stopped" {
		return follow, err
	}
	stopped := follow
	stopped.State, stopped.Epoch, stopped.UpdatedAt = "stopped", follow.Epoch+1, time.Now().UTC()
	stopped.Reason = "Owner 已停止只读 Follow；历史快照保持不变。"
	if err = saveFollow(ctx, tx, follow, stopped); err != nil {
		return follow, err
	}
	if err = appendFollowEvent(ctx, tx, stopped, "session.follow.stopped", ""); err != nil {
		return follow, err
	}
	return stopped, tx.Commit()
}

func (s *Store) RecordFollowRetry(ctx context.Context, previous domain.SessionFollow, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := scanFollow(tx.QueryRowContext(ctx, "SELECT "+followColumns+" FROM session_follows WHERE thread_id=? AND id=?", previous.ThreadID, previous.ID))
	if err != nil {
		return err
	}
	if current.State == "stopped" || current.Epoch != previous.Epoch || current.Cursor != previous.Cursor || current.CurrentSnapshotID != previous.CurrentSnapshotID {
		return domain.ErrFollowFence
	}
	if current.State == "retrying" && current.Reason == reason {
		return nil
	}
	next := current
	next.State, next.Reason, next.UpdatedAt = "retrying", reason, time.Now().UTC()
	if err = saveFollow(ctx, tx, current, next); err != nil {
		return err
	}
	if err = appendFollowEvent(ctx, tx, next, "session.follow.retrying", ""); err != nil {
		return err
	}
	return tx.Commit()
}

// CommitFollowPoll publishes a new immutable capture, optional Round and cursor
// in the SAME transaction. A stopped/stale reader cannot publish late output.
// A nil snapshot is an unchanged read and advances only the cursor/check time.
func (s *Store) CommitFollowPoll(ctx context.Context, previous, next domain.SessionFollow, snapshot *domain.SessionSnapshot, round *domain.Round) error {
	var objectHash string
	if (snapshot == nil) != (round == nil) {
		return fmt.Errorf("Follow capture and Round must be committed together")
	}
	if snapshot == nil && next.CurrentSnapshotID != previous.CurrentSnapshotID {
		return fmt.Errorf("unchanged Follow cannot replace its snapshot")
	}
	if snapshot != nil {
		if snapshot.ID == "" || snapshot.ThreadID != previous.ThreadID || round.ID == "" || round.ThreadID != previous.ThreadID || next.CurrentSnapshotID != snapshot.ID || snapshot.Source.Provider != previous.Source.Provider || snapshot.Source.SessionID != previous.Source.SessionID {
			return fmt.Errorf("invalid Follow capture identity")
		}
		data, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		object, err := s.PutObject(ctx, data, "application/vnd.teamcross.session-snapshot+json")
		if err != nil {
			return err
		}
		objectHash = object.Hash
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Take the writer lock before reading the checkpoint. Live Share creation
	// takes the same SQLite writer lock, so its seed cannot miss this admission.
	if _, err = tx.ExecContext(ctx, "UPDATE session_follows SET cursor=cursor WHERE thread_id=? AND id=?", previous.ThreadID, previous.ID); err != nil {
		return err
	}
	current, err := scanFollow(tx.QueryRowContext(ctx, "SELECT "+followColumns+" FROM session_follows WHERE thread_id=? AND id=?", previous.ThreadID, previous.ID))
	if err != nil {
		return err
	}
	if current.State == "stopped" || current.Epoch != previous.Epoch || current.Cursor != previous.Cursor || current.CurrentSnapshotID != previous.CurrentSnapshotID {
		return domain.ErrFollowFence
	}
	if next.ID != previous.ID || next.ThreadID != previous.ThreadID || next.Epoch != previous.Epoch || next.Source.Provider != previous.Source.Provider || next.Source.SessionID != previous.Source.SessionID || next.State != "active" {
		return fmt.Errorf("invalid Follow checkpoint")
	}
	if snapshot != nil {
		var number int64
		if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(round_number),-1)+1 FROM rounds WHERE thread_id=?", previous.ThreadID).Scan(&number); err != nil {
			return err
		}
		if number != round.Number {
			return domain.ErrRoundSequence
		}
		if number > 0 {
			var parent string
			if err = tx.QueryRowContext(ctx, "SELECT id FROM rounds WHERE thread_id=? AND round_number=?", previous.ThreadID, number-1).Scan(&parent); err != nil {
				return err
			}
			if parent != round.ParentRoundID {
				return domain.ErrRoundSequence
			}
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO session_snapshots(id,thread_id,object_hash,captured_at) VALUES(?,?,?,?)", snapshot.ID, snapshot.ThreadID, objectHash, millis(snapshot.CapturedAt)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO rounds(id,thread_id,round_number,parent_round_id,kind,summary,snapshot_id,manifest_object,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, round.ID, round.ThreadID, round.Number, nullString(round.ParentRoundID), round.Kind, round.Summary, nullString(round.SnapshotID), nullString(round.ManifestObject), millis(round.CreatedAt)); err != nil {
			return err
		}
	}
	if err = saveFollow(ctx, tx, previous, next); err != nil {
		return err
	}
	if snapshot != nil {
		if err = s.admitNativeLivePoll(ctx, tx, next); err != nil {
			return err
		}
	}
	// Make the first successful read observable even if it matches the imported
	// snapshot. Otherwise an idle Session could display "never read" forever.
	firstRead := current.LastPolledAt == nil && next.LastPolledAt != nil
	if snapshot != nil || firstRead || current.State != next.State || current.Reason != next.Reason {
		roundID := ""
		if round != nil {
			roundID = round.ID
		}
		if err = appendFollowEvent(ctx, tx, next, "session.follow.updated", roundID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
