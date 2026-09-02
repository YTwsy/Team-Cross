package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"teamcross/internal/domain"
)

var errCommandInProgress = errors.New("command is still in progress")

type commandReplayError struct {
	message string
}

func (err *commandReplayError) Error() string {
	if err.message == "" {
		return "previous command attempt failed"
	}
	return err.message
}

func (app *App) handleEvents(response http.ResponseWriter, request *http.Request) {
	threadID, ok := app.authorizeThread(response, request)
	if !ok {
		return
	}
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeError(response, http.StatusInternalServerError, "streaming", "Streaming is unsupported")
		return
	}
	after := int64(0)
	if value := request.Header.Get("Last-Event-ID"); value != "" {
		after, _ = strconv.ParseInt(value, 10, 64)
	}
	if value := request.URL.Query().Get("after"); value != "" {
		if queryAfter, err := strconv.ParseInt(value, 10, 64); err == nil && queryAfter > after {
			after = queryAfter
		}
	}
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache, no-transform")
	response.Header().Set("Connection", "keep-alive")
	response.WriteHeader(http.StatusOK)
	flusher.Flush()
	ticker := time.NewTicker(350 * time.Millisecond)
	keepAlive := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	defer keepAlive.Stop()
	for {
		events, err := app.store.EventsAfter(request.Context(), threadID, after, 500)
		if err != nil {
			return
		}
		for _, event := range events {
			view := convertEvent(event)
			payload, _ := json.Marshal(view)
			if _, err := fmt.Fprintf(response, "id: %d\ndata: %s\n\n", event.Seq, payload); err != nil {
				return
			}
			after = event.Seq
		}
		if len(events) > 0 {
			flusher.Flush()
		}
		select {
		case <-request.Context().Done():
			return
		case <-ticker.C:
		case <-keepAlive.C:
			_, _ = response.Write([]byte(": keepalive\n\n"))
			flusher.Flush()
		}
	}
}

type annotationRequest struct {
	Body             string `json:"body"`
	File             string `json:"file,omitempty"`
	Line             int    `json:"line,omitempty"`
	CommandID        string `json:"commandId,omitempty"`
	ExpectedRevision int64  `json:"expectedRevision,omitempty"`
	LeaseEpoch       int64  `json:"leaseEpoch,omitempty"`
}

func (app *App) handleCreateAnnotation(response http.ResponseWriter, request *http.Request) {
	threadID, ok := app.authorizeThread(response, request)
	if !ok {
		return
	}
	var input annotationRequest
	if !decodeJSON(response, request, &input) {
		return
	}
	input.Body = strings.TrimSpace(input.Body)
	if input.Body == "" {
		writeError(response, http.StatusBadRequest, "missing_body", "Annotation body is required")
		return
	}
	identity := accessFrom(request)
	annotation := domain.Annotation{ThreadID: threadID, ParticipantID: identity.ParticipantID, Path: input.File, StartLine: input.Line, EndLine: input.Line, Body: input.Body}
	if identity.Mode == "share" {
		if input.CommandID == "" {
			writeError(response, http.StatusBadRequest, "missing_command", "Remote annotations require commandId")
			return
		}
		if duplicate, commandErr := app.claimRemoteCommand(request.Context(), identity, input.CommandID, "annotate", input.ExpectedRevision, input.LeaseEpoch, input); commandErr != nil {
			writeDomainError(response, commandErr)
			return
		} else if duplicate {
			detail, err := app.buildThreadDetail(request.Context(), threadID, identity)
			if err != nil {
				writeDomainError(response, err)
				return
			}
			writeJSON(response, http.StatusOK, detail)
			return
		}
		_, _, err := app.store.CreateAnnotationExpected(request.Context(), annotation, input.ExpectedRevision)
		if err != nil {
			_, _ = app.store.CompleteCommand(request.Context(), identity.ShareID, input.CommandID, "error", "", err.Error(), time.Time{})
			writeDomainError(response, err)
			return
		}
		_, _ = app.store.CompleteCommand(request.Context(), identity.ShareID, input.CommandID, "completed", "", "", time.Time{})
	} else {
		created, err := app.store.CreateAnnotation(request.Context(), annotation)
		if err != nil {
			writeDomainError(response, err)
			return
		}
		if _, err := app.store.AppendEvent(request.Context(), threadID, "annotation.created", jsonBytes(map[string]any{"actor": "Owner", "annotationId": created.ID})); err != nil {
			writeDomainError(response, err)
			return
		}
	}
	detail, err := app.buildThreadDetail(request.Context(), threadID, identity)
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, detail)
}

func (app *App) claimRemoteCommand(ctx context.Context, identity access, id, kind string, expectedRevision, leaseEpoch int64, requestValue any) (bool, error) {
	object, err := app.store.PutObject(ctx, remoteCommandRequestBytes(identity, requestValue), "application/vnd.teamcross.command+json")
	if err != nil {
		return false, err
	}
	command, created, err := app.store.ClaimCommand(ctx, domain.Command{
		ShareID: identity.ShareID, ID: id, Kind: kind, ExpectedRevision: expectedRevision,
		LeaseEpoch: leaseEpoch, RequestObject: object.Hash,
	})
	if err != nil || created {
		return false, err
	}
	return replayCommand(command)
}

