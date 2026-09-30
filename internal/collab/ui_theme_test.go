package collab

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUIThemePersistsAndCannotBeOverwrittenByClientSettings(t *testing.T) {
	a, _, _ := fixture(t)
	handler := a.Handler(http.NotFoundHandler())
	request := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(method, "http://127.0.0.1/api/"+path, strings.NewReader(body)))
		return w
	}
	if w := request("GET", "ui-theme", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"system"`) {
		t.Fatal(w.Body.String())
	}
	for _, mode := range []string{"light", "system", "dark"} {
		if w := request("POST", "ui-theme", `{"mode":"`+mode+`"}`); w.Code != 200 || a.UITheme() != mode {
			t.Fatal(w.Code, a.UITheme())
		}
	}
	if w := request("POST", "ui-theme", `{"mode":"invalid"}`); w.Code == 200 || a.UITheme() != "dark" {
		t.Fatal(w.Code, a.UITheme())
	}
	if w := request("DELETE", "ui-theme", ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if err := a.SetUILanguage("en"); err != nil {
		t.Fatal(err)
	}
	if w := request("POST", "settings", `{"binary":"/usr/bin/true","uiTheme":"light","uiLanguage":"auto"}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var saved Settings
	data, err := os.ReadFile(filepath.Join(a.Config.DataDir, "settings.json"))
	if err != nil || json.Unmarshal(data, &saved) != nil {
		t.Fatal(err)
	}
	if saved.UITheme != "dark" || saved.UILanguage != "en" || saved.Binary != "/usr/bin/true" {
		t.Fatal(saved)
	}
	if a.UITheme() != "dark" {
		t.Fatal("theme overwritten")
	}
}

func TestUIThemeWriteFailureKeepsConfirmedPreference(t *testing.T) {
	a := &App{Config: Config{DataDir: t.TempDir()}, settings: Settings{UITheme: "light"}}
	if err := os.Mkdir(filepath.Join(a.Config.DataDir, "settings.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := a.SetUITheme("dark"); err == nil {
		t.Fatal("expected persistence error")
	}
	if a.UITheme() != "light" {
		t.Fatal("unpersisted preference applied")
	}
}
