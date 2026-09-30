package lifecycle

import (
	"sync"
	"sync/atomic"
	"testing"

	"teamcross/internal/service"
)

func TestQuitDuringStartupWaitsForCoreThenCanCancelAndRetry(t *testing.T) {
	core := &fakeCore{status: service.Status{Running: true, Active: 1}}
	var confirmations, finished atomic.Int32
	quit := &Quit{Core: core, Confirm: func(service.Status) bool {
		return confirmations.Add(1) > 1
	}, Failed: func(err error) { t.Error(err) }, Finished: func() { finished.Add(1) }}
	startup := &Startup{Quit: quit.Request}
	for i := 0; i < 10; i++ {
		startup.RequestQuit()
	}
	if !startup.Quitting() || quit.Busy() || confirmations.Load() != 0 || core.stops.Load() != 0 {
		t.Fatal("quit probed or stopped Core before startup settled")
	}
	startup.Finish()
	waitIdle(t, quit)
	if confirmations.Load() != 1 || core.stops.Load() != 0 || finished.Load() != 0 {
		t.Fatal("startup quit was not coalesced or cancellation lost")
	}
	startup.RequestQuit()
	waitIdle(t, quit)
	if confirmations.Load() != 2 || core.stops.Load() != 1 || finished.Load() != 1 {
		t.Fatal("explicit retry after cancellation did not stop Core")
	}
}

func TestStartupCompletionRacingQuitNeverLosesRequest(t *testing.T) {
	for i := 0; i < 1000; i++ {
		var calls atomic.Int32
		startup := &Startup{Quit: func() { calls.Add(1) }}
		var workers sync.WaitGroup
		workers.Add(2)
		go func() { defer workers.Done(); startup.RequestQuit() }()
		go func() { defer workers.Done(); startup.Finish() }()
		workers.Wait()
		startup.Finish()
		if calls.Load() != 1 {
			t.Fatalf("completion race dispatched %d quits", calls.Load())
		}
	}
}
