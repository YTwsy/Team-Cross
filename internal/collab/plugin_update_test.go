package collab

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"teamcross/internal/mcpassets"
	"testing"
)

func TestPluginBundleIsAuthenticatedVersionPinnedAndReadOnly(t *testing.T) {
	a := &App{Token: "fixture-token"}
	for _, tc := range []struct {
		method, path, token, origin string
		status                      int
		contains                    string
	}{
		{"GET", "plugin/panel-version", "fixture-token", "", 200, mcpassets.Current.Revision},
		{"GET", "plugin/panel-bundle?revision=" + mcpassets.Current.Revision, "fixture-token", "", 200, `"script":`},
		{"GET", "plugin/panel-bundle?revision=stale", "fixture-token", "", 409, ""},
		{"GET", "plugin/panel-version", "", "", 403, ""},
		{"GET", "plugin/panel-version", "fixture-token", "http://localhost", 403, ""},
		{"POST", "plugin/panel-version", "fixture-token", "", 403, ""},
	} {
		r := httptest.NewRequest(tc.method, "http://localhost/api/"+tc.path, nil)
		r.Header.Set("Authorization", "Bearer "+tc.token)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		a.Handler(http.NotFoundHandler()).ServeHTTP(w, r)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.contains) {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String()[:min(100, w.Body.Len())])
		}
		if tc.path == "plugin/panel-version" && w.Code == 200 && strings.Contains(w.Body.String(), `"script"`) {
			t.Fatal("version check sent full bundle")
		}
	}
}
