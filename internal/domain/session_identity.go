package domain

import "errors"

// ErrSessionIdentityConflict means an already established Provider identity
// cannot be replaced by another identity within the same managed Run.
var ErrSessionIdentityConflict = errors.New("managed Run Session identity conflicts with its established identity")

// CanonicalSessionID preserves opaque Provider identifiers. Only the precise
// legacy Claude placeholder for this Run is recognized as an unknown identity.
func CanonicalSessionID(provider, runID, sessionID string) string {
	if provider == "claude" && sessionID == "claude-pending-"+runID {
		return ""
	}
	return sessionID
}

// AdoptSessionID is monotonic: unknown may become known, but delayed empty or
// placeholder descriptors cannot demote it and another known ID cannot rebind it.
func AdoptSessionID(provider, runID, current, candidate string) (string, error) {
	current = CanonicalSessionID(provider, runID, current)
	candidate = CanonicalSessionID(provider, runID, candidate)
	if current == "" {
		return candidate, nil
	}
	if candidate == "" || candidate == current {
		return current, nil
	}
	return current, ErrSessionIdentityConflict
}
