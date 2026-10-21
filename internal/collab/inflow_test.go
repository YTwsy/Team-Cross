package collab

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/inflow"
	"teamcross/internal/mcp"
	"teamcross/internal/service"
)

func inflowBatch(t *testing.T, a *App, caller mcp.CallerSource) inflow.Batch {
	t.Helper()
	out, err := a.personalInFlow(context.Background(), inFlowCall{Caller: caller, Operation: "prepare"})
	if err != nil {
		t.Fatal(err)
	}
	return workbenchDecode[inflow.Batch](out)
}
func finishInflow(t *testing.T, a *App, c mcp.CallerSource, b inflow.Batch, op string) {
	t.Helper()
	if _, err := a.personalInFlow(context.Background(), inFlowCall{Caller: c, Operation: op, Batch: b}); err != nil {
		t.Fatal(err)
	}
}
func TestInFlowOptInOwnResultsAndDeliveryVersions(t *testing.T) {
	a, _, s := spaceFixture(t)
	_, caller, target := spaceChannel(t, a, s.record.ID)
	_, other, otherTarget := spaceChannel(t, a, s.record.ID)
	if len(inflowBatch(t, a, caller.Caller).Items) != 0 {
		t.Fatal("unrequested delivery")
	}
	wb(t, a, s.record.ID, "inflow-config", workbenchInput{TargetID: target.ID, Enabled: false})
	wb(t, a, s.record.ID, "brief", workbenchInput{Brief: SpaceBrief{Topic: "historical"}})
	wb(t, a, s.record.ID, "inflow-config", workbenchInput{TargetID: target.ID, Enabled: true})
	wb(t, a, s.record.ID, "inflow-config", workbenchInput{TargetID: otherTarget.ID, Enabled: true})
	if len(inflowBatch(t, a, caller.Caller).Items) != 0 {
		t.Fatal("replayed old brief")
	}
	note := workbenchDecode[SpaceNote](wb(t, a, s.record.ID, "inflow-note", workbenchInput{RequestID: uuid.NewString(), TargetID: target.ID, Instruction: "only for this target"}))
	wb(t, a, s.record.ID, "brief", workbenchInput{BaseRevision: 1, Brief: SpaceBrief{Topic: "new"}})
	s.mu.Lock()
	actor := s.workbenchActor(context.Background(), inFlowActor(caller.Caller))
	s.record.Workbench.Requests["own"] = SpaceRequest{ID: "own", Actor: actor, State: "completed", Summary: "own result", UpdatedAt: time.Now()}
	s.record.Workbench.Requests["human"] = SpaceRequest{ID: "human", Actor: SpaceActor{Kind: "human", MemberID: "owner"}, State: "completed", Summary: "not a session", UpdatedAt: time.Now()}
	s.record.Workbench.Requests["queued"] = SpaceRequest{ID: "queued", Actor: SpaceActor{Kind: "human", MemberID: "owner"}, State: "queued", CreatedAt: time.Now().Add(-time.Minute)}
	s.mu.Unlock()
	b := inflowBatch(t, a, caller.Caller)
	if len(b.Items) != 3 || b.Items[0].SourceID != note.ID || b.Items[1].Kind != "brief" || b.Items[2].SourceID != "own" {
		t.Fatal(b)
	}
	if len(inflowBatch(t, a, caller.Caller).Items) != 0 {
		t.Fatal("concurrent transport duplicate")
	}
	finishInflow(t, a, caller.Caller, b, "release")
	b = inflowBatch(t, a, caller.Caller)
	finishInflow(t, a, caller.Caller, b, "delivered")
	s.mu.Lock()
	queuedState := s.record.Workbench.Requests["queued"].State
	s.mu.Unlock()
	if queuedState != "queued" {
		t.Fatal("passive bookkeeping changed a business request", queuedState)
	}
	if len(inflowBatch(t, a, caller.Caller).Items) != 0 {
		t.Fatal("repeated delivered versions")
	}
	otherBatch := inflowBatch(t, a, other.Caller)
	if len(otherBatch.Items) != 1 || otherBatch.Items[0].Kind != "brief" {
		t.Fatal("other session received note or result", otherBatch)
	}
	stored := workbenchDecode[SpaceNote](wb(t, a, s.record.ID, "inflow-note-read", workbenchInput{RequestID: note.ID}))
	if stored.DeliveredAt == nil {
		t.Fatal("delivery not persisted")
	}
	// Model work state is independent of passive transport bookkeeping.
	if got := workbenchDecode[SpaceRequest](wb(t, a, s.record.ID, "request", workbenchInput{RequestID: "own"})); got.State != "completed" || got.ReceivedAt != nil {
		t.Fatal(got)
	}
	var disk Record
	// Use the same storage reload path as a restart, without launching a client.
	if err := readJSON(filepath.Join(s.runtimeDirectory(), "collaboration.json"), &disk); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.inFlowLeases = nil
	s.record.Workbench = nil
	s.mu.Unlock()
	if disk.Workbench == nil {
		t.Fatal("workbench missing in persisted shape")
	}
	s.mu.Lock()
	s.record.Workbench = disk.Workbench
	s.mu.Unlock()
	if len(inflowBatch(t, a, caller.Caller).Items) != 0 {
		t.Fatal("restart replay")
	}
}

