package shadow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"teamcross/internal/spacecontext"
)

type AttentionRoute struct {
	ShadowID string `json:"shadowId"`
	Revision uint64 `json:"revision"`
	Enabled  bool   `json:"enabled"`
}

type SourceSnapshot struct {
	Source      spacecontext.SourceRef   `json:"source"`
	Data        json.RawMessage          `json:"data"`
	Partial     bool                     `json:"partial"`
	Latest      bool                     `json:"latest"`
	Unavailable string                   `json:"unavailable,omitempty"`
	ObservedAt  time.Time                `json:"observedAt"`
	References  []spacecontext.SourceRef `json:"references,omitempty"`
}

type AttentionReceipt struct {
	Token   string         `json:"token"`
	Route   AttentionRoute `json:"route"`
	Through uint64         `json:"through"`
	Initial bool           `json:"initial"`
}

type AttentionEvent struct {
	Signal         spacecontext.Signal `json:"signal"`
	Fingerprint    string              `json:"fingerprint"`
	FocusID        string              `json:"focusId"`
	State          string              `json:"state"`
	ReceivedAt     time.Time           `json:"receivedAt"`
	TaskDeliveries []TaskDelivery      `json:"taskDeliveries,omitempty"`
	ObserveOnly    bool                `json:"observeOnly,omitempty"`
}

// Focus is understanding of one object, independent of the member's follow
// preference and of a native thread. A route change must retain this record.
type Focus struct {
	ID                    string                   `json:"id"`
	Subject               spacecontext.Subject     `json:"subject"`
	Goal                  string                   `json:"goal,omitempty"`
	GoalSourceID          string                   `json:"goalSourceId,omitempty"`
	GoalVersion           uint64                   `json:"goalVersion"`
	Revision              uint64                   `json:"revision"`
	UnderstandingRevision uint64                   `json:"understandingRevision,omitempty"`
	Summary               string                   `json:"summary,omitempty"`
	Evidence              []spacecontext.SourceRef `json:"evidence"`
	OpenQuestions         []string                 `json:"openQuestions"`
	ActionIDs             []string                 `json:"actionIds"`
	JobIDs                []string                 `json:"jobIds"`
	QuestionIDs           []string                 `json:"questionIds,omitempty"`
	ProcessedSeq          uint64                   `json:"processedSeq"`
	State                 string                   `json:"state"`
	Processing            string                   `json:"processing"`
	UpdatedAt             time.Time                `json:"updatedAt"`
	Error                 string                   `json:"error,omitempty"`
	RetryAfter            time.Time                `json:"retryAfter,omitempty"`
}

type AttentionRecord struct {
	Schema                int                                   `json:"schema"`
	SpaceID               string                                `json:"spaceId"`
	MemberID              string                                `json:"memberId"`
	Route                 AttentionRoute                        `json:"route"`
	Events                map[string]AttentionEvent             `json:"events"`
	Focuses               map[string]Focus                      `json:"focuses"`
	Batches               map[string]ObservationBatch           `json:"batches"`
	Continuations         map[string]AttentionContinuation      `json:"continuations"`
	Follows               map[string]spacecontext.FollowSetting `json:"follows,omitempty"`
	WatchCommands         map[string]WatchCommand               `json:"watchCommands,omitempty"`
	Handling              map[string]QuestionHandling           `json:"handling"`
	HandlingCommands      map[string]HandlingCommand            `json:"handlingCommands"`
	UnderstandingCommands map[string]TaskUnderstandingCommand   `json:"understandingCommands,omitempty"`
	UnderstoodAnswers     map[string]bool                       `json:"understoodAnswers,omitempty"`
	Pending               *AttentionReceipt                     `json:"pending,omitempty"`
	Error                 string                                `json:"error,omitempty"`
	UpdatedAt             time.Time                             `json:"updatedAt"`
}

// AttentionStore belongs to the member Core, not to a particular shadow. It
// is deliberately excluded from the public C1 projection and survives changing
// the default shadow, pausing it, or replacing a native conversation segment.
type AttentionStore struct {
	mu        sync.Mutex
	directory string
	record    AttentionRecord
}

const maxAttentionEvents = 50000
const maxAttentionBytes = 32 << 20

