package collab

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/problem"
)

func TestReloadedReceiverCanSendFirstRequest(t *testing.T) {
	ctx := context.Background()
	a, _, space := spaceFixture(t)
	requestID := uuid.NewString()
	if _, err := a.createSpaceReceiver(ctx, space.record.ID, requestID, "Receiver"); err != nil {
		t.Fatal(err)
	}
	data, config := a.Config.DataDir, a.Config
	id := "receiver-" + requestID
	sessionID := a.receivers[id].record.SessionID
	a.Close()
	config.DataDir = data
	restored, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	stopWorkbenchPump(restored)
	receiver := restored.receivers[id]
	if receiver.record.Commands == nil || receiver.record.SessionID != sessionID || receiver.process != nil {
		t.Fatal("receiver restore lost ledger/identity or automatically resumed")
	}
	if err := receiver.start(ctx, true); err != nil {
		t.Fatal(err)
	}
	input := agentRequestInput{RequestID: uuid.NewString(), PairingID: requestID, Intent: "analyze", Instruction: "Read fixture", WorkbenchSpaceID: space.record.ID}
	result, err := restored.sendAgentRequest(ctx, input)
	if err != nil || result.State != "submitted" || result.TurnID == "" {
		t.Fatal(result, err)
	}
	again, err := restored.sendAgentRequest(ctx, input)
	if err != nil || again.TurnID != result.TurnID {
		t.Fatal("delivery replayed", again, err)
	}
	status, err := restored.ControlStatus(ctx)
	if err != nil || !status.Running || status.Active != 1 {
		t.Fatal(status, err)
	}
	if len(receiver.record.Commands) != 1 {
		t.Fatal("duplicate provider command")
	}
}

type panicRPCInput struct{}

func (panicRPCInput) MarshalJSON() ([]byte, error) { panic("synthetic encoding failure") }

func TestRPCReleasesSessionAfterPanic(t *testing.T) {
	s := &Session{online: true, writer: "owner", process: &fakeRuntime{alive: true},
		record: Record{ExecutionRecord: &ExecutionRecord{Provider: "codex", SessionID: "fixture"}},
	}
	done := make(chan any, 1)
	go func() {
		defer func() { done <- recover() }()
		_, _ = s.RPC(context.Background(), "owner", "thread/name/set", map[string]any{"name": panicRPCInput{}}, "fixture")
	}()
	select {
	case value := <-done:
		if fmt.Sprint(value) != "synthetic encoding failure" {
			t.Fatal(value)
		}
	case <-time.After(time.Second):
		t.Fatal("RPC deadlocked while unwinding panic")
	}
	if !s.mu.TryLock() {
		t.Fatal("panic leaked Session mutex")
	}
	defer s.mu.Unlock()
	if s.activeCalls != 0 {
		t.Fatal("panic leaked active call", s.activeCalls)
	}
}

func TestRPCInitializesMissingCommandLedger(t *testing.T) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	s.mu.Lock()
	s.record.Commands = nil
	s.mu.Unlock()
	if _, err := s.RPC(context.Background(), "owner", "thread/name/set", map[string]any{"name": "fixture"}, "first"); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.record.Commands) != 1 {
		t.Fatal("first command was not recorded")
	}
}

func TestControlStatusBoundsSessionAndAppContention(t *testing.T) {
	for _, kind := range []string{"app", "session", "receiver", "joined", "share-request"} {
		t.Run(kind, func(t *testing.T) {
			a := &App{settings: Settings{UILanguage: "en"}}
			session := &Session{online: true}
			joined := &Joined{confirmed: true}
			job := &shareRequest{record: shareRequestRecord{State: "waiting"}}
			var lock *sync.Mutex
			switch kind {
			case "app":
				lock = &a.mu
			case "session":
				a.sessions = map[string]*Session{"test": session}
				lock = &session.mu
			case "receiver":
				a.receivers = map[string]*Session{"test": session}
				lock = &session.mu
			case "joined":
				a.joined = map[string]*Joined{"test": joined}
				lock = &joined.mu
			case "share-request":
				a.shareRequests = map[string]*shareRequest{"test": job}
				lock = &job.mu
			}
			lock.Lock()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			_, err := a.ControlStatus(ctx)
			cancel()
			blocked := a.UpgradeBusy()
			lock.Unlock()
			if err == nil || problem.Describe(err).Code != "core_busy" || !blocked {
				t.Fatal("unknown activity became healthy/idle", err, blocked)
			}
			status, err := a.ControlStatus(context.Background())
			if err != nil || !status.Running {
				t.Fatal("released lock did not recover", status, err)
			}
		})
	}
}

func TestControlStatusProtectsContendedAgentRegistry(t *testing.T) {
	a := &App{settings: Settings{UILanguage: "en"}}
	a.agentMu.Lock()
	status, err := a.ControlStatus(context.Background())
	a.agentMu.Unlock()
	if err != nil || !status.Running || !status.UpgradeBlocked {
		t.Fatal(status, err)
	}
	status, err = a.ControlStatus(context.Background())
	if err != nil || status.UpgradeBlocked {
		t.Fatal(status, err)
	}
}
