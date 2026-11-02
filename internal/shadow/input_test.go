package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/nativecodex"
	"teamcross/internal/spacecontext"
)

func currentRun(e *Engine, id string) Run {
	e.mu.Lock()
	defer e.mu.Unlock()
	return clone(e.record.Runs[id])
}
func inputFor(m Message) string { return "message:" + m.ID }

func TestInputIntentMustPersistBeforeStartOrSteer(t *testing.T) {
	for _, method := range []string{"turn/start", "turn/steer"} {
		t.Run(method, func(t *testing.T) {
			e, f, cfg := setup(t)
			m := send(t, e, "A", "Persist before delivery")
			run := Run{ID: "run", Kind: "main", ThreadID: "thread", TurnID: "turn", State: "submitted"}
			batch := InputBatch{ID: "batch", Method: method, TurnID: run.TurnID, State: "prepared", Inputs: []Input{{ID: inputFor(m), Kind: "main", SourceID: m.ID, Message: &m}}}
			e.mu.Lock()
			e.runtime = f
			e.record.Current = run.ThreadID
			if method == "turn/steer" {
				e.record.Runs[run.ID] = run
			}
			e.mu.Unlock()
			file := filepath.Join(cfg.Directory, "shadow.json")
			if err := os.Rename(file, file+".saved"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(file, 0700); err != nil {
				t.Fatal(err)
			}
			var err error
			if method == "turn/steer" {
				err = e.steerInputs(context.Background(), run, batch)
			} else {
				err = e.submit(context.Background(), Run{Kind: "main", ThreadID: run.ThreadID, InputBatches: []InputBatch{batch}}, "input", nil)
			}
			if err == nil || len(f.calls) != 0 || e.View().Messages[0].State != "delivered" || len(currentRun(e, run.ID).InputBatches) != 0 {
				t.Fatal("storage failure leaked an unrecorded native input", err, f.calls)
			}
		})
	}
}

func TestInputLongBodiesAreExplicitPreviewsWithReadableOriginals(t *testing.T) {
	e, _, _ := setup(t)
	m := send(t, e, "A", strings.Repeat("<", 20000)+"ORIGINAL_TAIL")
	in := boundedInput(Input{ID: inputFor(m), Kind: "main", SourceID: m.ID, Message: &m})
	if !in.Truncated || len(jsonText(in)) > maxInputBytes || e.View().Messages[0].Text != m.Text {
		t.Fatal("large message blocked the stream or changed original")
	}
	e.mu.Lock()
	page, err := e.historyPageLocked("", m.ID, 16000)
	e.mu.Unlock()
	if err != nil || !strings.Contains(jsonText(page), "ORIGINAL_TAIL") {
		t.Fatal("original tail is not readable", page, err)
	}
	ref := spacecontext.SourceRef{SpaceID: "space-test", AnnotationID: "original-discussion"}
	raw := json.RawMessage(jsonText(map[string]any{"body": strings.Repeat("evidence", 20000)}))
	direct := boundedInput(Input{ID: "activity:long", Kind: "continuation", Continuation: &ContinuationContext{Sources: []spacecontext.SourceRef{ref}, SourceBodies: []SourceSnapshot{{Source: ref, Data: raw}}}})
	if !direct.Truncated || !direct.Continuation.SourceBodies[0].Partial || len(jsonText(direct)) > maxInputBytes || direct.Continuation.Sources[0].AnnotationID != ref.AnnotationID {
		t.Fatal("large discussion did not retain an exact partial source")
	}
}

func TestInputBatchKeepsAuthorsMemoriesJobsAndReplyTargets(t *testing.T) {
	e, f, _ := setup(t)
	a := send(t, e, "A", "remember concise replies and investigate alpha")
	b := send(t, e, "B", "remember detailed replies and investigate beta")
	e.Start()
	run := active(t, e, "main")
	if len(runInputs(run)) != 2 || run.SourceID != "" {
		t.Fatal("batch inherited a single origin", run)
	}
	f.mu.Lock()
	prompt := itemText(f.threads[run.ThreadID][0].Items[0])
	f.mu.Unlock()
	for _, text := range []string{a.Text, b.Text, inputFor(a), inputFor(b)} {
		if !strings.Contains(prompt, text) {
			t.Fatal("lost input", text)
		}
	}
	for _, m := range []Message{a, b} {
		memory := cloneMap(tool(t, e, f, run, "shadow_remember", map[string]any{"inputId": inputFor(m), "operation": "upsert", "scope": "member", "text": m.Actor.ID + " preference"}))
		if memory["subjectId"] != m.Actor.ID {
			t.Fatal("memory author crossed", memory)
		}
		job := cloneMap(tool(t, e, f, run, "shadow_task", map[string]any{"inputId": inputFor(m), "operation": "start", "title": m.Actor.ID + " job", "prompt": m.Text}))
		if job["sourceId"] != m.ID {
			t.Fatal("job origin crossed", job)
		}
		reply := cloneMap(tool(t, e, f, run, "shadow_reply", map[string]any{"inputId": inputFor(m), "text": "answer for " + m.Actor.ID}))
		if reply["replyTo"] != m.ID {
			t.Fatal("reply target crossed", reply)
		}
	}
	for _, id := range []string{"", "message:invented"} {
		args, _ := json.Marshal(map[string]any{"inputId": id, "operation": "start", "title": "wrong", "prompt": "must refuse"})
		e.mu.Lock()
		_, err := e.toolLocked(toolCall{ThreadID: run.ThreadID, TurnID: run.TurnID, CallID: uuid.NewString(), Tool: "shadow_task", Arguments: args})
		e.mu.Unlock()
		if err == nil {
			t.Fatal("ambiguous origin accepted", id)
		}
	}
	f.complete(run.ThreadID, "internal final must not broadcast to A or B", "completed", true)
	until(t, func() bool { return currentRun(e, run.ID).State == "completed" })
	if messages := e.View().Messages; len(messages) != 4 {
		t.Fatal("multi-input final leaked or explicit reply duplicated", messages)
	}
}

func TestInputSteerUsesSameTurnAndIncrementalPayload(t *testing.T) {
	e, f, _ := setup(t)
	e.Start()
	a := send(t, e, "A", "first question")
	run := active(t, e, "main")
	b := send(t, e, "B", "independent second question")
	until(t, func() bool { return len(runInputs(currentRun(e, run.ID))) == 2 })
	f.mu.Lock()
	calls := clone(f.calls)
	f.mu.Unlock()
	starts, steers := 0, 0
	for _, call := range calls {
		if call.Method == "turn/start" {
			starts++
		}
		if call.Method == "turn/steer" {
			steers++
			if call.Params["expectedTurnId"] != run.TurnID || call.Params["threadId"] != run.ThreadID {
				t.Fatal(call)
			}
			text := call.Params["input"].([]any)[0].(map[string]any)["text"].(string)
			if strings.Contains(text, a.Text) || !strings.Contains(text, b.Text) {
				t.Fatal("steer repeated history or lost increment", text)
			}
			if _, ok := call.Params["model"]; ok {
				t.Fatal("turn override sent to steer")
			}
		}
	}
	if starts != 1 || steers != 1 {
		t.Fatal("extra turn or missing steer", starts, steers)
	}
	tool(t, e, f, run, "shadow_reply", map[string]any{"inputId": inputFor(b), "text": "answer B"})
	if got := e.View().Messages[2].ReplyTo; got != b.ID {
		t.Fatal(got)
	}
}

func TestInputSameDiscussionBurstPreservesEachOriginAndKeepsSteering(t *testing.T) {
	e, f, _, store, first := directAttention(t)
	first.ID, first.Source.ReplyID = "message-a", "question-a"
	first.Actor = spacecontext.Actor{Kind: "human", MemberID: "A", Name: "A"}
	second := clone(first)
	second.ID, second.Seq, second.Source.ReplyID = "message-b", 2, "question-b"
	second.Actor = spacecontext.Actor{Kind: "human", MemberID: "B", Name: "B"}
	acceptAttentionSignals(t, store, first, second)
	e.Start()
	s := active(t, e, "continuation")
	input := runInputs(currentRun(e, s.ID))[0]
	if len(input.Origins) != 2 || input.Origins[0].Actor.MemberID != "A" || input.Origins[1].Actor.MemberID != "B" {
		t.Fatal("discussion batch lost actual authors", input)
	}
	if err := taskToolError(e, s, map[string]any{"operation": "start", "title": "ambiguous", "prompt": "Ambiguous source"}); err == nil {
		t.Fatal("discussion batch silently selected its first source")
	}
	for _, origin := range input.Origins {
		created := cloneMap(tool(t, e, f, s, "shadow_task", map[string]any{"operation": "start", "inputId": origin.ID, "title": "work for " + origin.Actor.MemberID, "prompt": "Investigate the exact question"}))
		j := currentJob(t, e, created["id"].(string))
		if j.SourceID != origin.SourceID || len(j.Trigger.Sources) != 1 || j.Trigger.Sources[0].ReplyID != origin.Source.ReplyID || len(j.Trigger.EventIDs) != 1 {
			t.Fatal("Work inherited another discussion question", j, origin)
		}
	}
	third := clone(second)
	third.ID, third.Seq, third.Source.ReplyID = "message-c", 3, "question-c"
	third.Actor = spacecontext.Actor{Kind: "human", MemberID: "C", Name: "C"}
	acceptAttentionSignals(t, store, third)
	until(t, func() bool { return len(runInputs(currentRun(e, s.ID))) == 2 })
	updated := currentRun(e, s.ID)
	if updated.TurnID != s.TurnID || runInputs(updated)[1].Origins[0].SourceID != third.ID {
		t.Fatal("later same-discussion input waited for native completion or crossed sources", updated)
	}
}

func TestInputDiscussionAndSharedChatStayInOneSessionWithSeparateOrigins(t *testing.T) {
	e, f, _, store, signal := directAttention(t)
	acceptAttentionSignals(t, store, signal)
	batch, err := store.ClaimBatch(e.View().ID, time.Now(), 0, 0)
	if err != nil || batch == nil {
		t.Fatal(batch, err)
	}
	prepareObservation(t, store, batch, 1)
	chat := send(t, e, "B", "independent shared chat request")
	e.Start()
	run := active(t, e, "main")
	until(t, func() bool { return len(runInputs(currentRun(e, run.ID))) == 2 })
	run = currentRun(e, run.ID)
	var discussion Input
	for _, in := range runInputs(run) {
		if in.Kind == "continuation" {
			discussion = in
		}
	}
	if discussion.ID == "" {
		t.Fatal("discussion input missing", run)
	}
	actionID := uuid.NewString()
	tool(t, e, f, run, "reply_to_annotation", map[string]any{"inputId": discussion.ID, "annotationId": signal.Source.AnnotationID, "requestId": actionID, "text": "discussion-only answer"})
	tool(t, e, f, run, "shadow_reply", map[string]any{"inputId": inputFor(chat), "text": "shared-chat-only answer"})
	job := cloneMap(tool(t, e, f, run, "shadow_task", map[string]any{"inputId": discussion.ID, "operation": "start", "title": "discussion work", "prompt": "investigate original discussion"}))
	e.mu.Lock()
	action := e.record.Actions[actionID]
	j := e.record.Jobs[job["id"].(string)]
	e.mu.Unlock()
	if action.EventID != discussion.EventID || action.SourceID != signal.ID || j.Trigger == nil || j.Trigger.ContinuationID != discussion.EventID || j.SourceID != signal.ID {
		t.Fatal("discussion provenance crossed", action, j)
	}
	for _, message := range e.View().Messages {
		if strings.Contains(message.Text, "discussion-only answer") {
			t.Fatal("discussion leaked to shared chat")
		}
	}
	f.complete(run.ThreadID, "internal summary contains both conversations", "completed", true)
	until(t, func() bool { return store.Snapshot().Continuations[discussion.EventID].State == "processed" })
	if messages := e.View().Messages; len(messages) != 2 || messages[1].ReplyTo != chat.ID {
		t.Fatal("mixed final or wrong shared reply", messages)
	}
	if len(e.View().Segments) != 1 {
		t.Fatal("second main session created")
	}
}

func TestInputSteerKnownEndedTurnRetriesOnlyAfterHistory(t *testing.T) {
	e, f, _ := setup(t)
	e.Start()
	send(t, e, "A", "first")
	first := active(t, e, "main")
	f.complete(first.ThreadID, "first answer", "completed", false)
	b := send(t, e, "B", "next")
	var second Run
	until(t, func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		for _, r := range e.record.Runs {
			if r.ID != first.ID && r.Kind == "main" && r.TurnID != "" && r.State == "submitted" {
				second = clone(r)
				return true
			}
		}
		return false
	})
	if second.ThreadID != first.ThreadID || second.SourceID != b.ID {
		t.Fatal("stale rejection changed session or source", second)
	}
	f.mu.Lock()
	calls := clone(f.calls)
	f.mu.Unlock()
	methods := []string{}
	for _, call := range calls {
		if call.Method == "turn/start" || call.Method == "turn/steer" || call.Method == "thread/turns/list" {
			methods = append(methods, call.Method)
		}
	}
	if strings.Join(methods, ",") != "turn/start,turn/steer,thread/turns/list,turn/start" {
		t.Fatal("rejected input replayed before reconcile", methods)
	}
}

