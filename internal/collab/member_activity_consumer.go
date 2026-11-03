package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"teamcross/internal/sharing"
)

// A consumer receipt acknowledges durable delivery to the member's Core. It
// never changes human Reads or claims that a model has understood the event.
type ActivityConsumer struct {
	ShadowID    string                     `json:"shadowId"`
	Revision    uint64                     `json:"revision"`
	Enabled     bool                       `json:"enabled"`
	Through     uint64                     `json:"through"`
	InitialSeq  uint64                     `json:"initialSeq"`
	Initial     []string                   `json:"initial"`
	InitialDone int                        `json:"initialDone"`
	Commands    map[string]consumerCommand `json:"commands,omitempty"`
}

type consumerCommand struct {
	Hash  string             `json:"hash"`
	Route ActivityAgentRoute `json:"route"`
}

type ActivityAgentRoute struct {
	ShadowID         string `json:"shadowId,omitempty"`
	Revision         uint64 `json:"revision"`
	Enabled          bool   `json:"enabled"`
	Through          uint64 `json:"through"`
	InitialRemaining int    `json:"initialRemaining"`
}

func (c ActivityConsumer) route() ActivityAgentRoute {
	return ActivityAgentRoute{ShadowID: c.ShadowID, Revision: c.Revision, Enabled: c.Enabled, Through: c.Through, InitialRemaining: len(c.Initial) - c.InitialDone}
}

