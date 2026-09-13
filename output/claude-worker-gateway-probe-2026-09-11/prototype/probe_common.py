import hashlib
import http.client
import http.server
import json
import os
import pathlib
import queue
import signal
import subprocess
import threading
import time
import urllib.parse
import uuid

ROOT = pathlib.Path(__file__).resolve().parent
CLI = '/Users/wsy/.nvm/versions/node/v24.18.0/bin/claude'
MODEL = 'gpt-5.6-luna'
MODEL_NAMES = {MODEL, 'gpt-5.6-luna[1m]'}
SETTINGS = ROOT / 'config' / 'settings.json'
CREDS = json.loads((pathlib.Path.home() / '.claude/settings.json').read_text())['env']
TOKEN = CREDS.get('ANTHROPIC_AUTH_TOKEN') or CREDS.get('ANTHROPIC_API_KEY')
DUMMY_TOKEN = 'teamcross-local-fixture-only'

def clean(v):
    if isinstance(v, str):
        return v.replace(TOKEN, '[redacted]').replace(DUMMY_TOKEN, '[fixture-token]')
    if isinstance(v, dict):
        return {k: ('[redacted]' if k.lower() in ('authorization', 'x-api-key', 'accesstoken', 'authtoken') else clean(x)) for k,x in v.items()}
    if isinstance(v, list):
        return [clean(x) for x in v]
    return v

def emit(kind, **data):
    print(json.dumps(clean({'event': kind, **data}), ensure_ascii=False), flush=True)

class Guard:
    def __init__(self):
        self.entries = []
        self.allow_generation = True
        self.lock = threading.Lock()
        guard = self
        upstream = urllib.parse.urlsplit(CREDS['ANTHROPIC_BASE_URL'])
        assert upstream.hostname in ('127.0.0.1', 'localhost'), 'Expected configured local CliProxyAPI'
        class Handler(http.server.BaseHTTPRequestHandler):
            protocol_version = 'HTTP/1.1'
            def log_message(self, *args): pass
            def do_GET(self): self.forward()
            def do_POST(self): self.forward()
            def forward(self):
                body = self.rfile.read(int(self.headers.get('Content-Length', '0')))
                payload = {}
                if body:
                    try: payload = json.loads(body)
                    except ValueError: pass
                model = payload.get('model')
                entry = {'time': time.time(), 'method': self.command, 'path': self.path, 'model': model}
                if model:
                    (ROOT/'evidence'/'last-model-request.json').write_text(json.dumps(clean(payload),ensure_ascii=False,indent=2))
                    (ROOT/'evidence'/'last-model-headers.json').write_text(json.dumps({k:v for k,v in self.headers.items() if k.lower() not in ('authorization','x-api-key','host')},indent=2))
                    entry['request_bytes']=len(body)
                    entry['max_tokens']=payload.get('max_tokens')
                    entry['thinking']=payload.get('thinking')
                    entry['output_config']=payload.get('output_config')
                with guard.lock:
                    guard.entries.append(entry)
                    with (ROOT/'evidence'/'model-requests.jsonl').open('a') as f:
                        f.write(json.dumps(entry)+'\n')
                if (model and model not in MODEL_NAMES) or (not guard.allow_generation and self.command=='POST' and '/messages' in self.path and '/count_tokens' not in self.path) or not self.path.startswith(('/v1/', '/api/claude_code/')):
                    entry['blocked'] = True
                    data = json.dumps({'type':'error','error':{'type':'invalid_request_error','message':'Test guard rejected unselected model or unrelated endpoint'}}).encode()
                    self.send_response(403);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(data)));self.end_headers();self.wfile.write(data)
                    emit('guard_block', model=model, path=self.path)
                    return
                headers = {k:v for k,v in self.headers.items() if k.lower() not in ('host','authorization','x-api-key','connection','accept-encoding')}
                headers['Authorization'] = 'Bearer '+TOKEN
                headers['x-api-key'] = TOKEN
                headers['Accept-Encoding'] = 'identity'
                conn = http.client.HTTPConnection(upstream.hostname, upstream.port, timeout=75)
                try:
                    conn.request(self.command, (upstream.path.rstrip('/')+self.path), body=body, headers=headers)
                    response = conn.getresponse()
                    entry['status'] = response.status
                    self.send_response(response.status)
                    for k,v in response.getheaders():
                        if k.lower() not in ('transfer-encoding','connection','content-length'):
                            self.send_header(k,v)
                    self.send_header('Connection','close'); self.end_headers()
                    while True:
                        data = response.read1(65536)
                        if not data: break
                        self.wfile.write(data); self.wfile.flush()
                    self.close_connection = True
                except (BrokenPipeError, ConnectionResetError): pass
                except Exception as e:
                    entry['error'] = type(e).__name__
                    emit('guard_error', error=type(e).__name__)
                    self.close_connection = True
                finally:
                    conn.close()
                    with guard.lock:
                        with (ROOT/'evidence'/'model-responses.jsonl').open('a') as f:
                            f.write(json.dumps(clean(entry))+'\n')
        self.server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.server.daemon_threads = True
        self.url = 'http://127.0.0.1:'+str(self.server.server_port)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
    def close(self):
        self.server.shutdown(); self.server.server_close()
    def generation_count(self):
        return sum(e['method']=='POST' and '/messages' in e['path'] and '/count_tokens' not in e['path'] for e in self.entries)

