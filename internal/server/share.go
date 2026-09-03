package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"teamcross/internal/domain"
	"teamcross/internal/share"
)

func (app *App) handleCreateShare(response http.ResponseWriter, request *http.Request) {
	threadID, ok := app.authorizeThread(response, request)
	if !ok {
		return
	}
	var input struct {
		TTLSeconds    int64              `json:"ttlSeconds"`
		AllowDegraded bool               `json:"allowDegraded"`
		AllowControl  bool               `json:"allowControl"`
		Scope         *domain.ShareScope `json:"scope,omitempty"`
	}
	if !decodeJSON(response, request, &input) {
		return
	}
	if input.TTLSeconds == 0 {
		input.TTLSeconds = int64(time.Hour / time.Second)
	}
	ttl := time.Duration(input.TTLSeconds) * time.Second
	if ttl <= 0 || ttl > 24*time.Hour {
		writeError(response, http.StatusBadRequest, "invalid_ttl", "Share TTL must be between one second and 24 hours")
		return
	}
	op, expired, err := app.beginShareCreation(request.Context(), threadID)
	if err != nil {
		status, code := http.StatusConflict, "share_active"
		if errors.Is(err, errSharesClosed) {
			status, code = http.StatusServiceUnavailable, "share_closed"
		} else if errors.Is(err, errShareStarting) {
			code = "share_starting"
		}
		writeError(response, status, code, err.Error())
		return
	}
	defer app.finishShareCreation(threadID, op)
	if expired != nil {
		app.retireShare(expired, expired.ExpiresAt, "share.expired", "System")
	}
	shareID := uuid.NewString()
	projection, err := app.prepareShareProjection(op.ctx, threadID, input.Scope, input.AllowControl)
	if err != nil {
		writeError(response, 422, "share_scope", err.Error())
		return
	}
	granted := []string{"view", "annotate"}
	if input.AllowControl {
		granted = append(granted, "send", "steer", "interrupt")
	}
	expires := time.Now().UTC().Add(ttl)
	config := share.RuntimeConfig{
		ShareID: shareID, ExpiresAt: expires, Capabilities: granted,
		Handler: app.remoteHandler(threadID, shareID), EnableMDNS: true, EnableTailcat: true,
	}
	var runtime shareRuntime
	published, persisted := false, false
	defer func() {
		if !published {
			stopShareRuntime(runtime)
			if persisted {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = app.store.RevokeShare(ctx, shareID, time.Time{})
			}
		}
	}()
	runtime, err = app.startShare(op.ctx, config)
	degraded := false
	if err != nil && op.ctx.Err() == nil && input.AllowDegraded && errors.Is(err, share.ErrTailcatPrewarm) {
		stopShareRuntime(runtime)
		runtime = nil
		config.EnableTailcat = false
		runtime, err = app.startShare(op.ctx, config)
		degraded = true
	}
	if op.ctx.Err() != nil {
		writeError(response, http.StatusConflict, "share_cancelled", "Share creation was cancelled; no invitation was published")
		return
	}
	if err != nil {
		if errors.Is(err, share.ErrTailcatPrewarm) {
			writeError(response, http.StatusServiceUnavailable, "tailcat_prewarm", "Tailcat prewarm failed; enable degraded sharing to continue with LAN/Tailnet only: "+err.Error())
		} else {
			writeError(response, http.StatusServiceUnavailable, "share_start", "Unable to start the Share listener: "+err.Error())
		}
		return
	}
	token, err := runtime.Token()
	if err != nil {
		writeError(response, http.StatusInternalServerError, "invite", err.Error())
		return
	}
	invitation := runtime.Invitation()
	expires = time.Unix(invitation.ExpiresAt, 0).UTC()
	capabilities, _ := json.Marshal(invitation.Capabilities)
	if _, err := app.store.CreateShare(op.ctx, domain.Share{
		ID: shareID, ThreadID: threadID, SecretHash: hashSecret(invitation.Secret),
		ServerSPKI: fmt.Sprintf("%x", invitation.ServerSPKISHA256), Capabilities: capabilities, ExpiresAt: expires,
	}); err != nil {
		writeDomainError(response, err)
		return
	}
	persisted = true
	var transports []string
	if err := app.store.SaveScopedShareProjection(op.ctx, shareID, jsonBytes(projection), projection.LiveBinding); err != nil {
		if errors.Is(err, domain.ErrLiveShareStale) || errors.Is(err, domain.ErrLiveShareFence) {
			writeError(response, http.StatusConflict, "share_preview_changed", "Follow 或预览窗口已变化；本次分享未发布。请刷新内容后重新确认。")
		} else {
			writeDomainError(response, err)
		}
		return
	}
	if invitation.LAN.MDNSInstance != "" || len(invitation.LAN.Endpoints) > 0 {
		transports = append(transports, "lan")
	}
	if invitation.Tailscale != nil {
		transports = append(transports, "tailscale")
	}
	if invitation.Tailcat != nil {
		transports = append(transports, "tailcat")
	}
	state := &hostedShare{ID: shareID, ThreadID: threadID, Token: token, Runtime: runtime, ExpiresAt: expires, Transports: transports}
	if !app.publishShare(threadID, op, state) {
		writeError(response, http.StatusConflict, "share_cancelled", "Share creation was cancelled; no invitation was published")
		return
	}
	published = true
	payload := map[string]any{"actor": "Owner", "shareId": shareID, "transports": transports, "warnings": runtime.Warnings()}
	if degraded {
		payload["degraded"] = true
	}
	_, _ = app.store.AppendEvent(request.Context(), threadID, "share.created", jsonBytes(payload))
	detail, err := app.buildThreadDetail(request.Context(), threadID, accessFrom(request))
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, detail)
}

