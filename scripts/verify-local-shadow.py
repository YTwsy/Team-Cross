#!/usr/bin/env python3
"""Three isolated Cores on one Mac, real TLS and Codex gpt-5.6-luna.

Uses only a new fixture home and new native conversations. Optional browser
inspection keeps these exact Cores alive until fixture/finish appears.
"""
import argparse
import importlib.util
import json
import os
import pathlib
import subprocess
import time
import uuid

spec = importlib.util.spec_from_file_location('native_fixture', pathlib.Path(__file__).with_name('verify-claude-native.py'))
n = importlib.util.module_from_spec(spec)
spec.loader.exec_module(n)


class ShadowCore(n.Core):
    def __init__(self, label, env, binary):
        self.label=label; self.data=n.ROOT/label
        self.log=(n.ROOT/'evidence'/(label+'.log')).open('w')
        command=[n.BINARY,'serve','--foreground','--no-open','--listen','127.0.0.1:0','--data-dir',str(self.data),'--repo',str(n.ROOT/'repo'),'--claude-bin','/usr/bin/false','--codex-bin',str(binary),'--test-loopback']
        self.p=subprocess.Popen(command,stdout=self.log,stderr=self.log,env=env,start_new_session=True)
        def ready():
            try:return json.loads((self.data/'connection.json').read_text()).get('pid')==self.p.pid
            except (OSError,ValueError):return False
        try:
            n.wait_until(ready,15)
            self.url=json.loads((self.data/'connection.json').read_text())['url']
            n.emit('core_start',label=label,pid=self.p.pid,url=self.url)
        except Exception:
            self.close()
            raise


