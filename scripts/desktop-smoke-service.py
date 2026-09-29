#!/usr/bin/env python3
"""Harmless desktop transport fixture. No models, user sessions, or real Core."""
import argparse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import secrets
import signal
import threading
import uuid

parser = argparse.ArgumentParser()
parser.add_argument('directory', type=Path)
parser.add_argument('--manifest', required=True, type=Path, help='desktop-build.json from this preview build')
args = parser.parse_args()
directory = args.directory.resolve()
directory.mkdir(parents=True, exist_ok=True, mode=0o700)
marker = directory / '.desktop-smoke-fixture'
if not marker.exists() and any(directory.iterdir()):
    parser.error('fixture requires a new empty directory or its own marked directory')
marker.touch(mode=0o600)
manifest = json.loads(args.manifest.read_text())
token = secrets.token_hex(32)
mode = 'auto'
lock = threading.Lock()


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def send(self, status, value):
        data = json.dumps(value, ensure_ascii=False).encode()
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        if self.path == '/api/control/status':
            if self.headers.get('Authorization') != 'Bearer ' + token:
                self.send(403, {'error': 'unauthorized'}); return
            public = {k: v for k, v in connection.items() if k != 'token'}
            self.send(200, dict(public, running=True, active=0)); return
        if self.path == '/api/ui-language':
            with lock:
                self.send(200, {'mode': mode, 'resolved': 'zh-Hans' if mode != 'en' else 'en'})
            return
        self.send(404, {'error': 'unknown fixture route'})

    def do_POST(self):
        global mode
        if self.path != '/api/ui-language':
            self.send(404, {'error': 'unknown fixture route'}); return
        length = int(self.headers.get('Content-Length', '0'))
        if length > 4096:
            self.send(413, {'error': 'too large'}); return
        try:
            body = json.loads(self.rfile.read(length))
            if body['mode'] not in ('auto', 'zh-Hans', 'en'):
                raise ValueError('mode')
        except (ValueError, KeyError, TypeError):
            self.send(400, {'error': 'invalid preference'}); return
        with lock:
            mode = body['mode']
            with (directory / 'writes.jsonl').open('a') as observations:
                observations.write(json.dumps({'method': 'POST', 'path': self.path, 'mode': mode}) + '\n')
        if (directory / 'drop-write-response').exists():
            self.close_connection = True
            return
        self.send(200, {'mode': mode, 'resolved': 'zh-Hans' if mode != 'en' else 'en'})


server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
connection = dict(url=f'http://127.0.0.1:{server.server_port}', pid=os.getpid(), instance=str(uuid.uuid4()),
                  token=token, version=manifest['version'], commit=manifest['commit'], protocol=1, dataDir=str(directory))
temporary = directory / 'connection.fixture.tmp'
with os.fdopen(os.open(temporary, os.O_CREAT | os.O_WRONLY | os.O_TRUNC, 0o600), 'w') as stream:
    json.dump(connection, stream)
temporary.replace(directory / 'connection.json')
for name in (signal.SIGINT, signal.SIGTERM):
    signal.signal(name, lambda *_: threading.Thread(target=server.shutdown, daemon=True).start())
print(json.dumps({'pid': os.getpid(), 'directory': str(directory), 'url': connection['url']}), flush=True)
try:
    server.serve_forever()
finally:
    server.server_close()
