package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"teamcross/internal/domain"
)

const DefaultControlLeaseTTL = 60 * time.Second

func (s *Store) CreateShare(ctx context.Context, share domain.Share) (domain.Share, error) {
	if share.ID == "" {
		share.ID = newID()
	}
	if share.CreatedAt.IsZero() {
		share.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO shares(
		  id, thread_id, secret_hash, server_spki, capabilities, expires_at, revoked_at, created_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, share.ID, share.ThreadID, share.SecretHash,
		share.ServerSPKI, share.Capabilities, millis(share.ExpiresAt),
		nullMillis(share.RevokedAt), millis(share.CreatedAt))
	if err != nil {
		return domain.Share{}, fmt.Errorf("create share: %w", err)
	}
	return share, nil
}

func (s *Store) GetShare(ctx context.Context, id string) (domain.Share, error) {
	var share domain.Share
	var expires, created int64
	var revoked sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, thread_id, secret_hash, server_spki, capabilities, expires_at, revoked_at, created_at
		FROM shares WHERE id = ?`, id).Scan(&share.ID, &share.ThreadID, &share.SecretHash,
		&share.ServerSPKI, &share.Capabilities, &expires, &revoked, &created)
	if err != nil {
		return domain.Share{}, mapNotFound(err)
	}
	share.ExpiresAt = fromMillis(expires)
	if revoked.Valid {
		share.RevokedAt = fromMillis(revoked.Int64)
	}
	share.CreatedAt = fromMillis(created)
	return share, nil
}

func (s *Store) RevokeShare(ctx context.Context, id string, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE shares SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?`, millis(now), id)
	if err != nil {
		return fmt.Errorf("revoke share: %w", err)
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

func (s *Store) UpsertParticipant(ctx context.Context, participant domain.Participant) (domain.Participant, error) {
	if participant.ID == "" {
		participant.ID = newID()
	}
	now := time.Now().UTC()
	if participant.JoinedAt.IsZero() {
		participant.JoinedAt = now
	}
	if participant.LastSeen.IsZero() {
		participant.LastSeen = now
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO participants(id, share_id, name, role, joined_at, last_seen)
		VALUES(?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		  name = excluded.name, role = excluded.role, last_seen = excluded.last_seen
		WHERE participants.share_id = excluded.share_id`, participant.ID, participant.ShareID,
		participant.Name, participant.Role, millis(participant.JoinedAt), millis(participant.LastSeen))
	if err != nil {
		return domain.Participant{}, fmt.Errorf("upsert participant: %w", err)
	}
	return participant, nil
}

func (s *Store) ListParticipants(ctx context.Context, shareID string) ([]domain.Participant, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, share_id, name, role, joined_at, last_seen
		FROM participants WHERE share_id = ? ORDER BY joined_at, id`, shareID)
	if err != nil {
		return nil, fmt.Errorf("list participants: %w", err)
	}
	defer rows.Close()
	var result []domain.Participant
	for rows.Next() {
		var p domain.Participant
		var joined, seen int64
		if err := rows.Scan(&p.ID, &p.ShareID, &p.Name, &p.Role, &joined, &seen); err != nil {
			return nil, err
		}
		p.JoinedAt = fromMillis(joined)
		p.LastSeen = fromMillis(seen)
		result = append(result, p)
	}
	return result, rows.Err()
}

// ListThreadParticipants returns persisted participant identities across all
// shares for a thread. Most recently seen entries come first so callers can
// retain the latest display name when the same identity appears more than once.
func (s *Store) ListThreadParticipants(ctx context.Context, threadID string) ([]domain.Participant, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT participants.id, participants.share_id, participants.name,
		       participants.role, participants.joined_at, participants.last_seen
		FROM participants
		JOIN shares ON shares.id = participants.share_id
		WHERE shares.thread_id = ?
		ORDER BY participants.last_seen DESC, participants.id`, threadID)
	if err != nil {
		return nil, fmt.Errorf("list thread participants: %w", err)
	}
	defer rows.Close()
	result := make([]domain.Participant, 0)
	for rows.Next() {
		var participant domain.Participant
		var joined, seen int64
		if err := rows.Scan(&participant.ID, &participant.ShareID, &participant.Name,
			&participant.Role, &joined, &seen); err != nil {
			return nil, err
		}
		participant.JoinedAt = fromMillis(joined)
		participant.LastSeen = fromMillis(seen)
		result = append(result, participant)
	}
	return result, rows.Err()
}

// AcquireControl returns a 60 second fenced lease. An expired or released
// lease advances epoch before it is granted to a new participant.
func (s *Store) AcquireControl(ctx context.Context, shareID, participantID string, now time.Time, ttl time.Duration) (domain.ControlLease, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if ttl <= 0 {
		ttl = DefaultControlLeaseTTL
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ControlLease{}, err
	}
	defer tx.Rollback()
	lease, err := readLease(ctx, tx, shareID)
	if errors.Is(err, domain.ErrNotFound) {
		lease = domain.ControlLease{
			ShareID:       shareID,
			ParticipantID: participantID,
			Epoch:         1,
			ExpiresAt:     now.Add(ttl),
			UpdatedAt:     now,
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO control_leases(share_id, participant_id, epoch, expires_at, updated_at)
			VALUES(?, ?, ?, ?, ?)`, shareID, participantID, lease.Epoch,
			millis(lease.ExpiresAt), millis(now))
		if err != nil {
			return domain.ControlLease{}, fmt.Errorf("create control lease: %w", err)
		}
	} else if err != nil {
		return domain.ControlLease{}, err
	} else if lease.ParticipantID == participantID && lease.ExpiresAt.After(now) {
		// Acquiring an already-held lease is idempotent and does not change its
		// fencing token. Renewal has its own explicit operation.
	} else if lease.ParticipantID != "" && lease.ExpiresAt.After(now) {
		return lease, domain.ErrLeaseHeld
	} else {
		lease.ParticipantID = participantID
		lease.Epoch++
		lease.ExpiresAt = now.Add(ttl)
		lease.UpdatedAt = now
		_, err = tx.ExecContext(ctx, `
			UPDATE control_leases
			SET participant_id = ?, epoch = ?, expires_at = ?, updated_at = ?
			WHERE share_id = ?`, participantID, lease.Epoch, millis(lease.ExpiresAt),
			millis(now), shareID)
		if err != nil {
			return domain.ControlLease{}, fmt.Errorf("take control lease: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.ControlLease{}, fmt.Errorf("commit control lease: %w", err)
	}
	return lease, nil
}

func (s *Store) RenewControl(ctx context.Context, shareID, participantID string, epoch int64, now time.Time, ttl time.Duration) (domain.ControlLease, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if ttl <= 0 {
		ttl = DefaultControlLeaseTTL
	}
	expires := now.Add(ttl)
	result, err := s.db.ExecContext(ctx, `
		UPDATE control_leases SET expires_at = ?, updated_at = ?
		WHERE share_id = ? AND participant_id = ? AND epoch = ? AND expires_at > ?`,
		millis(expires), millis(now), shareID, participantID, epoch, millis(now))
	if err != nil {
		return domain.ControlLease{}, fmt.Errorf("renew control lease: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return domain.ControlLease{}, err
	}
	if changed == 0 {
		return domain.ControlLease{}, s.leaseFailure(ctx, shareID, participantID, epoch, now)
	}
	return domain.ControlLease{ShareID: shareID, ParticipantID: participantID, Epoch: epoch, ExpiresAt: expires, UpdatedAt: now}, nil
}

func (s *Store) ReleaseControl(ctx context.Context, shareID, participantID string, epoch int64, now time.Time) (domain.ControlLease, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE control_leases
		SET participant_id = NULL, epoch = epoch + 1, expires_at = 0, updated_at = ?
		WHERE share_id = ? AND participant_id = ? AND epoch = ?`,
		millis(now), shareID, participantID, epoch)
	if err != nil {
		return domain.ControlLease{}, fmt.Errorf("release control lease: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return domain.ControlLease{}, err
	}
	if changed == 0 {
		return domain.ControlLease{}, s.leaseFailure(ctx, shareID, participantID, epoch, now)
	}
	return s.GetControlLease(ctx, shareID)
}

// PreemptControl is host-owner only at the API layer. Passing an empty
// participantID revokes remote control while preserving a monotonic epoch.
func (s *Store) PreemptControl(ctx context.Context, shareID, participantID string, now time.Time, ttl time.Duration) (domain.ControlLease, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if ttl <= 0 {
		ttl = DefaultControlLeaseTTL
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ControlLease{}, err
	}
	defer tx.Rollback()
	lease, err := readLease(ctx, tx, shareID)
	if errors.Is(err, domain.ErrNotFound) {
		lease = domain.ControlLease{ShareID: shareID, Epoch: 1}
	} else if err != nil {
		return domain.ControlLease{}, err
	} else {
		lease.Epoch++
	}
	lease.ParticipantID = participantID
	lease.UpdatedAt = now
	if participantID == "" {
		lease.ExpiresAt = time.Time{}
	} else {
		lease.ExpiresAt = now.Add(ttl)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO control_leases(share_id, participant_id, epoch, expires_at, updated_at)
		VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(share_id) DO UPDATE SET
		  participant_id = excluded.participant_id,
		  epoch = excluded.epoch,
		  expires_at = excluded.expires_at,
		  updated_at = excluded.updated_at`,
		shareID, nullString(participantID), lease.Epoch, millis(lease.ExpiresAt), millis(now))
	if err != nil {
		return domain.ControlLease{}, fmt.Errorf("preempt control: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.ControlLease{}, fmt.Errorf("commit control preemption: %w", err)
	}
	return lease, nil
}

func (s *Store) GetControlLease(ctx context.Context, shareID string) (domain.ControlLease, error) {
	return readLease(ctx, s.db, shareID)
}

func (s *Store) ValidateControl(ctx context.Context, shareID, participantID string, epoch int64, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	lease, err := s.GetControlLease(ctx, shareID)
	if err != nil {
		return err
	}
	if lease.Epoch != epoch || lease.ParticipantID != participantID {
		return domain.ErrLeaseFence
	}
	if !lease.ExpiresAt.After(now) {
		return domain.ErrLeaseExpired
	}
	return nil
}

// ValidateCommandFence checks both optimistic Thread revision and the current
// controller fencing token. Call it immediately before dispatching a remote
// Agent command; the eventual event append should still use
// AppendEventExpected to atomically consume expectedRevision.
func (s *Store) ValidateCommandFence(ctx context.Context, shareID, participantID string, expectedRevision, epoch int64, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var revision int64
	var currentEpoch, expires sql.NullInt64
	var holder sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT threads.revision, control_leases.participant_id,
		       control_leases.epoch, control_leases.expires_at
		FROM shares
		JOIN threads ON threads.id = shares.thread_id
		LEFT JOIN control_leases ON control_leases.share_id = shares.id
		WHERE shares.id = ? AND shares.revoked_at IS NULL AND shares.expires_at > ?`, shareID, millis(now)).Scan(&revision, &holder, &currentEpoch, &expires)
	if err != nil {
		return mapNotFound(err)
	}
	if revision != expectedRevision {
		return domain.ErrRevisionConflict
	}
	if !holder.Valid || holder.String != participantID || !currentEpoch.Valid || currentEpoch.Int64 != epoch {
		return domain.ErrLeaseFence
	}
	if !expires.Valid || expires.Int64 <= millis(now) {
		return domain.ErrLeaseExpired
	}
	return nil
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readLease(ctx context.Context, q queryRower, shareID string) (domain.ControlLease, error) {
	var lease domain.ControlLease
	var participant sql.NullString
	var expires, updated int64
	err := q.QueryRowContext(ctx, `
		SELECT share_id, participant_id, epoch, expires_at, updated_at
		FROM control_leases WHERE share_id = ?`, shareID).Scan(
		&lease.ShareID, &participant, &lease.Epoch, &expires, &updated)
	if err != nil {
		return domain.ControlLease{}, mapNotFound(err)
	}
	lease.ParticipantID = participant.String
	lease.ExpiresAt = fromMillis(expires)
	lease.UpdatedAt = fromMillis(updated)
	return lease, nil
}

func (s *Store) leaseFailure(ctx context.Context, shareID, participantID string, epoch int64, now time.Time) error {
	lease, err := s.GetControlLease(ctx, shareID)
	if err != nil {
		return err
	}
	if lease.Epoch != epoch || lease.ParticipantID != participantID {
		return domain.ErrLeaseFence
	}
	if !lease.ExpiresAt.After(now) {
		return domain.ErrLeaseExpired
	}
	return domain.ErrLeaseFence
}

// ClaimCommand implements durable command idempotency. created is false when
// the exact command ID has already been claimed, in which case the persisted
// status/result is returned.
func (s *Store) ClaimCommand(ctx context.Context, command domain.Command) (persisted domain.Command, created bool, err error) {
	if command.ID == "" || command.ShareID == "" {
		return domain.Command{}, false, fmt.Errorf("share id and command id are required")
	}
	if command.Status == "" {
		command.Status = "running"
	}
	if command.CreatedAt.IsZero() {
		command.CreatedAt = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO commands(
		  share_id, command_id, kind, expected_revision, lease_epoch,
		  request_object, status, result_object, error_text, created_at, completed_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(share_id, command_id) DO NOTHING`, command.ShareID, command.ID,
		command.Kind, command.ExpectedRevision, command.LeaseEpoch,
		nullString(command.RequestObject), command.Status, nullString(command.ResultObject),
		command.ErrorText, millis(command.CreatedAt), nullMillis(command.CompletedAt))
	if err != nil {
		return domain.Command{}, false, fmt.Errorf("claim command: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return domain.Command{}, false, err
	}
	persisted, err = s.GetCommand(ctx, command.ShareID, command.ID)
	if err != nil {
		return domain.Command{}, false, err
	}
	created = changed == 1
	if !created && (persisted.Kind != command.Kind ||
		persisted.ExpectedRevision != command.ExpectedRevision ||
		persisted.LeaseEpoch != command.LeaseEpoch ||
		persisted.RequestObject != command.RequestObject) {
		return domain.Command{}, false, domain.ErrCommandConflict
	}
	return persisted, created, nil
}

func (s *Store) CompleteCommand(ctx context.Context, shareID, commandID, status, resultObject, errorText string, completedAt time.Time) (domain.Command, error) {
	if completedAt.IsZero() {
		completedAt = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE commands
		SET status = ?, result_object = ?, error_text = ?, completed_at = ?
		WHERE share_id = ? AND command_id = ? AND status = 'running'`,
		status, nullString(resultObject), errorText, millis(completedAt), shareID, commandID)
	if err != nil {
		return domain.Command{}, fmt.Errorf("complete command: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return domain.Command{}, err
	}
	if changed == 0 {
		command, getErr := s.GetCommand(ctx, shareID, commandID)
		if getErr != nil {
			return domain.Command{}, getErr
		}
		// A duplicate completion returns the already durable result.
		return command, nil
	}
	return s.GetCommand(ctx, shareID, commandID)
}

func (s *Store) GetCommand(ctx context.Context, shareID, commandID string) (domain.Command, error) {
	var command domain.Command
	var request, result sql.NullString
	var completed sql.NullInt64
	var created int64
	err := s.db.QueryRowContext(ctx, `
		SELECT share_id, command_id, kind, expected_revision, lease_epoch,
		       request_object, status, result_object, error_text, created_at, completed_at
		FROM commands WHERE share_id = ? AND command_id = ?`, shareID, commandID).Scan(
		&command.ShareID, &command.ID, &command.Kind, &command.ExpectedRevision,
		&command.LeaseEpoch, &request, &command.Status, &result, &command.ErrorText,
		&created, &completed)
	if err != nil {
		return domain.Command{}, mapNotFound(err)
	}
	command.RequestObject = request.String
	command.ResultObject = result.String
	command.CreatedAt = fromMillis(created)
	if completed.Valid {
		command.CompletedAt = fromMillis(completed.Int64)
	}
	return command, nil
}
