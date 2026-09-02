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

func (s *Store) AppendEvent(ctx context.Context, threadID, eventType string, payload []byte) (domain.Event, error) {
	return s.appendEvent(ctx, threadID, -1, eventType, payload)
}

// AppendEventExpected atomically checks and advances the Thread revision. It is
// the storage primitive used by remote commands carrying expected_revision.
func (s *Store) AppendEventExpected(ctx context.Context, threadID string, expectedRevision int64, eventType string, payload []byte) (domain.Event, error) {
	return s.appendEvent(ctx, threadID, expectedRevision, eventType, payload)
}

func (s *Store) appendEvent(ctx context.Context, threadID string, expectedRevision int64, eventType string, payload []byte) (domain.Event, error) {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Event{}, err
	}
	defer tx.Rollback()
	var result sql.Result
	if expectedRevision >= 0 {
		result, err = tx.ExecContext(ctx, `
			UPDATE threads SET revision = revision + 1, updated_at = ?
			WHERE id = ? AND revision = ?`, millis(now), threadID, expectedRevision)
	} else {
		result, err = tx.ExecContext(ctx, `
			UPDATE threads SET revision = revision + 1, updated_at = ? WHERE id = ?`,
			millis(now), threadID)
	}
	if err != nil {
		return domain.Event{}, fmt.Errorf("advance thread revision: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return domain.Event{}, err
	}
	if changed == 0 {
		var exists int
		getErr := tx.QueryRowContext(ctx, `SELECT 1 FROM threads WHERE id = ?`, threadID).Scan(&exists)
		if errors.Is(getErr, sql.ErrNoRows) {
			return domain.Event{}, domain.ErrNotFound
		}
		return domain.Event{}, domain.ErrRevisionConflict
	}
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM threads WHERE id = ?`, threadID).Scan(&revision); err != nil {
		return domain.Event{}, fmt.Errorf("read advanced thread revision: %w", err)
	}
	payload, err = eventPayloadWithRevision(payload, revision)
	if err != nil {
		return domain.Event{}, fmt.Errorf("add event revision: %w", err)
	}
	result, err = tx.ExecContext(ctx, `
		INSERT INTO events(thread_id, event_type, payload, created_at)
		VALUES(?, ?, ?, ?)`, threadID, eventType, payload, millis(now))
	if err != nil {
		return domain.Event{}, fmt.Errorf("append event: %w", err)
	}
	seq, err := result.LastInsertId()
	if err != nil {
		return domain.Event{}, fmt.Errorf("read event sequence: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Event{}, fmt.Errorf("commit event: %w", err)
	}
	return domain.Event{Seq: seq, ThreadID: threadID, Type: eventType, Payload: payload, CreatedAt: now}, nil
}

func (s *Store) EventsAfter(ctx context.Context, threadID string, afterSeq int64, limit int) ([]domain.Event, error) {
	if limit <= 0 || limit > 10_000 {
		limit = 1_000
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT seq, thread_id, event_type, payload, created_at
		FROM events WHERE thread_id = ? AND seq > ? ORDER BY seq LIMIT ?`,
		threadID, afterSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}
	defer rows.Close()
	var events []domain.Event
	for rows.Next() {
		var event domain.Event
		var created int64
		if err := rows.Scan(&event.Seq, &event.ThreadID, &event.Type, &event.Payload, &created); err != nil {
			return nil, err
		}
		event.CreatedAt = fromMillis(created)
		events = append(events, event)
	}
	return events, rows.Err()
}

