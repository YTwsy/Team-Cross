package collab

import "time"

// Published content contains visible messages and saved tool results only. It
// never grants native resume, filesystem access, or access to the source reader.
type MaterialItem struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Text   string `json:"text"`
	Notice string `json:"notice,omitempty"`
}
type MaterialTurn struct {
	ID     string         `json:"id"`
	Status string         `json:"status"`
	Items  []MaterialItem `json:"items"`
}
type MaterialContent struct {
	Title          string         `json:"title"`
	Provider       string         `json:"provider"`
	SourceID       string         `json:"sourceId"`
	StartTurnID    string         `json:"startTurnId"`
	EndTurnID      string         `json:"endTurnId"`
	ReadingStartID string         `json:"readingStartId,omitempty"`
	Turns          []MaterialTurn `json:"turns"`
}
type MaterialVersion struct {
	MaterialContent
	Version     int       `json:"version"`
	Hash        string    `json:"hash"`
	RequestID   string    `json:"requestId"`
	RequestHash string    `json:"requestHash"`
	CreatedAt   time.Time `json:"createdAt"`
}
type Material struct {
	ID          string            `json:"id"`
	AuthorID    string            `json:"authorId"`
	Author      string            `json:"author"`
	WithdrawnAt *time.Time        `json:"withdrawnAt,omitempty"`
	Versions    []MaterialVersion `json:"versions"`
}
type MaterialReference struct {
	MaterialID string `json:"materialId"`
	Version    int    `json:"version"`
	TurnID     string `json:"turnId,omitempty"`
}
type PublicationDraft struct {
	ID string `json:"id"`
	MaterialContent
	FrozenAt time.Time `json:"frozenAt"`
	Hash     string    `json:"hash"`
}
type PublicationSelection struct {
	DraftID        string `json:"draftId"`
	Title          string `json:"title"`
	StartTurnID    string `json:"startTurnId"`
	EndTurnID      string `json:"endTurnId"`
	ReadingStartID string `json:"readingStartId,omitempty"`
}
type PublishInput struct {
	PreviewID   string `json:"previewId"`
	PreviewHash string `json:"previewHash"`
	RequestID   string `json:"requestId"`
	MaterialID  string `json:"materialId,omitempty"`
	BaseVersion int    `json:"baseVersion,omitempty"`
}
type publicationUpload struct {
	Content     MaterialContent `json:"content"`
	RequestID   string          `json:"requestId"`
	MaterialID  string          `json:"materialId,omitempty"`
	BaseVersion int             `json:"baseVersion,omitempty"`
}
type MaterialRead struct {
	MaterialID string `json:"materialId"`
	Version    int    `json:"version"`
	Cursor     string `json:"cursor,omitempty"`
	TurnID     string `json:"turnId,omitempty"`
}
