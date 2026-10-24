package shadow

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"teamcross/internal/spacecontext"
)

// A local projection of real outstanding dependencies, never a model summary
// or an inference from somebody having read a notification.
type WaitingTask struct {
	ShadowID     string `json:"shadowId"`
	JobID        string `json:"jobId"`
	Title        string `json:"title"`
	State        string `json:"state"`
	DependencyID string `json:"dependencyId"`
	Condition    string `json:"condition"`
}

type WaitingItem struct {
	ID         string                 `json:"id"`
	Kind       string                 `json:"kind"`
	Direction  string                 `json:"direction"` // needs_owner, waiting_others, processing
	State      string                 `json:"state"`     // waiting, answer_received, delivery_unknown, held_for_owner, unavailable
	Source     spacecontext.SourceRef `json:"source"`
	Question   string                 `json:"question"`
	Recipients []string               `json:"recipients,omitempty"`
	Tasks      []WaitingTask          `json:"tasks"`
	TotalTasks int                    `json:"totalTasks"`
	Handling   *QuestionHandling      `json:"handling,omitempty"`
	DueAt      *time.Time             `json:"dueAt,omitempty"`
	Error      string                 `json:"error,omitempty"`
	UpdatedAt  time.Time              `json:"updatedAt"`
}

type WaitingPage struct {
	Items      []WaitingItem `json:"items"`
	Total      int           `json:"total"`
	Revision   string        `json:"revision"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

func waitingIdentity(kind string, source spacecontext.SourceRef, fallback string) string {
	if kind == "discussion" && source.AnnotationID != "" {
		return questionSourceID(source)
	}
	if kind == "request" && source.RequestID != "" {
		return attentionHash([]any{source.SpaceID, source.RequestID})
	}
	return attentionHash([]any{kind, fallback})
}

func (e *Engine) WaitingItems() []WaitingItem {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []WaitingItem{}
	for _, job := range e.record.Jobs {
		if !taskAcceptsAnswers(job) {
			continue
		}
		for _, d := range job.Dependencies {
			if d.State == "satisfied" {
				continue
			}
			item := WaitingItem{Kind: d.Kind, Direction: "waiting_others", State: "waiting", Source: clone(d.Source), Question: clip(d.Condition, 1000), DueAt: d.DueAt, Error: clip(d.Error, 500), UpdatedAt: job.UpdatedAt}
			fallback := e.record.ID + ":" + job.ID + ":" + d.ID
			if q, exists := e.record.Questions[d.QuestionID]; exists {
				item.Question, item.Recipients = clip(q.Text, 1000), clone(q.Recipients)
				if q.UpdatedAt.After(item.UpdatedAt) {
					item.UpdatedAt = q.UpdatedAt
				}
				if q.Delivery != "saved" || d.QuestionSourcePending {
					// An unsent reply's parent is not the question we are waiting for.
					item.Source = spacecontext.SourceRef{SpaceID: e.record.SpaceID}
					item.State, item.Direction, item.Error = "delivery_unknown", "processing", clip(q.Error, 500)
					if e.record.Actions[q.ActionID].State == "held_for_owner" {
						item.State = "held_for_owner"
					}
					fallback = e.record.ID + ":" + q.ID
				} else {
					item.Source = clone(q.Source)
					for _, member := range q.Recipients {
						if member == e.record.Owner.ID {
							item.Direction = "needs_owner"
						}
					}
				}
			}
			if len(d.Signals) > 0 {
				item.State, item.Direction = "answer_received", "processing"
				for _, signal := range d.Signals {
					if signal.State == "unavailable" {
						item.State = "unavailable"
					}
				}
			}
			if d.Kind == "time" || d.Kind == "job" {
				item.Direction = "processing"
			}
			item.ID = waitingIdentity(d.Kind, item.Source, fallback)
			item.Tasks = []WaitingTask{{ShadowID: e.record.ID, JobID: job.ID, Title: clip(job.Title, 240), State: job.State, DependencyID: d.ID, Condition: clip(d.Condition, 500)}}
			item.TotalTasks = 1
			out = append(out, item)
		}
	}
	return out
}

// Inputs come only from engines owned by this member. Exact source identity
// merges a question used by several tasks; similar wording never does.
func (s *AttentionStore) WaitingPage(items []WaitingItem, direction, cursor string, limit int) (WaitingPage, error) {
	if direction != "" && direction != "needs_owner" && direction != "waiting_others" && direction != "processing" {
		return WaitingPage{}, fmt.Errorf("等待分类无效")
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > 50 {
		limit = 50
	}
	state := s.Snapshot()
	items = clone(items)
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return jsonText(a.Tasks) < jsonText(b.Tasks)
	})
	byID := map[string]WaitingItem{}
	for _, item := range items {
		if item.Source.SpaceID != "" && item.Source.SpaceID != state.SpaceID {
			continue
		}
		if old, exists := byID[item.ID]; exists {
			old.Tasks = append(old.Tasks, item.Tasks...)
			old.TotalTasks += item.TotalTasks
			if item.UpdatedAt.After(old.UpdatedAt) {
				old.UpdatedAt = item.UpdatedAt
			}
			// If one task still needs an answer, a different task having received
			// a partial answer does not remove that outstanding condition.
			if item.State == "waiting" {
				old.State = "waiting"
			}
			if item.Direction == "needs_owner" || old.Direction == "processing" {
				old.Direction = item.Direction
			}
			if old.Error == "" {
				old.Error = item.Error
			}
			old.Recipients = appendUniqueLinks(old.Recipients, item.Recipients)
			sort.Strings(old.Recipients)
			byID[item.ID] = old
		} else {
			byID[item.ID] = clone(item)
		}
	}
	for _, h := range state.Handling {
		if h.Handler != "owner" {
			continue
		}
		item, exists := byID[h.ID]
		if h.State == "answered" && !exists {
			continue
		}
		if !exists {
			item = WaitingItem{ID: h.ID, Kind: "discussion", Source: clone(h.Source), Question: h.Question, Tasks: []WaitingTask{}, UpdatedAt: h.UpdatedAt}
		}
		item.Handling, item.Direction = ptrHandling(h), "needs_owner"
		item.State, item.Error = h.State, h.Error
		if h.State == "answered" {
			item.Direction, item.State = "processing", "answer_received"
		}
		if h.UpdatedAt.After(item.UpdatedAt) {
			item.UpdatedAt = h.UpdatedAt
		}
		byID[item.ID] = item
	}
	all := []WaitingItem{}
	for _, item := range byID {
		if direction != "" && item.Direction != direction {
			continue
		}
		sort.Slice(item.Tasks, func(i, j int) bool {
			a, b := item.Tasks[i], item.Tasks[j]
			return a.ShadowID+":"+a.JobID+":"+a.DependencyID < b.ShadowID+":"+b.JobID+":"+b.DependencyID
		})
		if len(item.Tasks) > 16 {
			item.Tasks = item.Tasks[:16]
		}
		all = append(all, item)
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].UpdatedAt.Equal(all[j].UpdatedAt) {
			return all[i].UpdatedAt.After(all[j].UpdatedAt)
		}
		return all[i].ID < all[j].ID
	})
	revision := attentionHash(all)
	offset := 0
	if cursor != "" {
		var c struct {
			Revision string `json:"revision"`
			Offset   int    `json:"offset"`
		}
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if len(cursor) > 500 || err != nil || json.Unmarshal(raw, &c) != nil || c.Revision != revision || c.Offset < 0 || c.Offset > len(all) {
			return WaitingPage{}, fmt.Errorf("等待列表已变化，请从第一页重新读取")
		}
		offset = c.Offset
	}
	end := min(offset+limit, len(all))
	page := WaitingPage{Items: all[offset:end], Total: len(all), Revision: revision}
	if end < len(all) {
		b, _ := json.Marshal(map[string]any{"revision": revision, "offset": end})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(b)
	}
	return page, nil
}

func ptrHandling(h QuestionHandling) *QuestionHandling {
	h = clone(h)
	h.Cursor = ""
	h.Answer = nil
	return &h
}
