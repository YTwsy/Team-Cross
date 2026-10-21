package mcpevents

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// Gateway serves only the event protocol. The forwarder authenticates to the
// local Core separately; no public request path is forwarded verbatim.
func Gateway(forward func(context.Context, string, json.RawMessage) (any, int, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Path != "/mcp" || r.URL.RawQuery != "" {
			http.NotFound(w, r)
			return
		}
		if r.Method != "POST" {
			w.Header().Set("Allow", "POST")
			http.Error(w, "Method not allowed", 405)
			return
		}
		if r.Header.Get("Origin") != "" {
			http.Error(w, "Origin not allowed", 403)
			return
		}
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer TC_EVT_") || len(auth) > 256 {
			http.Error(w, "Unauthorized", 401)
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
		if err != nil {
			http.Error(w, "Request too large", 413)
			return
		}
		body, status, err := forward(r.Context(), strings.TrimPrefix(auth, "Bearer "), raw)
		if err != nil {
			http.Error(w, "Local event service unavailable", 503)
			return
		}
		if status < 200 || status > 599 {
			http.Error(w, "Invalid service response", 502)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != nil {
			_ = json.NewEncoder(w).Encode(body)
		}
	})
}
