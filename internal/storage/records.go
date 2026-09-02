package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"teamcross/internal/domain"
)

func newID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	// Set RFC 4122 version/variant bits while avoiding a package dependency.
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(raw[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}

func (s *Store) CreateEvidence(ctx context.Context, evidence domain.Evidence) (domain.Evidence, error) {
	if evidence.ID == "" {
		evidence.ID = newID()
	}
	if evidence.CreatedAt.IsZero() {
		evidence.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO evidence(id, thread_id, round_id, kind, title, source, object_hash, metadata, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, evidence.ID, evidence.ThreadID,
		nullString(evidence.RoundID), evidence.Kind, evidence.Title, evidence.Source,
		nullString(evidence.ObjectHash), evidence.Metadata, millis(evidence.CreatedAt))
	if err != nil {
		return domain.Evidence{}, fmt.Errorf("create evidence: %w", err)
	}
	return evidence, nil
}

func (s *Store) ListEvidence(ctx context.Context, threadID string) ([]domain.Evidence, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, thread_id, round_id, kind, title, source, object_hash, metadata, created_at
		FROM evidence WHERE thread_id = ? ORDER BY created_at, id`, threadID)
	if err != nil {
		return nil, fmt.Errorf("list evidence: %w", err)
	}
	defer rows.Close()
	var result []domain.Evidence
	for rows.Next() {
		var e domain.Evidence
		var roundID, objectHash sql.NullString
		var created int64
		if err := rows.Scan(&e.ID, &e.ThreadID, &roundID, &e.Kind, &e.Title,
			&e.Source, &objectHash, &e.Metadata, &created); err != nil {
			return nil, err
		}
		e.RoundID = roundID.String
		e.ObjectHash = objectHash.String
		e.CreatedAt = fromMillis(created)
		result = append(result, e)
	}
	return result, rows.Err()
}

func (s *Store) GetEvidence(ctx context.Context, threadID, evidenceID string) (domain.Evidence, error) {
	var evidence domain.Evidence
	var roundID, objectHash sql.NullString
	var created int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, thread_id, round_id, kind, title, source, object_hash, metadata, created_at
		FROM evidence WHERE thread_id = ? AND id = ?`, threadID, evidenceID).Scan(
		&evidence.ID, &evidence.ThreadID, &roundID, &evidence.Kind, &evidence.Title,
		&evidence.Source, &objectHash, &evidence.Metadata, &created)
	if err != nil {
		return domain.Evidence{}, mapNotFound(err)
	}
	evidence.RoundID = roundID.String
	evidence.ObjectHash = objectHash.String
	evidence.CreatedAt = fromMillis(created)
	return evidence, nil
}

func (s *Store) CreateAgentRun(ctx context.Context, run domain.AgentRun) (domain.AgentRun, error) {
	if run.ID == "" {
		run.ID = newID()
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}
	if run.Status == "" {
		run.Status = "starting"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_runs(id, thread_id, provider, session_id, status, started_at, closed_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`, run.ID, run.ThreadID, run.Provider,
		run.SessionID, run.Status, millis(run.StartedAt), nullMillis(run.ClosedAt))
	if err != nil {
		return domain.AgentRun{}, fmt.Errorf("create agent run: %w", err)
	}
	return run, nil
}

func (s *Store) UpdateAgentRun(ctx context.Context, runID, sessionID, status string, closedAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE agent_runs SET session_id = ?, status = ?, closed_at = ? WHERE id = ?`,
		sessionID, status, nullMillis(closedAt), runID)
	if err != nil {
		return fmt.Errorf("update agent run: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) GetAgentRun(ctx context.Context, runID string) (domain.AgentRun, error) {
	return scanAgentRun(s.db.QueryRowContext(ctx, `
		SELECT id, thread_id, provider, session_id, status, started_at, closed_at
		FROM agent_runs WHERE id = ?`, runID))
}

func (s *Store) ListAgentRuns(ctx context.Context, threadID string) ([]domain.AgentRun, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, thread_id, provider, session_id, status, started_at, closed_at
		FROM agent_runs WHERE thread_id = ? ORDER BY started_at, id`, threadID)
	if err != nil {
		return nil, fmt.Errorf("list agent runs: %w", err)
	}
	defer rows.Close()
	var runs []domain.AgentRun
	for rows.Next() {
		run, err := scanAgentRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func scanAgentRun(row rowScanner) (domain.AgentRun, error) {
	var run domain.AgentRun
	var started int64
	var closed sql.NullInt64
	err := row.Scan(&run.ID, &run.ThreadID, &run.Provider, &run.SessionID,
		&run.Status, &started, &closed)
	if err != nil {
		return domain.AgentRun{}, mapNotFound(err)
	}
	run.StartedAt = fromMillis(started)
	if closed.Valid {
		run.ClosedAt = fromMillis(closed.Int64)
	}
	return run, nil
}
