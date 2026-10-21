#!/usr/bin/env python3
"""Opt-in Luna-only proof of passive MCP and native prompt-hook delivery.

Creates an isolated home, repository, Core and two native forks. Never operates
on a pre-existing user session or modifies personal configuration.
"""
import argparse
import importlib.util
import json
import os
import pathlib
import shlex
import shutil
import subprocess
import traceback
import uuid

spec = importlib.util.spec_from_file_location('inflow_native', pathlib.Path(__file__).with_name('verify-claude-native.py'))
n = importlib.util.module_from_spec(spec)
spec.loader.exec_module(n)


def run(args):
    root = args.fixture_dir.resolve()
    if root.exists():
        raise ValueError('Choose a new fixture directory')
    root.mkdir(mode=0o700, parents=True)
    (root/'evidence').mkdir()
    repo = root/'repo'; repo.mkdir()
    home = root/'codex-home'; home.mkdir()
    (home/'auth.json').symlink_to(args.codex_auth.resolve())
    binary = shutil.which(args.codex_bin) or args.codex_bin
    n.ROOT, n.BINARY, n.CLI, n.TOKEN = root, str(args.teamcross_bin.resolve()), '/usr/bin/false', ''
    env = {k:v for k,v in os.environ.items() if not k.startswith(('CODEX_', 'CLAUDE_', 'ANTHROPIC_')) and k != 'CLAUDECODE'}
    env.update({'CODEX_HOME':str(home), 'TERM':'xterm-256color'})
    cores, terminals, executions = [], [], []
    report = {'model':n.MODEL, 'scope':'one Mac, isolated native Codex TUI, one Core, two forks'}
    try:
        for command in (['init','-b','main'], ['config','user.name','In-flow fixture'], ['config','user.email','fixture@example.invalid']):
            subprocess.run(['git','-C',str(repo)]+command, check=True, capture_output=True)
        (repo/'baseline.txt').write_text('In-flow fixture\n')
        subprocess.run(['git','-C',str(repo),'add','baseline.txt'], check=True, capture_output=True)
        subprocess.run(['git','-C',str(repo),'commit','-m','Isolated in-flow fixture'], check=True, capture_output=True)
        config = 'model='+json.dumps(n.MODEL)+'\nmodel_reasoning_effort="low"\n'
        (home/'config.toml').write_text(config+'[features]\nplugins=false\nmemories=false\nmulti_agent=false\n')
        seed = subprocess.run([binary,'exec','--json','-m',n.MODEL,'Reply exactly TCX_INFLOW_SOURCE. Do not use tools.'],env=env,cwd=repo,capture_output=True,text=True,timeout=100)
        (root/'evidence/source.jsonl').write_text(seed.stdout)
        if seed.returncode: raise RuntimeError(seed.stderr[-1500:])
        source = next(json.loads(line)['thread_id'] for line in seed.stdout.splitlines() if '"type":"thread.started"' in line or '"type": "thread.started"' in line)
        hook = shlex.quote(n.BINARY)+' inflow-hook --provider codex --data-dir '+shlex.quote(str(root/'core'))
        (home/'config.toml').write_text(config+'default_permissions="inflow-fixture"\napproval_policy="on-request"\napprovals_reviewer="user"\n[permissions.inflow-fixture]\nextends=":workspace"\n[permissions.inflow-fixture.network]\nenabled=true\n[features]\nhooks=true\nplugins=false\nmemories=false\nmulti_agent=false\n[[hooks.UserPromptSubmit]]\nhooks=[{type="command",command='+json.dumps(hook)+',timeout=4}]\n')
        core = n.Core('core',env); cores.append(core)
        core.api('settings',{'binary':binary,'claudeBinary':'/usr/bin/false'})
        report['nativeVersion'] = subprocess.check_output([binary,'--version'],env=env,text=True).strip()
        for mode in ('restricted','trusted'):
            body = {'sourceId':source,'provider':'codex','runtimeMode':mode,'workspaceMode':'worktree','requestId':str(uuid.uuid4()),'title':'In-flow '+mode}
            body['previewHash'] = core.api('preview',body)['previewHash']
            c = core.api('collaborations',body); executions.append(c)
            base = 'collaborations/'+c['spaceId']+'/sessions/'+c['executionId']
            wb = 'collaborations/'+c['spaceId']+'/workbench/'
            core.api(wb+'inflow-config',{'executionId':c['executionId'],'enabled':True})
            marker = 'TCX_INFLOW_'+uuid.uuid4().hex[:12]
            note = core.api(base+'/annotations',{'text':'Verification marker: '+marker})
            if mode == 'trusted':
                core.api(base+'/rpc',{'method':'thread/settings/update','params':{'permissions':'inflow-fixture','approvalPolicy':'on-request','approvalsReviewer':'user'},'requestId':'inflow-native-permissions'})
            plan = core.api(base+'/open',{'client':'tui','launch':False})
            terminal = n.Terminal('inflow-'+mode,plan['command'],env); terminals.append(terminal)
            n.wait_until(lambda:core.api(base)['clientState']=='session_ready',50,terminal)
            if mode == 'trusted':
                hooks = core.api(base+'/rpc',{'method':'hooks/list','params':{}})
                pending = [h for entry in hooks['data'] for h in entry['hooks'] if h['enabled'] and h['trustStatus'] in ('untrusted','modified')]
                if pending:
                    assert len(pending)==1 and pending[0]['command']==hook, 'Unexpected hook'
                    n.wait_until(lambda:'Trustallandcontinue' in ''.join(n.terminal_text(terminal.data).split()),25,terminal)
                    terminal.send('2'); terminal.read(.3); terminal.send('\r')
                    def trusted():
                        current = core.api(base+'/rpc',{'method':'hooks/list','params':{}})
                        return any(h['key']==pending[0]['key'] and h['trustStatus']=='trusted' for entry in current['data'] for h in entry['hooks'])
                    n.wait_until(trusted,20,terminal)
            n.wait_until(lambda:'TCX_INFLOW_SOURCE' in n.terminal_text(terminal.data),100,terminal)
            prompt = ('Do not call any tools. ' if mode=='trusted' else 'Call the Team Cross read_space_brief tool exactly once. Do not call read_annotations or any other tool. ')
            prompt += 'From the in-flow awareness reference context accompanying this interaction, return only the verification marker beginning TCX_INFLOW_. If none is present return MISSING. Do not guess.'
            offset = len(terminal.data)
            terminal.send('\x1b[200~'+prompt+'\x1b[201~')
            suffix = ''.join(prompt.split())[-80:]
            n.wait_until(lambda:suffix in ''.join(n.terminal_text(terminal.data[offset:]).split()) or 'Pasted' in n.terminal_text(terminal.data[offset:]),15,terminal)
            terminal.send('\r')
            approved = set()
            def answer():
                events = core.api(base+'/context?kind=events')
                for item in events.get('approvals',[]):
                    p = item.get('params',{})
                    if mode=='restricted' and item['method']=='mcpServer/elicitation/request' and p.get('threadId')==c['sessionId'] and p.get('serverName','').startswith('teamcross_annotations') and '"read_space_brief"' in p.get('message','') and str(item['id']) not in approved:
                        core.api(base+'/respond',{'id':item['id'],'result':{'action':'accept','content':{}}}); approved.add(str(item['id']))
                history = core.api(base+'/context?kind=history&view=answers')
                (root/'evidence'/(mode+'-progress.json')).write_text(json.dumps(history,ensure_ascii=False,indent=2))
                found = any(marker in item.get('text','') for item in history.get('segments',[]) if item.get('type')=='agentMessage')
                if found and not core.api(base)['busy']: return history
                return None
            history = n.wait_until(answer,150,terminal)
            record = json.loads((core.data/'collaborations'/c['spaceId']/'collaboration.json').read_text())
            delivered = record['workbench']['inFlow']['execution:'+c['executionId']]['delivered']
            assert delivered['comment:'+note['id']]==1
            native = core.api(base+'/rpc',{'method':'thread/read','params':{'threadId':c['sessionId'],'includeTurns':True}})
            latest = native['thread']['turns'][-1]
            tool_calls = [item for item in latest['items'] if item['type']=='mcpToolCall']
            if mode == 'restricted':
                assert len(tool_calls)==1 and tool_calls[0]['tool']=='read_space_brief', tool_calls
            else:
                assert not tool_calls, 'Hook proof must not call an MCP tool'
            (root/'evidence'/(mode+'-history.json')).write_text(json.dumps(history,ensure_ascii=False,indent=2))
            report[mode] = {'markerRecovered':True,'deliveryVersion':1,'nativeApprovals':len(approved),'mcpCalls':len(tool_calls)}
            terminal.close()
            n.wait_until(lambda:not core.api(base)['connected'],15)
            core.api(base+'/action',{'action':'end','epoch':core.api(base)['epoch']})
            n.emit('inflow_verified',mode=mode)
        report['passed'] = True
    except Exception:
        (root/'evidence/failure.txt').write_text(traceback.format_exc())
        raise
    finally:
        errors=[]
        for terminal in reversed(terminals):
            try: terminal.close()
            except Exception as error: errors.append(str(error))
        for core in reversed(cores):
            try: core.close()
            except Exception as error: errors.append(str(error))
        report['cleanupErrors']=errors
        (root/'report.json').write_text(json.dumps(report,ensure_ascii=False,indent=2))


if __name__=='__main__':
    parser=argparse.ArgumentParser()
    parser.add_argument('--fixture-dir',type=pathlib.Path,required=True)
    parser.add_argument('--teamcross-bin',type=pathlib.Path,required=True)
    parser.add_argument('--codex-bin',default='codex')
    parser.add_argument('--codex-auth',type=pathlib.Path,default=pathlib.Path.home()/'.codex/auth.json')
    run(parser.parse_args())
