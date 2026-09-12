#!/usr/bin/env python3
"""Opt-in, one-Mac Claude TUI acceptance through two actual Team Cross Cores.

Uses only a new fixture directory and CliProxyAPI's gpt-5.6-luna route. Secrets
are read in memory by a loopback guard; fixture settings receive a dummy token.
No personal session is resumed and no installed Core is stopped.
"""
import argparse
import fcntl
import hashlib
import http.client
import http.server
import json
import os
import pathlib
import pty
import re
import select
import signal
import struct
import subprocess
import termios
import threading
import time
import traceback
import urllib.error
import urllib.parse
import urllib.request
import uuid

MODEL = "gpt-5.6-luna"
MODEL_NAMES = {MODEL, MODEL+"[1m]"}
DUMMY_TOKEN = "teamcross-local-fixture-only"


def clean(value):
    if isinstance(value, str):
        return value.replace(TOKEN, "[redacted]") if TOKEN else value
    if isinstance(value, dict):
        return {k: ("[redacted]" if k.lower() in ("authorization", "x-api-key", "token", "invitation", "credential") else clean(v)) for k,v in value.items()}
    if isinstance(value, list): return [clean(v) for v in value]
    return value


def emit(event, **fields):
    print(json.dumps(clean({"event":event, **fields}), ensure_ascii=False), flush=True)


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


def wait_until(check, seconds=45, terminal=None):
    end=time.monotonic()+seconds
    while time.monotonic()<end:
        if terminal: terminal.read(.15)
        else: time.sleep(.15)
        value=check()
        if value: return value
    raise TimeoutError(getattr(check, '__name__', 'condition'))


def terminal_text(data):
    text=data.decode('utf-8','replace')
    text=re.sub(r'\x1b\][^\x07]*(?:\x07|\x1b\\)','',text)
    text=re.sub(r'\x1b\[[0-?]*[ -/]*[@-~]','',text)
    return text.replace('\r','\n').replace('\x1b','')


class Terminal:
    def __init__(self, label, command, env):
        self.label=label; self.data=bytearray(); self.closed=False
        self.master, slave=pty.openpty()
        fcntl.ioctl(slave,termios.TIOCSWINSZ,struct.pack('HHHH',36,120,0,0))
        def setup():
            os.setsid();fcntl.ioctl(slave,termios.TIOCSCTTY,0)
        self.p=subprocess.Popen(['/bin/zsh','-c',command],stdin=slave,stdout=slave,stderr=slave,env=env,cwd=ROOT,preexec_fn=setup,close_fds=True)
        os.close(slave);emit('terminal_start',label=label,pid=self.p.pid)
    def read(self, seconds=.3):
        if self.master is None: return terminal_text(bytes(self.data))
        end=time.monotonic()+seconds
        while time.monotonic()<end:
            ready,_,_=select.select([self.master],[],[],min(.1,max(0,end-time.monotonic())))
            if not ready: continue
            try: chunk=os.read(self.master,65536)
            except OSError: break
            if not chunk: break
            self.data.extend(chunk)
        return terminal_text(bytes(self.data))
    def send(self, text): os.write(self.master,text.encode())
    def prompt(self, text):
        self.send('\x1b[200~'+text+'\x1b[201~');self.read(.5);self.send('\r')
    def close(self):
        if self.closed:return
        self.closed=True
        # Drain and close the PTY before waiting: a native TUI may still be
        # writing its final terminal paint. Do not send Ctrl-C to a shared job.
        self.read(.15)
        os.close(self.master);self.master=None
        error=None
        try:
            # Closing the controlling terminal delivers SIGHUP. Give that
            # normal exit time to complete before signalling a process group
            # that may already have disappeared (sandbox killpg can return EPERM).
            try:self.p.wait(timeout=6)
            except subprocess.TimeoutExpired:
                try:os.killpg(self.p.pid,signal.SIGTERM)
                except ProcessLookupError:pass
                except PermissionError:
                    if self.p.poll() is None:self.p.terminate()
                try:self.p.wait(timeout=6)
                except subprocess.TimeoutExpired:
                    self.p.kill();self.p.wait(timeout=10)
        except Exception as e:
            error=e
        finally:
            (ROOT/'evidence'/(self.label+'.ansi')).write_bytes(self.data)
            (ROOT/'evidence'/(self.label+'.txt')).write_text(clean(terminal_text(self.data)))
            emit('terminal_closed',label=self.label,pid=self.p.pid,exit_code=self.p.returncode,cleanup_error=None if error is None else type(error).__name__)
        if error: raise error


