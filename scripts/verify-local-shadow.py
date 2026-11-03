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


INPUT_MCP = r'''
import json, pathlib, sys, time
repo = pathlib.Path(__file__).parent / "repo"
for line in sys.stdin:
    q = json.loads(line)
    if "id" not in q: continue
    method = q.get("method")
    if method == "initialize":
        out = {"protocolVersion": q["params"]["protocolVersion"], "capabilities": {"tools": {}}, "serverInfo": {"name": "input-fixture", "version": "1"}}
    elif method == "tools/list":
        out = {"tools": [{"name": "fixture_hold_"+phase, "description": "Read-only isolated fixture gate. Wait for the "+phase+" gate, then return evidence. No shell needed.", "annotations": {"readOnlyHint": True}, "inputSchema": {"type": "object", "properties": {}, "additionalProperties": False}} for phase in ("input", "work")]}
    elif method == "tools/call" and q["params"]["name"] in ("fixture_hold_input", "fixture_hold_work"):
        phase = q["params"]["name"].removeprefix("fixture_hold_")
        (repo / (phase+"-entered")).write_text("entered")
        deadline = time.monotonic()+180
        while not (repo / (phase+"-release")).exists() and time.monotonic()<deadline: time.sleep(.1)
        body = "CORE_WORK_EVIDENCE_782" if (repo / (phase+"-release")).exists() else "FIXTURE_TIMEOUT"
        out = {"content": [{"type": "text", "text": body}]}
    else:
        print(json.dumps({"jsonrpc": "2.0", "id": q["id"], "error": {"code": -32601, "message": "Unknown fixture request"}}), flush=True)
        continue
    print(json.dumps({"jsonrpc": "2.0", "id": q["id"], "result": out}), flush=True)
'''


