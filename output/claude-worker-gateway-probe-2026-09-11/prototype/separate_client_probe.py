from pty_probe import *
from controlled_gateway import Gateway
import socket

g=Guard();g.allow_generation=False;gateway=None;bridge=None;a=None;b=None;job=None
report={'model':MODEL,'generation_allowed':False,'scope':'same Mac, distinct Claude config and cwd, B daemon facade over loopback TCP'}
class ClientBridge:
    def __init__(self,path,port):
        self.path=pathlib.Path(path);self.old=self.path.with_name('tcx-b-backend.sock');assert not self.old.exists()
        self.path.parent.mkdir(parents=True,exist_ok=True)
        if self.path.exists():
            assert self.path.is_socket();self.path.rename(self.old)
        self.listener=socket.socket(socket.AF_UNIX);self.listener.bind(str(self.path));self.listener.listen(8);self.closed=False;self.connections=[];self.port=port
        threading.Thread(target=self.accept,daemon=True).start()
    def accept(self):
        while not self.closed:
            try:c,_=self.listener.accept()
            except OSError:return
            threading.Thread(target=self.handle,args=(c,),daemon=True).start()
    def handle(self,c):
        up=socket.socket()
        try:
            up.connect(('127.0.0.1',self.port));self.connections.append((c,up))
            while not self.closed:
                ready,_,_=select.select([c,up],[],[],.2)
                for src in ready:
                    buf=src.recv(65536)
                    if not buf:return
                    (up if src is c else c).sendall(buf)
        except OSError:pass
        finally:c.close();up.close()
    def close(self):
        self.closed=True;self.listener.close()
        for pair in self.connections:
            for s in pair:
                try:s.shutdown(socket.SHUT_RDWR);s.close()
                except OSError:pass
        if self.path.exists():self.path.unlink()
        if self.old.exists():self.old.rename(self.path)
def run(args,config='config',cwd='repo'):
    p=subprocess.run([CLI]+args,env=make_env(g,config),cwd=ROOT/cwd,capture_output=True,text=True,timeout=25)
    return {'code':p.returncode,'stdout':clean(p.stdout),'stderr':clean(p.stderr)}
def endpoint(config):
    status=run(['daemon','status'],config);report[config+'_daemon_status']=status
    m=re.search(r'(/(?:private/)?tmp/cc-daemon-[^\s]+)',status['stdout']);assert m,status
    return pathlib.Path(m.group(1).rstrip('/'))/'control.sock'
try:
    (ROOT/'b-client').mkdir(exist_ok=True);bootstrap('config-b')
    success=json.loads((ROOT/'evidence/live-gateway-summary.json').read_text());sid=success['worker_session_id']
    report['launch']=run(base_args()+['--resume',sid,'--bg','--name','TCX independent native client'])
    m=re.search(r'backgrounded · ([0-9a-f]+)',report['launch']['stdout']);assert m,report['launch'];job=m.group(1)
    gateway=Gateway(endpoint('config'),job)
    # Capture the host's legitimate daemon authentication in memory through its native client.
    a=Terminal(g,'separate-client-host-bootstrap',['attach',job],config='config');a.read(3);assert gateway.auth
    a.send('\x1a');a.read(.8);a.close();a=None;gateway.translate_auth=True
    # B has a different, empty config directory and no copy of A's transcript or API credential.
    report['b_initial_agents']=run(['agents','--json','--all'],'config-b','b-client')
    bendpoint=endpoint('config-b');report['separate_socket_dirs']=bendpoint.parent!=gateway.original.parent;assert report['separate_socket_dirs']
    report['b_transcripts_before']=len(list((ROOT/'config-b/projects').glob('**/*.jsonl')))
    bridge=ClientBridge(bendpoint,gateway.port)
    (ROOT/'config-b/jobs'/job).mkdir(parents=True,exist_ok=True)
    report['b_job_registration']='empty local jobs/<short-id> directory; no copied state or transcript'
    b=Terminal(g,'separate-client-native-b',['attach',job],config='config-b',cwd=ROOT/'b-client');screen=b.read(5)
    report['b_screen']=screen[-6000:];compact=re.sub(r'\s+','',screen)
    report['a_history_visible_to_b']='TCX_A_CONTROL_OK' in compact and 'TCX_HANDOFF_DONE' in compact
    report['b_attach_alive']=b.p.poll() is None
    report['host_auth_supplied_by_gateway']=any(x['event']=='backend_auth_translation' for x in gateway.logs)
    report['b_native_auth_present']=any(x.get('client_auth_present') for x in gateway.logs if x['event']=='backend_auth_translation')
    report['b_transcripts_after']=len(list((ROOT/'config-b/projects').glob('**/*.jsonl')))
    report['worker']=next(x for x in json.loads(run(['agents','--json','--all'])['stdout']) if x.get('id')==job)
    assert report['a_history_visible_to_b'] and report['b_attach_alive'],screen[-2500:]
    assert report['b_transcripts_before']==report['b_transcripts_after']==0
    assert report['host_auth_supplied_by_gateway']
    emit('separate_client_passed',history_visible=True,b_transcripts=0,host_auth_supplied_by_gateway=True,worker=report['worker'])
    b.send('\x1a');b.read(.8);b.close();b=None
except Exception as e:
    report['error']=type(e).__name__+': '+str(e);emit('separate_client_error',error=report['error'])
finally:
    if b:b.close()
    if a:a.close()
    if bridge:bridge.close()
    if gateway:report['gateway_events']=list(gateway.logs);gateway.close()
    if job:report['stop']=run(['stop',job])
    report['b_daemon_stop']=run(['daemon','stop','--any'],'config-b','b-client')
    report['a_daemon_stop']=run(['daemon','stop','--any'])
    report['generations']=g.generation_count();report['model_requests']=g.entries;g.close()
    (ROOT/'evidence/separate-client-summary.json').write_text(json.dumps(clean(report),ensure_ascii=False,indent=2))
    emit('separate_client_complete',error=report.get('error'),generations=report['generations'])