class Core:
    def __init__(self, label, env, repo=None):
        self.label=label;self.data=ROOT/label
        self.log=(ROOT/'evidence'/(label+'.log')).open('w')
        args=[BINARY,'serve','--foreground','--no-open','--listen','127.0.0.1:0','--data-dir',str(self.data),'--repo',str(repo or ROOT/'repo'),'--claude-bin',CLI,'--codex-bin','/usr/bin/false','--test-loopback']
        self.p=subprocess.Popen(args,stdout=self.log,stderr=self.log,env=env,start_new_session=True)
        def owned_connection():
            try:return json.loads((self.data/'connection.json').read_text()).get('pid')==self.p.pid
            except (OSError,ValueError):return False
        wait_until(owned_connection,15)
        self.url=json.loads((self.data/'connection.json').read_text())['url']
        emit('core_start',label=label,pid=self.p.pid,url=self.url)
    def api(self, path, body=None, allow_error=False):
        request=urllib.request.Request(self.url+'/api/'+path,data=None if body is None else json.dumps(body).encode(),headers={'Content-Type':'application/json'})
        try:
            with urllib.request.urlopen(request,timeout=90) as r:return json.load(r)
        except urllib.error.HTTPError as e:
            result=json.load(e)
            if allow_error:return result
            raise RuntimeError(path+': '+str(result)) from None
    def close(self):
        if self.p.poll() is None:self.p.terminate()
        try:self.p.wait(timeout=40)
        except subprocess.TimeoutExpired:
            os.killpg(self.p.pid,signal.SIGTERM)
            self.p.wait(timeout=10)
        self.log.close();emit('core_closed',label=self.label,pid=self.p.pid,exit_code=self.p.returncode)


def environment(guard):
    env={k:v for k,v in os.environ.items() if not k.startswith(('ANTHROPIC_', 'CLAUDE_')) and k!='CLAUDECODE'}
    env.update({'CLAUDE_CONFIG_DIR':str(ROOT/'source-home'),'ANTHROPIC_BASE_URL':guard.url,'ANTHROPIC_AUTH_TOKEN':DUMMY_TOKEN,'ANTHROPIC_MODEL':MODEL,'CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC':'1','CLAUDE_CODE_DISABLE_AUTO_MEMORY':'1','CLAUDE_CODE_MAX_OUTPUT_TOKENS':'2048','CLAUDE_CODE_MAX_RETRIES':'0','DISABLE_AUTOUPDATER':'1','DISABLE_TELEMETRY':'1','TERM':'xterm-256color','NO_COLOR':'1'})
    for name in ('OPUS','SONNET','HAIKU','FABLE'):env['ANTHROPIC_DEFAULT_'+name+'_MODEL']=MODEL
    return env


def cli(env, args, cwd=None):
    p=subprocess.run([CLI]+args,env=env,cwd=cwd or ROOT/'repo',capture_output=True,text=True,timeout=50)
    if p.returncode:raise RuntimeError('Claude '+args[0]+' failed: '+clean(p.stderr[-1000:]))
    return p.stdout


