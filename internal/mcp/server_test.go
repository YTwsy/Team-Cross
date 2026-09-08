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
	"testing"
)

func TestStdioDoesNotSendInputWhileListing(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer server.Close()
	dir := t.TempDir()
	b, _ := json.Marshal(map[string]string{"url": server.URL})
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
