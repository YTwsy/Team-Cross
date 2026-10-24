package shadow

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"teamcross/internal/spacecontext"
)

// Understanding is shared by this member's tasks. Receiving an answer in a
// task does not mean its facts have been reconciled into that understanding.
type TaskUnderstandingTarget struct {
	ID          string               `json:"id"`
	Subject     spacecontext.Subject `json:"subject"`
	Current     TaskContextSnapshot  `json:"current"`
	Answers     []string             `json:"answers"`
	AnswerCount int                  `json:"answerCount"`
}

type TaskUnderstandingUpdate struct {
	ID            string                   `json:"id"`
	BaseRevision  uint64                   `json:"baseRevision"`
	SignalIDs     []string                 `json:"signalIds"`
	Summary       string                   `json:"summary"`
	Evidence      []spacecontext.SourceRef `json:"evidence"`
	OpenQuestions []string                 `json:"openQuestions"`
}

type TaskUnderstandingCommand struct {
	Fingerprint string            `json:"fingerprint"`
	Revisions   map[string]uint64 `json:"revisions"`
}

type taskUnderstandingInput struct {
	ID           string                    `json:"id"`
	BaseVersion  int                       `json:"baseVersion"`
	BaseRevision uint64                    `json:"baseRevision"`
	RequestID    string                    `json:"requestId"`
	Updates      []TaskUnderstandingUpdate `json:"understandings"`
}

type understandingAnswer struct {
	ID     string
	Key    string
	Source spacecontext.SourceRef
}

type understandingTarget struct {
	Focus   Focus
	Answers []understandingAnswer
}

func understandingSubject(ref spacecontext.SourceRef) (spacecontext.Subject, bool) {
	switch {
	case ref.AnnotationID != "":
		return spacecontext.Subject{Kind: "discussion", ID: ref.AnnotationID, ExecutionID: ref.ExecutionID}, true
	case ref.MaterialID != "":
		return spacecontext.Subject{Kind: "material", ID: ref.MaterialID}, true
	case ref.ExecutionID != "":
		return spacecontext.Subject{Kind: "execution", ID: ref.ExecutionID}, true
	}
	return spacecontext.Subject{}, false
}

func understandingAnswerKey(focusID string, signal DependencySignal) string {
	// Reply bodies are immutable. Dependency discovery and source discovery
	// can include different optional material metadata for the same reply.
	if signal.Source.AnnotationID != "" && signal.State != "source_resync_partial" {
		ref := signal.Source
		signal.Source = spacecontext.SourceRef{SpaceID: ref.SpaceID, ExecutionID: ref.ExecutionID, AnnotationID: ref.AnnotationID, ReplyID: ref.ReplyID}
		signal.ID = "discussion-source"
	}
	return attentionHash([]any{focusID, signal.ID, signal.Source})
}

func understoodAnswer(record AttentionRecord, focusID string, signal DependencySignal) bool {
	return record.UnderstoodAnswers[understandingAnswerKey(focusID, signal)] ||
		record.UnderstoodAnswers[attentionHash([]any{focusID, signal.ID, signal.Source})]
}

// Facts already reconciled by a task need no second ordinary observation.
// Direct questions still retain their action path: understanding an incoming
// question's facts is not the same as answering that question.
func reconcileUnderstoodEvents(record *AttentionRecord) {
	changed := map[string]bool{}
	for id, event := range record.Events {
		if event.State != "queued" || event.Signal.Follow != nil || signalDirect(event.Signal) || !understoodAnswer(*record, event.FocusID, DependencySignal{ID: event.Signal.ID, Source: event.Signal.Source}) {
			continue
		}
		event.State = "processed"
		record.Events[id] = event
		// Individual receipts may be reconciled out of order. They must not
		// advance the ordinary observation prefix past other unread inputs.
		changed[event.FocusID] = true
	}
	for id := range changed {
		f := record.Focuses[id]
		if f.Processing != "queued" {
			continue
		}
		queued := false
		for _, event := range record.Events {
			queued = queued || event.FocusID == id && event.State == "queued"
		}
		if !queued {
			f.Processing = "idle"
			record.Focuses[id] = f
		}
	}
}

