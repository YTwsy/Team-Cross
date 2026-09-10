#!/usr/bin/env python3
"""Verify checksums, mounted DMG installation, CLI/App parity and service reuse."""
import argparse, hashlib, json, os, pathlib, plistlib, shutil, subprocess, tempfile
ROOT=pathlib.Path(__file__).resolve().parents[1]
p=argparse.ArgumentParser();p.add_argument('release');a=p.parse_args();release=pathlib.Path(a.release).resolve();manifest=json.loads((release/'release.json').read_text())
def run(*args,**kw): return subprocess.run(args,check=True,**kw)
for name,sha in manifest['artifacts'].items(): assert hashlib.sha256((release/name).read_bytes()).hexdigest()==sha,name
checksums={name:sha for sha,name in (line.split(maxsplit=1) for line in (release/'SHA256SUMS').read_text().splitlines())}
assert checksums==manifest['artifacts']
with tempfile.TemporaryDirectory(prefix='teamcross-install-') as temp:
    root=pathlib.Path(temp);extracted=root/'CLI with spaces';extracted.mkdir()
    archive=next(release.glob('teamcross-*-darwin-arm64.tar.gz'));run('tar','-xzf',str(archive),'-C',str(extracted))
    binary=extracted/'teamcross'
    version=json.loads(subprocess.check_output([str(binary),'version','--json'],text=True));assert version['version']==manifest['version'] and version['commit']==manifest['commit']
    run('python3',str(ROOT/'scripts/verify-lifecycle.py'),str(binary))
    mount=root/'volume';mount.mkdir();mounted=False
    try:
        run('hdiutil','attach','-readonly','-nobrowse','-mountpoint',str(mount),str(next(release.glob('*.dmg'))),stdout=subprocess.DEVNULL);mounted=True
        installed=root/'Applications with spaces'/'Team Cross.app';installed.parent.mkdir();shutil.copytree(mount/'Team Cross.app',installed)
        info=plistlib.loads((installed/'Contents/Info.plist').read_bytes());assert info['LSMinimumSystemVersion']=='14.0';assert info['CFBundleURLTypes'][0]['CFBundleURLSchemes']==['teamcross']
        run('codesign','--verify','--deep','--strict',str(installed))
        helper=installed/'Contents/Resources/teamcross';assert json.loads(subprocess.check_output([str(helper),'version','--json'],text=True))==version
        commands=root/'terminal commands';cliEnv=os.environ.copy();cliEnv['PATH']=str(commands)+':/usr/bin:/bin'
        def install_call(action):
            return json.loads(subprocess.check_output([str(helper),action,'--cli-dir',str(commands),'--json'],env=cliEnv,text=True,timeout=15))
        status=install_call('install-cli');assert status['installed'] and status['pathReady']
        assert json.loads(subprocess.check_output([str(commands/'teamcross'),'version','--json'],env=cliEnv,text=True))==version
        assert install_call('install-cli')['installed']
        install_call('uninstall-cli');assert not (commands/'teamcross').exists() and helper.exists()
        commands.mkdir(exist_ok=True);foreign=commands/'teamcross';foreign.write_text('user-owned\n')
        result=subprocess.run([str(helper),'install-cli','--cli-dir',str(commands),'--json'],env=cliEnv,text=True,capture_output=True)
        assert result.returncode!=0 and foreign.read_text()=='user-owned\n'
        data=root/'shared-data'
        def call(path,*args): return subprocess.check_output([str(path),*args,'--data-dir',str(data)],text=True)
        try:
            first=json.loads(call(binary,'serve','--no-open','--json'))['service']
            second=json.loads(call(helper,'serve','--no-open','--json'))['service'];assert first['pid']==second['pid']
            # A newly installed executable continues to use the existing compatible Core.
            assert json.loads(call(helper,'status','--json'))['pid']==first['pid']
        finally: run(str(helper),'stop','--force','--data-dir',str(data),stdout=subprocess.DEVNULL)
    finally:
        if mounted: run('hdiutil','detach',str(mount),stdout=subprocess.DEVNULL)
print(json.dumps({'checksums':True,'dmgInstall':True,'appSignatureIntegrity':True,'cliAppVersionParity':True,'appCLIInstallAndRemove':True,'existingCommandPreserved':True,'compatibleCoreReuse':True,'publicInstallation':False}))
