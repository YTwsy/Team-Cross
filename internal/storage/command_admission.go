package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"teamcross/internal/domain"
)

// AdmitRemoteCommand is the execution-admission cutoff, not the earlier HTTP
// authorization check. The INSERT's implicit SQLite transaction checks the
// durable Share/revision/lease and claims commandId together. A revoke committed
// first prevents admission; an admitted operation may finish after revocation.
// Exact duplicates keep their durable status and never gain a second dispatch.
// Invalid revision/lease under an otherwise authorized Share is stored only as
// a terminal error, preserving failed-command replay without admitting it.
func (s *Store) AdmitRemoteCommand(ctx context.Context, command domain.Command, participantID string, now time.Time) (persisted domain.Command, created bool, err error) {
	capability, needsLease, ok := remoteCommandPermission(command.Kind)
	if !ok || command.ID == "" || command.ShareID == "" || participantID == "" || command.ExpectedRevision < 0 {
		return domain.Command{}, false, errors.New("invalid remote command admission")
	}
	wallClock := now.IsZero()
	if wallClock {
		now = time.Now().UTC()
	}
	if command.CreatedAt.IsZero() {
		command.CreatedAt = now
	}
	// Callers cannot manufacture a completed command as a new admission.
	command.Status = "running"
	result, err := s.db.ExecContext(ctx, `
		WITH admission_clock(ms) AS (
		  SELECT CASE WHEN ? THEN CAST(unixepoch('subsec') * 1000 AS INTEGER) ELSE ? END
		), candidate AS (
		  SELECT admission_clock.ms, CASE
		    WHEN threads.revision <> ? THEN 'thread revision conflict'
		    WHEN ? AND NOT EXISTS (
		      SELECT 1 FROM control_leases WHERE share_id=shares.id AND participant_id=? AND epoch=?
		    ) THEN 'stale control lease epoch'
		    WHEN ? AND NOT EXISTS (
		      SELECT 1 FROM control_leases WHERE share_id=shares.id AND expires_at > admission_clock.ms
		    ) THEN 'control lease has expired'
		    ELSE '' END AS fence_error
		  FROM shares JOIN threads ON threads.id = shares.thread_id CROSS JOIN admission_clock
		  WHERE shares.id = ? AND shares.revoked_at IS NULL AND shares.expires_at > admission_clock.ms
		    AND EXISTS (SELECT 1 FROM json_each(shares.capabilities) WHERE value = ? AND type = 'text')
		    AND EXISTS (SELECT 1 FROM participants WHERE participants.id = ? AND participants.share_id = shares.id)
		)
		INSERT INTO commands(
		  share_id, command_id, kind, expected_revision, lease_epoch,
		  request_object, status, result_object, error_text, created_at, completed_at
		)
		SELECT ?, ?, ?, ?, ?, ?, CASE WHEN fence_error='' THEN 'running' ELSE 'error' END,
		  NULL, fence_error, ?, CASE WHEN fence_error='' THEN NULL ELSE ms END
		FROM candidate WHERE true
		ON CONFLICT(share_id, command_id) DO NOTHING`,
		wallClock, millis(now), command.ExpectedRevision, needsLease, participantID, command.LeaseEpoch, needsLease,
		command.ShareID, capability, participantID,
		command.ShareID, command.ID, command.Kind, command.ExpectedRevision, command.LeaseEpoch,
		nullString(command.RequestObject), millis(command.CreatedAt))
	if err != nil {
		return domain.Command{}, false, fmt.Errorf("admit remote command: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return domain.Command{}, false, err
	}
	persisted, err = s.GetCommand(ctx, command.ShareID, command.ID)
	if errors.Is(err, domain.ErrNotFound) {
		// This read only classifies a denied admission. It can never grant one.
		if wallClock {
			now = time.Now().UTC()
		}
		return domain.Command{}, false, s.remoteAdmissionFailure(ctx, command, participantID, capability, needsLease, now)
	}
	if err != nil {
		return domain.Command{}, false, err
	}
	if persisted.Kind != command.Kind || persisted.ExpectedRevision != command.ExpectedRevision || persisted.LeaseEpoch != command.LeaseEpoch || persisted.RequestObject != command.RequestObject {
		return domain.Command{}, false, domain.ErrCommandConflict
	}
	if changed == 1 && persisted.Status == "error" {
		// Preserve durable failed-command replay without granting execution.
		// First response carries the original fence error; exact retries read the
		// terminal record and cannot turn into an admitted running command.
		switch persisted.ErrorText {
		case domain.ErrRevisionConflict.Error():
			return persisted, false, domain.ErrRevisionConflict
		case domain.ErrLeaseExpired.Error():
			return persisted, false, domain.ErrLeaseExpired
		default:
			return persisted, false, domain.ErrLeaseFence
		}
	}
	return persisted, changed == 1, nil
}

func remoteCommandPermission(kind string) (capability string, needsLease bool, valid bool) {
	switch kind {
	case "annotate":
		return "annotate", false, true
	case "control.request":
		return "send", false, true
	case "control.renew", "control.release", "agent.send", "agent.input":
		return "send", true, true
	case "agent.steer":
		return "steer", true, true
	case "agent.interrupt":
		return "interrupt", true, true
	default:
		return "", false, false
	}
}

func (s *Store) remoteAdmissionFailure(ctx context.Context, command domain.Command, participantID, capability string, needsLease bool, now time.Time) error {
	share, err := s.GetShare(ctx, command.ShareID)
	if err != nil {
		return err
	}
	var capabilities []string
	if !share.RevokedAt.IsZero() || !share.ExpiresAt.After(now) || json.Unmarshal(share.Capabilities, &capabilities) != nil || !slices.Contains(capabilities, capability) {
		return domain.ErrNotFound
	}
	var participant bool
	if err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM participants WHERE id=? AND share_id=?)`, participantID, share.ID).Scan(&participant); err != nil {
		return err
	}
	if !participant {
		return domain.ErrNotFound
	}
	thread, err := s.GetThread(ctx, share.ThreadID)
	if err != nil {
		return err
	}
	if thread.Revision != command.ExpectedRevision {
		return domain.ErrRevisionConflict
	}
	if needsLease {
		if err := s.ValidateCommandFence(ctx, share.ID, participantID, command.ExpectedRevision, command.LeaseEpoch, now); err != nil {
			return err
		}
	}
	// Concurrent changes can make the diagnostic read valid after rejection.
	// The caller must retry admission with a new check, never dispatch here.
	return domain.ErrRevisionConflict
}
