package collab

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"teamcross/internal/sharing"
)

type ActivityInput struct {
	VisibleSource *SpaceSourceRef `json:"visibleSource,omitempty"`
	View          string          `json:"view,omitempty"`
	Category      string          `json:"category,omitempty"`
	Unread        bool            `json:"unread,omitempty"`
	Cursor        string          `json:"cursor,omitempty"`
	Limit         int             `json:"limit,omitempty"`
	Subject       ActivitySubject `json:"subject,omitempty"`
	Mode          string          `json:"mode,omitempty"`
	Revision      uint64          `json:"revision,omitempty"`
	RequestID     string          `json:"requestId,omitempty"`
	EventIDs      []string        `json:"eventIds,omitempty"`
	Snapshot      string          `json:"snapshot,omitempty"`
	Group         string          `json:"group,omitempty"`
}
type ActivityItem struct {
	ID        string          `json:"id"`
	Seq       uint64          `json:"seq"`
	Kind      string          `json:"kind"`
	Source    SpaceSourceRef  `json:"source"`
	Subject   ActivitySubject `json:"subject"`
	Actor     SpaceActor      `json:"actor"`
	CreatedAt time.Time       `json:"createdAt"`
	Preview   string          `json:"preview"`
	Reasons   []string        `json:"reasons"`
	Unread    bool            `json:"unread"`
}
type ActivityGroup struct {
	ID        string         `json:"id"`
	Latest    ActivityItem   `json:"latest"`
	Attention *ActivityItem  `json:"attention,omitempty"`
	Events    []ActivityItem `json:"events"`
	Count     int            `json:"count"`
	Unread    int            `json:"unread"`
}
type ActivityPage struct {
	PendingSync  bool            `json:"pendingSync,omitempty"`
	SpaceID      string          `json:"spaceId"`
	Title        string          `json:"title"`
	SelfID       string          `json:"selfId"`
	Groups       []ActivityGroup `json:"groups"`
	Unread       int             `json:"unread"`
	Snapshot     string          `json:"snapshot"`
	Next         string          `json:"nextCursor,omitempty"`
	Revision     uint64          `json:"revision"`
	ReadRevision uint64          `json:"readRevision"`
	Error        string          `json:"error,omitempty"`
	SyncedAt     time.Time       `json:"syncedAt"`
}
type FollowView struct {
	MemberFollowPreference
	EffectiveMode string `json:"effectiveMode"`
	InheritedFrom string `json:"inheritedFrom,omitempty"`
	Available     bool   `json:"available"`
	Title         string `json:"title"`
}
type FollowPage struct {
	Items     []FollowView `json:"items"`
	Next      string       `json:"nextCursor,omitempty"`
	Total     int          `json:"total"`
	Following int          `json:"following"`
}
type activityCursor struct {
	Member       string `json:"m"`
	Through      uint64 `json:"s"`
	Query        string `json:"q,omitempty"`
	Offset       int    `json:"o,omitempty"`
	ReadRevision uint64 `json:"r,omitempty"`
	Retention    uint64 `json:"h,omitempty"`
}

