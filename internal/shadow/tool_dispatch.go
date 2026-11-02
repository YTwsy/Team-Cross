package shadow

import (
	"context"
	"encoding/json"
)

// A slow collaboration tool blocks only its own native session. Preserve call
// order within that session while the engine continues scheduling S and W.
func (e *Engine) dispatchTool(ctx context.Context, event nativeEvent) {
	var call toolCall
	if json.Unmarshal(event.message.Params, &call) != nil || call.ThreadID == "" {
		return
	}
	e.mu.Lock()
	if e.toolQueues == nil {
		e.toolQueues = map[string][]nativeEvent{}
		e.toolRunning = map[string]bool{}
	}
	e.toolQueues[call.ThreadID] = append(e.toolQueues[call.ThreadID], event)
	if e.toolRunning[call.ThreadID] {
		e.mu.Unlock()
		return
	}
	e.toolRunning[call.ThreadID] = true
	e.toolWait.Add(1)
	e.mu.Unlock()
	go func() {
		defer e.toolWait.Done()
		for {
			e.mu.Lock()
			queue := e.toolQueues[call.ThreadID]
			if len(queue) == 0 || ctx.Err() != nil {
				delete(e.toolQueues, call.ThreadID)
				delete(e.toolRunning, call.ThreadID)
				e.mu.Unlock()
				return
			}
			next := queue[0]
			e.toolQueues[call.ThreadID] = queue[1:]
			current := !e.closed && next.generation == e.generation
			runtime := e.runtime
			e.mu.Unlock()
			if !current {
				continue
			}
			if err := e.handleToolWithRuntime(ctx, next.message, runtime); err != nil && ctx.Err() == nil {
				e.mu.Lock()
				if next.generation == e.generation {
					_ = e.mutateLocked(func() error { e.record.Error = err.Error(); e.record.Enabled = false; return nil })
				}
				e.mu.Unlock()
			}
			e.notify()
		}
	}()
}