def run(args):
    root = args.fixture_dir.resolve()
    root.mkdir(mode=0o700)
    (root/'evidence').mkdir(); (root/'repo').mkdir(); (root/'codex-home').mkdir()
    home = root/'codex-home'
    (home/'auth.json').symlink_to(args.codex_auth.resolve())
    (home/'config.toml').write_text('model = "gpt-5.6-luna"\nmodel_reasoning_effort = "medium"\napproval_policy = "on-request"\nsandbox_mode = "workspace-write"\n')
    env = {k:v for k,v in os.environ.items() if not k.startswith(('CODEX_', 'CLAUDE_', 'ANTHROPIC_')) and k!='CLAUDECODE'}
    env['CODEX_HOME'] = str(home)
    n.ROOT, n.BINARY, n.CLI, n.TOKEN = root, str(args.teamcross_bin.resolve()), '/usr/bin/false', ''
    cores = []
    report = {'scope':'one Mac, three Cores, TLS member transport, new native Codex shadow','model':n.MODEL}
    try:
        for label in ('host','member-b','member-c'):
            core = ShadowCore(label,env,args.codex_bin.resolve()); cores.append(core)
            core.api('settings',{'binary':str(args.codex_bin.resolve()),'claudeBinary':'/usr/bin/false'})
        a,b,c = cores
        s = a.api('spaces',{'requestId':str(uuid.uuid4()),'title':'Local shadow acceptance'})
        shared = a.api('collaborations/'+s['id']+'/action',{'action':'share','transport':'lan'})
        jb = b.api('join',{'invitation':shared['invitation']})['id']
        jc = c.api('join',{'invitation':shared['invitation']})['id']
        sid = str(uuid.uuid4())
        options = b.api('collaborations/'+jb+'/shadows/models?cwd='+str(root/'repo'))
        assert options['defaults']['model']==n.MODEL and options['defaults']['reasoningEffort']=='medium'
        settings = dict(options['defaults'], reasoningEffort='low')
        shadow = b.api('collaborations/'+jb+'/shadows/create',{'requestId':sid,'name':'Codex shadow on B','cwd':str(root/'repo'),'settings':settings})
        assert shadow['settings']==settings
        assert shadow['id']==sid
        def view(core,space):
            return core.api('collaborations/'+space+'/shadows/view?shadowId='+sid)
        n.wait_until(lambda:view(c,jc)['shadows'][0]['online'],30)
        self_id = view(c,jc)['selfId']
        mid = str(uuid.uuid4())
        payload = {'shadowId':sid,'message':{'id':mid,'text':'Explicitly remember my test color preference is amber using shadow_remember with member scope. Reply briefly with CORE_PIPELINE_OK. Do not use other tools or change files.'}}
        receipt = c.api('collaborations/'+jc+'/shadows/send',payload)
        assert receipt['state']=='sending' and not receipt['message'].get('receivedAt')
        def answered():
            v=view(c,jc)['shadows'][0]
            if v.get('error'): raise RuntimeError(v['error'])
            return v if any(m.get('replyTo')==mid and m['role']=='assistant' and 'CORE_PIPELINE_OK' in m['text'] for m in v['messages']) else None
        v=n.wait_until(answered,180)
        assert v['model']==n.MODEL and v['memories'][0]['subjectId']==self_id
        assert next(m for m in v['messages'] if m['id']==mid)['receivedAt']
        native_id=v['current']
        for core,space in ((a,s['id']),(b,jb),(c,jc)):
            shared_view=n.wait_until(lambda:view(core,space)['shadows'][0] if len(view(core,space)['shadows'][0]['messages'])==2 else None,10)
            assert shared_view['current']==native_id and shared_view['spaceId']==space
        assert c.api('collaborations/'+jc+'/shadows/send',payload)['state']=='delivered'
        report['allMembersShareAuthoredHistory']=True
        report['durableReceiptAndSameIdDeduplication']=True
        n.emit('shared_native_conversation_verified')
        # No cloud queue: an offline attempt remains failed after reconnect
        # until the sender explicitly retries the same id.
        (home/'config.toml').write_text((home/'config.toml').read_text().replace('model_reasoning_effort = "medium"','model_reasoning_effort = "high"'))
        b.close()
        n.wait_until(lambda:not view(c,jc)['shadows'][0]['online'],20)
        mid2=str(uuid.uuid4())
        retry={'shadowId':sid,'message':{'id':mid2,'text':'What is my remembered test color? Answer with amber and RESUMED_PIPELINE_OK. Do not change memory or start any tasks.'}}
        assert c.api('collaborations/'+jc+'/shadows/send',retry)['state']=='sendFailed'
        (root/'evidence/member-b.log').rename(root/'evidence/member-b-before-restart.log')
        b=ShadowCore('member-b',env,args.codex_bin.resolve()); cores[1]=b
        b.api('settings',{'binary':str(args.codex_bin.resolve()),'claudeBinary':'/usr/bin/false'})
        n.wait_until(lambda:view(c,jc)['shadows'][0]['online'],40)
        time.sleep(1)
        before=view(c,jc)['shadows'][0]
        assert before['current']==native_id
        failed=next(m for m in before['messages'] if m['id']==mid2)
        assert failed['state']=='sendFailed' and not failed.get('receivedAt')
        c.api('collaborations/'+jc+'/shadows/send',retry)
        def resumed():
            v=view(c,jc)['shadows'][0]
            return v if any(m.get('replyTo')==mid2 and 'RESUMED_PIPELINE_OK' in m['text'] and 'amber' in m['text'].lower() for m in v['messages']) else None
        v=n.wait_until(resumed,180)
        assert v['current']==native_id and len([m for m in v['messages'] if m['id']==mid2])==1
        assert v['settings']==settings and v['reasoningEffort']=='low'
        report['launchSettingsSurviveChangedDefaultsAndCoreRestart']=True
        report['offlineManualRetryOnly']=True
        report['exactNativeThreadAndMemoryAfterCoreRestart']=True
        report['nativeThread']=native_id
        # Read through real stdio MCP as well; the existing Core is reused.
        requests=[{'id':1,'method':'initialize','params':{'protocolVersion':'2024-11-05','capabilities':{},'clientInfo':{'name':'shadow-test','version':'1'}}},{'id':2,'method':'tools/call','params':{'name':'read_space_shadow','arguments':{'spaceId':jc,'shadowId':sid}}}]
        proc=subprocess.run([n.BINARY,'mcp','--data-dir',str(c.data)],input='\n'.join(json.dumps(r) for r in requests)+'\n',text=True,capture_output=True,env=env,timeout=30,check=True)
        responses=[json.loads(line) for line in proc.stdout.splitlines() if line.strip()]
        result=next(r['result'] for r in responses if r.get('id')==2)
        assert not result.get('isError') and 'RESUMED_PIPELINE_OK' in json.dumps(result)
        report['stdioMCPSharedRead']=True
        (root/'evidence/report.json').write_text(json.dumps(report,indent=2)+'\n')
        # Only synthetic messages and fixture IDs are written, never auth or invitation secrets.
        (root/'evidence/shared-view.json').write_text(json.dumps(v,indent=2)+'\n')
        fixture={'url':a.url+'/#/collaborations/'+s['id'],'ownerUrl':b.url+'/#/collaborations/'+jb,'memberUrl':c.url+'/#/collaborations/'+jc,'spaceId':s['id'],'shadowId':sid,'api':a.url}
        (root/'fixture.json').write_text(json.dumps(fixture,indent=2)+'\n')
        n.emit('shadow_core_acceptance_passed',report=str(root/'evidence/report.json'))
        if args.keep_browser:
            n.emit('browser_fixture_ready',url=fixture['url'])
            until=time.monotonic()+1200
            while time.monotonic()<until and not (root/'finish').exists(): time.sleep(.3)
    finally:
        for core in reversed(cores): core.close()


if __name__=='__main__':
    p=argparse.ArgumentParser()
    p.add_argument('--fixture-dir',required=True,type=pathlib.Path)
    p.add_argument('--teamcross-bin',required=True,type=pathlib.Path)
    p.add_argument('--codex-bin',required=True,type=pathlib.Path)
    p.add_argument('--codex-auth',required=True,type=pathlib.Path)
    p.add_argument('--keep-browser',action='store_true')
    run(p.parse_args())
