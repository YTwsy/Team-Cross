package collab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"teamcross/internal/nativecodex"
)

func receiverFixture(t *testing.T) (*App, *fakeRuntime, *Session) {
	t.Helper()
	a, f, space := spaceFixture(t)
	out, err := a.createSpaceReceiver(context.Background(), space.record.ID, uuid.NewString(), "Receiver fixture")
	if err != nil {
		t.Fatal(err)
	}
	id := out.(map[string]any)["id"].(string)
	return a, f, a.receivers[id]
}

func receiverEvent(s *Session, method, thread string, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["threadId"] = thread
	data, _ := json.Marshal(fields)
	s.onMessage(nativecodex.Message{Method: method, Params: data})
}

func TestReceiverNativeClientResolvesApprovalAndReleasesDelivery(t *testing.T) {
	a, _, s := receiverFixture(t)
	id := s.record.SessionID
	receiverEvent(s, "turn/started", id, map[string]any{"turn": map[string]any{"id": "turn-1"}})
	for _, rid := range []string{"0", `"request-1"`} {
		params, _ := json.Marshal(map[string]any{"threadId": id, "turnId": "turn-1"})
		s.onMessage(nativecodex.Message{ID: json.RawMessage(rid), Method: "mcpServer/elicitation/request", Params: params})
		receiverEvent(s, "serverRequest/resolved", "another-thread", map[string]any{"requestId": json.RawMessage(rid)})
		if a.receiverView(s)["approvals"] != 1 {
			t.Fatal("another thread cleared approval")
		}
		receiverEvent(s, "serverRequest/resolved", id, map[string]any{"requestId": json.RawMessage(rid)})
		if a.receiverView(s)["approvals"] != 0 {
			t.Fatal("native resolution left a stale approval")
		}
	}
	receiverEvent(s, "turn/completed", "another-thread", map[string]any{"turn": map[string]any{"id": "other"}})
	if a.receiverView(s)["busy"] != true {
		t.Fatal("another thread completed this turn")
	}
	receiverEvent(s, "turn/completed", id, map[string]any{"turn": map[string]any{"id": "older-turn"}})
	if a.receiverView(s)["busy"] != true {
		t.Fatal("old turn completed this turn")
	}
	receiverEvent(s, "turn/completed", id, map[string]any{"turn": map[string]any{"id": "turn-1"}})
	a.agentMu.Lock()
	p := a.agents.Pairings[s.record.RequestID]
	a.agentMu.Unlock()
	if reason := a.nativeAgentReason(p); reason != "" {
		t.Fatal("completed receiver is still blocked", reason)
	}
}

func TestReceiverInterruptUsesExactTurnAndWaitsForCompletion(t *testing.T) {
	a, f, s := receiverFixture(t)
	id := s.record.SessionID
	receiverEvent(s, "turn/started", id, map[string]any{"turn": map[string]any{"id": "turn-1"}})
	requestID := uuid.NewString()
	invoke := func(turn, request string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"action": "interrupt", "turnId": turn, "requestId": request})
		r := httptest.NewRequest("POST", "http://127.0.0.1/api/space-receivers/"+s.record.ID+"/action", strings.NewReader(string(body)))
		w := httptest.NewRecorder()
		a.Handler(http.NotFoundHandler()).ServeHTTP(w, r)
		return w
	}
	if w := invoke("old-turn", uuid.NewString()); w.Code == 200 {
		t.Fatal("stale stop accepted")
	}
	if w := invoke("turn-1", requestID); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if a.receiverView(s)["busy"] != true {
		t.Fatal("RPC receipt prematurely marked turn idle")
	}
	receiverEvent(s, "turn/completed", id, map[string]any{"turn": map[string]any{"id": "turn-1", "status": "interrupted"}})
	if w := invoke("turn-1", requestID); w.Code != 200 {
		t.Fatal("duplicate stop not idempotent", w.Body.String())
	}
	if w := invoke("turn-1", uuid.NewString()); w.Code == 200 {
		t.Fatal("finished turn accepted a new stop")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, method := range f.calls {
		if method == "turn/interrupt" {
			count++
		}
	}
	if count != 1 || f.requests["turn/interrupt"]["threadId"] != id || f.requests["turn/interrupt"]["turnId"] != "turn-1" {
		t.Fatal("stop retargeted or replayed", count, f.requests["turn/interrupt"])
	}
}

func TestReceiverDesktopOpenUsesSavedIdentityWithoutNewRuntime(t *testing.T) {
	a, f, s := receiverFixture(t)
	a.settings.DesktopApp = t.TempDir()
	bin := t.TempDir()
	capture := filepath.Join(bin, "arguments")
	if err := os.WriteFile(filepath.Join(bin, "open"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$TEAMCROSS_OPEN_CAPTURE\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("TEAMCROSS_OPEN_CAPTURE", capture)
	if w := receiverAction(t, a, s, map[string]any{"action": "open-desktop"}); w.Code == 200 {
		t.Fatal("Desktop opened before native release")
	}
	if err := a.pauseReceiver(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	waitReceiverReleased(t, s)
	f.mu.Lock()
	before := len(f.calls)
	f.mu.Unlock()
	r := httptest.NewRequest("POST", "http://127.0.0.1/api/space-receivers/"+s.record.ID+"/action", strings.NewReader(`{"action":"open-desktop","sessionId":"wrong"}`))
	w := httptest.NewRecorder()
	a.Handler(http.NotFoundHandler()).ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	args, err := os.ReadFile(capture)
	if err != nil || string(args) != "-a\n"+a.settings.DesktopApp+"\ncodex://threads/"+s.record.SessionID+"\n" {
		t.Fatal(string(args), err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) != before+1 || f.calls[before] != "thread/turns/list" || f.requests["thread/turns/list"]["threadId"] != s.record.SessionID {
		t.Fatal("opening changed runtime or read another thread", f.calls)
	}
}
