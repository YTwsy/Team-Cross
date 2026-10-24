package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/spacecontext"
)

func TestWatchGoalVersionsDoNotRaceObservationRevisions(t *testing.T) {
	s, receipt, signal := attentionFixture(t)
	if err := s.Accept(receipt, []spacecontext.Signal{signal}); err != nil {
		t.Fatal(err)
	}
	id := attentionHash(signal.Subject)
	in := WatchInput{Operation: "update", ID: id, Goal: "Watch old client compatibility", BaseGoalVersion: 1, RequestID: "goal-update"}
	if _, err := s.BeginWatch(in, "actual-owner-message"); err != nil {
		t.Fatal(err)
	}
	batch, _ := s.ClaimBatch(receipt.Route.ShadowID, time.Now(), 0, 0)
	body, _ := observationReader(context.Background(), signal.Source)
	if _, err := s.PrepareBatch(batch.ID, []SourceSnapshot{body}); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishBatch(batch.ID, Observation{Summary: "Current fact saved while goal request was pending", Outcome: "no_action"}); err != nil {
		t.Fatal(err)
	}
	f, err := s.FinishWatch(in.RequestID)
	if err != nil || f.GoalVersion != 2 || f.Goal != in.Goal || f.Summary == "" || f.GoalSourceID != "actual-owner-message" {
		t.Fatal("observation revision incorrectly invalidated the goal edit", f, err)
	}
	for _, state := range []string{"pause", "resume", "complete"} {
		cmd := WatchInput{Operation: state, ID: id, BaseGoalVersion: f.GoalVersion, RequestID: state}
		if _, err := s.BeginWatch(cmd, "actual-owner-message"); err != nil {
			t.Fatal(err)
		}
		old := f.GoalVersion
		f, err = s.FinishWatch(cmd.RequestID)
		if err != nil || s.ValidateGoal(id, old) == nil {
			t.Fatal("changed goal did not fence old actions", state, err)
		}
	}
	if f.State != "completed" || f.Summary == "" || f.Goal != in.Goal {
		t.Fatal("completing the goal discarded understanding", f)
	}
	reopened, err := OpenAttention(s.directory, "space", "owner")
	if err != nil {
		t.Fatal(err)
	}
	receiptBefore := reopened.Snapshot().WatchCommands[in.RequestID]
	again, err := reopened.BeginWatch(in, "a-later-owner-message")
	if err != nil || !reflect.DeepEqual(again, receiptBefore) || again.Result.GoalVersion != 2 {
		t.Fatal("retry after completion or restart replayed an earlier edit", again, err)
	}
	in.Goal = "different goal with reused request ID"
	if _, err := reopened.BeginWatch(in, "owner"); err == nil {
		t.Fatal("changed command reused the receipt")
	}
}

func TestWatchReservesBeforeRemoteAndRecoversLostFollowReceipt(t *testing.T) {
	e, f, cfg := setup(t)
	s, _ := attachEngineAttention(t, e)
	var mu sync.Mutex
	calls, writes := 0, 0
	remote := map[string]spacecontext.FollowSetting{}
	e.cfg.EnsureFollow = func(_ context.Context, subject spacecontext.Subject, id string) (spacecontext.FollowSetting, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if value, ok := remote[id]; ok {
			return value, nil
		}
		if len(s.Snapshot().WatchCommands) != 1 {
			t.Fatal("remote follow was called before durable intent")
		}
		writes++
		remote[id] = spacecontext.FollowSetting{Subject: subject, Mode: "following", Revision: 1}
		return spacecontext.FollowSetting{}, fmt.Errorf("response lost after host saved")
	}
	cfg.EnsureFollow = e.cfg.EnsureFollow
	e.Start()
	send(t, e, "owner", "Follow this discussion and focus on compatibility")
	run := active(t, e, "main")
	in := WatchInput{Operation: "start", Subject: spacecontext.Subject{Kind: "discussion", ID: "discussion"}, Goal: "Track compatibility", RequestID: "owner-follow"}
	args, _ := json.Marshal(in)
	call := toolCall{ThreadID: run.ThreadID, TurnID: run.TurnID, CallID: uuid.NewString(), Tool: "shadow_watch", Arguments: args}
	if _, err := e.callWatch(context.Background(), call); err == nil {
		t.Fatal("lost remote receipt claimed the whole watch saved")
	}
	if pending := s.Snapshot(); pending.WatchCommands[in.RequestID].State != "pending" || pending.Focuses[attentionHash(in.Subject)].State != "paused" {
		t.Fatal("incomplete watch was not recoverable", pending)
	}
	// Finish the native turn and restart the actual engine; recovery reads the
	// same shared attention record, then uses the original business command.
	f.complete(run.ThreadID, "Follow confirmation is pending.", "completed", true)
	until(t, func() bool { return !e.View().Busy })
	e.Close()
	restored, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Close)
	if err := restored.AttachAttention(s); err != nil {
		t.Fatal(err)
	}
	restored.Start()
	send(t, restored, "owner", "Continue the same follow request")
	run = active(t, restored, "main")
	call.ThreadID, call.TurnID, call.CallID = run.ThreadID, run.TurnID, uuid.NewString()
	out, err := restored.callWatch(context.Background(), call)
	if err != nil || out.(Focus).GoalVersion != 1 || out.(Focus).State != "active" {
		t.Fatal(out, err)
	}
	if _, err := restored.callWatch(context.Background(), call); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 2 || writes != 1 {
		t.Fatal("watch receipt recovery duplicated the remote mutation", calls, writes)
	}
}

func TestWatchFailedIntentNeverCallsRemoteAndOtherAuthorsDoNotOwnPreferences(t *testing.T) {
	e, f, _ := setup(t)
	s, _ := attachEngineAttention(t, e)
	called := false
	e.cfg.EnsureFollow = func(context.Context, spacecontext.Subject, string) (spacecontext.FollowSetting, error) {
		called = true
		return spacecontext.FollowSetting{}, nil
	}
	e.Start()
	send(t, e, "owner", "Follow this object")
	run := active(t, e, "main")
	path := filepath.Join(s.directory, "attention.json")
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(WatchInput{Operation: "start", Subject: spacecontext.Subject{Kind: "discussion", ID: "d"}, RequestID: "save-failure"})
	call := toolCall{ThreadID: run.ThreadID, TurnID: run.TurnID, CallID: uuid.NewString(), Tool: "shadow_watch", Arguments: args}
	if _, err := e.callWatch(context.Background(), call); err == nil || called || len(s.Snapshot().WatchCommands) != 0 {
		t.Fatal("failed watch intent reached remote business state", err, called)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	f.complete(run.ThreadID, "Recorded the storage problem.", "completed", true)
	until(t, func() bool { return !e.View().Busy })
	send(t, e, "another-member", "Show me the owner's private watch list")
	run = active(t, e, "main")
	call.ThreadID, call.TurnID, call.Arguments = run.ThreadID, run.TurnID, json.RawMessage(`{"operation":"list"}`)
	if _, err := e.callWatch(context.Background(), call); err == nil {
		t.Fatal("a shared participant became the owner of private preferences")
	}
}
