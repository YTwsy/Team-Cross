package main

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
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

func TestCLIShareKeepsCreatedCollaborationOnInvitationFailure(t *testing.T) {
	data, _ := service.Normalize(t.TempDir())
	id, source := uuid.NewString(), uuid.NewString()
	hash := strings.Repeat("a", 64)
	var connection service.Connection
	creates, invites := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/control/status" {
			json.NewEncoder(w).Encode(service.Status{Connection: connection, Running: true})
			return
		}
		switch r.URL.Path {
		case "/api/collaborations":
			creates++
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			if in["requestId"] != id || in["sourceId"] != source || in["previewHash"] != hash {
				t.Error(in)
			}
			json.NewEncoder(w).Encode(map[string]any{"id": id, "state": "ready", "sessionId": "new-fork"})
		case "/api/collaborations/" + id + "/invitations":
			invites++
			if invites == 1 {
				w.WriteHeader(400)
				w.Write([]byte(`{"code":"network_unavailable","error":"fixture network unavailable"}`))
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"id": id, "state": "ready", "invitation": "tcx3.fixture"})
		default:
			t.Error("unexpected call", r.URL.Path)
		}
	}))
	defer server.Close()
	connection = service.Connection{URL: server.URL, PID: os.Getpid(), Instance: "fixture", Token: "test", Protocol: buildinfo.ControlProtocol, DataDir: data}
	if err := service.Save(data, connection); err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	args := []string{"share", "--data-dir", data, "--json", "--provider", "codex", "--source", source, "--workspace", "existing", "--request-id", id, "--preview-hash", hash, "--transport", "lan"}
	if err := runCollaboration(args, &out, &diagnostic); err == nil {
		t.Fatal("invitation failure hidden")
	}
	var partial map[string]any
	if err := json.Unmarshal(out.Bytes(), &partial); err != nil {
		t.Fatal(out.String(), err)
	}
	if partial["stage"] != "created" || partial["collaboration"].(map[string]any)["id"] != id {
		t.Fatal(partial)
	}
	out.Reset()
	if err := runCollaboration([]string{"invite", "--id", id, "--transport", "lan", "--data-dir", data, "--json"}, &out, &diagnostic); err != nil {
		t.Fatal(err)
	}
	if creates != 1 || invites != 2 || !strings.Contains(out.String(), "invitationUrl") {
		t.Fatal(creates, invites, out.String())
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
