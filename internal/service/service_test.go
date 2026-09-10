package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"teamcross/internal/buildinfo"
	"teamcross/internal/problem"
	"testing"
)

func TestProbeIdentityAndProtocol(t *testing.T) {
	data, _ := Normalize(t.TempDir())
	var advertised Connection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private" {
			t.Error("missing credential")
		}
		s := Status{Connection: advertised, Running: true}
		s.Token = ""
		json.NewEncoder(w).Encode(s)
	}))
	defer server.Close()
	c := Connection{URL: server.URL, PID: 42, Instance: "instance-a", Token: "private", Protocol: buildinfo.ControlProtocol, DataDir: data}
	if e := Save(data, c); e != nil {
		t.Fatal(e)
	}
	advertised = c
	s, e := Probe(context.Background(), data)
	if e != nil || s.Token != "private" {
		t.Fatal(s, e)
	}
	advertised.Instance = "another"
	_, e = Probe(context.Background(), data)
	if problem.Describe(e).Code != "instance_mismatch" {
		t.Fatal(e)
	}
	advertised = c
	advertised.Protocol++
	_, e = Probe(context.Background(), data)
	if problem.Describe(e).Code != "version_incompatible" {
		t.Fatal(e)
	}
}
func TestNormalizeAndStablePath(t *testing.T) {
	root := t.TempDir()
	actual := filepath.Join(root, "actual")
	os.Mkdir(actual, 0700)
	os.Symlink(actual, filepath.Join(root, "alias"))
	a, _ := Normalize(filepath.Join(root, "alias", "new"))
	b, _ := Normalize(filepath.Join(actual, "new"))
	if a != b {
		t.Fatal(a, b)
	}
	opt := filepath.Join(root, "opt/teamcross/bin")
	os.MkdirAll(opt, 0700)
	os.WriteFile(filepath.Join(opt, "teamcross"), []byte("test"), 0700)
	if got := StableExecutable(filepath.Join(root, "Cellar/teamcross/1.2.3/bin/teamcross")); got != filepath.Join(opt, "teamcross") {
		t.Fatal(got)
	}
}
func TestReadRejectsNonlocalAndCredentialsInURL(t *testing.T) {
	for _, u := range []string{"https://127.0.0.1:1234", "http://example.com:1234", "http://u:p@127.0.0.1:1234", "http://127.0.0.1:1234/api"} {
		d, _ := Normalize(t.TempDir())
		Save(d, Connection{URL: u, Instance: "x", Token: "x", DataDir: d})
		if _, e := Read(d); e == nil {
			t.Fatal(u)
		}
	}
}

func TestStablePathFromHomebrewBinSymlink(t *testing.T) {
	root, e := Normalize(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	cellar := filepath.Join(root, "Cellar/teamcross/1.2.3")
	for _, p := range []string{filepath.Join(cellar, "bin"), filepath.Join(root, "bin"), filepath.Join(root, "opt")} {
		if e := os.MkdirAll(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	target := filepath.Join(cellar, "bin/teamcross")
	if e := os.WriteFile(target, []byte("test"), 0700); e != nil {
		t.Fatal(e)
	}
	alias := filepath.Join(root, "bin/teamcross")
	if e := os.Symlink(target, alias); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(cellar, filepath.Join(root, "opt/teamcross")); e != nil {
		t.Fatal(e)
	}
	if got := StableExecutable(alias); got != filepath.Join(root, "opt/teamcross/bin/teamcross") {
		t.Fatal(got)
	}
}

func TestStableAppPathSurvivesCaskCommandRemoval(t *testing.T) {
	root, _ := Normalize(t.TempDir())
	helper := filepath.Join(root, "Applications with spaces/Team Cross.app/Contents/Resources/teamcross")
	if err := os.MkdirAll(filepath.Dir(helper), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nprintf '{\"version\":\"0.1.1\"}\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "teamcross")
	if err := os.Symlink(helper, link); err != nil {
		t.Fatal(err)
	}
	stable := StableExecutable(link)
	if stable != helper {
		t.Fatal(stable)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if got := InstalledVersion(context.Background(), stable); got != "0.1.1" {
		t.Fatal(got)
	}
}
