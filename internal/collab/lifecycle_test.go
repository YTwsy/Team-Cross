package collab

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"teamcross/internal/nativecodex"
)

func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for lifecycle transition")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestJoinedAccessSurvivesDeadlineAndParticipantCoreRestart(t *testing.T) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	b, _, _ := fixture(t)
	ctx := context.Background()
	if e := s.Action(ctx, "share"); e != nil {
		t.Fatal(e)
	}
	if s.view()["transport"] != "lan" {
		t.Fatal("default share did not select LAN", s.view()["transport"])
	}
	token := s.share.Token()
	j, e := b.Join(ctx, token)
	if e != nil {
		t.Fatal(e)
	}
	if j.Credential == j.Invitation.Secret || !j.confirmed {
		t.Fatal("membership not separated from admission")
	}
	j.mu.Lock()
	j.Invitation.ExpiresAt = time.Now().Add(-2 * time.Hour)
	j.mu.Unlock()
	if got := j.view(ctx); got["online"] != true || got["state"] != "ready" || got["expiresAt"] != nil || got["transport"] != "lan" {
		t.Fatal("joined access expired", got)
	}
	if b.Active() != 1 {
		t.Fatal("membership disappeared from active count")
	}
	if e = s.Action(ctx, "handoff"); e != nil {
		t.Fatal(e)
	}
	remoteEndpoint, e := b.endpoint(j.ID)
	if e != nil {
		t.Fatal(e)
	}
	remote, _, e := websocket.Dial(ctx, remoteEndpoint, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer remote.CloseNow()
	if e = s.Action(ctx, "reclaim"); e != nil {
		t.Fatal(e)
	}
	ownerEndpoint, e := a.endpoint(s.record.ID)
	if e != nil {
		t.Fatal(e)
	}
	owner, _, e := websocket.Dial(ctx, ownerEndpoint, nil)
	if e != nil {
		t.Fatal("owner cannot attach after reclaim", e)
	}
	owner.CloseNow()
	eventually(t, func() bool { return s.view()["connected"] == false })
	if !f.Alive() || s.view()["sharing"] != true {
		t.Fatal("disconnect closed live shared runtime")
	}
	b.Close()
	reopened, e := Open(b.Config)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	restored := reopened.joined[j.ID]
	if restored == nil || restored.Credential != j.Credential || restored.view(ctx)["online"] != true {
		t.Fatal("B restart lost member access")
	}
	c, _, _ := fixture(t)
	if _, e = c.Join(ctx, token); e == nil {
		t.Fatal("copied invitation admitted another participant")
	}
	if e = restored.leave(ctx); e != nil {
		t.Fatal(e)
	}
	if state := s.view()["invitationState"]; state != "left" {
		t.Fatal("explicit leave not revoked", state)
	}
	if e = s.Action(ctx, "share"); e != nil {
		t.Fatal(e)
	}
	if _, e = c.Join(ctx, s.share.Token()); e != nil {
		t.Fatal("new invitation failed after leave", e)
	}
}