// The store lock protects both the current facts and their answer receipts.
// Targets are derived from exact discussion/source and registered question
// relations; an arbitrary model-supplied focus ID cannot expand the scope.
func understandingTargets(record AttentionRecord, job Job, questions map[string]TaskQuestion) map[string]understandingTarget {
	targets := map[string]understandingTarget{}
	for _, dependency := range job.Dependencies {
		for _, signal := range dependency.Signals {
			if signal.Source.SpaceID != record.SpaceID {
				continue
			}
			focuses := map[string]Focus{}
			addSource := func(source spacecontext.SourceRef) {
				if source.SpaceID != record.SpaceID {
					return
				}
				if subject, ok := understandingSubject(source); ok {
					id := attentionHash(subject)
					f, exists := record.Focuses[id]
					if !exists {
						f = Focus{ID: id, Subject: subject, GoalVersion: 1, State: "active"}
					}
					focuses[id] = f
				}
			}
			addSource(signal.Source)
			if job.Trigger != nil && record.Focuses[job.Trigger.FocusID].ID != "" {
				focuses[job.Trigger.FocusID] = record.Focuses[job.Trigger.FocusID]
			}
			if q, exists := questions[dependency.QuestionID]; exists {
				for _, relation := range q.RespondsTo {
					addSource(relation.Source)
				}
			}
			for id, f := range focuses {
				key := understandingAnswerKey(id, signal)
				if understoodAnswer(record, id, signal) {
					continue
				}
				target := targets[id]
				target.Focus = clone(f)
				duplicate := false
				for _, old := range target.Answers {
					duplicate = duplicate || old.Key == key
				}
				if !duplicate {
					target.Answers = append(target.Answers, understandingAnswer{signal.ID, key, signal.Source})
				}
				targets[id] = target
			}
		}
	}
	for _, c := range job.Contexts {
		if c.Mode == "snapshot" || c.Source == nil {
			continue
		}
		f, exists := record.Focuses[c.ID]
		if !exists {
			continue
		}
		for _, signal := range c.Source.Signals {
			if signal.Source.SpaceID != record.SpaceID || !sourceSignalMatches(c.Subject, signal.Source) || understoodAnswer(record, c.ID, signal) {
				continue
			}
			key := understandingAnswerKey(c.ID, signal)
			target := targets[c.ID]
			target.Focus = clone(f)
			duplicate := false
			for _, answer := range target.Answers {
				duplicate = duplicate || answer.Key == key
			}
			if !duplicate {
				target.Answers = append(target.Answers, understandingAnswer{signal.ID, key, signal.Source})
			}
			targets[c.ID] = target
		}
	}
	return targets
}

func understandingProjection(targets map[string]understandingTarget) []TaskUnderstandingTarget {
	ids := make([]string, 0, len(targets))
	for id := range targets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := []TaskUnderstandingTarget{}
	for _, id := range ids[:min(8, len(ids))] {
		target := targets[id]
		current := contextSnapshot(target.Focus)
		current.Fingerprint = ""
		item := TaskUnderstandingTarget{ID: id, Subject: target.Focus.Subject, Current: current, Answers: []string{}, AnswerCount: len(target.Answers)}
		for _, answer := range target.Answers[:min(16, len(target.Answers))] {
			item.Answers = append(item.Answers, answer.ID)
		}
		out = append(out, item)
	}
	return out
}

func (e *Engine) taskUnderstandingTargetsLocked(job Job) []TaskUnderstandingTarget {
	if e.attention == nil {
		return nil
	}
	e.attention.mu.Lock()
	defer e.attention.mu.Unlock()
	return understandingProjection(understandingTargets(e.attention.record, job, e.record.Questions))
}

func (e *Engine) taskDetailLocked(job Job) Job {
	out := taskForContext(job)
	out.Understandings = e.taskUnderstandingTargetsLocked(job)
	return out
}

