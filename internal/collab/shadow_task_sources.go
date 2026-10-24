package collab

import (
	"context"
	"fmt"
	"net/http"

	"teamcross/internal/shadow"
	"teamcross/internal/sharing"
)

type shadowTaskSourceInput struct {
	ShadowID string                 `json:"shadowId"`
	Query    shadow.TaskSourceQuery `json:"query"`
}

func (a *App) readShadowTaskSource(ctx context.Context, id string, query shadow.TaskSourceQuery) (shadow.TaskSourceUpdate, error) {
	a.mu.Lock()
	e, closed := a.shadows[id], a.closed
	a.mu.Unlock()
	if e == nil || closed {
		return shadow.TaskSourceUpdate{}, fmt.Errorf("分身不存在或 Core 已关闭")
	}
	_, spaceID, _, _ := e.Identity()
	a.mu.Lock()
	s, j := a.sessions[spaceID], a.joined[spaceID]
	a.mu.Unlock()
	in := shadowTaskSourceInput{ShadowID: id, Query: query}
	if s != nil {
		return s.shadowTaskSourceOperation(ctx, in)
	}
	if j == nil {
		return shadow.TaskSourceUpdate{}, fmt.Errorf("任务来源空间不存在")
	}
	j.mu.Lock()
	hostID := j.Invitation.ID
	j.mu.Unlock()
	var out shadow.TaskSourceUpdate
	if err := j.request(ctx, "POST", "/v2/shadow-task-source", in, &out); err != nil {
		return out, err
	}
	return workbenchDecode[shadow.TaskSourceUpdate](remapWorkbench(out, hostID, spaceID)), nil
}

// The caller supplies only an exact public object and a discovery cursor.
// Personal follow/read preferences and private task contents are not queried.
func (s *Session) shadowTaskSourceOperation(ctx context.Context, in shadowTaskSourceInput) (shadow.TaskSourceUpdate, error) {
	s = s.activityRoot()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := shadow.TaskSourceUpdate{Cursor: in.Query.Cursor, Signals: []shadow.DependencySignal{}}
	registered, exists := s.record.Shadows[in.ShadowID]
	if s.closed || s.record.State == "ended" || s.record.State == "unsupported" || !s.callerValidLocked(ctx) || !exists || registered.View.Owner.ID != sharing.MemberID(ctx) {
		return out, fmt.Errorf("任务来源或分身成员身份无效")
	}
	subject := in.Query.Subject
	target, err := s.activitySubjectSessionLocked(subject, false)
	if err != nil {
		return out, err
	}
	execution := sharing.ExecutionAuthorized(ctx, target.sharedRuntime(), target.record.ID)
	if target.space != nil && !execution {
		return out, fmt.Errorf("任务会话来源已不可访问")
	}
	if subject.Kind == "discussion" {
		for _, n := range target.record.Annotations {
			if n.ID == subject.ID && !annotationVisible(n, execution) {
				return out, fmt.Errorf("任务讨论来源不可访问")
			}
		}
	}
	if err := s.drainActivityLocked(); err != nil {
		return out, err
	}
	log := s.activityLog
	if in.Query.Cursor.Seq > log.Seq {
		return out, fmt.Errorf("任务来源位置超过当前空间记录")
	}
	out.Cursor = shadow.TaskSourceCursor{Seq: log.Seq, Retention: log.Retention, Initialized: true}
	if in.Query.Cursor.Retention != log.Retention && in.Query.Cursor.Seq < log.Seq {
		// Retention can remove older unrelated events as well. A conservative
		// scoped resync is preferable to claiming a missing range was reviewed.
		base, snapshot, err := target.followSourceBaselineLocked(subject)
		if err != nil {
			return out, err
		}
		refs := snapshot.Baseline
		if len(refs) == 0 {
			refs = []SpaceSourceRef{base}
		}
		for i, ref := range refs {
			out.Signals = append(out.Signals, shadow.DependencySignal{
				ID: "source-resync:" + contentHash([]any{subject, log.Seq, log.Retention, i, ref}), Source: ref, State: "source_resync_partial",
				Preview: "Earlier event history is incomplete. Reconcile this bounded current snapshot; do not claim the missing history was reviewed. Read exact sources for any remaining facts.",
			})
		}
		if subject.Kind == "execution" && len(snapshot.Baseline) == 0 {
			out.Signals[0].Preview += " This execution currently has no recorded completed turn."
		}
		out.Resync = true
		return out, nil
	}
	for _, event := range log.Events {
		if event.Seq <= in.Query.Cursor.Seq || event.Follow != nil || event.Kind == "follow_changed" {
			continue
		}
		matches := event.Subject == subject
		if subject.Kind == "material" {
			matches = event.Source.MaterialID == subject.ID
		}
		if subject.Kind == "execution" {
			matches = event.Source.ExecutionID == subject.ID
		}
		if !matches || !s.activityAvailableLocked(event) {
			continue
		}
		if event.Source.AnnotationID != "" {
			visible := false
			for _, n := range target.record.Annotations {
				if n.ID == event.Source.AnnotationID {
					visible = annotationVisible(n, execution)
					break
				}
			}
			if !visible {
				continue
			}
		}
		out.Signals = append(out.Signals, shadow.DependencySignal{ID: event.ID, Source: event.Source, Actor: event.Actor, State: event.Kind, Preview: event.Preview})
		if len(out.Signals) == 16 {
			out.Cursor.Seq = event.Seq
			break
		}
	}
	return out, nil
}

func (s *Session) remoteShadowTaskSourceHTTP(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/v2/shadow-task-source" {
		return false
	}
	if r.Method != "POST" {
		http.NotFound(w, r)
		return true
	}
	var in shadowTaskSourceInput
	if decode(w, r, &in) {
		out, err := s.shadowTaskSourceOperation(r.Context(), in)
		respond(w, out, err)
	}
	return true
}
