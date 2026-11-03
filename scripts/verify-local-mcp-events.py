#!/usr/bin/env python3
"""Opt-in local MCP Events capability probe; all state stays in a fresh fixture.

Uses a small stdio event producer, a synthetic protocol control, and the installed
Codex app-server. Real model calls are limited to a dedicated gpt-5.6-luna thread.
No production Team Cross, user configuration, or existing thread is mutated.
"""

import argparse
import collections
import datetime
import json
import os
import queue
import shutil
import signal
import subprocess
import sys
import threading
import time
import uuid
from pathlib import Path


MODEL = "gpt-5.6-luna"
EVENT = "space.discussion.updated"
SPACE = "tcx-event-probe-space"


def stamp():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def save(path, value):
    temporary = path.with_suffix(path.suffix + ".next")
    temporary.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n")
    temporary.replace(path)


def append(path, value):
    with path.open("a") as file:
        file.write(json.dumps({"at": stamp(), **value}, ensure_ascii=False) + "\n")


def records(path):
    if not path.exists():
        return []
    return [json.loads(line) for line in path.read_text().splitlines() if line]


def serve(root):
    root = Path(root)
    lock = threading.RLock()
    stopped = threading.Event()
    streams = {}
    current = None
    seen = None
    target = None
    read = False

    def emit(value):
        with lock:
            append(root / "mcp-wire.jsonl", {"direction": "out", "pid": os.getpid(), "message": value})
            sys.stdout.write(json.dumps(value, ensure_ascii=False) + "\n")
            sys.stdout.flush()

    def notify(stream_id, kind, data):
        emit({"jsonrpc": "2.0", "method": "notifications/events/" + kind,
              "params": {**data, "_meta": {"io.modelcontextprotocol/subscriptionId": stream_id}}})

    def producer():
        nonlocal current, seen, read
        heartbeat = time.monotonic()
        while not stopped.wait(0.1):
            try:
                update = json.loads((root / "trigger.json").read_text())
            except (FileNotFoundError, ValueError):
                update = None
            with lock:
                if update and update["eventId"] != seen:
                    seen, current, read = update["eventId"], update, False
                    for stream_id in list(streams):
                        notify(stream_id, "event", {key: value for key, value in current.items() if key != "sourceText"})
                if time.monotonic() - heartbeat >= 30:
                    for stream_id in list(streams):
                        notify(stream_id, "heartbeat", {"cursor": current["cursor"] if current else "probe:0"})
                    heartbeat = time.monotonic()

    def schema(properties, required=()):
        return {"type": "object", "properties": properties, "required": list(required), "additionalProperties": False}

    string = {"type": "string"}
    tools = [
        {"name": "connect_current_session", "description": "Record the current MCP-proven thread in this isolated test fixture.",
         "inputSchema": schema({}), "annotations": {"readOnlyHint": False, "idempotentHint": True}},
        {"name": "read_space_event", "description": "Read the public fixture source for one delivered event and record its read receipt.",
         "inputSchema": schema({"eventId": string}, ["eventId"]), "annotations": {"readOnlyHint": True}},
        {"name": "finish_space_event", "description": "Record fixture processing completion after reading its source. No external writes.",
         "inputSchema": schema({"eventId": string, "status": {"enum": ["completed", "failed"]}, "summary": string},
                               ["eventId", "status", "summary"]),
         "annotations": {"readOnlyHint": False, "idempotentHint": True}},
    ]
    append(root / "lifecycle.jsonl", {"state": "started", "pid": os.getpid(), "ppid": os.getppid()})
    threading.Thread(target=producer, daemon=True).start()
    try:
        for line in sys.stdin:
            request = json.loads(line)
            with lock:
                append(root / "mcp-wire.jsonl", {"direction": "in", "pid": os.getpid(), "message": request})
                method, params = request["method"], request.get("params") or {}
                if method == "notifications/cancelled":
                    streams.pop(params.get("requestId"), None)
                    continue
                if "id" not in request:
                    continue
                result, error = {}, None
                if method == "initialize":
                    result = {"protocolVersion": params.get("protocolVersion", "2025-11-25"),
                              "serverInfo": {"name": "teamcross-event-probe", "version": "1"},
                              "capabilities": {"tools": {}, "events": {}, "experimental": {
                                  "teamcross/event-probe": {"delivery": ["push", "poll"]}}},
                              "instructions": "Native event: space.discussion.updated, spaceId tcx-event-probe-space. "
                                              "Streaming requires the host to open events/stream; ordinary tools do not subscribe."}
                elif method == "server/discover":
                    result = {"resultType": "complete", "supportedVersions": ["2026-07-28"],
                              "serverInfo": {"name": "teamcross-event-probe", "version": "1"},
                              "capabilities": {"tools": {}, "events": {}}}
                elif method == "tools/list":
                    result = {"tools": tools}
                elif method in ("resources/list", "resources/templates/list", "prompts/list"):
                    result = {"resources/list": {"resources": []}, "resources/templates/list": {"resourceTemplates": []},
                              "prompts/list": {"prompts": []}}[method]
                elif method == "events/list":
                    result = {"events": [{"name": EVENT, "description": "A public fixture discussion changed.",
                                          "delivery": ["push", "poll"],
                                          "inputSchema": schema({"spaceId": {"type": "string", "enum": [SPACE]}}, ["spaceId"]),
                                          "payloadSchema": schema({"spaceId": string, "annotationId": string, "replyCount": {"type": "integer"}},
                                                                  ["spaceId", "annotationId", "replyCount"])}]}
                elif method in ("events/stream", "events/poll"):
                    if params.get("name") != EVENT or params.get("arguments", {}).get("spaceId") != SPACE:
                        error = {"code": -32602, "message": "Invalid fixture event scope"}
                    elif method == "events/stream":
                        streams[request["id"]] = params
                        notify(request["id"], "active", {"cursor": current["cursor"] if current else "probe:0", "truncated": False})
                        if current and params.get("cursor") == "probe:0":
                            notify(request["id"], "event", {key: value for key, value in current.items() if key != "sourceText"})
                        continue
                    else:
                        batch = [current] if current and params.get("cursor") == "probe:0" else []
                        result = {"events": [{key: value for key, value in event.items() if key != "sourceText"} for event in batch],
                                  "cursor": current["cursor"] if current else "probe:0", "nextPollMs": 1000, "truncated": False}
                elif method == "tools/call":
                    name, arguments, meta = params.get("name"), params.get("arguments") or {}, params.get("_meta") or {}
                    turn_meta = meta.get("x-codex-turn-metadata") or {}
                    if isinstance(turn_meta, str):
                        try:
                            turn_meta = json.loads(turn_meta)
                        except ValueError:
                            turn_meta = {}
                    identity = meta.get("threadId") or turn_meta.get("thread_id") or meta.get("codex/thread-id") or meta.get("codex/threadId")
                    value, bad = {}, False
                    if name == "connect_current_session":
                        target = identity
                        value = {"linked": identity is not None, "threadId": identity, "spaceId": SPACE, "streamOpen": bool(streams)}
                        append(root / "receipts.jsonl", {"kind": "connected", "threadId": identity, "metaKeys": sorted(meta)})
                    elif not current or arguments.get("eventId") != current["eventId"]:
                        value, bad = {"error": "Fixture event is unavailable"}, True
                    elif target and identity != target:
                        value, bad = {"error": "Current thread does not match the connected fixture thread"}, True
                    elif name == "read_space_event":
                        read = True
                        value = {"eventId": current["eventId"], "sourceText": current["sourceText"], "spaceId": SPACE}
                        append(root / "receipts.jsonl", {"kind": "read", "threadId": identity, "eventId": current["eventId"]})
                    elif name == "finish_space_event" and read:
                        value = {"recorded": True, "status": arguments.get("status")}
                        append(root / "receipts.jsonl", {"kind": "finished", "threadId": identity, "eventId": current["eventId"],
                                                          "status": arguments.get("status"), "summary": arguments.get("summary")})
                    else:
                        value, bad = {"error": "Read the event before finishing"}, True
                    result = {"isError": bad, "content": [{"type": "text", "text": json.dumps(value)}]}
                elif method != "ping":
                    error = {"code": -32601, "message": "Unknown fixture method"}
                emit({"jsonrpc": "2.0", "id": request["id"], **({"error": error} if error else {"result": result})})
    finally:
        stopped.set()
        append(root / "lifecycle.jsonl", {"state": "closed", "pid": os.getpid(), "openStreams": len(streams)})