def message_input_scenarios(root, a, b, c, spaces, sid, view, report):
    from concurrent.futures import ThreadPoolExecutor
    def send(core, space, text):
        mid = str(uuid.uuid4())
        core.api('collaborations/'+space+'/shadows/send', {'shadowId': sid, 'message': {'id': mid, 'text': text}})
        return mid
    def reply(mid, marker):
        def seen():
            current = view(c, spaces[2])['shadows'][0]
            if current.get('error'): raise RuntimeError(current['error'])
            return current if any(m.get('replyTo')==mid and marker in m['text'] for m in current['messages'] if m['role']=='assistant') else None
        return n.wait_until(seen, 180)
    author_a = view(a, spaces[0])['selfId']
    author_b = view(b, spaces[1])['selfId']
    mid_a = send(a, spaces[0], '隔离验证：先调用 fixture_hold_input 等待测试门控；返回后用 shadow_remember 明确记住我的个人饮品偏好为 tea，scope=member；用 shadow_reply 对本条回复 CORE_A_291。不使用 shell、不创建任务。')
    n.wait_until(lambda:(root/'repo/input-entered').exists(), 180)
    with ThreadPoolExecutor(max_workers=2) as pool:
        pending_b = pool.submit(send, b, spaces[1], '请明确记住我的个人饮品偏好为 coffee，scope=member。用 shadow_reply 回复本条 CORE_B_364。不要改变其他人的偏好，不创建任务。')
        pending_c = pool.submit(send, c, spaces[2], '运行中的独立短问：3+4 是多少？用 shadow_reply 对我这条回复 CORE_C_725 和答案，不改变任何记忆。')
        mid_b, mid_c = pending_b.result(), pending_c.result()
    ledger = b.data/'shadows'/sid/'shadow.json'
    def accepted():
        record = json.loads(ledger.read_text())
        for run in record['runs'].values():
            batches = run.get('inputBatches', [])
            ids = {i['sourceId'] for batch in batches if batch['state']=='accepted' for i in batch['inputs']}
            if {mid_a, mid_b, mid_c} <= ids and any(x['method']=='turn/steer' and x['state']=='accepted' for x in batches): return run
        return None
    accepted_run = n.wait_until(accepted, 40)
    (root/'repo/input-release').write_text('release')
    for mid, marker in ((mid_a,'CORE_A_291'),(mid_b,'CORE_B_364'),(mid_c,'CORE_C_725')): v=reply(mid,marker)
    assert any(m['subjectId']==author_a and 'tea' in m['text'].lower() for m in v['memories'])
    assert any(m['subjectId']==author_b and 'coffee' in m['text'].lower() for m in v['memories'])
    for m in v['messages']:
        if m['role']!='assistant': continue
        for mid, marker in ((mid_a,'CORE_A_291'),(mid_b,'CORE_B_364'),(mid_c,'CORE_C_725')):
            if marker in m['text']: assert m['replyTo']==mid
    assert not v['jobs']
    n.wait_until(lambda:not view(c,spaces[2])['shadows'][0]['busy'],40)
    work_request=send(a,spaces[0], '创建且只创建一个独立 Work：先用 report continue 发布 CORE_WORK_PROGRESS_649；再调用 fixture_hold_work 读取证据；最后用 report complete 提交包含原样 CORE_WORK_EVIDENCE_782 的结果和 criteriaChecks。主会话不要调用慢工具，派发后用 shadow_reply 回复本条 CORE_WORK_ACK_860 并结束，不等待 Work。不使用 shell。')
    reply(work_request,'CORE_WORK_ACK_860')
    n.wait_until(lambda:(root/'repo/work-entered').exists(),180)
    reply(work_request,'CORE_WORK_PROGRESS_649')
    quick=send(c,spaces[2], 'Work 还在执行时的独立短问：8+9 是多少？用 shadow_reply 对本条回复 CORE_QUICK_483 和答案，不等 Work，也不创建任务。')
    v=reply(quick,'CORE_QUICK_483')
    assert len(v['jobs'])==1 and v['jobs'][0]['state']=='running' and v['jobs'][0]['sourceId']==work_request
    (root/'repo/work-release').write_text('release')
    v=reply(work_request,'CORE_WORK_EVIDENCE_782')
    n.wait_until(lambda:view(c,spaces[2])['shadows'][0]['jobs'][0]['state']=='completed',40)
    def settled():
        record=json.loads(ledger.read_text())
        return record if all(r['state'] not in ('prepared','submitted','unknown') for r in record['runs'].values()) else None
    record=n.wait_until(settled,60)
    assert not any(r['kind']=='result' for r in record['runs'].values())
    for core,space in zip((a,b,c),spaces):
        n.wait_until(lambda: any(m.get('replyTo')==work_request and 'CORE_WORK_EVIDENCE_782' in m['text'] for m in view(core,space)['shadows'][0]['messages']),20)
    report.update(multiMemberNativeSteer=True, exactAuthorsAndReplyTargets=True, chatRepliedBeforeWorkGateRelease=True, directProgressAndCompletion=True, noMainResultRewrite=True)
    report['inputTurn']={'threadId':accepted_run['threadId'],'turnId':accepted_run['turnId'],'acceptedBatchCount':len(accepted_run['inputBatches'])}
    n.emit('message_inputs_and_work_chat_verified')


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


