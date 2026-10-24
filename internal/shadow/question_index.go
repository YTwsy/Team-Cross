package shadow

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"teamcross/internal/spacecontext"
)

// Private member projection. Raw native sessions, task prompts, delivery
// bookkeeping and action arguments are intentionally absent.
type QuestionSummary struct {
	ID               string                  `json:"id"`
	Text             string                  `json:"text"`
	Delivery         string                  `json:"delivery"`
	State            string                  `json:"state"`
	Source           *spacecontext.SourceRef `json:"source,omitempty"`
	TotalTasks       int                     `json:"totalTasks"`
	ActiveTasks      int                     `json:"activeTasks"`
	RelatedQuestions int                     `json:"relatedQuestions"`
	UpdatedAt        time.Time               `json:"updatedAt"`
}

type QuestionPage struct {
	Items      []QuestionSummary `json:"items"`
	Total      int               `json:"total"`
	Revision   string            `json:"revision"`
	NextCursor string            `json:"nextCursor,omitempty"`
}

type QuestionDetail struct {
	QuestionSummary
	Condition       string             `json:"condition"`
	RespondsTo      []QuestionRelation `json:"respondsTo"`
	Tasks           []QuestionTaskLink `json:"tasks"`
	TotalConditions int                `json:"totalConditions"`
	NextCursor      string             `json:"nextCursor,omitempty"`
	Revision        string             `json:"revision"`
	Error           string             `json:"error,omitempty"`
}

func (e *Engine) questionSummaryLocked(q TaskQuestion, links []QuestionTaskLink) QuestionSummary {
	item := QuestionSummary{ID: q.ID, Text: clip(q.Text, 300), Delivery: q.Delivery, State: "conditions_met", TotalTasks: questionTaskCount(links, false), ActiveTasks: questionTaskCount(links, true), RelatedQuestions: len(q.RespondsTo), UpdatedAt: q.UpdatedAt}
	if q.Delivery == "saved" {
		ref := clone(q.Source)
		item.Source = &ref
	}
	if item.ActiveTasks == 0 {
		item.State = "closed"
		return item
	}
	for _, link := range links {
		if link.Active && link.DependencyState != "satisfied" {
			if link.Unavailable {
				item.State = "unavailable"
				break
			}
			if link.AnswerReceived && item.State != "waiting" {
				item.State = "answer_received"
			} else if !link.AnswerReceived {
				item.State = "waiting"
			}
		}
	}
	if q.Delivery != "saved" {
		item.State = "delivery_unknown"
		if e.record.Actions[q.ActionID].State == "held_for_owner" {
			item.State = "held_for_owner"
		}
	}
	return item
}

func questionOffset(cursor, revision string, total int) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	var c struct {
		Revision string `json:"revision"`
		Offset   int    `json:"offset"`
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if len(cursor) > 500 || err != nil || json.Unmarshal(raw, &c) != nil || c.Revision != revision || c.Offset < 0 || c.Offset > total {
		return 0, fmt.Errorf("提问记录已变化，请返回第一页")
	}
	return c.Offset, nil
}

func questionCursor(revision string, end, total int) string {
	if end >= total {
		return ""
	}
	b, _ := json.Marshal(map[string]any{"revision": revision, "offset": end})
	return base64.RawURLEncoding.EncodeToString(b)
}

func (e *Engine) QuestionPage(cursor string, history bool, limit int) (QuestionPage, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	links := e.questionLinksLocked()
	items := []QuestionSummary{}
	for _, q := range e.record.Questions {
		item := e.questionSummaryLocked(q, links[q.ID])
		if history || item.ActiveTasks > 0 {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		}
		return items[i].ID < items[j].ID
	})
	revision := attentionHash([]any{e.record.ID, history, items})
	offset, err := questionOffset(cursor, revision, len(items))
	if err != nil {
		return QuestionPage{}, err
	}
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	end := min(offset+limit, len(items))
	page := QuestionPage{Total: len(items), Revision: revision}
	for {
		page.Items, page.NextCursor = items[offset:end], questionCursor(revision, end, len(items))
		if b, _ := json.Marshal(page); len(b) <= 48<<10 {
			return page, nil
		}
		if end <= offset+1 {
			return QuestionPage{}, fmt.Errorf("提问索引超过读取预算")
		}
		end--
	}
}

func (e *Engine) QuestionDetail(id, cursor string, limit int) (QuestionDetail, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	q, exists := e.record.Questions[id]
	if !exists {
		return QuestionDetail{}, fmt.Errorf("提问记录不存在")
	}
	links := e.questionTaskLinksLocked(id)
	if links == nil {
		links = []QuestionTaskLink{}
	}
	detail := QuestionDetail{QuestionSummary: e.questionSummaryLocked(q, links), Condition: clip(q.Condition, 2400), RespondsTo: clone(q.RespondsTo), TotalConditions: len(links), Error: clip(q.Error, 500)}
	detail.Text = clip(q.Text, 4000)
	if detail.RespondsTo == nil {
		detail.RespondsTo = []QuestionRelation{}
	}
	detail.Revision = attentionHash([]any{e.record.ID, detail, links})
	offset, err := questionOffset(cursor, detail.Revision, len(links))
	if err != nil {
		return QuestionDetail{}, err
	}
	if limit <= 0 || limit > 16 {
		limit = 16
	}
	end := min(offset+limit, len(links))
	for {
		detail.Tasks, detail.NextCursor = links[offset:end], questionCursor(detail.Revision, end, len(links))
		if b, _ := json.Marshal(detail); len(b) <= 48<<10 {
			return detail, nil
		}
		if end <= offset+1 {
			return QuestionDetail{}, fmt.Errorf("提问详情超过读取预算")
		}
		end--
	}
}
