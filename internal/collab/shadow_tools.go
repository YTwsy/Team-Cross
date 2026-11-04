package collab

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"teamcross/internal/mcp"
	"teamcross/internal/readview"
	"teamcross/internal/shadow"
	"teamcross/internal/sharing"
)

type shadowToolInput struct {
	ShadowID  string         `json:"shadowId"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func (a *App) shadowToolCall(ctx context.Context, shadowID, name string, args map[string]any) (any, error) {
	a.mu.Lock()
	e, closed := a.shadows[shadowID], a.closed
	a.mu.Unlock()
	if e == nil || closed {
		return nil, fmt.Errorf("分身运行记录不存在或 Core 已关闭")
	}
	_, spaceID, _, _ := e.Identity()
	a.mu.Lock()
	s, j := a.sessions[spaceID], a.joined[spaceID]
	a.mu.Unlock()
	in := shadowToolInput{ShadowID: shadowID, Name: name, Arguments: libraryClone(args)}
	if s != nil {
		return s.shadowToolOperation(ctx, in)
	}
	if j == nil {
		return nil, fmt.Errorf("分身所属空间不存在")
	}
	j.mu.Lock()
	hostID := j.Invitation.ID
	j.mu.Unlock()
	var out any
	if err := j.request(ctx, "POST", "/v2/shadow-tools", remapWorkbench(in, spaceID, hostID), &out); err != nil {
		return nil, err
	}
	return remapWorkbench(out, hostID, spaceID), nil
}

func (s *Session) shadowToolOperation(ctx context.Context, in shadowToolInput) (any, error) {
	if err := mcp.ValidateShadowCall(in.Name, in.Arguments); err != nil {
		return nil, err
	}
	s.mu.Lock()
	shared, exists := s.record.Shadows[in.ShadowID]
	valid := s.space == nil && !s.closed && s.record.State != "ended" && s.record.State != "unsupported" && s.callerValidLocked(ctx) && exists && shared.View.Owner.ID == sharing.MemberID(ctx)
	spaceID := s.record.ID
	target := s
	executionID, _ := in.Arguments["executionId"].(string)
	if executionID != "" {
		target = s.executions[executionID]
	}
	s.mu.Unlock()
	if !valid {
		return nil, fmt.Errorf("分身与当前空间成员归属不匹配")
	}
	if target == nil {
		return nil, fmt.Errorf("来源会话不属于当前空间")
	}
	actor := SpaceActor{Kind: "shadow", ShadowID: in.ShadowID, MemberID: shared.View.Owner.ID, Name: shared.View.Name}
	ctx = context.WithValue(ctx, activityActorKey{}, actor)
	args := libraryClone(in.Arguments)
	switch in.Name {
	case "list_space_members":
		return s.activityOperation(ctx, "members", ActivityInput{})
	case "read_annotations", "read_context":
		readview.Defaults(args)
		q := url.Values{"compact": {"true"}}
		for key, value := range args {
			q.Set(key, fmt.Sprint(value))
		}
		kind := "annotations"
		if in.Name == "read_context" {
			kind, _ = args["kind"].(string)
		}
		return target.ContextHistory(ctx, kind, q.Get("path"), number(q.Get("after")), historyReadFromURL(&url.URL{RawQuery: q.Encode()}))
	case "list_materials":
		return s.materialOperation(ctx, "materials", nil)
	case "read_material":
		readview.Defaults(args)
		return s.readMaterial(ctx, workbenchDecode[MaterialRead](args))
	case "add_annotation":
		return target.annotate(workbenchDecode[Annotation](args), actor.Name, ctx)
	case "reply_to_annotation":
		return target.replyAnnotationResult(ctx, workbenchDecode[AnnotationReplyInput](args), actor.Name, true)
	case "list_space_shadows", "read_space_shadow", "send_space_shadow":
		payload := workbenchDecode[struct {
			ShadowID  string `json:"shadowId"`
			MessageID string `json:"messageId"`
			ResultID  string `json:"resultId"`
			Text      string `json:"text"`
			Offset    int    `json:"offset"`
		}](args)
		op := "view"
		if in.Name == "send_space_shadow" {
			op = "send"
		} else if in.Name == "read_space_shadow" && (payload.MessageID != "" || payload.ResultID != "") {
			op = "source"
		}
		return s.shadowOperation(ctx, op, shadowInput{ID: payload.ShadowID, Offset: payload.Offset, MessageID: payload.MessageID, ResultID: payload.ResultID, Message: shadow.Message{ID: payload.MessageID, Text: payload.Text}})
	default:
		input := workbenchDecode[workbenchInput](args)
		input.Actor = actor
		switch in.Name {
		case "read_space_brief":
			return s.workbenchOperation(ctx, "brief-read", input)
		case "update_space_brief":
			return s.workbenchOperation(ctx, "brief-update", input)
		case "send_space_request":
			out, err := s.workbenchOperation(ctx, "send", input)
			if err == nil {
				s.app.wakeWorkbench()
			}
			return out, err
		case "get_space_request":
			return s.workbenchOperation(ctx, "request", input)
		case "send_space_note":
			return s.workbenchOperation(ctx, "inflow-note", input)
		case "get_space_note":
			return s.workbenchOperation(ctx, "inflow-note-read", input)
		}
		out, err := s.workbenchOperation(ctx, "view", input)
		if err != nil {
			return nil, err
		}
		return projectWorkbenchRead(in.Name, spaceID, input.Offset, workbenchDecode[WorkbenchView](out))
	}
}

func (s *Session) remoteShadowToolsHTTP(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/v2/shadow-tools" {
		return false
	}
	if r.Method != "POST" {
		http.NotFound(w, r)
		return true
	}
	var in shadowToolInput
	if decode(w, r, &in) {
		out, err := s.shadowToolOperation(r.Context(), in)
		respond(w, out, err)
	}
	return true
}

// Only trusted internal adapters set this context value after checking the
// shadow's registration against the authenticated member. Public JSON cannot.
func collaborationShadowActor(ctx context.Context) (SpaceActor, bool) {
	a, ok := ctx.Value(activityActorKey{}).(SpaceActor)
	return a, ok && a.Kind == "shadow" && a.ShadowID != "" && a.MemberID == sharing.MemberID(ctx)
}