def self_mention_scenarios(root, a, b, spaces, sid, view, report):
    space, joined = spaces
    prefix = 'collaborations/'+joined+'/'
    owner = view(b, joined)['selfId']
    mention = [{'memberId':owner, 'label':'Self', 'startOffset':0, 'endOffset':5}]
    n.wait_until(lambda:not view(b, joined)['shadows'][0]['busy'], 40)
    b.api('shadows/'+sid+'/action', {'action':'pause'})
    before = b.api(prefix+'activity')['unread']
    note = b.api(prefix+'annotations', {'requestId':str(uuid.uuid4()), 'text':'@Self 这是本人交给分身的测试：用 reply_to_annotation 在此根讨论回复 SELF_ROOT_417 和 3+4 的答案。不要创建 Work，不修改文件。', 'mentions':mention})
    aid = note['id']
    def events():
        for path in (b.data/'shadow-activity').glob('*/attention.json'):
            record = json.loads(path.read_text())
            if record['spaceId']==joined:
                return [e for e in record['events'].values() if e['signal']['source'].get('annotationId')==aid and 'mention' in e['signal'].get('reasons', [])]
        return []
    queued = n.wait_until(lambda:events() or None, 30)
    assert len(queued)==1 and queued[0]['state']=='queued'
    assert queued[0]['signal']['actor']['memberId']==owner
    assert 'own_source_change' not in queued[0]['signal']['reasons']
    assert b.api(prefix+'activity')['unread']==before
    b.api('shadows/'+sid+'/action', {'action':'resume'})
    def answer(marker):
        notes = a.api('collaborations/'+space+'/context?kind=annotations')['annotations']
        note = next(x for x in notes if x['id']==aid)
        return next((r for r in note.get('replies', []) if r.get('actor', {}).get('kind')=='shadow' and marker in r['text']), None)
    first = n.wait_until(lambda:answer('SELF_ROOT_417'), 180)
    assert first['actor']['memberId']==owner
    request = {'annotationId':aid, 'requestId':str(uuid.uuid4()), 'text':'@Self 再交给分身一个短问题：用 reply_to_annotation 回复本条具体回复，包含 SELF_REPLY_836 和 8+9 的答案。不要创建 Work，不修改文件。', 'mentions':mention}
    note = b.api(prefix+'annotation-replies', request)
    own_reply = next(r for r in note['replies'] if r.get('requestId')==request['requestId'])
    b.api(prefix+'annotation-replies', request)
    second = n.wait_until(lambda:answer('SELF_REPLY_836'), 180)
    assert second['replyTo']=={'kind':'reply', 'id':own_reply['id']}
    n.wait_until(lambda:not view(b, joined)['shadows'][0]['busy'], 40)
    assert len(events())==2 and not view(b, joined)['shadows'][0]['jobs']
    report.update(selfMentionRootAndReply=True, selfMentionQueuedWhilePaused=True, selfMentionNoOwnUnread=True, selfMentionExactRemoteAuthor=True, selfMentionNoWorkOrDuplicate=True)
    (root/'evidence/self-mentions.json').write_text(json.dumps({'annotationId':aid,'events':events(),'replies':[first,second]}, indent=2)+'\n')
    n.emit('self_mention_delegation_verified')


def run(args):
    root = args.fixture_dir.resolve()
    root.mkdir(mode=0o700)
    (root/'evidence').mkdir(); (root/'repo').mkdir(); (root/'codex-home').mkdir()
    subprocess.run(['git','init','-q','--initial-branch=main',str(root/'repo')], check=True)
    home = root/'codex-home'
    (home/'auth.json').symlink_to(args.codex_auth.resolve())
    (home/'config.toml').write_text('model = "gpt-5.6-luna"\nmodel_reasoning_effort = "medium"\napproval_policy = "on-request"\nsandbox_mode = "workspace-write"\n')
    if args.message_inputs:
        import sys
        (root/'input-mcp.py').write_text(INPUT_MCP)
        with (home/'config.toml').open('a') as config:
            config.write('[mcp_servers.input_fixture]\ncommand = '+json.dumps(sys.executable)+'\nargs = ['+json.dumps(str(root/'input-mcp.py'))+']\ntool_timeout_sec = 240\n')
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
        if args.message_inputs:
            message_input_scenarios(root,a,b,c,(s['id'],jb,jc),sid,view,report)
        if args.self_mentions:
            self_mention_scenarios(root,a,b,(s['id'],jb),sid,view,report)
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
        (root/'evidence/stdio-shadow-read.json').write_text(json.dumps(result,indent=2)+'\n')
        (root/'evidence/report.json').write_text(json.dumps(report,indent=2)+'\n')
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
    p.add_argument('--message-inputs',action='store_true',help='Also verify real three-member steer attribution and foreground chat during a gated Work')
    p.add_argument('--self-mentions',action='store_true',help='Verify owner mentions in roots and replies, paused receipt, exact remote authors and no duplicate Work')
    run(p.parse_args())
