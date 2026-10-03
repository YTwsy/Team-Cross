#!/usr/bin/env python3
"""Small synthetic stdio MCP service. No user data or network service."""
import errno
import hashlib
import json
import os
from pathlib import Path
import socket
import sys
import time
import uuid

ROOT = Path(__file__).resolve().parent
LOG = Path(os.environ.get("TEAMCROSS_PROBE_LOG", str(ROOT / "probe.jsonl")))
RUN = str(uuid.uuid4())
URI = "ui://teamcross-local-probe/panel.html"


def record(event, **fields):
    LOG.parent.mkdir(parents=True, exist_ok=True)
    with LOG.open("a", encoding="utf-8") as stream:
        stream.write(json.dumps({"time": time.time(), "run": RUN, "pid": os.getpid(),
                                 "ppid": os.getppid(), "event": event, **fields}) + "\n")


def network_guard():
    try:
        with socket.create_connection(("127.0.0.1", 9), timeout=0.1):
            pass
    except OSError as error:
        record("network_guard", errno=error.errno, denied=error.errno in (errno.EPERM, errno.EACCES))
        return error.errno in (errno.EPERM, errno.EACCES)
    return False


ANNOTATIONS = {"readOnlyHint": True, "destructiveHint": False,
               "idempotentHint": True, "openWorldHint": False}
OUTPUT = {"type": "object", "properties": {
    "nonce": {"type": "string"}, "run": {"type": "string"},
    "pid": {"type": "integer"}, "networkDenied": {"type": "boolean"}},
    "required": ["nonce", "run", "pid", "networkDenied"], "additionalProperties": False}


def tools():
    common = {"annotations": ANNOTATIONS, "outputSchema": OUTPUT}
    return [
        {**common, "name": "probe_echo", "description": "Echo a synthetic nonce from the local stdio process.",
         "inputSchema": {"type": "object", "properties": {"nonce": {"type": "string", "maxLength": 200}},
                         "required": ["nonce"], "additionalProperties": False},
         "_meta": {"ui": {"visibility": ["model", "app"]}, "openai/widgetAccessible": True}},
        {**common, "name": "open_probe_panel", "description": "Open the synthetic local probe panel; no real Team Cross data.",
         "inputSchema": {"type": "object", "properties": {}, "additionalProperties": False},
         "_meta": {"ui": {"resourceUri": URI, "visibility": ["model", "app"]},
                   "openai/outputTemplate": URI, "openai/widgetAccessible": True,
                   "openai/ui": {"entrypoints": [{"type": "global"}, {"type": "thread"}]}}}
    ]


def result(nonce):
    data = {"nonce": nonce, "run": RUN, "pid": os.getpid(), "networkDenied": DENIED}
    return {"content": [{"type": "text", "text": json.dumps(data)}], "structuredContent": data}


def handle(request):
    method = request.get("method", "")
    params = request.get("params") or {}
    record("request", method=method, requestId=request.get("id"),
           tool=params.get("name"), nonce=(params.get("arguments") or {}).get("nonce"))
    if method == "initialize":
        return {"protocolVersion": params.get("protocolVersion", "2025-11-25"),
                "capabilities": {"tools": {}, "resources": {}},
                "serverInfo": {"name": "teamcross-local-probe", "version": "0.1.0"},
                "instructions": "Only synthetic compatibility data. Never read files or run shell commands for this probe."}
    if method == "ping":
        return {}
    if method == "tools/list":
        return {"tools": tools()}
    if method == "resources/list":
        return {"resources": [{"uri": URI, "name": "Local Probe Panel",
                               "mimeType": "text/html;profile=mcp-app"}]}
    if method == "resources/templates/list":
        return {"resourceTemplates": []}
    if method == "resources/read":
        if params.get("uri") != URI:
            raise ValueError("Unknown probe resource")
        html = (ROOT / "panel.html").read_text(encoding="utf-8")
        record("resource_read", uri=URI, sha256=hashlib.sha256(html.encode()).hexdigest())
        return {"contents": [{"uri": URI, "mimeType": "text/html;profile=mcp-app", "text": html,
                              "_meta": {"ui": {"prefersBorder": True,
                                               "csp": {"connectDomains": [], "resourceDomains": []}},
                                        "openai/ui": {"availableDisplayModes": ["inline", "fullscreen"]}}}]}
    if method == "tools/call":
        name = params.get("name")
        args = params.get("arguments") or {}
        if name == "probe_echo":
            nonce = args.get("nonce")
            if not isinstance(nonce, str) or len(nonce) > 200 or set(args) != {"nonce"}:
                raise ValueError("Expected a synthetic nonce string")
            return result(nonce)
        if name == "open_probe_panel" and not args:
            return result("panel-open")
        raise ValueError("Unknown probe tool")
    raise ValueError("Unsupported method: " + method)


record("start", executable=sys.executable, root=str(ROOT))
DENIED = network_guard()
if not DENIED:
    record("refused", reason="network sandbox not active")
    sys.exit(2)
try:
    for line in sys.stdin:
        try:
            request = json.loads(line)
            if "id" not in request:
                record("notification", method=request.get("method"))
                continue
            response = {"jsonrpc": "2.0", "id": request["id"]}
            try:
                response["result"] = handle(request)
            except ValueError as error:
                response["error"] = {"code": -32602, "message": str(error)}
            print(json.dumps(response), flush=True)
        except (ValueError, KeyError) as error:
            record("invalid_request", error=str(error))
finally:
    record("stop")
