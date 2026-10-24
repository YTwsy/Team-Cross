package shadow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"teamcross/internal/spacecontext"
)

func (e *Engine) startAttentionPreparation(ctx context.Context, store *AttentionStore, batch *ObservationBatch, reader func(context.Context, spacecontext.SourceRef) (SourceSnapshot, error)) {
	e.mu.Lock()
	if e.attentionPreparing == nil {
		e.attentionPreparing = map[string]bool{}
	}
	if e.closed || e.attentionPreparing[batch.ID] {
		e.mu.Unlock()
		return
	}
	e.attentionPreparing[batch.ID] = true
	e.attentionWait.Add(1)
	e.mu.Unlock()
	go func() {
		defer e.attentionWait.Done()
		err := e.prepareAttention(ctx, store, batch.ID, reader)
		if err != nil && ctx.Err() == nil && !errors.Is(err, errObsoleteBatch) {
			if saveErr := store.FailBatch(batch.ID, "准备动态失败："+err.Error(), time.Now().Add(30*time.Second)); saveErr != nil {
				err = saveErr
			}
		}
		e.mu.Lock()
		delete(e.attentionPreparing, batch.ID)
		if err != nil && ctx.Err() == nil && !errors.Is(err, errObsoleteBatch) {
			// A background source/route failure must not disable the shared
			// conversation. The source cursor and exact pending batch remain.
			e.record.Error = "准备动态失败：" + err.Error()
		}
		e.mu.Unlock()
		e.notify()
	}()
}

func (e *Engine) prepareAttention(ctx context.Context, store *AttentionStore, batchID string, reader func(context.Context, spacecontext.SourceRef) (SourceSnapshot, error)) error {
	batch, err := e.routeBatch(ctx, store, batchID)
	if err != nil || batch.State == "task" {
		return err
	}
	signals, err := store.BatchSignals(batch.ID)
	if err != nil {
		return err
	}
	sources := []SourceSnapshot{}
	for _, signal := range signals {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		var source SourceSnapshot
		var readErr error
		if signal.Follow != nil && e.cfg.ReadFollowSource != nil {
			source, readErr = e.cfg.ReadFollowSource(readCtx, signal)
		} else {
			source, readErr = reader(readCtx, signal.Source)
		}
		cancel()
		if readErr != nil {
			if len(sources) > 0 {
				break
			}
			return readErr
		}
		candidate := append(sources, source)
		encoded, _ := json.Marshal(candidate)
		if len(encoded) > 96<<10 {
			if len(sources) == 0 {
				return fmt.Errorf("单条原文超过观察预算")
			}
			break
		}
		sources = candidate
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	_, err = store.PrepareBatch(batch.ID, sources)
	return err
}

// The stamp covers task routes, not chat text or native token updates. Changes
// while an asynchronous check runs require another check before native intent.
func (e *Engine) DependencyRouteStamp() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	type route struct {
		ID           string
		Version      int
		State        string
		Dependencies []TaskDependency
	}
	routes := []route{}
	for _, job := range sortedJobs(e.record.Jobs) {
		if len(job.Dependencies) > 0 {
			routes = append(routes, route{job.ID, job.Version, job.State, job.Dependencies})
		}
	}
	return attentionHash(routes)
}

func (e *Engine) activityRouteStamp() string {
	if e.cfg.ActivityRouteStamp != nil {
		return e.cfg.ActivityRouteStamp()
	}
	return e.DependencyRouteStamp()
}

func (e *Engine) checkDirectRoute(ctx context.Context, store *AttentionStore, id string) bool {
	stamp := e.activityRouteStamp()
	e.mu.Lock()
	if e.continuationChecking == nil {
		e.continuationChecking, e.continuationReady = map[string]bool{}, map[string]string{}
	}
	if e.continuationReady[id] == stamp {
		e.mu.Unlock()
		return true
	}
	if e.closed || e.continuationChecking[id] {
		e.mu.Unlock()
		return false
	}
	e.continuationChecking[id] = true
	e.attentionWait.Add(1)
	e.mu.Unlock()
	go func() {
		defer e.attentionWait.Done()
		batch, err := e.routeBatch(ctx, store, id)
		if err != nil && ctx.Err() == nil && !errors.Is(err, errObsoleteBatch) {
			if saveErr := store.DeferDirectRoute(id, err.Error()); saveErr != nil {
				err = saveErr
			}
		}
		after := e.activityRouteStamp()
		e.mu.Lock()
		delete(e.continuationChecking, id)
		if err == nil && batch.State != "task" && stamp == after && !e.closed {
			e.continuationReady[id] = after
		}
		if err != nil && ctx.Err() == nil && !errors.Is(err, errObsoleteBatch) {
			e.record.Error = "核对答复路线失败：" + err.Error()
		}
		e.mu.Unlock()
		e.notify()
	}()
	return false
}

func (s *AttentionStore) DeferDirectRoute(id, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.record.Continuations[id]
	if !ok || c.Mode != "direct" || c.State != "running" {
		return errObsoleteBatch
	}
	next := clone(s.record)
	c.State, c.Error = "queued", reason
	next.Continuations[id] = c
	f := next.Focuses[c.FocusID]
	f.Processing, f.Error, f.RetryAfter = "error", reason, time.Now().Add(30*time.Second)
	next.Focuses[f.ID] = f
	return s.saveLocked(next)
}