func OpenAttention(directory, spaceID, memberID string) (*AttentionStore, error) {
	if spaceID == "" || memberID == "" || !filepath.IsAbs(directory) {
		return nil, fmt.Errorf("分身动态存储需要明确空间、成员和目录")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	r := AttentionRecord{Schema: 1, SpaceID: spaceID, MemberID: memberID, Events: map[string]AttentionEvent{}, Focuses: map[string]Focus{}, Batches: map[string]ObservationBatch{}, Continuations: map[string]AttentionContinuation{}}
	data, err := os.ReadFile(filepath.Join(directory, "attention.json"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		if len(data) > maxAttentionBytes || json.Unmarshal(data, &r) != nil || r.Schema != 1 || r.SpaceID != spaceID || r.MemberID != memberID || r.Events == nil || r.Focuses == nil {
			return nil, fmt.Errorf("分身动态存储无效或归属不符，原记录已保留")
		}
		for id, e := range r.Events {
			if e.Signal.ID != id || e.Signal.Source.SpaceID != spaceID || e.Fingerprint != attentionHash(e.Signal) || r.Focuses[e.FocusID].ID != e.FocusID {
				return nil, fmt.Errorf("分身已接收事件不完整，原记录已保留")
			}
		}
	}
	if r.Batches == nil {
		r.Batches = map[string]ObservationBatch{}
	}
	if r.Continuations == nil {
		r.Continuations = map[string]AttentionContinuation{}
	}
	if r.Follows == nil {
		r.Follows = map[string]spacecontext.FollowSetting{}
	}
	if r.Handling == nil {
		r.Handling = map[string]QuestionHandling{}
	}
	if r.HandlingCommands == nil {
		r.HandlingCommands = map[string]HandlingCommand{}
	}
	return &AttentionStore{directory: directory, record: r}, nil
}

func attentionHash(value any) string {
	b, _ := json.Marshal(value)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (s *AttentionStore) Identity() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.record.SpaceID, s.record.MemberID
}

func (s *AttentionStore) ValidateGoal(id string, version uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.record.Focuses[id]
	if !ok || f.GoalVersion != version || f.State == "paused" {
		return fmt.Errorf("关注目标已变化或暂停，停止旧目标行动")
	}
	return nil
}

func (s *AttentionStore) HasPending(shadowID string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.record.Route.Enabled {
		return false
	}
	for _, c := range s.record.Continuations {
		f := s.record.Focuses[c.FocusID]
		if c.ShadowID == shadowID && (c.State == "queued" || c.State == "running") && continuationFollowEligible(s.record, c) && (c.HandlingID != "" || f.State != "paused" && f.GoalVersion == c.GoalVersion) {
			return true
		}
	}
	if s.record.Route.ShadowID != shadowID {
		return false
	}
	for _, event := range s.record.Events {
		f := s.record.Focuses[event.FocusID]
		if event.State == "queued" && attentionEligible(s.record, event.Signal) && f.State != "paused" && attentionRetryReady(s.record, event, now) {
			return true
		}
	}
	return false
}

func (s *AttentionStore) Snapshot() AttentionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.record)
}

func (s *AttentionStore) memberChangesLocked(eventIDs []string) []spacecontext.SourceRef {
	refs := []spacecontext.SourceRef{}
	for _, id := range eventIDs {
		signal := s.record.Events[id].Signal
		if signal.Actor.Kind != "human" || signal.Actor.MemberID != s.record.MemberID {
			continue
		}
		for _, reason := range signal.Reasons {
			if reason == "own_source_change" {
				refs = append(refs, clone(signal.Source))
				break
			}
		}
	}
	return uniqueQuestionAndOtherSources(refs)
}

func (s *AttentionStore) observationContext(batch ObservationBatch) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]any{"batchId": batch.ID, "memberId": s.record.MemberID, "memberChanges": s.memberChangesLocked(batch.EventIDs), "focus": batch.Focus, "sources": batch.Sources}
}

func (s *AttentionStore) saveLocked(next AttentionRecord) error {
	next.UpdatedAt = time.Now()
	data, err := json.Marshal(next)
	if err != nil || len(data) > maxAttentionBytes || len(next.Events) > maxAttentionEvents || len(next.Focuses) > 10000 {
		return fmt.Errorf("分身动态待处理记录已达容量边界，接收位置未推进")
	}
	if err := writeAtomic(filepath.Join(s.directory, "attention.json"), next); err != nil {
		return err
	}
	s.record = next
	return nil
}

