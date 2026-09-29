// Package desktopactions exposes fixed UI capabilities, never generic commands.
package desktopactions

import (
	"encoding/json"
	"io"
	"net/http"
)

func New(copyText func(string) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/desktop/clipboard" || r.URL.RawPath != "" {
			http.NotFound(w, r)
			return
		}
		var in struct {
			Text string `json:"text"`
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
		if err != nil || len(body) > 1<<20 || json.Unmarshal(body, &in) != nil {
			http.Error(w, "invalid clipboard request", http.StatusBadRequest)
			return
		}
		if !copyText(in.Text) {
			http.Error(w, "clipboard unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
}
