package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	"teamcross/internal/domain"
)

// closeConflictedRunBeforeTransition is called under the Thread transition
// fence, before preparing or creating a replacement Writer. A conflict may
// have happened while the old Agent was still executing; an archived label or
// best-effort close after activation is not evidence that it has stopped.
func (app *App) closeConflictedRunBeforeTransition(ctx context.Context, run *managedRun) error {
	if run == nil {
		return nil
	}
	app.mu.RLock()
	needsClose := (run.IdentityConflict || run.Status == "archived") && run.Status != "closed"
	app.mu.RUnlock()
	if !needsClose {
		return nil
	}
	if err := app.closeManagedRun(ctx, run); err != nil {
		return fmt.Errorf("cannot replace unavailable Run until the old Writer has closed: %w", err)
	}
	return nil
}

// finishManagedReplacement requires confirmed closure of the old Writer before
// the target's first prompt, irrespective of when identity events arrive. On
// failure the never-prompted target becomes unreachable; the old archived
// reference remains only for explicit Owner close/transition retries.
func (app *App) finishManagedReplacement(ctx context.Context, current, target *managedRun) error {
	if err := app.closeOutgoingRun(ctx, target.ThreadID, current); err != nil {
		return app.abortManagedReplacement(ctx, current, target, err)
	}
	return nil
}

// abortManagedReplacement compensates every failure after runtime activation
// and before the first prompt, not just a failed Provider close. A failed
// durable activation/event must not leave a newly addressable Writer behind.
// The outgoing reference remains fenced for an explicit Owner transition; it
// is never resumed and no close acknowledgement is invented.
func (app *App) abortManagedReplacement(ctx context.Context, current, target *managedRun, cause error) error {
	discardErr := app.discardUnstartedRun(ctx, target)
	app.mu.Lock()
	outgoingID := ""
	if current != nil {
		outgoingID = current.ID
		if current.Status != "closed" {
			current.Status = "archived"
			current.TurnID = ""
		}
		if app.runs[target.ThreadID] == nil || app.runs[target.ThreadID] == target {
			app.runs[target.ThreadID] = current
		}
	}
	app.mu.Unlock()
	_, eventErr := app.store.AppendEvent(ctx, target.ThreadID, "run.replacement_blocked", jsonBytes(map[string]any{
		"runId": target.ID, "outgoingRunId": outgoingID,
		"message": "The unstarted replacement was disabled because its durable activation or outgoing Writer close did not complete; Owner may explicitly retry the transition",
	}))
	return joinErrors(cause, discardErr, eventErr)
}

// adoptManagedRunIdentity is the only mutation path for a live managed Run's
// Provider ID. The durable first writer wins; a late empty descriptor cannot
// undo an earlier event, even if their persistence completions are reordered.
func (app *App) adoptManagedRunIdentity(ctx context.Context, run *managedRun, candidate string) error {
	app.mu.RLock()
	known := app.runsByID[run.ID] == run
	app.mu.RUnlock()
	if !known {
		return domain.ErrNotFound
	}
	identity, err := app.store.BindAgentRunIdentity(ctx, run.ID, run.Provider, candidate)
	if err != nil && !errors.Is(err, domain.ErrSessionIdentityConflict) {
		return err
	}
	app.mu.Lock()
	if app.runsByID[run.ID] != run {
		app.mu.Unlock()
		return domain.ErrNotFound
	}
	adopted, memoryErr := domain.AdoptSessionID(run.Provider, run.ID, run.SessionID, identity)
	run.SessionID = adopted
	if err == nil {
		err = memoryErr
	}
	firstConflict := err != nil && !run.IdentityConflict
	if err != nil {
		run.IdentityConflict = true
		if run.Status != "archived" && run.Status != "closed" {
			run.Status = "identity_conflict"
		}
	}
	status := run.Status
	app.mu.Unlock()
	if firstConflict {
		// No Provider call is retried or redirected after a conflict. Owner may
		// explicitly replace/close the Run; existing provenance remains intact.
		if status == "identity_conflict" {
			_ = app.store.UpdateAgentRun(ctx, run.ID, status, time.Time{})
		}
		_, _ = app.store.AppendEvent(ctx, run.ThreadID, "run.identity_conflict", jsonBytes(map[string]any{
			"provider": run.Provider, "runId": run.ID, "sessionId": adopted,
			"message": "A different Session identity was rejected; further Agent commands are blocked for this Run",
		}))
	}
	return err
}
