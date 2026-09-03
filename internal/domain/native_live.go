package domain

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

var (
	ErrLiveShareStale = errors.New("native live Share preview is stale")
	ErrLiveShareFence = errors.New("native live Share or Follow is no longer authorized")
)

// NativeLiveScope is an explicit grant for the current window AND later windows
// of one Follow. It is independent from managed-Agent IncludeEvents.
type NativeLiveScope struct {
	FollowID                string   `json:"followId"`
	ExpectedSnapshotID      string   `json:"expectedSnapshotId"`
	EntryKinds              []string `json:"entryKinds"`
	ConfirmCurrentAndFuture bool     `json:"confirmCurrentAndFuture"`
}

// NativeLiveBinding is prepared by the Owner API and fenced again by storage.
// Source contains only the native identity, never local reader configuration.
type NativeLiveBinding struct {
	FollowID           string     `json:"followId"`
	FollowEpoch        int64      `json:"followEpoch"`
	Source             SessionRef `json:"source"`
	EntryKinds         []string   `json:"entryKinds"`
	ExpectedSnapshotID string     `json:"expectedSnapshotId"`
}

type NativeLiveState struct {
	Binding          NativeLiveBinding `json:"binding"`
	State            string            `json:"state"`
	Reason           string            `json:"reason,omitempty"`
	LatestSnapshotID string            `json:"latestSnapshotId"`
	Count            int64             `json:"count"`
	Bytes            int64             `json:"bytes"`
	StartSeq         int64             `json:"startSeq"`
}

// NormalizeNativeLiveKinds rejects ambiguous grants and stores a stable order.
// Unknown source entry kinds are handled differently: projection drops them.
func NormalizeNativeLiveKinds(kinds []string) ([]string, error) {
	selected := map[string]bool{}
	for _, kind := range kinds {
		if (kind != "message" && kind != "tool" && kind != "notice") || selected[kind] {
			return nil, fmt.Errorf("native live Share requires distinct known entry kinds")
		}
		selected[kind] = true
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("native live Share requires at least one entry kind")
	}
	result := []string{}
	for _, kind := range []string{"message", "tool", "notice"} {
		if selected[kind] {
			result = append(result, kind)
		}
	}
	return result, nil
}

func NativeLiveSource(source SessionRef) SessionRef {
	return SessionRef{Provider: source.Provider, SessionID: source.SessionID, IdentityKind: source.IdentityKind, Surface: source.Surface, ProviderVersion: source.ProviderVersion}
}

func SameNativeLiveSource(a, b SessionRef) bool {
	return a.Provider == b.Provider && a.SessionID == b.SessionID && a.IdentityKind == b.IdentityKind && a.Surface == b.Surface && a.ProviderVersion == b.ProviderVersion
}

// ProjectNativeSnapshot is pure. Filtering is disclosure authorization, not
// redaction: text in an allowed entry kind is shared verbatim and remains
// untrusted. Snapshot IDs and entry IDs retain immutable annotation anchors.
func ProjectNativeSnapshot(raw SessionSnapshot, kinds []string) (SessionSnapshot, bool, error) {
	kinds, err := NormalizeNativeLiveKinds(kinds)
	if err != nil {
		return SessionSnapshot{}, false, err
	}
	if raw.ID == "" || raw.ThreadID == "" || raw.CapturedAt.IsZero() || len(raw.Entries) > 5000 {
		return SessionSnapshot{}, false, fmt.Errorf("invalid native live snapshot shape")
	}
	source := NativeLiveSource(raw.Source)
	if (source.Provider != "codex" && source.Provider != "claude") || source.SessionID == "" || source.IdentityKind == "" || source.Surface == "" {
		return SessionSnapshot{}, false, fmt.Errorf("invalid native live snapshot source")
	}
	for _, value := range []string{raw.ID, raw.ThreadID, source.Provider, source.SessionID, source.IdentityKind, source.Surface, source.ProviderVersion} {
		if len(value) > 4096 || !utf8.ValidString(value) {
			return SessionSnapshot{}, false, fmt.Errorf("invalid native live identity")
		}
	}
	selected := map[string]bool{}
	for _, kind := range kinds {
		selected[kind] = true
	}
	result := SessionSnapshot{ID: raw.ID, ThreadID: raw.ThreadID, Source: source, CapturedAt: raw.CapturedAt, Entries: []SessionEntry{}, Truncated: raw.Truncated, Warnings: []string{}, Capabilities: SessionCapabilities{Read: true, Reason: "共享的只读快照不授予原生 Session 控制权。"}}
	seen := map[string]bool{}
	for _, entry := range raw.Entries {
		if entry.ID == "" || len(entry.ID) > 4096 || seen[entry.ID] || !utf8.ValidString(entry.ID) || !utf8.ValidString(entry.Text) {
			return SessionSnapshot{}, false, fmt.Errorf("invalid native live entry shape")
		}
		seen[entry.ID] = true
		if selected[entry.Kind] {
			result.Entries = append(result.Entries, entry)
		}
	}
	if raw.Truncated {
		result.Warnings = []string{"来源历史不完整；此视图仅包含获准分享的记录。"}
	}
	return result, len(result.Entries) == len(raw.Entries), nil
}