type inFlowBrokenWriter struct{}

func (inFlowBrokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestInFlowHookRealHTTPAndOutputFailure(t *testing.T) {
	a, _, s := spaceFixture(t)
	_, caller, target := spaceChannel(t, a, s.record.ID)
	wb(t, a, s.record.ID, "inflow-config", workbenchInput{TargetID: target.ID, Enabled: true})
	note := workbenchDecode[SpaceNote](wb(t, a, s.record.ID, "inflow-note", workbenchInput{RequestID: uuid.NewString(), TargetID: target.ID, Instruction: "quoted external note"}))
	server := httptest.NewServer(a.Handler(http.NotFoundHandler()))
	defer server.Close()
	connection := service.Connection{URL: server.URL, Token: a.Token, DataDir: a.Config.DataDir, Instance: "inflow-fixture"}
	if err := writeJSONFile(filepath.Join(a.Config.DataDir, "connection.json"), connection); err != nil {
		t.Fatal(err)
	}
	if err := mcp.WriteInFlowHook(context.Background(), a.Config.DataDir, "", "", caller.Caller, inFlowBrokenWriter{}); err == nil {
		t.Fatal("expected failed stdout")
	}
	if stored := workbenchDecode[SpaceNote](wb(t, a, s.record.ID, "inflow-note-read", workbenchInput{RequestID: note.ID})); stored.DeliveredAt != nil {
		t.Fatal("failed output recorded delivery")
	}
	var output bytes.Buffer
	if err := mcp.WriteInFlowHook(context.Background(), a.Config.DataDir, "", "", caller.Caller, &output); err != nil {
		t.Fatal(err)
	}
	var hook struct {
		Output struct {
			Event string `json:"hookEventName"`
			Text  string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if json.Unmarshal(output.Bytes(), &hook) != nil || hook.Output.Event != "UserPromptSubmit" || !strings.Contains(hook.Output.Text, "quoted external note") {
		t.Fatal(output.String())
	}
	if len(inflowBatch(t, a, caller.Caller).Items) != 0 {
		t.Fatal("MCP repeated hook-delivered note")
	}
}

func TestInFlowCommentsStayWithExecutionAndRepliesAdvance(t *testing.T) {
	a, reader, s := multiExecutionFixture(t)
	stopWorkbenchPump(a)
	one := createInSpace(t, a, reader, s, "worktree")
	two := createInSpace(t, a, reader, s, "worktree")
	wb(t, a, s.record.ID, "inflow-config", workbenchInput{ExecutionID: one.record.ID, Enabled: true})
	wb(t, a, s.record.ID, "inflow-config", workbenchInput{ExecutionID: two.record.ID, Enabled: true})
	note, err := one.annotate(Annotation{Text: "session opinion without anchor"}, "member")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.annotate(Annotation{Text: "space discussion"}, "member"); err != nil {
		t.Fatal(err)
	}
	c := mcp.CallerSource{Provider: one.record.Provider, SourceID: one.record.SessionID}
	b := inflowBatch(t, a, c)
	if len(b.Items) != 1 || b.Items[0].SourceID != note.ID || b.Items[0].ExecutionID != one.record.ID {
		t.Fatal(b)
	}
	if len(inflowBatch(t, a, mcp.CallerSource{Provider: two.record.Provider, SourceID: two.record.SessionID}).Items) != 0 {
		t.Fatal("sibling received comment")
	}
	if _, err = one.replyAnnotation(context.Background(), AnnotationReplyInput{AnnotationID: note.ID, RequestID: uuid.NewString(), Text: "later reply"}, "another member"); err != nil {
		t.Fatal(err)
	}
	finishInflow(t, a, c, b, "delivered")
	next := inflowBatch(t, a, c)
	if len(next.Items) != 1 || next.Items[0].Version != 2 || next.Items[0].Text != "later reply" {
		t.Fatal("new reply lost to earlier delivery", next)
	}
	wrong := inFlowCall{Caller: mcp.CallerSource{Provider: two.record.Provider, SourceID: two.record.SessionID}, Operation: "delivered", Batch: next}
	if _, err = a.personalInFlow(context.Background(), wrong); err == nil {
		t.Fatal("cross execution receipt accepted")
	}
	finishInflow(t, a, c, next, "release")
	wb(t, a, s.record.ID, "inflow-config", workbenchInput{ExecutionID: one.record.ID, Enabled: false})
	if len(inflowBatch(t, a, c).Items) != 0 {
		t.Fatal("disabled session received update")
	}
}

func TestInFlowRemovalRequiresNewOptInAndPreservesDeliveryBaseline(t *testing.T) {
	a, _, s := spaceFixture(t)
	p, caller, target := spaceChannel(t, a, s.record.ID)
	wb(t, a, s.record.ID, "inflow-config", workbenchInput{TargetID: target.ID, Enabled: true})
	wb(t, a, s.record.ID, "inflow-note", workbenchInput{RequestID: uuid.NewString(), TargetID: target.ID, Instruction: "pending"})
	batch := inflowBatch(t, a, caller.Caller)
	wb(t, a, s.record.ID, "remove", workbenchInput{TargetID: target.ID})
	if _, err := a.registerSpacePairing(context.Background(), s.record.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	if len(inflowBatch(t, a, caller.Caller).Items) != 0 {
		t.Fatal("reassociation silently restored opt-in")
	}
	wb(t, a, s.record.ID, "inflow-config", workbenchInput{TargetID: target.ID, Enabled: true})
	if _, err := a.personalInFlow(context.Background(), inFlowCall{Caller: caller.Caller, Operation: "delivered", Batch: batch}); err == nil {
		t.Fatal("removed association's old lease survived")
	}
	if next := inflowBatch(t, a, caller.Caller); len(next.Items) != 1 || next.Items[0].Text != "pending" {
		t.Fatal("re-opt-in lost undelivered data", next)
	}
}

func TestInFlowReceiverIsBoundToOneSpaceAndAliasesDoNotHideNotes(t *testing.T) {
	a, _, s := spaceFixture(t)
	p, caller, first := spaceChannel(t, a, s.record.ID)
	ctx := context.Background()
	other, err := a.CreateSpace(ctx, SpaceInput{RequestID: uuid.NewString(), Title: "other"})
	if err != nil {
		t.Fatal(err)
	}
	second := workbenchDecode[SpaceTarget](wb(t, a, other.record.ID, "register", workbenchInput{PairingID: p.ID, TargetID: uuid.NewString(), Name: p.Name, Provider: p.Provider, Session: first.Session}))
	a.agentMu.Lock()
	stored := a.agents.Pairings[p.ID]
	stored.Links[other.record.ID] = second.ID
	a.agents.Pairings[p.ID] = stored
	a.agentMu.Unlock()
	wb(t, a, s.record.ID, "inflow-config", workbenchInput{TargetID: first.ID, Enabled: true})
	wb(t, a, other.record.ID, "inflow-config", workbenchInput{TargetID: second.ID, Enabled: true})
	wb(t, a, other.record.ID, "inflow-note", workbenchInput{RequestID: uuid.NewString(), TargetID: second.ID, Instruction: "other space only"})
	receiver := a.newReceiver(receiverRecord{SpaceID: s.record.ID, Record: Record{ID: uuid.NewString(), ExecutionRecord: &ExecutionRecord{Provider: caller.Caller.Provider, SessionID: caller.Caller.SourceID}}})
	out, err := a.runtimeInFlow(ctx, receiver, inFlowCall{Operation: "prepare"})
	if err != nil || len(workbenchDecode[inflow.Batch](out).Items) != 0 {
		t.Fatal("bound receiver saw another linked space", out, err)
	}
	// A second explicit alias must not overwrite the first in an unordered map.
	alias := p
	alias.ID, alias.Links = uuid.NewString(), map[string]string{s.record.ID: uuid.NewString()}
	a.agentMu.Lock()
	a.agents.Pairings[alias.ID] = alias
	a.agentMu.Unlock()
	if _, err = a.registerSpacePairing(ctx, s.record.ID, alias.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = a.configureCurrentInFlow(ctx, s.record.ID, caller.Caller, "", true); err == nil {
		t.Fatal("ambiguous current-session configuration chose an arbitrary alias")
	}
	wb(t, a, s.record.ID, "inflow-note", workbenchInput{RequestID: uuid.NewString(), TargetID: first.ID, Instruction: "original target"})
	out, err = a.runtimeInFlow(ctx, receiver, inFlowCall{Operation: "prepare"})
	batch := workbenchDecode[inflow.Batch](out)
	if err != nil || len(batch.Items) != 1 || batch.Items[0].Text != "original target" {
		t.Fatal("alias hid enabled target", out, err)
	}
}

func TestInFlowThreeCoresScopeRevokeAndNoActiveDelivery(t *testing.T) {
	a, _, s := spaceFixture(t)
	ctx := context.Background()
	if err := s.Share(ctx, "lan"); err != nil {
		t.Fatal(err)
	}
	b, _, _ := fixture(t)
	c, _, _ := fixture(t)
	stopWorkbenchPump(b)
	stopWorkbenchPump(c)
	jb, err := b.Join(ctx, s.sharedRuntime().Token())
	if err != nil {
		t.Fatal(err)
	}
	jc, err := c.Join(ctx, s.sharedRuntime().Token())
	if err != nil {
		t.Fatal(err)
	}
	p, caller, target := spaceChannel(t, b, jb.ID)
	_, third, thirdTarget := spaceChannel(t, c, jc.ID)
	// Passive linked sessions do not need a live channel or native receiver.
	b.agentMu.Lock()
	next := libraryClone(b.agents)
	p = next.Pairings[p.ID]
	p.State = "linked"
	next.Pairings[p.ID] = p
	err = b.saveAgentsLocked(next)
	b.agentMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	wb(t, b, jb.ID, "inflow-config", workbenchInput{TargetID: target.ID, Enabled: true})
	wb(t, c, jc.ID, "inflow-config", workbenchInput{TargetID: thirdTarget.ID, Enabled: true})
	if _, err = c.workbenchCall(ctx, jc.ID, "inflow-config", workbenchInput{TargetID: target.ID, Enabled: false}); err == nil {
		t.Fatal("other member changed preference")
	}
	note := workbenchDecode[SpaceNote](wb(t, a, s.record.ID, "inflow-note", workbenchInput{RequestID: uuid.NewString(), TargetID: target.ID, Instruction: "across Core"}))
	batch := inflowBatch(t, b, caller.Caller)
	if batch.SpaceID != jb.ID || len(batch.Items) != 1 || batch.Items[0].SpaceID != jb.ID || batch.Items[0].SourceID != note.ID {
		t.Fatal(batch)
	}
	if len(inflowBatch(t, c, third.Caller).Items) != 0 {
		t.Fatal("other Core received note")
	}
	finishInflow(t, b, caller.Caller, batch, "delivered")
	if got := workbenchDecode[SpaceNote](wb(t, a, s.record.ID, "inflow-note-read", workbenchInput{RequestID: note.ID})); got.DeliveredAt == nil {
		t.Fatal(got)
	}
	wb(t, a, s.record.ID, "remove", workbenchInput{TargetID: target.ID})
	if len(inflowBatch(t, b, caller.Caller).Items) != 0 {
		t.Fatal("removed association received update")
	}
	if _, err = b.personalInFlow(ctx, inFlowCall{Caller: caller.Caller, Operation: "delivered", Batch: batch}); err == nil {
		t.Fatal("removed target committed receipt")
	}
	wb(t, a, s.record.ID, "inflow-note", workbenchInput{RequestID: uuid.NewString(), TargetID: thirdTarget.ID, Instruction: "revoke before output"})
	pending := inflowBatch(t, c, third.Caller)
	if err = s.RevokeMember(thirdTarget.MemberID); err != nil {
		t.Fatal(err)
	}
	if len(inflowBatch(t, c, third.Caller).Items) != 0 {
		t.Fatal("revoked member received update")
	}
	if _, err = c.personalInFlow(ctx, inFlowCall{Caller: third.Caller, Operation: "delivered", Batch: pending}); err == nil {
		t.Fatal("revoked member committed delivery")
	}
}

func TestInFlowOutputFailureAndSharedHookMCPDedupe(t *testing.T) {
	a, _, s := spaceFixture(t)
	_, caller, target := spaceChannel(t, a, s.record.ID)
	wb(t, a, s.record.ID, "inflow-config", workbenchInput{TargetID: target.ID, Enabled: true})
	wb(t, a, s.record.ID, "inflow-note", workbenchInput{RequestID: uuid.NewString(), TargetID: target.ID, Instruction: "once"})
	var batches []inflow.Batch
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := a.personalInFlow(context.Background(), inFlowCall{Caller: caller.Caller, Operation: "prepare"})
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			batches = append(batches, workbenchDecode[inflow.Batch](out))
			mu.Unlock()
		}()
	}
	wg.Wait()
	count := 0
	var batch inflow.Batch
	for _, b := range batches {
		if len(b.Items) > 0 {
			count++
			batch = b
		}
	}
	if count != 1 {
		t.Fatal("parallel prepare", count)
	}
	finishInflow(t, a, caller.Caller, batch, "release")
	if len(inflowBatch(t, a, caller.Caller).Items) != 1 {
		t.Fatal("known failed output advanced delivery")
	}
	// The bookkeeping API is not available to browser/app or unauthenticated callers.
	r := httptest.NewRequest(http.MethodPost, "/api/inflow", strings.NewReader(`{"operation":"prepare"}`))
	w := httptest.NewRecorder()
	a.Handler(http.NotFoundHandler()).ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
