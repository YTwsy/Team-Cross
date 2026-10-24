package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/google/uuid"
	"teamcross/internal/sharing"
	"teamcross/internal/spacecontext"
)

// Member activity is private durable state, never part of Workbench/View or publication exports.
type MemberMention struct {
	MemberID    string `json:"memberId"`
	StartOffset int    `json:"startOffset"`
	EndOffset   int    `json:"endOffset"`
	Label       string `json:"label"`
}
type ReplyTarget struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type ActivitySubject = spacecontext.Subject
type SpaceSourceRef = spacecontext.SourceRef
type MemberFollowPreference struct {
	Subject      ActivitySubject `json:"subject"`
	Mode         string          `json:"mode"`
	PreviousMode string          `json:"previousMode,omitempty"`
	Origin       string          `json:"origin"`
	Revision     uint64          `json:"revision"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}
type ActivityEnvelope struct {
	ID        string          `json:"id"`
	Seq       uint64          `json:"seq"`
	Kind      string          `json:"kind"`
	Group     string          `json:"group"`
	Source    SpaceSourceRef  `json:"source"`
	Subject   ActivitySubject `json:"subject"`
	Actor     SpaceActor      `json:"actor"`
	CreatedAt time.Time       `json:"createdAt"`
	// Only bounded, source-checked previews. Recipient maps stay on the host;
	// each consumer receives only its own reasons in ActivityItem.
	Preview         string                     `json:"preview"`
	Recipients      map[string][]string        `json:"recipients,omitempty"`
	AgentRecipients map[string][]string        `json:"agentRecipients,omitempty"`
	Direct          map[string]string          `json:"direct,omitempty"`
	Mentions        []MemberMention            `json:"mentions,omitempty"`
	MaterialAuthor  string                     `json:"materialAuthor,omitempty"`
	Follow          *spacecontext.FollowChange `json:"follow,omitempty"`
}
type ActivityTurn struct {
	ID        string    `json:"id"`
	State     string    `json:"state"`
	Members   []string  `json:"members,omitempty"`
	ItemIDs   []string  `json:"itemIds,omitempty"`
	Preview   string    `json:"preview,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}
type ActivityTurnInput struct {
	Method   string `json:"method"`
	MemberID string `json:"memberId"`
	TurnID   string `json:"turnId,omitempty"`
	State    string `json:"state"`
}
type ActivitySourceState struct {
	NativeKnown    map[string]bool                              `json:"nativeKnown,omitempty"`
	NativeReady    bool                                         `json:"nativeReady,omitempty"`
	SyncError      string                                       `json:"syncError,omitempty"`
	Preferences    map[string]map[string]MemberFollowPreference `json:"preferences,omitempty"`
	FollowCommands map[string]FollowReceipt                     `json:"followCommands,omitempty"`
	Outbox         []ActivityEnvelope                           `json:"outbox,omitempty"`
	Turns          map[string]ActivityTurn                      `json:"turns,omitempty"`
	Inputs         map[string]ActivityTurnInput                 `json:"inputs,omitempty"`
}
type FollowReceipt struct {
	Hash       string                 `json:"hash"`
	Preference MemberFollowPreference `json:"preference"`
}
type MemberActivityReadState struct {
	Through  uint64          `json:"through"`
	Events   map[string]bool `json:"events"`
	Revision uint64          `json:"revision"`
}
type ActivityLog struct {
	Consumers map[string]ActivityConsumer        `json:"consumers,omitempty"`
	Retention uint64                             `json:"retention,omitempty"`
	Secret    string                             `json:"secret"`
	Seq       uint64                             `json:"seq"`
	Events    []ActivityEnvelope                 `json:"events"`
	Seen      map[string]bool                    `json:"seen"`
	Reads     map[string]MemberActivityReadState `json:"reads"`
}

const maxActivityEvents = 50000
const maxActivityOutbox = 10000
const maxFollowPreferences = 10000
const maxActivityStateBytes = 16 << 20
const maxActivityLogBytes = 64 << 20

func activityClone[T any](value T) T {
	b, _ := json.Marshal(value)
	var out T
	_ = json.Unmarshal(b, &out)
	return out
}
func (s *Session) activityRoot() *Session {
	if s.space != nil {
		return s.space
	}
	return s
}
func subjectKey(v ActivitySubject) string { return v.Kind + ":" + v.ExecutionID + ":" + v.ID }
func (s *Session) activityStateLocked() *ActivitySourceState {
	if s.record.Activity == nil {
		s.record.Activity = &ActivitySourceState{}
	}
	a := s.record.Activity
	if a.Preferences == nil {
		a.Preferences = map[string]map[string]MemberFollowPreference{}
	}
	if a.FollowCommands == nil {
		a.FollowCommands = map[string]FollowReceipt{}
	}
	if a.Turns == nil {
		a.Turns = map[string]ActivityTurn{}
	}
	if a.Inputs == nil {
		a.Inputs = map[string]ActivityTurnInput{}
	}
	return a
}
func activityPreview(text string) string {
	r := []rune(strings.TrimSpace(text))
	return string(r[:min(len(r), 240)])
}
func annotationActor(actor SpaceActor, id, name string) SpaceActor {
	if actor.Kind != "" {
		return actor
	}
	// Historical authors without a typed identity cannot be attributed to a member.
	return SpaceActor{Kind: "unknown", Name: name}
}

type activityActorKey struct{}

func (s *Session) activityAuthorLocked(ctx context.Context, name string) SpaceActor {
	if actor, ok := ctx.Value(activityActorKey{}).(SpaceActor); ok {
		return actor
	}
	if _, ok := ctx.Value(runtimeAnnotationKey{}).(string); ok {
		return SpaceActor{Kind: "session", Name: name}
	}
	return SpaceActor{Kind: "human", MemberID: sharing.MemberID(ctx), Name: name}
}
func (s *Session) validateMentionsLocked(text string, mentions []MemberMention) error {
	if !utf8.ValidString(text) {
		return fmt.Errorf("评论正文不是有效文字")
	}
	chars := utf16.Encode([]rune(text))
	seen := map[string]bool{}
	last := 0
	for _, m := range mentions {
		if m.StartOffset < last || m.EndOffset <= m.StartOffset || m.EndOffset > len(chars) || m.Label == "" || len(m.Label) > 300 {
			return fmt.Errorf("@ 成员的文字范围无效，请重新选择成员")
		}
		if string(utf16.Decode(chars[m.StartOffset:m.EndOffset])) != "@"+m.Label {
			return fmt.Errorf("@ 成员与正文不一致，请重新选择成员")
		}
		if m.MemberID != "owner" && (s.sharedRuntime() == nil || !s.sharedRuntime().HasMember(m.MemberID)) {
			return fmt.Errorf("被 @ 的成员已离开，请移除失效提及后重试")
		}
		seen[m.MemberID] = true
		last = m.EndOffset
	}
	if len(seen) > 20 {
		return fmt.Errorf("每条评论最多 @ 20 位成员")
	}
	return nil
}
func (s *Session) activitySubjects(note Annotation) (ActivitySubject, SpaceSourceRef) {
	root := s.activityRoot()
	ref := SpaceSourceRef{SpaceID: root.record.ID, AnnotationID: note.ID}
	subject := ActivitySubject{Kind: "discussion", ID: note.ID}
	if s.space != nil {
		subject.ExecutionID = s.record.ID
		ref.ExecutionID = s.record.ID
	}
	if note.Target != nil && note.Target.Kind == "material" {
		ref.MaterialID = note.Target.MaterialID
		ref.Version = note.Target.Version
	}
	return subject, ref
}
func (s *Session) activityCandidatesLocked() []ActivityEnvelope {
	root := s.activityRoot()
	out := []ActivityEnvelope{}
	add := func(key string, e ActivityEnvelope) {
		e.ID = contentHash([]string{s.record.ID, key})
		e.Group = subjectKey(e.Subject)
		if e.CreatedAt.IsZero() {
			e.CreatedAt = s.record.UpdatedAt
		}
		e.Preview = activityPreview(e.Preview)
		out = append(out, e)
	}
	for _, n := range s.record.Annotations {
		subject, ref := s.activitySubjects(n)
		actor := annotationActor(n.Actor, n.AuthorID, n.Author)
		add("annotation/"+n.ID, ActivityEnvelope{Kind: "comment", Subject: subject, Source: ref, Actor: actor, CreatedAt: n.CreatedAt, Preview: n.Text, Mentions: n.Mentions})
		for _, r := range n.Replies {
			parent := actor
			if r.ReplyTo != nil && r.ReplyTo.Kind == "reply" {
				parent = SpaceActor{}
				for _, p := range n.Replies {
					if p.ID == r.ReplyTo.ID {
						parent = annotationActor(p.Actor, p.AuthorID, p.Author)
					}
				}
			}
			replyRef := ref
			replyRef.ReplyID = r.ID
			direct := map[string]string{}
			if parent.MemberID != "" {
				direct[parent.MemberID] = "direct_reply"
			}
			add("reply/"+r.ID, ActivityEnvelope{Kind: "reply", Subject: subject, Source: replyRef, Actor: annotationActor(r.Actor, r.AuthorID, r.Author), CreatedAt: r.CreatedAt, Preview: r.Text, Mentions: r.Mentions, Direct: direct})
		}
	}
	for _, m := range s.record.Materials {
		for _, v := range m.Versions {
			kind := "material_updated"
			if v.Version == 1 {
				kind = "material_published"
			}
			add(fmt.Sprintf("material/%s/%d", m.ID, v.Version), ActivityEnvelope{Kind: kind, Subject: ActivitySubject{Kind: "material", ID: m.ID}, Source: SpaceSourceRef{SpaceID: root.record.ID, MaterialID: m.ID, Version: v.Version}, Actor: SpaceActor{Kind: "human", MemberID: m.AuthorID, Name: m.Author}, CreatedAt: v.CreatedAt, Preview: v.Title})
		}
		if m.WithdrawnAt != nil {
			add("withdraw/"+m.ID, ActivityEnvelope{Kind: "material_withdrawn", Subject: ActivitySubject{Kind: "material", ID: m.ID}, Source: SpaceSourceRef{SpaceID: root.record.ID, MaterialID: m.ID}, Actor: SpaceActor{Kind: "system"}, CreatedAt: *m.WithdrawnAt})
		}
	}
	if b := s.record.Workbench; b != nil {
		for _, r := range b.Requests {
			if r.State == "completed" || r.State == "failed" || r.State == "cancelled" {
				direct := map[string]string{}
				if r.Actor.MemberID != "" {
					direct[r.Actor.MemberID] = "own_request_result"
				}
				add("request/"+r.ID+"/"+r.State, ActivityEnvelope{Kind: "request_" + r.State, Subject: ActivitySubject{Kind: "request", ID: r.ID}, Source: SpaceSourceRef{SpaceID: root.record.ID, RequestID: r.ID}, Actor: SpaceActor{Kind: "session", Name: "Agent"}, CreatedAt: r.UpdatedAt, Preview: r.Summary, Direct: direct})
			}
		}
		if b.Brief.Revision > 0 {
			add(fmt.Sprintf("brief/%d", b.Brief.Revision), ActivityEnvelope{Kind: "brief_updated", Subject: ActivitySubject{Kind: "brief", ID: root.record.ID}, Source: SpaceSourceRef{SpaceID: root.record.ID, BriefRevision: b.Brief.Revision}, Actor: b.Brief.UpdatedBy, CreatedAt: b.Brief.UpdatedAt, Preview: b.Brief.Topic})
		}
	}
	if a := s.record.Activity; a != nil {
		for _, turn := range a.Turns {
			if turn.State == "" || turn.State == "inProgress" {
				continue
			}
			pending := false
			for _, input := range a.Inputs {
				if input.TurnID == turn.ID && input.State == "pending" {
					pending = true
				}
			}
			if pending {
				continue
			}
			direct := map[string]string{}
			for _, id := range turn.Members {
				direct[id] = "own_turn_result"
			}
			add("turn/"+turn.ID+"/"+turn.State, ActivityEnvelope{Kind: "turn_" + turn.State, Subject: ActivitySubject{Kind: "execution", ID: s.record.ID}, Source: SpaceSourceRef{SpaceID: root.record.ID, ExecutionID: s.record.ID, TurnID: turn.ID, ItemIDs: turn.ItemIDs}, Actor: SpaceActor{Kind: "session", Name: s.record.Title}, CreatedAt: turn.UpdatedAt, Preview: turn.Preview, Direct: direct})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}
func (s *Session) activityBaselineLocked() {
	s.activitySeen = map[string]bool{}
	for _, e := range s.activityCandidatesLocked() {
		s.activitySeen[e.ID] = true
	}
}
func (s *Session) activityMembersLocked() []string {
	ids := []string{"owner"}
	if s.sharedRuntime() != nil {
		for _, m := range s.sharedRuntime().Members() {
			if m.Active {
				ids = append(ids, m.ID)
			}
		}
	}
	return ids
}
func (s *Session) activityPreferenceLocked(member string, subject ActivitySubject) (MemberFollowPreference, bool) {
	target, err := s.activitySubjectSessionLocked(subject, false)
	if err != nil || target.record.Activity == nil {
		return MemberFollowPreference{}, false
	}
	p, ok := target.record.Activity.Preferences[member][subjectKey(subject)]
	return p, ok
}
func (s *Session) activitySubjectSessionLocked(subject ActivitySubject, requireAvailable bool) (*Session, error) {
	root := s.activityRoot()
	target := root
	if subject.Kind == "execution" {
		target = root.executions[subject.ID]
		if target == nil && s.record.ExecutionRecord != nil && s.record.ID == subject.ID {
			target = s
		}
	}
	if subject.ExecutionID != "" {
		target = root.executions[subject.ExecutionID]
	}
	if target == nil {
		return nil, fmt.Errorf("关注对象已不可用")
	}
	switch subject.Kind {
	case "execution":
		if subject.ExecutionID != "" || target.record.ExecutionRecord == nil {
			return nil, fmt.Errorf("协作会话身份无效")
		}
		return target, nil
	case "material":
		if subject.ExecutionID != "" {
			break
		}
		for _, m := range root.record.Materials {
			if m.ID == subject.ID && (!requireAvailable || m.WithdrawnAt == nil) {
				return root, nil
			}
		}
	case "discussion":
		for _, n := range target.record.Annotations {
			if n.ID == subject.ID {
				if requireAvailable && n.Target != nil && n.Target.Kind == "material" {
					if _, err := root.activitySubjectSessionLocked(ActivitySubject{Kind: "material", ID: n.Target.MaterialID}, true); err != nil {
						return nil, err
					}
				}
				return target, nil
			}
		}
	}
	return nil, fmt.Errorf("关注对象不属于当前空间或已不可用")
}
func (s *Session) activityParentLocked(subject ActivitySubject) ActivitySubject {
	target, err := s.activitySubjectSessionLocked(subject, false)
	if err != nil || subject.Kind != "discussion" {
		return ActivitySubject{}
	}
	for _, n := range target.record.Annotations {
		if n.ID == subject.ID {
			if n.Target != nil && n.Target.Kind == "material" {
				return ActivitySubject{Kind: "material", ID: n.Target.MaterialID}
			}
			if target.space != nil {
				return ActivitySubject{Kind: "execution", ID: target.record.ID}
			}
		}
	}
	return ActivitySubject{}
}
func (s *Session) activityEffectiveLocked(member string, subject ActivitySubject) (string, string) {
	p, ok := s.activityPreferenceLocked(member, subject)
	parent := s.activityParentLocked(subject)
	pp, pok := s.activityPreferenceLocked(member, parent)
	if pok && pp.Mode == "muted" {
		return "muted", parent.Kind
	}
	if ok {
		return p.Mode, subject.Kind
	}
	if pok {
		return pp.Mode, parent.Kind
	}
	return "direct_only", subject.Kind
}
func (s *Session) activityAutoFollowLocked(member string, subject ActivitySubject, origin string) error {
	if member == "" {
		return nil
	}
	a := s.activityStateLocked()
	if a.Preferences[member] == nil {
		a.Preferences[member] = map[string]MemberFollowPreference{}
	}
	key := subjectKey(subject)
	if _, ok := a.Preferences[member][key]; ok {
		return nil
	}
	if len(a.Preferences[member]) >= maxFollowPreferences {
		return fmt.Errorf("关注对象数量已达上限，无法保存本次自动关注")
	}
	p := MemberFollowPreference{Subject: subject, Mode: "following", Origin: origin, Revision: 1, UpdatedAt: time.Now()}
	if err := s.queueFollowChangeLocked(member, p); err != nil {
		return err
	}
	a.Preferences[member][key] = p
	return nil
}
func (s *Session) prepareActivityLocked() error {
	if s.activitySeen == nil {
		s.activityBaselineLocked()
		return nil
	}
	candidates := s.activityCandidatesLocked()
	newEvents := []ActivityEnvelope{}
	for _, e := range candidates {
		if !s.activitySeen[e.ID] {
			newEvents = append(newEvents, e)
		}
	}
	if len(newEvents) == 0 {
		return nil
	}
	a := s.activityStateLocked()
	if len(a.Outbox)+len(newEvents) > maxActivityOutbox {
		return fmt.Errorf("动态待发布队列已满，请恢复同步后重试")
	}
	for _, e := range newEvents {
		// A first publication/participation already creates a fixed follow
		// baseline below. Subsequent human writes to an existing follow must
		// update the shadow even though they do not notify their own author.
		ownMode, _ := s.activityEffectiveLocked(e.Actor.MemberID, e.Subject)
		ownFollowing := e.Actor.Kind == "human" && e.Actor.MemberID != "" && ownMode == "following"
		if e.Actor.MemberID != "" && e.Actor.Kind != "shadow" && (e.Kind == "comment" || e.Kind == "reply") {
			if err := s.activityAutoFollowLocked(e.Actor.MemberID, e.Subject, "participation"); err != nil {
				return err
			}
		}
		if e.Kind == "material_published" {
			if err := s.activityAutoFollowLocked(e.Actor.MemberID, e.Subject, "publication"); err != nil {
				return err
			}
		}
		if e.Source.MaterialID != "" {
			for _, m := range s.activityRoot().record.Materials {
				if m.ID == e.Source.MaterialID {
					e.MaterialAuthor = m.AuthorID
				}
			}
		}
		e.Recipients = map[string][]string{}
		for _, id := range s.activityMembersLocked() {
			if e.Actor.Kind == "human" && e.Actor.MemberID == id {
				if ownFollowing {
					_, kind := s.activityEffectiveLocked(id, e.Subject)
					e.AgentRecipients = map[string][]string{id: {"own_source_change", "follow_" + kind}}
				}
				continue
			}
			mode, kind := s.activityEffectiveLocked(id, e.Subject)
			if mode == "muted" {
				continue
			}
			reasons := []string{}
			for _, m := range e.Mentions {
				if m.MemberID == id {
					reasons = append(reasons, "mention")
					break
				}
			}
			if reason := e.Direct[id]; reason != "" {
				reasons = append(reasons, reason)
			}
			if mode == "following" {
				reasons = append(reasons, "follow_"+kind)
				parent := s.activityParentLocked(e.Subject)
				if kind == e.Subject.Kind && parent.Kind != "" {
					if p, ok := s.activityPreferenceLocked(id, parent); ok && p.Mode == "following" {
						reasons = append(reasons, "follow_"+parent.Kind)
					}
				}
			}
			if len(reasons) > 0 {
				e.Recipients[id] = reasons
			}
		}
		e.Direct = nil
		e.Mentions = nil
		if len(a.Outbox) >= maxActivityOutbox {
			return fmt.Errorf("动态待发布队列已满，请恢复同步后重试")
		}
		a.Outbox = append(a.Outbox, e)
	}
	return nil
}
func (s *Session) loadActivityLogLocked() error {
	root := s.activityRoot()
	if root.activityLog != nil {
		return nil
	}
	var log ActivityLog
	err := readJSON(filepath.Join(root.runtimeDirectory(), "member-activity.json"), &log)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("动态存储读取失败: %w", err)
	}
	if os.IsNotExist(err) {
		log = ActivityLog{Secret: uuid.NewString(), Seen: map[string]bool{}, Reads: map[string]MemberActivityReadState{}, Events: []ActivityEnvelope{}}
	}
	if log.Secret == "" || log.Seen == nil || log.Reads == nil {
		return fmt.Errorf("动态存储不完整，原始记录已保留")
	}
	if log.Consumers == nil {
		log.Consumers = map[string]ActivityConsumer{}
	}
	for member, c := range log.Consumers {
		if member == "" || c.ShadowID == "" || c.Revision == 0 || c.Through > log.Seq || c.InitialSeq > c.Through || c.InitialDone < 0 || c.InitialDone > len(c.Initial) {
			return fmt.Errorf("分身动态接收记录无效，原始记录已保留")
		}
	}
	root.activityLog = &log
	return nil
}
func (s *Session) drainActivityLocked() error {
	root := s.activityRoot()
	if err := root.loadActivityLogLocked(); err != nil {
		return err
	}
	records := []*Session{root}
	for _, child := range root.executions {
		records = append(records, child)
	}
	next := activityClone(*root.activityLog)
	// Source snapshots and pending outboxes retain event identity across restarts.
	// Only prune read/public metadata; unread reminders and unacknowledged outbox IDs survive.
	pending := map[string]bool{}
	for _, record := range records {
		if record.record.Activity != nil {
			for _, e := range record.record.Activity.Outbox {
				pending[e.ID] = true
			}
		}
	}
	changed := next.pruneActivity(time.Now().AddDate(0, 0, -90), pending)
	for _, record := range records {
		if record.record.Activity == nil {
			continue
		}
		for _, e := range record.record.Activity.Outbox {
			if next.Seen[e.ID] {
				continue
			}
			if len(next.Events) >= maxActivityEvents {
				return fmt.Errorf("动态存储已满，待发布事件已保留")
			}
			next.Seq++
			e.Seq = next.Seq
			next.Events = append(next.Events, e)
			next.Seen[e.ID] = true
			changed = true
		}
	}
	if changed {
		encoded, err := json.Marshal(next)
		if err != nil {
			return err
		}
		if len(encoded) > maxActivityLogBytes {
			return fmt.Errorf("动态存储字节上限已达，待发布事件已保留")
		}
		if err := writeJSONFile(filepath.Join(root.runtimeDirectory(), "member-activity.json"), next); err != nil {
			return err
		}
		root.activityLog = &next
	}
	for _, record := range records {
		if record.record.Activity == nil || len(record.record.Activity.Outbox) == 0 {
			continue
		}
		previous := record.record.Activity
		record.record.Activity = activityClone(previous)
		record.record.Activity.Outbox = nil
		if err := record.saveRecordLocked(); err != nil {
			record.record.Activity = previous
			return err
		}
	}
	root.activityError = ""
	return nil
}

func (log *ActivityLog) pruneActivity(before time.Time, pending map[string]bool) bool {
	consumerPending := map[string]bool{}
	for _, consumer := range log.Consumers {
		for i, id := range consumer.Initial {
			if i >= consumer.InitialDone {
				consumerPending[id] = true
			}
		}
	}
	kept := make([]ActivityEnvelope, 0, len(log.Events))
	removed := false
	for _, e := range log.Events {
		retain := !e.CreatedAt.Before(before) || pending[e.ID] || consumerPending[e.ID]
		for member := range e.Recipients {
			consumer, consuming := log.Consumers[member]
			if activityUnread(log, e, member) || (consuming && e.Seq > consumer.Through) {
				retain = true
				break
			}
		}
		for member := range e.AgentRecipients {
			if consumer, ok := log.Consumers[member]; ok && e.Seq > consumer.Through {
				retain = true
				break
			}
		}
		if retain {
			kept = append(kept, e)
			continue
		}
		removed = true
		delete(log.Seen, e.ID)
		for member, r := range log.Reads {
			delete(r.Events, e.ID)
			log.Reads[member] = r
		}
	}
	if removed {
		log.Events = kept
		log.Retention++
	}
	return removed
}
