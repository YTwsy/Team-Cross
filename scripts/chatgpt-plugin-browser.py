#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Independent browser host harness, using real embedded HTML and stdio MCP.

Host message/context acknowledgements are simulated; business tools are real.
The panel iframe has an opaque sandbox origin and no network access. Its parent
uses one loopback test endpoint; no Core token is exposed to browser JavaScript.
"""
import argparse
import importlib.util
import json
import threading
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

parser=argparse.ArgumentParser();parser.add_argument("--fixture",required=True);parser.add_argument("--teamcross-bin",required=True);parser.add_argument("--output",required=True)
args=parser.parse_args();root=Path(__file__).resolve().parents[1];output=Path(args.output).resolve();output.mkdir(mode=0o700)
fixture=json.loads(Path(args.fixture).read_text())
spec=importlib.util.spec_from_file_location("plugin_verify",root/"scripts/verify-chatgpt-plugin.py");module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
client=module.Stdio(str(Path(args.teamcross_bin).resolve()),fixture["dataDir"])
html=client.rpc("resources/read",{"uri":"ui://teamcross/workspace-v1.html"})["contents"][0]["text"]
token=uuid.uuid4().hex;prefix="/"+token;lock=threading.Lock();events=[]
page='''<!doctype html><meta charset="utf-8"><title>Team Cross plugin acceptance</title><style>body{margin:0;font:14px system-ui;background:#eee}header{padding:12px}iframe{display:block;border:0;height:1200px;width:1440px;max-width:100%;margin:auto}label{margin-right:20px}</style><header><strong>Independent browser · real Core · simulated host</strong><label>Panel width<select id="width"><option>1440</option><option>1024</option><option>768</option><option>480</option></select></label><span id="status">Waiting</span></header><iframe title="Team Cross WebGUI" sandbox="allow-scripts" src="PREFIX/panel"></iframe><script>
const frame=document.querySelector('iframe');document.querySelector('#width').onchange=e=>frame.style.width=e.target.value+'px';
window.addEventListener('message',async e=>{
if(e.source!==frame.contentWindow||e.data?.jsonrpc!=='2.0')return;const m=e.data;if(!m.id)return;
let result;
try{if(m.method==='ui/initialize')result={protocolVersion:'2026-01-26',hostInfo:{name:'teamcross-browser-fixture',version:'1.0.0'},hostCapabilities:{message:{},updateModelContext:{}},hostContext:{theme:'light',locale:'zh-CN',displayMode:'fullscreen'}};
else{const response=await fetch('PREFIX/rpc',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(m)});if(!response.ok)throw Error('Harness unavailable');result=await response.json();}
frame.contentWindow.postMessage({jsonrpc:'2.0',id:m.id,result},'*');document.querySelector('#status').textContent=m.method+' acknowledged';
}catch(error){frame.contentWindow.postMessage({jsonrpc:'2.0',id:m.id,error:{code:-32000,message:error.message}},'*');}
});</script>'''.replace('PREFIX',prefix)
class Handler(BaseHTTPRequestHandler):
    def log_message(self,*values):pass
    def send(self,body,kind,status=200):
        encoded=body.encode();self.send_response(status);self.send_header('Content-Type',kind);self.send_header('Content-Length',str(len(encoded)));self.end_headers();self.wfile.write(encoded)
    def do_GET(self):
        if self.path==prefix+'/':self.send(page,'text/html; charset=utf-8')
        elif self.path==prefix+'/panel':
            self.send_response(200);self.send_header('Content-Type','text/html; charset=utf-8');self.send_header('Content-Security-Policy',"default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; connect-src 'none'; font-src 'none'; base-uri 'none'; form-action 'none'");self.end_headers();self.wfile.write(html.encode())
        else:self.send('Not found','text/plain',404)
    def do_POST(self):
        if self.path!=prefix+'/rpc':self.send('Not found','text/plain',404);return
        message=json.loads(self.rfile.read(min(int(self.headers.get('Content-Length','0')),1<<20)))
        with lock:
            if message['method']=='tools/call':
                result=client.rpc('tools/call',message['params'])
                events.append({'method':message['method'],'params':message['params'],'result':result})
            elif message['method'] in ('ui/message','ui/update-model-context'):
                result={};events.append({'method':message['method'],'params':message['params'],'host':'simulated'})
            else:result={}
            module.write(output/'events.json',events)
        self.send(json.dumps(result,ensure_ascii=False),'application/json')
server=ThreadingHTTPServer(('127.0.0.1',0),Handler)
module.write(output/'browser.json',{'url':f'http://127.0.0.1:{server.server_port}{prefix}/','stdioPid':client.process.pid,'host':'simulated','panel':'embedded production resource','core':'real'})
print(f'http://127.0.0.1:{server.server_port}{prefix}/',flush=True)
try:server.serve_forever()
except KeyboardInterrupt:pass
finally:server.server_close();client.close()
