#!/usr/bin/env python3
"""Isolated Luna receiver: approval, interrupt, release and same-thread handoff.

Uses a second real WebSocket client for approvals, never the Core respond route.
Only this fixture's receiver, named tools and request IDs may be approved.
A second independent app-server continues the persisted thread; Desktop opening
is captured by a test command and does not automate the personal Desktop UI.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import queue
import shlex
import shutil
import socket
import urllib.request
import subprocess
import threading
import time
import uuid

spec = importlib.util.spec_from_file_location('native_fixture', Path(__file__).with_name('verify-claude-native.py'))
n = importlib.util.module_from_spec(spec)
spec.loader.exec_module(n)


class NativeClient:
    def __init__(self, endpoint, env):
        source = '''const ws = new WebSocket(process.argv[1]);
ws.onopen = () => { console.log(JSON.stringify({ready:true}));
require('node:readline').createInterface({input:process.stdin}).on('line', line => ws.send(line)); };
ws.onmessage = e => console.log(e.data);
ws.onerror = () => process.exit(1);
ws.onclose = () => process.exit(0);
process.stdin.on('end', () => ws.close());'''
        self.p = subprocess.Popen([shutil.which('node'), '-e', source, endpoint], env=env,
                                  stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
        self.responses, self.requests, self.events, self.seq = queue.Queue(), {}, [], 100
        assert json.loads(self.p.stdout.readline()) == {'ready': True}
        def read():
            for line in self.p.stdout:
                message = json.loads(line)
                if 'method' in message and 'id' in message:
                    self.requests[str(message['id'])] = message
                elif 'id' in message:
                    self.responses.put(message)
                elif 'method' in message:
                    self.events.append(message)
        threading.Thread(target=read, daemon=True).start()
        self.rpc('initialize', {'clientInfo': {'name': 'teamcross_receiver_fixture', 'version': '1'}, 'capabilities': {'experimentalApi': True}})
        self.send({'method': 'initialized'})

    def send(self, message):
        self.p.stdin.write(json.dumps(message)+'\n'); self.p.stdin.flush()

    def rpc(self, method, params):
        self.seq += 1
        self.send({'id': self.seq, 'method': method, 'params': params})
        message = self.responses.get(timeout=30)
        assert message.get('id') == self.seq and 'result' in message, message
        return message['result']

    def close(self):
        self.p.stdin.close()
        try: self.p.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.p.terminate(); self.p.wait(timeout=5)


def run(args):
    root = args.fixture_dir.resolve()
    if root.exists() and any(root.iterdir()): raise ValueError('Use a fresh fixture directory')
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    for name in ('evidence', 'repo', 'codex-home', 'bin', 'Desktop.app'): (root/name).mkdir()
    opener = root/'bin/open'
    opener.write_text('#!/bin/sh\nprintf \'%s\\n\' "$@" > "$TEAMCROSS_OPEN_CAPTURE"\n')
    opener.chmod(0o700)
    native_home = root/'codex-home'
    (native_home/'auth.json').symlink_to(args.codex_auth.resolve())
    (native_home/'config.toml').write_text('model="gpt-5.6-luna"\nmodel_reasoning_effort="low"\n[features]\nmemories=false\nplugins=false\napps=false\nmulti_agent=false\n')
    env = {k:v for k,v in os.environ.items() if not k.startswith(('CODEX_', 'CLAUDE_', 'ANTHROPIC_')) and k != 'CLAUDECODE'}
    env.update({'CODEX_HOME': str(native_home), 'TERM': 'xterm-256color'})
    env.update({'PATH': str(root/'bin')+os.pathsep+env['PATH'], 'TEAMCROSS_OPEN_CAPTURE': str(root/'evidence/desktop-open.txt')})
    binary = str(args.codex_bin.resolve())
    n.ROOT, n.BINARY, n.CLI, n.TOKEN = root, str(args.teamcross_bin.resolve()), '/usr/bin/false', ''
    report = {'scope': 'single Mac, isolated Core/home/workspace, real native WebSocket client', 'model': n.MODEL,
              'nativeVersion': subprocess.check_output([binary, '--version'], env=env, text=True).strip()}
    core = client = personal = personal_process = None
    try:
        core = n.Core('core', env)
        core.api('settings', {'binary': binary, 'claudeBinary': '/usr/bin/false', 'desktopApp': str(root/'Desktop.app')})
        space = core.api('spaces', {'requestId': str(uuid.uuid4()), 'title': '接收会话状态回归'})
        sid = space['id']; base = 'collaborations/'+sid+'/workbench/'
        receiver = core.api(base+'create-receiver', {'requestId': str(uuid.uuid4()), 'name': '审批与停止测试助手'})
        assert receiver['model'] == n.MODEL, receiver['model']
        receiver_path = 'space-receivers/'+receiver['id']
        empty_open = core.api(receiver_path+'/action', {'action': 'open-desktop'}, allow_error=True)
        assert 'error' in empty_open and not (root/'evidence/desktop-open.txt').exists(), empty_open
        report['emptyHistoryOpenRejected'] = True
        empty_pause = core.api(receiver_path+'/action', {'action': 'pause'}, allow_error=True)
        assert 'error' in empty_pause, empty_pause
        report['emptyHistoryReleaseRejected'] = True
        endpoint = shlex.split(receiver['command'])[-1]
        client = NativeClient(endpoint, env)
        target = n.wait_until(lambda: next((t for t in core.api(base+'view')['targets'] if t['available']), None), 30)
        request_id = str(uuid.uuid4())
        core.api(base+'send', {'requestId': request_id, 'targetId': target['id'], 'references': [], 'intent': 'analyze',
                              'instruction': 'This is a dedicated lifecycle test. Read this request and then call finish_agent_request with status completed and summary exactly NATIVE_APPROVAL_DONE. Do not use shell, edit files, send other requests or reply to annotations. Empty references are intentional.'})
        # A brand-new, empty native thread may not have a persisted rollout yet.
        # Attach once its first authorized turn has actually been started.
        n.wait_until(lambda: core.api(receiver_path+'/events')['busy'], 30)
        resumed = client.rpc('thread/resume', {'threadId': receiver['sessionId'], 'excludeTurns': True})
        assert resumed['thread']['id'] == receiver['sessionId'] and resumed['model'] == n.MODEL
        approved, previewed = set(), False
        def completed():
            nonlocal previewed
            events = core.api(receiver_path+'/events')
            for approval in events['approvals']:
                key = str(approval['id'])
                if key in approved: continue
                native = client.requests.get(key)
                if not native: continue
                p = native.get('params', {})
                tool = p.get('_meta', {}).get('tool_params', {})
                allowed = any('"'+name+'"' in p.get('message', '') for name in ('read_agent_request', 'finish_agent_request'))
                assert native['method'] == 'mcpServer/elicitation/request' and p.get('threadId') == receiver['sessionId'] and p.get('serverName') == 'teamcross_annotations' and allowed and tool.get('requestId') == request_id, 'Unexpected native approval'
                if args.preview_seconds and not previewed:
                    previewed = True
                    (root/'preview.json').write_text(json.dumps({'url': core.url, 'spaceId': sid, 'receiverId': receiver['id'], 'nativeThreadId': receiver['sessionId']}))
                    n.emit('preview_pending_approval', url=core.url, spaceId=sid, continueFile=str(root/'continue-native'))
                    deadline = time.monotonic()+args.preview_seconds
                    while time.monotonic() < deadline and not (root/'continue-native').exists(): time.sleep(.25)
                client.send({'id': approval['id'], 'result': {'action': 'accept', 'content': {}}})
                approved.add(key)
            result = core.api(base+'request?requestId='+request_id)
            if result['state'] == 'failed': raise RuntimeError(result)
            return result if result['state'] == 'completed' else None
        result = n.wait_until(completed, 180+args.preview_seconds)
        assert result['summary'] == 'NATIVE_APPROVAL_DONE' and approved, result
        n.wait_until(lambda: not core.api(receiver_path+'/events')['busy'] and not core.api(receiver_path+'/events')['approvals'], 30)
        n.wait_until(lambda: core.api(base+'view')['targets'][0]['available'], 30)
        report['nativeApprovalsResolved'] = len(approved)
        report['availableAfterNativeCompletion'] = True
        interrupted_id = str(uuid.uuid4())
        core.api(base+'send', {'requestId': interrupted_id, 'targetId': target['id'], 'references': [], 'intent': 'analyze',
                              'instruction': 'Dedicated cancellation test. Read the request, then finish with summary INTERRUPT_FIXTURE. Do not use other tools.'})
        current = n.wait_until(lambda: next((r for r in core.api('space-receivers?spaceId='+sid) if r.get('activeTurnId')), None), 30)
        stop = {'action': 'interrupt', 'turnId': current['activeTurnId'], 'requestId': str(uuid.uuid4())}
        core.api(receiver_path+'/action', stop)
        def stopped():
            e = core.api(receiver_path+'/events')
            return e if not e['busy'] and not e['approvals'] and any(v['method'] == 'turn/completed' and v['params']['turn']['id'] == stop['turnId'] and v['params']['turn']['status'] == 'interrupted' for v in e['events']) else None
        events = n.wait_until(stopped, 30)
        core.api(receiver_path+'/action', stop)
        report['exactTurnInterrupted'] = report['duplicateStopNotReplayed'] = True
        # Release the dedicated worker, keeping the thread, directory and pairing.
        core.api(receiver_path+'/action', {'action': 'pause'})
        def released():
            r = core.api('space-receivers?spaceId='+sid)[0]
            return r if r['receivingPaused'] and not r['releasePending'] and not r['online'] else None
        n.wait_until(released, 30)
        client.close(); client = None
        n.wait_until(lambda: not core.api(base+'view')['targets'][0]['available'], 30)
        opened = core.api(receiver_path+'/action', {'action': 'open-desktop'})
        assert opened['sessionId'] == receiver['sessionId']
        assert (root/'evidence/desktop-open.txt').read_text().splitlines() == ['-a', str(root/'Desktop.app'), 'codex://threads/'+receiver['sessionId']]
        report['desktopOpenArgumentsCaptured'] = True
        # A separate app-server owns the persisted thread, like a personal native
        # client. This proves the real writer-lock boundary, not Desktop UI behavior.
        listener = socket.socket(); listener.bind(('127.0.0.1', 0))
        endpoint = 'ws://127.0.0.1:'+str(listener.getsockname()[1]); listener.close()
        personal_log = (root/'evidence/personal-native.log').open('w')
        personal_process = subprocess.Popen([binary, 'app-server', '--listen', endpoint], env=env, cwd=root/'repo', stdout=personal_log, stderr=personal_log)
        personal_log.close()
        def ready():
            try:
                with urllib.request.urlopen(endpoint.replace('ws:', 'http:')+'/readyz', timeout=1) as r: return r.status == 200
            except OSError: return False
        n.wait_until(ready, 15)
        personal = NativeClient(endpoint, env)
        personal_thread = personal.rpc('thread/resume', {'threadId': receiver['sessionId'], 'excludeTurns': True})
        assert personal_thread['thread']['id'] == receiver['sessionId'] and personal_thread['model'] == n.MODEL
        conflict = core.api(receiver_path+'/action', {'action': 'start'}, allow_error=True)
        assert conflict.get('code') == 'receiver_resume_failed', conflict
        (root/'evidence/occupied.json').write_text(json.dumps(conflict, ensure_ascii=False, indent=2))
        assert released(), 'failed resume changed pause or identity'
        report['occupiedResumeRejected'] = True
        marker = 'HANDOFF_'+uuid.uuid4().hex[:12]
        turn = personal.rpc('turn/start', {'threadId': receiver['sessionId'], 'model': n.MODEL, 'input': [{'type':'text', 'text':'This is a dedicated handoff test. Remember the private marker '+marker+'. Reply exactly PERSONAL_CONTINUATION_SAVED. Do not call tools.'}]})
        personal_turn = turn['turn']['id']
        def personal_done():
            return any(e['method'] == 'turn/completed' and e['params']['turn']['id'] == personal_turn and e['params']['turn']['status'] == 'completed' for e in personal.events)
        n.wait_until(personal_done, 180)
        history = personal.rpc('thread/turns/list', {'threadId': receiver['sessionId'], 'limit': 20, 'itemsView': 'full'})
        assert marker in json.dumps(history) and 'PERSONAL_CONTINUATION_SAVED' in json.dumps(history)
        turns_before = len(history['data'])
        personal.close(); personal = None
        personal_process.terminate(); personal_process.wait(timeout=15); personal_process = None
        restored = core.api(receiver_path+'/action', {'action': 'start'})
        assert restored['sessionId'] == receiver['sessionId'] and restored['pairingId'] == receiver['pairingId'] and restored['online'] and not restored['receivingPaused']
        client = NativeClient(shlex.split(restored['command'])[-1], env)
        resumed_history = client.rpc('thread/turns/list', {'threadId': receiver['sessionId'], 'limit': 20, 'itemsView': 'full'})
        assert len(resumed_history['data']) == turns_before and marker in json.dumps(resumed_history), 'resume lost history or started input'
        report['sameThreadAndPairingRestored'] = report['personalHistoryRetained'] = report['resumeDidNotReplayInput'] = True
        n.wait_until(lambda: core.api(base+'view')['targets'][0]['available'], 30)
        request_id = str(uuid.uuid4())
        approved = set()
        core.api(base+'send', {'requestId': request_id, 'targetId': target['id'], 'references': [], 'intent': 'analyze',
            'instruction': 'Dedicated handoff verification. Read this request, then finish_agent_request with status completed and summary exactly the private HANDOFF marker from the earlier personal user turn in this same conversation. The marker is intentionally not repeated here. Do not call other tools.'})
        client.rpc('thread/resume', {'threadId': receiver['sessionId'], 'excludeTurns': True})
        n.wait_until(lambda: core.api(receiver_path+'/events')['approvals'], 60)
        pending = core.api(receiver_path+'/action', {'action': 'pause'})
        assert pending['releasePending'] and pending['online']
        report['pauseWaitedForNativeApproval'] = True
        def handoff_completed():
            # The first request's helper enforces the exact current request and
            # receiver identities before approving either native tool.
            return completed()
        result = n.wait_until(handoff_completed, 180)
        assert result['summary'] == marker, result
        n.wait_until(released, 30)
        report['modelUsedPersonalContinuation'] = report['releasedAfterApprovedTurn'] = True
        report['desktopSidebarRefresh'] = 'not tested; Desktop UI was not automated'
        (root/'evidence'/'events.json').write_text(json.dumps(core.api(receiver_path+'/events'), ensure_ascii=False, indent=2)+'\n')
        report['passed'] = True
        n.emit('receiver_lifecycle_passed', **report)
    finally:
        if personal: personal.close()
        if personal_process:
            personal_process.terminate(); personal_process.wait(timeout=15)
        if client: client.close()
        if core: core.close()
        (root/'evidence'/'report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--fixture-dir', type=Path, required=True)
    parser.add_argument('--teamcross-bin', type=Path, required=True)
    parser.add_argument('--codex-bin', type=Path, required=True)
    parser.add_argument('--codex-auth', type=Path, default=Path.home()/'.codex/auth.json')
    parser.add_argument('--preview-seconds', type=int, default=0)
    run(parser.parse_args())
