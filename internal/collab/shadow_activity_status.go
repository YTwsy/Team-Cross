package collab

import (
	"encoding/json"
	"fmt"
	"net/http"

	"teamcross/internal/shadow"
)

func (a *App) memberShadowEngines(space, member string) []*shadow.Engine {
	a.mu.Lock()
	all := make([]*shadow.Engine, 0, len(a.shadows))
	for _, e := range a.shadows {
		all = append(all, e)
	}
	a.mu.Unlock()
	owned := []*shadow.Engine{}
	for _, e := range all {
		s, m := e.MemberIdentity()
		if s == space && m == member {
			owned = append(owned, e)
		}
	}
	return owned
}

// Enrich only the local personal UI response, after the host has checked member
// access. The remote cache, /v2 responses, public space view and published
// materials never acquire these private processing records.
func (a *App) activityProcessingPage(page ActivityPage, view string) ActivityPage {
	if view == "space" || page.SelfID == "" {
		return page
	}
	engines := a.memberShadowEngines(page.SpaceID, page.SelfID)
	a.attentionMu.Lock()
	store := a.shadowAttention[contentHash([]string{page.SpaceID, page.SelfID})]
	a.attentionMu.Unlock()
	ids := []string{}
	collect := func(item ActivityItem) {
		// The consumer deliberately suppresses the member's own shadow output.
		// It remains visible to the person, without a fictitious pending receipt.
		if len(item.Reasons) > 0 && !(item.Actor.Kind == "shadow" && item.Actor.MemberID == page.SelfID) {
			ids = append(ids, item.ID)
		}
	}
	for _, g := range page.Groups {
		collect(g.Latest)
		if g.Attention != nil {
			collect(*g.Attention)
		}
		for _, item := range g.Events {
			collect(item)
		}
	}
	states := map[string]*shadow.ActivityProcessing{}
	if store != nil {
		states = store.ActivityStatuses(ids, engines)
	} else {
		state := "no_shadow"
		if len(engines) > 0 {
			state = "unreceived"
		}
		for _, id := range ids {
			states[id] = &shadow.ActivityProcessing{State: state}
		}
	}
	for id, p := range states {
		states[id] = p.Summary()
	}
	// Do not mutate slices or pointers which may still belong to a host/cache.
	page = activityClone(page)
	for i := range page.Groups {
		g := &page.Groups[i]
		g.Latest.Processing = states[g.Latest.ID]
		if g.Attention != nil {
			g.Attention.Processing = states[g.Attention.ID]
		}
		for j := range g.Events {
			g.Events[j].Processing = states[g.Events[j].ID]
		}
	}
	// Host pages remain at 64 KiB. The local UI has a separate 32 KiB budget
	// for terse processing labels; expanding one event fetches its details.
	if b, _ := json.Marshal(page); len(b) > 96<<10 {
		for _, p := range states {
			p.ShadowID, p.ShadowName, p.Hold = "", "", ""
			p.Details = true
		}
		if b, _ := json.Marshal(page); len(b) > 96<<10 {
			for i := range page.Groups {
				g := &page.Groups[i]
				g.Latest.Processing = nil
				if g.Attention != nil {
					g.Attention.Processing = nil
				}
				for j := range g.Events {
					g.Events[j].Processing = nil
				}
			}
			page.ProcessingError = "处理状态较多，请缩小动态范围后查看。"
		}
	}
	return page
}

func (a *App) shadowEventStatusHTTP(w http.ResponseWriter, r *http.Request, space string) {
	if r.Method != "GET" {
		http.NotFound(w, r)
		return
	}
	id := r.URL.Query().Get("eventId")
	if id == "" || len(id) > 256 {
		respond(w, nil, fmt.Errorf("需要准确的动态 ID"))
		return
	}
	store, engines, err := a.localAttention(r.Context(), space)
	if err != nil {
		respond(w, nil, err)
		return
	}
	p := store.ActivityStatuses([]string{id}, engines)[id]
	// The source opener uses one precise location per action.
	for i := range p.Actions {
		if len(p.Actions[i].Sources) > 1 {
			p.Actions[i].Sources = p.Actions[i].Sources[:1]
		}
	}
	for {
		b, _ := json.Marshal(p)
		if len(b) <= 32<<10 {
			break
		}
		if len(p.Actions) > 0 {
			p.Actions = p.Actions[:len(p.Actions)-1]
		} else if len(p.Jobs) > 0 {
			p.Jobs = p.Jobs[:len(p.Jobs)-1]
		} else {
			respond(w, nil, fmt.Errorf("动态处理详情超过读取预算"))
			return
		}
	}
	respond(w, p, nil)
}

type shadowRouteView struct {
	Route        ActivityAgentRoute            `json:"route"`
	Runtime      *shadow.AttentionRuntimeState `json:"runtime,omitempty"`
	LocalShadows int                           `json:"localShadows"`
}

func (a *App) shadowRouteHTTP(w http.ResponseWriter, r *http.Request, spaceID string) {
	if r.Method != "GET" && r.Method != "POST" {
		http.NotFound(w, r)
		return
	}
	store, engines, err := a.localAttention(r.Context(), spaceID)
	if err != nil {
		respond(w, nil, err)
		return
	}
	if r.Method == "POST" {
		var in ActivityConsumerInput
		if !decode(w, r, &in) {
			return
		}
		if in.ShadowID == "" || in.Enabled != nil {
			respond(w, nil, fmt.Errorf("请选择接手动态的本人分身"))
			return
		}
		_, err = a.activityConsumerCall(r.Context(), spaceID, "configure", in)
		if err != nil {
			respond(w, nil, err)
			return
		}
	}
	// A retried configure command may return an old receipt. Always show/apply
	// the current authoritative route, never roll back a newer member choice.
	out, err := a.activityConsumerCall(r.Context(), spaceID, "status", ActivityConsumerInput{})
	if err != nil {
		respond(w, nil, err)
		return
	}
	route := workbenchDecode[ActivityAgentRoute](out)
	if r.Method == "POST" && route.ShadowID != "" {
		if err = store.SetRoute(shadow.AttentionRoute{ShadowID: route.ShadowID, Revision: route.Revision, Enabled: route.Enabled}); err != nil {
			respond(w, nil, err)
			return
		}
	}
	view := shadowRouteView{Route: route, LocalShadows: len(engines)}
	for _, e := range engines {
		state := e.AttentionRuntime()
		if state.ShadowID == route.ShadowID {
			view.Runtime = &state
			break
		}
	}
	respond(w, view, nil)
}
