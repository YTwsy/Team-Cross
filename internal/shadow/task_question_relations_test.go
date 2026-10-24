package shadow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/spacecontext"
)

func relationWorker(t *testing.T, e *Engine, f *fakeNative, refs ...spacecontext.SourceRef) Run {
	t.Helper()
	e.Start()
	send(t, e, "owner", "Resolve the related discussion questions")
	run := startWorkingTask(t, e, f, active(t, e, "main"))
	e.mu.Lock()
	err := e.mutateLocked(func() error {
		j := e.record.Jobs[run.JobID]
		j.Trigger.Sources = clone(refs)
		e.record.Jobs[j.ID] = j
		return nil
	})
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func relationAsk(t *testing.T, e *Engine, run Run) map[string]any {
	t.Helper()
	j := currentJob(t, e, run.JobID)
	args := questionCommand()
	args["id"], args["baseVersion"], args["baseRevision"] = j.ID, j.Version, j.Revision
	question := args["question"].(map[string]any)
	question["tool"] = "add_annotation"
	delete(question["arguments"].(map[string]any), "annotationId")
	return args
}

func TestTaskQuestionRelationsChooseExactOriginAndPersistBeforeSend(t *testing.T) {
	e, f, _ := setup(t)
	connectQuestionTools(e)
	s, _ := attachEngineAttention(t, e)
	a := spacecontext.SourceRef{SpaceID: "space-test", AnnotationID: "first-question"}
	b := spacecontext.SourceRef{SpaceID: "space-test", AnnotationID: "second-question", ReplyID: "exact-question"}
	var reads, sends atomic.Int32
	e.cfg.ReadSource = func(_ context.Context, ref spacecontext.SourceRef) (SourceSnapshot, error) {
		reads.Add(1)
		return handlingSnapshot(ref, 1), nil
	}
	base := e.cfg.CallTool
	e.cfg.CallTool = func(ctx context.Context, name string, args map[string]any) (any, error) {
		data, err := os.ReadFile(filepath.Join(e.cfg.Directory, "shadow.json"))
		if err != nil {
			return nil, err
		}
		var disk Record
		if err := json.Unmarshal(data, &disk); err != nil {
			return nil, err
		}
		action := disk.Actions[args["requestId"].(string)]
		q := disk.Questions[action.QuestionID]
		if !q.RelationsRecorded || len(q.RespondsTo) != 1 || !reflect.DeepEqual(q.RespondsTo[0].Source, b) || q.RespondsTo[0].Question != "Confirm the supported version." || !reflect.DeepEqual(action.ResponseSources, []spacecontext.SourceRef{b}) {
			return nil, fmt.Errorf("outgoing question did not durably retain its exact relation: %s", jsonText(q))
		}
		sends.Add(1)
		return base(ctx, name, args)
	}
	run := relationWorker(t, e, f, a, b)
	args := relationAsk(t, e, run)
	if _, err := e.callTaskAsk(context.Background(), askCall(run, args)); err == nil || !strings.Contains(err.Error(), "respondsTo") || sends.Load() != 0 {
		t.Fatal("ambiguous new discussion was sent", err, sends.Load())
	}
	controlHandling(t, s, a, "owner", 0)
	// Taking the first question back must not block work on the second.
	args["question"].(map[string]any)["respondsTo"] = []spacecontext.SourceRef{b, b}
	result, err := e.callTaskAsk(context.Background(), askCall(run, args))
	if err != nil {
		t.Fatal(err)
	}
	q, _ := questionPair(t, result)
	if sends.Load() != 1 || reads.Load() != 1 || len(q.RespondsTo) != 1 {
		t.Fatal("relation was duplicated or question was not sent", q, reads.Load(), sends.Load())
	}
	// Re-reading a saved receipt must not reinterpret its source or reread it.
	again, err := e.callTaskAsk(context.Background(), askCall(run, args))
	if err != nil || sends.Load() != 1 || reads.Load() != 1 {
		t.Fatal("saved question was repeated", err, sends.Load(), reads.Load())
	}
	q2, _ := questionPair(t, again)
	if !reflect.DeepEqual(q.RespondsTo, q2.RespondsTo) {
		t.Fatal("saved relationship changed")
	}
	e.Close()
	restored, err := Open(e.cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Close)
	if !reflect.DeepEqual(restored.record.Questions[q.ID].RespondsTo, q.RespondsTo) {
		t.Fatal("relationship lost after reopening")
	}
}

func TestTaskQuestionRelationsKeepOriginalQuestionDuringUnknownRetry(t *testing.T) {
	e, f, _ := setup(t)
	connectQuestionTools(e)
	s, _ := attachEngineAttention(t, e)
	a := spacecontext.SourceRef{SpaceID: "space-test", AnnotationID: "original"}
	b := spacecontext.SourceRef{SpaceID: "space-test", AnnotationID: "later-work"}
	var sends atomic.Int32
	e.cfg.CallTool = func(context.Context, string, map[string]any) (any, error) {
		sends.Add(1)
		return nil, fmt.Errorf("write receipt lost")
	}
	run := relationWorker(t, e, f, a)
	args := relationAsk(t, e, run)
	result, err := e.callTaskAsk(context.Background(), askCall(run, args))
	if err != nil {
		t.Fatal(err)
	}
	q, _ := questionPair(t, result)
	if q.Delivery != "unknown" || len(q.RespondsTo) != 1 || questionSourceID(q.RespondsTo[0].Source) != questionSourceID(a) {
		t.Fatal(q)
	}
	controlHandling(t, s, a, "owner", 0)
	e.mu.Lock()
	j := e.record.Jobs[run.JobID]
	j.Trigger.Sources = []spacecontext.SourceRef{b}
	e.record.Jobs[j.ID] = j
	e.mu.Unlock()
	_, err = e.callTaskAsk(context.Background(), askCall(run, args))
	if err != nil || sends.Load() != 1 {
		t.Fatal("retry rebound the action to later work and bypassed owner takeover", err, sends.Load())
	}
	e.mu.Lock()
	got := clone(e.record.Actions[q.ActionID].ResponseSources)
	e.mu.Unlock()
	if !reflect.DeepEqual(got, []spacecontext.SourceRef{a}) {
		t.Fatal("action sources changed", got)
	}
}

func TestTaskQuestionRelationsCannotRemoveActualReplyTarget(t *testing.T) {
	e, _, _ := setup(t)
	connectQuestionTools(e)
	s, _ := attachEngineAttention(t, e)
	ref := spacecontext.SourceRef{SpaceID: "space-test", AnnotationID: "discussion"}
	controlHandling(t, s, ref, "owner", 0)
	e.Start()
	send(t, e, "owner", "Reply to this question")
	args := questionCommand()
	args["question"].(map[string]any)["respondsTo"] = []spacecontext.SourceRef{}
	_, err := e.callTaskAsk(context.Background(), askCall(active(t, e, "main"), args))
	if !errors.Is(err, errQuestionOwned) {
		t.Fatal("empty explicit relations removed the actual reply target", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.record.Actions) != 0 || len(e.record.Questions) != 0 || len(e.record.Jobs) != 0 {
		t.Fatal("blocked send left a phantom task")
	}
}

func TestTaskQuestionRelationsSlowReadDoesNotBlockChatOrCommitStaleTask(t *testing.T) {
	e, f, _ := setup(t)
	connectQuestionTools(e)
	ref := spacecontext.SourceRef{SpaceID: "space-test", AnnotationID: "original"}
	started, release := make(chan struct{}), make(chan struct{})
	finished := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() {
		once.Do(func() { close(release) })
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Error("question read did not finish")
		}
	})
	e.cfg.ReadSource = func(_ context.Context, source spacecontext.SourceRef) (SourceSnapshot, error) {
		close(started)
		<-release
		return handlingSnapshot(source, 0), nil
	}
	run := relationWorker(t, e, f, ref)
	args := relationAsk(t, e, run)
	args["question"].(map[string]any)["respondsTo"] = []spacecontext.SourceRef{ref}
	done := make(chan error, 1)
	go func() {
		defer close(finished)
		_, err := e.callTaskAsk(context.Background(), askCall(run, args))
		done <- err
	}()
	<-started
	chat := make(chan error, 1)
	go func() {
		_, err := e.Receive(Message{ID: uuid.NewString(), Actor: Actor{ID: "owner", Name: "Owner"}, Text: "A separate message"})
		chat <- err
	}()
	select {
	case err := <-chat:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("source read locked normal chat")
	}
	e.mu.Lock()
	j := e.record.Jobs[run.JobID]
	j.Version++
	e.record.Jobs[j.ID] = j
	e.mu.Unlock()
	once.Do(func() { close(release) })
	if err := <-done; err == nil {
		t.Fatal("late read sent a question for an obsolete task")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.record.Actions) != 0 || len(e.record.Questions) != 0 {
		t.Fatal("late read left a pending action")
	}
}

