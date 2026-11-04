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
	"sync/atomic"
	"teamcross/internal/mcp"
	"teamcross/internal/mcpassets"
	"testing"
	"time"
)

// Real production bundles/Core, synthetic MCP host and source identity.
// Explicitly enabled; all fixtures are isolated and terminate via finish.
func TestPluginUpdateBrowserFixture(t *testing.T) {
	dir := os.Getenv("TEAMCROSS_UPDATE_BROWSER_DIR")
	if dir == "" {
		t.Skip("explicit fixture directory required")
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a, f, s := spaceFixture(t)
	_ = a.SetUILanguage("zh-CN")
	f.source.Name = "插件更新体验验证"
	caller := agentCaller{Caller: mcp.CallerSource{Provider: "codex", SourceID: f.source.ID, TurnID: "turn-fixture"}}
	_, target := connectNamedFixture(t, a, caller, s.record.ID, "")
	a.pumpWorkbench(context.Background())
	// Enable the fixture target so its note composer can be used without a model.
	if _, err := a.workbenchCall(context.Background(), s.record.ID, "inflow-config", workbenchInput{TargetID: target.ID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	api := a.Handler(http.NotFoundHandler())
	var active atomic.Int32
	panels := []string{}
	bundles := []mcpassets.Bundle{}
	for n := 0; n < 4; n++ {
		panel := strings.Replace(mcpassets.Panel, "</script>", fmt.Sprintf("\n;document.documentElement.dataset.fixtureBuild='%d';\n</script>", n), 1)
		// Migration must use an actually old updater, not another copy of the
		// fixed entrypoint. The supplied file is an explicit local test fixture.
		if legacy := os.Getenv("TEAMCROSS_UPDATE_LEGACY_PANEL"); n == 0 && legacy != "" {
			b, err := os.ReadFile(legacy)
			if err != nil {
				t.Fatal(err)
			}
			panel = string(b)
		}
		if n == 3 {
			panel = strings.Replace(panel, `<script type="module">`, `<script type="module">throw new Error('fixture startup failure');`, 1)
		}
		bundle := mcpassets.ParseBundle(panel)
		panel = strings.Replace(panel, `content="`+mcpassets.Current.Revision+`"`, `content="`+bundle.Revision+`"`, 1)
		panels = append(panels, panel)
		bundles = append(bundles, bundle)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/panel.html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; connect-src 'none'")
			fmt.Fprint(w, panels[0])
		case "/fixture-version":
			if r.Method == "POST" && active.Load() < 3 {
				active.Add(1)
			}
			respond(w, map[string]int32{"version": active.Load()}, nil)
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><style>body{margin:0;font:14px system-ui}header{height:40px;display:flex;align-items:center;gap:20px;padding:0 12px;background:#eee}iframe{width:100vw;height:calc(100vh - 40px);border:0}</style><header><button id="publish">发布下一个测试版本</button><span id="server">服务器版本 0</span><span id="loaded">面板版本 0</span><span id="writes">业务写入 0</span></header><iframe id="panel" sandbox="allow-scripts allow-same-origin allow-modals" src="/panel.html#/collaborations/%s"></iframe><script>
let writes=0;
publish.onclick=async()=>{const value=await(await fetch('/fixture-version',{method:'POST'})).json();server.textContent='服务器版本 '+value.version;panel.contentWindow.dispatchEvent(new Event('focus'));};
setInterval(()=>{const d=panel.contentDocument;loaded.textContent='面板版本 '+(d.documentElement.dataset.fixtureBuild||'旧版')+' · '+(d.querySelector('meta[name="teamcross-panel-revision"]')?.content||'加载中').slice(0,12);},200);
addEventListener('message',async event=>{
 const m=event.data;if(event.source!==panel.contentWindow||!m.id)return;let result={};
 if(m.method==='ui/initialize')result={hostCapabilities:{message:{}},hostContext:{theme:'light'}};
 if(m.method==='tools/call'){
  if(m.params.name==='teamcross_ui_write'){writes++;document.getElementById('writes').textContent='业务写入 '+writes;}
  const response=await fetch('/fixture-tools',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(m.params.arguments)});
  result={structuredContent:await response.json(),isError:!response.ok};
 }
 event.source.postMessage({jsonrpc:'2.0',id:m.id,result},'*');
});</script>`, s.record.ID)
		case "/fixture-tools":
			var in struct {
				Path string          `json:"path"`
				Body json.RawMessage `json:"body"`
			}
			if !decode(w, r, &in) {
				return
			}
			if in.Path == "plugin/bootstrap" {
				respond(w, map[string]any{"scope": "update-fixture", "bridgeProtocol": mcp.UIBridgeProtocol}, nil)
				return
			}
			if in.Path == "plugin/panel-version" {
				b := bundles[active.Load()]
				b.Script = ""
				b.Style = ""
				respond(w, b, nil)
				return
			}
			if strings.HasPrefix(in.Path, "plugin/panel-bundle?") {
				b := bundles[active.Load()]
				if in.Path != "plugin/panel-bundle?revision="+b.Revision {
					respond(w, nil, fmt.Errorf("version changed"))
					return
				}
				respond(w, b, nil)
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
	if err := writeJSONFile(filepath.Join(dir, "fixture.json"), map[string]string{"url": server.URL, "spaceId": s.record.ID}); err != nil {
		t.Fatal(err)
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	deadline := time.NewTimer(25 * time.Minute)
	defer deadline.Stop()
	for {
		select {
		case <-tick.C:
			if _, err := os.Stat(filepath.Join(dir, "finish")); err == nil {
				return
			}
		case <-deadline.C:
			t.Fatal("fixture expired")
		}
	}
}
