package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/nativecodex"
)

func receiverAction(t *testing.T, a *App, s *Session, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	data, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	a.Handler(http.NotFoundHandler()).ServeHTTP(w, httptest.NewRequest("POST", "http://127.0.0.1/api/space-receivers/"+s.record.ID+"/action", strings.NewReader(string(data))))
	return w
}

func waitReceiverReleased(t *testing.T, s *Session) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		released := s.receiverPaused && s.process == nil && s.stopping == nil && !s.online
		s.mu.Unlock()
		if released {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("receiver was not released")
}

func TestReceiverPauseDrainsApprovalsAndAcceptedCalls(t *testing.T) {
	a, f, s := receiverFixture(t)
	id := s.record.SessionID
	receiverEvent(s, "turn/started", id, map[string]any{"turn": map[string]any{"id": "active"}})
	params, _ := json.Marshal(map[string]any{"threadId": id})
	s.onMessage(nativecodex.Message{ID: json.RawMessage(`7`), Method: "mcpServer/elicitation/request", Params: params})
	s.mu.Lock()
	s.activeCalls++
	s.mu.Unlock()
	if w := receiverAction(t, a, s, map[string]any{"action": "pause"}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if !f.Alive() || a.receiverView(s)["releasePending"] != true {
		t.Fatal("pause closed active native work")
	}
	if w := receiverAction(t, a, s, map[string]any{"action": "start"}); w.Code == 200 {
		t.Fatal("resume silently cancelled pending release")
	}
	a.agentMu.Lock()
	pairing := a.agents.Pairings[s.record.RequestID]
	a.agentMu.Unlock()
	if reason := a.nativeAgentReason(pairing); !strings.Contains(reason, "暂停") {
		t.Fatal(reason)
	}
	_, err := s.RPC(context.Background(), "owner", "turn/start", map[string]any{"input": []any{}}, uuid.NewString())
	if err == nil || !strings.Contains(err.Error(), "暂停") {
		t.Fatal("paused RPC accepted new input", err)
	}
	if _, err := a.sendAgentRequest(context.Background(), agentRequestInput{RequestID: uuid.NewString(), PairingID: pairing.ID, Intent: "analyze", Instruction: "must not send", WorkbenchSpaceID: s.receiverSpace}); err == nil {
		t.Fatal("paused pairing accepted delivery")
	}
	receiverEvent(s, "turn/completed", id, map[string]any{"turn": map[string]any{"id": "active"}})
	if !f.Alive() {
		t.Fatal("closed with outstanding approval")
	}
	receiverEvent(s, "serverRequest/resolved", id, map[string]any{"requestId": 7})
	if !f.Alive() {
		t.Fatal("closed with accepted RPC still pending")
	}
	s.finishCall()
	waitReceiverReleased(t, s)
	if f.Alive() {
		t.Fatal("native process still holds the thread")
	}
	// A duplicated pause is a read of the same transition, never a resume.
	if w := receiverAction(t, a, s, map[string]any{"action": "pause"}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
}

func TestReceiverPauseSurvivesRestartAndResumeKeepsIdentity(t *testing.T) {
	a, f, s := receiverFixture(t)
	id, pairingID, spaceID, cwd := s.record.SessionID, s.record.RequestID, s.receiverSpace, s.record.ExecutionCwd
	keep := filepath.Join(cwd, "keep.txt")
	if err := os.WriteFile(keep, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.pauseReceiver(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	waitReceiverReleased(t, s)
	config := a.Config
	a.Close()
	b, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	stopWorkbenchPump(b)
	restored := b.receivers[s.record.ID]
	if !restored.receiverPaused || restored.process != nil || restored.online {
		t.Fatal("restart resumed paused receiver")
	}
	effort := "medium"
	f.mu.Lock()
	thread := f.threads[id]
	thread.Model, thread.ReasoningEffort = "model-selected-in-personal-client", &effort
	f.threads[id] = thread
	before := len(f.calls)
	f.mu.Unlock()
	if w := receiverAction(t, b, restored, map[string]any{"action": "start"}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	v := b.receiverView(restored)
	if v["receivingPaused"] != false || v["online"] != true || v["sessionId"] != id || v["pairingId"] != pairingID || v["spaceId"] != spaceID {
		t.Fatal(v)
	}
	if data, err := os.ReadFile(keep); err != nil || string(data) != "preserve" {
		t.Fatal("resume lost workspace", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.requests["thread/resume"]["threadId"] != id || f.requests["thread/resume"]["model"] != thread.Model {
		t.Fatal("resume changed identity or restored stale model", f.requests["thread/resume"])
	}
	for _, method := range f.calls[before:] {
		if method == "thread/fork" || method == "thread/start" || method == "turn/start" {
			t.Fatal("resume created or replayed input", method)
		}
	}
}

type occupiedReceiverRuntime struct{ *fakeRuntime }

func (p *occupiedReceiverRuntime) Call(ctx context.Context, method string, in, out any) error {
	if method == "thread/resume" {
		return fmt.Errorf(`{"code":-32000,"message":"thread is in use by another application"}`)
	}
	return p.fakeRuntime.Call(ctx, method, in, out)
}

func TestReceiverFailedResumePreservesPauseAndCanRetry(t *testing.T) {
	a, f, s := receiverFixture(t)
	id := s.record.SessionID
	if err := a.pauseReceiver(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	waitReceiverReleased(t, s)
	start := a.Config.StartProcess
	a.Config.StartProcess = func(a, b, c, d string) (Runtime, error) {
		p, err := start(a, b, c, d)
		if err != nil {
			return nil, err
		}
		return &occupiedReceiverRuntime{p.(*fakeRuntime)}, nil
	}
	w := receiverAction(t, a, s, map[string]any{"action": "start"})
	if w.Code == 200 || !strings.Contains(w.Body.String(), "其他应用") {
		t.Fatal(w.Code, w.Body.String())
	}
	v := a.receiverView(s)
	if v["receivingPaused"] != true || v["online"] != false || v["sessionId"] != id || f.Alive() {
		t.Fatal("failed resume changed ownership", v)
	}
	a.Config.StartProcess = start
	if w := receiverAction(t, a, s, map[string]any{"action": "start"}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
}

func TestReceiverWithoutSavedHistoryCannotBeReleased(t *testing.T) {
	a, f, s := receiverFixture(t)
	f.mu.Lock()
	f.history = []MaterialTurn{}
	f.mu.Unlock()
	w := receiverAction(t, a, s, map[string]any{"action": "pause"})
	if w.Code == 200 || !strings.Contains(w.Body.String(), "首轮") {
		t.Fatal(w.Code, w.Body.String())
	}
	if s.receiverPaused || !f.Alive() {
		t.Fatal("lost empty native thread")
	}
}

type closingReceiverRuntime struct {
	*fakeRuntime
	closing chan struct{}
	finish  chan struct{}
}

func (p *closingReceiverRuntime) Close() {
	close(p.closing)
	<-p.finish
	p.fakeRuntime.Close()
}

func TestReceiverRemainsReleasingUntilProcessExits(t *testing.T) {
	a, f, s := receiverFixture(t)
	p := &closingReceiverRuntime{fakeRuntime: f, closing: make(chan struct{}), finish: make(chan struct{})}
	s.mu.Lock()
	s.process = p
	s.mu.Unlock()
	defer close(p.finish)
	if err := a.pauseReceiver(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.closing:
	case <-time.After(time.Second):
		t.Fatal("close did not begin")
	}
	if a.receiverView(s)["releasePending"] != true {
		t.Fatal("claimed released before process exit")
	}
	if _, err := a.openReceiverDesktop(context.Background(), s); err == nil {
		t.Fatal("Desktop opened while writer is closing")
	}
	if err := a.resumeReceiver(context.Background(), s); err == nil {
		t.Fatal("resumed while old writer is closing")
	}
}

func TestReceiverCanReleaseAfterSpaceEnds(t *testing.T) {
	a, _, s := receiverFixture(t)
	space := a.sessions[s.receiverSpace]
	space.mu.Lock()
	space.record.State = "ended"
	space.mu.Unlock()
	if w := receiverAction(t, a, s, map[string]any{"action": "pause"}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	waitReceiverReleased(t, s)
	if w := receiverAction(t, a, s, map[string]any{"action": "start"}); w.Code == 200 {
		t.Fatal("ended space resumed receiving")
	}
}
