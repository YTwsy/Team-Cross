package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"teamcross/internal/buildinfo"
	"teamcross/internal/service"
	"testing"
)

func TestUIHandshakeAndEntrypointsWithoutCore(t *testing.T) {
	input := `{"id":1,"method":"initialize"}
{"id":2,"method":"tools/list"}
{"id":3,"method":"resources/read","params":{"uri":"ui://teamcross/workspace-v1.html"}}
{"id":4,"method":"tools/call","params":{"name":"open_teamcross","arguments":{}}}
{"id":5,"method":"tools/call","params":{"name":"send_input","arguments":{}}}
{"id":6,"method":"tools/call","params":{"name":"teamcross_ui_write","arguments":{"path":"agent-tools/pair_current_session","body":{}}}}
`
	var out bytes.Buffer
	if err := ServeUI(context.Background(), t.TempDir(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(&out)
	for n := 1; n <= 6; n++ {
		var row struct{ Result map[string]json.RawMessage }
		if err := dec.Decode(&row); err != nil {
			t.Fatal(err)
		}
		switch n {
		case 1:
			if !bytes.Contains(row.Result["capabilities"], []byte(`"resources"`)) {
				t.Fatal(row.Result)
			}
		case 2:
			var tools []map[string]any
			json.Unmarshal(row.Result["tools"], &tools)
			for _, tool := range tools {
				name := tool["name"].(string)
				if name == "send_input" || name == "create_collaboration" || name == "open_client" {
					t.Fatal(name)
				}
				meta := tool["_meta"].(map[string]any)
				if name == "open_teamcross" && len(meta["openai/ui"].(map[string]any)["entrypoints"].([]any)) != 2 {
					t.Fatal(meta)
				}
				if strings.HasPrefix(name, "teamcross_") && string(mustJSON(meta["ui"].(map[string]any)["visibility"])) != `["app"]` {
					t.Fatal(meta)
				}
			}
			if len(tools) != 11 {
				t.Fatal(len(tools))
			}
		case 3:
			if !bytes.Contains(row.Result["contents"], []byte("text/html;profile=mcp-app")) || !bytes.Contains(row.Result["contents"], []byte("Team Cross")) {
				t.Fatal("missing embedded resource")
			}
		case 4:
			if string(row.Result["isError"]) != "false" {
				t.Fatal(row.Result)
			}
		case 5, 6:
			if string(row.Result["isError"]) != "true" {
				t.Fatal(row.Result)
			}
		}
	}
	// Calling UITools must not narrow the normal MCP's history contract.
	for _, tool := range Tools() {
		if tool["name"] == "read_context" && !bytes.Contains(mustJSON(tool["inputSchema"]), []byte("history")) {
			t.Fatal("normal tools mutated")
		}
	}
}
func mustJSON(value any) []byte { b, _ := json.Marshal(value); return b }

func TestUIClientRecheckKeepsReadOnlyRoute(t *testing.T) {
	if method, err := uiRoute("info?refreshClients=1", nil, false); err != nil || method != "GET" {
		t.Fatal(method, err)
	}
	for _, path := range []string{"info?binary=/bin/sh", "info?refreshClients=1&refreshClients=1", "info?refreshClients=1&token=secret"} {
		if _, err := uiRoute(path, nil, false); err == nil {
			t.Fatal(path)
		}
	}
	if _, err := uiRoute("info?refreshClients=1", map[string]any{"binary": "/bin/sh"}, false); err == nil {
		t.Fatal("recheck must not write settings")
	}
}

func TestPluginConnectionIsAppOnlyAndReadWriteSeparated(t *testing.T) {
	if _, err := uiRoute("plugin/connection", nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := uiRoute("plugin/connection", map[string]any{"action": "connect"}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := uiRoute("plugin/connection", map[string]any{"action": "connect"}, false); err == nil {
		t.Fatal("write routed as read")
	}
	for _, path := range []string{"plugin/connection?binary=/bin/sh", "plugin/sync", "plugin/export", "plugin/connection/loaded"} {
		if _, err := uiRoute(path, nil, false); err == nil {
			t.Fatal(path)
		}
	}
}

func TestUIWebGUIRoutesKeepNativeShapeAndSeparateWrites(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer local" {
			t.Error("missing local authentication")
		}
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		w.Write([]byte(`{"annotations":[{"text":"full web shape"}],"selection":["v1"]}`))
	}))
	defer server.Close()
	b := Backend{URL: server.URL, Token: "local", Client: localClient()}
	requests := []struct {
		path  string
		body  any
		write bool
	}{
		{"collaborations/a", nil, false},
		{"library", nil, false},
		{"sources?provider=codex&search=two%20words&cursor=next%2Fpage", nil, false},
		{"collaborations/a/context?kind=history&path=src%2Fmain.go", nil, false},
		{"collaborations/a/read-material", map[string]any{"version": 1}, false},
		{"collaborations/a/publication-status", map[string]any{"requestId": "fixed"}, false},
		{"library/read", map[string]any{"kind": "annotation"}, false},
		{"publications/read-draft", map[string]any{"draftId": "d"}, false},
		{"library/bundles", map[string]any{"references": []any{}}, true},
		{"agent-pairings", map[string]any{"name": "user target"}, true},
		{"agent-requests/request", nil, false},
		{"agent-pairings/pair/remove", map[string]any{}, true},
		{"ui-language", map[string]any{"mode": "en"}, true},
		{"collaborations/a/annotation-replies", map[string]any{"text": "explicit reply"}, true},
		{"collaborations/a/action", map[string]any{"action": "request_input"}, true},
		{"collaborations/a/workbench/view?offset=25", nil, false},
		{"collaborations/a/workbench/request?requestId=fixed", nil, false},
		{"space-receivers?spaceId=a", nil, false},
		{"space-receivers/receiver/events", nil, false},
		{"collaborations/a/workbench/register", map[string]any{"pairingId": "pair"}, true},
		{"collaborations/a/workbench/send", map[string]any{"requestId": "fixed", "targetId": "target"}, true},
		{"collaborations/a/workbench/brief", map[string]any{"revision": 1}, true},
		{"collaborations/a/workbench/assistant", map[string]any{"action": "pause", "epoch": 1}, true},
		{"collaborations/a/workbench/cancel", map[string]any{"requestId": "fixed"}, true},
		{"collaborations/a/workbench/remove", map[string]any{"targetId": "target"}, true},
		{"collaborations/a/workbench/create-receiver", map[string]any{"requestId": "create-fixed", "name": "Explicit receiver"}, true},
		{"space-receivers/receiver/action", map[string]any{"action": "start"}, true},
		{"space-receivers/receiver/respond", map[string]any{"id": "approval", "result": map[string]any{"decision": "decline"}}, true},
	}
	for _, request := range requests {
		out, err := b.uiRequest(context.Background(), request.path, request.body, request.write)
		if err != nil || !bytes.Contains(out, []byte("full web shape")) || !bytes.Contains(out, []byte(`"selection"`)) {
			t.Fatalf("%s: %s %v", request.path, out, err)
		}
	}
	if len(calls) != len(requests) || calls[3] != "GET /api/collaborations/a/context?kind=history&path=src%2Fmain.go" {
		t.Fatal(calls)
	}
	calls = nil
	for _, path := range []string{
		"https://example.com/", "//example.com/path", "collaborations/../materials", "collaborations/a%2fb/materials",
		"collaborations/a/history", "collaborations/a/materials#x", "collaborations/a/materials?compact=true",
		"collaborations/a/context?kind=history&kind=annotations", "collaborations/a/context?kind+path=history", "collaborations/a/context?=history", "collaborations/a/context?token=secret",
		"control/status", "control/stop", "agent-tools/pair_current_session", "agent-receivers/poll", "mcp/observed", "runtime-annotations/a", "collaborations/a/rpc", "future/route",
		"collaborations/a/workbench/poll", "collaborations/a/workbench/claim", "collaborations/a/workbench/receiver-check",
		"collaborations/a/workbench/view?offset=0&offset=25", "collaborations/a/workbench/view?token=secret",
		"collaborations/a/workbench/send?requestId=fixed", "space-receivers?spaceId=a&spaceId=b",
		"space-receivers/receiver/rpc", "space-receivers/receiver/events?after=0",
	} {
		for _, write := range []bool{false, true} {
			for _, body := range []any{nil, map[string]any{}} {
				if _, err := b.uiRequest(context.Background(), path, body, write); err == nil {
					t.Fatalf("allowed %s (write=%v body=%v)", path, write, body)
				}
			}
		}
	}
	if _, err := b.uiRequest(context.Background(), "library/bundles", map[string]any{}, false); err == nil {
		t.Fatal("write allowed through read tool")
	}
	if _, err := b.uiRequest(context.Background(), "collaborations", nil, true); err == nil {
		t.Fatal("GET allowed through write tool")
	}
	for _, request := range requests {
		if _, err := b.uiRequest(context.Background(), request.path, request.body, !request.write); err == nil {
			t.Fatalf("allowed %s through wrong read/write tool", request.path)
		}
	}
	if len(calls) != 0 {
		t.Fatal(calls)
	}
}

