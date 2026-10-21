package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"teamcross/internal/buildinfo"
	"teamcross/internal/problem"
	"testing"
	"time"
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

func TestEnsureUsesDiskBuildAndDefersWithoutInterrupting(t *testing.T) {
	for _, scenario := range []string{"active", "busy", "became-busy", "same", "other-installation", "unreadable"} {
		t.Run(scenario, func(t *testing.T) {
			data, _ := Normalize(t.TempDir())
			binary := filepath.Join(t.TempDir(), "teamcross")
			write := func(path, commit string) {
				t.Helper()
				if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' '{\"version\":\"same-version\",\"commit\":\""+commit+"\",\"protocol\":1}'\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			write(binary, "installed")
			var stops atomic.Int32
			var advertised Status
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					stops.Add(1)
					if r.URL.Path != "/api/control/upgrade" {
						t.Error(r.URL.Path)
					}
					w.WriteHeader(409)
					json.NewEncoder(w).Encode(problem.New("upgrade_busy", "busy", ""))
					return
				}
				json.NewEncoder(w).Encode(advertised)
			}))
			defer server.Close()
			c := Connection{URL: server.URL, PID: 42, Instance: "exact-instance", Token: "private", Version: "same-version", Commit: "running", Protocol: 1, DataDir: data, Executable: binary}
			advertised = Status{Connection: c, Running: true, UpgradeSupported: true}
			switch scenario {
			case "active":
				advertised.Active = 1
			case "busy":
				advertised.UpgradeBlocked = true
			case "same":
				advertised.Commit = "installed"
			case "other-installation":
				advertised.Commit = "installed"
			case "unreadable":
				os.Remove(binary)
			}
			if err := Save(data, c); err != nil {
				t.Fatal(err)
			}
			caller := binary
			if scenario == "other-installation" {
				caller = filepath.Join(t.TempDir(), "teamcross")
				write(caller, "older-caller")
			}
			s, err := Ensure(context.Background(), data, caller, nil)
			if err != nil || s.Instance != c.Instance {
				t.Fatal(s, err)
			}
			pending := scenario == "active" || scenario == "busy" || scenario == "became-busy"
			if s.UpdatePending != pending {
				t.Fatal("wrong build state", s)
			}
			wantStops := int32(0)
			if scenario == "became-busy" {
				wantStops = 1
			}
			if stops.Load() != wantStops {
				t.Fatal("unexpected stop", stops.Load())
			}
		})
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
	for _, formula := range []string{"teamcross", "teamcross-rc"} {
		opt := filepath.Join(root, "opt", formula, "bin")
		os.MkdirAll(opt, 0700)
		os.WriteFile(filepath.Join(opt, "teamcross"), []byte("test"), 0700)
		if got := StableExecutable(filepath.Join(root, "Cellar", formula, "1.2.3/bin/teamcross")); got != filepath.Join(opt, "teamcross") {
			t.Fatal(formula, got)
		}
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
	for _, formula := range []string{"teamcross", "teamcross-rc"} {
		t.Run(formula, func(t *testing.T) {
			root, e := Normalize(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			cellar := filepath.Join(root, "Cellar", formula, "1.2.3")
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
			if e := os.Symlink(cellar, filepath.Join(root, "opt", formula)); e != nil {
				t.Fatal(e)
			}
			if got := StableExecutable(alias); got != filepath.Join(root, "opt", formula, "bin/teamcross") {
				t.Fatal(got)
			}
		})
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

func TestEnsureDoesNotSpawnWhileUnresponsiveCoreOwnsLock(t *testing.T) {
	for _, scenario := range []string{"timeout", "missing-connection", "broken-connection"} {
		t.Run(scenario, func(t *testing.T) {
			data, _ := Normalize(t.TempDir())
			core, err := Lock(data, "core.lock")
			if err != nil {
				t.Fatal(err)
			}
			defer core.Close()
			if scenario == "timeout" {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
				defer server.Close()
				if err := Save(data, Connection{URL: server.URL, PID: 42, Instance: "fixture", Token: "private", DataDir: data}); err != nil {
					t.Fatal(err)
				}
			} else if scenario == "broken-connection" {
				if err := os.WriteFile(filepath.Join(data, "connection.json"), []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			marker := filepath.Join(data, "spawned")
			binary := filepath.Join(data, "helper")
			if err := os.WriteFile(binary, []byte(fmt.Sprintf("#!/bin/sh\nprintf launched > %q\nexit 1\n", marker)), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			_, err = Ensure(ctx, data, binary, nil)
			if err == nil || problem.Describe(err).Code != "core_unresponsive" {
				t.Fatal("lost original Core state", err)
			}
			if scenario == "timeout" && !strings.Contains(problem.Describe(err).Recovery, "context deadline exceeded") {
				t.Fatal("lost probe cause", err)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("spawned another Core")
			}
			if _, err := os.Stat(filepath.Join(data, "core.log")); !os.IsNotExist(err) {
				t.Fatal("opened launch log despite held Core lock")
			}
		})
	}
}
