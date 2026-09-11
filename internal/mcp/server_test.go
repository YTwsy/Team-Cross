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
	"teamcross/internal/buildinfo"
	"teamcross/internal/service"
	"testing"
)

func TestStdioDoesNotSendInputWhileListing(t *testing.T) {
	calls := []string{}
	dir, _ := service.Normalize(t.TempDir())
	var connection service.Connection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/control/status" {
			json.NewEncoder(w).Encode(service.Status{Connection: connection, Running: true})
			return
		}
		if r.URL.Path == "/api/mcp/observed" {
			w.Write([]byte(`{}`))
			return
		}
		calls = append(calls, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer server.Close()
	connection = service.Connection{URL: server.URL, PID: os.Getpid(), Instance: "test", Token: "test-token", Version: buildinfo.Version, Protocol: buildinfo.ControlProtocol, DataDir: dir}
	b, _ := json.Marshal(connection)
	os.WriteFile(filepath.Join(dir, "connection.json"), b, 0600)
	input := strings.NewReader("{\"id\":1,\"method\":\"initialize\"}\n{\"method\":\"notifications/initialized\"}\n{\"id\":2,\"method\":\"tools/list\"}\n{\"id\":3,\"method\":\"tools/call\",\"params\":{\"name\":\"list_collaborations\",\"arguments\":{}}}\n")
	var output bytes.Buffer
	if e := Serve(context.Background(), dir, input, &output); e != nil {
		t.Fatal(e)
	}
	if len(calls) != 1 || calls[0] != "GET /api/collaborations" {
		t.Fatal(calls)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 {
		t.Fatal(output.String())
	}
	for _, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatal("stdout contains non JSON")
		}
	}
}
func TestSendPreservesTextAndId(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v map[string]any
		json.NewDecoder(r.Body).Decode(&v)
		if v["requestId"] != "stable-id" || v["method"] != "turn/steer" {
			t.Error(v)
		}
		params := v["params"].(map[string]any)
		text := params["input"].([]any)[0].(map[string]any)["text"]
		if text != "选定内容" || params["expectedTurnId"] != "turn1" {
			t.Error(params)
		}
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	backend := Backend{URL: server.URL, Client: server.Client()}
	if _, e := backend.Invoke(context.Background(), "send_input", map[string]any{"id": "abc", "requestId": "stable-id", "mode": "steer", "turnId": "turn1", "text": "选定内容"}); e != nil {
		t.Fatal(e)
	}
}

func TestAnnotationsCarryStructuredSourceThroughMCP(t *testing.T) {
	target := map[string]any{"kind": "changes", "path": "src/示例.ts", "startLine": float64(12), "endLine": float64(12), "side": "old", "quote": "removed()", "contentHash": strings.Repeat("a", 64), "baseRevision": strings.Repeat("b", 40)}
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		if r.Method == "POST" {
			var input map[string]any
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			got, _ := json.Marshal(input["target"])
			want, _ := json.Marshal(target)
			if string(got) != string(want) || input["text"] != "检查被删的调用" {
				t.Error(input)
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"annotations": []any{map[string]any{"text": "检查被删的调用", "target": target}}})
	}))
	defer server.Close()
	backend := Backend{URL: server.URL, Client: server.Client()}
	if _, err := backend.Invoke(context.Background(), "add_annotation", map[string]any{"id": "collaboration", "text": "检查被删的调用", "target": target}); err != nil {
		t.Fatal(err)
	}
	result, err := backend.Invoke(context.Background(), "read_context", map[string]any{"id": "collaboration", "kind": "annotations"})
	if err != nil || !bytes.Contains(result, []byte(`"side":"old"`)) || !bytes.Contains(result, []byte(`"quote":"removed()"`)) {
		t.Fatal(string(result), err)
	}
	if len(calls) != 2 || calls[0] != "POST /api/collaborations/collaboration/annotations" || calls[1] != "GET /api/collaborations/collaboration/context?kind=annotations" {
		t.Fatal(calls)
	}
}
