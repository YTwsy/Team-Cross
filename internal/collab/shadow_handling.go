package collab

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"teamcross/internal/shadow"
	"teamcross/internal/spacecontext"
)

// Private, local UI only: membership is checked at the hosting Core, then
// records are restricted to this member's local engines. No /v2 projection.
func (a *App) localAttention(ctx context.Context, spaceID string) (*shadow.AttentionStore, []*shadow.Engine, error) {
	out, err := a.shadowCall(ctx, spaceID, "check", shadowInput{})
	if err != nil {
		return nil, nil, err
	}
	actor := workbenchDecode[shadow.Actor](out)
	if actor.ID == "" {
		return nil, nil, fmt.Errorf("当前空间成员身份不可用")
	}
	store, err := a.memberAttention(spaceID, actor.ID)
	if err != nil {
		return nil, nil, err
	}
	a.mu.Lock()
	engines := []*shadow.Engine{}
	for _, e := range a.shadows {
		engines = append(engines, e)
	}
	a.mu.Unlock()
	owned := []*shadow.Engine{}
	for _, e := range engines {
		space, member := e.MemberIdentity()
		if space == spaceID && member == actor.ID {
			if err := e.AttachAttention(store); err != nil {
				return nil, nil, err
			}
			owned = append(owned, e)
		}
	}
	sort.Slice(owned, func(i, j int) bool {
		a, _, _, _ := owned[i].Identity()
		b, _, _, _ := owned[j].Identity()
		return a < b
	})
	return store, owned, nil
}

func (a *App) shadowHandlingHTTP(w http.ResponseWriter, r *http.Request, path string) bool {
	parts := strings.Split(path, "/")
	if len(parts) < 3 || len(parts) > 4 || parts[0] != "collaborations" || parts[2] != "attention" {
		return false
	}
	if len(parts) == 4 && parts[3] == "route" {
		a.shadowRouteHTTP(w, r, parts[1])
		return true
	}
	if len(parts) == 4 && parts[3] == "event" {
		a.shadowEventStatusHTTP(w, r, parts[1])
		return true
	}
	if len(parts) == 4 && (parts[3] == "questions" || parts[3] == "question") {
		a.shadowQuestionsHTTP(w, r, parts[1], parts[3] == "question")
		return true
	}
	if r.Method != "GET" && r.Method != "POST" || len(parts) == 4 && parts[3] != "handling" || len(parts) == 3 && r.Method != "GET" {
		http.NotFound(w, r)
		return true
	}
	store, engines, err := a.localAttention(r.Context(), parts[1])
	if err != nil {
		respond(w, nil, err)
		return true
	}
	if len(parts) == 3 {
		items := []shadow.WaitingItem{}
		for _, e := range engines {
			items = append(items, e.WaitingItems()...)
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		page, err := store.WaitingPage(items, r.URL.Query().Get("direction"), r.URL.Query().Get("cursor"), limit)
		respond(w, map[string]any{"waiting": page, "route": store.Snapshot().Route, "localShadows": len(engines)}, err)
		return true
	}
	if r.Method == "GET" {
		q := r.URL.Query()
		out, err := store.HandlingFor(spacecontext.SourceRef{SpaceID: parts[1], ExecutionID: q.Get("executionId"), AnnotationID: q.Get("annotationId"), ReplyID: q.Get("replyId")})
		respond(w, struct {
			shadow.QuestionHandling
			CanHandoff bool `json:"canHandoff"`
		}{out, len(engines) > 0}, err)
		return true
	}
	var in struct {
		shadow.HandlingInput
		ShadowID string `json:"shadowId"`
	}
	if !decode(w, r, &in) {
		return true
	}
	if in.Source.SpaceID == "" {
		in.Source.SpaceID = parts[1]
	}
	if in.Source.SpaceID != parts[1] {
		respond(w, nil, fmt.Errorf("问题不属于当前空间"))
		return true
	}
	target := in.ShadowID
	if target == "" {
		target = store.Snapshot().Route.ShadowID
	}
	if target == "" && len(engines) == 1 {
		target, _, _, _ = engines[0].Identity()
	}
	for _, e := range engines {
		id, _, _, _ := e.Identity()
		if id != target {
			continue
		}
		out, err := e.SetQuestionHandler(r.Context(), in.HandlingInput, "local-ui:"+in.RequestID)
		respond(w, out, err)
		return true
	}
	respond(w, nil, fmt.Errorf("本机尚无可接续这条问题的本人分身"))
	return true
}
