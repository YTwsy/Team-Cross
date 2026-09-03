package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"teamcross/internal/domain"
	"time"
)

func (s *Store) CreateSessionSnapshot(ctx context.Context, snapshot domain.SessionSnapshot) (domain.SessionSnapshot, error) {
	if snapshot.ID == "" {
		snapshot.ID = newID()
	}
	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = time.Now().UTC()
	}
	if snapshot.Entries == nil {
		snapshot.Entries = []domain.SessionEntry{}
	}
	if snapshot.Warnings == nil {
		snapshot.Warnings = []string{}
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return domain.SessionSnapshot{}, err
	}
	object, err := s.PutObject(ctx, data, "application/vnd.teamcross.session-snapshot+json")
	if err != nil {
		return domain.SessionSnapshot{}, err
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO session_snapshots(id,thread_id,object_hash,captured_at) VALUES(?,?,?,?)", snapshot.ID, snapshot.ThreadID, object.Hash, millis(snapshot.CapturedAt))
	if err != nil {
		return domain.SessionSnapshot{}, fmt.Errorf("create session snapshot: %w", err)
	}
	return snapshot, nil
}

func (s *Store) GetSessionSnapshot(ctx context.Context, threadID, id string) (domain.SessionSnapshot, error) {
	var hash string
	err := s.db.QueryRowContext(ctx, "SELECT object_hash FROM session_snapshots WHERE thread_id=? AND id=?", threadID, id).Scan(&hash)
	if err != nil {
		return domain.SessionSnapshot{}, mapNotFound(err)
	}
	var snapshot domain.SessionSnapshot
	data, err := s.GetObject(ctx, hash)
	if err == nil {
		err = json.Unmarshal(data, &snapshot)
	}
	return snapshot, err
}

func (s *Store) ListSessionSnapshots(ctx context.Context, threadID string) ([]domain.SessionSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT object_hash FROM session_snapshots WHERE thread_id=? ORDER BY captured_at,id", threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hashes := []string{}
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, err
		}
		hashes = append(hashes, hash)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	out := []domain.SessionSnapshot{}
	for _, hash := range hashes {
		data, err := s.GetObject(ctx, hash)
		if err != nil {
			return nil, err
		}
		var snapshot domain.SessionSnapshot
		if err := json.Unmarshal(data, &snapshot); err != nil {
			return nil, err
		}
		out = append(out, snapshot)
	}
	return out, nil
}

func (s *Store) SaveShareProjection(ctx context.Context, shareID string, payload []byte) error {
	return s.SaveScopedShareProjection(ctx, shareID, payload, nil)
}
func (s *Store) GetShareProjection(ctx context.Context, shareID string) ([]byte, error) {
	var data []byte
	err := s.db.QueryRowContext(ctx, "SELECT payload FROM share_projections WHERE share_id=?", shareID).Scan(&data)
	return data, mapNotFound(err)
}
func (s *Store) SaveRunBinding(ctx context.Context, runID string, payload []byte) error {
	tx, run, err := s.beginRunBindingUpdate(ctx, runID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	existing, err := readRunBinding(ctx, tx, runID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	merged, err := mergeRunBinding(existing, payload, run)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO run_bindings(run_id,payload) VALUES(?,?) ON CONFLICT(run_id) DO UPDATE SET payload=excluded.payload", runID, merged); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetRunBinding(ctx context.Context, runID string) ([]byte, error) {
	// Project a legacy binding using the authoritative Run from the same read
	// snapshot, so old placeholder payloads are never exposed as native IDs.
	var payload []byte
	var run domain.AgentRun
	run.ID = runID
	err := s.db.QueryRowContext(ctx, `SELECT b.payload,r.provider,r.session_id FROM run_bindings b JOIN agent_runs r ON r.id=b.run_id WHERE b.run_id=?`, runID).Scan(&payload, &run.Provider, &run.SessionID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	return mergeRunBinding(payload, nil, run)
}

// BindAgentRunIdentity persists the first actual Provider identity and updates
// the binding in one transaction. Source/Fork/execution metadata is not replaced.
func (s *Store) BindAgentRunIdentity(ctx context.Context, runID, provider, candidate string) (string, error) {
	tx, run, err := s.beginRunBindingUpdate(ctx, runID)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if provider != run.Provider {
		return run.SessionID, fmt.Errorf("Provider does not match Run: %w", domain.ErrSessionIdentityConflict)
	}
	identity, err := domain.AdoptSessionID(provider, runID, run.SessionID, candidate)
	if err != nil {
		return run.SessionID, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE agent_runs SET session_id=? WHERE id=?", identity, runID); err != nil {
		return "", err
	}
	run.SessionID = identity
	existing, err := readRunBinding(ctx, tx, runID)
	if err == nil {
		merged, mergeErr := mergeRunBinding(existing, nil, run)
		if mergeErr != nil {
			return "", mergeErr
		}
		if _, err := tx.ExecContext(ctx, "UPDATE run_bindings SET payload=? WHERE run_id=?", merged, runID); err != nil {
			return "", err
		}
	} else if !errors.Is(err, domain.ErrNotFound) {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return identity, nil
}

// Acquire the SQLite writer before reading. A deferred read-then-write
// transaction can fail to upgrade its WAL snapshot during concurrent events.
func (s *Store) beginRunBindingUpdate(ctx context.Context, runID string) (*sql.Tx, domain.AgentRun, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, domain.AgentRun{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE agent_runs SET session_id=session_id WHERE id=?", runID); err != nil {
		tx.Rollback()
		return nil, domain.AgentRun{}, err
	}
	run, err := scanAgentRun(tx.QueryRowContext(ctx, `SELECT id,thread_id,provider,session_id,status,started_at,closed_at FROM agent_runs WHERE id=?`, runID))
	if err != nil {
		tx.Rollback()
		return nil, domain.AgentRun{}, err
	}
	return tx, run, nil
}

func readRunBinding(ctx context.Context, query interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, runID string) ([]byte, error) {
	var payload []byte
	err := query.QueryRowContext(ctx, "SELECT payload FROM run_bindings WHERE run_id=?", runID).Scan(&payload)
	return payload, mapNotFound(err)
}

func mergeRunBinding(existing, incoming []byte, run domain.AgentRun) ([]byte, error) {
	merged := map[string]json.RawMessage{}
	ref := map[string]json.RawMessage{}
	for _, payload := range [][]byte{existing, incoming} {
		if len(payload) == 0 {
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(payload, &fields); err != nil || fields == nil {
			return nil, fmt.Errorf("Run binding must be a JSON object")
		}
		for key, value := range fields {
			if key != "sessionRef" {
				merged[key] = value
				continue
			}
			var supplied map[string]json.RawMessage
			if err := json.Unmarshal(value, &supplied); err != nil || supplied == nil {
				return nil, fmt.Errorf("Run sessionRef must be a JSON object")
			}
			for field, raw := range supplied {
				ref[field] = raw
			}
		}
	}
	ref["provider"], _ = json.Marshal(run.Provider)
	ref["sessionId"], _ = json.Marshal(domain.CanonicalSessionID(run.Provider, run.ID, run.SessionID))
	merged["sessionRef"], _ = json.Marshal(ref)
	return json.Marshal(merged)
}
func targetBytes(target *domain.AnnotationTarget) []byte {
	if target == nil {
		return nil
	}
	data, _ := json.Marshal(target)
	return data
}
