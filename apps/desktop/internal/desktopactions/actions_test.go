package desktopactions

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClipboardIsFixedAndBounded(t *testing.T) {
	var copied []string
	handler := New(Actions{Clipboard: func(text string) bool { copied = append(copied, text); return true }})
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/desktop/clipboard", `{"text":"中文与 emoji 🧪"}`, 200},
		{"GET", "/desktop/clipboard", "", 404},
		{"POST", "/desktop/open", `{"url":"file:///tmp"}`, 400},
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

func TestWindowActionsCannotNavigateOutsideKnownRoutes(t *testing.T) {
	var opened []string
	var pins, menus int
	handler := New(Actions{
		Open: func(route string) bool { opened = append(opened, route); return true },
		Pin:  func() bool { pins++; return true },
		Menu: func() bool { menus++; return false },
	})
	for index, route := range []string{"/", "/settings", "/join", "/library", "/library?item=" + strings.Repeat("a", 32), "/collaborations/abc-123", "https://example.com", "//example.com", "/join/pending", "/library?item=bad", "/collaborations/abc/../x", "/collaborations/x#evil", "javascript:alert(1)"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", "/desktop/open", strings.NewReader(`{"route":"`+route+`"}`)))
		valid := index < 6
		if valid && w.Code != 200 || !valid && w.Code != 400 {
			t.Fatal(route, w.Code)
		}
	}
	if len(opened) != 6 {
		t.Fatal("unexpected navigation", opened)
	}
	for _, tc := range []struct {
		path   string
		status int
	}{{"/desktop/pin", 200}, {"/desktop/menu", 503}, {"/desktop/pin?retry=1", 404}, {"/desktop/execute", 404}} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", tc.path, strings.NewReader(`{}`)))
		if w.Code != tc.status {
			t.Fatal(tc.path, w.Code)
		}
	}
	if pins != 1 || menus != 1 {
		t.Fatal(pins, menus)
	}
}
