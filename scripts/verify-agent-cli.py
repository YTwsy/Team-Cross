#!/usr/bin/env python3
"""Opt-in personal MCP current-session share and CLI relay, Luna only.

Requires a new fixture directory. Runs two Cores on one Mac, a real personal
Codex/Claude turn and a direct TUI via PTY; no personal source is resumed.
Ordinary management tools are also exercised over real MCP STDIO and CLI.
"""
import argparse
import importlib.util
import json
import os
import pathlib
import shlex
import shutil
import subprocess
import time
import traceback
import uuid

spec = importlib.util.spec_from_file_location('agent_cli_native_fixture', pathlib.Path(__file__).with_name('verify-claude-native.py'))
n = importlib.util.module_from_spec(spec)
spec.loader.exec_module(n)


def assistant_has(value, marker):
    if isinstance(value, dict):
        if value.get('type') == 'agentMessage' and marker in value.get('text', ''): return True
        return any(assistant_has(v, marker) for v in value.values())
    if isinstance(value, list): return any(assistant_has(v, marker) for v in value)
    return False


def run(args):
    root = args.fixture_dir.resolve()
    if root.exists() and any(root.iterdir()):
        raise ValueError('Choose a new empty fixture directory')
    root.mkdir(parents=True, exist_ok=True); root.chmod(0o700)
    (root/'evidence').mkdir()
    n.ROOT, n.TOKEN = root, ''
    n.CLI = str(pathlib.Path(args.claude_bin or '/usr/bin/false').resolve())
    n.BINARY = str(pathlib.Path(args.teamcross_bin).resolve())
    cores, terminals, guard = [], [], None
    report = {'provider': args.provider, 'model': n.MODEL, 'scope': 'one Mac, two Cores, loopback TLS membership'}
    try:
        repo = root/'repo'; repo.mkdir()
        for command in (['init', '-b', 'main'], ['config', 'user.name', 'Team Cross Agent CLI Fixture'], ['config', 'user.email', 'fixture@example.invalid']):
            subprocess.run(['git', '-C', str(repo)]+command, check=True, capture_output=True)
        (repo/'baseline.txt').write_text('TCX_AGENT_CLI_BASELINE\n')
        subprocess.run(['git', '-C', str(repo), 'add', 'baseline.txt'], check=True, capture_output=True)
        subprocess.run(['git', '-C', str(repo), 'commit', '-m', 'Dedicated agent CLI fixture'], check=True, capture_output=True)
        current_tools = ['get_current_source', 'preview_current_share', 'share_current_session']
        if args.provider == 'codex':
            env = {k:v for k,v in os.environ.items() if not k.startswith(('CODEX_', 'CLAUDE_')) and k != 'CLAUDECODE'}
            home = root/'codex-home'; home.mkdir()
            (home/'auth.json').symlink_to(args.codex_auth.resolve())
            (home/'config.toml').write_text(
                'model='+json.dumps(n.MODEL)+'\nmodel_reasoning_effort="low"\n'
                '[features]\nmemories=false\nplugins=false\nmulti_agent=false\n'
                '[mcp_servers.teamcross]\ncommand='+json.dumps(n.BINARY)+'\n'
                'args='+json.dumps(['mcp', '--data-dir', str(root/'core-a')])+'\n'
                'enabled_tools='+json.dumps(current_tools)+'\n'
                '[mcp_servers.teamcross.tools.share_current_session]\napproval_mode="approve"\n')
            env.update({'CODEX_HOME': str(home), 'TERM': 'xterm-256color'})
            report['nativeVersion'] = subprocess.check_output([args.codex_bin, '--version'], env=env, text=True).strip()
        else:
            if not args.claude_bin: raise ValueError('Pass --claude-bin')
            n.CREDS = json.loads(args.source_settings.read_text())['env']
            n.TOKEN = n.CREDS.get('ANTHROPIC_AUTH_TOKEN') or n.CREDS.get('ANTHROPIC_API_KEY')
            if not n.TOKEN: raise ValueError('Local proxy credential missing')
            guard = n.Guard(); env = n.environment(guard)
            home = root/'source-home'; home.mkdir()
            settings = home/'settings.json'
            settings.write_text(json.dumps({'env': {k:v for k,v in env.items() if k.startswith('ANTHROPIC_')}, 'disableAllHooks': True, 'enabledPlugins': {}})); settings.chmod(0o600)
            n.cli(env, ['mcp', 'add', '--scope', 'user', '--transport', 'stdio', 'teamcross', '--', n.BINARY, 'mcp', '--data-dir', str(root/'core-a')])
            report['nativeVersion'] = n.cli(env, ['--version']).strip()
        a = n.Core('core-a', env); cores.append(a)
        b = n.Core('core-b', env); cores.append(b)
        if args.provider == 'codex':
            for core in cores: core.api('settings', {'binary': args.codex_bin, 'claudeBinary': n.CLI})

        def cli(core, command, extra=(), stdin=None, allow_error=False):
            p = subprocess.run([n.BINARY, command, '--data-dir', str(core.data), '--json']+list(extra), env=env, cwd=repo, input=stdin, capture_output=True, text=True, timeout=100)
            value = json.loads(p.stdout)
            if p.returncode and not allow_error: raise RuntimeError(command+': '+str(n.clean(value)))
            return value

        def mcp(core, tool, arguments, allow_error=False):
            messages = [
                {'jsonrpc':'2.0', 'id':1, 'method':'initialize', 'params':{'clientInfo':{'name':'teamcross-acceptance'}}},
                {'jsonrpc':'2.0', 'id':2, 'method':'tools/call', 'params':{'name':tool, 'arguments':arguments}},
            ]
            p = subprocess.run([n.BINARY, 'mcp', '--data-dir', str(core.data)], env=env, cwd=repo, input='\n'.join(json.dumps(x) for x in messages)+'\n', capture_output=True, text=True, timeout=100)
            if p.returncode: raise RuntimeError('MCP failed: '+n.clean(p.stderr[-1200:]))
            result = next(json.loads(x)['result'] for x in p.stdout.splitlines() if json.loads(x).get('id') == 2)
            value = json.loads(result['content'][0]['text'])
            if result.get('isError') and not allow_error: raise RuntimeError(tool+': '+str(n.clean(value)))
            return value

        prompt = ('This is a dedicated Team Cross acceptance fixture. I explicitly authorize sharing THIS current session, '
                  'workspaceMode=existing, runtimeMode=restricted, transport=lan, title=Agent CLI acceptance. '
                  'Use only the Team Cross MCP tools. First get_current_source, then preview_current_share with those choices, '
                  'then share_current_session using exactly the returned requestId and previewHash and the same choices. '
                  'Do not choose or list a different session. Do not wait or poll after registering, and do not send any model input. '
                  'If a tool fails, stop and report its error. After successful registration end this turn with TCX_CURRENT_FINAL_MARKER and the request ID.')
        n.emit('personal_share_start', provider=args.provider)
        if args.provider == 'codex':
            native_args = [args.codex_bin, 'exec', '--json', '-m', n.MODEL, prompt]
        else:
            native_args = [n.CLI, '--print', '--output-format', 'json', '--model', n.MODEL, '--effort', 'low', '--tools', '', '--permission-mode', 'manual', '--allowedTools', ','.join('mcp__teamcross__'+x for x in current_tools), '--no-chrome', prompt]
        p = subprocess.run(native_args, env=env, cwd=repo, capture_output=True, text=True, timeout=240)
        (root/'evidence/personal-output.txt').write_text(n.clean(p.stdout))
        (root/'evidence/personal-stderr.txt').write_text(n.clean(p.stderr))
        if p.returncode: raise RuntimeError('Personal client failed: '+n.clean(p.stderr[-1500:]))
        if args.provider == 'codex':
            events = list(map(json.loads, p.stdout.splitlines()))
            source = next(x['thread_id'] for x in events if x.get('type') == 'thread.started')
            final = [x['item']['text'] for x in events if x.get('type') == 'item.completed' and x.get('item', {}).get('type') == 'agent_message'][-1]
        else:
            source = json.loads(p.stdout)['session_id']
            final = json.loads(p.stdout)['result']
        if 'TCX_CURRENT_FINAL_MARKER' not in final: raise RuntimeError('Personal client did not complete registration; see personal-output.txt')
        requests = list((a.data/'share-requests').glob('*.json'))
        assert len(requests) == 1, 'expected exactly one registered share'
        request = json.loads(requests[0].read_text())
        assert request['input']['sourceId'] == source
        request_id = request['id']
        report.update({'sourceId':source, 'shareRequestId':request_id})
        def completed_share():
            r = cli(a, 'share-status', ['--id', request_id])
            if r['state'] in ('failed', 'cancelled', 'interrupted'): raise RuntimeError('Share did not complete: '+str(r.get('error')))
            return r if r['state'] == 'ready' else None
        ready = n.wait_until(completed_share, 100)
        cid = ready['collaborationId']
        c = cli(a, 'collaborations', ['--id', cid])
        assert c['sessionId'] != source and c['provider'] == args.provider
        # Claude's zero-input fork is native in-memory state until its first
        # business turn. Do not invent a transcript or seed an extra prompt.
        if args.provider == 'codex':
            history = mcp(a, 'read_context', {'id':cid, 'kind':'history'})
            assert assistant_has(history, 'TCX_CURRENT_FINAL_MARKER'), 'fork omitted final source response'
            report['currentTurnIncluded'] = True
        report.update({'collaborationId':cid, 'sessionId':c['sessionId']})
        n.emit('current_share_ready', provider=args.provider, id=cid)

        # Generate a consumable invitation only through the dedicated tool;
        # keep secrets in memory and do not include them in evidence reports.
        invited = mcp(a, 'create_invitation', {'id':cid, 'transport':'lan'})
        invitation = invited['invitation']
        preview = cli(b, 'inspect-invitation', ['--stdin'], invitation)
        assert preview['runtimeMode'] == 'restricted'
        joined = cli(b, 'join', ['--no-open', '--stdin'], invitation)
        bid = joined['id']
        assert mcp(b, 'join_collaboration', {'invitation':invitation})['id'] == bid
        guest = mcp(b, 'get_collaboration', {'id':bid})
        assert 'invitation' not in guest
        note = mcp(b, 'add_annotation', {'id':bid, 'text':'TCX_AGENT_CLI_NOTE'})
        mcp(a, 'reply_to_annotation', {'id':cid, 'annotationId':note['id'], 'text':'TCX_AGENT_CLI_REPLY', 'requestId':str(uuid.uuid4())})
        assert 'TCX_AGENT_CLI_REPLY' in json.dumps(mcp(b, 'read_context', {'id':bid,'kind':'annotations'}))
        denied = mcp(b, 'send_input', {'id':bid, 'mode':'start', 'text':'Do not run', 'requestId':str(uuid.uuid4())}, allow_error=True)
        assert 'code' in denied
        mcp(b, 'request_input', {'id':bid, 'epoch':guest['epoch']})
        # `input` puts the action before flags, unlike other CLI commands.
        def relay(core, action, local_id):
            state = mcp(core, 'get_collaboration', {'id':local_id})
            p = subprocess.run([n.BINARY, 'input', action, '--data-dir', str(core.data), '--id', local_id, '--epoch', str(state['epoch']), '--json'], env=env, cwd=repo, capture_output=True, text=True, timeout=90)
            if p.returncode: raise RuntimeError('CLI relay failed: '+n.clean(p.stdout+p.stderr))
            return json.loads(p.stdout)
        relay(a, 'handoff', cid)
        plan = cli(b, 'open', ['--id', bid, '--client', 'tui', '--print-command'])
        assert plan['sessionId'] == c['sessionId'] and not plan['launched']
        direct = n.Terminal('direct-'+args.provider, plan['command'], env); terminals.append(direct)
        n.wait_until(lambda:a.api('collaborations/'+cid)['clientState'] == 'session_ready', 45, direct)
        direct.close()
        n.wait_until(lambda:not a.api('collaborations/'+cid)['connected'], 20)
        request_id = str(uuid.uuid4())
        send = {'id':bid, 'mode':'start', 'text':'Reply exactly TCX_AGENT_CLI_RELAY_DONE. Do not use tools.', 'requestId':request_id}
        mcp(b, 'send_input', send)
        def relay_done():
            state = a.api('collaborations/'+cid)
            if state['busy']: return False
            h = mcp(a, 'read_context', {'id':cid, 'kind':'history'})
            return assistant_has(h, 'TCX_AGENT_CLI_RELAY_DONE')
        n.wait_until(relay_done, 90)
        assert assistant_has(mcp(a, 'read_context', {'id':cid, 'kind':'history'}), 'TCX_CURRENT_FINAL_MARKER'), 'fork omitted final source response'
        report['currentTurnIncluded'] = True
        mcp(b, 'send_input', send)
        relay(b, 'return', bid)
        relay(a, 'handoff', cid)
        relay(a, 'reclaim', cid)
        report.update({'cliJoinNoBrowser':True, 'mcpAnnotationsAndReply':True, 'inputRelay':True, 'directTUI':True})
        mcp(b, 'leave_collaboration', {'id':bid})
        cli(a, 'end', ['--id',cid])
        resumed = cli(a, 'resume', ['--id',cid])
        assert resumed['sessionId'] == c['sessionId'] and not resumed['sharing']
        assert (repo/'baseline.txt').read_text() == 'TCX_AGENT_CLI_BASELINE\n'
        report['sameForkResume'] = True
        cli(a, 'end', ['--id',cid])
        n.emit('agent_cli_verified', provider=args.provider)
    except Exception as e:
        report['error'] = n.clean(type(e).__name__+': '+str(e))
        report['traceback'] = n.clean(traceback.format_exc())
        n.emit('agent_cli_failed', error=report['error'])
    finally:
        cleanup_errors = []
        for resource in list(reversed(terminals))+list(reversed(cores)):
            try: resource.close()
            except Exception as e: cleanup_errors.append(type(e).__name__+': '+str(e))
        if args.provider == 'claude' and 'env' in locals():
            try: n.stop_fixture_daemon(env, root)
            except Exception as e: cleanup_errors.append(type(e).__name__+': '+str(e))
        if cleanup_errors:
            report['cleanupErrors'] = n.clean(cleanup_errors)
            report.setdefault('error', 'Fixture cleanup failed')
        if guard:
            report['generationRequests'] = guard.generation_count()
            report['modelsObserved'] = sorted({x['model'] for x in guard.entries if x.get('model')})
            guard.close()
        (root/'evidence/summary.json').write_text(json.dumps(n.clean(report), ensure_ascii=False, indent=2))
        n.emit('complete', provider=args.provider, error=report.get('error'), fixture=str(root))
    return 1 if report.get('error') else 0


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--provider', choices=['codex','claude'], required=True)
    p.add_argument('--fixture-dir', type=pathlib.Path, required=True)
    p.add_argument('--teamcross-bin', required=True)
    p.add_argument('--codex-bin', default=shutil.which('codex'))
    p.add_argument('--codex-auth', type=pathlib.Path, default=pathlib.Path.home()/'.codex/auth.json')
    p.add_argument('--claude-bin', default=shutil.which('claude'))
    p.add_argument('--source-settings', type=pathlib.Path, default=pathlib.Path.home()/'.claude/settings.json')
    raise SystemExit(run(p.parse_args()))
