#!/usr/bin/env python3
"""Inspect a Wails DMG/App/CLI candidate without launching UI or touching user data.

This packaging gate deliberately does not claim native installation acceptance.
"""
import argparse
import hashlib
import json
from pathlib import Path
import plistlib
import subprocess
import tarfile
import tempfile

parser = argparse.ArgumentParser()
parser.add_argument('release', type=Path)
args = parser.parse_args()
release = args.release.resolve()
manifest = json.loads((release / 'release.json').read_text())
assert manifest['desktopHost'] == 'wails'
assert manifest['architecture'] == 'arm64' and manifest['minimumMacOS'] == '14.0'


def output(*command):
    return subprocess.check_output(command, text=True).strip()


def inspect(app):
    info = plistlib.loads((app / 'Contents/Info.plist').read_bytes())
    assert info['CFBundleIdentifier'] == 'io.github.ytwsy.teamcross'
    assert info['CFBundleExecutable'] == 'TeamCross'
    assert info['TeamCrossDesktopHost'] == 'wails' and info['LSUIElement'] is False
    assert info['CFBundleURLTypes'][0]['CFBundleURLSchemes'] == ['teamcross']
    assert info['LSMinimumSystemVersion'] == '14.0'
    assert info['TeamCrossVersion'] == manifest['version']
    assert info['CFBundleVersion'] == manifest['buildNumber']
    assert info['CFBundleIconFile'] == 'TeamCross'
    resources = app / 'Contents/Resources'
    assert (resources / 'TeamCross.icns').is_file()
    for language in ('zh-Hans', 'en'):
        for name in ('InfoPlist.strings', 'Localizable.strings'):
            assert (resources / (language + '.lproj') / name).is_file()
    shell, helper = app / 'Contents/MacOS/TeamCross', resources / 'teamcross'
    for binary in (shell, helper):
        assert output('lipo', '-archs', str(binary)) == 'arm64'
    subprocess.run(['codesign', '--verify', '--deep', '--strict', str(app)], check=True)
    desktop = json.loads(output(str(shell), '--version'))
    cli = json.loads(output(str(helper), 'version', '--json'))
    assert desktop['profile'] == 'release' and desktop['desktopHost'] == 'wails'
    for key in ('version', 'commit'):
        assert desktop[key] == cli[key] == manifest[key]
    assert desktop['protocol'] == cli['protocol']
    return cli, hashlib.sha256(helper.read_bytes()).hexdigest()


checksums = {name: sha for sha, name in (line.split(maxsplit=1) for line in (release / 'SHA256SUMS').read_text().splitlines())}
assert checksums == manifest['artifacts']
for name, sha in checksums.items():
    assert Path(name).name == name
    assert hashlib.sha256((release / name).read_bytes()).hexdigest() == sha
cli_version, cli_sha = inspect(release / 'Team Cross.app')
with tempfile.TemporaryDirectory(prefix='teamcross-wails-package-') as temporary:
    root = Path(temporary)
    with tarfile.open(release / f"teamcross-{manifest['version']}-darwin-arm64.tar.gz") as archive:
        assert set(archive.getnames()) == {'teamcross', 'README.md'}
        assert archive.getmember('teamcross').isfile()
        assert hashlib.sha256(archive.extractfile('teamcross').read()).hexdigest() == cli_sha
    mount = root / 'mounted'
    mount.mkdir()
    subprocess.run(['hdiutil', 'attach', '-readonly', '-nobrowse', '-mountpoint', str(mount),
                    str(release / f"Team-Cross-{manifest['version']}-arm64.dmg")], check=True, stdout=subprocess.DEVNULL)
    try:
        assert (mount / 'Applications').is_symlink()
        assert (mount / 'Applications').readlink() == Path('/Applications')
        assert inspect(mount / 'Team Cross.app') == (cli_version, cli_sha)
    finally:
        subprocess.run(['hdiutil', 'detach', str(mount)], check=True, stdout=subprocess.DEVNULL)
print(json.dumps(dict(checksums=True, dmgContents=True, appSignatureIntegrity=True,
                      appCLIParity=True, productionIdentity=True, desktopHost='wails',
                      nativeInstallationAcceptance=False, publicInstallation=False)))