func (app *App) handleRevokeShare(response http.ResponseWriter, request *http.Request) {
	threadID, ok := app.authorizeThread(response, request)
	if !ok {
		return
	}
	app.mu.Lock()
	if app.sharesClosed {
		app.mu.Unlock()
		writeError(response, http.StatusServiceUnavailable, "share_closed", "Share service is closing")
		return
	}
	op := app.shareStarting[threadID]
	if op != nil {
		op.cancel()
	}
	state := app.shares[threadID]
	if state != nil {
		delete(app.shares, threadID)
		delete(app.shareByID, state.ID)
	}
	app.shareWG.Add(1)
	app.mu.Unlock()
	defer app.shareWG.Done()
	if state == nil && op == nil {
		writeError(response, http.StatusNotFound, "share_not_found", "No active share")
		return
	}
	if state != nil {
		app.retireShare(state, time.Time{}, "share.revoked", "Owner")
	}
	detail, err := app.buildThreadDetail(request.Context(), threadID, accessFrom(request))
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, detail)
}

func (app *App) expireHostedShare(threadID, shareID string, expiredAt time.Time) {
	app.mu.Lock()
	state := app.shareByID[shareID]
	if state == nil {
		state = app.shares[threadID]
	}
	if app.sharesClosed || state == nil || state.ID != shareID {
		app.mu.Unlock()
		return
	}
	if current := app.shares[threadID]; current == state {
		delete(app.shares, threadID)
	}
	delete(app.shareByID, shareID)
	app.shareWG.Add(1)
	app.mu.Unlock()
	defer app.shareWG.Done()
	app.retireShare(state, expiredAt, "share.expired", "System")
}

func (app *App) remoteHandler(threadID, shareID string) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !strings.HasPrefix(request.URL.Path, "/api/v1/") {
			http.NotFound(response, request)
			return
		}
		if !app.isPublishedShare(threadID, shareID) {
			writeError(response, http.StatusGone, "share_inactive", "Share is not active on this host")
			return
		}
		if !app.authorizeShareCapability(response, request, shareID) {
			return
		}
		participantID := strings.TrimSpace(request.Header.Get("X-TeamCross-Participant-ID"))
		if participantID == "" || len(participantID) > 128 {
			writeError(response, http.StatusBadRequest, "participant", "Join proxy participant identity is required")
			return
		}
		identity := access{
			Mode: "share", Role: "observer", ThreadID: threadID, ShareID: shareID,
			ParticipantID: participantID, Name: cleanParticipantName(request.Header.Get("X-TeamCross-Participant-Name")),
			Transport: strings.TrimSpace(request.Header.Get("X-TeamCross-Transport")),
		}
		_, err := app.store.UpsertParticipant(request.Context(), domain.Participant{
			ID: participantID, ShareID: shareID, Name: identity.Name,
			Role: persistedParticipantRole(identity.Transport),
		})
		if err != nil {
			writeDomainError(response, err)
			return
		}
		app.withAccess(identity, app.api).ServeHTTP(response, request)
	})
}

type controlRequest struct {
	Action           string `json:"action"`
	CommandID        string `json:"commandId,omitempty"`
	ExpectedRevision int64  `json:"expectedRevision,omitempty"`
	LeaseEpoch       int64  `json:"leaseEpoch,omitempty"`
}

