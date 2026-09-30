package lifecycle

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"teamcross/internal/service"
)

type fakeCore struct {
	status service.Status
	err    error
	stops  atomic.Int32
	force  atomic.Bool
}

func (c *fakeCore) Status(context.Context) (service.Status, error) { return c.status, c.err }
func (c *fakeCore) Stop(_ context.Context, s service.Status, force bool) error {
	c.stops.Add(1)
	c.force.Store(force)
	return c.err
}
func waitIdle(t *testing.T, q *Quit) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for q.Busy() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if q.Busy() {
		t.Fatal("quit did not finish")
	}
}

func TestCancelAndRepeatedQuitKeepCoreAlive(t *testing.T) {
	c := &fakeCore{status: service.Status{Running: true, Active: 2}}
	entered, answer := make(chan struct{}), make(chan bool)
	q := &Quit{Core: c, Confirm: func(service.Status) bool { close(entered); return <-answer }, Failed: func(error) { t.Error("unexpected failure") }, Finished: func() { t.Error("cancelled quit completed") }}
	q.Request()
	<-entered
	for i := 0; i < 10; i++ {
		q.Request()
	}
	answer <- false
	waitIdle(t, q)
	if c.stops.Load() != 0 {
		t.Fatal("cancel stopped Core")
	}
}

func TestConfirmedQuitStopsOnceBeforeCompletion(t *testing.T) {
	c := &fakeCore{status: service.Status{Running: true, Active: 2}}
	var completed atomic.Bool
	q := &Quit{Core: c, Confirm: func(s service.Status) bool { return s.Active == 2 }, Failed: func(error) { t.Error("unexpected failure") }, Finished: func() {
		if c.stops.Load() != 1 {
			t.Error("finished before stopping")
		}
		completed.Store(true)
	}}
	q.Request()
	waitIdle(t, q)
	if !completed.Load() || !c.force.Load() {
		t.Fatal("quit not completed")
	}
}

func TestInactiveQuitDoesNotForceAndFailureKeepsShell(t *testing.T) {
	c := &fakeCore{status: service.Status{Running: true}}
	var completed, failed atomic.Int32
	q := &Quit{Core: c, Confirm: func(service.Status) bool { t.Error("unexpected confirmation"); return false }, Failed: func(error) { failed.Add(1) }, Finished: func() { completed.Add(1) }}
	q.Request()
	waitIdle(t, q)
	if c.force.Load() || completed.Load() != 1 {
		t.Fatal("inactive quit failed")
	}
	c.err = errors.New("offline")
	q.Request()
	waitIdle(t, q)
	if completed.Load() != 1 || failed.Load() != 1 || c.stops.Load() != 1 {
		t.Fatal("failed probe stopped Core or shell")
	}
}