func TestTaskQuestionRelationsRejectMissingOrMismatchedSourcesBeforeCreatingWork(t *testing.T) {
	for _, scenario := range []string{"other-space", "wrong-source", "withdrawn", "missing-reply", "too-many"} {
		t.Run(scenario, func(t *testing.T) {
			e, _, _ := setup(t)
			connectQuestionTools(e)
			e.cfg.ReadSource = func(_ context.Context, ref spacecontext.SourceRef) (SourceSnapshot, error) {
				snapshot := handlingSnapshot(ref, 0)
				switch scenario {
				case "wrong-source":
					snapshot.Source.AnnotationID = "different-question"
				case "withdrawn":
					snapshot.Unavailable = "withdrawn"
				case "missing-reply":
					snapshot.Data = json.RawMessage(`{"annotations":[{"id":"discussion","text":"root"}],"replyCount":0}`)
				}
				return snapshot, nil
			}
			e.cfg.CallTool = func(context.Context, string, map[string]any) (any, error) {
				t.Error("invalid relation reached business write")
				return nil, fmt.Errorf("must not send")
			}
			e.Start()
			send(t, e, "owner", "Ask the related question")
			ref := spacecontext.SourceRef{SpaceID: "space-test", AnnotationID: "discussion", ReplyID: "exact-reply"}
			if scenario == "other-space" {
				ref.SpaceID = "elsewhere"
			}
			refs := []spacecontext.SourceRef{ref}
			if scenario == "too-many" {
				for range 8 {
					refs = append(refs, ref)
				}
			}
			args := questionCommand()
			args["question"].(map[string]any)["respondsTo"] = refs
			if _, err := e.callTaskAsk(context.Background(), askCall(active(t, e, "main"), args)); err == nil {
				t.Fatal("invalid relation accepted")
			}
			e.mu.Lock()
			defer e.mu.Unlock()
			if len(e.record.Questions) != 0 || len(e.record.Actions) != 0 || len(e.record.Jobs) != 0 {
				t.Fatal("invalid relation left phantom work")
			}
		})
	}
}
