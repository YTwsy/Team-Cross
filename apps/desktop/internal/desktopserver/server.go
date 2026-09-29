// Package desktopserver serves private bundled assets through Wails, without a
// TCP listener. Only requests from those assets can reach the native relay.
package desktopserver

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"
)

const Header = "X-TeamCross-Desktop"

type Server struct {
	assets     http.Handler
	api        http.Handler
	index      []byte
	capability string
}

func New(assets fs.FS, api http.Handler) (*Server, error) {
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		return nil, err
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	capability := hex.EncodeToString(nonce[:])
	index = bytes.Replace(index, []byte("</head>"), []byte(`<meta name="teamcross-desktop" content="`+capability+`"></head>`), 1)
	return &Server{http.FileServer(http.FS(assets)), api, index, capability}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-src 'none'; frame-ancestors 'none'; form-action 'none'; base-uri 'none'")
	if strings.HasPrefix(r.URL.Path, "/api/") {
		origin := r.Header.Get("Origin")
		if subtle.ConstantTimeCompare([]byte(r.Header.Get(Header)), []byte(s.capability)) != 1 ||
			(origin != "" && origin != "null" && origin != "wails://localhost") {
			http.Error(w, "desktop request rejected", http.StatusForbidden)
			return
		}
		s.api.ServeHTTP(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		s.assets.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		_, _ = w.Write(s.index)
	}
}
