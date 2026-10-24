package shadow

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"teamcross/internal/spacecontext"
)

func TestQuestionIndexKeepsEachTasksConditionAndActualProgress(t *testing.T) {
	e, _, _ := setup(t)
	q := TaskQuestion{ID: "question", Text: "Which client version?", Condition: "A version and a migration date", Delivery: "saved", Source: spacecontext.SourceRef{SpaceID: "space-test", AnnotationID: "discussion", ReplyID: "actual-question"}, UpdatedAt: time.Now()}
	e.record.Questions[q.ID] = q
	for i := range 2 {
		id := fmt.Sprint(i)
		d := TaskDependency{ID: "condition-" + id, QuestionID: q.ID, State: "waiting", Condition: "Requirement " + id}
		if i == 0 {
			d.Signals = []DependencySignal{{ID: "partial", State: "answered"}}
		}
		e.record.Jobs[id] = Job{ID: id, Title: "Task " + id, Version: 2, State: "waiting", Prompt: "PRIVATE-TASK-PROMPT", ThreadID: "PRIVATE-NATIVE-SESSION", Dependencies: []TaskDependency{d}, Checkpoint: &TaskCheckpoint{NextStep: "Next " + id}}
	}
	detail, err := e.QuestionDetail(q.ID, "", 1)
	if err != nil || detail.State != "waiting" || detail.TotalTasks != 2 || detail.TotalConditions != 2 || len(detail.Tasks) != 1 || detail.NextCursor == "" {
		t.Fatal(detail, err)
	}
	second, err := e.QuestionDetail(q.ID, detail.NextCursor, 1)
	if err != nil || second.Tasks[0].Condition == detail.Tasks[0].Condition || second.Tasks[0].NextStep == detail.Tasks[0].NextStep || detail.Tasks[0].AnswerReceived == second.Tasks[0].AnswerReceived {
		t.Fatal("shared question collapsed task-specific state", detail, second, err)
	}
	b, _ := json.Marshal(detail)
	if strings.Contains(string(b), "PRIVATE-") {
		t.Fatal("private runtime internals exposed", string(b))
	}
	j := e.record.Jobs["1"]
	j.Dependencies[0].Signals = []DependencySignal{{ID: "other-answer", State: "answered"}}
	e.record.Jobs[j.ID] = j
	if _, err := e.QuestionDetail(q.ID, detail.NextCursor, 1); err == nil {
		t.Fatal("stale task page accepted")
	}
	page, err := e.QuestionPage("", false, 20)
	if err != nil || page.Items[0].State != "answer_received" {
		t.Fatal(page, err)
	}
	for id, j := range e.record.Jobs {
		j.Dependencies[0].State = "satisfied"
		j.State = "queued"
		e.record.Jobs[id] = j
	}
	page, _ = e.QuestionPage("", false, 20)
	if page.Items[0].State != "conditions_met" || page.Items[0].ActiveTasks != 2 {
		t.Fatal("satisfied conditions pretended the tasks were completed", page)
	}
	for id, j := range e.record.Jobs {
		j.State = "completed"
		e.record.Jobs[id] = j
	}
	page, _ = e.QuestionPage("", false, 20)
	history, _ := e.QuestionPage("", true, 20)
	if page.Total != 0 || history.Total != 1 || history.Items[0].State != "closed" {
		t.Fatal("closed work remained an active question", page, history)
	}
}

func TestQuestionIndexBoundsPagesAndKeepsUnsentSourcesUnavailable(t *testing.T) {
	e, _, _ := setup(t)
	for i := range 35 {
		id := fmt.Sprintf("q-%02d", i)
		e.record.Questions[id] = TaskQuestion{ID: id, Text: strings.Repeat("问题", 400), Condition: strings.Repeat("条件", 1000), Source: spacecontext.SourceRef{SpaceID: "space-test", AnnotationID: "parent-is-not-the-question"}, Delivery: "unknown"}
		e.record.Jobs[id] = Job{ID: id, Title: strings.Repeat("任务", 200), State: "waiting", Dependencies: []TaskDependency{{ID: id, QuestionID: id, State: "waiting", Condition: strings.Repeat("条件", 300)}}, Checkpoint: &TaskCheckpoint{NextStep: strings.Repeat("继续", 300)}}
	}
	page, err := e.QuestionPage("", false, 10)
	if err != nil || len(page.Items) != 10 || page.Total != 35 || page.NextCursor == "" {
		t.Fatal(page, err)
	}
	for _, item := range page.Items {
		if item.Source != nil || item.State != "delivery_unknown" {
			t.Fatal("unsent parent presented as sent question", item)
		}
	}
	if b, _ := json.Marshal(page); len(b) > 48<<10 {
		t.Fatal("index exceeded budget")
	}
	if _, err := e.QuestionPage(page.NextCursor, true, 10); err == nil {
		t.Fatal("cursor accepted a different filter")
	}
	q := e.record.Questions["q-00"]
	q.Delivery = "saved"
	e.record.Questions[q.ID] = q
	for i := range 100 {
		id := fmt.Sprintf("shared-%03d", i)
		e.record.Jobs[id] = Job{ID: id, Title: strings.Repeat("标题", 200), State: "waiting", Dependencies: []TaskDependency{{ID: id, QuestionID: q.ID, State: "waiting", Condition: strings.Repeat("条件", 1000)}}, Checkpoint: &TaskCheckpoint{NextStep: strings.Repeat("继续", 1000)}}
	}
	seen, cursor := map[string]bool{}, ""
	for {
		detail, err := e.QuestionDetail(q.ID, cursor, 16)
		if err != nil || len(detail.Tasks) > 16 || detail.TotalTasks != 101 {
			t.Fatal(detail, err)
		}
		if b, _ := json.Marshal(detail); len(b) > 48<<10 {
			t.Fatal("detail exceeded budget")
		}
		for _, task := range detail.Tasks {
			if seen[task.ID] {
				t.Fatal("page repeated a task")
			}
			seen[task.ID] = true
		}
		cursor = detail.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 101 {
		t.Fatal("paging skipped tasks", len(seen))
	}
	j := e.record.Jobs[q.ID]
	j.Dependencies[0].Signals = []DependencySignal{{ID: "withdrawn", State: "unavailable"}}
	e.record.Jobs[j.ID] = j
	detail, _ := e.QuestionDetail(q.ID, "", 16)
	if detail.State != "unavailable" {
		t.Fatal("withdrawn source reported as an answer", detail.State)
	}
}
