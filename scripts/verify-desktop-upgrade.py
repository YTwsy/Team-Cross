#!/usr/bin/env python3
"""Verify Swift -> Wails package replacement, CLI/MCP paths and retained data.

Only the bundled CLI runs. Native App quit, single-instance, Dock/menu/hotkey and
URL delivery must be observed separately in a macOS GUI session.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import urllib.request

parser = argparse.ArgumentParser()
parser.add_argument('previous', type=Path, help='Swift release output')
parser.add_argument('candidate', type=Path, help='Wails release output')
args = parser.parse_args()
previous, candidate = args.previous.resolve(), args.candidate.resolve()
old = json.loads((previous / 'release.json').read_text())
new = json.loads((candidate / 'release.json').read_text())
assert old.get('desktopHost', 'swift') == 'swift' and new['desktopHost'] == 'wails'
assert old['version'] != new['version'], 'Use distinct versions to prove replacement'
for release, manifest in ((previous, old), (candidate, new)):
    for name, checksum in manifest['artifacts'].items():
        assert Path(name).name == name
        assert hashlib.sha256((release / name).read_bytes()).hexdigest() == checksum

with tempfile.TemporaryDirectory(prefix='teamcross-upgrade-') as temporary:
    root = Path(temporary).resolve()
    app = root / 'Applications with spaces/Team Cross.app'
    data, commands = root / 'data with spaces', root / 'commands with spaces'
    data.mkdir()
    (data / 'preserved.txt').write_text('user-owned data survives upgrade and CLI removal\n')
    env = dict(os.environ, PATH=str(commands) + ':/usr/bin:/bin:/usr/sbin:/sbin',
               CODEX_HOME=str(root / 'empty-codex'), CLAUDE_CONFIG_DIR=str(root / 'empty-claude'))
    Path(env['CODEX_HOME']).mkdir()
    Path(env['CLAUDE_CONFIG_DIR']).mkdir()
    helper = app / 'Contents/Resources/teamcross'

    def call(binary, *arguments):
        return json.loads(subprocess.check_output([str(binary), *arguments, '--data-dir', str(data)],
                                                 env=env, text=True, timeout=30))

    def command(action):
        return call(helper, action, '--cli-dir', str(commands), '--json')

    def request(url, path, value):
        req = urllib.request.Request(url + '/api/' + path, data=json.dumps(value).encode(),
                                     headers={'Content-Type': 'application/json'}, method='POST')
        with urllib.request.urlopen(req, timeout=10) as response:
            assert response.status == 200

    def install(release):
        shutil.copytree(release / 'Team Cross.app', app)
        subprocess.run(['codesign', '--verify', '--deep', '--strict', str(app)], check=True)

    install(previous)
    try:
        assert command('install-cli')['installed']
        launcher = commands / 'teamcross'
        launcher_bytes = launcher.read_bytes()
        assert call(launcher, 'version', '--json')['version'] == old['version']
        before = call(helper, 'serve', '--no-open', '--json')['service']
        assert before['running'] and before['version'] == old['version']
        request(before['url'], 'ui-language', {'mode': 'en'})
        # Older releases need not understand ui-theme; only seed the preference
        # that the Swift baseline supports.
        settings = (data / 'settings.json').read_bytes()
        stable_mcp = call(helper, 'doctor', '--json')['diagnostics']['mcpCommand']
        assert str(helper) in stable_mcp
        call(helper, 'stop', '--force', '--json')
        assert not call(helper, 'status', '--json').get('running')
        app.rename(root / 'Previous Swift.app')
        install(candidate)
        assert launcher.read_bytes() == launcher_bytes
        assert call(launcher, 'version', '--json')['version'] == new['version']
        desktop = json.loads(subprocess.check_output([str(app / 'Contents/MacOS/TeamCross'), '--version'],
                                                    env=env, text=True, timeout=10))
        assert desktop['profile'] == 'release' and desktop['commit'] == new['commit']
        after = call(launcher, 'serve', '--no-open', '--json')['service']
        assert after['running'] and after['version'] == new['version']
        assert after['instance'] != before['instance']
        assert call(helper, 'doctor', '--json')['diagnostics']['mcpCommand'] == stable_mcp
        assert (data / 'settings.json').read_bytes() == settings
        # Handshake through the unchanged App-relative MCP command path.
        result = subprocess.check_output([str(helper), 'mcp', '--data-dir', str(data)], env=env,
                                         input='{"jsonrpc":"2.0","id":1,"method":"initialize"}\n'
                                               '{"jsonrpc":"2.0","id":2,"method":"tools/list"}\n',
                                         text=True, timeout=15)
        replies = [json.loads(line) for line in result.splitlines()]
        assert len(replies) == 2 and replies[1]['result']['tools']
        assert call(helper, 'status', '--json')['instance'] == after['instance']
        command('uninstall-cli')
        assert not launcher.exists() and helper.exists()
        assert (data / 'preserved.txt').read_text() == 'user-owned data survives upgrade and CLI removal\n'
        assert call(helper, 'status', '--json')['instance'] == after['instance']
    finally:
        if helper.exists():
            subprocess.run([str(helper), 'stop', '--force', '--data-dir', str(data)], env=env,
                           check=True, stdout=subprocess.DEVNULL, timeout=30)
print(json.dumps(dict(swiftToWailsPackageReplacement=True, oldCoreStoppedBeforeReplacement=True,
                      unchangedCLILauncher=True, stableMCPPathAndHandshake=True, preferencesPreserved=True,
                      userDataPreserved=True, cliRemovalKeepsCore=True,
                      nativeAppAcceptance='required-separately', publicInstallation=False)))
