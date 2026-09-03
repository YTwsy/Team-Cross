package domain

import (
	"errors"
	"testing"
)

func TestSessionIdentityAdoptionIsMonotonic(t *testing.T) {
	for _, test := range []struct {
		name, provider, current, candidate, want string
		conflict                                 bool
	}{
		{"delayed", "claude", "", "native", "native", false},
		{"legacy", "claude", "claude-pending-run", "native", "native", false},
		{"still_unknown", "claude", "", "claude-pending-run", "", false},
		{"duplicate", "claude", "native", "native", "native", false},
		{"stale_empty", "claude", "native", "", "native", false},
		{"stale_placeholder", "claude", "native", "claude-pending-run", "native", false},
		{"conflict", "claude", "native", "different", "native", true},
		{"opaque_other_run", "claude", "", "claude-pending-other", "claude-pending-other", false},
		{"opaque_other_provider", "codex", "", "claude-pending-run", "claude-pending-run", false},
		{"mock_native_id", "mock", "", "mock-run", "mock-run", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := AdoptSessionID(test.provider, "run", test.current, test.candidate)
			if got != test.want || errors.Is(err, ErrSessionIdentityConflict) != test.conflict {
				t.Fatalf("got %q, %v; want %q, conflict=%v", got, err, test.want, test.conflict)
			}
		})
	}
}
