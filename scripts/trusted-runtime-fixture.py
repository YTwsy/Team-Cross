#!/usr/bin/env python3
"""Local-only read-only MCP fixture for the opt-in trusted-runtime acceptance."""
import json
import pathlib
import sys

for line in sys.stdin:
    request = json.loads(line)
    if 'id' not in request:
        continue
    method = request.get('method')
    if method == 'initialize':
        result = {'protocolVersion': request['params']['protocolVersion'], 'capabilities': {'tools': {}}, 'serverInfo': {'name': 'trusted-fixture', 'version': '1.0'}}
    elif method == 'tools/list':
        result = {'tools': [{'name': 'read_fixture', 'description': 'Read the dedicated trusted-runtime fixture marker.', 'inputSchema': {'type': 'object', 'properties': {}}, 'annotations': {'readOnlyHint': True}}]}
    elif method == 'tools/call' and request['params']['name'] == 'read_fixture':
        if len(sys.argv) > 1:
            pathlib.Path(sys.argv[1]).write_text('TCX_INHERITED_TOOL_583\n')
        result = {'content': [{'type': 'text', 'text': 'TCX_INHERITED_TOOL_583'}]}
    elif method == 'ping':
        result = {}
    else:
        print(json.dumps({'jsonrpc': '2.0', 'id': request['id'], 'error': {'code': -32601, 'message': 'Fixture method not found'}}), flush=True)
        continue
    print(json.dumps({'jsonrpc': '2.0', 'id': request['id'], 'result': result}), flush=True)