// This operation only writes the member's understanding store. Facts, exact
// answer receipts, and the idempotent command are one durable transaction;
// no second engine write or new model round is needed to recover its result.
func (e *Engine) understandTaskLocked(run Run, in taskUnderstandingInput) (any, error) {
	if e.attention == nil || in.RequestID == "" || len(in.RequestID) > 160 || len(in.Updates) == 0 || len(in.Updates) > 8 {
		return nil, fmt.Errorf("认识回写需要已连接的成员记录、稳定 requestId 和一至八个更新")
	}
	s := e.attention
	s.mu.Lock()
	defer s.mu.Unlock()
	commandID := e.record.ID + ":" + in.RequestID
	fingerprint := attentionHash(in)
	if previous, ok := s.record.UnderstandingCommands[commandID]; ok {
		if previous.Fingerprint != fingerprint {
			return nil, fmt.Errorf("同一认识回写 requestId 不能改变内容")
		}
		return map[string]any{"status": "saved", "revisions": clone(previous.Revisions)}, nil
	}
	job, exists := e.record.Jobs[in.ID]
	if !exists || job.Version != in.BaseVersion || job.Revision != in.BaseRevision || !taskAcceptsAnswers(job) || job.State == "paused" || job.State == "stopping" {
		return nil, fmt.Errorf("任务已变化或停止，请先读取当前任务再更新认识")
	}
	// validateTaskFollowLocked reads the same store; inspect the captured goal
	// under this lock rather than recursively acquiring it.
	if job.Follow != nil {
		f := s.record.Focuses[job.Follow.FocusID]
		if f.GoalVersion != job.Follow.GoalVersion || f.State != "active" || job.Follow.NeedsReplan {
			return nil, fmt.Errorf("关注目标已变化，请先重核任务要求")
		}
	}
	if e.taskRunActiveLocked(job.ID) && (run.Kind != "job" || run.JobID != job.ID || run.JobVersion != job.Version) {
		return nil, fmt.Errorf("该任务仍由原工作轮处理")
	}
	if _, err := e.taskOriginForUnderstandingLocked(run, job); err != nil {
		return nil, err
	}
	targets := understandingTargets(s.record, job, e.record.Questions)
	next := clone(s.record)
	if next.UnderstoodAnswers == nil {
		next.UnderstoodAnswers = map[string]bool{}
	}
	if next.UnderstandingCommands == nil {
		next.UnderstandingCommands = map[string]TaskUnderstandingCommand{}
	}
	revisions := map[string]uint64{}
	for _, update := range in.Updates {
		target, ok := targets[update.ID]
		if !ok || revisions[update.ID] != 0 || update.BaseRevision != target.Focus.Revision {
			return nil, fmt.Errorf("认识对象或版本已变化，请读取任务 understandings 后合并最新事实")
		}
		if strings.TrimSpace(update.Summary) == "" || len(update.Summary) > 8000 || len(update.OpenQuestions) > 16 || len(update.Evidence) > 32 || len(update.SignalIDs) == 0 || len(update.SignalIDs) > 16 {
			return nil, fmt.Errorf("认识回写需要有界的当前结论、未决问题和准确依据")
		}
		for _, question := range update.OpenQuestions {
			if len(question) > 4000 {
				return nil, fmt.Errorf("认识中的未决问题过长")
			}
		}
		known := map[string]bool{}
		answerSources := map[string]bool{}
		selected := map[string]bool{}
		for _, id := range update.SignalIDs {
			if id == "" || selected[id] {
				return nil, fmt.Errorf("认识回写需要唯一且实际提供的答复身份")
			}
			selected[id] = true
		}
		available := map[string]bool{}
		for _, source := range target.Focus.Evidence {
			known[attentionHash(source)] = true
		}
		for _, answer := range target.Answers {
			available[answer.ID] = true
			if selected[answer.ID] {
				known[attentionHash(answer.Source)] = true
				answerSources[attentionHash(answer.Source)] = true
			}
		}
		for id := range selected {
			if !available[id] {
				return nil, fmt.Errorf("认识回写包含不属于该对象的答复")
			}
		}
		for _, source := range run.ReadSources {
			known[attentionHash(source)] = true
		}
		citedAnswer := false
		for _, source := range update.Evidence {
			if source.SpaceID != e.record.SpaceID || !known[attentionHash(source)] {
				return nil, fmt.Errorf("认识依据必须来自实际答复、当前有效认识或本轮已读取来源")
			}
			citedAnswer = citedAnswer || answerSources[attentionHash(source)]
		}
		if !citedAnswer {
			return nil, fmt.Errorf("认识回写需要保留本次实际答复的依据")
		}
		focus := target.Focus
		focus.Summary, focus.Evidence, focus.OpenQuestions = update.Summary, clone(update.Evidence), clone(update.OpenQuestions)
		focus.Revision++
		focus.UnderstandingRevision++
		focus.UpdatedAt = time.Now()
		linked := false
		for _, id := range focus.JobIDs {
			linked = linked || id == job.ID
		}
		if !linked {
			focus.JobIDs = append(focus.JobIDs, job.ID)
		}
		if len(focus.JobIDs) > 32 {
			focus.JobIDs = focus.JobIDs[len(focus.JobIDs)-32:]
		}
		if focus.Processing == "" || focus.Processing == "task" {
			focus.Processing = "idle"
		}
		for _, answer := range target.Answers {
			if selected[answer.ID] {
				next.UnderstoodAnswers[answer.Key] = true
			}
		}
		next.Focuses[focus.ID] = focus
		revisions[focus.ID] = focus.Revision
	}
	next.UnderstandingCommands[commandID] = TaskUnderstandingCommand{fingerprint, revisions}
	reconcileUnderstoodEvents(&next)
	if err := s.saveLocked(next); err != nil {
		return nil, err
	}
	e.notify()
	return map[string]any{"status": "saved", "revisions": revisions, "next": "Read task status, reconcile any changed linked contexts, then continue, wait or finish the original task."}, nil
}

// The caller holds both locks. A real current member turn may coordinate an
// idle task; a worker may only reconcile its own actual task version.
func (e *Engine) taskOriginForUnderstandingLocked(run Run, job Job) (bool, error) {
	if run.Kind == "job" || run.Kind == "result" {
		if run.JobID == job.ID && run.JobVersion == job.Version {
			return true, nil
		}
	} else if run.Kind == "main" && e.hasMessageLocked(run.SourceID) {
		return true, nil
	} else if run.Kind == "continuation" && e.attention.record.Continuations[run.EventID].ShadowID == e.record.ID {
		return true, nil
	}
	return false, fmt.Errorf("认识回写缺少当前任务或真实协作来源")
}
