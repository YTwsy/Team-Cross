package nativecodex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Full opt-in acceptance for an already opened personal Codex conversation.
// Auth is linked into a temporary home; no personal config/session is changed.
func TestExistingConversationMCPRoundTrip(t *testing.T) {
	binary, auth := os.Getenv("TEAMCROSS_TEST_BINARY"), os.Getenv("TEAMCROSS_TEST_CODEX_AUTH")
	if binary == "" || auth == "" {
		t.Skip("explicit real Luna acceptance only")
	}
	codex, err := Binary()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	home, repo, data := filepath.Join(root, "home"), filepath.Join(root, "repo"), filepath.Join(root, "core")
	for _, path := range []string{home, repo} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(auth, filepath.Join(home, "auth.json")); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf("model=\"gpt-5.6-luna\"\nmodel_reasoning_effort=\"low\"\napproval_policy=\"on-request\"\nsandbox_mode=\"read-only\"\n[features]\nplugins=false\napps=false\nmemories=false\nmulti_agent=false\n[mcp_servers.teamcross]\ncommand=%q\nargs=[\"mcp\",\"--data-dir\",%q]\n", binary, data)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	start := func(label string, args ...string) *exec.Cmd {
		log, err := os.Create(filepath.Join(root, label+".log"))
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = repo, environment(home), log, log
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		t.Cleanup(func() {
			_ = cmd.Process.Signal(os.Interrupt)
			select {
			case <-done:
			case <-time.After(20 * time.Second):
				_ = cmd.Process.Kill()
				<-done
			}
			_ = log.Close()
		})
		return cmd
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	start("core", binary, "serve", "--foreground", "--no-open", "--listen", "127.0.0.1:0", "--data-dir", data, "--repo", repo, "--codex-bin", codex, "--claude-bin", "/usr/bin/false")
	var conn struct {
		URL string `json:"url"`
	}
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
		raw, _ := os.ReadFile(filepath.Join(data, "connection.json"))
		if json.Unmarshal(raw, &conn) == nil && conn.URL != "" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if conn.URL == "" {
		t.Fatal("Core did not start")
	}
	client := &http.Client{Timeout: 15 * time.Second}
	api := func(path string, body any) map[string]any {
		method := "GET"
		var payload io.Reader
		if body != nil {
			method = "POST"
			raw, _ := json.Marshal(body)
			payload = bytes.NewReader(raw)
		}
		req, _ := http.NewRequestWithContext(ctx, method, conn.URL+"/api/"+path, payload)
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		if err = json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatal(path, err)
		}
		if res.StatusCode != 200 {
			t.Fatal(path, out)
		}
		return out
	}
	space := api("spaces", map[string]any{"title": "Existing conversation acceptance", "requestId": uuid.NewString()})
	sid := space["id"].(string)
	base := "collaborations/" + sid + "/workbench/"
	api(base+"brief", map[string]any{"baseRevision": 0, "brief": map[string]any{"topic": "Dedicated acceptance", "decisions": []any{map[string]any{"text": "TCX_BRIEF_CONFIRMED", "sources": []any{}}}, "questions": []any{}}})
	start("daemon", codex, "app-server", "--listen", "unix://")
	var owner *Process
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
		owner, err = connectProxy(ctx, codex, home, filepath.Join(root, "owner.log"))
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	var created struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
		Model string `json:"model"`
	}
	if err = owner.Call(ctx, "thread/start", map[string]any{"cwd": repo, "model": "gpt-5.6-luna"}, &created); err != nil {
		t.Fatal(err)
	}
	if created.Model != "gpt-5.6-luna" {
		t.Fatal("unexpected model", created.Model)
	}
	thread := created.Thread.ID
	owner.SetHandler(func(message Message) {
		if len(message.ID) == 0 {
			return
		}
		var p struct {
			ThreadID string `json:"threadId"`
			Server   string `json:"serverName"`
			Message  string `json:"message"`
		}
		_ = json.Unmarshal(message.Params, &p)
		allowed := false
		for _, tool := range []string{"connect_current_session", "read_agent_request", "finish_agent_request"} {
			allowed = allowed || strings.Contains(p.Message, "\""+tool+"\"")
		}
		if message.Method == "mcpServer/elicitation/request" && p.ThreadID == thread && p.Server == "teamcross" && allowed {
			_ = owner.Reply(ctx, message.ID, map[string]any{"action": "accept", "content": map[string]any{}})
		} else {
			_ = owner.Reply(ctx, message.ID, map[string]any{"action": "decline", "decision": "decline"})
		}
	})
	var turn any
	prompt := "Connect this current conversation to Team Cross space " + sid + " using connect_current_session with name 'Existing Luna'. Report linked and receiving. Do not create or resume another session, send a request, or use other tools."
	if err = owner.Call(ctx, "turn/start", map[string]any{"threadId": thread, "input": []any{map[string]any{"type": "text", "text": prompt}}}, &turn); err != nil {
		t.Fatal(err)
	}
	var target string
	for ctx.Err() == nil {
		view := api(base+"view", nil)
		for _, raw := range view["targets"].([]any) {
			v := raw.(map[string]any)
			if v["name"] == "Existing Luna" && v["available"] == true {
				target = v["id"].(string)
			}
		}
		if target != "" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if target == "" {
		t.Fatal("existing conversation did not connect")
	}
	requestID := uuid.NewString()
	api(base+"send", map[string]any{"requestId": requestID, "targetId": target, "instruction": "Read this request and its immutable brief. If the confirmed decision is TCX_BRIEF_CONFIRMED, finish_agent_request with status completed and summary TCX_EXISTING_DONE. Do not send any other request or edit files.", "intent": "analyze", "references": []any{}})
	var result map[string]any
	for ctx.Err() == nil {
		result = api(base+"request?requestId="+requestID, nil)
		if result["state"] == "completed" || result["state"] == "failed" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if result["state"] != "completed" || result["summary"] != "TCX_EXISTING_DONE" || result["receivedAt"] == nil {
		t.Fatal("missing successful receipt", result)
	}
	if result["briefRevision"] != float64(1) {
		t.Fatal("request lost confirmed brief", result)
	}
	var read struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err = owner.Call(ctx, "thread/read", map[string]any{"threadId": thread, "includeTurns": false}, &read); err != nil || read.Thread.ID != thread {
		t.Fatal(read, err)
	}
	t.Logf("single Mac; native model=%s; same thread connected, request submitted/read/completed; immutable brief revision=1", created.Model)
}