type ActivityConsumerInput struct {
	Operation    string `json:"operation,omitempty"`
	ShadowID     string `json:"shadowId,omitempty"`
	BaseRevision uint64 `json:"baseRevision,omitempty"`
	Enabled      *bool  `json:"enabled,omitempty"`
	RequestID    string `json:"requestId,omitempty"`
	ThroughSeq   uint64 `json:"throughSeq,omitempty"`
	Receipt      string `json:"receipt,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

type ActivityConsumerPage struct {
	Route       ActivityAgentRoute `json:"route"`
	Items       []ActivityItem     `json:"items"`
	Receipt     string             `json:"receipt,omitempty"`
	ThroughSeq  uint64             `json:"throughSeq"`
	SnapshotSeq uint64             `json:"snapshotSeq"`
	Initial     bool               `json:"initial"`
	More        bool               `json:"more"`
}

func consumerQuery(c ActivityConsumer, initial bool) string {
	return "agent/" + c.ShadowID + "/" + strconv.FormatUint(c.Revision, 10) + "/" + strconv.FormatBool(initial)
}

func activityDirect(reasons []string) bool {
	for _, reason := range reasons {
		if reason == "mention" || reason == "direct_reply" || reason == "own_request_result" || reason == "own_turn_result" {
			return true
		}
	}
	return false
}

func activityAgentReasons(event ActivityEnvelope, member string) []string {
	reasons := append([]string{}, event.Recipients[member]...)
	for _, reason := range event.AgentRecipients[member] {
		found := false
		for _, old := range reasons {
			found = found || old == reason
		}
		if !found {
			reasons = append(reasons, reason)
		}
	}
	return reasons
}

func (s *Session) firstMemberShadowLocked(member string) string {
	ids := []string{}
	for id, entry := range s.record.Shadows {
		if entry.View.Owner.ID == member {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := s.record.Shadows[ids[i]].View, s.record.Shadows[ids[j]].View
		if a.CreatedAt.Equal(b.CreatedAt) {
			return ids[i] < ids[j]
		}
		return a.CreatedAt.Before(b.CreatedAt)
	})
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func (s *Session) saveActivityConsumerLocked(member string, c ActivityConsumer) error {
	next := activityClone(*s.activityLog)
	if next.Consumers == nil {
		next.Consumers = map[string]ActivityConsumer{}
	}
	next.Consumers[member] = c
	data, err := json.Marshal(next)
	if err != nil || len(data) > maxActivityLogBytes {
		return fmt.Errorf("分身接收状态超过动态存储预算，记录尚未修改")
	}
	if err := writeJSONFile(s.runtimeDirectory()+"/member-activity.json", next); err != nil {
		return err
	}
	s.activityLog = &next
	return nil
}

func (s *Session) activityConsumerOperation(ctx context.Context, op string, in ActivityConsumerInput) (any, error) {
	if _, bound := ctx.Value(runtimeAnnotationKey{}).(string); bound {
		return nil, fmt.Errorf("成员动态消费者由所属成员 Core 管理")
	}
	s = s.activityRoot()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.record.State == "ended" || s.record.State == "unsupported" || !s.callerValidLocked(ctx) {
		return nil, fmt.Errorf("成员动态访问已失效")
	}
	if err := s.drainActivityLocked(); err != nil {
		return nil, err
	}
	member := sharing.MemberID(ctx)
	c, exists := s.activityLog.Consumers[member]
	switch op {
	case "status":
		return c.route(), nil
	case "ensure", "configure":
		if op == "ensure" && exists {
			return c.route(), nil
		}
		fingerprint := contentHash(in)
		if op == "configure" {
			if in.RequestID == "" || len(in.RequestID) > 200 {
				return nil, fmt.Errorf("配置分身接手需要稳定 requestId")
			}
			if old, ok := c.Commands[in.RequestID]; ok {
				if old.Hash != fingerprint {
					return nil, fmt.Errorf("配置 requestId 已用于不同内容")
				}
				return old.Route, nil
			}
			if c.Revision != in.BaseRevision {
				return nil, fmt.Errorf("默认分身设置已变化，请刷新后更新")
			}
			if len(c.Commands) >= 1000 {
				return nil, fmt.Errorf("分身配置回执已达存储上限")
			}
		}
		id := in.ShadowID
		if op == "ensure" {
			// Election happens on the authoritative host, not in the order in
			// which local runtimes happen to be pumped or reconnect.
			id = s.firstMemberShadowLocked(member)
			if id == "" {
				return ActivityAgentRoute{}, nil
			}
		}
		if id == "" {
			id = c.ShadowID
		}
		entry, ok := s.record.Shadows[id]
		if !ok || entry.View.Owner.ID != member {
			return nil, fmt.Errorf("请选择本空间属于本人的分身")
		}
		if !exists {
			c = ActivityConsumer{Enabled: true, Through: s.activityLog.Seq, InitialSeq: s.activityLog.Seq, Initial: []string{}, Commands: map[string]consumerCommand{}}
			// Freeze the initial set. Later human reads must not remove items
			// between pages. A previously read direct question may be unresolved.
			for _, event := range s.activityLog.Events {
				if len(activityAgentReasons(event, member)) > 0 && s.activityAvailableLocked(event) && (event.Follow != nil || len(event.AgentRecipients[member]) > 0 || activityUnread(s.activityLog, event, member) || activityDirect(event.Recipients[member])) {
					c.Initial = append(c.Initial, event.ID)
				}
			}
		}
		c = activityClone(c)
		c.ShadowID, c.Revision = id, c.Revision+1
		if in.Enabled != nil {
			c.Enabled = *in.Enabled
		}
		if c.Commands == nil {
			c.Commands = map[string]consumerCommand{}
		}
		if op == "configure" {
			c.Commands[in.RequestID] = consumerCommand{Hash: fingerprint, Route: c.route()}
		}
		if err := s.saveActivityConsumerLocked(member, c); err != nil {
			return nil, err
		}
		return c.route(), nil
	case "consume", "ack":
		if !exists || in.ShadowID != c.ShadowID || in.BaseRevision != c.Revision {
			return nil, fmt.Errorf("分身消费路线已变化，请重新读取")
		}
		if op == "consume" {
			return s.activityConsumerPageLocked(member, c, in)
		}
		cursor, err := s.activityCursorLocked(in.Receipt, member)
		if err != nil {
			return nil, err
		}
		previousThrough, previousInitial := c.Through, c.InitialDone
		switch cursor.Query {
		case consumerQuery(c, true):
			if cursor.Through != c.InitialSeq || cursor.Offset > len(c.Initial) {
				return nil, fmt.Errorf("初始接收回执不匹配")
			}
			c.InitialDone = max(c.InitialDone, cursor.Offset)
		case consumerQuery(c, false):
			if c.InitialDone != len(c.Initial) || cursor.Offset != 0 {
				return nil, fmt.Errorf("尚有初始动态未持久接收")
			}
			c.Through = max(c.Through, cursor.Through)
		default:
			return nil, fmt.Errorf("该回执不属于当前分身消费者")
		}
		if previousThrough == c.Through && previousInitial == c.InitialDone {
			return c.route(), nil
		}
		if err := s.saveActivityConsumerLocked(member, c); err != nil {
			return nil, err
		}
		return c.route(), nil
	}
	return nil, fmt.Errorf("分身动态操作不受支持")
}

func (s *Session) activityConsumerPageLocked(member string, c ActivityConsumer, in ActivityConsumerInput) (ActivityConsumerPage, error) {
	page := ActivityConsumerPage{Route: c.route(), Items: []ActivityItem{}, ThroughSeq: c.Through, SnapshotSeq: s.activityLog.Seq, Initial: c.InitialDone < len(c.Initial)}
	if !c.Enabled {
		return page, nil
	}
	limit := in.Limit
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return page, fmt.Errorf("动态批次大小需要 1–100")
	}
	if in.ThroughSeq != 0 {
		if in.ThroughSeq > page.SnapshotSeq || in.ThroughSeq < c.Through {
			return page, fmt.Errorf("动态批次上界无效")
		}
		page.SnapshotSeq = in.ThroughSeq
	}
	bytes := 0
	add := func(e ActivityEnvelope) (bool, error) {
		reasons := activityAgentReasons(e, member)
		if len(reasons) == 0 || !s.activityAvailableLocked(e) || (e.Actor.Kind == "shadow" && e.Actor.MemberID == member) {
			return true, nil
		}
		mode, _ := s.activityEffectiveLocked(member, e.Subject)
		if e.Follow != nil {
			current, exists := s.activityPreferenceLocked(member, e.Subject)
			// A paged initial review must not initialize an old follow while
			// its later cancellation is still waiting on another page.
			if !exists || current.Revision != e.Follow.Revision || e.Follow.Mode == "following" && mode != "following" {
				return true, nil
			}
		}
		if e.Follow == nil && (mode == "muted" || (mode == "direct_only" && !activityDirect(reasons))) {
			return true, nil
		}
		item := ActivityItem{ID: e.ID, Seq: e.Seq, Kind: e.Kind, Source: e.Source, Subject: e.Subject, Actor: e.Actor, CreatedAt: e.CreatedAt, Preview: e.Preview, Reasons: reasons, Follow: activityClone(e.Follow)}
		data, err := json.Marshal(item)
		if err != nil || len(data) > 64<<10 {
			return false, fmt.Errorf("单条动态超过分身接收预算，未推进接收位置")
		}
		if len(page.Items) >= limit || bytes+len(data) > 64<<10 {
			return false, nil
		}
		bytes += len(data)
		page.Items = append(page.Items, item)
		return true, nil
	}
	cursor := activityCursor{Member: member, Query: consumerQuery(c, page.Initial)}
	if page.Initial {
		byID := map[string]ActivityEnvelope{}
		for _, e := range s.activityLog.Events {
			byID[e.ID] = e
		}
		cursor.Through, cursor.Offset = c.InitialSeq, c.InitialDone
		for i := c.InitialDone; i < len(c.Initial); i++ {
			e, ok := byID[c.Initial[i]]
			if !ok {
				return page, fmt.Errorf("resync_required: 初始动态已不可重放")
			}
			accepted, err := add(e)
			if err != nil {
				return page, err
			}
			if !accepted {
				break
			}
			cursor.Offset = i + 1
		}
		page.More = cursor.Offset < len(c.Initial) || s.activityLog.Seq > c.Through
	} else {
		cursor.Through = c.Through
		for _, e := range s.activityLog.Events {
			if e.Seq <= c.Through || e.Seq > page.SnapshotSeq {
				continue
			}
			accepted, err := add(e)
			if err != nil {
				return page, err
			}
			if !accepted {
				page.More = true
				break
			}
			cursor.Through = e.Seq
		}
		if !page.More {
			// Non-recipient and pruned events are sequence gaps, not lost work.
			cursor.Through = page.SnapshotSeq
		}
		page.ThroughSeq = cursor.Through
	}
	if page.Initial || cursor.Through > c.Through {
		page.Receipt = s.activityTokenLocked(cursor)
	}
	return page, nil
}

func (a *App) activityConsumerCall(ctx context.Context, id, op string, in ActivityConsumerInput) (any, error) {
	a.mu.Lock()
	s, j, execution := a.targetLocked(id)
	a.mu.Unlock()
	if execution != "" {
		return nil, fmt.Errorf("分身接手需要空间 ID")
	}
	if j != nil {
		j.mu.Lock()
		hostID := j.Invitation.ID
		j.mu.Unlock()
		var out any
		if err := j.request(ctx, "POST", "/v2/activity-consumer/"+op, in, &out); err != nil {
			return nil, err
		}
		return remapWorkbench(out, hostID, id), nil
	}
	if s == nil {
		return nil, fmt.Errorf("空间不存在")
	}
	return s.activityConsumerOperation(ctx, op, in)
}

func (s *Session) remoteActivityConsumerHTTP(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/v2/activity-consumer/") {
		return false
	}
	if r.Method != "POST" {
		http.NotFound(w, r)
		return true
	}
	var in ActivityConsumerInput
	if !decode(w, r, &in) {
		return true
	}
	out, err := s.activityConsumerOperation(r.Context(), strings.TrimPrefix(r.URL.Path, "/v2/activity-consumer/"), in)
	respond(w, out, err)
	return true
}

func (a *App) activityConsumerHTTP(w http.ResponseWriter, r *http.Request, path string) bool {
	parts := strings.Split(path, "/")
	if len(parts) != 3 || parts[0] != "collaborations" || parts[2] != "activity-agent" {
		return false
	}
	op, in := "status", ActivityConsumerInput{}
	switch r.Method {
	case "GET":
	case "PUT":
		op = "configure"
		if !decode(w, r, &in) {
			return true
		}
	default:
		http.NotFound(w, r)
		return true
	}
	out, err := a.activityConsumerCall(r.Context(), parts[1], op, in)
	respond(w, out, err)
	return true
}
