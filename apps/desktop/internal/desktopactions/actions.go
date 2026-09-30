// Package desktopactions exposes fixed UI capabilities, never generic commands.
package desktopactions

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
)

type Actions struct {
	Clipboard func(string) bool
	Open      func(string) bool
	Pin       func() bool
	Menu      func() bool
}

var readingRoute = regexp.MustCompile(`^/library\?item=[a-f0-9]{32}$`)
var collaborationRoute = regexp.MustCompile(`^/collaborations/[A-Za-z0-9-]+$`)

// ValidRoute is deliberately smaller than the browser router. No URL, script,
// pending invitation, or arbitrary fragment is accepted from a window action.
func ValidRoute(route string) bool {
	if len(route) > 128 {
		return false
	}
	return route == "/" || route == "/library" || route == "/settings" || route == "/join" ||
		readingRoute.MatchString(route) || collaborationRoute.MatchString(route)
}

func New(actions Actions) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.RawPath != "" || r.URL.RawQuery != "" {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Path {
		case "/desktop/clipboard", "/desktop/open", "/desktop/pin", "/desktop/menu":
		default:
			http.NotFound(w, r)
			return
		}
		var in struct {
			Text  string `json:"text"`
			Route string `json:"route"`
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
		if err != nil || len(body) > 1<<20 || json.Unmarshal(body, &in) != nil {
			http.Error(w, "invalid desktop request", http.StatusBadRequest)
			return
		}
		var accepted bool
		switch r.URL.Path {
		case "/desktop/clipboard":
			accepted = actions.Clipboard != nil && actions.Clipboard(in.Text)
		case "/desktop/open":
			if !ValidRoute(in.Route) {
				http.Error(w, "invalid desktop route", http.StatusBadRequest)
				return
			}
			accepted = actions.Open != nil && actions.Open(in.Route)
		case "/desktop/pin":
			accepted = actions.Pin != nil && actions.Pin()
		case "/desktop/menu":
			accepted = actions.Menu != nil && actions.Menu()
		}
		if !accepted {
			http.Error(w, "desktop action unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
}