func TestInputSteerLostReceiptReconcilesWithoutReplay(t *testing.T) {
	for _, accepted := range []bool{true, false} {
		t.Run(fmt.Sprint(accepted), func(t *testing.T) {
			e, f, cfg := setup(t)
			e.Start()
			send(t, e, "A", "first")
			run := active(t, e, "main")
			f.mu.Lock()
			f.loseSteerReply = accepted
			f.unknownSteerWithoutAccept = !accepted
			f.mu.Unlock()
			b := send(t, e, "B", "late exact input")
			until(t, func() bool {
				r := currentRun(e, run.ID)
				return len(r.InputBatches) == 2 && r.InputBatches[1].State == "unknown"
			})
			e.Close()
			e2, err := Open(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(e2.Close)
			e2.Start()
			until(t, func() bool { r := currentRun(e2, run.ID); return r.State == "submitted" })
			r := currentRun(e2, run.ID)
			want := "unknown"
			if accepted {
				want = "accepted"
			}
			if r.InputBatches[1].State != want {
				t.Fatal(r.InputBatches)
			}
			if accepted {
				tool(t, e2, f, r, "shadow_reply", map[string]any{"inputId": inputFor(b), "text": "exact restored reply"})
			}
			f.mu.Lock()
			calls := clone(f.calls)
			f.mu.Unlock()
			starts, steers := 0, 0
			for _, call := range calls {
				if call.Method == "turn/start" {
					starts++
				}
				if call.Method == "turn/steer" {
					steers++
				}
			}
			if starts != 1 || steers != 1 {
				t.Fatal("unknown input replayed", starts, steers)
			}
		})
	}
}

func TestWorkSlowCollaborationToolDoesNotBlockMainChat(t *testing.T) {
	e, f, _ := setup(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	e.cfg.CollaborationTools = []map[string]any{{"name": "slow_read", "description": "Read fixture evidence", "annotations": map[string]any{"readOnlyHint": true}, "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}}}
	e.cfg.CallTool = func(ctx context.Context, name string, args map[string]any) (any, error) {
		once.Do(func() { close(entered) })
		select {
		case <-release:
			return map[string]any{"evidence": "ready"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	e.Start()
	send(t, e, "A", "investigate")
	main := active(t, e, "main")
	tool(t, e, f, main, "shadow_task", map[string]any{"operation": "start", "title": "long work", "prompt": "read slow evidence"})
	w := active(t, e, "job")
	f.complete(main.ThreadID, "started", "completed", true)
	until(t, func() bool { return currentRun(e, main.ID).State == "completed" })
	params, _ := json.Marshal(toolCall{ThreadID: w.ThreadID, TurnID: w.TurnID, CallID: "slow-call", Tool: "slow_read", Arguments: json.RawMessage(`{}`)})
	f.mu.Lock()
	handler := f.handler
	f.mu.Unlock()
	handler(nativecodex.Message{ID: json.RawMessage(`"slow-request"`), Method: "item/tool/call", Params: params})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker tool did not start")
	}
	b := send(t, e, "B", "quick question while work is blocked")
	foreground := active(t, e, "main")
	if foreground.ThreadID == w.ThreadID || foreground.SourceID != b.ID {
		t.Fatal("chat routed to worker", foreground)
	}
	tool(t, e, f, foreground, "shadow_reply", map[string]any{"text": "quick answer"})
	f.mu.Lock()
	_, workerReplied := f.replies[`"slow-request"`]
	f.mu.Unlock()
	if workerReplied {
		t.Fatal("worker was not still blocked")
	}
	if currentRun(e, w.ID).State != "submitted" {
		t.Fatal("worker interrupted")
	}
	close(release)
	until(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); _, ok := f.replies[`"slow-request"`]; return ok })
}
