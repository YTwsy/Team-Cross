// Package lifecycle coordinates explicit quit without owning business state.
package lifecycle

import (
	"context"
	"sync/atomic"
	"time"

	"teamcross/internal/service"
)

type Core interface {
	Status(context.Context) (service.Status, error)
	Stop(context.Context, service.Status, bool) error
}

type Quit struct {
	Core     Core
	Confirm  func(service.Status) bool
	Failed   func(error)
	Finished func()
	busy     atomic.Bool
}

func (q *Quit) Busy() bool { return q.busy.Load() }

// Request returns immediately to the native termination callback. A cancelled
// confirmation or failed stop keeps the existing windows and their state alive.
func (q *Quit) Request() {
	if !q.busy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer q.busy.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		status, err := q.Core.Status(ctx)
		cancel()
		if err != nil {
			q.Failed(err)
			return
		}
		if !status.Running {
			q.Finished()
			return
		}
		force := status.Active > 0
		if force && !q.Confirm(status) {
			return
		}
		ctx, cancel = context.WithTimeout(context.Background(), 18*time.Second)
		defer cancel()
		if err := q.Core.Stop(ctx, status, force); err != nil {
			q.Failed(err)
			return
		}
		q.Finished()
	}()
}