class Wire:
    def __init__(self, command, root, env=None, cwd=None, native=False):
        self.root, self.native = root, native
        self.process = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                        cwd=cwd, env=env, text=True, start_new_session=True)
        self.messages, self.condition, self.next_id = [], threading.Condition(), 0
        self.stderr = root / "stderr.log"
        threading.Thread(target=self.reader, daemon=True).start()
        threading.Thread(target=self.errors, daemon=True).start()

    def reader(self):
        for line in self.process.stdout:
            try:
                message = json.loads(line)
            except ValueError:
                continue
            with self.condition:
                append(self.root / "client-wire.jsonl", {"direction": "in", "message": message})
                self.messages.append(message)
                self.condition.notify_all()
            if self.native and "id" in message and "method" in message:
                self.send({"id": message["id"], "error": {"code": -32601, "message": "Unsupported fixture request; no broad approvals"}})

    def errors(self):
        with self.stderr.open("w") as file:
            for line in self.process.stderr:
                file.write(line)

    def send(self, message):
        append(self.root / "client-wire.jsonl", {"direction": "out", "message": message})
        self.process.stdin.write(json.dumps(message) + "\n")
        self.process.stdin.flush()

    def wait(self, predicate, timeout=30, start=0):
        end = time.monotonic() + timeout
        with self.condition:
            while True:
                for message in self.messages[start:]:
                    if predicate(message):
                        return message
                if self.process.poll() is not None:
                    raise RuntimeError("Fixture process exited: " + str(self.process.returncode))
                remaining = end - time.monotonic()
                if remaining <= 0:
                    raise TimeoutError("Timed out waiting for fixture response")
                self.condition.wait(min(remaining, 1))

    def call(self, method, params=None, timeout=30):
        self.next_id += 1
        request_id = self.next_id
        self.send({"jsonrpc": "2.0", "id": request_id, "method": method, "params": params or {}})
        return self.wait(lambda message: message.get("id") == request_id and "method" not in message, timeout)

    def close(self):
        try:
            self.process.stdin.close()
        except (BrokenPipeError, OSError):
            pass
        try:
            self.process.wait(timeout=8)
        except subprocess.TimeoutExpired:
            os.killpg(self.process.pid, signal.SIGTERM)
            try:
                self.process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(self.process.pid, signal.SIGKILL)
                self.process.wait(timeout=5)
        # Only this runner's new process group is eligible for cleanup.
        group_alive = []
        for line in subprocess.check_output(["ps", "-axo", "pid=,pgid="], text=True).splitlines():
            pid, group = map(int, line.split())
            if group == self.process.pid:
                group_alive.append(pid)
        if group_alive:
            os.killpg(self.process.pid, signal.SIGTERM)
            time.sleep(0.3)
        return {"pid": self.process.pid, "exitCode": self.process.returncode, "remainingGroupPids": group_alive}


