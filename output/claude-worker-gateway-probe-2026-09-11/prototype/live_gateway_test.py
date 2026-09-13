from pty_probe import *
from controlled_gateway import Gateway
import traceback

NODE='/Users/wsy/.nvm/versions/node/v24.18.0/bin/node'
SDK='/private/tmp/teamcross-claude-probe-20260911-p6yo8o8_/sdk/package/sdk.mjs'
g=Guard();env=make_env(g);job=None;gw=None;b=None;a=None;seed=None
report={'cli_version':'2.1.268','model':MODEL,'scope':'single Mac, isolated background worker, loopback TCP gateway'}
def run(args,timeout=25):
    p=subprocess.run([CLI]+args,env=env,cwd=ROOT/'repo',capture_output=True,text=True,timeout=timeout)
    return {'code':p.returncode,'stdout':clean(p.stdout),'stderr':clean(p.stderr)}
def records():
    out=[]
    for path in (ROOT/'config/projects').glob('**/*.jsonl'):
        if path.stem!=report.get('worker_session_id'):continue
        for line in path.read_text().splitlines():
            try:out.append(json.loads(line))
            except ValueError:pass
    return out
def content_has(marker,role=None):
    return any((role is None or x.get('type')==role) and marker in json.dumps(x.get('message',{})) for x in records())
def wait_until(check,seconds=45,terminal=None):
    end=time.monotonic()+seconds
    while time.monotonic()<end:
        if terminal:terminal.read(.25)
        else:time.sleep(.15)
        result=check()
        if result:return result
    raise TimeoutError('Waiting for '+getattr(check,'__name__','condition'))
def prompt(t,text):
    t.send('\x1b[200~'+text+'\x1b[201~');t.read(.6);t.send('\r')
def save():
    if gw:report['gateway_events']=gw.logs
    report['model_requests']=g.entries
    (ROOT/'evidence/live-gateway-summary.json').write_text(json.dumps(clean(report),ensure_ascii=False,indent=2))
