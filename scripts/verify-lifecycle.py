#!/usr/bin/env python3
"""Exercise installed executables against disposable data; never inspect user sessions."""
import argparse, concurrent.futures, json, os, pathlib, signal, socket, subprocess, tempfile, time, urllib.request
p=argparse.ArgumentParser();p.add_argument('binary');a=p.parse_args();binary=str(pathlib.Path(a.binary).resolve())
with tempfile.TemporaryDirectory(prefix='teamcross-lifecycle-') as temp:
    root=pathlib.Path(temp);data=root/'data with spaces';data.mkdir();env=os.environ.copy();env['CODEX_HOME']=str(root/'empty-codex');pathlib.Path(env['CODEX_HOME']).mkdir()
    def call(*args, target=data, check=True):
        return subprocess.run([binary,*args,'--data-dir',str(target)],env=env,text=True,capture_output=True,check=check,timeout=30)
    def status(target=data): return json.loads(call('status','--json',target=target).stdout)
    owned=[]
    try:
        # A valid Core lock is the lifetime boundary, including runtime cleanup.
        blocker=socket.socket()
        try: blocker.bind(('127.0.0.1',43210));blocker.listen()
        except OSError: blocker.close();blocker=None
        try:
            with concurrent.futures.ThreadPoolExecutor(max_workers=5) as pool:
                starts=list(pool.map(lambda _:json.loads(call('serve','--no-open','--json').stdout),range(5)))
            pids={x['service']['pid'] for x in starts};assert len(pids)==1,pids
            owned.append(data);s=status();assert s['running'] and not s.get('token');assert s['url']!='http://127.0.0.1:43210'
        finally:
            if blocker: blocker.close()
        alias=root/'alias';alias.symlink_to(data,target_is_directory=True)
        assert json.loads(call('serve','--no-open','--json',target=alias).stdout)['service']['pid']==s['pid']
        c=json.loads((data/'connection.json').read_text())
        req=urllib.request.Request(c['url']+'/api/control/stop',data=b'{"force":true}',headers={'Content-Type':'application/json'})
        try: urllib.request.urlopen(req);raise AssertionError('unauthenticated stop accepted')
        except urllib.error.HTTPError as e: assert e.code==403
        # Caller exits, service remains. Version replacement cannot replace a running instance.
        assert status()['pid']==s['pid']
        (data/'preserved.txt').write_text('keep')
        call('stop','--json');assert not status()['running'];assert (data/'preserved.txt').read_text()=='keep'
        # MCP handshake/listing tools does not launch Core; invoking a tool does.
        handshake='{"jsonrpc":"2.0","id":1,"method":"initialize"}\n{"jsonrpc":"2.0","id":2,"method":"tools/list"}\n'
        result=subprocess.run([binary,'mcp','--data-dir',str(data)],input=handshake,env=env,text=True,capture_output=True,check=True,timeout=10)
        assert len(result.stdout.strip().splitlines())==2 and not status()['running']
        invoke=handshake+'{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_collaborations","arguments":{}}}\n'
        result=subprocess.run([binary,'mcp','--data-dir',str(data)],input=invoke,env=env,text=True,capture_output=True,check=True,timeout=30)
        assert json.loads(result.stdout.splitlines()[-1])['result']['isError'] is False
        s=status();assert s['running']
        os.kill(s['pid'],signal.SIGKILL)
        time.sleep(.3)
        restarted=json.loads(call('serve','--no-open','--json').stdout)['service'];assert restarted['pid']!=s['pid'];call('stop','--json')
        # An explicitly requested occupied port must fail instead of silently moving.
        with socket.socket() as occupied:
            occupied.bind(('127.0.0.1',0));occupied.listen()
            failed=call('serve','--no-open','--listen',f'127.0.0.1:{occupied.getsockname()[1]}',target=root/'port-error',check=False)
            assert failed.returncode!=0
        assert not (root/'port-error/connection.json').exists()
        print(json.dumps({'singleInstance':True,'canonicalPath':True,'defaultPortFallback':True,'explicitPortConflict':True,'authenticatedStop':True,'mcpLazyStart':True,'crashRecovery':True,'dataPreserved':True}))
    finally:
        for directory in owned:
            subprocess.run([binary,'stop','--force','--data-dir',str(directory)],env=env,capture_output=True,timeout=30)
