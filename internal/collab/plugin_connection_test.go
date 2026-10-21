package collab

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginConnectionRoutesOnlyExplicitActionsToInstalledHelper(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", filepath.Join(dir, "native-home"))
	binary, log := filepath.Join(dir, "teamcross"), filepath.Join(dir, "calls")
	os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> '"+log+"'\nprintf '{\"state\":\"not_installed\",\"available\":true}'\n"), 0700)
	a := &App{Config: Config{DataDir: dir, Executable: binary}}
	for _, test := range []struct {
		method, body, action string
		status               int
	}{
		{"GET", "", "connection-status", 200},
		{"POST", `{"action":"connect"}`, "connect", 200},
		{"POST", `{"action":"disconnect"}`, "disconnect", 200},
		{"POST", `{"action":"sync"}`, "", 400},
		{"POST", `{"action":"connect","binary":"/untrusted"}`, "", 400},
		{"POST", `{"action":"export"}`, "", 400},
		{"POST", `{"action":"connect"}{"action":"disconnect"}`, "", 400},
		{"DELETE", "", "", 405},
	} {
		before, _ := os.ReadFile(log)
		r := httptest.NewRequest(test.method, "/api/plugin/connection", strings.NewReader(test.body))
		w := httptest.NewRecorder()
		if !a.httpPluginConnection(w, r, "plugin/connection") {
			t.Fatal("not routed")
		}
		if w.Code != test.status {
			t.Fatalf("%+v: %d %s", test, w.Code, w.Body.String())
		}
		after, _ := os.ReadFile(log)
		if test.action == "" && string(before) != string(after) {
			t.Fatal("invalid action reached helper")
		}
		if test.action != "" && !strings.Contains(string(after[len(before):]), "plugin "+test.action+" --data-dir "+dir) {
			t.Fatal(string(after))
		}
	}
}
