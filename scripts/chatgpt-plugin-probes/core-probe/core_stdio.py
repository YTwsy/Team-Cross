"""Fixture-scoped client for the existing Team Cross CLI stdio MCP."""
import json
import os
import queue
import subprocess
import threading
from pathlib import Path
from urllib.parse import urlparse


class CoreMCP:
    def __init__(self, manifest_path, binary):
        self.manifest = json.loads(Path(manifest_path).read_text())
        api = urlparse(self.manifest['api'])
        assert api.scheme == 'http' and api.hostname == '127.0.0.1' and api.port
        assert self.manifest['executionEnabled'] is False
        os.kill(self.manifest['pid'], 0)
        self.process = subprocess.Popen([binary, 'mcp', '--data-dir', self.manifest['dataDir']],
                                        stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                        stderr=subprocess.PIPE, text=True)
        self.responses = queue.Queue()
        self.next_id = 0
        self.trace = []
        self.stderr = []
        threading.Thread(target=self._stdout, daemon=True).start()
        threading.Thread(target=self._stderr, daemon=True).start()
        self.rpc('initialize', {'protocolVersion': '2024-11-05', 'capabilities': {},
                               'clientInfo': {'name': 'teamcross-core-ui-probe', 'version': '0.2.0'}})
        self.process.stdin.write(json.dumps({'jsonrpc': '2.0', 'method': 'notifications/initialized'}) + '\n')
        self.process.stdin.flush()

    def _stdout(self):
        for line in self.process.stdout:
            self.responses.put(json.loads(line))
        self.responses.put({'processExited': True})

    def _stderr(self):
        for line in self.process.stderr:
            self.stderr.append(line.rstrip())

    def rpc(self, method, params):
        os.kill(self.manifest['pid'], 0)
        self.next_id += 1
        self.process.stdin.write(json.dumps({'jsonrpc': '2.0', 'id': self.next_id,
                                            'method': method, 'params': params}) + '\n')
        self.process.stdin.flush()
        response = self.responses.get(timeout=15)
        assert response.get('id') == self.next_id and 'error' not in response, response
        self.trace.append({'method': method, 'params': params, 'response': response})
        return response['result']

    def call(self, name, arguments):
        assert name in ('read_material', 'read_context', 'reply_to_annotation')
        assert arguments['id'] == self.manifest['spaceId']
        value = self.rpc('tools/call', {'name': name, 'arguments': arguments})
        assert not value.get('isError'), value
        return json.loads(value['content'][0]['text'])

    def read(self):
        m = self.manifest
        material = self.call('read_material', {'id': m['spaceId'], 'materialId': m['materialId'],
                                              'version': 1, 'view': 'conversation',
                                              'turnId': 'summary', 'itemId': 'answer'})
        note = self.call('read_context', {'id': m['spaceId'], 'kind': 'annotations',
                                        'annotationId': m['annotationId']})
        return {'material': material, 'annotation': note, 'spaceId': m['spaceId'],
                'materialId': m['materialId'], 'annotationId': m['annotationId'], 'version': 1}

    def reply(self, text, request_id):
        m = self.manifest
        return self.call('reply_to_annotation', {'id': m['spaceId'], 'annotationId': m['annotationId'],
                                                'text': text, 'requestId': request_id})

    def close(self):
        self.process.stdin.close()
        try:
            self.process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.process.terminate()
            self.process.wait(timeout=5)
        return self.process.returncode
