package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"teamcross/internal/buildinfo"
	"teamcross/internal/mcpassets"
	"teamcross/internal/problem"
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
			if len(tools) != 11+len(AgentTools()) {
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

func TestUIPanelResourceChangesCacheKeyWithContent(t *testing.T) {
	if panelResourceURI("first") == panelResourceURI("second") || panelResourceURI("first") != panelResourceURI("first") {
		t.Fatal("panel resource does not identify its content")
	}
	for _, uri := range []string{UIResourceURI, legacyUIResourceURI} {
		if _, err := uiResource("resources/read", mustJSON(map[string]any{"uri": uri})); err != nil {
			t.Fatal(err)
		}
	}
}

// A sidebar can retain newly discovered metadata while dispatching its read to
// an older thread's MCP process (or retain old metadata after a process restart).
func TestUIPanelResourceSurvivesMixedHostAndMCPVersions(t *testing.T) {
	dir := t.TempDir()
	uris := []string{
		UIResourceURI, legacyUIResourceURI,
		"ui://teamcross/workspace-07d625b5e6b6640ee4bb7e44.html",
		panelResourceURI("a later panel discovered by the host"),
	}
	var input, output bytes.Buffer
	for i, uri := range uris {
		if err := json.NewEncoder(&input).Encode(map[string]any{"id": i + 1, "method": "resources/read", "params": map[string]any{"uri": uri}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := ServeUI(context.Background(), dir, &input, &output); err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(&output)
	for _, uri := range uris {
		var row struct {
			Error  json.RawMessage
			Result struct {
				Contents []struct {
					URI, MimeType, Text string
					Meta                map[string]any `json:"_meta"`
				}
			}
		}
		if err := dec.Decode(&row); err != nil {
			t.Fatal(err)
		}
		if len(row.Error) != 0 || len(row.Result.Contents) != 1 {
			t.Fatalf("cached panel %s failed: %s", uri, row.Error)
		}
		resource := row.Result.Contents[0]
		if resource.URI != uri || resource.MimeType != "text/html;profile=mcp-app" || resource.Text != mcpassets.Panel {
			t.Fatalf("cached panel %s did not return the local shell", uri)
		}
		if resource.Meta["ui"] == nil || resource.Meta["openai/ui"] == nil {
			t.Fatal("missing sandbox metadata")
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("static panel read touched Core data: %v %v", entries, err)
	}
}

func TestUIPanelAliasesRejectOtherResources(t *testing.T) {
	valid := panelResourceURI("cached")
	for _, uri := range []string{
		"", "ui://teamcross/workspace.html", "ui://teamcross/workspace-v2.html",
		strings.Replace(valid, "teamcross", "another-app", 1),
		strings.Replace(valid, "workspace-", "workspace-../", 1),
		strings.Replace(valid, "workspace-", "workspace-%2e%2e", 1),
		strings.Replace(valid, "ui://", "file://", 1),
		strings.Replace(valid, "ui://", "https://", 1),
		strings.Replace(valid, "ui://", "teamcross://", 1),
		strings.Replace(valid, "workspace-", "workspace-0", 1),
		"ui://teamcross/workspace-" + strings.Repeat("g", 24) + ".html",
		"ui://teamcross/workspace-" + strings.Repeat("A", 24) + ".html",
		valid + "?", valid + "#", valid + "/", valid + "?path=/private",
		legacyUIResourceURI + "?", legacyUIResourceURI + "#",
	} {
		t.Run(uri, func(t *testing.T) {
			params := mustJSON(map[string]any{"uri": uri})
			if _, err := uiResource("resources/read", params); err == nil {
				t.Fatal("invalid panel alias accepted")
			}
			if _, err := uiResources(t.TempDir())(context.Background(), "resources/read", params); err == nil {
				t.Fatal("invalid resource escaped the UI/mention router")
			}
		})
	}
}

func TestUIPanelUpdatesUseCoreAndStrictReadRoutes(t *testing.T) {
	for _, path := range []string{"plugin/panel-version", "plugin/panel-bundle?revision=abc"} {
		if _, err := uiRoute(path, nil, false); err != nil {
			t.Fatal(err)
		}
		if _, err := uiRoute(path, map[string]any{}, true); err == nil {
			t.Fatal("update endpoint accepts write")
		}
	}
	for _, path := range []string{"plugin/panel-version?url=https://example.invalid", "plugin/panel-bundle?revision=a&revision=b", "plugin/panel-bundle?revision=a&url=file:x"} {
		if _, err := uiRoute(path, nil, false); err == nil {
			t.Fatal("unsafe route", path)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/plugin/panel-version" || r.Header.Get("Authorization") != "Bearer local" {
			t.Error(r.URL, r.Header)
		}
		w.Write([]byte(`{"protocol":1,"revision":"new-core-revision"}`))
	}))
	defer server.Close()
	b := Backend{URL: server.URL, Token: "local", Client: localClient()}
	out, err := b.uiRequest(context.Background(), "plugin/panel-version", nil, false)
	if err != nil || !bytes.Contains(out, []byte("new-core-revision")) {
		t.Fatal(string(out), err)
	}
}

func TestUIPanelExplainsPendingCoreUpgradeWithoutBlockingOtherReads(t *testing.T) {
	for _, path := range []string{"plugin/current-connection?spaceId=space", "plugin/panel-version", "plugin/panel-bundle?revision=abc"} {
		err := checkPanelCoreVersion(service.Status{UpdatePending: true}, path)
		if err == nil || problem.Describe(err).Code != "core_update_pending" {
			t.Fatal("missing upgrade explanation", path, err)
		}
		if err := checkPanelCoreVersion(service.Status{}, path); err != nil {
			t.Fatal("current Core rejected", err)
		}
	}
	if err := checkPanelCoreVersion(service.Status{UpdatePending: true}, "collaborations"); err != nil {
		t.Fatal("pending update blocks existing reads", err)
	}
}

func TestUIPanelBundleIsNotDuplicatedAsText(t *testing.T) {
	input := strings.NewReader(`{"id":1,"method":"tools/call","params":{"name":"teamcross_ui_read","arguments":{"path":"plugin/panel-bundle?revision=fixture"}}}`)
	var output bytes.Buffer
	err := serveWithResources(context.Background(), input, &output, UITools(), "", func(context.Context, string, map[string]any, string) (json.RawMessage, error) {
		return json.RawMessage(`{"protocol":1,"revision":"fixture","script":"local bundle","style":"body{}"}`), nil
	}, func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		return uiResource(method, params)
	})
	if err != nil {
		t.Fatal(err)
	}
	var row struct {
		Result struct {
			Content    []any          `json:"content"`
			Structured map[string]any `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(output.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	if len(row.Result.Content) != 0 || row.Result.Structured["script"] != "local bundle" {
		t.Fatal(output.String())
	}
}

func TestUICurrentConnectionUsesOnlyTransportIdentityWithoutModelTurn(t *testing.T) {
	id := uuid.NewString()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/api/agent-current-connection" || r.Header.Get("Authorization") != "Bearer local" {
			t.Error(r.Method, r.URL.Path)
		}
		var in struct {
			Caller  CallerSource `json:"caller"`
			SpaceID string       `json:"spaceId"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		if in.SpaceID != "space" || in.Caller.SourceID != id || in.Caller.TurnID != "" {
			t.Error(in)
		}
		w.Write([]byte(`{"known":true,"linked":true,"receiving":false,"name":"verified"}`))
	}))
	defer server.Close()
	b := Backend{URL: server.URL, Token: "local", Client: localClient()}
	ctx := callContext(context.Background(), "codex", map[string]json.RawMessage{"threadId": mustJSON(id)})
	out, err := b.uiRequest(ctx, "plugin/current-connection?spaceId=space", nil, false)
	if err != nil || !bytes.Contains(out, []byte(`"linked":true`)) || calls != 1 {
		t.Fatal(string(out), err, calls)
	}
	if _, err := callerSource(ctx); err == nil {
		t.Fatal("status identity weakened model/source verification")
	}
	unknown, err := b.uiRequest(context.Background(), "plugin/current-connection?spaceId=space", nil, false)
	if err != nil || !bytes.Contains(unknown, []byte(`"known":false`)) || calls != 1 {
		t.Fatal(string(unknown), err, calls)
	}
	for _, path := range []string{"plugin/current-connection", "plugin/current-connection?spaceId=space&sourceId=" + id, "plugin/current-connection?spaceId=a&spaceId=b"} {
		if _, err := b.uiRequest(ctx, path, nil, false); err == nil {
			t.Fatal("accepted forged/ambiguous status query", path)
		}
	}
	if _, err := b.uiRequest(ctx, "plugin/current-connection?spaceId=space", map[string]any{"caller": id}, false); err == nil {
		t.Fatal("accepted supplied identity")
	}
}

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
		{"collaborations/a/shadows/view?shadowId=b&offset=100", nil, false},
		{"collaborations/a/shadows/models?cwd=%2Ffixture", nil, false},
		{"shadows/b/local", nil, false},
		{"collaborations/a/shadows/create", map[string]any{"requestId": "fixed"}, true},
		{"collaborations/a/shadows/send", map[string]any{"shadowId": "b"}, true},
		{"shadows/b/action", map[string]any{"action": "pause"}, true},
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
		"collaborations/a/shadows/sync", "collaborations/a/shadows/register", "collaborations/a/shadows/check",
		"collaborations/a/shadows/view?shadowId=b&shadowId=c", "shadows/b/rpc", "shadows/b/local?token=secret", "collaborations/a/shadows/models?cwd=%2Ffixture&token=secret",
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
