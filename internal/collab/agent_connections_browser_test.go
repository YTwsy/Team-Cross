package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"teamcross/internal/mcp"
	"teamcross/internal/mcpassets"
)

// Production panel + real Core/storage; the host bridge and native source are
// synthetic. This does not claim a live Codex host refresh/identity acceptance.
func TestCurrentConnectionBrowserFixture(t *testing.T) {
	dir := os.Getenv("TEAMCROSS_CONNECTION_BROWSER_DIR")
	if dir == "" {
		t.Skip("explicit fresh fixture directory required")
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a, f, s := spaceFixture(t)
	_ = a.SetUILanguage("zh-CN")
	f.source.Name = "顺流感知设计与验证"
	caller := agentCaller{Caller: mcp.CallerSource{Provider: "codex", SourceID: f.source.ID, TurnID: "turn-fixture"}}
	_, target := connectNamedFixture(t, a, caller, s.record.ID, "")
	a.pumpWorkbench(context.Background())
	api := a.Handler(http.NotFoundHandler())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/panel.html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, mcpassets.Panel)
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><style>body{margin:0}iframe{width:100vw;height:100vh;border:0}</style><iframe id="panel" src="/panel.html#/collaborations/%s"></iframe><script>
let saved;
addEventListener('message', async (event) => {
 const m=event.data; if(event.source!==panel.contentWindow||!m.id)return;
 let result={};
 if(m.method==='ui/initialize') result={hostCapabilities:{message:{}},hostContext:{theme:'light'}};
 if(m.method==='tools/call') {
   const response=await fetch('/fixture-tools',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(m.params.arguments)});
   result={structuredContent:await response.json(),isError:!response.ok};
 }
 event.source.postMessage({jsonrpc:'2.0',id:m.id,result},'*');
});
</script>`, s.record.ID)
		case "/fixture-tools":
			var in struct {
				Path string          `json:"path"`
				Body json.RawMessage `json:"body"`
			}
			if !decode(w, r, &in) {
				return
			}
			if in.Path == "plugin/bootstrap" {
				respond(w, map[string]string{"scope": "connection-fixture"}, nil)
				return
			}
			if strings.HasPrefix(in.Path, "plugin/current-connection?") {
				out, err := a.currentConnection(r.Context(), caller.Caller, s.record.ID)
				respond(w, out, err)
				return
			}
			method := "GET"
			if len(in.Body) > 0 {
				method = "POST"
			}
			forward := httptest.NewRequest(method, "/api/"+in.Path, strings.NewReader(string(in.Body)))
			forward.RemoteAddr = r.RemoteAddr
			forward.Host = r.Host
			forward.Header.Set("Authorization", "Bearer "+a.Token)
			api.ServeHTTP(w, forward)
		default:
			api.ServeHTTP(w, r)
		}
	}))
	defer server.Close()
	if err := writeJSONFile(filepath.Join(dir, "fixture.json"), map[string]string{"url": server.URL, "spaceId": s.record.ID, "targetId": target.ID}); err != nil {
		t.Fatal(err)
	}
	t.Log("current connection browser fixture ready", dir)
	timer := time.NewTimer(15 * time.Minute)
	defer timer.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-timer.C:
			t.Fatal("browser fixture expired")
		case <-tick.C:
			if _, err := os.Stat(filepath.Join(dir, "finish")); err == nil {
				return
			}
		}
	}
}