def trigger(root):
    token = uuid.uuid4().hex
    event = {"eventId": "tcx_evt_" + token, "name": EVENT, "timestamp": stamp(),
             "data": {"spaceId": SPACE, "annotationId": "fixture-annotation", "replyCount": 1},
             "cursor": "probe:1", "sourceText": "Fixture review marker TCX_SOURCE_" + token}
    save(root / "trigger.json", event)
    return event


def protocol_control(root, script):
    root.mkdir()
    wire = Wire([sys.executable, str(script), "--serve", str(root)], root)
    result = {"kind": "synthetic protocol control", "modelUsed": False}
    try:
        assert "result" in wire.call("initialize", {"protocolVersion": "2025-11-25", "clientInfo": {"name": "fixture-control", "version": "1"}, "capabilities": {}})
        wire.send({"jsonrpc": "2.0", "method": "notifications/initialized"})
        assert wire.call("events/list")["result"]["events"][0]["name"] == EVENT
        meta = {"codex/thread-id": "synthetic-control-thread"}
        assert wire.call("tools/call", {"name": "connect_current_session", "arguments": {}, "_meta": meta})["result"]["isError"] is False
        wire.send({"jsonrpc": "2.0", "id": "control-stream", "method": "events/stream", "params": {"name": EVENT, "arguments": {"spaceId": SPACE}, "cursor": None}})
        wire.wait(lambda message: message.get("method") == "notifications/events/active")
        event = trigger(root)
        delivered = wire.wait(lambda message: message.get("method") == "notifications/events/event")
        assert delivered["params"]["eventId"] == event["eventId"]
        assert delivered["params"]["_meta"]["io.modelcontextprotocol/subscriptionId"] == "control-stream"
        assert wire.call("tools/call", {"name": "read_space_event", "arguments": {"eventId": event["eventId"]}, "_meta": meta})["result"]["isError"] is False
        assert wire.call("tools/call", {"name": "finish_space_event", "arguments": {"eventId": event["eventId"], "status": "completed", "summary": event["sourceText"]}, "_meta": meta})["result"]["isError"] is False
        wire.send({"jsonrpc": "2.0", "method": "notifications/cancelled", "params": {"requestId": "control-stream"}})
        wire.send({"jsonrpc": "2.0", "id": "replay-stream", "method": "events/stream", "params": {"name": EVENT, "arguments": {"spaceId": SPACE}, "cursor": "probe:0"}})
        replay = wire.wait(lambda message: message.get("method") == "notifications/events/event" and message["params"]["_meta"]["io.modelcontextprotocol/subscriptionId"] == "replay-stream")
        assert replay["params"]["eventId"] == event["eventId"]
        wire.send({"jsonrpc": "2.0", "method": "notifications/cancelled", "params": {"requestId": "replay-stream"}})
        assert "result" in wire.call("ping")
        result.update({"passed": True, "subscription": True, "delivery": True, "readReceipt": True, "finishReceipt": True, "replaySameEventId": True})
    finally:
        result["cleanup"] = wire.close()
    return result