// replayRemoteCommand checks for a durable result before mutable preconditions
// such as the current Thread revision, control lease, managed Run, or Bridge.
// A completed command has already consumed those preconditions and must be safe
// to retry after its HTTP response was lost. A new command returns false and is
// validated and atomically claimed by the normal dispatch path.
func (app *App) replayRemoteCommand(ctx context.Context, identity access, id, kind string, expectedRevision, leaseEpoch int64, requestValue any) (bool, error) {
	command, err := app.store.GetCommand(ctx, identity.ShareID, id)
	if errors.Is(err, domain.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if command.Kind != kind || command.ExpectedRevision != expectedRevision || command.LeaseEpoch != leaseEpoch {
		return false, domain.ErrCommandConflict
	}
	persistedRequest, err := app.store.GetObject(ctx, command.RequestObject)
	if err != nil {
		return false, err
	}
	if !bytes.Equal(persistedRequest, remoteCommandRequestBytes(identity, requestValue)) {
		return false, domain.ErrCommandConflict
	}
	return replayCommand(command)
}

func remoteCommandRequestBytes(identity access, requestValue any) []byte {
	return jsonBytes(struct {
		ParticipantID string `json:"participantId"`
		Request       any    `json:"request"`
	}{ParticipantID: identity.ParticipantID, Request: requestValue})
}

func replayCommand(command domain.Command) (bool, error) {
	switch command.Status {
	case "completed":
		return true, nil
	case "error", "failed":
		return false, &commandReplayError{message: command.ErrorText}
	default:
		return false, errCommandInProgress
	}
}

func (app *App) handleCreateEvidence(response http.ResponseWriter, request *http.Request) {
	threadID, ok := app.authorizeThread(response, request)
	if !ok {
		return
	}
	var input struct {
		Kind          string `json:"kind"`
		Name          string `json:"name"`
		Source        string `json:"source,omitempty"`
		Content       string `json:"content,omitempty"`
		ContentBase64 string `json:"contentBase64,omitempty"`
		MIMEType      string `json:"mimeType,omitempty"`
	}
	if !decodeJSON(response, request, &input) {
		return
	}
	if input.Kind == "" {
		input.Kind = "text"
	}
	if strings.TrimSpace(input.Name) == "" {
		input.Name = "Attached evidence"
	}
	if input.Content != "" && input.ContentBase64 != "" {
		writeError(response, http.StatusBadRequest, "evidence_content", "Provide either content or contentBase64, not both")
		return
	}
	content := []byte(input.Content)
	if input.ContentBase64 != "" {
		decoded, decodeErr := base64.StdEncoding.DecodeString(input.ContentBase64)
		if decodeErr != nil {
			writeError(response, http.StatusBadRequest, "evidence_content", "contentBase64 is not valid base64")
			return
		}
		content = decoded
	}
	if len(content) > 5<<20 {
		writeError(response, http.StatusRequestEntityTooLarge, "evidence_too_large", "Evidence files are limited to 5 MiB in this prototype")
		return
	}
	mimeType := strings.TrimSpace(input.MIMEType)
	if mimeType == "" {
		mimeType = "text/plain; charset=utf-8"
	}
	if _, _, parseErr := mime.ParseMediaType(mimeType); parseErr != nil {
		writeError(response, http.StatusBadRequest, "evidence_mime", "mimeType is invalid")
		return
	}
	metadata, _ := json.Marshal(map[string]string{"mimeType": mimeType})
	object, err := app.store.PutObject(request.Context(), content, mimeType)
	if err != nil {
		writeDomainError(response, err)
		return
	}
	evidence, err := app.store.CreateEvidence(request.Context(), domain.Evidence{ThreadID: threadID, Kind: input.Kind, Title: input.Name, Source: input.Source, ObjectHash: object.Hash, Metadata: metadata})
	if err != nil {
		writeDomainError(response, err)
		return
	}
	if _, err := app.store.AppendEvent(request.Context(), threadID, "evidence.attached", jsonBytes(map[string]any{"actor": "Owner", "evidenceId": evidence.ID, "name": evidence.Title})); err != nil {
		writeDomainError(response, err)
		return
	}
	detail, err := app.buildThreadDetail(request.Context(), threadID, accessFrom(request))
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, detail)
}

func (app *App) handleGetEvidence(response http.ResponseWriter, request *http.Request) {
	threadID, ok := app.authorizeThread(response, request)
	if !ok {
		return
	}
	evidence, err := app.store.GetEvidence(request.Context(), threadID, request.PathValue("evidenceID"))
	if err != nil {
		writeDomainError(response, err)
		return
	}
	content, err := app.store.GetObject(request.Context(), evidence.ObjectHash)
	if err != nil {
		writeDomainError(response, err)
		return
	}
	mimeType := "application/octet-stream"
	var metadata struct {
		MIMEType string `json:"mimeType"`
	}
	if json.Unmarshal(evidence.Metadata, &metadata) == nil && metadata.MIMEType != "" {
		mimeType = metadata.MIMEType
	}
	disposition := "inline"
	if request.URL.Query().Get("download") == "1" || (!strings.HasPrefix(mimeType, "text/") && mimeType != "application/json") {
		disposition = "attachment"
	}
	response.Header().Set("Content-Type", mimeType)
	response.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": evidence.Title}))
	response.Header().Set("Content-Length", strconv.Itoa(len(content)))
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(content)
}
