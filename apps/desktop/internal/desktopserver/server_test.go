package desktopserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestPrivateAssetTransport(t *testing.T) {
	calls := 0
	server, err := New(fstest.MapFS{"index.html": {Data: []byte("<html><head></head><body>fixture</body></html>")}}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) }))
	if err != nil {
		t.Fatal(err)
	}
	index := httptest.NewRecorder()
	server.ServeHTTP(index, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(index.Body.String(), server.capability) || index.Header().Get("Content-Security-Policy") == "" || index.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(index)
	}
	for _, tc := range []struct {
		proof, origin string
		status        int
	}{
		{"", "wails://localhost", 403}, {server.capability, "https://example.com", 403},
		{"wrong", "", 403}, {server.capability, "wails://localhost", 204},
		{server.capability, "null", 204}, {server.capability, "", 204},
	} {
		r := httptest.NewRequest("GET", "/api/ui-language", nil)
		r.Header.Set(Header, tc.proof)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatal(tc.origin, w.Code)
		}
	}
	if calls != 3 {
		t.Fatal(calls)
	}
	second, _ := New(fstest.MapFS{"index.html": {Data: []byte("<head></head>")}}, http.NotFoundHandler())
	if second.capability == server.capability {
		t.Fatal("reused capability")
	}
	post := httptest.NewRecorder()
	server.ServeHTTP(post, httptest.NewRequest("POST", "/", nil))
	if post.Code != 405 {
		t.Fatal(post.Code)
	}
}
