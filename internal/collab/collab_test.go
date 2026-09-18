package collab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/coder/websocket"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"teamcross/internal/nativecodex"
	"teamcross/internal/sharing"
	"teamcross/internal/workspace"
	"testing"
	"time"
)

type fakeRuntime struct {
	mu               sync.Mutex
	source           Source
	calls            []string
	forks            int
	handler          func(nativecodex.Message)
	last             map[string]any
	fail             bool
	alive            bool
	threads          map[string]Source
	requests         map[string]map[string]any
	sourceTurnID     string
	sourceTurnStatus string
}

func (f *fakeRuntime) Call(_ context.Context, method string, in, out any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method)
	params, _ := in.(map[string]any)
	f.last = params
	if f.requests == nil {
		f.requests = map[string]map[string]any{}
	}
	encoded, _ := json.Marshal(params)
	var request map[string]any
	_ = json.Unmarshal(encoded, &request)
	f.requests[method] = request
	if f.threads == nil {
		f.threads = map[string]Source{}
	}
	id, _ := params["threadId"].(string)
	thread, exists := f.threads[id]
	if !exists {
		thread = f.source
	}
	if method == "turn/start" || method == "thread/resume" || method == "thread/settings/update" || method == "thread/fork" {
		if params["model"] == "rejected-fixture-model" {
			return fmt.Errorf(`{"code":-32602,"message":"unsupported fixture model"}`)
		}
		if model, ok := params["model"].(string); ok && model != "" {
			thread.Model = model
		}
		if effort, ok := params["effort"].(string); ok {
			thread.ReasoningEffort = &effort
		}
		if config, ok := params["config"].(map[string]any); ok {
			if effort, ok := config["model_reasoning_effort"].(string); ok {
				thread.ReasoningEffort = &effort
			}
		}
		f.threads[id] = thread
	}
	var result any = map[string]any{}
	switch method {
	case "thread/list":
		result = map[string]any{"data": []Source{f.source}, "nextCursor": nil}
	case "getAuthStatus":
		result = map[string]any{"authToken": f.source.Name + "-token", "authMethod": "chatgpt"}
	case "config/read":
		result = map[string]any{"config": map[string]any{"model": "fixture-config-model", "mcp_servers": map[string]any{"private": "secret"}, "model_providers": map[string]any{"secret": "key"}}, "origins": map[string]string{"secret": "path"}}
	case "hooks/list", "skills/list":
		if cwds, ok := request["cwds"].([]any); ok && len(cwds) == 1 {
			result = map[string]any{"data": []any{map[string]any{"cwd": cwds[0], "hooks": []any{}, "skills": []any{}, "errors": []any{}}}}
		}
	case "thread/read":
		result = map[string]any{"thread": thread}
	case "thread/turns/list":
		turn, status := f.sourceTurnID, f.sourceTurnStatus
		if turn == "" {
			turn = "turn-fixture"
		}
		if status == "" {
			status = "completed"
		}
		result = map[string]any{"data": []any{map[string]any{"id": turn, "status": status}}}
	case "thread/fork":
		f.forks++
		thread.ID = uuid.NewString()
		thread.Cwd, _ = params["cwd"].(string)
		f.threads[thread.ID] = thread
		result = map[string]any{"model": thread.Model, "modelProvider": thread.ModelProvider, "reasoningEffort": thread.ReasoningEffort, "thread": map[string]any{"id": thread.ID, "forkedFromId": f.source.ID, "cwd": thread.Cwd}}
	case "turn/start":
		result = map[string]any{"turn": map[string]any{"id": "active-turn"}}
	case "thread/resume":
		result = map[string]any{"thread": thread, "model": thread.Model, "reasoningEffort": thread.ReasoningEffort}
	}
	if out != nil {
		b, _ := json.Marshal(result)
		return json.Unmarshal(b, out)
	}
	return nil
}
func (f *fakeRuntime) Alive() bool                                       { f.mu.Lock(); defer f.mu.Unlock(); return f.alive }
func (f *fakeRuntime) Close()                                            { f.mu.Lock(); f.alive = false; f.mu.Unlock() }
func (f *fakeRuntime) Reply(context.Context, json.RawMessage, any) error { return nil }
func (f *fakeRuntime) Initialization() json.RawMessage {
	return json.RawMessage(`{"userAgent":"fake"}`)
}
func (f *fakeRuntime) SetHandler(h func(nativecodex.Message)) {
	f.mu.Lock()
	f.handler = h
	f.mu.Unlock()
}
func fixture(t *testing.T) (*App, *fakeRuntime, string) {
	t.Helper()
	repo := t.TempDir()
	ctx := context.Background()
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Fixture"}, {"config", "user.email", "fixture@example.invalid"}} {
		if _, e := workspace.Git(ctx, repo, args...); e != nil {
			t.Fatal(e)
		}
	}
	os.WriteFile(filepath.Join(repo, "file.txt"), []byte("baseline"), 0600)
	workspace.Git(ctx, repo, "add", "file.txt")
	workspace.Git(ctx, repo, "commit", "-m", "fixture")
	effort := "high"
	f := &fakeRuntime{source: Source{ID: uuid.NewString(), Cwd: repo, Name: "fixture", Model: "fixture-source-model", ModelProvider: "fixture-provider", ReasoningEffort: &effort}, alive: true}
	a, e := Open(Config{DataDir: t.TempDir(), Repo: repo, Binary: "/usr/bin/true", Loopback: true, StartProcess: func(string, string, string, string) (Runtime, error) {
		f.mu.Lock()
		f.alive = true
		f.mu.Unlock()
		return f, nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(a.Close)
	return a, f, repo
}
func createFixture(t *testing.T, a *App, f *fakeRuntime, mode string) *Session {
	t.Helper()
	in := CreateInput{SourceID: f.source.ID, WorkspaceMode: mode, RequestID: uuid.NewString()}
	p, e := a.Preview(context.Background(), in)
	if e != nil {
		t.Fatal(e)
	}
	in.PreviewHash = p.Hash
	s, e := a.Create(context.Background(), in)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestCreateAndRecoverWithoutPrompt(t *testing.T) {
	for _, mode := range []string{"existing", "worktree"} {
		t.Run(mode, func(t *testing.T) {
			a, f, repo := fixture(t)
			os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("local"), 0600)
			in := CreateInput{SourceID: f.source.ID, WorkspaceMode: mode, RequestID: uuid.NewString()}
			p, e := a.Preview(context.Background(), in)
			if e != nil {
				t.Fatal(e)
			}
			in.PreviewHash = p.Hash
			s, e := a.Create(context.Background(), in)
			if e != nil {
				t.Fatal(e)
			}
			again, e := a.Create(context.Background(), in)
			if e != nil || again != s || f.forks != 1 {
				t.Fatal("creation not idempotent", e)
			}
			if s.record.SessionID == in.SourceID || s.record.SourceTurnID != "turn-fixture" {
				t.Fatal("fork identity missing")
			}
			for _, method := range f.calls {
				if strings.HasPrefix(method, "turn/") {
					t.Fatal("creation sent prompt", method)
				}
			}
			a.Close()
			b, e := Open(a.Config)
			if e != nil {
				t.Fatal(e)
			}
			defer b.Close()
			restored := b.sessions[s.record.ID]
			if restored == nil || restored.record.SessionID != s.record.SessionID {
				t.Fatal("session lost on restart")
			}
			if e = restored.Action(context.Background(), "start"); e != nil {
				t.Fatal(e)
			}
			if f.forks != 1 {
				t.Fatal("resume created another fork")
			}
		})
	}
}
func TestInputOwnershipDedupAndApproval(t *testing.T) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	ctx := context.Background()
	params := map[string]any{"input": []any{}, "cwd": "/wrong", "model": "fixture-client-model", "effort": "medium"}
	if _, e := s.RPC(ctx, firstRemote(s), "turn/start", params, "blocked"); e == nil {
		t.Fatal("remote writes without share")
	}
	one, e := s.RPC(ctx, "owner", "turn/start", params, "request1")
	if e != nil {
		t.Fatal(e)
	}
	two, e := s.RPC(ctx, "owner", "turn/start", params, "request1")
	if e != nil || !bytes.Equal(one, two) {
		t.Fatal("duplicate result not replayed", e)
	}
	f.mu.Lock()
	if f.requests["turn/start"]["cwd"] != s.record.ExecutionCwd || f.requests["turn/start"]["model"] != "fixture-client-model" || f.requests["turn/start"]["effort"] != "medium" {
		t.Fatal("runtime binding overridden")
	}
	f.mu.Unlock()
	if _, e = s.RPC(ctx, "owner", "turn/start", map[string]any{}, "request2"); e == nil {
		t.Fatal("parallel start accepted")
	}
	s.onMessage(nativecodex.Message{Method: "turn/completed"})
	if e = s.Action(ctx, "share"); e != nil {
		t.Fatal(e)
	}
	joinFixture(t, s)
	if e = s.Action(ctx, "handoff"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.RPC(ctx, "owner", "turn/start", params, "request3"); e == nil {
		t.Fatal("old writer accepted")
	}
	id := json.RawMessage(`12`)
	s.onMessage(nativecodex.Message{ID: id, Method: "item/commandExecution/requestApproval", Params: json.RawMessage(`{}`)})
	if e = s.Respond(ctx, "owner", id, map[string]any{}); e == nil {
		t.Fatal("nonwriter approval")
	}
	if e = s.Respond(ctx, firstRemote(s), id, map[string]any{"decision": "decline"}); e != nil {
		t.Fatal(e)
	}
	if e = s.Respond(ctx, firstRemote(s), id, map[string]any{}); e == nil {
		t.Fatal("approval replied twice")
	}
	if e = s.Action(ctx, "end"); e != nil {
		t.Fatal(e)
	}
	if s.record.State != "ready" || s.record.SessionID == "" {
		t.Fatal("end destroyed session")
	}
	if _, e = s.RPC(ctx, firstRemote(s), "thread/read", nil, ""); e == nil {
		t.Fatal("ended share still readable")
	}
}
func TestNativeAndToolShareOneWriter(t *testing.T) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	endpoint, e := a.endpoint(s.record.ID)
	if e != nil {
		t.Fatal(e)
	}
	conn, _, e := websocket.Dial(ctx, endpoint, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.CloseNow()
	if second, res, e := websocket.Dial(ctx, endpoint, nil); e == nil {
		second.CloseNow()
		t.Fatal("second direct client accepted")
	} else if res.StatusCode != 409 {
		t.Fatal(res.Status)
	}
	conn.Write(ctx, websocket.MessageText, []byte(`{"id":1,"method":"initialize","params":{"clientInfo":{"name":"test"}}}`))
	if _, _, e = conn.Read(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Context(ctx, "events", "", 0); e != nil {
		t.Fatal("concurrent tool read failed", e)
	}
	s.mu.Lock()
	s.busy = false
	s.mu.Unlock()
	if e = s.Action(ctx, "share"); e != nil {
		t.Fatal(e)
	}
	joinFixture(t, s)
	if e = s.Action(ctx, "handoff"); e != nil {
		t.Fatal(e)
	}
	if _, _, e = conn.Read(ctx); e == nil {
		t.Fatal("old direct survived handoff")
	}
}
func TestHostOriginAndScopedAccess(t *testing.T) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	if _, e := s.RPC(context.Background(), "owner", "thread/read", map[string]any{"threadId": uuid.NewString()}, ""); e == nil {
		t.Fatal("wrong thread allowed")
	}
	handler := a.Handler(http.NotFoundHandler())
	for _, test := range []struct {
		host, origin string
		want         int
	}{{"evil.example", "", 403}, {"localhost:43210", "https://evil.example", 403}, {"localhost:43210", "http://localhost:43210", 200}} {
		req := httptest.NewRequest("GET", "http://"+test.host+"/api/collaborations", nil)
		req.Header.Set("Origin", test.origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != test.want {
			t.Fatal(w.Code, test)
		}
	}
}

func TestListReturnsCachedJoinedStatusWhileRefreshing(t *testing.T) {
	refreshStarted := make(chan struct{}, 1)
	releaseRefresh := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/status" {
			select {
			case refreshStarted <- struct{}{}:
			default:
			}
			select {
			case <-releaseRefresh:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"title":        "远端新状态",
					"createdAt":    "2026-09-10T10:00:00Z",
					"updatedAt":    "2026-09-10T11:00:00Z",
					"state":        "ready",
					"online":       true,
					"runtimeState": "running",
				})
			case <-r.Context().Done():
			}
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	t.Cleanup(server.Close)
	a, _, _ := fixture(t)
	j := &Joined{
		app:        a,
		ID:         "joined-cached",
		Invitation: sharing.Invitation{ID: "remote-cached", Title: "邀请标题", Host: "A 的 Mac"},
		Credential: "credential",
		URL:        server.URL,
		Client:     server.Client(),
		Last: map[string]any{
			"title":     "本机快照",
			"createdAt": "2026-09-10T10:00:00Z",
			"updatedAt": "2026-09-10T10:30:00Z",
			"state":     "ready",
			"online":    true,
		},
		confirmed: true,
		done:      make(chan struct{}),
	}
	a.mu.Lock()
	a.joined[j.ID] = j
	a.mu.Unlock()

	listed := make(chan []map[string]any, 1)
	go func() { listed <- a.List(context.Background()) }()
	var list []map[string]any
	select {
	case list = <-listed:
	case <-time.After(500 * time.Millisecond):
		close(releaseRefresh)
		t.Fatal("collaboration list waited for the remote status request")
	}
	if len(list) != 1 || list[0]["title"] != "本机快照" || list[0]["online"] != false {
		close(releaseRefresh)
		t.Fatal("list did not return the offline local snapshot", list)
	}
	select {
	case <-refreshStarted:
	case <-time.After(time.Second):
		close(releaseRefresh)
		t.Fatal("remote status was not refreshed in the background")
	}
	close(releaseRefresh)
	eventually(t, func() bool {
		view := j.cachedView()
		return view["title"] == "远端新状态" && view["online"] == true
	})
}

func TestParticipantLoginStaysLocal(t *testing.T) {
	a, af, _ := fixture(t)
	af.source.Name = "host-a"
	s := createFixture(t, a, af, "existing")
	b, bf, _ := fixture(t)
	bf.source.Name = "participant-b"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e := s.Action(ctx, "share"); e != nil {
		t.Fatal(e)
	}
	j, e := b.Join(ctx, s.share.Token())
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Action(ctx, "handoff"); e != nil {
		t.Fatal(e)
	}
	endpoint, e := b.endpoint(j.ID)
	if e != nil {
		t.Fatal(e)
	}
	conn, _, e := websocket.Dial(ctx, endpoint, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.CloseNow()
	request := func(id int, method string, params any) nativecodex.Message {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
		if e := conn.Write(ctx, websocket.MessageText, payload); e != nil {
			t.Fatal(e)
		}
		_, payload, e := conn.Read(ctx)
		if e != nil {
			t.Fatal(e)
		}
		var m nativecodex.Message
		if e = json.Unmarshal(payload, &m); e != nil {
			t.Fatal(e)
		}
		if len(m.Error) > 0 {
			t.Fatalf("%s: %s", method, m.Error)
		}
		return m
	}
	request(1, "initialize", map[string]any{"clientInfo": map[string]string{"name": "desktop-test"}})
	auth := request(2, "getAuthStatus", map[string]bool{"includeToken": true})
	if !bytes.Contains(auth.Result, []byte("participant-b-token")) || bytes.Contains(auth.Result, []byte("host-a-token")) {
		t.Fatalf("wrong local auth: %s", auth.Result)
	}
	request(3, "account/login/start", map[string]string{"type": "apiKey", "apiKey": "local-fixture"})
	bf.mu.Lock()
	handler := bf.handler
	bf.mu.Unlock()
	handler(nativecodex.Message{Method: "account/login/completed", Params: json.RawMessage(`{"success":true}`)})
	_, notification, e := conn.Read(ctx)
	if e != nil || !bytes.Contains(notification, []byte("account/login/completed")) {
		t.Fatal("login completion missing", e)
	}
	af.mu.Lock()
	for _, method := range af.calls {
		if method == "account/login/start" || method == "getAuthStatus" {
			t.Errorf("local auth reached A: %s", method)
		}
	}
	af.mu.Unlock()
	raw, e := s.RPC(ctx, firstRemote(s), "getAuthStatus", map[string]any{"includeToken": true}, "")
	if e != nil || bytes.Contains(raw, []byte("token")) {
		t.Fatal("share exported host token", e)
	}
	config, e := s.RPC(ctx, firstRemote(s), "config/read", nil, "")
	if e != nil || bytes.Contains(config, []byte("secret")) {
		t.Fatal("share exported private config", e)
	}
	if e = s.Action(ctx, "end"); e != nil {
		t.Fatal(e)
	}
	view := j.view(ctx)
	if view["state"] != "ended" {
		t.Fatal("end not propagated", view["state"], view["error"])
	}
	if _, _, e = conn.Read(ctx); e == nil {
		t.Fatal("native access survived end")
	}
	b.Close()
	b.Close()
	restored, e := Open(b.Config)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	if restored.joined[j.ID] == nil || restored.joined[j.ID].view(ctx)["state"] != "ended" {
		t.Fatal("closed share became reconnectable after restart")
	}
}