def native_probe(root, script, codex, auth, repo, modern, apps=False, real_model=True):
    root.mkdir()
    home = root / "codex-home"
    home.mkdir(mode=0o700)
    (home / "auth.json").symlink_to(auth)
    config = ('model="' + MODEL + '"\nmodel_reasoning_effort="low"\napproval_policy="on-request"\nsandbox_mode="read-only"\n'
              '[features]\napps=' + str(apps).lower() + '\nplugins=false\nmemories=false\nmulti_agent=false\nhooks=false\ncode_mode=false\ncode_mode_host=true\n'
              'mcp_2026_07_28=' + str(modern).lower() + '\n'
              '[mcp_servers.event_probe]\ncommand=' + json.dumps(sys.executable) + '\nargs=' + json.dumps([str(script), "--serve", str(root)]) + '\n'
              'startup_timeout_sec=10\ntool_timeout_sec=10\n'
              '[mcp_servers.event_probe.tools.connect_current_session]\napproval_mode="approve"\n'
              '[mcp_servers.event_probe.tools.read_space_event]\napproval_mode="approve"\n'
              '[mcp_servers.event_probe.tools.finish_space_event]\napproval_mode="approve"\n')
    (home / "config.toml").write_text(config)
    environment = {key: value for key, value in os.environ.items() if not key.startswith(("CODEX_", "CLAUDE_")) and key != "CLAUDECODE"}
    environment["CODEX_HOME"] = str(home)
    wire = Wire([codex, "app-server", "--listen", "stdio://"], root, env=environment, cwd=repo, native=True)
    result = {"kind": "installed Codex app-server with a synthetic outer client", "mcp2026Feature": modern,
              "model": MODEL, "appsEnabled": apps, "realModelTurn": real_model,
              "desktopUiTested": False, "autoTurnStartedByProbe": False}
    try:
        initialized = wire.call("initialize", {"clientInfo": {"name": "teamcross_mcp_events_probe", "version": "1"}, "capabilities": {"experimentalApi": True}})
        assert "result" in initialized, initialized
        wire.send({"method": "initialized", "params": {}})
        started = wire.call("thread/start", {"model": MODEL, "cwd": str(repo), "approvalPolicy": "on-request", "sandbox": "read-only", "allowProviderModelFallback": False,
                                             "developerInstructions": "Dedicated MCP Events acceptance fixture. Only use event_probe MCP tools. Do not run commands, browse, delegate, or change files. "
                                                                      "The user authorizes observing this fixture space for 60 seconds. If an event independently starts a later turn, read its source with read_space_event, "
                                                                      "then finish_space_event with completed and a summary including the source marker. Never invent a receipt."})
        assert "result" in started, started
        assert started["result"]["model"] == MODEL, started
        thread = started["result"]["thread"]["id"]
        result["threadId"] = thread
        if real_model:
            turn = wire.call("turn/start", {"threadId": thread, "input": [{"type": "text", "text": "Call event_probe connect_current_session once for this current conversation. Do not read or finish any event now. After that call, reply exactly PROBE_READY."}]})
            assert "result" in turn, turn
            first_turn = turn["result"]["turn"]["id"]
            completed = wire.wait(lambda message: message.get("method") == "turn/completed" and message.get("params", {}).get("turn", {}).get("id") == first_turn, timeout=100)
            result["initialTurnStatus"] = completed["params"]["turn"]["status"]
            result["initialTurnId"] = first_turn
        else:
            tool = wire.call("mcpServer/tool/call", {"threadId": thread, "server": "event_probe", "tool": "connect_current_session", "arguments": {},
                                                     "_meta": {"threadId": thread}})
            assert "result" in tool and not tool["result"].get("isError"), tool
            result["directToolCallSucceeded"] = True
        connected = [item for item in records(root / "receipts.jsonl") if item["kind"] == "connected"]
        assert connected and connected[-1]["threadId"] == thread, connected
        result["mcpIdentityMatchesThread"] = True
        before = len(wire.messages)
        subscription_id = "tcx_probe_" + uuid.uuid4().hex
        stream = wire.call("mcpServer/event/stream/start", {"threadId": thread, "server": "event_probe", "subscriptionId": subscription_id,
                                                           "name": EVENT, "arguments": {"spaceId": SPACE}})
        result["streamStartResponse"] = stream
        result["streamStarted"] = "result" in stream
        if result["streamStarted"]:
            wire.wait(lambda message: message.get("method") == "mcpServer/event/stream/notification" and message.get("params", {}).get("subscriptionId") == subscription_id, timeout=15)
            event = trigger(root)
            result["triggeredEventId"] = event["eventId"]
            start = time.monotonic()
            while time.monotonic() - start < 60 and not any(item.get("kind") == "finished" for item in records(root / "receipts.jsonl")):
                time.sleep(0.25)
            result["idleObservationSeconds"] = round(time.monotonic() - start, 2)
            result["stopResponse"] = wire.call("mcpServer/event/stream/stop", {"subscriptionId": subscription_id})
        else:
            result["idleDelivery"] = "not reached: subscription prerequisite rejected"
        later = wire.messages[before:]
        result["automaticLaterTurns"] = [message for message in later if message.get("method") == "turn/started"]
        result["eventStreamNotifications"] = [message for message in later if message.get("method") == "mcpServer/event/stream/notification"]
        result["readReceipts"] = [item for item in records(root / "receipts.jsonl") if item["kind"] == "read"]
        result["finishReceipts"] = [item for item in records(root / "receipts.jsonl") if item["kind"] == "finished"]
    finally:
        result["cleanup"] = wire.close()
        result["observedMcpRequests"] = dict(collections.Counter(item["message"]["method"] for item in records(root / "mcp-wire.jsonl")
                                                                 if item["direction"] == "in" and "method" in item["message"]))
        result["serverLifecycle"] = records(root / "lifecycle.jsonl")
        save(root / "result.json", result)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--serve", type=Path)
    parser.add_argument("--fixture-dir", type=Path)
    parser.add_argument("--codex-bin", default=shutil.which("codex"))
    parser.add_argument("--auth-source", type=Path, default=Path.home() / ".codex/auth.json")
    args = parser.parse_args()
    if args.serve:
        serve(args.serve)
        return 0
    if not args.fixture_dir or not args.auth_source.is_file():
        parser.error("A fresh fixture directory and existing authorized Codex authentication are required")
    root = args.fixture_dir.resolve()
    if root.exists() and any(root.iterdir()):
        parser.error("Choose a fresh empty fixture directory; existing files must not be changed")
    root.mkdir(mode=0o700, parents=True, exist_ok=True)
    repo = root / "repo"
    repo.mkdir(exist_ok=True)
    subprocess.run(["git", "init", "-q", str(repo)], check=True)
    script = Path(__file__).resolve()
    report = {"startedAt": stamp(), "fixtureDir": str(root), "codexVersion": subprocess.check_output([args.codex_bin, "--version"], text=True).strip(),
              "model": MODEL, "environment": "one Mac; isolated test repository and Codex homes", "productionCodeChanged": False,
              "protocolControl": protocol_control(root / "protocol-control", script), "nativeCases": []}
    save(root / "summary.json", report)
    for modern, apps, real_model, name in [(False, False, True, "native-default"), (True, False, True, "native-mcp2026"),
                                          (True, True, False, "native-apps-enabled")]:
        print("Starting " + name, flush=True)
        try:
            case = native_probe(root / name, script, args.codex_bin, args.auth_source.resolve(), repo, modern, apps, real_model)
        except Exception as error:
            evidence = root / name / "result.json"
            case = json.loads(evidence.read_text()) if evidence.exists() else {}
            case["probeError"] = {"type": type(error).__name__, "message": str(error)}
            report["nativeCases"].append(case)
            save(root / "summary.json", report)
            raise
        report["nativeCases"].append(case)
        save(root / "summary.json", report)
        print(json.dumps(report["nativeCases"][-1], ensure_ascii=False), flush=True)
    report["finishedAt"] = stamp()
    save(root / "summary.json", report)
    print(json.dumps({"summary": str(root / "summary.json"), "protocolControlPassed": report["protocolControl"]["passed"],
                      "nativeStreamStarted": [case["streamStarted"] for case in report["nativeCases"]]}, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
