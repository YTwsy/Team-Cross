package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPairingRejectsModelSuppliedIdentity(t *testing.T) {
	for _, name := range []string{"pair_current_session", "read_agent_request", "finish_agent_request"} {
		if _, err := (Backend{}).Invoke(context.Background(), name, map[string]any{"code": "TCP-test", "sourceId": uuid.NewString(), "provider": "codex"}); err == nil {
			t.Fatal("model chose session identity", name)
		}
	}
}

func TestChannelStdioLifecycle(t *testing.T) {
	id := uuid.NewString()
	t.Setenv("CLAUDE_CODE_SESSION_ID", id)
	var polls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if r.URL.Path == "/api/agent-receivers/poll" {
			if polls.Add(1) == 1 {
				_, _ = w.Write([]byte(`{"event":{"kind":"pairing_verification","pairingId":"pair","challenge":"notification-only"}}`))
				return
			}
			<-r.Context().Done()
			return
		}
		caller := in["caller"].(map[string]any)
		if caller["sourceId"] != id || caller["toolUseId"] != "call-1" || len(in["arguments"].(map[string]any)) != 1 || in["receiverId"] == "" {
			t.Error(in)
		}
		_, _ = w.Write([]byte(`{"state":"verifying"}`))
	}))
	defer server.Close()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	defer inR.Close()
	defer outR.Close()
	backend := Backend{URL: server.URL, Token: "local-secret", Client: server.Client()}
	done := make(chan error, 1)
	go func() {
		defer outW.Close()
		done <- serve(context.Background(), inR, outW, Tools(), "", func(ctx context.Context, name string, args map[string]any, _ string) (json.RawMessage, error) {
			return backend.Invoke(ctx, name, args)
		})
	}()
	lines := make(chan map[string]any, 8)
	go func() {
		defer close(lines)
		scan := bufio.NewScanner(outR)
		for scan.Scan() {
			var v map[string]any
			if err := json.Unmarshal(scan.Bytes(), &v); err != nil {
				t.Error(err)
				return
			}
			lines <- v
		}
	}()
	_, _ = io.WriteString(inW, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"claude-code"}}}`+"\n")
	init := <-lines
	capability := init["result"].(map[string]any)["capabilities"].(map[string]any)["experimental"].(map[string]any)
	if capability["claude/channel"] == nil {
		t.Fatal(init)
	}
	_, _ = io.WriteString(inW, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"pair_current_session","arguments":{"code":"TCP-example"},"_meta":{"claudecode/toolUseId":"call-1"}}}`+"\n")
	reply, notice := false, false
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for !reply || !notice {
		select {
		case value := <-lines:
			if value["method"] == "notifications/claude/channel" {
				notice = true
				if !strings.Contains(value["params"].(map[string]any)["content"].(string), "notification-only") {
					t.Fatal(value)
				}
			} else if value["id"] == float64(2) {
				reply = true
				if value["result"].(map[string]any)["isError"] != false {
					t.Fatal(value)
				}
			}
		case <-timer.C:
			t.Fatal("missing response or unsolicited channel notification")
		}
	}
	_ = inW.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("receiver did not stop with stdio")
	}
}
