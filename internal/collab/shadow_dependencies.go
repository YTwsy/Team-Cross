package collab

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"teamcross/internal/shadow"
	"teamcross/internal/sharing"
	"teamcross/internal/spacecontext"
)

type shadowDependencyInput struct {
	ShadowID   string                `json:"shadowId"`
	Dependency shadow.TaskDependency `json:"dependency"`
	Candidate  *spacecontext.Signal  `json:"candidate,omitempty"`
}

func (a *App) readShadowDependency(ctx context.Context, shadowID string, dependency shadow.TaskDependency) (shadow.DependencyUpdate, error) {
	return a.queryShadowDependency(ctx, shadowDependencyInput{ShadowID: shadowID, Dependency: dependency})
}

func (a *App) resolveShadowDependency(ctx context.Context, shadowID string, dependency shadow.TaskDependency, signal spacecontext.Signal) (*shadow.DependencySignal, error) {
	out, err := a.queryShadowDependency(ctx, shadowDependencyInput{ShadowID: shadowID, Dependency: dependency, Candidate: &signal})
	if err != nil || len(out.Signals) == 0 {
		return nil, err
	}
	out.Signals[0].QuestionSource = out.QuestionSource
	return &out.Signals[0], nil
}

func (a *App) queryShadowDependency(ctx context.Context, in shadowDependencyInput) (shadow.DependencyUpdate, error) {
	a.mu.Lock()
	e, closed := a.shadows[in.ShadowID], a.closed
	a.mu.Unlock()
	if e == nil || closed {
		return shadow.DependencyUpdate{}, fmt.Errorf("分身运行记录不存在或 Core 已关闭")
	}
	_, spaceID, _, _ := e.Identity()
	if in.Dependency.Source.SpaceID != spaceID {
		return shadow.DependencyUpdate{}, fmt.Errorf("任务依赖不属于本分身空间")
	}
	a.mu.Lock()
	s, j := a.sessions[spaceID], a.joined[spaceID]
	a.mu.Unlock()
	if s != nil {
		return s.shadowDependencyOperation(ctx, in)
	}
	if j == nil {
		return shadow.DependencyUpdate{}, fmt.Errorf("任务依赖空间不存在")
	}
	j.mu.Lock()
	hostID := j.Invitation.ID
	j.mu.Unlock()
	var out shadow.DependencyUpdate
	if err := j.request(ctx, "POST", "/v2/shadow-dependency", remapWorkbench(in, spaceID, hostID), &out); err != nil {
		return out, err
	}
	return workbenchDecode[shadow.DependencyUpdate](remapWorkbench(out, hostID, spaceID)), nil
}

// Search only this member's durable jobs, including work retained by an older
// default shadow. No private state or signal is broadcast to other members.
func (a *App) routeShadowActivity(ctx context.Context, shadowID string, signal spacecontext.Signal) ([]shadow.TaskDelivery, error) {
	a.mu.Lock()
	caller := a.shadows[shadowID]
	engines := make([]*shadow.Engine, 0, len(a.shadows))
	for _, e := range a.shadows {
		engines = append(engines, e)
	}
	a.mu.Unlock()
	if caller == nil {
		return nil, fmt.Errorf("分身路线不存在")
	}
	spaceID, memberID := caller.MemberIdentity()
	if signal.Source.SpaceID != spaceID {
		return nil, fmt.Errorf("动态来源空间不符")
	}
	sort.Slice(engines, func(i, j int) bool {
		a, _, _, _ := engines[i].Identity()
		b, _, _, _ := engines[j].Identity()
		return a < b
	})
	out := []shadow.TaskDelivery{}
	for _, e := range engines {
		s, m := e.MemberIdentity()
		if s != spaceID || m != memberID {
			continue
		}
		deliveries, err := e.RouteDependencyActivity(ctx, signal)
		if err != nil {
			return nil, err
		}
		out = append(out, deliveries...)
	}
	return out, nil
}

func (a *App) shadowActivityRouteStamp(shadowID string) string {
	a.mu.Lock()
	caller := a.shadows[shadowID]
	engines := make([]*shadow.Engine, 0, len(a.shadows))
	for _, e := range a.shadows {
		engines = append(engines, e)
	}
	a.mu.Unlock()
	if caller == nil {
		return "missing"
	}
	space, member := caller.MemberIdentity()
	parts := []string{}
	for _, e := range engines {
		s, m := e.MemberIdentity()
		if s == space && m == member {
			id, _, _, _ := e.Identity()
			parts = append(parts, id+":"+e.DependencyRouteStamp())
		}
	}
	sort.Strings(parts)
	return contentHash(parts)
}

