package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"teamcross/internal/service"
	"testing"
)

func TestRuntimeStdioHasOnlyScopedToolsAndUsesCurrentCore(t *testing.T) {
	dir, _ := service.Normalize(t.TempDir())
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer scoped-token" || r.URL.Path != "/api/runtime-annotations/shared" {
			t.Error("wrong credentials or scope")
		}
		w.Write([]byte(`{"annotations":[{"id":"note","replies":[{"text":"answer"}]}]}`))
	}))
	defer server.Close()
	connection := service.Connection{URL: server.URL, PID: os.Getpid(), Instance: "fixture", Token: "admin-must-not-be-sent", DataDir: dir}
	data, _ := json.Marshal(connection)
	os.WriteFile(filepath.Join(dir, "connection.json"), data, 0600)
	input := `{"id":1,"method":"initialize"}
{"id":2,"method":"tools/list"}
{"id":3,"method":"tools/call","params":{"name":"send_input","arguments":{"text":"loop"}}}
{"id":4,"method":"tools/call","params":{"name":"read_annotations","arguments":{"id":"other"}}}
{"id":5,"method":"tools/call","params":{"name":"read_annotations","arguments":{}}}
`
	var output bytes.Buffer
	if err := ServeRuntime(context.Background(), dir, "shared", "scoped-token", strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("unscoped calls reached Core", calls)
	}
	var responses []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var value map[string]any
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			t.Fatal(err)
		}
		responses = append(responses, value)
	}
	list := responses[1]["result"].(map[string]any)["tools"].([]any)
	if len(list) != 4 || list[0].(map[string]any)["name"] != "read_annotations" || list[1].(map[string]any)["name"] != "reply_to_annotation" {
		t.Fatal(list)
	}
	for _, i := range []int{2, 3} {
		if responses[i]["result"].(map[string]any)["isError"] != true {
			t.Fatal(responses[i])
		}
	}
	if responses[4]["result"].(map[string]any)["isError"] != false {
		t.Fatal(responses[4])
	}
}

func TestRuntimeHandshakeDoesNotNeedOrStartCore(t *testing.T) {
	var output bytes.Buffer
	input := `{"id":1,"method":"initialize"}
{"id":2,"method":"tools/list"}
`
	if err := ServeRuntime(context.Background(), t.TempDir(), "shared", "token", strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), `"isError":true`) {
		t.Fatal(output.String())
	}
}

func TestPersonalReplyUsesDedicatedEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/collaborations/shared/annotation-replies" {
			t.Error(r.Method, r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if len(body) != 3 || body["annotationId"] != "root" || body["requestId"] != "stable" || body["text"] != "answer" {
			t.Error(body)
		}
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	_, err := (Backend{URL: server.URL, Client: server.Client()}).Invoke(context.Background(), "reply_to_annotation", map[string]any{"id": "shared", "annotationId": "root", "requestId": "stable", "text": "answer"})
	if err != nil {
		t.Fatal(err)
	}
}
