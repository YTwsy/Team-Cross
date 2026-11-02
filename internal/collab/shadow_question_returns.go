package collab

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"teamcross/internal/inflow"
	"teamcross/internal/shadow"
)

func shadowQuestionReturnVisible(route ShadowReturnInbox, event ShadowReturnEvent) bool {
	if event.Question == nil || event.Item.State == "waiting" {
		return true
	}
	// An already answered question is not a new prompt. Send its updated state
	// only when an earlier version actually reached this originating session.
	_, sent := route.Delivered[event.Item.Key]
	return sent || route.DeliveredVersions[event.Item.Key] > 0
}

type shadowQuestionFacts struct {
	Available  bool
	Owned      bool
	Text       string
	Recipients []string
	Answer     *SpaceSourceRef
}

// Read only the exact recorded public question. This host-side check also
// invalidates old prompts while the working member's Core is offline/paused.
// Space and execution records share the root session mutex.
func (s *Session) shadowQuestionFactsLocked(shared ShadowShared, ref SpaceSourceRef) shadowQuestionFacts {
	facts := shadowQuestionFacts{}
	target := s
	if ref.ExecutionID != "" {
		target = s.executions[ref.ExecutionID]
	}
	if target == nil || target.closed {
		return facts
	}
	var note *Annotation
	for i := range target.record.Annotations {
		if target.record.Annotations[i].ID == ref.AnnotationID {
			note = &target.record.Annotations[i]
			break
		}
	}
	if note == nil || !annotationVisible(*note, true) {
		return facts
	}
	available := true
	if note.Target != nil && note.Target.Kind == "material" {
		available = false
		for _, m := range s.record.Materials {
			available = available || m.ID == note.Target.MaterialID && m.WithdrawnAt == nil
		}
	}
	actor := note.Actor
	facts.Text = note.Text
	for _, m := range note.Mentions {
		facts.Recipients = append(facts.Recipients, m.MemberID)
	}
	if ref.ReplyID != "" {
		found := false
		for _, reply := range note.Replies {
			if reply.ID != ref.ReplyID {
				continue
			}
			found = true
			actor = reply.Actor
			facts.Text = reply.Text
			facts.Recipients = nil
			for _, m := range reply.Mentions {
				facts.Recipients = append(facts.Recipients, m.MemberID)
			}
			parent := note.Actor
			if reply.ReplyTo != nil && reply.ReplyTo.Kind == "reply" {
				parent = SpaceActor{}
				for _, p := range note.Replies {
					if p.ID == reply.ReplyTo.ID {
						parent = p.Actor
						break
					}
				}
			}
			if parent.Kind != "shadow" && parent.MemberID != "" {
				facts.Recipients = append(facts.Recipients, parent.MemberID)
			}
			break
		}
		if !found {
			return shadowQuestionFacts{}
		}
	}
	facts.Available = available
	if !available {
		facts.Text = ""
	}
	facts.Owned = actor.Kind == "shadow" && actor.ShadowID == shared.View.ID && actor.MemberID == shared.View.Owner.ID
	descendants := map[string]bool{}
	if ref.ReplyID != "" {
		descendants[ref.ReplyID] = true
	}
	for _, reply := range note.Replies {
		root := reply.ReplyTo == nil || reply.ReplyTo.Kind == "annotation" && reply.ReplyTo.ID == note.ID
		matches := ref.ReplyID == "" && root || reply.ReplyTo != nil && reply.ReplyTo.Kind == "reply" && descendants[reply.ReplyTo.ID]
		if matches {
			descendants[reply.ID] = true
		}
		if !matches || reply.Actor.Kind == "shadow" && reply.Actor.ShadowID == shared.View.ID {
			continue
		}
		answer := ref
		answer.ReplyID = reply.ID
		facts.Answer = &answer
		break
	}
	return facts
}

func questionReturnIdentity(q shadow.QuestionReturn) string {
	return contentHash([]any{q.ID, q.QuestionID, q.TaskID, q.MessageID, q.Source})
}

func validQuestionReturnState(state string) bool {
	switch state {
	case "waiting", "answer_received", "conditions_met", "closed", "paused", "unavailable":
		return true
	}
	return false
}

