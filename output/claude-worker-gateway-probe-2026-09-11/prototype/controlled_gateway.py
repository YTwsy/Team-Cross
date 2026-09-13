from probe_common import *
import socket
import select

class Gateway:
    """A version-specific experimental gateway, not a Team Cross production adapter."""
    READ={'ping','nudge','has','list','status','peek','logs'}
    WRITE={'reply','resize'}
    def __init__(self,original,job):
        self.original=pathlib.Path(original);self.backend=self.original.with_name('tcx-backend.sock');self.job=job
        assert self.original.is_socket() and not self.backend.exists()
        self.original.rename(self.backend)
        self.a_path=ROOT/'a-control.sock';assert not self.a_path.exists()
        self.writer='B';self.epoch=1;self.closed=False;self.reject_b_attach=False;self.drop_next_b=False
        self.auth=None;self.translate_auth=False;self.caps=None;self.logs=[];self.connections=[];self.lock=threading.RLock()
        self.tcp=socket.socket();self.tcp.bind(('127.0.0.1',0));self.tcp.listen(8)
        self.a=socket.socket(socket.AF_UNIX);self.a.bind(str(self.a_path));self.a.listen(8)
        self.local_b=socket.socket(socket.AF_UNIX);self.local_b.bind(str(self.original));self.local_b.listen(8)
        self.port=self.tcp.getsockname()[1]
        for listener,role in [(self.tcp,'B'),(self.a,'A')]:threading.Thread(target=self.accept,args=(listener,role),daemon=True).start()
        threading.Thread(target=self.b_bridge,daemon=True).start()
    def log(self,event,**fields):
        with self.lock:self.logs.append({'time':time.time(),'event':event,**fields})
    def b_bridge(self):
        while not self.closed:
            try:c,_=self.local_b.accept()
            except OSError:return
            def copy(c):
                t=socket.socket()
                try:
                    t.connect(('127.0.0.1',self.port));self.connections.append({'role':'bridge','client':c,'upstream':t})
                    while not self.closed:
                        ready,_,_=select.select([c,t],[],[],.2)
                        for s in ready:
                            d=s.recv(65536)
                            if not d:return
                            (t if s is c else c).sendall(d)
                except OSError:pass
                finally:c.close();t.close()
            threading.Thread(target=copy,args=(c,),daemon=True).start()
    def accept(self,l,role):
        while not self.closed:
            try:c,_=l.accept()
            except OSError:return
            threading.Thread(target=self.handle,args=(c,role),daemon=True).start()
    def deny(self,c,op,reason):
        self.log('rejected',op=op,reason=reason)
        c.sendall(json.dumps({'ok':False,'op':op,'code':'ETCXOWNER','error':reason}).encode()+b'\n')
    def handle(self,c,role):
        up=None
        try:
            data=b''
            while b'\n' not in data:
                b=c.recv(8192)
                if not b:return
                data+=b
                if len(data)>65536:return
            line,rest=data.split(b'\n',1);req=json.loads(line);op=req.get('op')
            with self.lock:
                lease=self.epoch
                if req.get('short') not in (None,self.job):return self.deny(c,op,'wrong test session')
                if op=='attach' and role=='B' and self.reject_b_attach:return self.deny(c,op,'input ownership was reclaimed')
                if op not in self.READ|self.WRITE|{'attach'}:return self.deny(c,op,'operation outside prototype scope')
                if op in self.WRITE and role!=self.writer:return self.deny(c,op,'caller is not current input owner')
                if self.translate_auth and op in self.WRITE|{'attach'}:
                    self.log('backend_auth_translation',role=role,client_auth_present=bool(req.get('auth')),client_uses_different_token=req.get('auth')!=self.auth)
                    req['auth']=self.auth;line=json.dumps(req).encode()
                elif req.get('auth') and not self.translate_auth:self.auth=req['auth']
                if op=='attach' and req.get('caps'):self.caps=req['caps']
                self.log('control',role=role,epoch=lease,op=op,attach_id=req.get('attachId'))
            up=socket.socket(socket.AF_UNIX);up.connect(str(self.backend));connection={'role':role,'epoch':lease,'op':op,'client':c,'upstream':up};self.connections.append(connection)
            with self.lock:
                if op in self.WRITE and (role!=self.writer or lease!=self.epoch):return self.deny(c,op,'input ownership changed before forwarding')
                up.sendall(line+b'\n')
            if rest:
                with self.lock:
                    if role==self.writer and lease==self.epoch:up.sendall(rest)
                    else:self.log('blocked_input',role=role,epoch=lease,bytes=len(rest))
            while not self.closed:
                ready,_,_=select.select([c,up],[],[],.2)
                for src in ready:
                    b=src.recv(65536)
                    if not b:return
                    if src is up:c.sendall(b);continue
                    with self.lock:
                        if role=='B' and self.drop_next_b:
                            self.drop_next_b=False;self.log('fault_drop_before_forward',bytes=len(b),epoch=lease);return
                        if role!=self.writer or lease!=self.epoch:
                            self.log('blocked_input',role=role,epoch=lease,current_epoch=self.epoch,bytes=len(b));continue
                        up.sendall(b);self.log('forwarded_input',role=role,epoch=lease,bytes=len(b))
        except (BrokenPipeError,ConnectionResetError,OSError):pass
        except Exception as e:self.log('proxy_error',error=type(e).__name__+': '+str(e))
        finally:c.close();up.close() if up else None
    def reclaim(self):
        with self.lock:self.writer='A';self.epoch+=1;self.log('reclaim',writer=self.writer,epoch=self.epoch)
    def disconnect_b(self):
        with self.lock:
            self.reject_b_attach=True
            for x in self.connections:
                if x['role']=='B' and x.get('op')=='attach':
                    for s in [x['client'],x['upstream']]:
                        try:s.shutdown(socket.SHUT_RDWR)
                        except OSError:pass
            self.log('b_disconnected',epoch=self.epoch)
    def request(self,op,**fields):
        s=socket.socket(socket.AF_UNIX);s.settimeout(15);s.connect(str(self.a_path))
        try:
            s.sendall(json.dumps({'proto':1,'op':op,'short':self.job,'auth':self.auth,**fields}).encode()+b'\n')
            d=b''
            while b'\n' not in d:
                chunk=s.recv(16384)
                if not chunk:break
                d+=chunk
            return json.loads(d.split(b'\n',1)[0])
        finally:s.close()
    def attach_a(self):return RawTerminal(self)
    def close(self):
        self.closed=True
        for l in [self.local_b,self.a,self.tcp]:l.close()
        for x in self.connections:
            for key in ['client','upstream']:
                try:x[key].shutdown(socket.SHUT_RDWR);x[key].close()
                except OSError:pass
        if self.original.exists():self.original.unlink()
        if self.a_path.exists():self.a_path.unlink()
        if self.backend.exists():self.backend.rename(self.original)

class RawTerminal:
    def __init__(self,g):
        self.socket=socket.socket(socket.AF_UNIX);self.socket.connect(str(g.a_path));self.data=bytearray();self.closed=False
        self.socket.sendall(json.dumps({'proto':1,'op':'attach','short':g.job,'auth':g.auth,'cols':120,'rows':36,'attachId':str(uuid.uuid4()),'caps':g.caps}).encode()+b'\n')
        line=b''
        while b'\n' not in line:line+=self.socket.recv(8192)
        raw,rest=line.split(b'\n',1);self.ack=json.loads(raw);self.data.extend(rest)
        assert self.ack.get('ok'),self.ack
    def send(self,data):self.socket.sendall(data.encode() if isinstance(data,str) else data)
    def read(self,seconds=1):
        from pty_probe import terminal_text
        until=time.monotonic()+seconds
        while time.monotonic()<until:
            ready,_,_=select.select([self.socket],[],[],max(0,min(.1,until-time.monotonic())))
            if ready:
                b=self.socket.recv(65536)
                if not b:break
                self.data.extend(b)
        return terminal_text(bytes(self.data))
    def close(self):
        if self.closed:return
        self.closed=True;self.socket.close()