func (s *AttentionStore) SetRoute(route AttentionRoute) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if route.ShadowID == "" || route.Revision == 0 || route.Revision < s.record.Route.Revision {
		return fmt.Errorf("分身接手路线无效或已过时")
	}
	if route.Revision == s.record.Route.Revision {
		if route != s.record.Route {
			return fmt.Errorf("同一分身路线版本对应不同内容")
		}
		return nil
	}
	next := clone(s.record)
	next.Route, next.Pending = route, nil
	for _, batch := range next.Batches {
		if batch.State == "preparing" {
			resetBatch(&next, batch, "superseded", "", time.Time{})
		}
	}
	for id, c := range next.Continuations {
		if c.State == "queued" {
			c.ShadowID = route.ShadowID
			next.Continuations[id] = c
			if c.Mode == "direct" {
				batch := next.Batches[c.BatchID]
				batch.ShadowID, batch.RouteRevision = route.ShadowID, route.Revision
				next.Batches[batch.ID] = batch
			}
		}
	}
	// A stale receipt is discarded, not its persisted events. The host will
	// replay its unacknowledged prefix under the new route; IDs deduplicate it.
	return s.saveLocked(next)
}

func (s *AttentionStore) Accept(receipt AttentionReceipt, signals []spacecontext.Signal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if receipt.Route != s.record.Route || receipt.Token == "" || !receipt.Route.Enabled || len(signals) > 100 {
		return fmt.Errorf("动态批次与当前接手路线不符")
	}
	if s.record.Pending != nil && *s.record.Pending != receipt {
		return fmt.Errorf("上一批持久回执尚未确认")
	}
	next := clone(s.record)
	now := time.Now()
	for _, signal := range signals {
		if signal.ID == "" || len(signal.ID) > 200 || signal.Seq == 0 || signal.Source.SpaceID != next.SpaceID || signal.Subject.ID == "" || signal.Subject.Kind == "" {
			return fmt.Errorf("动态来源或对象无效")
		}
		signal = clone(signal)
		fingerprint := attentionHash(signal)
		if old, ok := next.Events[signal.ID]; ok {
			if old.Fingerprint != fingerprint {
				return fmt.Errorf("同一动态 ID 对应不同来源或内容")
			}
			continue
		}
		focusID := attentionHash(signal.Subject)
		focus, ok := next.Focuses[focusID]
		if !ok {
			focus = Focus{ID: focusID, Subject: signal.Subject, GoalVersion: 1, Revision: 1, Evidence: []spacecontext.SourceRef{}, OpenQuestions: []string{}, ActionIDs: []string{}, JobIDs: []string{}, State: "active"}
		}
		if focus.Processing != "observing" && focus.Processing != "preparing" {
			focus.Processing = "queued"
			if focus.Error == "" {
				focus.RetryAfter = time.Time{}
			}
		}
		focus.UpdatedAt = now
		next.Focuses[focusID] = focus
		next.Events[signal.ID] = AttentionEvent{Signal: signal, Fingerprint: fingerprint, FocusID: focusID, State: "queued", ReceivedAt: now}
		if h := next.Handling[questionSourceID(signal.Source)]; h.Handler == "owner" {
			event := next.Events[signal.ID]
			event.State = "owner"
			next.Events[signal.ID] = event
		}
		if signal.Follow != nil {
			if err := acceptFollow(&next, signal); err != nil {
				return err
			}
		}
	}
	reconcileFollowBatches(&next)
	reconcileUnderstoodEvents(&next)
	next.Pending, next.Error = &receipt, ""
	return s.saveLocked(next)
}

func (s *AttentionStore) ConfirmReceipt(receipt AttentionReceipt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.Pending == nil {
		return nil
	}
	if *s.record.Pending != receipt {
		return fmt.Errorf("待确认批次已变化")
	}
	next := clone(s.record)
	next.Pending, next.Error = nil, ""
	return s.saveLocked(next)
}

func (s *AttentionStore) PendingReceipt() *AttentionReceipt {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.record.Pending)
}

func (s *AttentionStore) SetError(message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.Error == message {
		return nil
	}
	next := clone(s.record)
	next.Error = message
	return s.saveLocked(next)
}

func (e *Engine) AttachAttention(s *AttentionStore) error {
	spaceID, memberID := s.Identity()
	e.mu.Lock()
	defer e.mu.Unlock()
	if spaceID != e.record.SpaceID || memberID != e.record.Owner.ID {
		return fmt.Errorf("分身动态存储归属不匹配")
	}
	e.attention = s
	e.notify()
	return nil
}