func (s *Session) captureShadowQuestionReturnsLocked(shared *ShadowShared, updates []shadow.QuestionReturn) (bool, error) {
	encoded, _ := json.Marshal(updates)
	if len(updates) > shadow.QuestionReturnBatchLimit || len(encoded) > shadow.QuestionReturnBatchBytes {
		return false, fmt.Errorf("提问返回批次超过预算")
	}
	changed := false
	seen := map[string]bool{}
	for _, q := range updates {
		canonical := SpaceSourceRef{SpaceID: s.record.ID, ExecutionID: q.Source.ExecutionID, AnnotationID: q.Source.AnnotationID, ReplyID: q.Source.ReplyID}
		if q.ID == "" || len(q.ID) > 256 || q.QuestionID == "" || len(q.QuestionID) > 256 || q.TaskID == "" || len(q.TaskID) > 256 || q.MessageID == "" || len(q.MessageID) > 256 || q.Revision < 1 || q.TaskVersion < 1 || seen[q.ID] || !validQuestionReturnState(q.State) || q.Source.AnnotationID == "" || contentHash(canonical) != contentHash(q.Source) {
			return false, fmt.Errorf("提问返回身份或状态无效")
		}
		seen[q.ID] = true
		old, exists := shared.Questions[q.ID]
		if exists {
			if questionReturnIdentity(old) != questionReturnIdentity(q) {
				return false, fmt.Errorf("同一提问返回不能改换原任务或问题")
			}
			if q.Revision < old.Revision {
				continue
			}
			if q.Revision == old.Revision {
				if contentHash(old) != contentHash(q) {
					return false, fmt.Errorf("同一提问修订内容发生变化")
				}
				continue
			}
		}
		facts := s.shadowQuestionFactsLocked(*shared, q.Source)
		if !facts.Owned && (facts.Available || !exists && q.State == "waiting") {
			return false, fmt.Errorf("提问返回尚未核对实际分身问题")
		}
		if shared.Questions == nil {
			shared.Questions = map[string]shadow.QuestionReturn{}
		}
		shared.Questions[q.ID], changed = q, true
		for routeID, route := range shared.Returns {
			if route.MessageID != q.MessageID {
				continue
			}
			key := "shadow_question:" + q.ID
			prior, had := route.Events[key]
			if !had && !slices.Contains(facts.Recipients, route.MemberID) {
				continue
			}
			event := s.shadowQuestionReturnEventLocked(*shared, q, prior, facts)
			route.Events[key] = event
			shared.Returns[routeID] = route
		}
	}
	return changed, nil
}

func (s *Session) shadowQuestionReturnEventLocked(shared ShadowShared, q shadow.QuestionReturn, prior ShadowReturnEvent, facts shadowQuestionFacts) ShadowReturnEvent {
	state, text := q.State, facts.Text
	answer := facts.Answer
	if answer == nil {
		answer = prior.Item.AnswerSource
	}
	if state != "closed" && state != "conditions_met" {
		if !facts.Available {
			state, text = "unavailable", "原问题已不可用，请核对任务来源"
		} else if answer != nil {
			state = "answer_received"
		}
	}
	for _, job := range shared.View.Jobs {
		if job.ID != q.TaskID {
			continue
		}
		if job.Version != q.TaskVersion {
			state = "superseded"
		}
		if job.State == "cancelled" || job.State == "completed" || job.State == "failed" || job.State == "interrupted" {
			state = "closed"
		}
		break
	}
	source := q.Source
	item := inflow.Item{Key: "shadow_question:" + q.ID, Version: prior.Item.Version, Kind: "shadow_question", SpaceID: shared.View.SpaceID, ShadowID: shared.View.ID, SourceID: q.MessageID, TaskID: q.TaskID, TaskVersion: q.TaskVersion, QuestionID: q.QuestionID, Source: &source, AnswerSource: answer, State: state, Text: shortText(text, 200), Condition: shortText(q.Condition, 120), NextStep: shortText(q.NextStep, 100), Author: shortText(shared.View.Name, 80)}
	before := prior.Item
	before.At = time.Time{}
	if contentHash(item) != contentHash(before) {
		item.Version++
		item.At = time.Now()
	} else {
		item.At = prior.Item.At
	}
	return ShadowReturnEvent{Item: item, Question: &q}
}

func (s *Session) refreshShadowQuestionReturnsLocked(shared *ShadowShared, route *ShadowReturnInbox) bool {
	changed := false
	for id, prior := range route.Events {
		if prior.Question == nil {
			continue
		}
		facts := s.shadowQuestionFactsLocked(*shared, prior.Question.Source)
		event := s.shadowQuestionReturnEventLocked(*shared, *prior.Question, prior, facts)
		if event.Item.Version != prior.Item.Version {
			route.Events[id] = event
			changed = true
		}
	}
	return changed
}
