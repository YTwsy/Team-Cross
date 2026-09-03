package server

import (
	"context"
	"errors"
	"time"

	"teamcross/internal/invite"
	"teamcross/internal/share"
)

// Keep startup injectable so cancellation/publication races can be exercised
// without opening a listener, mDNS, Tailnet or Tailcat connection.
type shareRuntime interface {
	Token() (string, error)
	Invitation() invite.InvitationV1
	Warnings() []string
	Revoke()
	Close() error
}

type shareStartFunc func(context.Context, share.RuntimeConfig) (shareRuntime, error)

type shareCreation struct {
	ctx    context.Context
	cancel context.CancelFunc
}

var (
	errShareActive   = errors.New("an active Share already exists")
	errShareStarting = errors.New("a Share is being created or cancelled")
	errSharesClosed  = errors.New("Share service is closing")
)

// Reserve before any slow transport work. Cancellation retains this slot until
// startup and failure cleanup drain; an uncooperative late starter cannot publish.
func (app *App) beginShareCreation(ctx context.Context, threadID string) (*shareCreation, *hostedShare, error) {
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.sharesClosed {
		return nil, nil, errSharesClosed
	}
	if app.shareStarting[threadID] != nil {
		return nil, nil, errShareStarting
	}
	existing := app.shares[threadID]
	if existing != nil && time.Now().Before(existing.ExpiresAt) {
		return nil, nil, errShareActive
	}
	if app.shareStarting == nil {
		app.shareStarting = make(map[string]*shareCreation)
	}
	child, cancel := context.WithCancel(ctx)
	op := &shareCreation{ctx: child, cancel: cancel}
	app.shareStarting[threadID] = op
	app.shareWG.Add(1)
	if existing != nil {
		delete(app.shares, threadID)
		delete(app.shareByID, existing.ID)
	}
	return op, existing, nil
}

func (app *App) finishShareCreation(threadID string, op *shareCreation) {
	op.cancel()
	app.mu.Lock()
	if app.shareStarting[threadID] == op {
		delete(app.shareStarting, threadID)
	}
	app.mu.Unlock()
	app.shareWG.Done()
}

func (app *App) startShare(ctx context.Context, config share.RuntimeConfig) (shareRuntime, error) {
	if app.shareStarter != nil {
		return app.shareStarter(ctx, config)
	}
	return share.Start(ctx, config)
}

func (app *App) publishShare(threadID string, op *shareCreation, state *hostedShare) bool {
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.sharesClosed || app.shareStarting[threadID] != op || op.ctx.Err() != nil || app.shares[threadID] != nil || !time.Now().Before(state.ExpiresAt) {
		return false
	}
	app.shares[threadID] = state
	app.shareByID[state.ID] = state
	state.ExpiryTimer = time.AfterFunc(time.Until(state.ExpiresAt), func() {
		app.expireHostedShare(threadID, state.ID, state.ExpiresAt)
	})
	return true
}

func (app *App) isPublishedShare(threadID, shareID string) bool {
	app.mu.RLock()
	defer app.mu.RUnlock()
	state := app.shares[threadID]
	return !app.sharesClosed && state != nil && state.ID == shareID && time.Now().Before(state.ExpiresAt)
}

func stopShareRuntime(runtime shareRuntime) {
	if runtime != nil {
		runtime.Revoke()
		_ = runtime.Close()
	}
}

func (app *App) retireShare(state *hostedShare, at time.Time, event, actor string) {
	if state.ExpiryTimer != nil {
		state.ExpiryTimer.Stop()
	}
	// Revoke persistent command/lease authorization before waiting for transport
	// shutdown. The publication check has already fenced all new requests.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = app.store.RevokeShare(ctx, state.ID, at)
	stopShareRuntime(state.Runtime)
	if event != "" {
		_, _ = app.store.AppendEvent(ctx, state.ThreadID, event, jsonBytes(map[string]any{"actor": actor, "shareId": state.ID}))
	}
}

func (app *App) closeShares() {
	app.mu.Lock()
	app.sharesClosed = true
	for _, op := range app.shareStarting {
		op.cancel()
	}
	states := make(map[string]*hostedShare, len(app.shareByID)+len(app.shares))
	for id, state := range app.shareByID {
		states[id] = state
	}
	for _, state := range app.shares {
		states[state.ID] = state
	}
	app.shares = make(map[string]*hostedShare)
	app.shareByID = make(map[string]*hostedShare)
	app.mu.Unlock()
	for _, state := range states {
		app.retireShare(state, time.Time{}, "", "")
	}
	// Add is fenced by sharesClosed under mu, so no new operation may race Wait.
	// Failed startup cleanup must finish while the Store is still open.
	app.shareWG.Wait()
}