try:
    bootstrap('config')
    seed=Runtime(g,'gateway-seed',['--system-prompt','Follow the exact user request. This is an isolated integration fixture.'])
    seed.initialize();seed.user('Remember TCX_WORKER_SEED. Reply exactly TCX_SEED_OK. Do not use tools.')
    result=seed.wait(lambda x:x.get('type')=='result',60)
    assert not result.get('is_error'),result
    source=result['session_id'];report['source_id']=source;seed.close();seed=None
    script=ROOT/'gateway-offline-fork.mjs'
    script.write_text('''import fs from 'node:fs';
import crypto from 'node:crypto';
const root=process.argv[2], source=process.argv[3];
process.env.CLAUDE_CONFIG_DIR=root+'/config';
let fetches=0;globalThis.fetch=()=>{fetches++;throw new Error('Offline fork may not fetch');};
const {forkSession,getSessionMessages}=await import('''+json.dumps(SDK)+''');
const dir=root+'/repo';
const sourcePath=root+'/config/projects/'+dir.replace(/[^a-zA-Z0-9]/g,'-')+'/'+source+'.jsonl';
const hash=()=>crypto.createHash('sha256').update(fs.readFileSync(sourcePath)).digest('hex');
const before=hash();const fork=await forkSession(source,{dir,title:'TCX controlled worker fork'});
const a=await getSessionMessages(source,{dir}),b=await getSessionMessages(fork.sessionId,{dir});
console.log(JSON.stringify({sourceId:source,forkId:fork.sessionId,sourceHash:before,sourceUnchanged:before===hash(),sourcePath,fetches,contentPreserved:JSON.stringify(a.map(x=>x.message))===JSON.stringify(b.map(x=>x.message))}));
''')
    forkproc=subprocess.run([NODE,str(script),str(ROOT),source],env=env,capture_output=True,text=True,timeout=20)
    assert forkproc.returncode==0,clean(forkproc.stderr)
    report['fork']=json.loads(forkproc.stdout);fid=report['fork']['forkId'];assert report['fork']['sourceUnchanged'] and report['fork']['contentPreserved']
    report['launch']=run(base_args()+['--resume',fid,'--bg','--name','TCX controlled single worker'])
    m=re.search(r'backgrounded · ([0-9a-f]+)',report['launch']['stdout']);assert m,report['launch'];job=m.group(1);report['job_id']=job
    rows=json.loads(run(['agents','--json','--all'])['stdout']);row=next(x for x in rows if x.get('id')==job);report['initial_worker']=row;report['worker_session_id']=row.get('sessionId') or fid
    status=run(['daemon','status']);report['daemon_status']=status
    found=re.search(r'(/(?:private/)?tmp/cc-daemon-[^\s]+)',status['stdout']);assert found,status
    gw=Gateway(pathlib.Path(found.group(1).rstrip('/'))/'control.sock',job)
    b=Terminal(g,'gateway-native-b',['attach',job],config='config');screen=b.read(4)
    assert gw.auth is not None,'Native attach did not authenticate'
    report['initial_b_screen']=screen[-4500:]
    rows=json.loads(run(['agents','--json','--all'])['stdout']);row=next(x for x in rows if x.get('id')==job);report['worker_before']=row
    report['worker_session_id']=row.get('sessionId') or fid
    emit('gateway_ready',job=job,worker=row,source_id=source,fork_id=fid)
    # Inject one lost packet before it reaches the worker. Native attach reconnects without re-sending it.
    gw.drop_next_b=True;b.send('\x1b[200~Reply exactly TCX_MUST_NOT_REPLAY.\x1b[201~\r');screen=b.read(5)
    assert any(x['event']=='fault_drop_before_forward' for x in gw.logs),'Fault injection did not intercept input'
    report['dropped_input_replayed']=content_has('TCX_MUST_NOT_REPLAY','user');assert not report['dropped_input_replayed']
    report['b_reconnected']=sum(x.get('op')=='attach' and x.get('role')=='B' for x in gw.logs)>=2
    emit('disconnect_probe',reconnected=report['b_reconnected'],replayed=report['dropped_input_replayed'],b_exit=b.p.poll())
    assert b.p.poll() is None,'Native client exited instead of reconnecting'
    target=ROOT/('repo/handoff-approved-'+job+'.txt');assert not target.exists()
    command="printf 'TCX_HANDOFF_OK\\n' > "+str(target)
    prompt(b,'Use Bash to run the exact command below. Ask for permission when required. After it succeeds reply exactly TCX_HANDOFF_DONE. Use no other tools and no alternative command.\n```sh\n'+command+'\n```')
    def pending_b():
        tail=b.read(.1)[len(screen):]
        compact=re.sub(r'\s+','',tail).lower()
        return 'doyouwanttoproceed?' in compact or 'allowthis' in compact
    wait_until(pending_b,60,b)
    report['pending_b_screen']=b.read(.2)[-6000:]
    assert not target.exists(),'Command executed before approval'
    wait_until(lambda:any(isinstance(v,dict) and v.get('type')=='tool_use' for x in records() for v in x.get('message',{}).get('content',[])),15,b)
    report['approval_tool_calls_before']= [v for x in records() for v in x.get('message',{}).get('content',[]) if isinstance(v,dict) and v.get('type')=='tool_use']
    report['a_send_while_b_owns']=gw.request('reply',text='TCX_UNAUTHORIZED_A_MESSAGE')
    assert report['a_send_while_b_owns'].get('code')=='ETCXOWNER'
    gw.reclaim();b.send('\r');b.read(.9)
    report['stale_b_blocked']=any(x['event']=='blocked_input' and x.get('role')=='B' for x in gw.logs)
    report['file_after_stale_b']=target.exists();assert report['stale_b_blocked'] and not target.exists()
    gw.disconnect_b();b.read(1);b.close();b=None
    a=gw.attach_a();report['a_attach_ack']=a.ack;screen=a.read(2)
    report['pending_a_screen']=screen[-6000:]
    compact=re.sub(r'\s+','',screen).lower();assert 'doyouwanttoproceed?' in compact or 'allowthis' in compact,screen[-2000:]
    assert not target.exists()
    emit('approval_handoff_pending',job=job,old_b_blocked=True,same_pending_dialog_visible_to_a=True)
    a.send('\r');wait_until(lambda:target.exists(),30,a)
    wait_until(lambda:content_has('TCX_HANDOFF_DONE','assistant'),50,a)
    report['approved_file']=target.read_text();assert report['approved_file']=='TCX_HANDOFF_OK\n'
    report['approval_tool_calls_after']=[v for x in records() for v in x.get('message',{}).get('content',[]) if isinstance(v,dict) and v.get('type')=='tool_use']
    before=[x['id'] for x in report['approval_tool_calls_before']];after=[x['id'] for x in report['approval_tool_calls_after']]
    report['same_tool_call_completed']=before==after and len(before)==1;assert report['same_tool_call_completed'],(before,after)
    emit('approval_handoff_passed',same_tool_call=True,content=target.read_text())
    report['a_structured_send']=gw.request('reply',text='Reply exactly TCX_A_CONTROL_OK. Do not use tools.')
    assert report['a_structured_send'].get('ok'),report['a_structured_send']
    wait_until(lambda:content_has('TCX_A_CONTROL_OK','assistant'),60,a)
    report['structured_send_completed']=True
    # Verify native interrupt against a real running Bash child in the dedicated fixture.
    started=ROOT/('repo/interrupt-started-'+job+'.txt');finished=ROOT/('repo/interrupt-finished-'+job+'.txt')
    stopcommand="printf 'started\\n' > "+str(started)+"; sleep 20; printf 'finished\\n' > "+str(finished)
    previous=len(a.data)
    report['a_interrupt_prompt']=gw.request('reply',text='Use Bash to run the exact command below, asking permission if required. Use no other tools. Do not run in the background.\n```sh\n'+stopcommand+'\n```')
    def pending_a():
        compact=re.sub(r'\s+','',terminal_text(bytes(a.data[previous:]))).lower()
        return 'doyouwanttoproceed?' in compact or 'allowthis' in compact
    wait_until(pending_a,60,a);a.send('\r');wait_until(lambda:started.exists(),20,a)
    a.send('\x1b');a.read(1.5);a.send('\x1b');a.read(1)
    report['interrupt_screen']=a.read(.2)[-5000:]
    startwait=time.monotonic()
    while time.monotonic()-startwait<21:a.read(.5)
    report['interrupted_child_finished']=finished.exists();assert not finished.exists(),'Interrupted Bash command reached its final write'
    report['worker_after']=next(x for x in json.loads(run(['agents','--json','--all'])['stdout']) if x.get('id')==job)
    report['same_worker_pid']=report['worker_before'].get('pid')==report['worker_after'].get('pid') and report['worker_before'].get('pid') is not None
    report['source_unchanged_at_end']=hashlib.sha256(pathlib.Path(report['fork']['sourcePath']).read_bytes()).hexdigest()==report['fork']['sourceHash']
    assert report['same_worker_pid'] and report['source_unchanged_at_end']
    report['unauthorized_a_reached_transcript']=content_has('TCX_UNAUTHORIZED_A_MESSAGE','user');assert not report['unauthorized_a_reached_transcript']
    emit('gateway_checks_passed',same_worker_pid=report['same_worker_pid'],structured_send=True,interrupt_stopped_child=not finished.exists())
except Exception as e:
    report['error']=type(e).__name__+': '+str(e);report['traceback']=traceback.format_exc();emit('gateway_error',error=report['error'])
finally:
    if seed:seed.close()
    if a:
        a.read(.2);(ROOT/'evidence/gateway-controller-a.txt').write_text(clean(terminal_text(bytes(a.data))));a.close()
    if b:b.close()
    if gw:report['gateway_events']=list(gw.logs);gw.close()
    if job:
        try:report['stop']=run(['stop',job])
        except Exception as e:report['stop_error']=str(e)
    try:report['daemon_stop']=run(['daemon','stop','--any'])
    except Exception as e:report['daemon_stop_error']=str(e)
    report['generation_requests']=g.generation_count();report['models_observed']=sorted({x['model'] for x in g.entries if x.get('model')});g.close();save()
    emit('gateway_complete',error=report.get('error'),generations=report['generation_requests'],models=report['models_observed'])
