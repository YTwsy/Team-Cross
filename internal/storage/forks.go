package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"teamcross/internal/domain"
)

// CreateForkImport publishes a complete fork atomically. Objects and an isolated
// staging repository may be prepared first, but no partially imported writable
// Thread is visible if any record fails validation or insertion.
func (store *Store) CreateForkImport(ctx context.Context, thread domain.Thread, snapshot domain.GitSnapshot, round domain.Round, evidence []domain.Evidence, sessions []domain.SessionSnapshot, origin []byte) (domain.Thread, error) {
	if thread.ID == "" || snapshot.ID == "" || round.ID == "" || thread.BaselineCommit == "" || thread.WorktreePath == "" || thread.ReadOnly || snapshot.ThreadID != thread.ID || snapshot.Head != thread.BaselineCommit || round.ThreadID != thread.ID || round.SnapshotID != snapshot.ID || round.Number != 0 || round.ParentRoundID != "" || round.AgentRunID != "" {
		return domain.Thread{}, errors.New("invalid complete fork import")
	}
	now := time.Now().UTC()
	thread.CreatedAt, thread.UpdatedAt, thread.Revision = now, now, 1
	snapshot.CreatedAt, round.CreatedAt = now, now
	// The object table is prepared before BeginTx to avoid nesting a write on
	// Store's SQLite connection; unreferenced immutable CAS objects are harmless.
	sessionObjects := make(map[string]string, len(sessions))
	for _, session := range sessions {
		if session.ID == "" || session.ThreadID != thread.ID {
			return domain.Thread{}, errors.New("invalid fork Session snapshot binding")
		}
		data, err := json.Marshal(session)
		if err != nil {
			return domain.Thread{}, err
		}
		object, err := store.PutObject(ctx, data, "application/vnd.teamcross.session-snapshot+json")
		if err != nil {
			return domain.Thread{}, err
		}
		sessionObjects[session.ID] = object.Hash
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Thread{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO threads(id,title,repo_root,baseline_commit,branch,worktree_path,read_only,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, thread.ID, thread.Title, thread.RepoRoot, thread.BaselineCommit, thread.Branch, thread.WorktreePath, false, thread.Revision, millis(now), millis(now)); err != nil {
		return domain.Thread{}, fmt.Errorf("insert fork Thread: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO git_snapshots(id,thread_id,head,branch,unborn,status_object,staged_patch_object,unstaged_patch_object,untracked_manifest_object,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, snapshot.ID, thread.ID, snapshot.Head, snapshot.Branch, false, nullString(snapshot.StatusObject), nullString(snapshot.StagedPatchObject), nullString(snapshot.UnstagedPatchObject), nullString(snapshot.UntrackedManifestObject), millis(now)); err != nil {
		return domain.Thread{}, fmt.Errorf("insert fork Git snapshot: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO rounds(id,thread_id,round_number,parent_round_id,kind,summary,snapshot_id,agent_run_id,event_from_seq,event_to_seq,manifest_object,created_at) VALUES(?,?,0,NULL,?,?,?,NULL,0,0,?,?)`, round.ID, thread.ID, round.Kind, round.Summary, snapshot.ID, nullString(round.ManifestObject), millis(now)); err != nil {
		return domain.Thread{}, fmt.Errorf("insert fork Round: %w", err)
	}
	for _, item := range evidence {
		if item.ID == "" || item.ThreadID != thread.ID || item.RoundID != round.ID {
			return domain.Thread{}, errors.New("invalid fork evidence binding")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO evidence(id,thread_id,round_id,kind,title,source,object_hash,metadata,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, item.ID, thread.ID, round.ID, item.Kind, item.Title, item.Source, nullString(item.ObjectHash), item.Metadata, millis(now)); err != nil {
			return domain.Thread{}, fmt.Errorf("insert fork evidence: %w", err)
		}
	}
	for _, session := range sessions {
		if _, err := tx.ExecContext(ctx, `INSERT INTO session_snapshots(id,thread_id,object_hash,captured_at) VALUES(?,?,?,?)`, session.ID, thread.ID, sessionObjects[session.ID], millis(session.CapturedAt)); err != nil {
			return domain.Thread{}, fmt.Errorf("insert fork Session snapshot: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO events(thread_id,event_type,payload,created_at) VALUES(?,?,?,?)`, thread.ID, "thread.forked", origin, millis(now)); err != nil {
		return domain.Thread{}, fmt.Errorf("insert fork origin event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Thread{}, err
	}
	return thread, nil
}
