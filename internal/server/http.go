package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"teamcross/internal/domain"
)

type access struct {
	Mode          string
	Role          string
	ThreadID      string
	ShareID       string
	ParticipantID string
	Name          string
	Transport     string
}

type accessKey struct{}

func accessFrom(request *http.Request) access {
	value, _ := request.Context().Value(accessKey{}).(access)
	if value.Mode == "" {
		value = access{Mode: "host", Role: "owner"}
	}
	return value
}

func (app *App) withAccess(value access, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		next.ServeHTTP(response, request.WithContext(context.WithValue(request.Context(), accessKey{}, value)))
	})
}

func (app *App) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/info", app.handleInfo)
	mux.HandleFunc("POST /api/v1/capture/preview", app.hostOnly(app.handleCapturePreview))
	mux.HandleFunc("GET /api/v1/threads", app.handleListThreads)
	mux.HandleFunc("POST /api/v1/threads", app.hostOnly(app.handleCreateThread))
	mux.HandleFunc("GET /api/v1/threads/{threadID}", app.handleGetThread)
	mux.HandleFunc("GET /api/v1/threads/{threadID}/events", app.handleEvents)
	mux.HandleFunc("GET /api/v1/threads/{threadID}/patch", app.handlePatch)
	mux.HandleFunc("POST /api/v1/threads/{threadID}/annotations", app.handleCreateAnnotation)
	mux.HandleFunc("POST /api/v1/threads/{threadID}/evidence", app.hostOnly(app.handleCreateEvidence))
	mux.HandleFunc("GET /api/v1/threads/{threadID}/evidence/{evidenceID}", app.handleGetEvidence)
	mux.HandleFunc("POST /api/v1/threads/{threadID}/shares", app.hostOnly(app.handleCreateShare))
	mux.HandleFunc("DELETE /api/v1/threads/{threadID}/shares/current", app.hostOnly(app.handleRevokeShare))
	mux.HandleFunc("POST /api/v1/threads/{threadID}/control", app.handleControl)
	mux.HandleFunc("POST /api/v1/threads/{threadID}/agent/send", app.handleAgentSend)
	mux.HandleFunc("POST /api/v1/threads/{threadID}/agent/steer", app.handleAgentSteer)
	mux.HandleFunc("POST /api/v1/threads/{threadID}/agent/interrupt", app.handleAgentInterrupt)
	mux.HandleFunc("POST /api/v1/threads/{threadID}/agent/input", app.handleAgentInput)
	mux.HandleFunc("POST /api/v1/threads/{threadID}/agent/switch", app.hostOnly(app.handleAgentSwitch))
	mux.HandleFunc("GET /api/v1/sessions/stored", app.hostOnly(app.handleStoredSessions))
	mux.HandleFunc("POST /api/v1/threads/{threadID}/sessions/import", app.hostOnly(app.handleImportSession))
	return mux
}

func (app *App) hostOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if accessFrom(request).Mode != "host" {
			writeError(response, http.StatusForbidden, "owner_only", "This operation is available only on the host")
			return
		}
		next(response, request)
	}
}

func (app *App) authorizeThread(response http.ResponseWriter, request *http.Request) (string, bool) {
	threadID := request.PathValue("threadID")
	identity := accessFrom(request)
	if identity.Mode == "share" && identity.ThreadID != threadID {
		writeError(response, http.StatusNotFound, "not_found", "Thread not found")
		return "", false
	}
	return threadID, true
}

func decodeJSON(response http.ResponseWriter, request *http.Request, destination any) bool {
	reader := http.MaxBytesReader(response, request.Body, 25<<20)
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_json", err.Error())
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "invalid_json", "Request must contain one JSON value")
		return false
	}
	return true
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	if err := json.NewEncoder(response).Encode(value); err != nil {
		return
	}
}

func writeError(response http.ResponseWriter, status int, code, message string) {
	writeJSON(response, status, map[string]any{"error": message, "code": code})
}

func writeDomainError(response http.ResponseWriter, err error) {
	var replayError *commandReplayError
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(response, http.StatusNotFound, "not_found", "Resource not found")
	case errors.Is(err, domain.ErrRevisionConflict):
		writeError(response, http.StatusConflict, "stale_revision", "Thread changed; refresh and retry")
	case errors.Is(err, domain.ErrLeaseHeld):
		writeError(response, http.StatusConflict, "control_held", "Another collaborator currently holds control")
	case errors.Is(err, domain.ErrLeaseFence), errors.Is(err, domain.ErrLeaseExpired):
		writeError(response, http.StatusConflict, "stale_lease", "The control lease expired or was replaced")
	case errors.Is(err, domain.ErrCommandConflict):
		writeError(response, http.StatusConflict, "command_conflict", "The command ID was already used with different input")
	case errors.Is(err, errCommandInProgress):
		writeError(response, http.StatusConflict, "command_in_progress", "The command is still running; retry after it completes")
	case errors.As(err, &replayError):
		writeError(response, http.StatusConflict, "command_failed", replayError.Error())
	default:
		writeError(response, http.StatusInternalServerError, "internal", err.Error())
	}
}

func cleanParticipantName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Collaborator"
	}
	runes := []rune(value)
	if len(runes) > 64 {
		runes = runes[:64]
	}
	return string(runes)
}

func requiredString(value, name string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	return nil
}
