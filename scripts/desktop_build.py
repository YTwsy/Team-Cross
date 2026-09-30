"""Shared macOS Wails compiler; CLI builds keep their independent CGO policy."""
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
WAILS_VERSION = 'v3.0.0-beta.26'


def build_shell(destination, version, commit, profile, base_env=None):
    if profile not in ('preview', 'release'):
        raise ValueError('unknown desktop build profile')
    env = dict(base_env if base_env is not None else os.environ)
    env.update(GOOS='darwin', GOARCH='arm64', CGO_ENABLED='1', MACOSX_DEPLOYMENT_TARGET='14.0')
    env['CGO_CFLAGS'] = env.get('CGO_CFLAGS', '') + ' -mmacosx-version-min=14.0'
    env['CGO_LDFLAGS'] = env.get('CGO_LDFLAGS', '') + ' -mmacosx-version-min=14.0'
    flags = (f'-s -w -X teamcross/internal/buildinfo.Version={version} '
             f'-X teamcross/internal/buildinfo.Commit={commit} -X main.buildProfile={profile}')
    subprocess.run(['go', 'build', '-mod=readonly', '-trimpath', '-tags', 'production',
                    '-ldflags', flags, '-o', str(destination), '.'],
                   cwd=ROOT / 'apps/desktop', env=env, check=True)