def run(keep_seconds):
    guard=Guard();env=environment(guard);cores=[];terms=[];report={'scope':'one Mac, two real Cores, TLS membership gateway','model':MODEL}
    try:
        report['cli_version']=cli(env,['--version'],cwd=ROOT).strip()
        assert tuple(map(int, report['cli_version'].split()[0].split('.'))) >= (2, 1, 268),report['cli_version']
        repo=ROOT/'repo';repo.mkdir();home=ROOT/'source-home';home.mkdir()
        def git(*args):return subprocess.check_output(['git','-C',str(repo),*args],text=True).strip()
        git('init','-b','main');git('config','user.name','Team Cross Claude Fixture');git('config','user.email','fixture@example.invalid')
        (repo/'baseline.txt').write_text('committed\n');(repo/'.gitignore').write_text('ignored.txt\n')
        git('add','baseline.txt','.gitignore');git('commit','-m','Dedicated Claude native fixture')
        routing={k:v for k,v in env.items() if k.startswith('ANTHROPIC_')}
        settings=home/'settings.json';settings.write_text(json.dumps({'env':routing,'disableAllHooks':True,'enabledPlugins':{},'permissions':{'defaultMode':'manual'}}));settings.chmod(0o600)
        args=['--print','--output-format','json','--model',MODEL,'--effort','low','--settings',str(settings),'--setting-sources','','--strict-mcp-config','--mcp-config','{"mcpServers":{}}','--tools','','--permission-mode','manual','--no-chrome','Remember TCX_SOURCE. Reply exactly TCX_SOURCE_OK. Do not use tools.']
        seed=json.loads(cli(env,args));assert not seed.get('is_error'),clean(seed)
        source=seed['session_id'];report['source_id']=source
        source_path=next(home.glob('projects/*/'+source+'.jsonl'));source_hash=hashlib.sha256(source_path.read_bytes()).hexdigest()
        (repo/'baseline.txt').write_text('staged\n');git('add','baseline.txt');(repo/'baseline.txt').write_text('unstaged\n')
        (repo/'untracked.txt').write_text('untracked\n');(repo/'ignored.txt').write_text('ignored\n')
        before={'head':git('rev-parse','HEAD'),'branch':git('branch','--show-current'),'index':git('show',':baseline.txt'),'working':(repo/'baseline.txt').read_text()}
        a=Core('core-a',env);cores.append(a);benv=dict(env);benv['CLAUDE_CONFIG_DIR']=str(ROOT/'b-personal-empty')
        b=Core('core-b',benv);cores.append(b)
        guard.allow_generation=False;count=guard.generation_count()
        rows=a.api('sources?provider=claude')['data'];assert any(x['id']==source for x in rows)
        views={}
        for mode in ('existing','worktree'):
            body={'provider':'claude','sourceId':source,'workspaceMode':mode,'requestId':str(uuid.uuid4()),'title':'Claude 实测 · '+mode}
            preview=a.api('preview',body);body['previewHash']=preview['previewHash'];view=a.api('collaborations',body)
            assert view['sessionId']!=source and view['provider']=='claude',view
            native_history=list((ROOT/'core-a/collaborations'/view['id']/'claude-home/projects').glob('*/'+view['sessionId']+'.jsonl'))
            assert len(native_history)==1 and 'TCX_SOURCE_OK' in native_history[0].read_text(),'fork history not materialized before first input'
            assert a.api('collaborations',body)['sessionId']==view['sessionId'],'duplicate creation forked again'
            views[mode]=view;cwd=pathlib.Path(view['executionCwd'])
            assert (cwd/'baseline.txt').read_text()==('unstaged\n' if mode=='existing' else 'committed\n')
            if mode=='worktree':assert not (cwd/'untracked.txt').exists() and not (cwd/'ignored.txt').exists()
            history=a.api('collaborations/'+view['id']+'/context?kind=history')
            assert history['thread']['id']==view['sessionId'] and 'TCX_SOURCE_OK' in json.dumps(history)
            if mode=='worktree':
                a.api('collaborations/'+view['id']+'/action',{'action':'end'})
                wait_until(lambda:a.api('collaborations/'+view['id'])['runtimeState']=='released',30)
                restored=a.api('collaborations/'+view['id']+'/action',{'action':'start'})
                assert restored['sessionId']==view['sessionId'] and restored['nativeJobId']==view['nativeJobId']
                a.api('collaborations/'+view['id']+'/action',{'action':'end'})
        assert guard.generation_count()==count,'fork/read/restore triggered generation'
        assert before=={'head':git('rev-parse','HEAD'),'branch':git('branch','--show-current'),'index':git('show',':baseline.txt'),'working':(repo/'baseline.txt').read_text()}
        report['workspace_and_preinput_restore']=True;report['collaborations']={k:{'id':v['id'],'sessionId':v['sessionId'],'cwd':v['executionCwd'],'jobId':v['nativeJobId']} for k,v in views.items()}
        emit('forks_verified',generation_requests=0,source_preserved=True,same_id_restore=True)
        c=views['existing'];base='collaborations/'+c['id'];managed=ROOT/'core-a/collaborations'/c['id']/'claude-home'
        job=c['nativeJobId'];worker_env=dict(env);worker_env['CLAUDE_CONFIG_DIR']=str(managed)
        def job_info():return next(x for x in json.loads(cli(worker_env,['agents','--json','--all'])) if x['id']==job)
        before_job=job_info();report['worker_before']=before_job
        def action(value):return a.api(base+'/action',{'action':value,'epoch':a.api(base)['epoch']})
        shared=action('share');joined=b.api('join',{'invitation':shared['invitation']});assert joined['provider']=='claude';bbase='collaborations/'+joined['id']
        action('handoff');plan=b.api(bbase+'/open',{'client':'tui','launch':False});bt=Terminal('native-b',plan['command'],benv);terms.append(bt)
        wait_until(lambda:a.api(base)['clientState']=='session_ready',30,bt)
        assert 'TCX_SOURCE_OK' in bt.read(.3),'B did not render shared source'
        denied=a.api(base+'/rpc',{'method':'turn/start','params':{'input':[{'type':'text','text':'TCX_UNAUTHORIZED'}]},'requestId':'unauthorized-a'},True);assert 'error' in denied
        guard.allow_generation=True
        target=repo/'handoff-approved.txt';command="printf 'TCX_HANDOFF_OK\\n' > "+str(target)
        bt.prompt('Use Bash to run this exact command. Ask for permission if required. Use no other tools or commands. After success reply exactly TCX_HANDOFF_DONE.\n```sh\n'+command+'\n```')
        def pending(t,start=0):return 'doyouwanttoproceed?' in re.sub(r'\s+','',terminal_text(t.data[start:])).lower()
        wait_until(lambda:pending(bt),60,bt);assert not target.exists(),'executed before approval'
        def records():
            paths=list(managed.glob('projects/*/'+c['sessionId']+'.jsonl'))
            if not paths:return []
            result=[]
            for line in paths[0].read_text().splitlines():
                try:result.append(json.loads(line))
                except ValueError:pass
            return result
        def assistant_has(marker):return any(x.get('type')=='assistant' and marker in json.dumps(x.get('message',{})) for x in records())
        def tool_ids():return [v['id'] for x in records() for v in x.get('message',{}).get('content',[]) if isinstance(v,dict) and v.get('type')=='tool_use']
        first_ids=tool_ids();report['pending_tool_ids_persisted']=bool(first_ids)
        report['worker_waiting']=job_info();assert report['worker_waiting']['pid']==before_job['pid']
        action('reclaim');bt.send('\r');bt.read(1);assert not target.exists(),'stale B approval reached A'
        bt.close();aplan=a.api(base+'/open',{'client':'tui','launch':False});at=Terminal('native-a',aplan['command'],env);terms.append(at)
        wait_until(lambda:pending(at),25,at);assert tool_ids()==first_ids
        assert job_info()['pid']==before_job['pid'] and not target.exists()
        at.send('\r');wait_until(target.exists,35,at);wait_until(lambda:assistant_has('TCX_HANDOFF_DONE'),60,at)
        assert target.read_text()=='TCX_HANDOFF_OK\n'
        completed_ids=tool_ids();assert len(completed_ids)==1,completed_ids
        if first_ids:assert completed_ids==first_ids
        report['completed_approval_tool_id']=completed_ids[0]
        report['approval_handoff']=True;emit('approval_handoff_verified',same_worker=True,one_command_executed=True,old_b_approval_blocked=True)
        wait_until(lambda:not a.api(base)['busy'],15,at)
        before_effort=guard.generation_count();offset=len(at.data);at.prompt('/effort medium')
        wait_until(lambda:'changeeffortlevel?' in re.sub(r'\s+','',terminal_text(at.data[offset:])).lower(),10,at)
        at.send('\r');at.read(1);assert guard.generation_count()==before_effort
        wait_until(lambda:job_info().get('status') not in ('busy','waiting') and not job_info().get('waitingFor'),15,at)
        send={'method':'turn/start','params':{'input':[{'type':'text','text':'Reply exactly TCX_CONTROL_OK. Do not use tools.'}]},'requestId':'controller-send-once'}
        messages=[{'jsonrpc':'2.0','id':1,'method':'initialize','params':{}},{'jsonrpc':'2.0','id':2,'method':'tools/call','params':{'name':'send_input','arguments':{'id':c['id'],'text':send['params']['input'][0]['text'],'mode':'start','requestId':send['requestId']}}}]
        mcp=subprocess.run([BINARY,'mcp','--data-dir',str(a.data)],input=''.join(json.dumps(m)+'\n' for m in messages),env=env,capture_output=True,text=True,timeout=40)
        assert mcp.returncode==0,clean(mcp.stderr)
        response=next(json.loads(line) for line in mcp.stdout.splitlines() if json.loads(line).get('id')==2)
        assert not response.get('error') and not response.get('result',{}).get('isError'),clean(response)
        accepted=json.loads(response['result']['content'][0]['text']);assert accepted.get('accepted'),accepted
        report['actual_mcp_send']=True
        assert a.api(base+'/rpc',send)==accepted,'duplicate command not deduplicated'
        wait_until(lambda:assistant_has('TCX_CONTROL_OK'),60,at)
        confirmed=a.api(base+'/context?kind=history')['thread'];assert confirmed['reasoningEffort']=='medium',confirmed.get('reasoningEffort')
        count_input=sum(x.get('type')=='user' and 'TCX_CONTROL_OK' in json.dumps(x.get('message',{})) for x in records());assert count_input==1,count_input
        wait_until(lambda:not a.api(base)['busy'],15,at)
        started=repo/'interrupt-started.txt';finished=repo/'interrupt-finished.txt';offset=len(at.data)
        stop_command="printf 'started\\n' > "+str(started)+"; sleep 20; printf 'finished\\n' > "+str(finished)
        at.prompt('Use Bash to run this exact command in the foreground. Ask for permission if required. Use no other tools or alternative commands.\n```sh\n'+stop_command+'\n```')
        wait_until(lambda:pending(at,offset),60,at);at.send('\r');wait_until(started.exists,25,at);command_started=time.monotonic();at.send('\x1b');at.read(2)
        report['worker_after_interrupt']=job_info();emit('interrupt_status',worker=report['worker_after_interrupt'])
        wait_until(lambda:not a.api(base)['busy'],30,at)
        while time.monotonic()<command_started+21:at.read(.2)
        assert not finished.exists(),'native interrupt did not stop the command before its final write'
        report['native_interrupt']=True;report['control_send_deduplicated']=True
        report['worker_after']=job_info();assert report['worker_after']['pid']==before_job['pid'],'TUI/controller replaced worker'
        assert not list((ROOT/'core-b/clients').glob('**/projects/*/*.jsonl')),'B persisted provider history'
        assert source_hash==hashlib.sha256(source_path.read_bytes()).hexdigest(),'source history modified'
        assert not any('TCX_UNAUTHORIZED' in json.dumps(x.get('message',{})) for x in records())
        at.close();wait_until(lambda:not a.api(base)['connected'],10)
        action('end');wait_until(lambda:a.api(base)['runtimeState']=='released',35)
        assert 'TCX_CONTROL_OK' in json.dumps(a.api(base+'/context?kind=history'))
        assert b.api(bbase)['state'] in ('ended','expired')
        guard.allow_generation=False;before_restore=guard.generation_count();restored=action('start')
        assert restored['sessionId']==c['sessionId'] and restored['nativeJobId']==job
        assert guard.generation_count()==before_restore
        restored_plan=a.api(base+'/open',{'client':'tui','launch':False});rt=Terminal('native-restored',restored_plan['command'],env);terms.append(rt)
        wait_until(lambda:a.api(base)['clientState']=='session_ready',30,rt)
        restored_screen=re.sub(r'\s+','',rt.read(1)).lower()
        assert 'medium·/effort' in restored_screen or 'withmediumeffort' in restored_screen,'effort selection was not restored'
        saved_flags=json.loads((managed/'jobs'/job/'state.json').read_text())['respawnFlags']
        assert saved_flags[saved_flags.index('--effort')+1]=='medium'
        rt.close();report['effort_selection_restored']=True
        report['restore_after_input']=True;report['same_worker']=True;report['source_unchanged']=True;report['b_history_files']=0
        # Verify actual execution in the selected worktree, not just its metadata.
        wc=views['worktree'];wbase='collaborations/'+wc['id'];a.api(wbase+'/action',{'action':'start'})
        wplan=a.api(wbase+'/open',{'client':'tui','launch':False});wt=Terminal('native-worktree',wplan['command'],env);terms.append(wt)
        wait_until(lambda:a.api(wbase)['clientState']=='session_ready',30,wt)
        wtarget=pathlib.Path(wc['executionCwd'])/'worktree-proof.txt';guard.allow_generation=True
        wt.prompt("Use Bash to run this exact command:\n```sh\nprintf 'TCX_WORKTREE_OK\\n' > worktree-proof.txt\n```\nAsk for permission if required. Use no other tools. Then reply exactly TCX_WORKTREE_DONE.")
        wait_until(lambda:pending(wt),60,wt);assert not wtarget.exists();wt.send('\r')
        wait_until(wtarget.exists,30,wt);wait_until(lambda:not a.api(wbase)['busy'],60,wt)
        assert wtarget.read_text()=='TCX_WORKTREE_OK\n' and not (repo/'worktree-proof.txt').exists()
        wt.close();guard.allow_generation=False;report['worktree_execution']=True
        emit('worktree_execution_verified',isolated_from_source=True)

        # Simulate an abrupt A Core exit after a completed turn; the worker survives.
        action('start');live_pid=job_info()['pid'];a.p.kill();a.p.wait(timeout=5)
        a=Core('core-a',env);cores.append(a);recovered=action('start')
        assert recovered['sessionId']==c['sessionId'] and job_info()['pid']==live_pid
        crash_plan=a.api(base+'/open',{'client':'tui','launch':False});ct=Terminal('native-crash-recovered',crash_plan['command'],env);terms.append(ct)
        wait_until(lambda:a.api(base)['clientState']=='session_ready',30,ct);ct.close()
        report['core_crash_same_worker']=True
        report['ui_url']=a.url;report['ui_detail']=a.url+'/#/'+base
        emit('acceptance_verified',url=a.url,detail=report['ui_detail'],root=str(ROOT))
        if keep_seconds:
            emit('preview_available',seconds=keep_seconds,stop_file=str(ROOT/'finish-preview'))
            end=time.monotonic()+keep_seconds
            while time.monotonic()<end and not (ROOT/'finish-preview').exists():time.sleep(.5)
        action('end')
    except Exception as e:
        if 'job_info' in locals():
            try:report['worker_at_failure']=job_info()
            except Exception:pass
        report['error']=clean(type(e).__name__+': '+str(e));report['traceback']=clean(traceback.format_exc());emit('acceptance_failed',error=report['error'])
    finally:
        cleanup_errors=[]
        for resource in list(reversed(terms))+list(reversed(cores)):
            try:resource.close()
            except Exception as e:cleanup_errors.append(type(e).__name__+': '+str(e))
        if cleanup_errors:
            report['cleanup_errors']=clean(cleanup_errors)
            report.setdefault('error','Test resource cleanup failed')
        report['generation_requests']=guard.generation_count();report['models_observed']=sorted({x['model'] for x in guard.entries if x.get('model')});report['requests']=guard.entries
        guard.close();(ROOT/'evidence/summary.json').write_text(json.dumps(clean(report),ensure_ascii=False,indent=2))
        emit('complete',error=report.get('error'),generations=report['generation_requests'],models=report['models_observed'])
    return 1 if report.get('error') else 0


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--fixture-dir',required=True,type=pathlib.Path)
    parser.add_argument('--teamcross-bin',required=True)
    parser.add_argument('--claude-bin',required=True)
    parser.add_argument('--source-settings',type=pathlib.Path,default=pathlib.Path.home()/'.claude/settings.json')
    parser.add_argument('--keep-preview-seconds',type=int,default=0)
    args=parser.parse_args();ROOT=args.fixture_dir.resolve();CLI=str(pathlib.Path(args.claude_bin).resolve());BINARY=str(pathlib.Path(args.teamcross_bin).resolve())
    if ROOT.exists() and any(ROOT.iterdir()):parser.error('Choose a new, empty dedicated fixture directory')
    ROOT.mkdir(parents=True,exist_ok=True);ROOT.chmod(0o700);(ROOT/'evidence').mkdir()
    CREDS=json.loads(args.source_settings.read_text())['env'];TOKEN=CREDS.get('ANTHROPIC_AUTH_TOKEN') or CREDS.get('ANTHROPIC_API_KEY')
    if not TOKEN:parser.error('CliProxyAPI authentication is missing')
    raise SystemExit(run(max(0,min(args.keep_preview_seconds,1800))))
