// Package spacecontext defines stable public source identities shared by the
// collaboration host and a member's local shadow. It contains no native routes.
package spacecontext

import "time"

type Subject struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	ExecutionID string `json:"executionId,omitempty"`
}

type SourceRef struct {
	SpaceID         string   `json:"spaceId"`
	ExecutionID     string   `json:"executionId,omitempty"`
	MaterialID      string   `json:"materialId,omitempty"`
	Version         int      `json:"version,omitempty"`
	AnnotationID    string   `json:"annotationId,omitempty"`
	ReplyID         string   `json:"replyId,omitempty"`
	TurnID          string   `json:"turnId,omitempty"`
	ItemIDs         []string `json:"itemIds,omitempty"`
	RequestID       string   `json:"requestId,omitempty"`
	BriefRevision   int      `json:"briefRevision,omitempty"`
	BriefItemID     string   `json:"briefItemId,omitempty"`
	ShadowID        string   `json:"shadowId,omitempty"`
	ShadowMessageID string   `json:"shadowMessageId,omitempty"`
	ShadowResultID  string   `json:"shadowResultId,omitempty"`
}

type Actor struct {
	MemberID string `json:"memberId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Provider string `json:"provider,omitempty"`
	Session  string `json:"session,omitempty"` // Opaque shared identity.
	ShadowID string `json:"shadowId,omitempty"`
}

// Signal carries only metadata and a preview. Observation must read the exact
// source before claiming understanding; the preview is not the source body.
type Signal struct {
	ID        string        `json:"id"`
	Seq       uint64        `json:"seq"`
	Kind      string        `json:"kind"`
	Source    SourceRef     `json:"source"`
	Subject   Subject       `json:"subject"`
	Actor     Actor         `json:"actor"`
	CreatedAt time.Time     `json:"createdAt"`
	Preview   string        `json:"preview"`
	Reasons   []string      `json:"reasons"`
	Follow    *FollowChange `json:"follow,omitempty"`
}

// FollowChange is a private control signal from the authoritative member
// preference. Baseline references are fixed when that preference is saved;
// their bodies are read later. It is never a human notification.
type FollowChange struct {
	Mode     string         `json:"mode"`
	Revision uint64         `json:"revision"`
	Parent   *FollowSetting `json:"parent,omitempty"`
	Baseline []SourceRef    `json:"baseline,omitempty"`
	Partial  bool           `json:"partial,omitempty"`
}

type FollowSetting struct {
	Subject  Subject `json:"subject"`
	Mode     string  `json:"mode"`
	Revision uint64  `json:"revision"`
}
