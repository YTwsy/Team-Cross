#!/usr/bin/env python3
"""Three Cores on one Mac; isolated Luna receivers, real MCP and TLS membership.

This fixture creates no shared execution or source fork. It approves only named
Team Cross calls for its exact receivers. It never resumes personal sessions.
"""
import argparse
import datetime
import importlib.util
import json
import os
import pathlib
import shutil
import subprocess
import traceback
import uuid

spec = importlib.util.spec_from_file_location('space_native_fixture', pathlib.Path(__file__).with_name('verify-claude-native.py'))
n = importlib.util.module_from_spec(spec)
spec.loader.exec_module(n)


def run(args):
    root = args.fixture_dir.resolve()
    if root.exists() and any(root.iterdir()):
        raise ValueError('Choose a new empty fixture directory')
    root.mkdir(parents=True, exist_ok=True); root.chmod(0o700)
    (root/'evidence').mkdir(); (root/'repo').mkdir()
    n.ROOT, n.BINARY, n.CLI, n.TOKEN = root, str(args.teamcross_bin.resolve()), '/usr/bin/false', ''
    binary = shutil.which(args.codex_bin) or args.codex_bin
    cores, receivers, request_ids = [], [], set()
    approved = set()
    report = {'scope': 'one Mac, three Cores, independent native Codex receivers and TLS membership', 'model': n.MODEL}
    try:
        home = root/'codex-home'; home.mkdir()
        (home/'auth.json').symlink_to(args.codex_auth.resolve())
        (home/'config.toml').write_text('model="'+n.MODEL+'"\nmodel_reasoning_effort="low"\n[features]\nmemories=false\nplugins=false\nmulti_agent=false\n')
        env = {k:v for k,v in os.environ.items() if not k.startswith(('CODEX_', 'CLAUDE_', 'ANTHROPIC_')) and k != 'CLAUDECODE'}
        env.update({'CODEX_HOME':str(home), 'TERM':'xterm-256color'})
        report['nativeVersion'] = subprocess.check_output([binary,'--version'],env=env,text=True).strip()
        for label in ('host','member-b','member-c'):
            core = n.Core(label,env); cores.append(core)
            core.api('settings',{'binary':binary, 'claudeBinary':'/usr/bin/false'})
        host,b,c = cores
        space = host.api('spaces',{'requestId':str(uuid.uuid4()),'title':'Space workbench live acceptance'})
        sid = space['id']; endpoint = 'collaborations/'+sid+'/workbench/'
        shared = host.api('collaborations/'+sid+'/action',{'action':'share','transport':'lan'})
        jb = b.api('join',{'invitation':shared['invitation']})['id']
        jc = c.api('join',{'invitation':shared['invitation']})['id']
        if args.reload_failed_receivers:
            def restore_failed(index, core, alias, name):
                # Reproduce a pre-thread CLI failure from an earlier Core. The
                # omitted empty ledger must survive reload and explicit retry.
                core.close()
                (root/'evidence'/(core.label+'.log')).rename(root/'evidence'/(core.label+'-before-restart.log'))
                request_id=str(uuid.uuid4()); receiver_id='receiver-'+request_id
                directory=core.data/'receivers'/receiver_id; cwd=directory/'workspace'
                cwd.mkdir(parents=True)
                now=datetime.datetime.now(datetime.timezone.utc).isoformat()
                record={'spaceId':alias,'record':{'schema':3,'id':receiver_id,'title':name,'state':'error',
                    'error':'Codex 路径不可用','createdAt':now,'updatedAt':now,'annotations':[],'materials':[],
                    'execution':{'requestId':request_id,'provider':'codex','runtimeMode':'restricted',
                        'providerHome':str(home),'executionCwd':str(cwd),'workspaceRoot':str(cwd),'workspaceOwned':True}}}
                (directory/'receiver.json').write_text(json.dumps(record))
                (directory/'receiver.json').chmod(0o600)
                replacement=n.Core(core.label,env); cores[index]=replacement
                replacement.api('settings',{'binary':binary,'claudeBinary':'/usr/bin/false'})
                restored=next(r for r in replacement.api('space-receivers?spaceId='+alias) if r['id']==receiver_id)
                assert restored['canRetryCreation'] and not restored['online'] and not restored['sessionId']
                receiver=replacement.api('space-receivers/'+receiver_id+'/action',{'action':'retry'})
                assert receiver['id']==receiver_id and receiver['online'] and receiver['sessionId']
                return replacement,receiver
            b,rb=restore_failed(1,b,jb,'Space coordinator fixture')
            c,rc=restore_failed(2,c,jc,'Peer analysis fixture')
            report['failedReceiversReloadedBeforeFirstDelivery']=True
            n.emit('failed_receivers_reloaded_before_first_delivery')
        else:
            rb = b.api('collaborations/'+jb+'/workbench/create-receiver',{'requestId':str(uuid.uuid4()),'name':'Space coordinator fixture'})
            rc = c.api('collaborations/'+jc+'/workbench/create-receiver',{'requestId':str(uuid.uuid4()),'name':'Peer analysis fixture'})
        receivers.extend([(b,rb),(c,rc)])
        assert not host.api('collaborations/'+sid)['hasExecution']
        assert rb['sessionId'] != rc['sessionId']
        report['receivers'] = [{'core':core.label,'id':r['id'],'sessionId':r['sessionId'],'model':r['model']} for core,r in receivers]
        assert all(r['model']==n.MODEL for _,r in receivers), report['receivers']
        def ready_targets():
            view = host.api(endpoint+'view')
            return view if len(view['targets'])==2 and all(t['available'] for t in view['targets']) else None
        view = n.wait_until(ready_targets,30)
        tb = next(t for t in view['targets'] if t['name']==rb['name'])
        tc = next(t for t in view['targets'] if t['name']==rc['name'])
        host.api(endpoint+'brief',{'baseRevision':0,'brief':{'topic':'Verify independent session collaboration. No repository execution.','decisions':[{'text':'Only published space material is in scope.','sources':[]}],'questions':[{'text':'Can a receiving conversation explicitly dispatch to its peer?','sources':[]}]}})
        boot_id = str(uuid.uuid4()); request_ids.add(boot_id)
        host.api(endpoint+'assistant',{'state':'initializing','targetId':tb['id'],'requestId':boot_id,'baseEpoch':0})

        def approvals():
            for core,receiver in receivers:
                events = core.api('space-receivers/'+receiver['id']+'/events')
                for item in events.get('approvals',[]):
                    params = item.get('params',{}); meta = params.get('_meta',{}); call = meta.get('tool_params',{}); message = params.get('message','')
                    allowed = any('"'+tool+'"' in message for tool in ('read_agent_request','finish_agent_request','get_space_request','send_space_request')) and call.get('requestId') in request_ids
                    allowed = allowed or any('"'+tool+'"' in message for tool in ('list_space_targets','read_space_brief','list_space_requests','read_annotations','list_materials'))
                    key = (receiver['id'],str(item['id']))
                    if allowed and key not in approved and item['method']=='mcpServer/elicitation/request' and params.get('threadId')==receiver['sessionId'] and params.get('serverName','').startswith('teamcross_annotations'):
                        core.api('space-receivers/'+receiver['id']+'/respond',{'id':item['id'],'result':{'action':'accept','content':{}}})
                        approved.add(key)
                (root/'evidence'/(core.label+'-events.json')).write_text(json.dumps(events,ensure_ascii=False,indent=2)+'\n')

        def accepted():
            approvals()
            view = host.api(endpoint+'view')
            if view['assistant']['state']=='failed': raise RuntimeError('bootstrap failed: '+json.dumps(view['requests'],ensure_ascii=False))
            return view if view['assistant']['state']=='ready' else None
        view = n.wait_until(accepted,180)
        boot = next(r for r in view['requests'] if r['id']==boot_id)
        assert boot['state']=='completed' and boot['receivedAt'] and boot['bootstrap']['brief']['revision']==1
        report['bootstrapReadAndAccepted'] = True
        n.wait_until(lambda:not b.api('space-receivers?spaceId='+jb)[0]['busy'],60)
        n.wait_until(ready_targets,30)
        outer,peer = str(uuid.uuid4()),str(uuid.uuid4()); request_ids.update((outer,peer))
        instruction = ('This is an explicit fixture authorization to send exactly one space request to peer target '+tc['id']+'. '
            'Use send_space_request with requestId '+peer+', targetId '+tc['id']+', parentRequestId '+outer+', references [], intent analyze, '
            'instruction "Read the space brief with read_space_brief. Do not send any other request. Finish your received request with summary exactly PEER_WORKBENCH_DONE, with no extra words or punctuation.". '
            'This MCP is already space-scoped, so omit spaceId. After the send receipt, finish this request with summary exactly DISPATCH_WORKBENCH_DONE, with no extra words or punctuation. '
            'Do not wait or poll. Do not modify files or run shell commands.')
        body = {'requestId':outer,'targetId':tb['id'],'instruction':instruction,'intent':'analyze','references':[]}
        sent = host.api(endpoint+'send',body); duplicate = host.api(endpoint+'send',body)
        assert sent['id']==duplicate['id']
        def finished():
            approvals()
            view=host.api(endpoint+'view'); requests={r['id']:r for r in view['requests']}
            for rid in (outer,peer):
                if requests.get(rid,{}).get('state')=='failed': raise RuntimeError(json.dumps(requests[rid],ensure_ascii=False))
            return view if all(requests.get(rid,{}).get('state')=='completed' for rid in (outer,peer)) else None
        view=n.wait_until(finished,240)
        requests={r['id']:r for r in view['requests']}
        report['requests'] = list(requests.values())
        assert requests[peer]['actor']['kind']=='session' and requests[peer]['actor']['memberId']==tb['memberId']
        assert requests[peer]['parentRequestId']==outer
        assert requests[peer]['summary']=='PEER_WORKBENCH_DONE'
        assert requests[outer]['summary']=='DISPATCH_WORKBENCH_DONE'
        for core,alias in ((b,jb),(c,jc)):
            member_view=core.api('collaborations/'+alias+'/workbench/view')
            assert len(member_view['requests'])==3 and all(r['state']=='completed' for r in member_view['requests'])
        assert not host.api('collaborations/'+sid)['hasExecution']
        host.api(endpoint+'assistant',{'state':'paused','baseEpoch':view['assistant']['epoch']})
        paused=host.api(endpoint+'view'); assert paused['assistant']['state']=='paused'
        host.api(endpoint+'assistant',{'state':'disabled','baseEpoch':paused['assistant']['epoch']})
        assert len(host.api(endpoint+'view')['requests'])==3
        assert b.api('space-receivers?spaceId='+jb)[0]['sessionId']==rb['sessionId']
        report.update({'genericPeerDispatch':True,'threeMembersSeeSameProgress':True,'duplicateDidNotReplay':True,'assistantOptionalAndNoExecution':True,'pauseDisablePreserveHistory':True,'scopedToolApprovals':len(approved),'requests':list(requests.values())})
        if args.reload_failed_receivers:
            for core,_ in receivers:
                status=json.loads(subprocess.check_output([n.BINARY,'status','--json','--data-dir',str(core.data)],env=env,text=True,timeout=5))
                assert status['running'] and status['pid']==core.p.pid
            report['coreStatusAfterRestoredDelivery']=True
        n.emit('space_workbench_verified')
    except Exception as error:
        report['error']=type(error).__name__+': '+str(error)
        report['traceback']=traceback.format_exc()
        n.emit('space_workbench_failed',error=report['error'])
    finally:
        for core in reversed(cores):
            try:core.close()
            except Exception as error:report.setdefault('cleanupErrors',[]).append(str(error))
        (root/'evidence/report.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    return 1 if 'error' in report or report.get('cleanupErrors') else 0


if __name__=='__main__':
    parser=argparse.ArgumentParser()
    parser.add_argument('--fixture-dir',type=pathlib.Path,required=True)
    parser.add_argument('--teamcross-bin',type=pathlib.Path,required=True)
    parser.add_argument('--codex-bin',default='codex')
    parser.add_argument('--reload-failed-receivers',action='store_true',help='Reload legacy pre-thread failures and explicitly retry before their first delivery')
    parser.add_argument('--codex-auth',type=pathlib.Path,default=pathlib.Path.home()/'.codex/auth.json')
    raise SystemExit(run(parser.parse_args()))
