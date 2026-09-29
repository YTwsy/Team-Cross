#!/usr/bin/env python3
"""Build an isolated Wails preview; never replace a pre-existing bundle."""
import argparse
import datetime
import json
import os
from pathlib import Path
import plistlib
import subprocess
import tempfile
import shutil

ROOT = Path(__file__).resolve().parents[1]


def run(*args, **kwargs):
    return subprocess.run(args, cwd=ROOT, check=True, **kwargs)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--output', type=Path, help='New output directory')
    args = parser.parse_args()
    if os.uname().sysname != 'Darwin':
        parser.error('desktop bundles require macOS')
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
    output = (args.output or ROOT / 'bin/desktop-preview' / stamp).resolve()
    output.mkdir(parents=True, exist_ok=False)
    commit = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    dirty = bool(subprocess.check_output(['git', 'status', '--porcelain'], cwd=ROOT, text=True).strip())
    version = '0.2.6-dev.desktop'
    flags = f'-s -w -X teamcross/internal/buildinfo.Version={version} -X teamcross/internal/buildinfo.Commit={commit}'
    env = os.environ.copy()
    env.update(GOOS='darwin', GOARCH='arm64', CGO_ENABLED='1', MACOSX_DEPLOYMENT_TARGET='14.0')
    env['CGO_CFLAGS'] = env.get('CGO_CFLAGS', '') + ' -mmacosx-version-min=14.0'
    env['CGO_LDFLAGS'] = env.get('CGO_LDFLAGS', '') + ' -mmacosx-version-min=14.0'
    with tempfile.TemporaryDirectory(prefix='.teamcross-desktop-', dir=output) as temp:
        app = Path(temp) / 'Team Cross Desktop Preview.app'
        mac = app / 'Contents/MacOS'
        resources = app / 'Contents/Resources'
        mac.mkdir(parents=True)
        resources.mkdir()
        subprocess.run(['go', 'build', '-mod=readonly', '-trimpath', '-tags', 'production', '-ldflags', flags,
                        '-o', str(mac / 'TeamCrossDesktop'), '.'], cwd=ROOT / 'apps/desktop', env=env, check=True)
        cli_env = env.copy(); cli_env['CGO_ENABLED'] = '0'
        run('go', 'build', '-mod=readonly', '-trimpath', '-ldflags', flags,
            '-o', str(resources / 'teamcross'), './cmd/teamcross', env=cli_env)
        info = plistlib.loads((ROOT / 'apps/desktop/Resources/Info.plist').read_bytes())
        info['TeamCrossVersion'] = version
        info['CFBundleVersion'] = subprocess.check_output(['git', 'rev-list', '--count', 'HEAD'], cwd=ROOT, text=True).strip()
        (app / 'Contents/Info.plist').write_bytes(plistlib.dumps(info))
        for binary in (resources / 'teamcross', mac / 'TeamCrossDesktop'):
            run('codesign', '--force', '--sign', '-', '--timestamp=none', str(binary))
            architecture = subprocess.check_output(['lipo', '-archs', str(binary)], text=True).strip()
            if architecture != 'arm64':
                raise RuntimeError(f'unexpected architecture: {architecture}')
        run('codesign', '--force', '--sign', '-', '--timestamp=none', str(app))
        run('codesign', '--verify', '--deep', '--strict', str(app))
        destination = output / app.name
        shutil.move(str(app), destination)
    manifest = dict(version=version, commit=commit, dirty=dirty, architecture='arm64', minimumMacOS='14.0',
                    wails='v3.0.0-beta.26', signature='ad-hoc', notarized=False, app=str(destination))
    (output / 'desktop-build.json').write_text(json.dumps(manifest, indent=2) + '\n')
    print(json.dumps(manifest, indent=2))


if __name__ == '__main__':
    main()
