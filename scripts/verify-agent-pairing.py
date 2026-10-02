#!/usr/bin/env python3
"""Opt-in native pairing acceptance, in a fresh repository and isolated homes.

Uses only gpt-5.6-luna. Never resumes personal sessions or changes installed
client configuration. The supplied fixture directory must be new and empty.
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

spec = importlib.util.spec_from_file_location('pairing_native_fixture', pathlib.Path(__file__).with_name('verify-claude-native.py'))
n = importlib.util.module_from_spec(spec)
spec.loader.exec_module(n)


def run(args):
    root = args.fixture_dir.resolve()
    if root.exists() and any(root.iterdir()):
        raise ValueError('Choose a new empty fixture directory')
    root.mkdir(parents=True, exist_ok=True); root.chmod(0o700)
    (root/'evidence').mkdir()
    n.ROOT, n.TOKEN = root, ''
    n.BINARY, n.CLI = str(args.teamcross_bin.resolve()), '/usr/bin/false'
    args.codex_bin = shutil.which(args.codex_bin) or args.codex_bin
    core, terminal = None, None
    report = {'model': n.MODEL, 'scope': 'one Mac, dedicated Codex TUI and Core'}
    try:
        repo = root/'repo'; repo.mkdir()
        for command in (['init', '-b', 'main'], ['config', 'user.name', 'Pairing Fixture'], ['config', 'user.email', 'fixture@example.invalid']):
            subprocess.run(['git', '-C', str(repo)]+command, check=True, capture_output=True)
        (repo/'baseline.txt').write_text('PAIRING_BASELINE\n')
        subprocess.run(['git', '-C', str(repo), 'add', 'baseline.txt'], check=True)
        subprocess.run(['git', '-C', str(repo), 'commit', '-m', 'Dedicated pairing fixture'], check=True, capture_output=True)
        env = {k:v for k,v in os.environ.items() if not k.startswith(('CODEX_', 'CLAUDE_', 'ANTHROPIC_')) and k != 'CLAUDECODE'}
        home = root/'codex-home'; home.mkdir()
        (home/'auth.json').symlink_to(args.codex_auth.resolve())
        (home/'config.toml').write_text('model="'+n.MODEL+'"\nmodel_reasoning_effort="low"\n[features]\nmemories=false\nplugins=false\nmulti_agent=false\n')
        env.update({'CODEX_HOME':str(home),'TERM':'xterm-256color'})
        report['nativeVersion'] = subprocess.check_output([args.codex_bin, '--version'],env=env,text=True).strip()
        p = subprocess.run([args.codex_bin,'exec','--json','-m',n.MODEL,'Reply exactly PAIRING_SOURCE. Do not use tools.'],env=env,cwd=repo,capture_output=True,text=True,timeout=180)
        (root/'evidence/source-output.jsonl').write_text(p.stdout)
        (root/'evidence/source-stderr.txt').write_text(p.stderr)
        if p.returncode: raise RuntimeError('Source creation failed; inspect source-stderr.txt')
        source = next(json.loads(x)['thread_id'] for x in p.stdout.splitlines() if json.loads(x).get('type') == 'thread.started')
        core = n.Core('core',env)
        core.api('settings',{'binary':args.codex_bin,'claudeBinary':'/usr/bin/false'})
        create = {'sourceId':source,'provider':'codex','workspaceMode':'existing','runtimeMode':'restricted','requestId':str(uuid.uuid4()),'title':'Pairing acceptance'}
        preview = core.api('preview',create); create['previewHash'] = preview['previewHash']
        session = core.api('collaborations',create); cid = session['id']
        report['sessionId'], report['collaborationId'] = session['sessionId'], cid
        pair = core.api('agent-pairings',{'name':'Acceptance conversation'})
        prompt = ('Dedicated acceptance task. Use only Team Cross MCP tools. Call pair_current_session with code '+pair['code']+'. '
                  'Do not guess another session. End this turn after the tool result. Do not poll. Do not change files.')
        plan = subprocess.run([n.BINARY,'open','--data-dir',str(core.data),'--id',cid,'--client','tui','--print-command','--json'],env=env,cwd=repo,capture_output=True,text=True,timeout=60)
        if plan.returncode: raise RuntimeError(plan.stdout+plan.stderr)
        terminal = n.Terminal('codex-pairing',json.loads(plan.stdout)['command'],env)
        n.wait_until(lambda:core.api('collaborations/'+cid)['clientState']=='session_ready',45,terminal)
        terminal.prompt(prompt)
        approved = set()
        def approve_fixture_tools():
            for approval in core.api('collaborations/'+cid+'/context?kind=events')['approvals']:
                p = approval.get('params',{}); meta = p.get('_meta',{}); params = meta.get('tool_params',{}); message = p.get('message','')
                known = params.get('code') == pair['code'] and '"pair_current_session"' in message
                if '"read_agent_request"' in message or '"finish_agent_request"' in message:
                    known = any(r['id']==params.get('requestId') and r['pairingId']==pair['pairing']['id'] for r in core.api('agent-requests'))
                if known and approval['id'] not in approved and approval['method']=='mcpServer/elicitation/request' and p.get('serverName')=='teamcross_annotations' and p.get('threadId')==session['sessionId']:
                    core.api('collaborations/'+cid+'/respond',{'id':approval['id'],'result':{'action':'accept','content':{}}})
                    approved.add(approval['id'])
                    report['scopedToolApprovals'] = len(approved)
            (root/'evidence/native-progress.txt').write_text(n.clean(n.terminal_text(terminal.data)))
        def paired():
            approve_fixture_tools()
            return next((p for p in core.api('agent-pairings') if p['id']==pair['pairing']['id'] and p['state']=='paired'),None)
        target = n.wait_until(paired,150,terminal)
        n.wait_until(lambda:not core.api('collaborations/'+cid)['busy'],100,terminal)
        assert target['sessionId']==session['sessionId'] and target['sessionId']!=source
        report['pairingId'], report['pairedThroughNativeTool'] = target['id'], True
        note = core.api('collaborations/'+cid+'/annotations',{'text':'Please check the exact contents of baseline.txt. Analysis only; do not reply to this annotation.'})
        request = {'requestId':str(uuid.uuid4()),'pairingId':target['id'],'references':[{'spaceId':cid,'kind':'annotation','annotationId':note['id']}], 'instruction':'Read the original annotation with read_annotations. Tell me what it requests. Do not modify files or reply to the annotation. Finish this request using finish_agent_request with summary PAIRING_ANALYSIS_DONE.','intent':'analyze'}
        submitted = core.api('agent-requests',request)
        assert submitted['state'] in ('submitted','received','completed')
        duplicate = core.api('agent-requests',request)
        assert duplicate['id']==submitted['id']
        def completed():
            approve_fixture_tools()
            state = core.api('agent-requests/'+request['requestId'])
            if state['state']=='failed': raise RuntimeError(state.get('summary','Agent failed'))
            return state if state['state']=='completed' else None
        result = n.wait_until(completed,180,terminal)
        assert result['summary']=='PAIRING_ANALYSIS_DONE'
        assert (repo/'baseline.txt').read_text()=='PAIRING_BASELINE\n'
        note_after = core.api('collaborations/'+cid+'/context?kind=annotations&annotationId='+note['id'])
        assert not note_after.get('replies'), 'analyze-only unexpectedly replied'
        report.update({'deliveryReadAndCompletion':True,'duplicateRequestId':True,'analysisDidNotReply':True,'result':result})
        core.api('agent-pairings/'+target['id']+'/remove',{})
        assert not any(p['id']==target['id'] for p in core.api('agent-pairings'))
        report['removed']=True
        n.emit('agent_pairing_verified')
    except Exception as e:
        report['error']=type(e).__name__+': '+str(e)
        report['traceback']=traceback.format_exc()
        n.emit('agent_pairing_failed',error=report['error'])
    finally:
        for item in (terminal,core):
            if item:
                try:item.close()
                except Exception as e:report.setdefault('cleanupErrors',[]).append(str(e))
        (root/'evidence/report.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    return 1 if 'error' in report or 'cleanupErrors' in report else 0

if __name__=='__main__':
    parser=argparse.ArgumentParser()
    parser.add_argument('--fixture-dir',type=pathlib.Path,required=True)
    parser.add_argument('--teamcross-bin',type=pathlib.Path,required=True)
    parser.add_argument('--codex-bin',default='codex')
    parser.add_argument('--codex-auth',type=pathlib.Path,default=pathlib.Path.home()/'.codex/auth.json')
    raise SystemExit(run(parser.parse_args()))
