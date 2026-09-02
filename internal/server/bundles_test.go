package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestOfflineImportDetailAndPatchCannotRunHostFilters(t *testing.T) {
	repo := serverTestRepository(t)
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("*.txt filter=host diff=host\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	serverTestGit(t, repo, "add", ".gitattributes")
	serverTestGit(t, repo, "commit", "-m", "attributes")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := newIntegrationApp(t, repo)
	detail := captureForContinuation(t, app)
	bundle, err := app.buildOfflineBundle(context.Background(), detail.ID, detail.Rounds[0].ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(t.TempDir(), "executed")
	script := filepath.Join(t.TempDir(), "filter")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf unsafe > '"+sentinel+"'\ncat\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(config, []byte("[filter \"host\"]\n clean = "+script+"\n smudge = "+script+"\n process = "+script+"\n required = true\n[diff \"host\"]\n textconv = "+script+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	response := requestJSON(t, app.Handler(), http.MethodPost, "/api/v1/bundles/import", bundle, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("import=%d %s", response.Code, response.Body.String())
	}
	var imported threadDetail
	decodeResponse(t, response, &imported)
	for _, path := range []string{"/api/v1/threads/" + imported.ID, "/api/v1/threads/" + imported.ID + "/patch"} {
		response := requestJSON(t, app.Handler(), http.MethodGet, path, nil, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("read=%d %s", response.Code, response.Body.String())
		}
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("import/detail/patch executed host filter")
	}
}

func TestRemoteCannotContinueForkExportOrImport(t *testing.T) {
	app := newIntegrationApp(t, serverTestRepository(t))
	detail := captureForContinuation(t, app)
	remote := app.withAccess(access{Mode: "share", Role: "controller", ThreadID: detail.ID}, app.api)
	for _, path := range []string{"/api/v1/threads/" + detail.ID + "/continue", "/api/v1/threads/" + detail.ID + "/fork", "/api/v1/threads/" + detail.ID + "/bundles", "/api/v1/bundles/import"} {
		response := requestJSON(t, remote, http.MethodPost, path, map[string]any{}, nil)
		if response.Code != http.StatusForbidden {
			t.Fatalf("remote %s=%d %s", path, response.Code, response.Body.String())
		}
	}
}
