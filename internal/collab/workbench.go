package collab

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/readview"
	"teamcross/internal/sharing"
	"teamcross/internal/spacecontext"
)

// Workbench data is explicitly space-public. Local pairings, native histories,
// receiver credentials and personal requests never enter this record.
type WorkbenchRecord struct {
	InFlow        map[string]InFlowPreference `json:"inFlow,omitempty"`
	Notes         map[string]SpaceNote        `json:"notes,omitempty"`
	Targets       map[string]SpaceTarget      `json:"targets"`
	Requests      map[string]SpaceRequest     `json:"requests"`
	Brief         SpaceBrief                  `json:"brief"`
	BriefHistory  map[int]SpaceBrief          `json:"briefHistory,omitempty"`
	BriefCommands map[string]BriefCommand     `json:"briefCommands,omitempty"`
}

type SpaceActor = spacecontext.Actor

type SpaceTarget struct {
	InFlow      bool      `json:"inFlow"`
	Session     string    `json:"session,omitempty"`
	ID          string    `json:"id"`
	MemberID    string    `json:"memberId"`
	Member      string    `json:"member"`
	Name        string    `json:"name"`
	Provider    string    `json:"provider"`
	ExecutionID string    `json:"executionId,omitempty"`
	Removed     bool      `json:"removed,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	Available   bool      `json:"available"`
	Reason      string    `json:"reason,omitempty"`
}

type SpaceRequest struct {
	ID              string               `json:"id"`
	TargetID        string               `json:"targetId"`
	Actor           SpaceActor           `json:"actor"`
	ParentRequestID string               `json:"parentRequestId,omitempty"`
	References      []LibraryReference   `json:"references"`
	Instruction     string               `json:"instruction"`
	Intent          string               `json:"intent"`
	State           string               `json:"state"`
	Summary         string               `json:"summary,omitempty"`
	Error           string               `json:"error,omitempty"`
	CreatedAt       time.Time            `json:"createdAt"`
	UpdatedAt       time.Time            `json:"updatedAt"`
	ReceivedAt      *time.Time           `json:"receivedAt,omitempty"`
	FinishedAt      *time.Time           `json:"finishedAt,omitempty"`
	Context         *SpaceRequestContext `json:"context,omitempty"`
	BriefRevision   int                  `json:"briefRevision"`
}

// Captured by the host when a request is created, never supplied by the caller.
// Full snapshots are returned for one request, not repeated in list responses.
type SpaceRequestContext struct {
	Brief                 SpaceBrief          `json:"brief"`
	PreviousBriefRevision *int                `json:"previousBriefRevision,omitempty"`
	Parent                *SpaceRequestResult `json:"parent,omitempty"`
}

type SpaceRequestResult struct {
	RequestID   string `json:"requestId"`
	State       string `json:"state"`
	Instruction string `json:"instruction"`
	Summary     string `json:"summary,omitempty"`
}

type BriefItem struct {
	ID        string           `json:"itemId"`
	Text      string           `json:"text"`
	Status    string           `json:"status"`
	Sources   []SpaceSourceRef `json:"sources"`
	RequestID string           `json:"requestId,omitempty"`
	UpdatedBy SpaceActor       `json:"updatedBy"`
	UpdatedAt time.Time        `json:"updatedAt"`
}

type SpaceBrief struct {
	Revision  int         `json:"revision"`
	Topic     string      `json:"topic"`
	Decisions []BriefItem `json:"decisions"`
	Questions []BriefItem `json:"questions"`
	Progress  []BriefItem `json:"progress"`
	UpdatedBy SpaceActor  `json:"updatedBy"`
	UpdatedAt time.Time   `json:"updatedAt"`
}

// The local HTTP and MCP edges set Actor themselves; the space host supplies
// MemberID from its authenticated grant, including for another member's Core.
type workbenchInput struct {
	Enabled         bool                 `json:"enabled"`
	Receipt         string               `json:"receipt,omitempty"`
	Session         string               `json:"session,omitempty"`
	Read            HistoryRead          `json:"read"`
	Reply           AnnotationReplyInput `json:"reply"`
	RequestID       string               `json:"requestId,omitempty"`
	TargetID        string               `json:"targetId,omitempty"`
	PairingID       string               `json:"pairingId,omitempty"`
	Name            string               `json:"name,omitempty"`
	Provider        string               `json:"provider,omitempty"`
	ExecutionID     string               `json:"executionId,omitempty"`
	Actor           SpaceActor           `json:"actor"`
	ParentRequestID string               `json:"parentRequestId,omitempty"`
	References      []LibraryReference   `json:"references,omitempty"`
	Instruction     string               `json:"instruction,omitempty"`
	Intent          string               `json:"intent,omitempty"`
	State           string               `json:"state,omitempty"`
	Summary         string               `json:"summary,omitempty"`
	Error           string               `json:"error,omitempty"`
	BaseRevision    int                  `json:"baseRevision,omitempty"`
	Brief           SpaceBrief           `json:"brief"`
	Topic           *string              `json:"topic,omitempty"`
	Changes         []BriefChange        `json:"changes,omitempty"`
	Revision        int                  `json:"revision,omitempty"`
	ItemID          string               `json:"itemId,omitempty"`
	Presence        map[string]string    `json:"presence,omitempty"`
	TargetNames     map[string]string    `json:"targetNames,omitempty"`
	Offset          int                  `json:"offset,omitempty"`
}

type WorkbenchView struct {
	InFlow   map[string]bool `json:"inFlow"`
	Notes    []SpaceNote     `json:"notes"`
	SpaceID  string          `json:"spaceId"`
	SelfID   string          `json:"selfId"`
	Targets  []SpaceTarget   `json:"targets"`
	Requests []SpaceRequest  `json:"requests"`
	Total    int             `json:"total"`
	Next     *int            `json:"nextOffset,omitempty"`
	Brief    SpaceBrief      `json:"brief"`
}

type targetPresence struct {
	At     time.Time
	Reason string
}

func newWorkbench() *WorkbenchRecord {
	return &WorkbenchRecord{Targets: map[string]SpaceTarget{}, Requests: map[string]SpaceRequest{}, Brief: SpaceBrief{Decisions: []BriefItem{}, Questions: []BriefItem{}}}
}

func (s *Session) workbenchActor(ctx context.Context, actor SpaceActor) SpaceActor {
	if shadowActor, ok := collaborationShadowActor(ctx); ok {
		return shadowActor
	}
	actor.MemberID, actor.Name = sharing.MemberID(ctx), sharing.MemberName(ctx)
	if actor.Kind != "session" {
		actor.Kind, actor.Provider, actor.Session = "human", "", ""
	}
	return actor
}

func (s *Session) workbenchRefsLocked(refs []LibraryReference) error {
	if len(refs) > 32 {
		return fmt.Errorf("最多选择 32 项已发布材料或空间批注")
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		if err := ref.validate(); err != nil {
			return err
		}
		if ref.SpaceID != s.record.ID || ref.Kind == "context" || ref.ExecutionID != "" {
			return fmt.Errorf("空间请求只能引用本空间已发布的材料和批注")
		}
		if seen[libraryKey(ref)] {
			return fmt.Errorf("选择中有重复引用")
		}
		seen[libraryKey(ref)] = true
		if ref.Kind == "material" {
			if err := s.validateReferencesLocked([]MaterialReference{{MaterialID: ref.MaterialID, Version: ref.Version}}); err != nil {
				return err
			}
		} else {
			found := false
			for _, note := range s.record.Annotations {
				if note.ID == ref.AnnotationID && (note.Target == nil || note.Target.Kind == "material") {
					found = true
					if err := s.validateReferencesLocked(note.Materials); err != nil {
						return err
					}
					if note.Target != nil {
						if err := s.validateReferencesLocked([]MaterialReference{{MaterialID: note.Target.MaterialID, Version: note.Target.Version}}); err != nil {
							return err
						}
					}
				}
			}
			if !found {
				return fmt.Errorf("批注不存在或包含未公开的执行上下文")
			}
		}
	}
	return nil
}

func (s *Session) spaceTargetLocked(t SpaceTarget) SpaceTarget {
	t.InFlow = s.record.Workbench.InFlow[inFlowRecipient(t.ID, t.ExecutionID)].Enabled && !t.Removed
	t.Available, t.Reason = false, "接收端未连接"
	if p, ok := s.workbenchPresence[t.ID]; ok && time.Since(p.At) < 20*time.Second {
		t.Reason, t.Available = p.Reason, p.Reason == ""
	}
	if t.Removed {
		t.Available, t.Reason = false, "已解除关联"
	}
	if t.MemberID != "owner" && (s.sharedRuntime() == nil || !s.sharedRuntime().HasMember(t.MemberID)) {
		t.Available, t.Reason = false, "成员已离开或共享已结束"
	}
	return t
}

func (s *Session) workbenchViewLocked(ctx context.Context, offset int) WorkbenchView {
	b := s.record.Workbench
	out := WorkbenchView{SpaceID: s.record.ID, SelfID: sharing.MemberID(ctx), Targets: []SpaceTarget{}, Requests: []SpaceRequest{}, Brief: b.Brief, Total: len(b.Requests)}
	out.InFlow = map[string]bool{}
	for key, pref := range b.InFlow {
		out.InFlow[key] = pref.Enabled
	}
	out.Notes = []SpaceNote{}
	for _, note := range b.Notes {
		out.Notes = append(out.Notes, note)
	}
	sort.Slice(out.Notes, func(i, j int) bool { return out.Notes[i].CreatedAt.After(out.Notes[j].CreatedAt) })
	out.Notes = out.Notes[:min(40, len(out.Notes))]
	for _, target := range b.Targets {
		out.Targets = append(out.Targets, s.spaceTargetLocked(target))
	}
	sort.Slice(out.Targets, func(i, j int) bool { return out.Targets[i].CreatedAt.Before(out.Targets[j].CreatedAt) })
	for _, request := range b.Requests {
		request.Context = nil
		out.Requests = append(out.Requests, request)
	}
	sort.Slice(out.Requests, func(i, j int) bool { return out.Requests[i].CreatedAt.After(out.Requests[j].CreatedAt) })
	if offset > len(out.Requests) {
		offset = len(out.Requests)
	}
	end := min(offset+40, len(out.Requests))
	if end < len(out.Requests) {
		out.Next = &end
	}
	out.Requests = out.Requests[offset:end]
	return libraryClone(out)
}

// All space mutations are serialized with membership/revision checks and the
// durable record. No native input or network IO occurs while this lock is held.
func (s *Session) workbenchOperation(ctx context.Context, op string, in workbenchInput) (out any, err error) {
	if op == "reply" {
		return s.workbenchReply(ctx, in)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.record.State == "unsupported" || s.space != nil || !s.callerValidLocked(ctx) {
		return nil, fmt.Errorf("共享已结束或成员已移除")
	}
	if s.record.State == "ended" && op != "view" && op != "brief-read" && op != "request" && op != "annotations" && op != "poll" && op != "receipt" && op != "finish" && op != "connection-status" {
		return nil, fmt.Errorf("空间已关闭，请重新开放后发起新的协作")
	}
	if in.Offset < 0 {
		return nil, fmt.Errorf("分页位置无效")
	}
	if s.record.Workbench == nil {
		s.record.Workbench = newWorkbench()
	}
	previous := libraryClone(s.record.Workbench)
	updated := s.record.UpdatedAt
	changed := false
	defer func() {
		if err == nil && changed {
			s.record.UpdatedAt = time.Now()
			err = s.saveLocked()
		}
		if err != nil {
			s.record.Workbench, s.record.UpdatedAt = previous, updated
		}
		// Never let callers alias a record after the lock is released.
		if err == nil {
			out = libraryClone[any](out)
		}
	}()
	b := s.record.Workbench
	b.Brief = briefWithIDs(b.Brief)
	actor := s.workbenchActor(ctx, in.Actor)
	if actor.Kind == "session" && ((actor.Provider != "codex" && actor.Provider != "claude" && actor.Provider != "chatgpt") || len(actor.Session) != 24) {
		return nil, fmt.Errorf("参与会话身份无效")
	}
	now := time.Now()
	member := actor.MemberID
	target := b.Targets[in.TargetID]
	request, exists := b.Requests[in.RequestID]
	owner := member == "owner" && actor.Kind == "human"
	if op == "connection-status" {
		if actor.Kind != "human" || target.ID == "" || target.MemberID != member || target.Removed {
			return SpaceTarget{}, nil
		}
		return s.spaceTargetLocked(target), nil
	}
	if strings.HasPrefix(op, "inflow-") {
		var mutation bool
		out, mutation, err = s.inFlowOperationLocked(ctx, op, in, actor)
		changed = changed || mutation
		return out, err
	}
	for id, r := range b.Requests {
		if r.State == "queued" && (now.Sub(r.CreatedAt) > 30*time.Second || s.record.State == "ended") {
			r.State, r.Error, r.UpdatedAt = "failed", "接收端未及时接手或空间已关闭；请核对后明确新建请求", now
			b.Requests[id], changed = r, true
		}
	}
	request, exists = b.Requests[in.RequestID]
	switch op {
	case "brief-read":
		return s.readBriefLocked(in)
	case "brief-update":
		var mutation bool
		out, mutation, err = s.updateBriefLocked(in, actor, now, false)
		changed = changed || mutation
		return out, err
	case "receiver-check":
		return map[string]bool{"allowed": true}, nil
	case "event-snapshot":
		return s.spaceEventSnapshotLocked(), nil
	case "annotations":
		notes := visibleAnnotations(s.record.Annotations, false)
		if in.Read.AnnotationID != "" {
			selected := []Annotation{}
			for _, note := range notes {
				if note.ID == in.Read.AnnotationID {
					selected = append(selected, note)
				}
			}
			notes = selected
		}
		return agentAnnotations(s.record.ID, notes, in.Read)
	case "view":
		return s.workbenchViewLocked(ctx, in.Offset), nil
	case "request":
		if !exists {
			return nil, fmt.Errorf("没有找到空间请求")
		}
		return request, nil
	case "register":
		if actor.Kind != "human" {
			return nil, fmt.Errorf("请由成员明确关联接收会话")
		}
		if _, e := uuid.Parse(in.TargetID); e != nil || strings.TrimSpace(in.Name) == "" || len([]rune(in.Name)) > 80 || len(in.Session) != 24 || (in.Provider != "codex" && in.Provider != "claude" && in.Provider != "chatgpt") {
			return nil, fmt.Errorf("接收会话信息无效")
		}
		if target.ID != "" {
			if target.MemberID != member || target.Provider != in.Provider || target.ExecutionID != in.ExecutionID || target.Session != in.Session {
				return nil, fmt.Errorf("关联标识已使用，请重新关联")
			}
			if target.Removed || target.Name != in.Name {
				target.Removed = false
				target.Name = in.Name
				b.Targets[target.ID], changed = target, true
			}
			return target, nil
		}
		if len(b.Targets) >= 256 {
			return nil, fmt.Errorf("空间接收会话已达上限")
		}
		if in.ExecutionID != "" && (member != "owner" || s.executions[in.ExecutionID] == nil) {
			return nil, fmt.Errorf("共同执行会话只能由空间发起者关联")
		}
		target = SpaceTarget{ID: in.TargetID, MemberID: member, Member: actor.Name, Name: in.Name, Provider: in.Provider, ExecutionID: in.ExecutionID, CreatedAt: now, Session: in.Session}
		b.Targets[target.ID], changed = target, true
		return target, nil
	case "remove":
		if target.ID == "" || (target.MemberID != member && !owner) || actor.Kind != "human" {
			return nil, fmt.Errorf("只能解除自己关联的会话，或由发起者移除")
		}
		target.Removed, target.Available = true, false
		b.Targets[target.ID], changed = target, true
		// Removing a personal association revokes its opt-in. An execution's
		// independent comment opt-in survives removal of an optional alias.
		if target.ExecutionID == "" {
			key := inFlowRecipient(target.ID, "")
			if pref, ok := b.InFlow[key]; ok {
				pref.Enabled = false
				b.InFlow[key] = pref
			}
			for token, lease := range s.inFlowLeases {
				if lease.Recipient == key {
					delete(s.inFlowLeases, token)
				}
			}
		}
		for id, r := range b.Requests {
			if r.TargetID == target.ID && r.State == "queued" {
				r.State, r.Error, r.UpdatedAt = "cancelled", "接收会话已解除关联", now
				b.Requests[id] = r
			}
		}
		return target, nil
	case "poll":
		if s.workbenchPresence == nil {
			s.workbenchPresence = map[string]targetPresence{}
		}
		for id, reason := range in.Presence {
			t := b.Targets[id]
			if t.ID != "" && t.MemberID == member && !t.Removed {
				s.workbenchPresence[id] = targetPresence{At: now, Reason: shortText(reason, 300)}
				if name := strings.TrimSpace(in.TargetNames[id]); actor.Kind == "human" && name != "" && len([]rune(name)) <= 80 && name != t.Name {
					t.Name = name
					b.Targets[id], changed = t, true
				}
			}
		}
		pending := []SpaceRequest{}
		for id, r := range b.Requests {
			if r.State != "queued" {
				continue
			}
			if now.Sub(r.CreatedAt) > 30*time.Second {
				r.State, r.Error, r.UpdatedAt = "failed", "接收端未及时接手；请核对后明确新建请求", now
				b.Requests[id], changed = r, true
				continue
			}
			if t := b.Targets[r.TargetID]; t.MemberID == member && s.spaceTargetLocked(t).Available {
				pending = append(pending, r)
			}
		}
		sort.Slice(pending, func(i, j int) bool { return pending[i].CreatedAt.Before(pending[j].CreatedAt) })
		return pending, nil
	case "send":
		in.Instruction = strings.TrimSpace(in.Instruction)
		if _, e := uuid.Parse(in.RequestID); e != nil || in.Instruction == "" || len([]rune(in.Instruction)) > 4000 || (in.Intent != "analyze" && in.Intent != "analyze_reply") {
			return nil, fmt.Errorf("请提供唯一请求标识、处理方式和 1–4000 字处理要求")
		}
		if exists {
			if request.TargetID != in.TargetID || request.Actor != actor || request.Instruction != in.Instruction || request.Intent != in.Intent || request.ParentRequestID != in.ParentRequestID || contentHash(request.References) != contentHash(in.References) {
				return nil, fmt.Errorf("请求标识已用于其他内容")
			}
			return request, nil
		}
		if len(b.Requests) >= 4096 {
			return nil, fmt.Errorf("空间请求记录已达上限，请建立后续空间")
		}
		if target.ID == "" || !s.spaceTargetLocked(target).Available {
			return nil, fmt.Errorf("接收会话未就绪，请刷新会话状态")
		}
		if target.ExecutionID != "" && !s.executionWriterLocked(target.ExecutionID, member) {
			return nil, fmt.Errorf("向共同执行会话投递需要有效空间成员资格和该会话当前输入权")
		}
		if err = s.workbenchRefsLocked(in.References); err != nil {
			return nil, err
		}
		if in.Intent == "analyze_reply" {
			found := false
			for _, ref := range in.References {
				found = found || ref.Kind == "annotation"
			}
			if !found {
				return nil, fmt.Errorf("分析并回复需要原批注引用")
			}
		}
		if in.ParentRequestID != "" {
			parent, ok := b.Requests[in.ParentRequestID]
			if !ok {
				return nil, fmt.Errorf("关联请求不属于当前空间")
			}
			depth := 0
			for parent.ParentRequestID != "" {
				depth++
				parent = b.Requests[parent.ParentRequestID]
				if depth >= 16 {
					return nil, fmt.Errorf("关联请求层级过多，请由成员核对后发起新任务")
				}
			}
		}
		request = SpaceRequest{ID: in.RequestID, TargetID: target.ID, Actor: actor, ParentRequestID: in.ParentRequestID, References: in.References, Instruction: in.Instruction, Intent: in.Intent, State: "queued", CreatedAt: now, UpdatedAt: now, BriefRevision: b.Brief.Revision, Context: &SpaceRequestContext{Brief: libraryClone(b.Brief)}}
		var previous time.Time
		for _, r := range b.Requests {
			if r.TargetID == target.ID && r.ReceivedAt != nil && r.ReceivedAt.After(previous) && r.Context != nil {
				previous = *r.ReceivedAt
				revision := r.BriefRevision
				request.Context.PreviousBriefRevision = &revision
			}
		}
		if parent, ok := b.Requests[in.ParentRequestID]; ok {
			request.Context.Parent = &SpaceRequestResult{RequestID: parent.ID, State: parent.State, Instruction: parent.Instruction, Summary: parent.Summary}
		}
		// Reserve room for the later result, receipt metadata and read wrapper.
		// Refuse before delivery rather than create a request its recipient cannot read.
		if readview.WireSize(request) > readview.MaxBytes-16<<10 {
			return nil, fmt.Errorf("请求上下文过长，请精简处理要求或空间简报后重试")
		}
		b.Requests[request.ID], changed = request, true
		return request, nil
	case "claim", "receipt", "read", "finish":
		if !exists {
			return nil, fmt.Errorf("没有找到空间请求")
		}
		target = b.Targets[request.TargetID]
		if target.Removed || target.MemberID != member || target.ID != in.TargetID {
			return nil, fmt.Errorf("请求不属于当前接收成员或关联已移除")
		}
		if op == "claim" {
			if request.State != "queued" {
				return nil, fmt.Errorf("请求已接手，不能重复投递")
			}
			if target.ExecutionID != "" && !s.executionWriterLocked(target.ExecutionID, request.Actor.MemberID) {
				return nil, fmt.Errorf("投递前输入权已变化")
			}
			if err = s.workbenchRefsLocked(request.References); err != nil {
				return nil, err
			}
			// Persist uncertainty before an external write. Claim is deliberately
			// one-shot; losing its response never causes a native-input retry.
			request.State, request.Error = "unknown", "正在投递，等待接收端回执"
		} else if op == "receipt" {
			if in.State != "submitted" && in.State != "failed" && in.State != "unknown" {
				return nil, fmt.Errorf("投递状态无效")
			}
			if request.State == "unknown" {
				request.State, request.Error = in.State, shortText(in.Error, 500)
			}
		} else if op == "read" {
			if request.State == "queued" || request.State == "cancelled" {
				return nil, fmt.Errorf("请求尚未投递或已取消")
			}
			if err = s.workbenchRefsLocked(request.References); err != nil {
				return nil, err
			}
			if request.State == "submitted" || request.State == "unknown" {
				request.State, request.Error, request.ReceivedAt = "received", "", &now
			}
		} else {
			in.Summary = strings.TrimSpace(in.Summary)
			if (in.State != "completed" && in.State != "failed") || in.Summary == "" || len([]rune(in.Summary)) > 2000 {
				return nil, fmt.Errorf("请提供完成状态和 1–2000 字摘要")
			}
			if request.FinishedAt != nil {
				if request.State != in.State || request.Summary != in.Summary {
					return nil, fmt.Errorf("请求已有不同的结果")
				}
				return request, nil
			}
			if request.State != "received" {
				return nil, fmt.Errorf("请先读取本次请求")
			}
			request.State, request.Summary, request.FinishedAt = in.State, in.Summary, &now
		}
		request.UpdatedAt = now
		b.Requests[request.ID], changed = request, true
		return request, nil
	case "cancel":
		if !exists || (request.Actor.MemberID != member && !owner) || request.State != "queued" {
			return nil, fmt.Errorf("只能取消尚未投递且属于自己的请求，或由发起者取消")
		}
		request.State, request.UpdatedAt = "cancelled", now
		b.Requests[request.ID], changed = request, true
		return request, nil
	case "brief":
		var mutation bool
		out, mutation, err = s.updateBriefLocked(in, actor, now, true)
		changed = changed || mutation
		return out, err
	}
	return nil, fmt.Errorf("未知的空间工作台操作")
}

func (s *Session) workbenchReply(ctx context.Context, in workbenchInput) (any, error) {
	s.mu.Lock()
	if s.closed || !s.callerValidLocked(ctx) {
		s.mu.Unlock()
		return nil, fmt.Errorf("共享已结束或成员已移除")
	}
	err := s.workbenchRefsLocked([]LibraryReference{{SpaceID: s.record.ID, Kind: "annotation", AnnotationID: in.Reply.AnnotationID}})
	actor := s.workbenchActor(ctx, in.Actor)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if actor.Kind != "session" || (actor.Provider != "codex" && actor.Provider != "claude" && actor.Provider != "chatgpt") || len(actor.Session) != 24 {
		return nil, fmt.Errorf("参与会话身份无效")
	}
	ctx = context.WithValue(ctx, activityActorKey{}, actor)
	return s.replyAnnotationResult(ctx, in.Reply, actor.Provider+" · "+actor.Name, true)
}
