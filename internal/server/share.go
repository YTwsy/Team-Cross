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
		TTLSeconds    int64 `json:"ttlSeconds"`
		AllowDegraded bool  `json:"allowDegraded"`
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
	now := time.Now()
	app.mu.Lock()
	existing := app.shares[threadID]
	expired := existing != nil && !now.Before(existing.ExpiresAt)
	if expired {
		delete(app.shares, threadID)
		delete(app.shareByID, existing.ID)
	}
	app.mu.Unlock()
	if expired {
		if existing.ExpiryTimer != nil {
			existing.ExpiryTimer.Stop()
		}
		existing.Runtime.Revoke()
		_ = existing.Runtime.Close()
		_ = app.store.RevokeShare(request.Context(), existing.ID, existing.ExpiresAt)
		_, _ = app.store.AppendEvent(request.Context(), threadID, "share.expired", jsonBytes(map[string]any{"actor": "System", "shareId": existing.ID}))
		existing = nil
	}
	if existing != nil {
		detail, err := app.buildThreadDetail(request.Context(), threadID, accessFrom(request))
		if err != nil {
			writeDomainError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, detail)
		return
	}
	shareID := uuid.NewString()
	expires := time.Now().UTC().Add(ttl)
	config := share.RuntimeConfig{
		ShareID: shareID, ExpiresAt: expires, Capabilities: share.DefaultCapabilities,
		Handler: app.remoteHandler(threadID, shareID), EnableMDNS: true, EnableTailcat: true,
	}
	runtime, err := share.Start(request.Context(), config)
	degraded := false
	if err != nil && input.AllowDegraded && errors.Is(err, share.ErrTailcatPrewarm) {
		config.EnableTailcat = false
		runtime, err = share.Start(request.Context(), config)
		degraded = true
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
		_ = runtime.Close()
		writeError(response, http.StatusInternalServerError, "invite", err.Error())
		return
	}
	invitation := runtime.Invitation()
	expires = time.Unix(invitation.ExpiresAt, 0).UTC()
	capabilities, _ := json.Marshal(invitation.Capabilities)
	if _, err := app.store.CreateShare(request.Context(), domain.Share{
		ID: shareID, ThreadID: threadID, SecretHash: hashSecret(invitation.Secret),
		ServerSPKI: fmt.Sprintf("%x", invitation.ServerSPKISHA256), Capabilities: capabilities, ExpiresAt: expires,
	}); err != nil {
		_ = runtime.Close()
		writeDomainError(response, err)
		return
	}
	var transports []string
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
	app.mu.Lock()
	app.shares[threadID] = state
	app.shareByID[shareID] = state
	state.ExpiryTimer = time.AfterFunc(time.Until(expires), func() {
		app.expireHostedShare(threadID, shareID, expires)
	})
	app.mu.Unlock()
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
	state := app.shares[threadID]
	if state != nil {
		delete(app.shares, threadID)
		delete(app.shareByID, state.ID)
	}
	app.mu.Unlock()
	if state == nil {
		writeError(response, http.StatusNotFound, "share_not_found", "No active share")
		return
	}
	if state.ExpiryTimer != nil {
		state.ExpiryTimer.Stop()
	}
	state.Runtime.Revoke()
	_ = state.Runtime.Close()
	_ = app.store.RevokeShare(request.Context(), state.ID, time.Time{})
	_, _ = app.store.AppendEvent(request.Context(), threadID, "share.revoked", jsonBytes(map[string]any{"actor": "Owner", "shareId": state.ID}))
	detail, err := app.buildThreadDetail(request.Context(), threadID, accessFrom(request))
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, detail)
}

func (app *App) expireHostedShare(threadID, shareID string, expiredAt time.Time) {
	app.mu.Lock()
	state := app.shares[threadID]
	if state == nil || state.ID != shareID {
		app.mu.Unlock()
		return
	}
	delete(app.shares, threadID)
	delete(app.shareByID, shareID)
	app.mu.Unlock()
	state.Runtime.Revoke()
	_ = state.Runtime.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = app.store.RevokeShare(ctx, shareID, expiredAt)
	_, _ = app.store.AppendEvent(ctx, threadID, "share.expired", jsonBytes(map[string]any{"actor": "System", "shareId": shareID}))
}

func (app *App) remoteHandler(threadID, shareID string) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !strings.HasPrefix(request.URL.Path, "/api/v1/") {
			http.NotFound(response, request)
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
