package shadow

import (
	"encoding/json"
	"sort"
	"time"

	"teamcross/internal/spacecontext"
)

// Private member-UI projections. They deliberately omit prompts, native IDs,
// transport receipts, tool arguments and personal follow metadata.
type ActivityProcessing struct {
	Details      bool               `json:"details,omitempty"`
	State        string             `json:"state"`
	ShadowID     string             `json:"shadowId,omitempty"`
	ShadowName   string             `json:"shadowName,omitempty"`
	Hold         string             `json:"hold,omitempty"`
	Error        string             `json:"error,omitempty"`
	ReceivedAt   *time.Time         `json:"receivedAt,omitempty"`
	Jobs         []ProcessingJob    `json:"jobs,omitempty"`
	Actions      []ProcessingAction `json:"actions,omitempty"`
	TotalJobs    int                `json:"totalJobs"`
	TotalActions int                `json:"totalActions"`
}

func (p *ActivityProcessing) Summary() *ActivityProcessing {
	return &ActivityProcessing{State: p.State, ShadowID: p.ShadowID, ShadowName: clip(p.ShadowName, 80), Hold: p.Hold, TotalJobs: p.TotalJobs, TotalActions: p.TotalActions, Details: p.TotalJobs > 0 || p.TotalActions > 0 || p.Error != ""}
}

type ProcessingJob struct {
	ID       string `json:"id"`
	ShadowID string `json:"shadowId"`
	Title    string `json:"title"`
	State    string `json:"state"`
	Version  int    `json:"version"`
	Waiting  string `json:"waiting,omitempty"`
}

type ProcessingAction struct {
	ID           string                   `json:"id"`
	Tool         string                   `json:"tool"`
	State        string                   `json:"state"`
	ReceiptState string                   `json:"receiptState,omitempty"`
	Sources      []spacecontext.SourceRef `json:"sources,omitempty"`
	UpdatedAt    time.Time                `json:"updatedAt"`
}

type AttentionRuntimeState struct {
	ShadowID string `json:"shadowId"`
	Name     string `json:"name"`
	State    string `json:"state"`
}

func (e *Engine) attentionRuntimeLocked() AttentionRuntimeState {
	state := "ready"
	switch {
	case e.closed:
		state = "offline"
	case !e.record.Enabled:
		state = "paused"
	case e.record.Error != "":
		state = "error"
	case !e.started:
		state = "starting"
	}
	// An idle native app-server need not exist yet. Core readiness is not
	// inferred from View.Online, which describes the native runtime only.
	return AttentionRuntimeState{e.record.ID, e.record.Name, state}
}

func (e *Engine) AttentionRuntime() AttentionRuntimeState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.attentionRuntimeLocked()
}

type activityStatusInput struct {
	event         AttentionEvent
	continuations map[string]bool
	jobs          map[string]map[string]bool
}

