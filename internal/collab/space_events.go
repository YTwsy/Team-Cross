package collab

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"teamcross/internal/mcpevents"
)

func (a *App) startSpaceEvents() error {
	m, err := mcpevents.Open(filepath.Join(a.Config.DataDir, "space-events"), a.spaceEventSnapshot, a.spaceEventRead)
	if err != nil {
		return err
	}
	a.events = m
	ctx, cancel := context.WithCancel(context.Background())
	a.eventsCancel = cancel
	a.eventsWG.Add(1)
	go func() {
		defer a.eventsWG.Done()
		tick := time.NewTicker(3 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				m.Tick(ctx)
			}
		}
	}()
	return nil
}

func (a *App) spaceEventSnapshot(ctx context.Context, id string) ([]mcpevents.Observation, error) {
	a.mu.Lock()
	s, j := a.sessions[id], a.joined[id]
	a.mu.Unlock()
	if s != nil {
		s.mu.Lock()
		revoked := s.closed || s.record.State == "ended"
		s.mu.Unlock()
		if revoked {
			return nil, mcpevents.ErrRevoked
		}
	}
	if j != nil {
		j.mu.Lock()
		revoked := j.ended
		j.mu.Unlock()
		if revoked {
			return nil, mcpevents.ErrRevoked
		}
	}
	if s == nil && j == nil {
		return nil, mcpevents.ErrRevoked
	}
	out, err := a.workbenchCall(ctx, id, "event-snapshot", workbenchInput{})
	if err != nil {
		return nil, err
	}
	return workbenchDecode[[]mcpevents.Observation](out), nil
}

func (s *Session) spaceEventSnapshotLocked() []mcpevents.Observation {
	b := s.record.Workbench
	id := s.record.ID
	out := []mcpevents.Observation{{Key: "brief", Name: "space.brief.updated", Timestamp: b.Brief.UpdatedAt, Data: map[string]any{"spaceId": id, "revision": b.Brief.Revision}}}
	for _, note := range s.record.Annotations {
		ref := LibraryReference{SpaceID: id, Kind: "annotation", AnnotationID: note.ID}
		if err := s.workbenchRefsLocked([]LibraryReference{ref}); err != nil {
			continue
		}
		updated := note.CreatedAt
		for _, reply := range note.Replies {
			if reply.CreatedAt.After(updated) {
				updated = reply.CreatedAt
			}
		}
		out = append(out, mcpevents.Observation{Key: "annotation:" + note.ID, Name: "space.discussion.updated", Timestamp: updated, Data: map[string]any{"spaceId": id, "annotationId": note.ID, "replyCount": len(note.Replies)}})
	}
	for _, r := range b.Requests {
		if (r.State != "completed" && r.State != "failed") || r.Summary == "" {
			continue
		}
		if err := s.workbenchRefsLocked(r.References); err != nil {
			continue
		}
		out = append(out, mcpevents.Observation{Key: "request:" + r.ID, Name: "space.request.completed", Timestamp: r.UpdatedAt, Data: map[string]any{"spaceId": id, "requestId": r.ID, "state": r.State}})
	}
	return out
}

func (a *App) spaceEventRead(ctx context.Context, id, name string, args map[string]any) (any, error) {
	if _, err := a.workbenchCall(ctx, id, "receiver-check", workbenchInput{}); err != nil {
		return nil, err
	}
	switch name {
	case "read_space_brief":
		raw, err := a.workbenchCall(ctx, id, "view", workbenchInput{})
		if err != nil {
			return nil, err
		}
		view := workbenchDecode[WorkbenchView](raw)
		return map[string]any{"spaceId": id, "brief": view.Brief, "assistant": view.Assistant}, nil
	case "get_space_request":
		requestID, _ := args["requestId"].(string)
		return a.workbenchCall(ctx, id, "request", workbenchInput{RequestID: requestID})
	case "read_space_discussion":
		var in HistoryRead
		raw, _ := json.Marshal(args)
		if json.Unmarshal(raw, &in) != nil || in.AnnotationID == "" {
			return nil, fmt.Errorf("invalid discussion reference")
		}
		return a.workbenchCall(ctx, id, "annotations", workbenchInput{Read: in})
	case "read_space_material":
		var in MaterialRead
		raw, _ := json.Marshal(args)
		if json.Unmarshal(raw, &in) != nil || in.MaterialID == "" || in.Version < 1 {
			return nil, fmt.Errorf("invalid fixed material reference")
		}
		return a.target(ctx, id, "POST", "read-material", in)
	}
	return nil, fmt.Errorf("unsupported event read tool")
}

func (a *App) spaceEventsHTTP(w http.ResponseWriter, r *http.Request, path string) bool {
	if path != "event-access" && path != "event-gateway" && !strings.HasPrefix(path, "event-access/") {
		return false
	}
	if path == "event-gateway" {
		// This is reachable only through the local service connection. The
		// standalone listener never forwards its remote bearer as a Core token.
		if r.Method != "POST" || r.Header.Get("Origin") != "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+a.Token)) != 1 {
			http.Error(w, "Forbidden", 403)
			return true
		}
		var in struct {
			Credential string          `json:"credential"`
			Request    json.RawMessage `json:"request"`
		}
		if decode(w, r, &in) {
			body, status := a.events.RPC(r.Context(), in.Credential, in.Request)
			respond(w, map[string]any{"status": status, "body": body}, nil)
		}
		return true
	}
	if path == "event-access" && r.Method == "GET" {
		respond(w, a.events.View(), nil)
		return true
	}
	if r.Method != "POST" {
		http.NotFound(w, r)
		return true
	}
	if path == "event-access" {
		var in struct {
			RequestID string `json:"requestId"`
			SpaceID   string `json:"spaceId"`
			Name      string `json:"name"`
		}
		if decode(w, r, &in) {
			grant, credential, err := a.events.CreateGrant(r.Context(), in.RequestID, in.SpaceID, in.Name)
			respond(w, map[string]any{"connection": grant, "credential": credential}, err)
		}
		return true
	}
	parts := strings.Split(path, "/")
	if len(parts) != 3 {
		http.NotFound(w, r)
		return true
	}
	var in struct {
		State string `json:"state"`
	}
	if decode(w, r, &in) {
		err := a.events.Change(parts[2], parts[1], in.State)
		respond(w, map[string]bool{"ok": err == nil}, err)
	}
	return true
}
