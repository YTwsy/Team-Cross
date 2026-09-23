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

func TestUILanguageSharedPreferenceSurvivesSettingsSave(t *testing.T) {
	a, _, _ := fixture(t)
	handler := a.Handler(http.NotFoundHandler())
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(method, "http://127.0.0.1/api/"+path, strings.NewReader(body))
		handler.ServeHTTP(recorder, req)
		return recorder
	}
	var language struct{ Mode, Resolved string }
	readLanguage := func() {
		t.Helper()
		response := request("GET", "ui-language", "")
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &language) != nil {
			t.Fatalf("language response: %d %s", response.Code, response.Body.String())
		}
	}
	readLanguage()
	if language.Mode != "auto" || (language.Resolved != "en" && language.Resolved != "zh-CN") {
		t.Fatal(language)
	}
	for _, mode := range []string{"en", "zh-CN", "auto"} {
		response := request("POST", "ui-language", `{"mode":"`+mode+`"}`)
		if response.Code != http.StatusOK {
			t.Fatalf("set %s: %d %s", mode, response.Code, response.Body.String())
		}
		readLanguage()
		if language.Mode != mode || (mode != "auto" && language.Resolved != mode) {
			t.Fatal(language)
		}
	}
	if response := request("POST", "ui-language", `{"mode":"fr"}`); response.Code == http.StatusOK {
		t.Fatal("invalid language accepted")
	}
	request("POST", "ui-language", `{"mode":"en"}`)
	if response := request("POST", "settings", `{"binary":"/usr/bin/true"}`); response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	readLanguage()
	if language.Mode != "en" || language.Resolved != "en" {
		t.Fatal("settings save changed language", language)
	}
	data, err := os.ReadFile(filepath.Join(a.Config.DataDir, "settings.json"))
	if err != nil || !strings.Contains(string(data), `"uiLanguage": "en"`) {
		t.Fatalf("preference not persisted: %v %s", err, data)
	}
}
