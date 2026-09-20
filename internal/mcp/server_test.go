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
func TestStdioReportsActualClientOnlyOnToolCall(t *testing.T) {
	for _, row := range []struct{ name, provider string }{{"claude-code", "claude"}, {"codex-mcp-client", "codex"}, {"teamcross-probe", ""}, {"", ""}} {
		t.Run(row.name, func(t *testing.T) {
			dir, _ := service.Normalize(t.TempDir())
			var connection service.Connection
			observed := []string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("missing local authentication")
				}
				if r.URL.Path == "/api/control/status" {
					json.NewEncoder(w).Encode(service.Status{Connection: connection, Running: true})
					return
				}
				if r.URL.Path == "/api/mcp/observed" {
					var v struct {
						Provider string `json:"provider"`
					}
					json.NewDecoder(r.Body).Decode(&v)
					observed = append(observed, v.Provider)
				}
				w.Write([]byte(`[]`))
			}))
			defer server.Close()
			connection = service.Connection{URL: server.URL, PID: os.Getpid(), Instance: "test", Token: "test-token", Version: buildinfo.Version, Protocol: buildinfo.ControlProtocol, DataDir: dir}
			b, _ := json.Marshal(connection)
			os.WriteFile(filepath.Join(dir, "connection.json"), b, 0600)
			init := `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"` + row.name + `"}}}` + "\n" + `{"id":2,"method":"tools/list"}` + "\n"
			var output bytes.Buffer
			if err := Serve(context.Background(), dir, strings.NewReader(init), &output); err != nil {
				t.Fatal(err)
			}
			if len(observed) != 0 {
				t.Fatal("protocol probe reported an actual client")
			}
			call := `{"id":3,"method":"tools/call","params":{"name":"list_collaborations","arguments":{}}}` + "\n"
			if err := Serve(context.Background(), dir, strings.NewReader(init+call), &output); err != nil {
				t.Fatal(err)
			}
			if row.provider == "" {
				if len(observed) != 0 {
					t.Fatal(observed)
				}
			} else if len(observed) != 1 || observed[0] != row.provider {
				t.Fatal(observed)
			}
		})
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

func TestPublicationDraftReaderKeepsLargeBodiesOutOfDefaultToolResults(t *testing.T) {
	type call struct {
		Path string
		Body map[string]any
	}
	calls := []call{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		calls = append(calls, call{Path: r.URL.Path, Body: body})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"draft","turns":[]}`))
	}))
	defer server.Close()
	backend := Backend{URL: server.URL, Client: server.Client()}
	if _, err := backend.Invoke(context.Background(), "freeze_source_session", map[string]any{"provider": "codex", "sourceId": "source"}); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Invoke(context.Background(), "read_publication_draft", map[string]any{"draftId": "draft"}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0].Path != "/api/publications/source" || calls[0].Body["compact"] != true {
		t.Fatal(calls)
	}
	if calls[1].Path != "/api/publications/read-draft" || calls[1].Body["includeOutline"] != true || calls[1].Body["draftId"] != "draft" {
		t.Fatal(calls[1])
	}
}

func TestReadContextForwardsHistoryItemCoordinates(t *testing.T) {
	var query map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = map[string]string{}
		for key := range r.URL.Query() {
			query[key] = r.URL.Query().Get(key)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"thread":{},"segments":[]}`))
	}))
	defer server.Close()
	backend := Backend{URL: server.URL, Client: server.Client()}
	_, err := backend.Invoke(context.Background(), "read_context", map[string]any{
		"id": "collaboration", "kind": "history", "cursor": "page-cursor",
		"turnId": "turn", "itemId": "tool", "startOffset": 16000,
	})
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"kind": "history", "cursor": "page-cursor", "turnId": "turn", "itemId": "tool", "startOffset": "16000"} {
		if query[key] != want {
			t.Fatalf("%s = %q, want %q", key, query[key], want)
		}
	}
}
