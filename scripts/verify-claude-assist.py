#!/usr/bin/env python3
"""Opt-in personal Claude Code TUI MCP acceptance on one Mac.

Uses two real Cores, separate A/B config and repository directories, the actual
native MCP installer, and only CliProxyAPI's gpt-5.6-luna. The personal test TUI
loads its user config normally (no --mcp-config/--strict-mcp-config injection).
"""
import argparse
import hashlib
import importlib.util
import json
import pathlib
import shlex
import subprocess
import sys
import time
import traceback
import uuid

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location('native_fixture', pathlib.Path(__file__).with_name('verify-claude-native.py'))
n = importlib.util.module_from_spec(spec)
spec.loader.exec_module(n)


def rows(home):
    result = []
    for path in home.glob('projects/*/*.jsonl'):
        for line in path.read_text().splitlines():
            try: result.append(json.loads(line))
            except ValueError: pass
    return result


def assistant_has(home, text):
    return any(x.get('type') == 'assistant' and text in json.dumps(x.get('message', {})) for x in rows(home))


def run(keep_seconds):
    root = n.ROOT
    guard = n.Guard()
    env = n.environment(guard)
    cores, terms = [], []
    report = {'scope': 'one Mac, two real Cores, separate personal and shared Claude sessions', 'model': n.MODEL}
    try:
        repo = root/'repo'; repo.mkdir()
        brepo = root/'b-repo'; brepo.mkdir()
        home = root/'source-home'; home.mkdir()
        bhome = root/'b-personal'; bhome.mkdir()
        def git(*args): return subprocess.check_output(['git', '-C', str(repo), *args], text=True).strip()
        git('init', '-b', 'main'); git('config', 'user.name', 'Team Cross MCP Fixture'); git('config', 'user.email', 'fixture@example.invalid')
        (repo/'baseline.txt').write_text('ONLY_ON_A_MCP_PROOF\n')
        (brepo/'baseline.txt').write_text('ONLY_ON_B_DO_NOT_READ_LOCALLY\n')
        git('add', 'baseline.txt'); git('commit', '-m', 'Dedicated Claude MCP fixture')
        routing = {k: v for k, v in env.items() if k.startswith('ANTHROPIC_')}
        allowed = ['mcp__teamcross__'+name for name in ('list_collaborations', 'get_collaboration', 'read_context', 'add_annotation', 'send_input')]
        for h in (home, bhome):
            settings = {'env': routing, 'disableAllHooks': True, 'enabledPlugins': {}, 'permissions': {'defaultMode': 'manual'}}
            (h/'settings.json').write_text(json.dumps(settings)); (h/'settings.json').chmod(0o600)
        # Trust only this newly created test directory. The product installer
        # never accepts personal project trust or alters the user's permissions.
        personal = {'hasCompletedOnboarding': True, 'lastOnboardingVersion': '2.1.268', 'autoUpdates': False, 'theme': 'light', 'hasSeenTasksHint': True,
                    'projects': {str(brepo): {'hasTrustDialogAccepted': True, 'hasCompletedProjectOnboarding': True, 'projectOnboardingSeenCount': 1, 'allowedTools': [], 'disabledMcpServers': ['fixture-unrelated']}},
                    'mcpServers': {'fixture-unrelated': {'type': 'stdio', 'command': '/usr/bin/false', 'args': []}}}
        (bhome/'.claude.json').write_text(json.dumps(personal)); (bhome/'.claude.json').chmod(0o600)
        report['cli_version'] = n.cli(env, ['--version']).strip()
        seed = json.loads(n.cli(env, ['--print', '--output-format', 'json', '--model', n.MODEL, '--effort', 'low', '--tools', '', '--strict-mcp-config', '--mcp-config', '{"mcpServers":{}}', '--no-chrome', 'Remember TCX_AUX_SOURCE. Reply exactly TCX_AUX_SOURCE_OK. Do not use tools.']))
        assert not seed.get('is_error'), n.clean(seed)
        source = seed['session_id']
        source_path = next(home.glob('projects/*/'+source+'.jsonl'))
        source_hash = hashlib.sha256(source_path.read_bytes()).hexdigest()
        a = n.Core('core-a', env); cores.append(a)
        benv = dict(env); benv['CLAUDE_CONFIG_DIR'] = str(bhome)
        b = n.Core('core-b', benv, repo=brepo); cores.append(b)
        guard.allow_generation = False
        before = guard.generation_count()
        body = {'provider': 'claude', 'sourceId': source, 'workspaceMode': 'existing', 'requestId': str(uuid.uuid4()), 'title': 'Claude 个人辅助 · MCP 实测'}
        body['previewHash'] = a.api('preview', body)['previewHash']
        c = a.api('collaborations', body); base = 'collaborations/'+c['id']
        def action(name): return a.api(base+'/action', {'action': name, 'epoch': a.api(base)['epoch']})
        joined = b.api('join', {'invitation': action('share')['invitation']}); bbase = 'collaborations/'+joined['id']
        shared_home = home
        runtime = root/'core-a/collaborations'/c['id']/'claude-runtime'
        worker_env = dict(env); worker_env['CLAUDE_CONFIG_DIR'] = str(runtime)
        def job(): return next(x for x in json.loads(n.cli(worker_env, ['agents', '--json', '--all'])) if x['id'] == c['nativeJobId'])
        report['worker_before'] = job()
        assert not b.api('info')['mcpClients']['claude']['configured']
        for _ in range(2): assert b.api('mcp/setup', {'provider': 'claude'})['ok']
        assert b.api('mcp/probe', {})['ready']
        configured = b.api('info')['mcpClients']['claude']
        assert configured['configured'] and configured['observedAt'].startswith('0001')
        saved = json.loads((bhome/'.claude.json').read_text())
        assert saved['mcpServers']['fixture-unrelated'] == personal['mcpServers']['fixture-unrelated'] and saved['theme'] == 'light'
        entry = saved['mcpServers']['teamcross']
        assert entry['command'] == n.BINARY and entry['args'] == ['mcp', '--data-dir', str(root/'core-b')]
        assert not list(bhome.glob('projects/*/*.jsonl'))
        plan = b.api(bbase+'/assist', {'provider': 'claude', 'client': 'tui', 'launch': False})
        assert str(brepo) in plan['command'] and str(bhome) in plan['command']
        assert not any(x in plan['command'] for x in ('--resume', '--model', '--mcp-config', '--strict-mcp-config', ' attach '))
        assert guard.generation_count() == before
        report['native_setup_idempotent'] = True; report['unrelated_config_preserved'] = True; report['setup_and_probe_generations'] = 0
        n.emit('personal_setup_verified', command=plan['command'], root=str(root))

        guard.allow_generation = True
        # Test-only tool restriction and exact allowlist: no local shell/file
        # tools and no blanket permission bypass. Product launch adds none of it.
        flags = ['--model', n.MODEL, '--effort', 'low', '--tools', '', '--permission-mode', 'manual', '--allowedTools', ','.join(allowed), '--no-chrome']
        t = n.Terminal('personal-claude-b', plan['command']+' '+shlex.join(flags), benv); terms.append(t)
        n.wait_until(lambda: 'gpt-5.6-luna' in t.read(.2).lower(), 30, t)
        t.read(2)
        def turn(prompt, marker, seconds=100):
            t.prompt(prompt)
            n.wait_until(lambda: assistant_has(bhome, marker), seconds, t)
            t.read(.8)
            n.emit('personal_turn', marker=marker)
        turn(f'Use Team Cross MCP tools to list collaborations, then read_context for collaboration {joined["id"]}, kind=file, path=baseline.txt. Also read its history and annotations. Use no other tools and do not send input. Reply TCX_READ_DONE and include the file content.', 'TCX_READ_DONE')
        text = json.dumps(rows(bhome))
        assert 'ONLY_ON_A_MCP_PROOF' in text and 'ONLY_ON_B_DO_NOT_READ_LOCALLY' not in text
        assert 'TCX_AUX_SOURCE_OK' in text
        actual = b.api('info')
        assert not actual['mcpClients']['claude']['observedAt'].startswith('0001')
        assert actual['mcpClients']['codex']['observedAt'].startswith('0001')
        report['personal_read_only_before_handoff'] = True; report['client_observation_separate'] = True
        denied_id = str(uuid.uuid4())
        turn(f'Use Team Cross add_annotation for collaboration {joined["id"]}, text TCX_AUX_ANNOTATION. Then attempt send_input once with id={joined["id"]}, mode=start, requestId={denied_id}, text="Reply exactly TCX_MUST_NOT_RUN. Do not use tools." This is a permission-denial test: if it fails, do not retry or work around it. Reply TCX_DENIED_DONE and the result.', 'TCX_DENIED_DONE')
        assert any(x['text'] == 'TCX_AUX_ANNOTATION' for x in a.api(base+'/context?kind=annotations')['annotations'])
        assert not any('TCX_MUST_NOT_RUN' in json.dumps(x.get('message', {})) for x in rows(shared_home))
        results = [v for x in rows(bhome) for v in x.get('message', {}).get('content', []) if isinstance(v, dict) and v.get('type') == 'tool_result']
        assert any(v.get('is_error') for v in results), 'no native MCP permission-denial result'
        report['annotation_from_personal_client'] = True; report['non_writer_send_denied'] = True
        action('handoff')
        request_id = str(uuid.uuid4())
        dispatch = f'Use Team Cross send_input exactly once: id={joined["id"]}, mode=start, requestId={request_id}, text="Reply exactly TCX_AUX_SHARED_OK. Do not use tools." Do not use other tools. Reply TCX_DISPATCH_DONE after the tool returns.'
        turn(dispatch, 'TCX_DISPATCH_DONE')
        n.wait_until(lambda: assistant_has(shared_home, 'TCX_AUX_SHARED_OK'), 60, t)
        n.wait_until(lambda: not a.api(base)['busy'], 20, t)
        turn(dispatch.replace('TCX_DISPATCH_DONE', 'TCX_DUPLICATE_DONE'), 'TCX_DUPLICATE_DONE')
        count = sum(x.get('type') == 'user' and 'TCX_AUX_SHARED_OK' in json.dumps(x.get('message', {})) for x in rows(shared_home))
        assert count == 1, count
        report['writer_send_on_a'] = True; report['same_request_deduplicated'] = True
        report['worker_after'] = job()
        assert report['worker_after']['pid'] == report['worker_before']['pid']
        personal_ids = sorted({x.get('sessionId') for x in rows(bhome) if x.get('sessionId')})
        assert len(personal_ids) == 1 and personal_ids[0] not in (c['sessionId'], source)
        report['personal_session_id'] = personal_ids[0]; report['shared_session_id'] = c['sessionId']
        report['personal_and_shared_sessions_distinct'] = True
        # The personal TUI remains open while the same participant directly
        # attaches to the existing A worker, proving these are separate modes.
        direct = b.api(bbase+'/open', {'client': 'tui', 'launch': False})
        dt = n.Terminal('direct-claude-b', direct['command'], benv); terms.append(dt)
        n.wait_until(lambda: a.api(base)['clientState'] == 'session_ready', 30, dt)
        assert 'TCX_AUX_SHARED_OK' in dt.read(1)
        dt.close(); n.wait_until(lambda: not a.api(base)['connected'], 15, t)
        assert job()['pid'] == report['worker_before']['pid']
        report['direct_and_auxiliary_same_target'] = True
        assert source_hash == hashlib.sha256(source_path.read_bytes()).hexdigest()
        report['source_unchanged'] = True
        report['ui_url'] = b.url; report['ui_detail'] = b.url+'/#/'+bbase
        (root/'evidence/preview.json').write_text(json.dumps(n.clean(report), ensure_ascii=False, indent=2))
        n.emit('acceptance_verified', url=b.url, detail=report['ui_detail'], root=str(root))
        if keep_seconds:
            end = time.monotonic()+keep_seconds
            n.emit('preview_available', seconds=keep_seconds, stop_file=str(root/'finish-preview'))
            while time.monotonic() < end and not (root/'finish-preview').exists(): t.read(.2)
        action('end')
        turn(f'Use Team Cross read_context once for collaboration {joined["id"]}, kind=file, path=baseline.txt. Sharing has ended; do not retry or use other tools if refused. Reply TCX_REVOKED_DONE with the result.', 'TCX_REVOKED_DONE')
        latest = [v for x in rows(bhome) for v in x.get('message', {}).get('content', []) if isinstance(v, dict) and v.get('type') == 'tool_result'][-1]
        assert latest.get('is_error'), 'ended share remained readable through personal MCP'
        report['ended_share_access_revoked'] = True
    except Exception as e:
        report['error'] = n.clean(type(e).__name__+': '+str(e)); report['traceback'] = n.clean(traceback.format_exc())
        n.emit('acceptance_failed', error=report['error'])
    finally:
        cleanup_errors = []
        for resource in list(reversed(terms))+list(reversed(cores)):
            try: resource.close()
            except Exception as e: cleanup_errors.append(type(e).__name__+': '+str(e))
        fixture_envs = {item['CLAUDE_CONFIG_DIR']: item for item in (env, locals().get('benv', env))}
        for fixture_env in fixture_envs.values():
            try: n.stop_fixture_daemon(fixture_env, root)
            except Exception as e: cleanup_errors.append(type(e).__name__+': '+str(e))
        if cleanup_errors:
            report['cleanup_errors'] = n.clean(cleanup_errors)
            report.setdefault('error', 'Test resource cleanup failed')
        report['generation_requests'] = guard.generation_count()
        report['models_observed'] = sorted({x['model'] for x in guard.entries if x.get('model')})
        report['requests'] = guard.entries
        guard.close()
        (root/'evidence/summary.json').write_text(json.dumps(n.clean(report), ensure_ascii=False, indent=2))
        n.emit('complete', error=report.get('error'), generations=report['generation_requests'], models=report['models_observed'])
    return 1 if report.get('error') else 0


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--fixture-dir', type=pathlib.Path, required=True)
    p.add_argument('--teamcross-bin', required=True)
    p.add_argument('--claude-bin', required=True)
    p.add_argument('--source-settings', type=pathlib.Path, default=pathlib.Path.home()/'.claude/settings.json')
    p.add_argument('--keep-preview-seconds', type=int, default=0)
    args = p.parse_args()
    n.ROOT = args.fixture_dir.resolve(); n.CLI = str(pathlib.Path(args.claude_bin).resolve()); n.BINARY = str(pathlib.Path(args.teamcross_bin).resolve())
    if n.ROOT.exists() and any(n.ROOT.iterdir()): p.error('Choose a new, empty dedicated fixture directory')
    n.ROOT.mkdir(parents=True, exist_ok=True); n.ROOT.chmod(0o700); (n.ROOT/'evidence').mkdir()
    n.CREDS = json.loads(args.source_settings.read_text())['env']
    n.TOKEN = n.CREDS.get('ANTHROPIC_AUTH_TOKEN') or n.CREDS.get('ANTHROPIC_API_KEY')
    if not n.TOKEN: p.error('CliProxyAPI authentication is missing')
    raise SystemExit(run(max(0, min(args.keep_preview_seconds, 1800))))