// ActivityStatuses projects only requested events. It neither acknowledges a
// human read nor starts/retries model work. Locks are never held across engines.
func (s *AttentionStore) ActivityStatuses(ids []string, engines []*Engine) map[string]*ActivityProcessing {
	out := map[string]*ActivityProcessing{}
	inputs := map[string]activityStatusInput{}
	s.mu.Lock()
	route, space, member := s.record.Route, s.record.SpaceID, s.record.MemberID
	for _, id := range ids {
		if out[id] != nil {
			continue
		}
		p := &ActivityProcessing{State: "unreceived", ShadowID: route.ShadowID}
		out[id] = p
		event, ok := s.record.Events[id]
		if !ok {
			if route.ShadowID == "" {
				p.State = "no_shadow"
			}
			continue
		}
		input := activityStatusInput{event: clone(event), continuations: map[string]bool{}, jobs: map[string]map[string]bool{}}
		deliveryOwner := ""
		for _, d := range event.TaskDeliveries {
			if deliveryOwner == "" || d.ShadowID < deliveryOwner {
				deliveryOwner = d.ShadowID
			}
			if input.jobs[d.ShadowID] == nil {
				input.jobs[d.ShadowID] = map[string]bool{}
			}
			input.jobs[d.ShadowID][d.JobID] = true
		}
		if deliveryOwner != "" {
			p.ShadowID = deliveryOwner
		}
		at := event.ReceivedAt
		p.ReceivedAt, p.State = &at, "received"
		switch event.State {
		case "processed":
			p.State = "processed"
		case "observing":
			p.State = "processing"
		case "owner":
			p.State = "needs_owner"
		}
		var newest time.Time
		for _, b := range s.record.Batches {
			if b.State == "superseded" || !containsString(b.EventIDs, id) || b.CreatedAt.Before(newest) {
				continue
			}
			newest, p.ShadowID = b.CreatedAt, b.ShadowID
			if b.State == "failed" {
				p.State, p.Error = "error", clip(b.Error, 500)
			}
		}
		newest = time.Time{}
		for _, c := range s.record.Continuations {
			if !containsString(c.EventIDs, id) {
				continue
			}
			input.continuations[c.ID] = true
			if c.State == "superseded" || c.CreatedAt.Before(newest) {
				continue
			}
			newest, p.ShadowID = c.CreatedAt, c.ShadowID
			switch c.State {
			case "queued":
				p.State, p.Error = "received", ""
			case "running":
				p.State, p.Error = "processing", ""
			case "failed":
				p.State, p.Error = "error", clip(c.Error, 500)
			case "processed":
				p.State, p.Error = "processed", ""
			}
		}
		f := s.record.Focuses[event.FocusID]
		if event.State == "queued" {
			if !attentionEligible(s.record, event.Signal) {
				p.State = "skipped"
			}
			if f.State == "paused" {
				p.Hold = "focus_paused"
			}
			if f.Error != "" {
				p.State, p.Error = "error", clip(f.Error, 500)
			}
		}
		if h, ok := s.record.Handling[questionSourceID(event.Signal.Source)]; ok && h.Handler == "owner" && h.State == "waiting" {
			p.State = "needs_owner"
		}
		inputs[id] = input
	}
	storeError := clip(s.record.Error, 500)
	s.mu.Unlock()
	runtimes := map[string]AttentionRuntimeState{}
	for _, e := range engines {
		e.mu.Lock()
		if e.record.SpaceID != space || e.record.Owner.ID != member {
			e.mu.Unlock()
			continue
		}
		runtime := e.attentionRuntimeLocked()
		runtimes[runtime.ShadowID] = runtime
		for id, input := range inputs {
			p := out[id]
			jobs := input.jobs[e.record.ID]
			if jobs == nil {
				jobs = map[string]bool{}
			}
			for _, run := range e.record.Runs {
				if input.continuations[run.EventID] {
					for _, jobID := range run.TaskIDs {
						jobs[jobID] = true
					}
				}
			}
			for _, job := range e.record.Jobs {
				if job.Trigger != nil && (containsString(job.Trigger.EventIDs, id) || input.continuations[job.Trigger.ContinuationID]) {
					jobs[job.ID] = true
				}
			}
			for jobID := range jobs {
				job, ok := e.record.Jobs[jobID]
				if !ok {
					continue
				}
				j := ProcessingJob{ID: job.ID, ShadowID: e.record.ID, Title: clip(job.Title, 200), State: job.State, Version: job.Version}
				for _, d := range job.Dependencies {
					if d.State == "satisfied" || !taskAcceptsAnswers(job) {
						continue
					}
					waiting := "waiting_others"
					q, asked := e.record.Questions[d.QuestionID]
					switch {
					case d.QuestionSourcePending || asked && q.Delivery != "saved":
						waiting = "delivery_unknown"
					case len(d.Signals) > 0:
						waiting = "answer_received"
					case asked && containsString(q.Recipients, member):
						waiting = "needs_owner"
					}
					if waitingPriority(waiting) > waitingPriority(j.Waiting) {
						j.Waiting = waiting
					}
				}
				p.Jobs = append(p.Jobs, j)
			}
			for _, action := range e.record.Actions {
				if !input.continuations[action.EventID] && !jobs[action.JobID] {
					continue
				}
				projected := ProcessingAction{ID: action.ID, Tool: action.Tool, State: action.State, Sources: clone(action.ResponseSources), UpdatedAt: action.UpdatedAt}
				if q, ok := e.record.Questions[action.QuestionID]; ok && q.Delivery == "saved" {
					projected.Sources = []spacecontext.SourceRef{clone(q.Source)}
				}
				if action.State == "saved" {
					var receipt struct {
						State string `json:"state"`
					}
					if json.Unmarshal(action.Result, &receipt) == nil {
						switch receipt.State {
						case "sending", "delivered", "sendFailed", "pending", "accepted", "running", "completed", "failed", "cancelled", "conflict":
							projected.ReceiptState = receipt.State
						}
					}
				}
				p.Actions = append(p.Actions, projected)
				if projected.ReceiptState == "sendFailed" || projected.ReceiptState == "failed" {
					if action.JobID == "" || e.record.Jobs[action.JobID].Version == action.JobVersion {
						p.Hold = "delivery_unknown"
					}
				}
				// Saved is a tool receipt, never proof of downstream completion.
				if action.State == "unknown" || action.State == "pending" {
					if action.JobID == "" || e.record.Jobs[action.JobID].Version == action.JobVersion {
						if action.State == "unknown" || !activeRun(e.record.Runs[action.RunID]) {
							p.Hold = "delivery_unknown"
						}
					}
				}
			}
		}
		e.mu.Unlock()
	}
	for id, p := range out {
		if p.State == "no_shadow" && len(runtimes) > 0 {
			p.State = "unreceived"
		}
		sort.Slice(p.Jobs, func(i, j int) bool { a, b := p.Jobs[i], p.Jobs[j]; return a.ShadowID+":"+a.ID < b.ShadowID+":"+b.ID })
		sort.Slice(p.Actions, func(i, j int) bool {
			a, b := p.Actions[i], p.Actions[j]
			if !a.UpdatedAt.Equal(b.UpdatedAt) {
				return a.UpdatedAt.After(b.UpdatedAt)
			}
			return a.ID < b.ID
		})
		jobState, allCompleted := "", len(p.Jobs) > 0
		for _, j := range p.Jobs {
			state := processingJobState(j)
			allCompleted = allCompleted && j.State == "completed"
			if processingPriority(state) > processingPriority(jobState) {
				jobState = state
				p.ShadowID = j.ShadowID
			}
		}
		if p.State != "error" && p.State != "needs_owner" {
			if processingPriority(jobState) > processingPriority(p.State) {
				p.State = jobState
			}
			if allCompleted && (p.State == "processed" || inputs[id].event.State == "task" && p.State == "received") && p.Hold != "delivery_unknown" {
				p.State = "completed"
			}
		}
		if p.ShadowID != "" {
			r, ok := runtimes[p.ShadowID]
			p.ShadowName = r.Name
			if p.State != "processed" && p.State != "completed" && p.State != "skipped" && p.State != "needs_owner" && p.State != "processing" && p.Hold == "" {
				switch {
				case !ok || r.State == "offline":
					p.Hold = "shadow_offline"
				case r.State == "paused":
					p.Hold = "shadow_paused"
				case r.State == "error":
					p.Hold = "shadow_error"
				case r.State == "starting":
					p.Hold = "shadow_starting"
				case !route.Enabled:
					p.Hold = "route_paused"
				}
			}
		}
		if (p.State == "unreceived" || p.State == "received") && storeError != "" {
			p.Error = storeError
		}
		if p.Hold == "delivery_unknown" && p.State != "processing" {
			p.State = "error"
		}
		p.TotalJobs, p.TotalActions = len(p.Jobs), len(p.Actions)
		if len(p.Jobs) > 16 {
			p.Jobs = p.Jobs[:16]
		}
		if len(p.Actions) > 16 {
			p.Actions = p.Actions[:16]
		}
	}
	return out
}

func containsString(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func waitingPriority(state string) int {
	return map[string]int{"waiting_others": 1, "answer_received": 2, "needs_owner": 3, "delivery_unknown": 4}[state]
}

func processingPriority(state string) int {
	return map[string]int{"processed": 1, "received": 2, "paused": 3, "waiting_others": 4, "processing": 5, "needs_owner": 6, "error": 7}[state]
}

func processingJobState(j ProcessingJob) string {
	if j.Waiting == "delivery_unknown" {
		return "error"
	}
	if j.Waiting == "needs_owner" {
		return "needs_owner"
	}
	switch j.State {
	case "failed", "unknown", "interrupted":
		return "error"
	case "running", "stopping":
		return "processing"
	case "paused":
		return "paused"
	case "waiting":
		if j.Waiting == "answer_received" {
			return "received"
		}
		return "waiting_others"
	case "queued", "needs_followup":
		return "received"
	}
	return ""
}