// The member's understanding store survives a default-shadow change. Include
// tasks still owned by their older shadows, without exposing another member's
// private task context or reading an unrelated space.
func (a *App) shadowContextUses(shadowID, focusID string) shadow.TaskContextUses {
	a.mu.Lock()
	caller := a.shadows[shadowID]
	engines := make([]*shadow.Engine, 0, len(a.shadows))
	for _, e := range a.shadows {
		engines = append(engines, e)
	}
	a.mu.Unlock()
	out := shadow.TaskContextUses{Items: []shadow.TaskContextUse{}}
	if caller == nil {
		return out
	}
	space, member := caller.MemberIdentity()
	sort.Slice(engines, func(i, j int) bool {
		a, _, _, _ := engines[i].Identity()
		b, _, _, _ := engines[j].Identity()
		return a < b
	})
	for _, e := range engines {
		s, m := e.MemberIdentity()
		if s != space || m != member {
			continue
		}
		uses := e.ContextUses(focusID)
		out.Total += uses.Total
		out.Items = append(out.Items, uses.Items[:min(len(uses.Items), 32-len(out.Items))]...)
	}
	return out
}

// Reads the exact business question, independent of recipient matching and
// human reads. In particular an owner's own answer still wakes their task.
func (s *Session) shadowDependencyOperation(ctx context.Context, in shadowDependencyInput) (shadow.DependencyUpdate, error) {
	d := in.Dependency
	out := shadow.DependencyUpdate{Cursor: d.Cursor, Signals: []shadow.DependencySignal{}}
	s.mu.Lock()
	registered, exists := s.record.Shadows[in.ShadowID]
	valid := s.space == nil && !s.closed && s.record.State != "ended" && s.record.State != "unsupported" && s.callerValidLocked(ctx) && exists && registered.View.Owner.ID == sharing.MemberID(ctx) && d.Source.SpaceID == s.record.ID
	target := s
	if d.Source.ExecutionID != "" {
		target = s.executions[d.Source.ExecutionID]
	}
	materials := map[string]bool{}
	for _, material := range s.record.Materials {
		materials[material.ID] = material.WithdrawnAt == nil
	}
	s.mu.Unlock()
	if !valid || target == nil {
		return out, fmt.Errorf("等待来源或分身成员身份无效")
	}
	if c := in.Candidate; c != nil && (c.Source.SpaceID != d.Source.SpaceID || c.Source.ExecutionID != d.Source.ExecutionID || !d.QuestionSourcePending && (c.Source.AnnotationID != d.Source.AnnotationID || c.Source.RequestID != d.Source.RequestID)) {
		return out, fmt.Errorf("答复候选与等待对象不匹配")
	}
	if d.Kind == "request" {
		if d.Source.RequestID == "" || d.Source.AnnotationID != "" || d.Source.ExecutionID != "" {
			return out, fmt.Errorf("请求依赖来源无效")
		}
		value, err := s.workbenchOperation(ctx, "request", workbenchInput{RequestID: d.Source.RequestID})
		if err != nil {
			return out, err
		}
		r := workbenchDecode[SpaceRequest](value)
		if r.State != "completed" && r.State != "failed" && r.State != "cancelled" && r.State != "unknown" {
			return out, nil
		}
		cursor := "request:" + r.ID + ":" + r.State + ":" + r.UpdatedAt.UTC().Format(time.RFC3339Nano)
		if cursor != d.Cursor || in.Candidate != nil {
			out.Cursor = cursor
			out.Signals = append(out.Signals, shadow.DependencySignal{ID: cursor, Source: d.Source, State: r.State, Preview: activityPreview(r.Summary)})
		}
		return out, nil
	}
	if d.Kind != "discussion" || d.Source.AnnotationID == "" && !d.QuestionSourcePending || d.Source.RequestID != "" {
		return out, fmt.Errorf("等待项需要准确讨论问题")
	}
	target.mu.Lock()
	defer target.mu.Unlock()
	if target.closed || !target.callerValidLocked(ctx) {
		return out, fmt.Errorf("等待来源访问已失效")
	}
	execution := sharing.ExecutionAuthorized(ctx, target.sharedRuntime(), target.record.ID)
	if target.space != nil && !execution {
		return out, fmt.Errorf("等待会话访问已失效")
	}
	if d.QuestionSourcePending {
		ref, found, err := target.shadowQuestionSourceLocked(in.ShadowID, sharing.MemberID(ctx), d, execution)
		if err != nil || !found {
			return out, err
		}
		d.Source, d.QuestionSourcePending = ref, false
		out.QuestionSource = &ref
		if c := in.Candidate; c != nil && c.Source.AnnotationID != ref.AnnotationID {
			return out, nil
		}
	}
	var note *Annotation
	for i := range target.record.Annotations {
		if target.record.Annotations[i].ID == d.Source.AnnotationID && annotationVisible(target.record.Annotations[i], execution) {
			note = &target.record.Annotations[i]
			break
		}
	}
	if note == nil {
		return out, fmt.Errorf("原问题不存在")
	}
	if note.Target != nil && note.Target.Kind == "material" {
		if d.Source.MaterialID != "" && d.Source.MaterialID != note.Target.MaterialID {
			return out, fmt.Errorf("问题与材料来源不匹配")
		}
		if !materials[note.Target.MaterialID] {
			if d.Cursor != "unavailable" {
				out.Cursor = "unavailable"
				out.Signals = []shadow.DependencySignal{{ID: "unavailable:" + note.ID, Source: d.Source, State: "unavailable", Preview: "材料已不可用，请核对任务依据"}}
			}
			return out, nil
		}
	}
	if candidate := in.Candidate; candidate != nil {
		if candidate.Kind != "reply" || candidate.Source.ReplyID == "" {
			return out, nil
		}
		descendants := map[string]bool{}
		if d.Source.ReplyID != "" {
			descendants[d.Source.ReplyID] = true
		}
		for _, reply := range note.Replies {
			root := reply.ReplyTo == nil || reply.ReplyTo.Kind == "annotation" && reply.ReplyTo.ID == note.ID
			matches := d.Source.ReplyID == "" && root || reply.ReplyTo != nil && reply.ReplyTo.Kind == "reply" && descendants[reply.ReplyTo.ID]
			if matches {
				descendants[reply.ID] = true
			}
			if reply.ID != candidate.Source.ReplyID {
				continue
			}
			if matches && !(reply.Actor.Kind == "shadow" && reply.Actor.ShadowID == in.ShadowID) {
				ref := d.Source
				ref.ReplyID = reply.ID
				out.Signals = []shadow.DependencySignal{{ID: "reply:" + reply.ID, Source: ref, Actor: reply.Actor, State: "answered", Preview: activityPreview(reply.Text)}}
			}
			return out, nil
		}
		return out, fmt.Errorf("候选答复已不存在")
	}
	start := 0
	if d.Source.ReplyID != "" {
		found := false
		for i, r := range note.Replies {
			if r.ID == d.Source.ReplyID {
				start = i + 1
				found = true
				break
			}
		}
		if !found {
			return out, fmt.Errorf("准确提问回复不存在")
		}
	}
	if d.Cursor != "" && d.Cursor != "unavailable" {
		position, err := strconv.Atoi(strings.TrimPrefix(d.Cursor, "reply:"))
		if err != nil || !strings.HasPrefix(d.Cursor, "reply:") || position < start || position > len(note.Replies) {
			return out, fmt.Errorf("问题回复位置无效，原记录已保留")
		}
		start = position
	}
	descendants := map[string]bool{}
	if d.Source.ReplyID != "" {
		descendants[d.Source.ReplyID] = true
	}
	through := start
	for i, r := range note.Replies {
		parentRoot := r.ReplyTo == nil || r.ReplyTo.Kind == "annotation" && r.ReplyTo.ID == note.ID
		matches := d.Source.ReplyID == "" && parentRoot || r.ReplyTo != nil && r.ReplyTo.Kind == "reply" && descendants[r.ReplyTo.ID]
		if matches {
			descendants[r.ID] = true
		}
		if i < start {
			continue
		}
		if i-start >= 1000 || len(out.Signals) >= 16 {
			break
		}
		through = i + 1
		if !matches || r.Actor.Kind == "shadow" && r.Actor.ShadowID == in.ShadowID {
			continue
		}
		ref := d.Source
		ref.ReplyID = r.ID
		out.Signals = append(out.Signals, shadow.DependencySignal{ID: "reply:" + r.ID, Source: ref, Actor: r.Actor, State: "answered", Preview: activityPreview(r.Text)})
	}
	out.Cursor = "reply:" + strconv.Itoa(through)
	return out, nil
}

