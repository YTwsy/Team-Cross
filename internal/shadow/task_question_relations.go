package shadow

import (
	"context"
	"fmt"
	"sort"

	"teamcross/internal/spacecontext"
)

// A question may advance several earlier questions. This is a business
// relationship, separate from where the new question is posted and from the
// material references used as evidence. Only short source excerpts are kept.
type QuestionRelation struct {
	Source   spacecontext.SourceRef `json:"source"`
	Question string                 `json:"question,omitempty"`
}

func questionRelationSources(relations []QuestionRelation) []spacecontext.SourceRef {
	refs := []spacecontext.SourceRef{}
	for _, r := range relations {
		refs = append(refs, r.Source)
	}
	return uniqueQuestionSources(refs)
}

func (e *Engine) prepareQuestionRelations(ctx context.Context, call toolCall, question askQuestion, requestID string) ([]QuestionRelation, error) {
	e.mu.Lock()
	var run Run
	for _, candidate := range e.record.Runs {
		if activeRun(candidate) && candidate.ThreadID == call.ThreadID && (candidate.TurnID == call.TurnID || candidate.TurnID == "") {
			run = candidate
			break
		}
	}
	if run.ID == "" || call.CallID == "" {
		e.mu.Unlock()
		return nil, fmt.Errorf("提问不属于当前活动轮次")
	}
	if old, ok := e.record.Questions[attentionHash([]any{e.record.ID, requestID})]; ok {
		// The normal ask fingerprint check still runs. Do not reread changed or
		// withdrawn sources just to return a previously saved question receipt.
		relations := clone(old.RespondsTo)
		e.mu.Unlock()
		return relations, nil
	}
	space, reader := e.record.SpaceID, e.cfg.ReadSource
	origins := e.questionOriginsLocked(run)
	refs := e.responseSourcesLocked(question.Tool, question.Arguments, run)
	e.mu.Unlock()

	if question.RespondsTo == nil {
		if question.Tool == "add_annotation" && len(origins) > 1 {
			return nil, fmt.Errorf("当前工作涉及多个问题；用 question.respondsTo 指定这次新讨论推进的准确问题，独立问题可显式使用空数组")
		}
		out := []QuestionRelation{}
		for _, ref := range refs {
			out = append(out, QuestionRelation{Source: ref})
		}
		return out, nil
	}
	if len(*question.RespondsTo) > 8 {
		return nil, fmt.Errorf("一次提问最多关联八个准确问题")
	}
	// An explicit relation replaces the inferred origin, but a real reply
	// still answers its actual replyTo target (or its discussion root).
	if question.Tool == "add_annotation" {
		refs = nil
	} else if len(refs) > 0 {
		refs = refs[:1]
	}
	out := []QuestionRelation{}
	seen := map[string]bool{}
	for _, ref := range *question.RespondsTo {
		if ref.SpaceID == "" {
			ref.SpaceID = space
		}
		if !validQuestionSource(ref, space) || len(ref.AnnotationID) > 256 || len(ref.ReplyID) > 256 || len(ref.ExecutionID) > 256 {
			return nil, fmt.Errorf("提问关联需要本空间的准确讨论或回复")
		}
		ref = questionSource(ref)
		key := questionSourceID(ref)
		if seen[key] {
			continue
		}
		seen[key] = true
		if reader == nil {
			return nil, fmt.Errorf("提问关联的准确来源读取尚未连接")
		}
		snapshot, err := reader(ctx, ref)
		if err != nil {
			return nil, err
		}
		if questionSourceID(snapshot.Source) != key {
			return nil, fmt.Errorf("提问关联返回了其他问题")
		}
		text, _, err := handlingQuestion(snapshot)
		if err != nil {
			return nil, err
		}
		out = append(out, QuestionRelation{Source: ref, Question: clip(text, 600)})
	}
	for _, ref := range refs {
		if !seen[questionSourceID(ref)] {
			out = append(out, QuestionRelation{Source: ref})
		}
	}
	if len(out) > 8 {
		return nil, fmt.Errorf("包括实际回复目标在内，一次提问最多关联八个准确问题")
	}
	return out, nil
}

type QuestionTaskLink struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Version         int    `json:"version"`
	State           string `json:"state"`
	DependencyID    string `json:"dependencyId"`
	Condition       string `json:"condition"`
	DependencyState string `json:"dependencyState"`
	NextStep        string `json:"nextStep,omitempty"`
	Active          bool   `json:"active"`
	AnswerReceived  bool   `json:"answerReceived,omitempty"`
	Unavailable     bool   `json:"unavailable,omitempty"`
}

func (e *Engine) questionTaskLinksLocked(id string) []QuestionTaskLink {
	return e.questionLinksLocked()[id]
}

func (e *Engine) questionLinksLocked() map[string][]QuestionTaskLink {
	byQuestion := map[string][]QuestionTaskLink{}
	for _, job := range e.record.Jobs {
		for _, d := range job.Dependencies {
			if d.QuestionID == "" {
				continue
			}
			next := ""
			if job.Checkpoint != nil {
				next = clip(job.Checkpoint.NextStep, 500)
			}
			unavailable := false
			for _, signal := range d.Signals {
				unavailable = unavailable || signal.State == "unavailable"
			}
			byQuestion[d.QuestionID] = append(byQuestion[d.QuestionID], QuestionTaskLink{ID: job.ID, Title: clip(job.Title, 240), Version: job.Version, State: job.State, DependencyID: d.ID, Condition: clip(d.Condition, 500), DependencyState: d.State, NextStep: next, Active: taskAcceptsAnswers(job), AnswerReceived: len(d.Signals) > 0 && !unavailable, Unavailable: unavailable})
		}
	}
	for id, links := range byQuestion {
		sort.Slice(links, func(i, j int) bool {
			if links[i].Active != links[j].Active {
				return links[i].Active
			}
			return links[i].ID+":"+links[i].DependencyID < links[j].ID+":"+links[j].DependencyID
		})
		byQuestion[id] = links
	}
	return byQuestion
}

func questionTaskCount(links []QuestionTaskLink, activeOnly bool) int {
	seen := map[string]bool{}
	for _, link := range links {
		if !activeOnly || link.Active {
			seen[link.ID] = true
		}
	}
	return len(seen)
}