func (app *App) handleControl(response http.ResponseWriter, request *http.Request) {
	threadID, ok := app.authorizeThread(response, request)
	if !ok {
		return
	}
	identity := accessFrom(request)
	var input controlRequest
	if !decodeJSON(response, request, &input) {
		return
	}
	if identity.Mode != "share" {
		app.handleOwnerControl(response, request, threadID, input)
		return
	}
	if input.CommandID == "" {
		writeError(response, http.StatusBadRequest, "missing_command", "Control writes require commandId")
		return
	}
	if input.Action == "" {
		input.Action = "request"
	}
	if input.Action != "request" && input.Action != "renew" && input.Action != "release" {
		writeError(response, http.StatusBadRequest, "invalid_action", "Unknown control action")
		return
	}
	duplicate, err := app.claimRemoteCommand(request.Context(), identity, input.CommandID, "control."+input.Action, input.ExpectedRevision, input.LeaseEpoch, input)
	if err != nil {
		writeDomainError(response, err)
		return
	}
	if !duplicate {
		switch input.Action {
		case "request":
			lease, acquireErr := app.store.AcquireControl(request.Context(), identity.ShareID, identity.ParticipantID, time.Time{}, 60*time.Second)
			if acquireErr == nil {
				_, acquireErr = app.store.AppendEventExpected(request.Context(), threadID, input.ExpectedRevision, "control.acquired", jsonBytes(map[string]any{"actor": identity.Name, "participantId": identity.ParticipantID, "leaseEpoch": lease.Epoch}))
				if acquireErr != nil {
					_, _ = app.store.PreemptControl(request.Context(), identity.ShareID, "", time.Time{}, 0)
				}
			}
			err = acquireErr
		case "renew":
			err = app.store.ValidateCommandFence(request.Context(), identity.ShareID, identity.ParticipantID, input.ExpectedRevision, input.LeaseEpoch, time.Time{})
			if err == nil {
				_, err = app.store.RenewControl(request.Context(), identity.ShareID, identity.ParticipantID, input.LeaseEpoch, time.Time{}, 60*time.Second)
			}
		case "release":
			err = app.store.ValidateCommandFence(request.Context(), identity.ShareID, identity.ParticipantID, input.ExpectedRevision, input.LeaseEpoch, time.Time{})
			if err == nil {
				_, err = app.store.ReleaseControl(request.Context(), identity.ShareID, identity.ParticipantID, input.LeaseEpoch, time.Time{})
			}
			if err == nil {
				_, err = app.store.AppendEventExpected(request.Context(), threadID, input.ExpectedRevision, "control.released", jsonBytes(map[string]any{"actor": identity.Name, "participantId": identity.ParticipantID}))
			}
		}
		if err != nil {
			_, _ = app.store.CompleteCommand(request.Context(), identity.ShareID, input.CommandID, "error", "", err.Error(), time.Time{})
			writeDomainError(response, err)
			return
		}
		_, _ = app.store.CompleteCommand(request.Context(), identity.ShareID, input.CommandID, "completed", "", "", time.Time{})
	}
	detail, err := app.buildThreadDetail(request.Context(), threadID, identity)
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, detail)
}

func (app *App) handleOwnerControl(response http.ResponseWriter, request *http.Request, threadID string, input controlRequest) {
	if input.Action != "revoke" {
		writeError(response, http.StatusBadRequest, "owner_action", "The host control action must be revoke")
		return
	}
	state := app.activeShare(threadID)
	if state == nil {
		writeError(response, http.StatusNotFound, "share_not_found", "No active share")
		return
	}
	lease, _ := app.store.GetControlLease(request.Context(), state.ID)
	_, err := app.store.AppendEventExpected(request.Context(), threadID, input.ExpectedRevision, "control.revoked", jsonBytes(map[string]any{
		"actor": "Owner", "participantId": lease.ParticipantID, "leaseEpoch": lease.Epoch,
	}))
	if err != nil {
		writeDomainError(response, err)
		return
	}
	if _, err := app.store.PreemptControl(request.Context(), state.ID, "", time.Time{}, 0); err != nil {
		writeDomainError(response, err)
		return
	}
	detail, err := app.buildThreadDetail(request.Context(), threadID, accessFrom(request))
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, detail)
}

func (app *App) activeShare(threadID string) *hostedShare {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.shares[threadID]
}

func (app *App) validateRemoteControl(ctx context.Context, identity access, expectedRevision, leaseEpoch int64) error {
	if identity.Mode != "share" {
		return nil
	}
	return app.store.ValidateCommandFence(ctx, identity.ShareID, identity.ParticipantID, expectedRevision, leaseEpoch, time.Time{})
}
