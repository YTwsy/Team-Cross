from probe_common import *
import fcntl
import pty
import re
import select
import struct
import termios

def terminal_text(data):
    text=data.decode('utf-8','replace')
    text=re.sub(r'\x1b\][^\x07]*(?:\x07|\x1b\\)','',text)
    text=re.sub(r'\x1b\[[0-?]*[ -/]*[@-~]','',text)
    return text.replace('\r','\n').replace('\x1b','')

def bootstrap(config):
    d=ROOT/config;d.mkdir(exist_ok=True)
    state=d/'.claude.json'
    x=json.loads(state.read_text()) if state.is_file() else {}
    x.update({'hasCompletedOnboarding':True,'lastOnboardingVersion':'2.1.268','theme':'light','autoUpdates':False,'hasSeenTasksHint':True})
    x.setdefault('projects',{})[str(ROOT/'repo')]={'hasTrustDialogAccepted':True,'allowedTools':[],'hasCompletedProjectOnboarding':True,'projectOnboardingSeenCount':1}
    state.write_text(json.dumps(x,indent=2))

class Terminal:
    def __init__(self,g,label,args,config='client-config',cwd=None):
        bootstrap(config)
        self.label=label;self.data=bytearray();self.closed=False
        self.master,slave=pty.openpty()
        fcntl.ioctl(slave,termios.TIOCSWINSZ,struct.pack('HHHH',36,120,0,0))
        def setup():
            os.setsid();fcntl.ioctl(slave,termios.TIOCSCTTY,0)
        self.p=subprocess.Popen([CLI]+args,stdin=slave,stdout=slave,stderr=slave,cwd=str(cwd or ROOT/'repo'),env=make_env(g,config),preexec_fn=setup,close_fds=True)
        os.close(slave)
        emit('terminal_start',label=label,pid=self.p.pid)
    def read(self,seconds=1):
        end=time.monotonic()+seconds
        while time.monotonic()<end:
            ready,_,_=select.select([self.master],[],[],min(.15,end-time.monotonic()))
            if not ready: continue
            try: data=os.read(self.master,65536)
            except OSError: break
            if not data:break
            self.data.extend(data)
        return terminal_text(bytes(self.data))
    def send(self,text):
        os.write(self.master,text.encode() if isinstance(text,str) else text)
    def close(self):
        if self.closed:return
        self.closed=True
        if self.p.poll() is None:
            try:
                self.send(b'\x03');self.read(.3)
                if self.p.poll() is None:self.send(b'\x03');self.read(.5)
            except OSError:pass
        if self.p.poll() is None:
            try:os.killpg(self.p.pid,signal.SIGTERM)
            except ProcessLookupError:pass
            self.read(.5)
            os.close(self.master);self.master=None
            try:self.p.wait(timeout=4)
            except subprocess.TimeoutExpired:
                os.killpg(self.p.pid,signal.SIGKILL)
                try:self.p.wait(timeout=3)
                except subprocess.TimeoutExpired:emit('terminal_reap_pending',pid=self.p.pid)
        if self.master is not None:self.read(.1)
        (ROOT/'evidence'/(self.label+'.ansi')).write_bytes(bytes(self.data))
        (ROOT/'evidence'/(self.label+'.txt')).write_text(clean(terminal_text(bytes(self.data))))
        if self.master is not None:os.close(self.master)
        emit('terminal_closed',label=self.label,pid=self.p.pid,exit_code=self.p.returncode)

if __name__=='__main__':
    g=Guard();g.allow_generation=False
    report=[]
    class Dummy(http.server.BaseHTTPRequestHandler):
        def log_message(self,*a):pass
        def do_GET(self):
            report.append({'incoming_path':self.path,'upgrade':self.headers.get('Upgrade')})
            self.send_error(400,'Fixture endpoint: no upstream attached')
    server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Dummy)
    threading.Thread(target=server.serve_forever,daemon=True).start()
    cases=[
      ('sdk-url',['--sdk-url',f'ws://127.0.0.1:{server.server_port}','--input-format','stream-json','--output-format','stream-json']),
      ('cc-url',[f'cc://127.0.0.1:{server.server_port}']),
      ('cc-unix-url',['cc+unix://'+str(ROOT/'absent.sock')]),
    ]
    try:
        for label,extra in cases:
            t=Terminal(g,label,base_args()+extra)
            try:
                output=t.read(5)
                report.append({'case':label,'exit_code':t.p.poll(),'output':clean(output[-5500:])})
                emit('terminal_probe',case=label,exit_code=t.p.poll(),output=output[-1800:])
            finally:t.close()
    finally:
        server.shutdown();server.server_close();g.close()
        (ROOT/'evidence'/'terminal-endpoints.json').write_text(json.dumps(report,ensure_ascii=False,indent=2))