func (s *Session) activityTokenLocked(c activityCursor) string {
	data, _ := json.Marshal(c)
	h := hmac.New(sha256.New, []byte(s.activityLog.Secret))
	h.Write(data)
	return base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (s *Session) activityCursorLocked(token, member string) (activityCursor, error) {
	var c activityCursor
	p := strings.Split(token, ".")
	if len(p) != 2 || len(token) > 4096 {
		return c, fmt.Errorf("动态快照无效，请刷新")
	}
	data, err := base64.RawURLEncoding.DecodeString(p[0])
	if err != nil {
		return c, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(p[1])
	if err != nil {
		return c, err
	}
	h := hmac.New(sha256.New, []byte(s.activityLog.Secret))
	h.Write(data)
	if !hmac.Equal(h.Sum(nil), sig) || json.Unmarshal(data, &c) != nil || c.Member != member || c.Through > s.activityLog.Seq || c.Offset < 0 {
		return c, fmt.Errorf("动态快照已失效，请刷新")
	}
	return c, nil
}
func (s *Session) activityAvailableLocked(e ActivityEnvelope) bool {
	root := s.activityRoot()
	if e.Source.MaterialID != "" {
		for _, m := range root.record.Materials {
			if m.ID == e.Source.MaterialID {
				if e.Kind == "material_withdrawn" {
					return true
				}
				if m.WithdrawnAt != nil {
					return false
				}
				for _, v := range m.Versions {
					if v.Version == e.Source.Version {
						return true
					}
				}
			}
		}
		return false
	}
	if e.Source.ExecutionID != "" {
		if root.executions[e.Source.ExecutionID] == nil {
			return false
		}
	}
	return true
}
func activityUnread(log *ActivityLog, e ActivityEnvelope, member string) bool {
	r := log.Reads[member]
	return len(e.Recipients[member]) > 0 && e.Seq > r.Through && !r.Events[e.ID]
}
func activityMatches(e ActivityEnvelope, member, category string) bool {
	if category == "" || category == "all" {
		return true
	}
	for _, reason := range e.Recipients[member] {
		if category == "mentions" && reason == "mention" {
			return true
		}
		if category == "following" && strings.HasPrefix(reason, "follow_") {
			return true
		}
		if category == "replies" && (reason == "direct_reply" || reason == "own_turn_result" || reason == "own_request_result" || (e.MaterialAuthor == member && (e.Kind == "comment" || e.Kind == "reply"))) {
			return true
		}
	}
	return false
}
func (s *Session) activityPageLocked(member string, in ActivityInput) (ActivityPage, error) {
	if in.View == "" {
		in.View = "mine"
	}
	if in.View != "mine" && in.View != "space" {
		return ActivityPage{}, fmt.Errorf("动态范围无效")
	}
	if in.Category != "" && in.Category != "all" && in.Category != "mentions" && in.Category != "replies" && in.Category != "following" {
		return ActivityPage{}, fmt.Errorf("动态分类无效")
	}
	if in.Limit == 0 {
		in.Limit = 50
	}
	if in.Limit < 1 || in.Limit > 100 {
		return ActivityPage{}, fmt.Errorf("动态分页大小需要 1–100")
	}
	query := contentHash([]any{in.View, in.Category, in.Unread})
	cursor := activityCursor{Member: member, Through: s.activityLog.Seq, Query: query, ReadRevision: s.activityLog.Reads[member].Revision, Retention: s.activityLog.Retention}
	if in.Cursor != "" {
		var err error
		cursor, err = s.activityCursorLocked(in.Cursor, member)
		if err != nil || cursor.Query != query {
			return ActivityPage{}, fmt.Errorf("动态分页范围已变化，请刷新")
		}
		if cursor.Retention != s.activityLog.Retention || (in.Unread && cursor.ReadRevision != s.activityLog.Reads[member].Revision) {
			return ActivityPage{}, fmt.Errorf("动态保留范围或已读状态已变化，请刷新列表")
		}
	}
	page := ActivityPage{SpaceID: s.record.ID, Title: s.record.Title, SelfID: member, Groups: []ActivityGroup{}, Revision: s.activityLog.Seq, ReadRevision: s.activityLog.Reads[member].Revision, Error: s.activityError, SyncedAt: time.Now()}
	page.Snapshot = s.activityTokenLocked(activityCursor{Member: member, Through: cursor.Through})
	for _, child := range s.executions {
		if child.record.Activity != nil && child.record.Activity.SyncError != "" {
			page.Error = child.record.Activity.SyncError
		}
	}
	groups := map[string]*ActivityGroup{}
	unread := map[string]bool{}
	for _, e := range s.activityLog.Events {
		if !s.activityAvailableLocked(e) {
			continue
		}
		isUnread := activityUnread(s.activityLog, e, member)
		if isUnread {
			unread[e.Group] = true
		}
		if e.Seq > cursor.Through || (in.View != "space" && len(e.Recipients[member]) == 0) || !activityMatches(e, member, in.Category) || (in.Unread && !isUnread) {
			continue
		}
		item := ActivityItem{ID: e.ID, Seq: e.Seq, Kind: e.Kind, Source: e.Source, Subject: e.Subject, Actor: e.Actor, CreatedAt: e.CreatedAt, Preview: e.Preview, Reasons: append([]string{}, e.Recipients[member]...), Unread: isUnread}
		g := groups[e.Group]
		if g == nil {
			g = &ActivityGroup{ID: e.Group, Events: []ActivityItem{}}
			groups[e.Group] = g
		}
		g.Count++
		if isUnread {
			g.Unread++
		}
		if item.Seq >= g.Latest.Seq {
			g.Latest = item
		}
		if item.Unread && (g.Attention == nil || activityPriority(item) >= activityPriority(*g.Attention)) {
			selected := item
			g.Attention = &selected
		}
		g.Events = append(g.Events, item)
		if len(g.Events) > 20 {
			g.Events = g.Events[len(g.Events)-20:]
		}
	}
	page.Unread = len(unread)
	for _, g := range groups {
		page.Groups = append(page.Groups, *g)
	}
	sort.Slice(page.Groups, func(i, j int) bool { return page.Groups[i].Latest.Seq > page.Groups[j].Latest.Seq })
	if cursor.Offset > len(page.Groups) {
		cursor.Offset = len(page.Groups)
	}
	end := min(cursor.Offset+in.Limit, len(page.Groups))
	if end < len(page.Groups) {
		next := cursor
		next.Offset = end
		page.Next = s.activityTokenLocked(next)
	}
	page.Groups = page.Groups[cursor.Offset:end]
	// Keep a complete response bounded even when many groups contain long previews.
	for len(page.Groups) > 1 {
		encoded, _ := json.Marshal(page)
		if len(encoded) <= 64<<10 {
			break
		}
		end--
		page.Groups = page.Groups[:len(page.Groups)-1]
		next := cursor
		next.Offset = end
		page.Next = s.activityTokenLocked(next)
	}
	if len(page.Groups) == 1 {
		for {
			encoded, _ := json.Marshal(page)
			if len(encoded) <= 64<<10 {
				break
			}
			group := &page.Groups[0]
			if len(group.Events) == 0 {
				return ActivityPage{}, fmt.Errorf("动态来源元数据超过单页预算，请从原空间读取")
			}
			group.Events = group.Events[1:]
		}
	}
	return page, nil
}

func activityPriority(item ActivityItem) int {
	priority := 0
	for _, reason := range item.Reasons {
		switch reason {
		case "mention":
			priority = max(priority, 3)
		case "direct_reply":
			priority = max(priority, 2)
		case "own_turn_result", "own_request_result":
			priority = max(priority, 1)
		}
	}
	return priority
}
func (s *Session) activityOperation(ctx context.Context, op string, in ActivityInput) (any, error) {
	if _, bound := ctx.Value(runtimeAnnotationKey{}).(string); bound {
		return nil, fmt.Errorf("运行时不能访问成员个人动态")
	}
	s = s.activityRoot()
	if op == "follow" && in.Subject.Kind == "execution" && (in.Mode == "following" || in.Mode == "unmute") {
		s.mu.Lock()
		target, err := s.activitySubjectSessionLocked(in.Subject, true)
		valid := s.callerValidLocked(ctx)
		s.mu.Unlock()
		if !valid {
			return nil, fmt.Errorf("成员动态访问已失效")
		}
		if err != nil {
			return nil, err
		}
		check, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = target.syncActivityHistory(check)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("需先确认会话历史边界，关注设置尚未保存：%w", err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || !s.callerValidLocked(ctx) || s.record.State == "unsupported" {
		return nil, fmt.Errorf("成员动态访问已失效")
	}
	member := sharing.MemberID(ctx)
	if err := s.drainActivityLocked(); err != nil {
		s.activityError = err.Error()
		if s.activityLog == nil {
			return nil, err
		}
	}
	switch op {
	case "list":
		return s.activityPageLocked(member, in)
	case "members":
		members := []map[string]any{{"id": "owner", "name": s.app.Host, "online": true}}
		for _, m := range s.membersLocked() {
			if m.Active {
				members = append(members, map[string]any{"id": m.ID, "name": m.Name, "online": m.Online})
			}
		}
		return members, nil
	case "follows":
		if in.Subject.ID != "" {
			_, err := s.activitySubjectSessionLocked(in.Subject, true)
			if err != nil {
				return nil, err
			}
			p, ok := s.activityPreferenceLocked(member, in.Subject)
			if !ok {
				p = MemberFollowPreference{Subject: in.Subject, Mode: "direct_only"}
			}
			mode, kind := s.activityEffectiveLocked(member, in.Subject)
			return []FollowView{{MemberFollowPreference: p, EffectiveMode: mode, InheritedFrom: kind, Available: true, Title: s.activitySubjectTitleLocked(in.Subject)}}, nil
		}
		result := []FollowView{}
		records := []*Session{s}
		for _, child := range s.executions {
			records = append(records, child)
		}
		for _, record := range records {
			if record.record.Activity == nil {
				continue
			}
			for _, p := range record.record.Activity.Preferences[member] {
				mode, kind := s.activityEffectiveLocked(member, p.Subject)
				_, err := s.activitySubjectSessionLocked(p.Subject, true)
				result = append(result, FollowView{MemberFollowPreference: p, EffectiveMode: mode, InheritedFrom: kind, Available: err == nil, Title: s.activitySubjectTitleLocked(p.Subject)})
			}
		}
		sort.Slice(result, func(i, j int) bool {
			if result[i].UpdatedAt.Equal(result[j].UpdatedAt) {
				return subjectKey(result[i].Subject) < subjectKey(result[j].Subject)
			}
			return result[i].UpdatedAt.After(result[j].UpdatedAt)
		})
		if in.Limit == 0 {
			in.Limit = 50
		}
		if in.Limit < 1 || in.Limit > 100 {
			return nil, fmt.Errorf("关注分页大小需要 1–100")
		}
		query := "follows:" + contentHash(result)
		cursor := activityCursor{Member: member, Through: s.activityLog.Seq, Query: query}
		if in.Cursor != "" {
			var err error
			cursor, err = s.activityCursorLocked(in.Cursor, member)
			if err != nil || cursor.Query != query || cursor.Offset > len(result) {
				return nil, fmt.Errorf("关注清单已变化，请刷新")
			}
		}
		end := min(cursor.Offset+in.Limit, len(result))
		page := FollowPage{Items: result[cursor.Offset:end], Total: len(result)}
		for _, p := range result {
			if p.Mode == "following" {
				page.Following++
			}
		}
		if end < len(result) {
			cursor.Offset = end
			page.Next = s.activityTokenLocked(cursor)
		}
		return page, nil
	case "follow":
		target, err := s.activitySubjectSessionLocked(in.Subject, true)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.RequestID) == "" || len(in.RequestID) > 200 {
			return nil, fmt.Errorf("关注设置需要唯一 requestId")
		}
		if in.Mode != "following" && in.Mode != "direct_only" && in.Mode != "muted" && in.Mode != "unmute" {
			return nil, fmt.Errorf("关注模式无效")
		}
		a := target.activityStateLocked()
		key := subjectKey(in.Subject)
		command := member + ":" + in.RequestID
		hash := contentHash([]any{in.Subject, in.Mode, in.Revision})
		if old, ok := a.FollowCommands[command]; ok {
			if old.Hash != hash {
				return nil, fmt.Errorf("requestId 已用于不同关注设置")
			}
			return old.Preference, nil
		}
		current := a.Preferences[member][key]
		if current.Revision != in.Revision {
			return nil, fmt.Errorf("关注设置已变化，请刷新后重试")
		}
		if len(a.Preferences[member]) >= maxFollowPreferences && current.Revision == 0 {
			return nil, fmt.Errorf("关注对象数量已达上限")
		}
		if len(a.FollowCommands) >= 50000 {
			return nil, fmt.Errorf("关注操作记录已达上限")
		}
		old := target.record.Activity
		target.record.Activity = activityClone(old)
		a = target.activityStateLocked()
		if a.Preferences[member] == nil {
			a.Preferences[member] = map[string]MemberFollowPreference{}
		}
		current.Subject = in.Subject
		current.Origin = "manual"
		current.Revision++
		current.UpdatedAt = time.Now()
		if in.Mode == "muted" {
			if current.Mode != "muted" {
				current.PreviousMode = current.Mode
				if current.PreviousMode == "" {
					current.PreviousMode, _ = s.activityEffectiveLocked(member, in.Subject)
				}
			}
		} else if in.Mode == "unmute" {
			if current.Mode != "muted" {
				target.record.Activity = old
				return nil, fmt.Errorf("对象尚未静音")
			}
			in.Mode = current.PreviousMode
			if in.Mode == "" {
				in.Mode = "direct_only"
			}
		}
		current.Mode = in.Mode
		a.Preferences[member][key] = current
		a.FollowCommands[command] = FollowReceipt{Hash: hash, Preference: current}
		if err := target.saveLocked(); err != nil {
			target.record.Activity = old
			return nil, err
		}
		return current, nil
	case "read":
		if len(in.EventIDs) > 500 {
			return nil, fmt.Errorf("每次最多确认 500 条动态")
		}
		if in.VisibleSource != nil {
			ref := in.VisibleSource
			if ref.SpaceID != s.record.ID || ref.AnnotationID == "" {
				return nil, fmt.Errorf("阅读确认需要当前空间的准确讨论来源")
			}
			for _, e := range s.activityLog.Events {
				if e.Source.AnnotationID == ref.AnnotationID && e.Source.ReplyID == ref.ReplyID && e.Source.ExecutionID == ref.ExecutionID && len(e.Recipients[member]) > 0 && s.activityAvailableLocked(e) {
					in.EventIDs = append(in.EventIDs, e.ID)
				}
			}
			if len(in.EventIDs) == 0 {
				return s.activityPageLocked(member, ActivityInput{})
			}
		}
		if in.Snapshot == "" && len(in.EventIDs) == 0 {
			return nil, fmt.Errorf("请选择要标为已读的动态")
		}
		next := activityClone(*s.activityLog)
		read := next.Reads[member]
		if read.Events == nil {
			read.Events = map[string]bool{}
		}
		if in.Snapshot != "" {
			cursor, err := s.activityCursorLocked(in.Snapshot, member)
			if err != nil {
				return nil, err
			}
			if in.Group == "" {
				read.Through = max(read.Through, cursor.Through)
			} else {
				for _, e := range next.Events {
					if e.Group == in.Group && e.Seq <= cursor.Through && len(e.Recipients[member]) > 0 && s.activityAvailableLocked(e) {
						read.Events[e.ID] = true
					}
				}
			}
		}
		for _, id := range in.EventIDs {
			found := false
			for _, e := range next.Events {
				if e.ID == id && len(e.Recipients[member]) > 0 && s.activityAvailableLocked(e) {
					read.Events[id] = true
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("动态不属于当前成员或来源已不可访问")
			}
		}
		for _, e := range next.Events {
			if e.Seq <= read.Through {
				delete(read.Events, e.ID)
			}
		}
		read.Revision++
		next.Reads[member] = read
		if data, err := json.Marshal(next); err != nil || len(data) > maxActivityLogBytes {
			return nil, fmt.Errorf("已读状态超过动态存储预算，记录尚未修改")
		}
		if err := writeJSONFile(s.runtimeDirectory()+"/member-activity.json", next); err != nil {
			return nil, err
		}
		s.activityLog = &next
		return s.activityPageLocked(member, ActivityInput{})
	}
	return nil, fmt.Errorf("成员动态操作不受支持")
}
func (s *Session) activitySubjectTitleLocked(subject ActivitySubject) string {
	target, err := s.activitySubjectSessionLocked(subject, false)
	if err != nil {
		return "来源已不可用"
	}
	switch subject.Kind {
	case "material":
		for _, m := range target.record.Materials {
			if m.ID == subject.ID && len(m.Versions) > 0 {
				return activityPreview(m.Versions[len(m.Versions)-1].Title)
			}
		}
	case "discussion":
		for _, n := range target.record.Annotations {
			if n.ID == subject.ID {
				return activityPreview(n.Text)
			}
		}
	case "execution":
		return target.record.Title
	}
	return subject.ID
}
func (a *App) memberActivityCall(ctx context.Context, id, op string, in ActivityInput) (any, error) {
	a.mu.Lock()
	s, j, executionID := a.targetLocked(id)
	a.mu.Unlock()
	if executionID != "" {
		return nil, fmt.Errorf("成员动态需要空间 ID")
	}
	if j != nil {
		return a.remoteMemberActivity(ctx, j, op, in)
	}
	if s == nil {
		return nil, fmt.Errorf("空间不存在")
	}
	return s.activityOperation(ctx, op, in)
}
func (a *App) activityHTTP(w http.ResponseWriter, r *http.Request, path string) bool {
	if path == "activity" && r.Method == "GET" {
		a.mu.Lock()
		ids := []string{}
		for id := range a.sessions {
			ids = append(ids, id)
		}
		for id := range a.joined {
			ids = append(ids, id)
		}
		a.mu.Unlock()
		sort.Strings(ids)
		pages := make([]ActivityPage, len(ids))
		var wg sync.WaitGroup
		slots := make(chan struct{}, 8)
		for index, id := range ids {
			wg.Add(1)
			go func(index int, id string) {
				defer wg.Done()
				slots <- struct{}{}
				defer func() { <-slots }()
				ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
				defer cancel()
				out, err := a.memberActivityCall(ctx, id, "list", activityInputURL(r))
				if err != nil {
					pages[index] = ActivityPage{SpaceID: id, Error: err.Error(), Groups: []ActivityGroup{}}
					return
				}
				b, _ := json.Marshal(out)
				_ = json.Unmarshal(b, &pages[index])
			}(index, id)
		}
		wg.Wait()
		respond(w, map[string]any{"spaces": pages}, nil)
		return true
	}
	parts := strings.Split(path, "/")
	if len(parts) < 3 || parts[0] != "collaborations" {
		return false
	}
	op := ""
	write := false
	if len(parts) == 3 {
		switch parts[2] {
		case "activity":
			op = "list"
		case "follows":
			op = "follows"
		case "mention-members":
			op = "members"
		case "follow-preferences":
			op = "follow"
			write = true
		}
	}
	if len(parts) == 4 && parts[2] == "activity" && parts[3] == "read" {
		op = "read"
		write = true
	}
	if op == "" {
		return false
	}
	if (!write && r.Method != "GET") || (write && r.Method != "POST" && !(op == "follow" && r.Method == "PUT")) {
		http.NotFound(w, r)
		return true
	}
	in := activityInputURL(r)
	if write && !decode(w, r, &in) {
		return true
	}
	out, err := a.memberActivityCall(r.Context(), parts[1], op, in)
	respond(w, out, err)
	return true
}
func activityInputURL(r *http.Request) ActivityInput {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	return ActivityInput{View: q.Get("view"), Category: q.Get("category"), Unread: q.Get("unread") == "true", Cursor: q.Get("cursor"), Limit: limit, Subject: ActivitySubject{Kind: q.Get("kind"), ID: q.Get("subjectId"), ExecutionID: q.Get("executionId")}}
}
func (s *Session) remoteActivityHTTP(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/v2/member-activity/") {
		return false
	}
	if r.Method != "POST" {
		http.NotFound(w, r)
		return true
	}
	var in ActivityInput
	if !decode(w, r, &in) {
		return true
	}
	out, err := s.activityOperation(r.Context(), strings.TrimPrefix(r.URL.Path, "/v2/member-activity/"), in)
	respond(w, out, err)
	return true
}
