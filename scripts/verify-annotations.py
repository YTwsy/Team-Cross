#!/usr/bin/env python3
"""Opt-in annotation acceptance in a fresh fixture, real native TUI and Luna only.

The provider flag chooses one dedicated runtime. Two local Cores exercise the
member gateway; this is not a two-Mac LAN test. Optional preview keeps only the
fixture running for WebGUI / dedicated Desktop inspection.
"""
import argparse
import importlib.util
import json
import os
import pathlib
import shutil
import subprocess
import time
import uuid

spec = importlib.util.spec_from_file_location('annotation_native_fixture', pathlib.Path(__file__).with_name('verify-claude-native.py'))
n = importlib.util.module_from_spec(spec)
spec.loader.exec_module(n)


def run(args):
    root = args.fixture_dir.resolve()
    if root.exists() and any(root.iterdir()):
        raise ValueError('Choose a new empty fixture directory')
    root.mkdir(parents=True, exist_ok=True)
    root.chmod(0o700)
    (root/'evidence').mkdir()
    n.ROOT = root
    if args.provider == 'claude' and not args.claude_bin:
        raise ValueError('Pass --claude-bin or put the Claude CLI on PATH')
    n.CLI = str(pathlib.Path(args.claude_bin or '/usr/bin/false').resolve())
    n.BINARY = str(pathlib.Path(args.teamcross_bin).resolve())
    n.TOKEN = ''
    guard = None
    cores, terminals = [], []
    report = {'provider': args.provider, 'scope': 'one Mac, two real Cores', 'model': n.MODEL}
    try:
        repo = root/'repo'; repo.mkdir()
        for command in (['init','-b','main'], ['config','user.name','Team Cross Annotation Fixture'], ['config','user.email','fixture@example.invalid']):
            subprocess.run(['git','-C',str(repo)]+command,check=True,capture_output=True)
        (repo/'baseline.txt').write_text('annotation fixture baseline\n')
        subprocess.run(['git','-C',str(repo),'add','baseline.txt'],check=True,capture_output=True)
        subprocess.run(['git','-C',str(repo),'commit','-m','Dedicated annotation fixture'],check=True,capture_output=True)
        if args.provider == 'claude':
            n.CREDS = json.loads(args.source_settings.read_text())['env']
            n.TOKEN = n.CREDS.get('ANTHROPIC_AUTH_TOKEN') or n.CREDS.get('ANTHROPIC_API_KEY')
            if not n.TOKEN: raise ValueError('Local proxy credential missing')
            guard = n.Guard(); env = n.environment(guard)
            home = root/'source-home'; home.mkdir()
            settings = home/'settings.json'
            settings.write_text(json.dumps({'env':{k:v for k,v in env.items() if k.startswith('ANTHROPIC_')},'disableAllHooks':True,'enabledPlugins':{}}))
            settings.chmod(0o600)
            seed = json.loads(n.cli(env,['--print','--output-format','json','--model',n.MODEL,'--effort','low','--tools','','--strict-mcp-config','--mcp-config','{"mcpServers":{}}','--no-chrome','Reply exactly TCX_ANNOTATION_SOURCE. Do not use tools.']))
            assert not seed.get('is_error'), 'source model failed'
            source = seed['session_id']
            report['version'] = n.cli(env,['--version'],cwd=root).strip()
        else:
            env = {k:v for k,v in os.environ.items() if not k.startswith(('CODEX_','CLAUDE_')) and k != 'CLAUDECODE'}
            home = root/'codex-home'; home.mkdir()
            (home/'auth.json').symlink_to(pathlib.Path.home()/'.codex/auth.json')
            (home/'config.toml').write_text('model = "'+n.MODEL+'"\nmodel_reasoning_effort = "low"\n[mcp_servers]\n')
            env.update({'CODEX_HOME':str(home),'TERM':'xterm-256color'})
            completed = subprocess.run([args.codex_bin,'exec','--json','-m',n.MODEL,'-c','mcp_servers={}','-c','features.multi_agent=false','-c','features.memories=false','-c','features.plugins=false','Reply exactly TCX_ANNOTATION_SOURCE. Do not use tools.'],env=env,cwd=repo,capture_output=True,text=True,timeout=100)
            (root/'evidence/source.jsonl').write_text(completed.stdout)
            if completed.returncode: raise RuntimeError('Codex fixture source failed: '+completed.stderr[-1500:])
            messages = [json.loads(line) for line in completed.stdout.splitlines() if line.startswith('{')]
            source = next(m['thread_id'] for m in messages if m.get('type') == 'thread.started')
            report['version'] = subprocess.check_output([args.codex_bin,'--version'],env=env,text=True).strip()
            # The runtime must disable inherited personal servers, even though
            # Codex merges MCP configuration tables across layers.
            with (home/'config.toml').open('a') as config:
                config.write('[mcp_servers.personal_fixture]\ncommand="/usr/bin/false"\n')
        a = n.Core('core-a',env); cores.append(a)
        b = n.Core('core-b',env); cores.append(b)
        if args.provider == 'codex':
            for core in (a,b): core.api('settings',{'binary':args.codex_bin, 'claudeBinary':n.CLI})
        body = {'provider':args.provider,'sourceId':source,'workspaceMode':'existing','requestId':str(uuid.uuid4()),'title':'批注闭环验证 · '+args.provider}
        body['previewHash'] = a.api('preview',body)['previewHash']
        c = a.api('collaborations',body); base='collaborations/'+c['id']
        report.update({'id':c['id'],'sessionId':c['sessionId'],'core':a.url,'detail':a.url+'/#/'+base})
        def action(value): return a.api(base+'/action',{'action':value,'epoch':a.api(base)['epoch']})
        shared=action('share'); joined=b.api('join',{'invitation':shared['invitation']}); bbase='collaborations/'+joined['id']
        annotation=b.api(bbase+'/annotations',{'text':'请核对批注读取结果：TCX_NATIVE_NOTE_SECRET_729。需要在这里回复处理结果。'})
        b.api(bbase+'/annotation-replies',{'annotationId':annotation['id'],'text':'协作者补充：TCX_HUMAN_REPLY_483','requestId':'human-reply'})
        action('handoff')
        plan=b.api(bbase+'/open',{'client':'tui','launch':False})
        terminal=n.Terminal(args.provider+'-native-b',plan['command'],env); terminals.append(terminal)
        n.wait_until(lambda:a.api(base)['clientState']=='session_ready',40,terminal)
        if args.provider == 'codex':
            status=a.api(base+'/rpc',{'method':'mcpServerStatus/list','params':{}})
            servers=status.get('data',[])
            (root/'evidence/mcp-status.json').write_text(json.dumps(status,ensure_ascii=False,indent=2))
            # Codex includes disabled entries in status, with no serverInfo or
            # tools. Only the scoped server may have a handshake and tools.
            active=[server for server in servers if server.get('serverInfo') or server.get('tools')]
            assert [server['name'] for server in active]==['teamcross_annotations'], 'Inherited MCP servers must not be active'
            assert set(active[0]['tools'])=={'read_annotations','reply_to_annotation'}
            report['inherited_mcp_disabled']=True
        n.emit('native_ready',provider=args.provider,detail=report['detail'],root=str(root))
        def notes():return a.api(base+'/context?kind=annotations')['annotations']
        def send_native_prompt(tui, prompt):
            if args.provider != 'codex':
                tui.prompt(prompt);return
            offset=len(tui.data)
            tui.send('\x1b[200~'+prompt+'\x1b[201~')
            # session_ready confirms the gateway attachment, while Codex can
            # still be painting the composer. Submit after the draft is echoed.
            suffix=''.join(prompt.split())[-80:]
            n.wait_until(lambda:suffix in ''.join(n.terminal_text(tui.data[offset:]).split()),10,tui)
            tui.send('\r')
        send_native_prompt(terminal,'Use only the Team Cross shared-runtime MCP tools. Call read_annotations without arguments and read all root annotations and replies. Then reply_to_annotation on the root annotation you read, text starting with TCX_AGENT_REPLY_OK followed by both TCX marker values you actually read from the root annotation and the human reply, requestId="native-agent-reply". Do not use shell/file tools, add_annotation, send_input, or any other MCP server. Then reply TCX_NATIVE_ANNOTATIONS_DONE. This is a dedicated test and this reply is authorized.')
        approved=0;screen_offset=0;approved_ids=set()
        def approve_fixture_tool(terminal, core, route):
            nonlocal approved,screen_offset
            status=a.api(base)
            if args.provider=='codex':
                for approval in a.api(base+'/context?kind=events')['approvals']:
                    p=approval.get('params',{});meta=p.get('_meta',{})
                    if approval['id'] not in approved_ids and approval['method']=='mcpServer/elicitation/request' and p.get('serverName')=='teamcross_annotations' and meta.get('tool_params',{}).get('requestId')=='native-agent-reply':
                        core.api(route+'/respond',{'id':approval['id'],'result':{'action':'accept','content':{}}})
                        approved_ids.add(approval['id']);approved+=1
                        report['codex_reply_approval_via_control_api']=True
            else:
                recent=n.terminal_text(terminal.data[screen_offset:]).lower()
                compact=''.join(recent.split())
                if status.get('nativeWaiting')=='permission prompt' and 'teamcross_annotations' in recent and ('readannotationstool' in compact or 'replytoannotationtool' in compact):
                    terminal.send('\r');approved+=1;screen_offset=len(terminal.data)
        start=time.monotonic()
        while time.monotonic()-start<150:
            text=terminal.read(.2)
            if any(reply.get('requestId')=='native-agent-reply' for note in notes() for reply in note.get('replies',[])):break
            # Native Claude can ask for MCP permission. Only approve a visible
            # Team Cross annotation tool confirmation in this dedicated fixture.
            approve_fixture_tool(terminal,b,bbase)
            if terminal.p.poll() is not None: raise RuntimeError('native TUI exited before reply')
        else:raise TimeoutError('native annotation tool reply')
        expected='Claude Code' if args.provider=='claude' else 'Codex'
        saved=notes();reply=next(reply for note in saved for reply in note.get('replies',[]) if reply.get('requestId')=='native-agent-reply')
        assert reply['author']==expected and 'TCX_NATIVE_NOTE_SECRET_729' in reply['text'] and 'TCX_HUMAN_REPLY_483' in reply['text']
        n.wait_until(lambda:not a.api(base)['busy'],50,terminal)
        history=a.api(base+'/context?kind=history')
        events=a.api(base+'/context?kind=events')
        (root/'evidence/annotations.json').write_text(json.dumps(saved,ensure_ascii=False,indent=2))
        (root/'evidence/history.json').write_text(json.dumps(history,ensure_ascii=False,indent=2))
        (root/'evidence/events.json').write_text(json.dumps(n.clean(events),ensure_ascii=False,indent=2))
        report.update({'native_read_and_reply':True,'native_mcp_approvals':approved,'root_count':len(saved)})
        terminal.close();n.wait_until(lambda:not a.api(base)['connected'],12)
        action('reclaim');action('end');n.wait_until(lambda:a.api(base)['runtimeState']=='released',40)
        restored=action('start');assert restored['sessionId']==c['sessionId']
        # Exercise the original persisted STDIO launch configuration after restore.
        restored_plan=a.api(base+'/open',{'client':'tui','launch':False})
        restored_tui=n.Terminal(args.provider+'-restored',restored_plan['command'],env);terminals.append(restored_tui)
        screen_offset=0
        n.wait_until(lambda:a.api(base)['clientState']=='session_ready',35,restored_tui)
        send_native_prompt(restored_tui,'Use the Team Cross read_annotations tool. Read the existing replies. Do not write or call any other tool. Reply exactly TCX_RESTORED_READ_OK after successfully reading the reply TCX_AGENT_REPLY_OK.')
        def restored_answer():
            approve_fixture_tool(restored_tui,a,base)
            history=a.api(base+'/context?kind=history')
            return any(item.get('type')=='agentMessage' and 'TCX_RESTORED_READ_OK' in item.get('text','') for turn in history.get('thread',{}).get('turns',[]) for item in turn.get('items',[])) and not a.api(base)['busy']
        n.wait_until(restored_answer,100,restored_tui)
        restored_tui.close();report['same_session_restore_read']=True
        (root/'preview.json').write_text(json.dumps(report,ensure_ascii=False,indent=2))
        n.emit('annotation_acceptance_verified',**report)
        if args.keep_preview_seconds:
            n.emit('preview_available',root=str(root),detail=report['detail'],stop_file=str(root/'finish-preview'))
            end=time.monotonic()+min(args.keep_preview_seconds,1800)
            while time.monotonic()<end and not (root/'finish-preview').exists():time.sleep(.5)
        action('end')
    except Exception as error:
        report['error']=n.clean(type(error).__name__+': '+str(error));n.emit('annotation_acceptance_failed',error=report['error'])
    finally:
        for resource in list(reversed(terminals))+list(reversed(cores)):
            try:resource.close()
            except Exception as error:report.setdefault('cleanup_errors',[]).append(type(error).__name__)
        if args.provider=='claude' and 'env' in locals():
            try:n.stop_fixture_daemon(env,root)
            except Exception as error:report.setdefault('cleanup_errors',[]).append(type(error).__name__)
        if guard:
            report['models_observed']=sorted({e['model'] for e in guard.entries if e.get('model')});guard.close()
        (root/'evidence/summary.json').write_text(json.dumps(n.clean(report),ensure_ascii=False,indent=2))
        n.emit('complete',error=report.get('error'),provider=args.provider)
    return 1 if report.get('error') or report.get('cleanup_errors') else 0


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--fixture-dir',type=pathlib.Path,required=True)
    parser.add_argument('--provider',choices=['codex','claude'],required=True)
    parser.add_argument('--teamcross-bin',required=True)
    parser.add_argument('--codex-bin',default='/Applications/ChatGPT.app/Contents/Resources/codex')
    parser.add_argument('--claude-bin',default=shutil.which('claude'))
    parser.add_argument('--source-settings',type=pathlib.Path,default=pathlib.Path.home()/'.claude/settings.json')
    parser.add_argument('--keep-preview-seconds',type=int,default=0)
    raise SystemExit(run(parser.parse_args()))
