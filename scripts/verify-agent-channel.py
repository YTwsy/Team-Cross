#!/usr/bin/env python3
"""Opt-in Claude Channel acceptance with a new home, Core, repo and Luna guard.

Exercises the development-channel flag documented by Anthropic. If the client
or account declines channel delivery, this must fail rather than forge a receipt.
"""
import argparse
import importlib.util
import json
import pathlib
import shlex
import shutil
import subprocess
import traceback
import uuid

spec = importlib.util.spec_from_file_location('channel_native_fixture', pathlib.Path(__file__).with_name('verify-claude-native.py'))
n = importlib.util.module_from_spec(spec)
spec.loader.exec_module(n)

def run(args):
    root=args.fixture_dir.resolve()
    if root.exists() and any(root.iterdir()): raise ValueError('Choose a new empty fixture directory')
    root.mkdir(parents=True,exist_ok=True);root.chmod(0o700);(root/'evidence').mkdir()
    n.ROOT=root;n.BINARY=str(args.teamcross_bin.resolve());n.CLI=shutil.which(args.claude_bin) or args.claude_bin
    n.TOKEN='';guard=core=terminal=None
    report={'model':n.MODEL,'scope':'one Mac, personal Claude TUI, development Channel and Core'}
    try:
        n.CREDS=json.loads(args.source_settings.read_text())['env']
        n.TOKEN=n.CREDS.get('ANTHROPIC_AUTH_TOKEN') or n.CREDS.get('ANTHROPIC_API_KEY')
        if not n.TOKEN:raise RuntimeError('Local proxy credential missing')
        guard=n.Guard();env=n.environment(guard)
        env={k:v for k,v in env.items() if not k.startswith('CODEX_')}
        repo=root/'repo';repo.mkdir()
        for command in (['init','-b','main'],['config','user.name','Channel Fixture'],['config','user.email','fixture@example.invalid']):
            subprocess.run(['git','-C',str(repo)]+command,check=True,capture_output=True)
        (repo/'baseline.txt').write_text('CHANNEL_BASELINE\n')
        subprocess.run(['git','-C',str(repo),'add','baseline.txt'],check=True)
        subprocess.run(['git','-C',str(repo),'commit','-m','Dedicated channel fixture'],check=True,capture_output=True)
        home=root/'source-home';home.mkdir()
        settings=home/'settings.json';settings.write_text(json.dumps({'env':{k:v for k,v in env.items() if k.startswith('ANTHROPIC_')},'disableAllHooks':True,'enabledPlugins':{}}));settings.chmod(0o600)
        version=n.cli(env,['--version']).strip();report['nativeVersion']=version
        state={'hasCompletedOnboarding':True,'lastOnboardingVersion':version.split()[0],'autoUpdates':False,'theme':'light','hasSeenTasksHint':True,'projects':{str(repo):{'hasTrustDialogAccepted':True,'hasCompletedProjectOnboarding':True,'projectOnboardingSeenCount':1,'allowedTools':[]}}}
        (home/'.claude.json').write_text(json.dumps(state));(home/'.claude.json').chmod(0o600)
        core=n.Core('core',env)
        space=core.api('spaces',{'requestId':str(uuid.uuid4()),'title':'Channel acceptance'})
        note=core.api('collaborations/'+space['id']+'/annotations',{'text':'CHANNEL_REFERENCE_749: please analyze this discussion without posting a reply.'})
        pair=core.api('agent-pairings',{'name':'Personal channel conversation'})
        config=root/'mcp.json';config.write_text(json.dumps({'mcpServers':{'teamcross':{'command':n.BINARY,'args':['mcp','--data-dir',str(core.data)]}}}))
        tools=['pair_current_session','confirm_pairing','read_agent_request','finish_agent_request','read_context']
        prompt=('Dedicated acceptance fixture. Pair THIS conversation using Team Cross pair_current_session with code '+pair['code']+'. '
                'If verifying, end your reply and process the Team Cross Channel verification notification when it arrives. '
                'Use confirm_pairing only with the challenge from that notification. Do not poll or invent a challenge. '
                'On subsequent Team Cross agent_request notifications, read the request and follow its instructions. Use only the allowed Team Cross tools.')
        command=[n.CLI,'--model',n.MODEL,'--effort','low','--tools','','--permission-mode','manual','--allowedTools',','.join('mcp__teamcross__'+t for t in tools),'--setting-sources','','--strict-mcp-config','--mcp-config',str(config),'--dangerously-load-development-channels','server:teamcross','--no-chrome',prompt]
        terminal=n.Terminal('claude-channel','cd '+shlex.quote(str(repo))+' && '+shlex.join(command),env)
        def paired():
            screen=n.clean(n.terminal_text(terminal.data))
            (root/'evidence/native-progress.txt').write_text(screen)
            if 'channelsarenotcurrentlyavailable' in ''.join(screen.lower().split()):
                raise RuntimeError('The installed client reports Channels are not currently available; no receipt was forged')
            return next((p for p in core.api('agent-pairings') if p['id']==pair['pairing']['id'] and p['state']=='paired'),None)
        target=n.wait_until(paired,150,terminal)
        report.update({'pairedThroughChannelReceipt':True,'sessionId':target['sessionId']})
        request={'requestId':str(uuid.uuid4()),'pairingId':target['id'],'references':[{'spaceId':space['id'],'kind':'annotation','annotationId':note['id']}],'intent':'analyze','instruction':'Read the original annotation with read_context(kind=annotations). Do not reply to it. Call finish_agent_request with status completed and summary exactly CHANNEL_REFERENCE_749.'}
        core.api('agent-requests',request)
        def completed():
            (root/'evidence/native-progress.txt').write_text(n.clean(n.terminal_text(terminal.data)))
            result=core.api('agent-requests/'+request['requestId'])
            return result if result['state']=='completed' else None
        result=n.wait_until(completed,160,terminal)
        assert result['summary']=='CHANNEL_REFERENCE_749'
        assert (repo/'baseline.txt').read_text()=='CHANNEL_BASELINE\n'
        report['channelRequestCompleted']=True
        n.emit('agent_channel_verified')
    except Exception as e:
        report['error']=n.clean(type(e).__name__+': '+str(e));report['traceback']=n.clean(traceback.format_exc())
        n.emit('agent_channel_failed',error=report['error'])
    finally:
        for item in (terminal,core):
            if item:
                try:item.close()
                except Exception as e:report.setdefault('cleanupErrors',[]).append(n.clean(str(e)))
        if 'env' in locals():
            try:n.stop_fixture_daemon(env,root)
            except Exception as e:report.setdefault('cleanupErrors',[]).append(n.clean(str(e)))
        if guard:
            report['generationRequests']=guard.generation_count();guard.close()
        (root/'evidence/report.json').write_text(json.dumps(n.clean(report),ensure_ascii=False,indent=2)+'\n')
    return 1 if 'error' in report or 'cleanupErrors' in report else 0

if __name__=='__main__':
    p=argparse.ArgumentParser()
    p.add_argument('--fixture-dir',type=pathlib.Path,required=True)
    p.add_argument('--teamcross-bin',type=pathlib.Path,required=True)
    p.add_argument('--claude-bin',default='claude')
    p.add_argument('--source-settings',type=pathlib.Path,default=pathlib.Path.home()/'.claude/settings.json')
    raise SystemExit(run(p.parse_args()))