func TestUIReplyIsStructuredAndPreservesRequest(t *testing.T) {
	dir, _ := service.Normalize(t.TempDir())
	var connection service.Connection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/control/status" {
			json.NewEncoder(w).Encode(service.Status{Connection: connection, Running: true})
			return
		}
		if r.URL.Path != "/api/collaborations/a/annotation-replies" {
			t.Errorf("unexpected route %s", r.URL.Path)
		}
		var args map[string]any
		json.NewDecoder(r.Body).Decode(&args)
		if args["requestId"] != "stable" || args["text"] != "exact reply" || args["annotationId"] != "n" {
			t.Error(args)
		}
		w.Write([]byte(`{"status":"saved","reply":{"id":"receipt","requestId":"stable"}}`))
	}))
	defer server.Close()
	connection = service.Connection{URL: server.URL, PID: os.Getpid(), Instance: "test", Token: "local", Version: buildinfo.Version, Protocol: buildinfo.ControlProtocol, DataDir: dir}
	if err := service.Save(dir, connection); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	input := `{"id":1,"method":"tools/call","params":{"name":"reply_to_annotation","arguments":{"id":"a","annotationId":"n","text":"exact reply","requestId":"stable"}}}` + "\n"
	if err := ServeUI(context.Background(), dir, strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"structuredContent":{"reply":{"id":"receipt","requestId":"stable"},"status":"saved"}`) {
		t.Fatal(out.String())
	}
}