// The caller has already proved member ownership of the registered shadow.
// Resolve only its persisted command ID and actual actor; a similarly worded
// post, another shadow's command or a human answer cannot become the question.
func (s *Session) shadowQuestionSourceLocked(shadowID, memberID string, d shadow.TaskDependency, execution bool) (spacecontext.SourceRef, bool, error) {
	if d.QuestionID == "" || d.QuestionActionID == "" || len(d.QuestionActionID) > 200 || d.Source.ReplyID != "" || d.QuestionTool != "add_annotation" && d.QuestionTool != "reply_to_annotation" || d.QuestionTool == "reply_to_annotation" && d.Source.AnnotationID == "" {
		return d.Source, false, fmt.Errorf("待核对的提问行动身份无效")
	}
	matches := func(requestID string, actor SpaceActor) bool {
		return requestID == d.QuestionActionID && actor.Kind == "shadow" && actor.ShadowID == shadowID && actor.MemberID == memberID
	}
	for _, note := range s.record.Annotations {
		if !annotationVisible(note, execution) || d.Source.AnnotationID != "" && d.Source.AnnotationID != note.ID {
			continue
		}
		ref := d.Source
		ref.AnnotationID = note.ID
		if d.QuestionTool == "add_annotation" && matches(note.RequestID, note.Actor) {
			return ref, true, nil
		}
		if d.QuestionTool == "reply_to_annotation" {
			for _, reply := range note.Replies {
				if matches(reply.RequestID, reply.Actor) {
					ref.ReplyID = reply.ID
					return ref, true, nil
				}
			}
		}
	}
	return d.Source, false, nil
}

func (s *Session) remoteShadowDependencyHTTP(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/v2/shadow-dependency" {
		return false
	}
	if r.Method != "POST" {
		http.NotFound(w, r)
		return true
	}
	var in shadowDependencyInput
	if decode(w, r, &in) {
		out, err := s.shadowDependencyOperation(r.Context(), in)
		respond(w, out, err)
	}
	return true
}
