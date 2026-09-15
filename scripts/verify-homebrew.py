#!/usr/bin/env python3
"""Install/uninstall local Formula and Cask in an isolated Homebrew checkout."""
import argparse, functools, http.server, json, os, pathlib, re, shutil, signal, subprocess, tempfile, threading
from homebrew_release import load_bundle
p=argparse.ArgumentParser();p.add_argument('release');p.add_argument('--brew',default='/opt/homebrew/bin/brew');p.add_argument('--public',action='store_true',help='Install from the public URLs instead of the local fixture server');a=p.parse_args();release=pathlib.Path(a.release).resolve()
manifest=json.loads((release/'release.json').read_text());metadata,channel=load_bundle(release/'homebrew-teamcross')
assert manifest.get('version')==metadata.get('version'), 'Release and Homebrew bundle versions differ'
source=pathlib.Path(subprocess.check_output([a.brew,'--repository'],text=True).strip())
with tempfile.TemporaryDirectory(prefix='teamcross-brew-') as temp:
    root=pathlib.Path(temp).resolve();prefix=root/'brew';apps=root/'Applications';apps.mkdir();data=root/'collaboration-data';data.mkdir();(data/'preserved.txt').write_text('retain')
    subprocess.run(['git','clone','--quiet','--local','--shared',str(source),str(prefix)],check=True)
    rubySource=source/'Library/Homebrew/vendor/portable-ruby';rubyTarget=prefix/'Library/Homebrew/vendor/portable-ruby'
    rubyTarget.mkdir(exist_ok=True)
    for path in rubySource.iterdir():
        target=rubyTarget/path.name
        if not target.exists(): target.symlink_to(path.resolve(),target_is_directory=path.is_dir())
    env=os.environ.copy();env.update(XDG_CONFIG_HOME=str(root/'config'),HOMEBREW_NO_AUTO_UPDATE='1',HOMEBREW_NO_INSTALL_FROM_API='1',HOMEBREW_NO_ANALYTICS='1',HOMEBREW_NO_INSTALL_CLEANUP='1',HOMEBREW_NO_AUTOREMOVE='1',HOMEBREW_NO_ENV_HINTS='1',HOMEBREW_CACHE=str(root/'cache'),HOMEBREW_LOGS=str(root/'logs'),HOMEBREW_TEMP=str(root/'tmp'))
    (root/'tmp').mkdir()
    env.pop('HOMEBREW_NO_INSTALL_FROM_API',None)
    apiCache=pathlib.Path.home()/'Library/Caches/Homebrew/api'
    if apiCache.exists(): shutil.copytree(apiCache,root/'cache/api')
    env['HOMEBREW_API_AUTO_UPDATE_SECS']='864000'
    brew=prefix/'bin/brew'
    def run(*args, expect_failure=False):
        process=subprocess.Popen([str(brew),*args],env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE,start_new_session=True)
        try: stdout,stderr=process.communicate(timeout=90)
        except subprocess.TimeoutExpired:
            # Git can start its own process group. Capture descendants while the
            # parent is still alive so timeout cleanup stays inside this test.
            rows=[line.split(None,2) for line in subprocess.check_output(['ps','-axo','pid,ppid,command'],text=True).splitlines()[1:]]
            descendants={process.pid}
            for _ in range(len(rows)):
                more={int(row[0]) for row in rows if len(row)==3 and int(row[1]) in descendants}
                if more <= descendants: break
                descendants.update(more)
            for pid in sorted(descendants,reverse=True):
                try: os.kill(pid,signal.SIGTERM)
                except ProcessLookupError: pass
            process.communicate(timeout=10);raise
        if expect_failure:
            assert process.returncode != 0, 'Conflicting installation unexpectedly succeeded'
            assert any(word in stdout+stderr for word in ['请先运行', 'conflict', 'Conflict', 'already a Binary']), stdout+stderr
        elif process.returncode: raise subprocess.CalledProcessError(process.returncode,process.args,stdout,stderr)
        return subprocess.CompletedProcess(process.args,process.returncode,stdout,stderr)
    assert pathlib.Path(run('--prefix').stdout.strip())==prefix,'Refusing to mutate the original Homebrew prefix'
    tap=prefix/'Library/Taps/teamcross/homebrew-install-test';shutil.copytree(release/'homebrew-teamcross',tap)
    server=None;thread=None
    if not a.public:
        class Quiet(http.server.SimpleHTTPRequestHandler):
            def log_message(self,*args): pass
            def copyfile(self,source,outputfile):
                try: super().copyfile(source,outputfile)
                except (BrokenPipeError,ConnectionResetError): pass
        server=http.server.ThreadingHTTPServer(('127.0.0.1',0),functools.partial(Quiet,directory=str(release)));thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
        for relative in [channel.formula_path,channel.cask_path]:
            path=tap/relative;text=path.read_text();text=re.sub(r'url "[^"]*/',f'url "http://127.0.0.1:{server.server_port}/',text,count=1);path.write_text(text)
    subprocess.run(['git','init','-q',str(tap)],check=True)
    subprocess.run(['git','-C',str(tap),'add','.'],check=True)
    subprocess.run(['git','-C',str(tap),'-c','user.name=Installation Fixture','-c','user.email=fixture@example.invalid','commit','-qm','Local install fixture'],check=True)
    formula=f'teamcross/install-test/{channel.formula_token}';cask=f'teamcross/install-test/{channel.cask_token}';installedFormula=False;installedCask=False
    run('trust','--formula',formula)
    run('trust','--cask',cask)
    try:
        run('install','--formula',formula);installedFormula=True
        run('test',formula)
        cli=prefix/'bin/teamcross';helper=apps/'Team Cross.app/Contents/Resources/teamcross'
        original=cli.read_bytes()
        run('install','--cask',f'--appdir={apps}',cask,expect_failure=True)
        assert cli.read_bytes()==original and not helper.exists()
        assert not json.loads(run('info','--cask','--json=v2',cask).stdout)['casks'][0]['installed'], 'Rejected Cask left an installed record'
        def call(binary,*args):return json.loads(subprocess.check_output([str(binary),*args,'--data-dir',str(data)],env=env,text=True,timeout=30))
        try:
            first=call(cli,'serve','--no-open','--json')['service'];assert first['running']
            info=call(cli,'doctor','--json')['diagnostics']
            assert str(prefix/f'opt/{channel.formula_token}/bin/teamcross') in info['mcpCommand']
            assert '/Cellar/' not in info['mcpCommand']
            # Replace the installed package while its Core is alive. Identical
            # executable bytes isolate Homebrew's upgrade and stable-path behavior.
            definition=tap/channel.formula_path;text=definition.read_text()
            text=re.sub(r'version "([^"]+)"',lambda m:'version "'+m[1]+'.1"',text,count=1)
            definition.write_text(text)
            run('upgrade','--formula',formula)
            upgraded=call(cli,'serve','--no-open','--json')['service']
            assert upgraded['pid']==first['pid'] and upgraded['instance']==first['instance']
        finally: subprocess.run([str(cli),'stop','--force','--data-dir',str(data)],env=env,capture_output=True,timeout=30)
        run('uninstall','--force','--ignore-dependencies','--formula',formula);installedFormula=False
        assert not cli.exists() and (data/'preserved.txt').read_text()=='retain'
        run('install','--cask',f'--appdir={apps}',cask);installedCask=True
        assert cli.is_symlink() and cli.resolve()==helper.resolve(), f'CLI {cli} -> {cli.resolve()}, expected {helper.resolve()}'
        # Preserve quarantine. Execute the separate local DMG copy in verify-release;
        # this gate checks the exact bytes and Homebrew's command ownership.
        assert helper.read_bytes()==original
        run('install','--formula',formula,expect_failure=True)
        assert cli.resolve()==helper.resolve() and helper.read_bytes()==original
        definition=tap/channel.cask_path;text=definition.read_text()
        text=re.sub(r'version "([^"]+)"',lambda m:'version "'+m[1]+'.1"',text,count=1)
        definition.write_text(text)
        run('upgrade','--cask',f'--appdir={apps}',cask)
        assert cli.resolve()==helper.resolve() and helper.read_bytes()==original
        run('uninstall','--cask',cask);installedCask=False
        assert not cli.exists() and not cli.is_symlink() and not helper.exists()
        assert (data/'preserved.txt').read_text()=='retain'
        print(json.dumps({'channel':channel.name,'isolatedPrefix':True,'formulaInstallAndTest':True,'caskInstall':True,'mutualExclusionBothOrders':True,'caskCommandLinksBundledCLI':True,'helperBytesMatch':True,'stableMCPPath':True,'formulaUpgradeReusesRunningCore':True,'caskUpgradeKeepsCommand':True,'caskFirstLaunch':'requires-system-approval','uninstallPreservesData':True,'publicTap':a.public}))
    except subprocess.CalledProcessError as e:
        print(e.stdout or '');print(e.stderr or '');raise
    finally:
        if installedCask: run('uninstall','--cask',cask)
        if installedFormula: run('uninstall','--force','--ignore-dependencies','--formula',formula)
        if server is not None:
            server.shutdown();server.server_close();thread.join()
