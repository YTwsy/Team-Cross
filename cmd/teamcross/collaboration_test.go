package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"teamcross/internal/buildinfo"
	"teamcross/internal/service"
)

func TestCLIQueriesAndInputUseLocalAPI(t *testing.T) {
	data, _ := service.Normalize(t.TempDir())
	var connection service.Connection
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Error("missing authentication")
		}
		if r.URL.Path == "/api/control/status" {
			json.NewEncoder(w).Encode(service.Status{Connection: connection, Running: true})
			return
		}
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Method == "POST" {
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			if in["action"] != "handoff" || in["epoch"] != float64(3) {
				t.Error(in)
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "fixture", "epoch": 3, "invitation": "private"})
	}))
	defer server.Close()
	connection = service.Connection{URL: server.URL, PID: os.Getpid(), Instance: "fixture", Token: "fixture-token", Protocol: buildinfo.ControlProtocol, DataDir: data}
	if err := service.Save(data, connection); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"collaborations", "--id", "fixture"}, {"input", "handoff", "--id", "fixture", "--epoch", "3"}} {
		var out, diagnostic bytes.Buffer
		args = append(args, "--data-dir", data, "--json")
		if err := runCollaboration(args, &out, &diagnostic); err != nil {
			t.Fatal(err)
		}
		if !json.Valid(out.Bytes()) || strings.Contains(out.String(), "private") || strings.Contains(out.String(), "fixture-token") {
			t.Fatal(out.String())
		}
	}
	if strings.Join(calls, ",") != "GET /api/collaborations/fixture,POST /api/collaborations/fixture/action" {
		t.Fatal(calls)
	}
}

func TestCLIInputRequiresExplicitObservedEpoch(t *testing.T) {
	for _, args := range [][]string{{"input", "handoff", "--id", "fixture"}, {"input", "bad", "--id", "fixture", "--epoch", "1"}, {"input", "handoff", "--id", "../other", "--epoch", "1"}} {
		var out bytes.Buffer
		if err := runCollaboration(args, &out, &out); err == nil {
			t.Fatal(args)
		}
	}
}
