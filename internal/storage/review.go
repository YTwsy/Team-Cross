package storage

import (
	"context"
	"encoding/json"
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
	_, err := s.db.ExecContext(ctx, "INSERT INTO share_projections(share_id,payload) VALUES(?,?)", shareID, payload)
	return err
}
func (s *Store) GetShareProjection(ctx context.Context, shareID string) ([]byte, error) {
	var data []byte
	err := s.db.QueryRowContext(ctx, "SELECT payload FROM share_projections WHERE share_id=?", shareID).Scan(&data)
	return data, mapNotFound(err)
}
func (s *Store) SaveRunBinding(ctx context.Context, runID string, payload []byte) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO run_bindings(run_id,payload) VALUES(?,?) ON CONFLICT(run_id) DO UPDATE SET payload=excluded.payload", runID, payload)
	return err
}
func targetBytes(target *domain.AnnotationTarget) []byte {
	if target == nil {
		return nil
	}
	data, _ := json.Marshal(target)
	return data
}
