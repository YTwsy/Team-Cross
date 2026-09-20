package collab

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"teamcross/internal/materialstore"
)

func (a *App) publicationHTTP(w http.ResponseWriter, r *http.Request, path string) bool {
	if path != "publications/source" && path != "publications/preview" && path != "publications/draft" && path != "publications/read-draft" {
		return false
	}
	if r.Method != "POST" {
		http.NotFound(w, r)
		return true
	}
	switch path {
	case "publications/source":
		var in struct {
			Provider string `json:"provider"`
			SourceID string `json:"sourceId"`
			Compact  bool   `json:"compact,omitempty"`
		}
		if !decode(w, r, &in) {
			return true
		}
		out, err := a.FreezeSource(r.Context(), in.Provider, in.SourceID)
		if err == nil && in.Compact {
			respond(w, publicationDraftSummary(out), nil)
		} else {
			respond(w, out, err)
		}
	case "publications/preview":
		var in PublicationSelection
		if !decode(w, r, &in) {
			return true
		}
		out, err := a.PreviewPublication(in)
		if err == nil && in.Compact {
			respond(w, publicationDraftSummary(out), nil)
		} else {
			respond(w, out, err)
		}
	case "publications/draft":
		var in struct {
			DraftID string `json:"draftId"`
		}
		if !decode(w, r, &in) {
			return true
		}
		out, err := a.loadDraft(in.DraftID)
		respond(w, out, err)
	case "publications/read-draft":
		var in PublicationDraftRead
		if !decode(w, r, &in) {
			return true
		}
		out, err := a.ReadPublicationDraft(in)
		respond(w, out, err)
	}
	return true
}

// materialUploadHTTP is remote-only. The local WebGUI and personal MCP hand a
// draft ID to their own Core; only Core-to-Core publication sends manifests and
// raw blobs across the collaboration transport.
func (s *Session) materialUploadHTTP(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path == "/v2/material-negotiate" {
		if r.Method != "POST" {
			http.NotFound(w, r)
			return true
		}
		var in publicationNegotiate
		if !decode(w, r, &in) {
			return true
		}
		out, err := s.negotiatePublication(r.Context(), in)
		respond(w, out, err)
		return true
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v2/"), "/")
	if len(parts) < 3 || parts[0] != "material-uploads" {
		return false
	}
	if len(parts) == 3 && parts[2] == "commit" {
		if r.Method != "POST" {
			http.NotFound(w, r)
			return true
		}
		out, err := s.commitPublicationUpload(r.Context(), parts[1])
		respond(w, out, err)
		return true
	}
	if len(parts) == 4 && parts[2] == "blobs" {
		if r.Method != "PUT" {
			http.NotFound(w, r)
			return true
		}
		r.Body = http.MaxBytesReader(w, r.Body, materialstore.MaxBlobBytes+1)
		body, err := io.ReadAll(r.Body)
		if err != nil || len(body) > materialstore.MaxBlobBytes {
			respond(w, nil, fmt.Errorf("blob 超过 8 MiB 或无法读取"))
			return true
		}
		err = s.uploadPublicationBlob(r.Context(), parts[1], parts[3], string(body))
		respond(w, map[string]bool{"uploaded": err == nil}, err)
		return true
	}
	http.NotFound(w, r)
	return true
}
func (s *Session) materialOperation(ctx context.Context, action string, in any) (any, error) {
	switch action {
	case "materials":
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.closed || !s.callerValidLocked(ctx) {
			return nil, fmt.Errorf("共享已结束")
		}
		return s.materialDirectoryLocked(), nil
	case "read-material":
		return s.readMaterial(ctx, in.(MaterialRead))
	case "withdraw-material":
		return s.withdrawMaterial(ctx, in.(materialIDInput).MaterialID)
	case "publication-status":
		return s.publicationStatus(ctx, in.(publicationIDInput).RequestID)
	}
	return nil, fmt.Errorf("材料操作不受支持")
}

type materialIDInput struct {
	MaterialID string `json:"materialId"`
}
type publicationIDInput struct {
	RequestID string `json:"requestId"`
}

// One router handles both local proxies and remote access. Export/previews stay
// on the publisher's Core; a guest can only upload an explicit frozen payload.
func materialHTTP(w http.ResponseWriter, r *http.Request, action string, remote bool, call func(string, any) (any, error)) bool {
	if action != "materials" && action != "read-material" && action != "withdraw-material" && action != "publication-status" {
		return false
	}
	if action == "materials" && r.Method == "GET" {
		out, err := call(action, nil)
		respond(w, out, err)
		return true
	}
	if action == "materials" && remote {
		// Remote publishing is intentionally available only through negotiate,
		// per-blob PUT, and commit. Keeping a second whole-bundle write route
		// would reintroduce the large, non-resumable request this protocol removes.
		http.NotFound(w, r)
		return true
	}
	if r.Method != "POST" {
		http.NotFound(w, r)
		return true
	}
	var in any
	switch action {
	case "materials":
		var v PublishInput
		if !decode(w, r, &v) {
			return true
		}
		in = v
	case "read-material":
		var v MaterialRead
		if !decode(w, r, &v) {
			return true
		}
		in = v
	case "withdraw-material":
		var v materialIDInput
		if !decode(w, r, &v) {
			return true
		}
		in = v
	case "publication-status":
		var v publicationIDInput
		if !decode(w, r, &v) {
			return true
		}
		in = v
	}
	out, err := call(action, in)
	respond(w, out, err)
	return true
}
