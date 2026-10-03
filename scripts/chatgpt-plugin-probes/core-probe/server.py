#!/usr/bin/env python3
"""Add an MCP Apps panel to the existing, isolated Team Cross stdio MCP."""
import errno
import hashlib
import json
import os
from pathlib import Path
import re
import socket
import sys
import time
import uuid
from core_stdio import CoreMCP

ROOT = Path(__file__).resolve().parent
LOG = Path(os.environ['TEAMCROSS_CORE_PROBE_LOG'])
URI = 'ui://teamcross-core-probe/panel.html'
RUN = str(uuid.uuid4())
CORE = None


def record(event, **fields):
    with LOG.open('a', encoding='utf-8') as stream:
        stream.write(json.dumps({'time': time.time(), 'event': event, 'pid': os.getpid(),
                                 'ppid': os.getppid(), 'run': RUN, **fields}) + '\n')


def core():
    global CORE
    if CORE is None:
        CORE = CoreMCP(os.environ['TEAMCROSS_CORE_MANIFEST'], os.environ['TEAMCROSS_CORE_BINARY'])
        record('core_mcp_started', childPid=CORE.process.pid, api=CORE.manifest['api'])
    return CORE


def result(data):
    return {'content': [{'type': 'text', 'text': json.dumps(data, ensure_ascii=False)}], 'structuredContent': data}


def inventory():
    def tool(name, description, properties, required, read):
        return {'name': name, 'description': description,
                'inputSchema': {'type': 'object', 'properties': properties, 'required': required, 'additionalProperties': False},
                'annotations': {'readOnlyHint': read, 'destructiveHint': False, 'idempotentHint': True, 'openWorldHint': False},
                '_meta': {'ui': {'visibility': ['model', 'app']}, 'openai/widgetAccessible': True}}
    opened = tool('open_core_panel', 'Open the isolated Core test panel with synthetic materials only.', {}, [], True)
    opened['_meta'].update({'ui': {'resourceUri': URI, 'visibility': ['model', 'app']},
                            'openai/outputTemplate': URI,
                            'openai/ui': {'entrypoints': [{'type': 'global'}, {'type': 'thread'}]}})
    return [opened, tool('read_test_material', 'Read the single synthetic material and annotation via existing Team Cross MCP.', {}, [], True),
            tool('reply_test_annotation', 'Save a synthetic test reply via existing Team Cross MCP. Never starts a model.',
                 {'nonce': {'type': 'string', 'format': 'uuid'}, 'requestId': {'type': 'string', 'maxLength': 100}},
                 ['nonce', 'requestId'], False)]


def handle(request):
    method = request.get('method')
    params = request.get('params') or {}
    args = params.get('arguments') or {}
    record('request', method=method, tool=params.get('name'), requestId=request.get('id'),
           nonce=args.get('nonce'), writeRequestId=args.get('requestId'))
    if method == 'initialize':
        return {'protocolVersion': params.get('protocolVersion', '2025-11-25'), 'capabilities': {'tools': {}, 'resources': {}},
                'serverInfo': {'name': 'teamcross-core-probe', 'version': '0.2.0'},
                'instructions': 'Only one synthetic fixture. Never access real collaborations or send model input.'}
    if method == 'ping':
        return {}
    if method == 'tools/list':
        return {'tools': inventory()}
    if method == 'resources/list':
        return {'resources': [{'uri': URI, 'name': 'Isolated Core Panel', 'mimeType': 'text/html;profile=mcp-app'}]}
    if method == 'resources/templates/list':
        return {'resourceTemplates': []}
    if method == 'resources/read':
        if params.get('uri') != URI:
            raise ValueError('Unknown resource')
        html = (ROOT / 'panel.html').read_text()
        record('resource_read', sha256=hashlib.sha256(html.encode()).hexdigest())
        return {'contents': [{'uri': URI, 'mimeType': 'text/html;profile=mcp-app', 'text': html,
                              '_meta': {'ui': {'prefersBorder': True, 'csp': {'connectDomains': [], 'resourceDomains': []}},
                                        'openai/ui': {'availableDisplayModes': ['inline', 'fullscreen']}}}]}
    if method == 'tools/call':
        name = params.get('name')
        if name == 'open_core_panel' and not args:
            return result({'run': RUN, 'pid': os.getpid(), 'syntheticFixtureOnly': True})
        if name == 'read_test_material' and not args:
            data = core().read()
            assert core().manifest['materialText'] in json.dumps(data, ensure_ascii=False)
            record('core_read', spaceId=data['spaceId'], materialId=data['materialId'], annotationId=data['annotationId'])
            return result(data)
        if name == 'reply_test_annotation' and set(args) == {'nonce', 'requestId'}:
            nonce = str(uuid.UUID(args['nonce']))
            if nonce != args['nonce'] or not re.fullmatch(r'(ui|native)-core-[0-9a-f-]{36}', args['requestId']):
                raise ValueError('Expected a dedicated test request ID and UUID nonce')
            saved = core().reply('UI-CORE-REPLY:' + nonce, args['requestId'])
            assert saved['status'] == 'saved'
            record('core_reply_saved', nonce=nonce, writeRequestId=args['requestId'],
                   annotationId=saved['annotationId'], replyId=saved['reply']['id'])
            return result(saved)
        raise ValueError('Only fixture-specific tools are available')
    raise ValueError('Unsupported method')


record('start', root=str(ROOT))
try:
    with socket.create_connection(('127.0.0.1', 9), timeout=0.1):
        raise RuntimeError('Network guard unexpectedly connected')
except OSError as error:
    denied = error.errno in (errno.EPERM, errno.EACCES)
    record('network_guard', denied=denied, errno=error.errno)
    if not denied:
        sys.exit(2)
try:
    for line in sys.stdin:
        request = json.loads(line)
        if 'id' not in request:
            record('notification', method=request.get('method'))
            continue
        response = {'jsonrpc': '2.0', 'id': request['id']}
        try:
            response['result'] = handle(request)
        except Exception as error:
            record('tool_error', message=str(error))
            response['error'] = {'code': -32602, 'message': str(error)}
        print(json.dumps(response, ensure_ascii=False), flush=True)
finally:
    if CORE is not None:
        record('core_mcp_stopped', childPid=CORE.process.pid, exitCode=CORE.close())
    record('stop')
