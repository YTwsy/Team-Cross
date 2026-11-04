package collab

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"teamcross/internal/mcp"
	"teamcross/internal/mcpassets"
	"teamcross/internal/webassets"
)

// Both production UIs use real Core/storage. Native sources and the plugin host
// are synthetic; transport identity enforcement is covered by MCP tests.
func TestInFlowConnectionsBrowserFixture(t *testing.T) {
	dir := os.Getenv("TEAMCROSS_INFLOW_CONNECTION_BROWSER_DIR")
	if dir == "" {
		t.Skip("explicit fresh fixture directory required")
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a, f, s := spaceFixture(t)
	_ = a.SetUILanguage("zh-CN")
	f.source.Name = "顺流感知：自己的工作会话"
	caller := mcp.CallerSource{Provider: "codex", SourceID: f.source.ID}
	assets, err := fs.Sub(webassets.Dist, "dist")
	if err != nil {
		t.Fatal(err)
	}
	api := a.Handler(http.FileServer(http.FS(assets)))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/panel.html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, mcpassets.Panel)
		case "/plugin-host":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><style>body{margin:0;overflow:hidden}iframe{display:block;width:100%%;height:100vh;border:0}</style><iframe id="panel" src="/panel.html#/collaborations/%s"></iframe><script>
addEventListener('message', async event => {
 const m=event.data; if(event.source!==panel.contentWindow||!m.id)return;
 let result={};
 if(m.method==='ui/initialize')result={hostCapabilities:{},hostContext:{theme:'light'}};
 if(m.method==='tools/call') {
  const response=await fetch('/fixture-tools',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(m.params.arguments)});
  result={structuredContent:await response.json(),isError:!response.ok};
 }
 if(m.method==='ui/message') throw new Error('Passive setup must not send a conversation message');
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
				respond(w, map[string]any{"scope": "inflow-browser-fixture", "bridgeProtocol": mcp.UIBridgeProtocol}, nil)
				return
			}
			if strings.HasPrefix(in.Path, "plugin/current-connection?") {
				out, err := a.currentConnection(r.Context(), caller, s.record.ID)
				respond(w, out, err)
				return
			}
			if in.Path == "plugin/current-inflow" {
				out, err := a.connectInFlow(r.Context(), s.record.ID, caller, "")
				respond(w, out, err)
				return
			}
			method := "GET"
			if len(in.Body) > 0 {
				method = "POST"
			}
			forward := httptest.NewRequest(method, "/api/"+in.Path, strings.NewReader(string(in.Body)))
			forward.Host, forward.RemoteAddr = r.Host, r.RemoteAddr
			forward.Header.Set("Authorization", "Bearer "+a.Token)
			api.ServeHTTP(w, forward)
		default:
			api.ServeHTTP(w, r)
		}
	}))
	defer server.Close()
	if err := writeJSONFile(filepath.Join(dir, "fixture.json"), map[string]string{"url": server.URL + "/#/collaborations/" + s.record.ID, "pluginUrl": server.URL + "/plugin-host", "spaceId": s.record.ID}); err != nil {
		t.Fatal(err)
	}
	t.Log("inflow connection browser ready", dir)
	timer := time.NewTimer(25 * time.Minute)
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
