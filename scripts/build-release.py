#!/usr/bin/env python3
"""Build local artifacts; publishing is intentionally a separate operation."""
import argparse, hashlib, json, os, pathlib, plistlib, re, shutil, subprocess, tarfile, tempfile
from homebrew_release import render_bundle
ROOT = pathlib.Path(__file__).resolve().parents[1]
def run(*args, **kw): subprocess.run(args, cwd=ROOT, check=True, **kw)
def digest(p): return hashlib.sha256(p.read_bytes()).hexdigest()
def build_app_icon(source, destination, stage):
    iconset=stage/'TeamCross.iconset';iconset.mkdir()
    for points,scale in ((16,1),(16,2),(32,1),(32,2),(128,1),(128,2),(256,1),(256,2),(512,1),(512,2)):
        pixels=points*scale;suffix=f'@{scale}x' if scale>1 else ''
        target=iconset/f'icon_{points}x{points}{suffix}.png'
        run('sips','-z',str(pixels),str(pixels),str(source),'--out',str(target),stdout=subprocess.DEVNULL)
    # iconutil rejects otherwise valid iconsets when downloaded source metadata propagates into the renditions.
    run('xattr','-cr',str(iconset))
    run('iconutil','-c','icns',str(iconset),'-o',str(destination))
p = argparse.ArgumentParser()
p.add_argument('--version', default='0.1.7-dev')
p.add_argument('--output', help='Version-specific output directory (default: dist/release/VERSION)')
p.add_argument('--base-url', help='Release asset base URL; no upload is performed')
p.add_argument('--sign-identity', help='Developer ID Application identity')
p.add_argument('--notary-profile', help='Existing notarytool keychain profile; requires signing')
p.add_argument('--skip-web', action='store_true')
a = p.parse_args()
if not re.fullmatch(r'\d+\.\d+\.\d+(?:[-+][A-Za-z0-9.-]+)?', a.version): p.error('Invalid version')
if a.notary_profile and not a.sign_identity: p.error('Notarization requires --sign-identity')
development = '-dev' in a.version
out = pathlib.Path(a.output).resolve() if a.output else ROOT / 'dist/release' / a.version
if not development and (out/'release.json').exists(): p.error('Release output already exists; use a new candidate output directory')
out.mkdir(parents=True, exist_ok=True)
marker=out/'.teamcross-build-output'
if (out/'Team Cross.app').exists() and not marker.exists(): p.error('Refusing to replace an App outside a marked Team Cross build output directory')
marker.write_text('Team Cross local build output\n')
if a.base_url and any(c in a.base_url for c in ['"', '\n', '\r', '\\']): p.error('Invalid artifact URL')
commit = subprocess.check_output(['git','rev-parse','HEAD'], cwd=ROOT, text=True).strip()
dirty = bool(subprocess.check_output(['git','status','--porcelain'],cwd=ROOT,text=True).strip())
if (not development or a.sign_identity) and dirty: p.error('Release builds require a clean checkout; use a -dev version for local work')
if not a.skip_web: run('pnpm','--filter','@teamcross/web','build')
dirty = bool(subprocess.check_output(['git','status','--porcelain'],cwd=ROOT,text=True).strip())
if (not development or a.sign_identity) and dirty: p.error('Web build changed source assets; commit the refreshed assets before building a release')
build_number = subprocess.check_output(['git','rev-list','--count','HEAD'],cwd=ROOT,text=True).strip()
env = os.environ.copy(); env.update(GOOS='darwin',GOARCH='arm64',CGO_ENABLED='0')
env.setdefault('GOCACHE',str(ROOT/'bin/go-cache')); env.setdefault('GOMODCACHE',str(ROOT/'bin/go-mod')); env.setdefault('CLANG_MODULE_CACHE_PATH',str(ROOT/'bin/clang-cache'))
with tempfile.TemporaryDirectory(prefix='teamcross-package-') as temp:
    stage=pathlib.Path(temp); app=stage/'Team Cross.app'; mac=app/'Contents/MacOS'; resources=app/'Contents/Resources'
    mac.mkdir(parents=True); resources.mkdir()
    build_app_icon(ROOT/'apps/macos/Assets/AppIcon.png',resources/'TeamCross.icns',stage)
    run('go','build','-trimpath','-ldflags',f'-s -w -X teamcross/internal/buildinfo.Version={a.version} -X teamcross/internal/buildinfo.Commit={commit}', '-o',str(resources/'teamcross'),'./cmd/teamcross',env=env)
    run('xcrun','swiftc','-O','-target','arm64-apple-macosx14.0','-module-cache-path',str(ROOT/'bin/swift-cache'),str(ROOT/'apps/macos/AppInstance.swift'),str(ROOT/'apps/macos/TeamCross.swift'),'-o',str(mac/'TeamCross'),env=env)
    info=plistlib.loads((ROOT/'apps/macos/Info.plist').read_bytes());info['CFBundleShortVersionString']=a.version.split('-')[0].split('+')[0];info['CFBundleVersion']=build_number;info['TeamCrossVersion']=a.version
    (app/'Contents/Info.plist').write_bytes(plistlib.dumps(info))
    identity=a.sign_identity or '-'
    for binary in (resources/'teamcross',mac/'TeamCross'):
        args=['codesign','--force','--sign',identity]
        if a.sign_identity: args+=['--options','runtime','--timestamp']
        run(*args,str(binary))
    args=['codesign','--force','--sign',identity]
    if a.sign_identity: args+=['--options','runtime','--timestamp']
    run(*args,str(app))
    if a.notary_profile:
        zipped=stage/'notarize.zip';run('ditto','-c','-k','--keepParent',str(app),str(zipped))
        run('xcrun','notarytool','submit',str(zipped),'--keychain-profile',a.notary_profile,'--wait')
        run('xcrun','stapler','staple',str(app))
    cli=out/f'teamcross-{a.version}-darwin-arm64.tar.gz'
    with tarfile.open(cli,'w:gz') as t: t.add(resources/'teamcross',arcname='teamcross');t.add(ROOT/'README.md',arcname='README.md')
    # Replace only builder-owned outputs; never touch collaboration directories.
    destination=out/'Team Cross.app'
    if destination.exists(): shutil.rmtree(destination)
    shutil.copytree(app,destination)
    volume=stage/'volume';volume.mkdir();shutil.copytree(app,volume/'Team Cross.app');(volume/'Applications').symlink_to('/Applications')
    dmg=out/f'Team-Cross-{a.version}-arm64.dmg'
    run('hdiutil','create','-ov','-format','UDZO','-volname','Team Cross','-srcfolder',str(volume),str(dmg))
    if a.notary_profile:
        run('codesign','--sign',a.sign_identity,'--timestamp',str(dmg))
        run('xcrun','notarytool','submit',str(dmg),'--keychain-profile',a.notary_profile,'--wait')
        run('xcrun','stapler','staple',str(dmg))
    base=a.base_url or f'https://github.com/YTwsy/Team-Cross/releases/download/v{a.version}'
    tap=out/'homebrew-teamcross'
    render_bundle(
        tap,
        version=a.version,
        commit=commit,
        base_url=base,
        cli_sha=digest(cli),
        dmg_sha=digest(dmg),
        developer_id_signed=bool(a.sign_identity),
        notarized=bool(a.notary_profile),
    )
    (out/'SHA256SUMS').write_text(''.join(f'{digest(f)}  {f.name}\n' for f in [cli,dmg]))
    (out/'release.json').write_text(json.dumps(dict(version=a.version,commit=commit,buildNumber=build_number,dirty=dirty,architecture='arm64',minimumMacOS='14.0',developerIDSigned=bool(a.sign_identity),notarized=bool(a.notary_profile),artifacts={f.name:digest(f) for f in [cli,dmg]}),indent=2)+'\n')
print(out)
