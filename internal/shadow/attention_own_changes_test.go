package shadow

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"teamcross/internal/spacecontext"
)

func TestAttentionOwnChangeUpdatesUnderstandingWithoutNewWork(t *testing.T) {
	e, native, _ := setup(t)
	store, question := attachEngineAttention(t, e)
	e.cfg.ReadSource = func(_ context.Context, ref spacecontext.SourceRef) (SourceSnapshot, error) {
		text := "Which minimum client version should we support?"
		if ref.ReplyID == "owner-answer" {
			text = "The minimum version is v3; I have updated the compatibility note."
		}
		return SourceSnapshot{Source: ref, Data: json.RawMessage(jsonText(map[string]string{"text": text})), ObservedAt: time.Now()}, nil
	}
	acceptAttentionSignals(t, store, question)
	e.Start()
	first := active(t, e, "attention")
	native.complete(first.ThreadID, jsonText(Observation{Summary: "The minimum client version is unresolved.", Evidence: []spacecontext.SourceRef{question.Source}, OpenQuestions: []string{"Which minimum client version?"}, Outcome: "no_action"}), "completed", true)
	until(t, func() bool { return attentionCommitted(e, first.ID) })
	change := clone(question)
	change.ID, change.Seq, change.Source.ReplyID = "owner-update", 2, "owner-answer"
	change.Actor = spacecontext.Actor{Kind: "human", MemberID: e.View().Owner.ID, Name: "Owner"}
	change.Reasons = []string{"own_source_change", "follow_discussion"}
	acceptAttentionSignals(t, store, change)
	run := active(t, e, "attention")
	native.mu.Lock()
	prompt := itemText(native.threads[run.ThreadID][0].Items[0])
	native.mu.Unlock()
	parts := strings.SplitN(prompt, "\nBounded observation context:\n", 2)
	var packet struct {
		MemberID      string                   `json:"memberId"`
		MemberChanges []spacecontext.SourceRef `json:"memberChanges"`
		Focus         Focus                    `json:"focus"`
		Sources       []SourceSnapshot         `json:"sources"`
	}
	if len(parts) != 2 || json.Unmarshal([]byte(parts[1]), &packet) != nil || packet.MemberID != change.Actor.MemberID || !reflect.DeepEqual(packet.MemberChanges, []spacecontext.SourceRef{change.Source}) || len(packet.Focus.OpenQuestions) != 1 || len(packet.Sources) != 1 || !strings.Contains(string(packet.Sources[0].Data), "minimum version is v3") {
		t.Fatal("own change lost the existing question, source or member identity", prompt)
	}
	native.complete(run.ThreadID, jsonText(Observation{Summary: "The member confirmed v3 and updated the note.", Evidence: []spacecontext.SourceRef{change.Source}, OpenQuestions: []string{}, Outcome: "no_action"}), "completed", true)
	until(t, func() bool { return attentionCommitted(e, run.ID) })
	record := store.Snapshot()
	focus := record.Focuses[record.Events[change.ID].FocusID]
	if focus.ProcessedSeq != 2 || len(focus.OpenQuestions) != 0 || len(record.Continuations) != 0 || len(e.View().Jobs) != 0 || len(e.View().Messages) != 0 {
		t.Fatal("private fact synchronization created work or shared chat", focus, record.Continuations, e.View())
	}
	acceptAttentionSignals(t, store, change)
	if batch, err := store.ClaimBatch(e.View().ID, time.Now().Add(time.Hour), 0, 0); err != nil || batch != nil {
		t.Fatal("replayed own change started another analysis", batch, err)
	}
}

func TestAttentionDirectBatchPreservesConcurrentMemberChange(t *testing.T) {
	store, receipt, direct := attentionFixture(t)
	direct.Reasons = []string{"mention"}
	own := clone(direct)
	own.ID, own.Seq, own.Source.ReplyID = "owner-answer", direct.Seq+1, "owner-answer"
	_, member := store.Identity()
	own.Actor = spacecontext.Actor{Kind: "human", MemberID: member, Name: "Owner"}
	own.Reasons = []string{"own_source_change", "follow_discussion"}
	acceptAttentionSignals(t, store, direct, own)
	batch, err := store.ClaimBatch(receipt.Route.ShadowID, time.Now(), time.Second, 10*time.Second)
	if err != nil || batch == nil || batch.Mode != "direct" {
		t.Fatal(batch, err)
	}
	prepareObservation(t, store, batch, 2)
	packet, err := store.ClaimContinuation(receipt.Route.ShadowID)
	if err != nil || packet == nil || packet.MemberID != member || !reflect.DeepEqual(packet.MemberChanges, []spacecontext.SourceRef{own.Source}) || len(packet.SourceBodies) != 2 {
		t.Fatal("direct processing lost the member's already supplied answer", packet, err)
	}
}
