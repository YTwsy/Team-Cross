package desktopactions

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClipboardIsFixedAndBounded(t *testing.T) {
	var copied []string
	handler := New(func(text string) bool { copied = append(copied, text); return true })
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/desktop/clipboard", `{"text":"中文与 emoji 🧪"}`, 200},
		{"GET", "/desktop/clipboard", "", 404},
		{"POST", "/desktop/open", `{"url":"file:///tmp"}`, 404},
		{"POST", "/desktop/clipboard", `{"text":false}`, 400},
		{"POST", "/desktop/clipboard", `{"text":"` + strings.Repeat("x", 1<<20) + `"}`, 400},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
		if w.Code != tc.status {
			t.Fatal(tc.path, w.Code)
		}
	}
	if len(copied) != 1 || copied[0] != "中文与 emoji 🧪" {
		t.Fatal(copied)
	}
}
