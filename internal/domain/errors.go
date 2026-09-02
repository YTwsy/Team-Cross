package domain

import "errors"

var (
	ErrNotFound         = errors.New("not found")
	ErrRevisionConflict = errors.New("thread revision conflict")
	ErrRoundExists      = errors.New("round already exists")
	ErrRoundSequence    = errors.New("round number is not the next immutable round")
	ErrLeaseHeld        = errors.New("control lease is held by another participant")
	ErrLeaseExpired     = errors.New("control lease has expired")
	ErrLeaseFence       = errors.New("stale control lease epoch")
	ErrCommandConflict  = errors.New("command id was reused with different parameters")
	ErrUnbornRepository = errors.New("repository has no commits")
	ErrNotUntracked     = errors.New("selected path is not an untracked file")
	ErrUnsafePath       = errors.New("path escapes repository root")
)
