#!/usr/bin/env python3
"""Real resident old MCP + isolated Core, with a simulated browser host.

Prepare immutable old HTML from explicit local binaries, then use
TestChatGPTPluginFixture with TEAMCROSS_PLUGIN_UPGRADE_BUNDLES set to the output.
Never restart a user's MCP, Core, or desktop. Browser controls affect only this
fixture. The iframe is opaque and cannot reach Core directly.
"""
import argparse
import hashlib
import importlib.util
import json
import threading
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

p = argparse.ArgumentParser()
p.add_argument("--output", required=True)
p.add_argument("--legacy-bin", required=True)
p.add_argument("--previous-bin")
p.add_argument("--current-bin")
p.add_argument("--fixture")
p.add_argument("--prepare", action="store_true")
p.add_argument("--initial-ui", choices=["legacy", "current"], default="legacy")
p.add_argument("--theme", choices=["light", "dark"], default="light")
p.add_argument("--width", type=int, default=0)
a = p.parse_args()
root = Path(__file__).resolve().parents[1]
out = Path(a.output).resolve()
spec = importlib.util.spec_from_file_location("plugin_verify", root / "scripts/verify-chatgpt-plugin.py")
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
if a.prepare:
    out.mkdir(mode=0o700)
    for name, binary in [("legacy", a.legacy_bin), ("previous", a.previous_bin)]:
        assert binary
        client = module.Stdio(str(Path(binary).resolve()), str(out / "unused-data"))
        try:
            panel = client.rpc("resources/read", {"uri": "ui://teamcross/workspace-v1.html"})["contents"][0]["text"]
            (out / (name + ".html")).write_text(panel)
        finally:
            client.close()
    raise SystemExit(0)

assert a.fixture and a.current_bin
fixture_path = Path(a.fixture).resolve()
fixture = json.loads(fixture_path.read_text())
client = module.Stdio(str(Path(a.legacy_bin).resolve()), fixture["dataDir"])
legacy_pid = client.process.pid
html = (out / "legacy.html").read_text()
if a.initial_ui == "current":
    resource_client = module.Stdio(str(Path(a.current_bin).resolve()), fixture["dataDir"])
    try:
        html = resource_client.rpc("resources/read", {"uri": "ui://teamcross/workspace-v1.html"})["contents"][0]["text"]
    finally:
        resource_client.close()
lock = threading.Lock()
events = []
prefix = "/" + uuid.uuid4().hex
page = '''<!doctype html><meta charset="utf-8"><title>Plugin bridge upgrade fixture</title>
<style>body{margin:0;font:14px system-ui}header{padding:8px;display:flex;gap:12px;flex-wrap:wrap}iframe{width:100%;height:calc(100vh - 74px);border:0}</style>
<header><span>Real resident MCP · simulated host</span><button data-stage="1">Load previous UI</button><button data-stage="2">Load fixed UI</button><button id="reload">Reload fixture MCP</button><span id="state">Legacy MCP</span></header>
<iframe title="Team Cross plugin" style="FRAMESTYLE" sandbox="allow-scripts" src="PREFIX/panel#/collaborations/SPACE"></iframe>
<script>
const frame=document.querySelector('iframe');
for(const b of document.querySelectorAll('[data-stage]'))b.onclick=async()=>{await fetch('PREFIX/stage',{method:'POST',body:b.dataset.stage});state.textContent='Core bundle '+b.dataset.stage;};
reload.onclick=async()=>{await fetch('PREFIX/reload',{method:'POST'});frame.src='PREFIX/panel?new=1#/collaborations/SPACE';state.textContent='New MCP';};
addEventListener('message',async e=>{if(e.source!==frame.contentWindow||e.data?.jsonrpc!=='2.0'||!e.data.id)return;const m=e.data;let result;
try{if(m.method==='ui/initialize')result={protocolVersion:'2026-01-26',hostCapabilities:{message:{},updateModelContext:{}},hostContext:{locale:'zh-CN',theme:'THEME'}};
else result=await(await fetch('PREFIX/rpc',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(m)})).json();
frame.contentWindow.postMessage({jsonrpc:'2.0',id:m.id,result},'*');}catch(e){frame.contentWindow.postMessage({jsonrpc:'2.0',id:m.id,error:{code:-32000,message:e.message}},'*');}});
</script>'''.replace("PREFIX", prefix).replace("SPACE", fixture["spaceId"]).replace("THEME", a.theme).replace("FRAMESTYLE", f"width:{a.width}px;max-width:100%" if a.width else "")

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args): pass
    def send(self, value, kind="application/json", csp=False):
        data = value.encode() if isinstance(value, str) else json.dumps(value).encode()
        self.send_response(200)
        self.send_header("Content-Type", kind)
        self.send_header("Content-Length", str(len(data)))
        if csp:
            self.send_header("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; connect-src 'none'; font-src 'none'; base-uri 'none'; form-action 'none'")
        self.end_headers()
        self.wfile.write(data)
    def do_GET(self):
        if self.path == prefix + "/": self.send(page, "text/html; charset=utf-8")
        elif self.path.startswith(prefix + "/panel"): self.send(html, "text/html; charset=utf-8", True)
        else: self.send_error(404)
    def do_POST(self):
        global client, html
        body = self.rfile.read(min(int(self.headers.get("Content-Length", "0")), 1 << 20))
        with lock:
            if self.path == prefix + "/stage":
                assert body in (b"1", b"2")
                (fixture_path.parent / "bundle-stage").write_bytes(body)
                events.append({"stage": int(body), "residentPid": client.process.pid, "legacyStillRunning": client.process.pid == legacy_pid and client.process.poll() is None})
                result = {}
            elif self.path == prefix + "/reload":
                client.close()
                client = module.Stdio(str(Path(a.current_bin).resolve()), fixture["dataDir"])
                html = client.rpc("resources/read", {"uri": "ui://teamcross/workspace-v1.html"})["contents"][0]["text"]
                events.append({"reload": True, "newPid": client.process.pid})
                result = {}
            elif self.path == prefix + "/rpc":
                m = json.loads(body)
                if m["method"] == "tools/call":
                    result = client.rpc(m["method"], m["params"])
                    value = result.get("structuredContent")
                    if value is None:
                        texts = [v.get("text", "null") for v in result.get("content", []) if v.get("type") == "text"]
                        value = json.loads(texts[0]) if texts else {}
                    events.append({"pid": client.process.pid, "tool": m["params"]["name"], "path": m["params"].get("arguments", {}).get("path"), "isError": bool(result.get("isError")), "error": value.get("error") if isinstance(value, dict) else None, "code": value.get("code") if isinstance(value, dict) else None})
                else: result = {}
            else:
                self.send_error(404)
                return
            module.write(out / "events.json", events)
        self.send(result)

server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
module.write(out / "browser.json", {"url": f"http://127.0.0.1:{server.server_port}{prefix}/", "legacyPid": legacy_pid, "legacySHA256": hashlib.sha256(Path(a.legacy_bin).read_bytes()).hexdigest(), "currentSHA256": hashlib.sha256(Path(a.current_bin).read_bytes()).hexdigest(), "host": "simulated; real old stdio and authenticated Core"})
def finish():
    while not (out / "finish-browser").exists(): time.sleep(0.5)
    server.shutdown()
threading.Thread(target=finish, daemon=True).start()
print((out / "browser.json").read_text(), flush=True)
try:
    server.serve_forever()
finally:
    server.server_close()
    client.close()