func TestShareRejectsUnknownTransport(t *testing.T) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	err := s.Share(context.Background(), "public-internet")
	if err == nil || !strings.Contains(err.Error(), "连接方式") {
		t.Fatal("unknown transport was accepted", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.share != nil || s.sharePreparing {
		t.Fatal("invalid transport changed sharing state")
	}
}

func TestEndReleasesOnlyAfterOwnerClientAndTurnFinish(t *testing.T) {
	a, f, repo := fixture(t)
	s := createFixture(t, a, f, "existing")
	ctx := context.Background()
	if e := s.Action(ctx, "share"); e != nil {
		t.Fatal(e)
	}
	endpoint, e := a.endpoint(s.record.ID)
	if e != nil {
		t.Fatal(e)
	}
	owner, _, e := websocket.Dial(ctx, endpoint, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer owner.CloseNow()
	if _, e = s.RPC(ctx, "owner", "turn/start", map[string]any{"input": []any{}}, "busy-before-end"); e != nil {
		t.Fatal(e)
	}
	sessionID := s.record.SessionID
	if e = s.Action(ctx, "end"); e != nil {
		t.Fatal(e)
	}
	if !f.Alive() || s.view()["online"] != true {
		t.Fatal("end interrupted execution/client")
	}
	s.onMessage(nativecodex.Message{Method: "turn/completed"})
	if !f.Alive() {
		t.Fatal("end closed owner client")
	}
	owner.CloseNow()
	eventually(t, func() bool { return s.view()["runtimeState"] == "released" })
	if f.Alive() {
		t.Fatal("native lock owner still alive")
	}
	if _, e = os.Stat(filepath.Join(repo, "file.txt")); e != nil {
		t.Fatal("workspace lost", e)
	}
	// Reading context must not resume/reacquire the native thread.
	f.mu.Lock()
	before := len(f.calls)
	f.mu.Unlock()
	if _, e = s.Context(ctx, "history", "", 0); e != nil {
		t.Fatal(e)
	}
	f.mu.Lock()
	reads := append([]string(nil), f.calls[before:]...)
	f.mu.Unlock()
	for _, method := range reads {
		if method == "thread/resume" || method == "thread/fork" {
			t.Fatal("read reacquired session", method)
		}
	}
	if s.view()["online"] != false {
		t.Fatal("polling started writer runtime")
	}
	if e = s.Action(ctx, "start"); e != nil {
		t.Fatal(e)
	}
	if s.record.SessionID != sessionID || f.forks != 1 || s.view()["online"] != true {
		t.Fatal("restore did not retain fork")
	}
}

type blockedRuntime struct {
	*fakeRuntime
	method       string
	entered      chan struct{}
	proceed      chan struct{}
	closeEntered chan struct{}
	closeProceed chan struct{}
	once         sync.Once
}

func (p *blockedRuntime) Call(ctx context.Context, method string, in, out any) error {
	if method == p.method {
		p.once.Do(func() { close(p.entered) })
		select {
		case <-p.proceed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return p.fakeRuntime.Call(ctx, method, in, out)
}
func (p *blockedRuntime) Close() {
	if p.closeEntered != nil {
		close(p.closeEntered)
		<-p.closeProceed
	}
	p.fakeRuntime.Close()
}

func TestEndWaitsForAcceptedRPCAndApproval(t *testing.T) {
	for _, method := range []string{"thread/name/set", "turn/start"} {
		t.Run(method, func(t *testing.T) {
			a, f, _ := fixture(t)
			s := createFixture(t, a, f, "existing")
			p := &blockedRuntime{fakeRuntime: f, method: method, entered: make(chan struct{}), proceed: make(chan struct{})}
			s.mu.Lock()
			s.process = p
			s.mu.Unlock()
			result := make(chan error, 1)
			go func() {
				_, e := s.RPC(context.Background(), "owner", method, map[string]any{}, "accepted")
				result <- e
			}()
			<-p.entered
			if e := s.Action(context.Background(), "end"); e != nil {
				t.Fatal(e)
			}
			if !f.Alive() {
				t.Fatal("closed before RPC completion")
			}
			close(p.proceed)
			if e := <-result; e != nil {
				t.Fatal(e)
			}
			if method == "turn/start" {
				id := json.RawMessage(`42`)
				s.onMessage(nativecodex.Message{ID: id, Method: "item/commandExecution/requestApproval"})
				s.onMessage(nativecodex.Message{Method: "turn/completed"})
				if !f.Alive() {
					t.Fatal("closed while awaiting approval")
				}
				if e := s.Respond(context.Background(), "owner", id, map[string]string{"decision": "decline"}); e != nil {
					t.Fatal(e)
				}
			}
			eventually(t, func() bool { return s.view()["runtimeState"] == "released" })
		})
	}
}

func TestRestoreWaitsForProcessExitAndIgnoresOldNotifications(t *testing.T) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	f.mu.Lock()
	oldHandler := f.handler
	f.mu.Unlock()
	p := &blockedRuntime{fakeRuntime: f, closeEntered: make(chan struct{}), closeProceed: make(chan struct{})}
	s.mu.Lock()
	s.process = p
	s.mu.Unlock()
	if e := s.Action(context.Background(), "end"); e != nil {
		t.Fatal(e)
	}
	<-p.closeEntered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if e := s.Action(ctx, "start"); e == nil {
		t.Fatal("started before old process exited")
	}
	close(p.closeProceed)
	if e := s.Action(context.Background(), "start"); e != nil {
		t.Fatal(e)
	}
	oldHandler(nativecodex.Message{Method: "teamcross/runtimeDisconnected"})
	oldHandler(nativecodex.Message{Method: "turn/started"})
	if s.view()["online"] != true || s.view()["busy"] != false {
		t.Fatal("old generation corrupted restored runtime")
	}
}

func TestOldDirectConnectionCannotWriteAfterReclaim(t *testing.T) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	old := &direct{done: make(chan struct{}), role: "owner"}
	s.mu.Lock()
	s.direct = old
	s.mu.Unlock()
	ctx := context.WithValue(context.Background(), directKey{}, old)
	if e := s.Action(ctx, "reclaim"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.RPC(ctx, "owner", "turn/start", nil, "stale"); e == nil || !strings.Contains(e.Error(), "连接") {
		t.Fatal("stale connection wrote", e)
	}
}

func TestLostJoinResponseRecoveredByReadAfterRestart(t *testing.T) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	b, _, _ := fixture(t)
	ctx := context.Background()
	if e := s.Action(ctx, "share"); e != nil {
		t.Fatal(e)
	}
	// Retain the admission that the host accepted, but emulate B stopping before
	// it records the successful response. Polling must confirm without rejoining.
	j, e := b.Join(ctx, s.share.Token())
	if e != nil {
		t.Fatal(e)
	}
	j.mu.Lock()
	j.confirmed = false
	j.Invitation.ExpiresAt = time.Now().Add(-time.Hour)
	j.mu.Unlock()
	b.Close()
	reopened, e := Open(b.Config)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	recovered := reopened.joined[j.ID]
	if recovered.view(ctx)["online"] != true {
		t.Fatal("lost response not recovered")
	}
	recovered.mu.Lock()
	confirmed := recovered.confirmed
	recovered.mu.Unlock()
	if !confirmed {
		t.Fatal("status did not confirm pending admission")
	}
	if _, e := reopened.endpoint(j.ID); e != nil {
		t.Fatal("recovered member cannot open a client", e)
	}
	eventually(t, func() bool { return s.view()["participantOnline"] == true })
}

func TestEndDuringRestoreWaitsForResume(t *testing.T) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	f.Close()
	p := &blockedRuntime{fakeRuntime: f, method: "thread/resume", entered: make(chan struct{}), proceed: make(chan struct{})}
	a.Config.StartProcess = func(string, string, string, string) (Runtime, error) {
		f.mu.Lock()
		f.alive = true
		f.mu.Unlock()
		return p, nil
	}
	result := make(chan error, 1)
	go func() { result <- s.Action(context.Background(), "start") }()
	<-p.entered
	if e := s.Action(context.Background(), "end"); e != nil {
		t.Fatal(e)
	}
	if !f.Alive() {
		t.Fatal("end interrupted resume")
	}
	close(p.proceed)
	if e := <-result; e != nil {
		t.Fatal(e)
	}
	eventually(t, func() bool { return s.view()["runtimeState"] == "released" })
	if f.Alive() {
		t.Fatal("end during restore left process alive")
	}
}