def make_env(guard, config='config'):
    env = {k:v for k,v in os.environ.items() if not k.startswith(('ANTHROPIC_', 'CLAUDE_'))}
    env.update({
        'CLAUDE_CONFIG_DIR': str(ROOT/config),
        'ANTHROPIC_BASE_URL': guard.url,
        'ANTHROPIC_AUTH_TOKEN': DUMMY_TOKEN,
        'ANTHROPIC_MODEL': MODEL,
        'CLAUDE_CODE_SUBAGENT_MODEL': MODEL,
        'CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY': '1',
        'CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC': '1',
        'CLAUDE_CODE_DISABLE_AUTO_MEMORY': '1',
        'CLAUDE_CODE_MAX_OUTPUT_TOKENS': '2048',
        'CLAUDE_CODE_MAX_RETRIES': '0',
        'DISABLE_AUTOUPDATER': '1',
        'DISABLE_TELEMETRY': '1',
        'NO_COLOR': '1',
        'TERM': 'xterm-256color',
    })
    for name in ('OPUS','SONNET','HAIKU','FABLE'):
        env['ANTHROPIC_DEFAULT_'+name+'_MODEL'] = MODEL
    return env

def base_args():
    return ['--model',MODEL,'--settings',str(SETTINGS),'--setting-sources','','--strict-mcp-config','--mcp-config','{"mcpServers":{}}','--tools','Bash,Read,Write,Edit,AskUserQuestion','--permission-mode','manual','--no-chrome']

class Runtime:
    def __init__(self, guard, label, extra=None, cwd=None):
        self.label=label; self.messages=[]; self.q=queue.Queue(); self.closed=False
        self.out=(ROOT/'evidence'/(label+'.jsonl')).open('w')
        self.err=(ROOT/'evidence'/(label+'.stderr')).open('w')
        args=[CLI,'--print','--input-format','stream-json','--output-format','stream-json','--verbose','--replay-user-messages','--permission-prompt-tool','stdio','--max-turns','5']+base_args()+(extra or [])
        self.p=subprocess.Popen(args,cwd=str(cwd or ROOT/'repo'),env=make_env(guard),stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=self.err,text=True,bufsize=1,start_new_session=True)
        def read():
            for line in self.p.stdout:
                try: m=json.loads(line)
                except ValueError: m={'type':'unparsed','text':line.strip()}
                self.messages.append(m);self.q.put(m)
                self.out.write(json.dumps(clean(m),ensure_ascii=False)+'\n');self.out.flush()
        self.reader=threading.Thread(target=read,daemon=True);self.reader.start()
        emit('runtime_start',label=label,pid=self.p.pid,cwd=str(cwd or ROOT/'repo'))
    def send(self,m):
        self.p.stdin.write(json.dumps(m)+'\n');self.p.stdin.flush()
    def control(self,subtype,**fields):
        rid=str(uuid.uuid4());self.send({'type':'control_request','request_id':rid,'request':{'subtype':subtype,**fields}});return rid
    def wait(self,predicate,timeout=50):
        end=time.monotonic()+timeout
        while time.monotonic()<end:
            try:m=self.q.get(timeout=min(.3,end-time.monotonic()))
            except queue.Empty:
                if self.p.poll() is not None: raise RuntimeError('CLI exited: '+str(self.p.returncode)+' '+self.err.name)
                continue
            if predicate(m): return m
        raise TimeoutError(self.label)
    def control_result(self,rid,timeout=25):
        return self.wait(lambda m:m.get('type')=='control_response' and m.get('response',{}).get('request_id')==rid,timeout)
    def initialize(self):
        return self.control_result(self.control('initialize',hooks=None))
    def user(self,text,session_id=''):
        uid=str(uuid.uuid4());self.send({'type':'user','uuid':uid,'session_id':session_id,'parent_tool_use_id':None,'message':{'role':'user','content':text}});return uid
    def reply(self,request,allow=False):
        data={'behavior':'allow','updatedInput':request['request']['input']} if allow else {'behavior':'deny','message':'Denied by the isolated Team Cross test controller. Do not retry.'}
        self.send({'type':'control_response','response':{'subtype':'success','request_id':request['request_id'],'response':data}})
    def close(self):
        if self.closed:return
        self.closed=True
        try:self.p.stdin.close()
        except Exception:pass
        try:self.p.wait(timeout=6)
        except subprocess.TimeoutExpired:
            os.killpg(self.p.pid,signal.SIGTERM)
            try:self.p.wait(timeout=4)
            except subprocess.TimeoutExpired:os.killpg(self.p.pid,signal.SIGKILL);self.p.wait()
        self.reader.join(timeout=2);self.out.close();self.err.close()
        emit('runtime_closed',label=self.label,pid=self.p.pid,exit_code=self.p.returncode)

def sessions():
    out=[]
    for p in (ROOT/'config'/'projects').glob('**/*.jsonl'):
        if '/subagents/' not in str(p):out.append({'path':str(p),'bytes':p.stat().st_size,'sha256':hashlib.sha256(p.read_bytes()).hexdigest()})
    return out