// LatestEventSeq returns the thread's monotonic event cursor without loading or
// truncating the event history. A thread with no events has cursor zero.
func (s *Store) LatestEventSeq(ctx context.Context, threadID string) (int64, error) {
	var seq int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(seq), 0) FROM events WHERE thread_id = ?`, threadID).Scan(&seq); err != nil {
		return 0, fmt.Errorf("read latest event sequence: %w", err)
	}
	return seq, nil
}

func (s *Store) CreateAnnotation(ctx context.Context, annotation domain.Annotation) (domain.Annotation, error) {
	if annotation.ID == "" {
		annotation.ID = newID()
	}
	if annotation.CreatedAt.IsZero() {
		annotation.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO annotations(
		  id, thread_id, round_id, participant_id, path, start_line, end_line, body, created_at, target
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		annotation.ID, annotation.ThreadID, nullString(annotation.RoundID),
		nullString(annotation.ParticipantID), annotation.Path, annotation.StartLine,
		annotation.EndLine, annotation.Body, millis(annotation.CreatedAt), targetBytes(annotation.Target))
	if err != nil {
		return domain.Annotation{}, fmt.Errorf("create annotation: %w", err)
	}
	return annotation, nil
}

func (s *Store) CreateAnnotationExpected(ctx context.Context, annotation domain.Annotation, expectedRevision int64) (domain.Annotation, domain.Event, error) {
	if annotation.ID == "" {
		annotation.ID = newID()
	}
	if annotation.CreatedAt.IsZero() {
		annotation.CreatedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Annotation{}, domain.Event{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE threads SET revision = revision + 1, updated_at = ?
		WHERE id = ? AND revision = ?`, millis(annotation.CreatedAt), annotation.ThreadID, expectedRevision)
	if err != nil {
		return domain.Annotation{}, domain.Event{}, fmt.Errorf("advance annotation revision: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return domain.Annotation{}, domain.Event{}, err
	}
	if changed == 0 {
		var exists int
		getErr := tx.QueryRowContext(ctx, `SELECT 1 FROM threads WHERE id = ?`, annotation.ThreadID).Scan(&exists)
		if errors.Is(getErr, sql.ErrNoRows) {
			return domain.Annotation{}, domain.Event{}, domain.ErrNotFound
		}
		return domain.Annotation{}, domain.Event{}, domain.ErrRevisionConflict
	}
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM threads WHERE id = ?`, annotation.ThreadID).Scan(&revision); err != nil {
		return domain.Annotation{}, domain.Event{}, fmt.Errorf("read advanced annotation revision: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO annotations(
		  id, thread_id, round_id, participant_id, path, start_line, end_line, body, created_at, target
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		annotation.ID, annotation.ThreadID, nullString(annotation.RoundID),
		nullString(annotation.ParticipantID), annotation.Path, annotation.StartLine,
		annotation.EndLine, annotation.Body, millis(annotation.CreatedAt), targetBytes(annotation.Target)); err != nil {
		return domain.Annotation{}, domain.Event{}, fmt.Errorf("create annotation: %w", err)
	}
	payload, err := json.Marshal(annotation)
	if err != nil {
		return domain.Annotation{}, domain.Event{}, fmt.Errorf("encode annotation event: %w", err)
	}
	payload, err = eventPayloadWithRevision(payload, revision)
	if err != nil {
		return domain.Annotation{}, domain.Event{}, fmt.Errorf("add annotation event revision: %w", err)
	}
	result, err = tx.ExecContext(ctx, `
		INSERT INTO events(thread_id, event_type, payload, created_at)
		VALUES(?, 'annotation.created', ?, ?)`, annotation.ThreadID, payload, millis(annotation.CreatedAt))
	if err != nil {
		return domain.Annotation{}, domain.Event{}, fmt.Errorf("append annotation event: %w", err)
	}
	seq, err := result.LastInsertId()
	if err != nil {
		return domain.Annotation{}, domain.Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Annotation{}, domain.Event{}, fmt.Errorf("commit annotation: %w", err)
	}
	event := domain.Event{Seq: seq, ThreadID: annotation.ThreadID, Type: "annotation.created", Payload: payload, CreatedAt: annotation.CreatedAt}
	return annotation, event, nil
}

// eventPayloadWithRevision normalizes every event payload to a JSON object and
// makes the thread revision reached by the same transaction explicit. Existing
// object fields are preserved; non-object JSON and legacy opaque bytes are kept
// under value and rawPayload respectively.
func eventPayloadWithRevision(payload []byte, revision int64) ([]byte, error) {
	object := map[string]json.RawMessage{}
	if len(payload) > 0 {
		var existing map[string]json.RawMessage
		if err := json.Unmarshal(payload, &existing); err == nil && existing != nil {
			object = existing
		} else if json.Valid(payload) {
			object["value"] = append(json.RawMessage(nil), payload...)
		} else {
			raw, err := json.Marshal(string(payload))
			if err != nil {
				return nil, err
			}
			object["rawPayload"] = raw
		}
	}
	encodedRevision, err := json.Marshal(revision)
	if err != nil {
		return nil, err
	}
	object["revision"] = encodedRevision
	return json.Marshal(object)
}

func (s *Store) ListAnnotations(ctx context.Context, threadID string) ([]domain.Annotation, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, thread_id, round_id, participant_id, path, start_line, end_line, body, created_at, target
		FROM annotations WHERE thread_id = ? ORDER BY created_at, id`, threadID)
	if err != nil {
		return nil, fmt.Errorf("list annotations: %w", err)
	}
	defer rows.Close()
	var annotations []domain.Annotation
	for rows.Next() {
		var a domain.Annotation
		var roundID, participantID sql.NullString
		var created int64
		var target []byte
		if err := rows.Scan(&a.ID, &a.ThreadID, &roundID, &participantID,
			&a.Path, &a.StartLine, &a.EndLine, &a.Body, &created, &target); err != nil {
			return nil, err
		}
		a.RoundID = roundID.String
		a.ParticipantID = participantID.String
		a.CreatedAt = fromMillis(created)
		if len(target) > 0 {
			if err := json.Unmarshal(target, &a.Target); err != nil {
				return nil, err
			}
		}
		annotations = append(annotations, a)
	}
	return annotations, rows.Err()
}
