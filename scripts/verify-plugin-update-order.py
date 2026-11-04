#!/usr/bin/env python3
"""Reproduce stop App -> replace App -> old host MCP -> start App, in isolation.

Uses production executables and one retained stdio process. No model, personal
configuration, or business writes. Browser/real-host UI checks are separate.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import queue
import shutil
import subprocess
import threading
import time

p = argparse.ArgumentParser()
for name in ("old-binary", "new-binary", "output"):
    p.add_argument("--" + name, required=True)
args = p.parse_args()
old, new, output = map(lambda x: Path(x).resolve(),
                       (args.old_binary, args.new_binary, args.output))
output.mkdir(parents=True, exist_ok=False)
data = output / "data"
data.mkdir()
native = output / "native-home"
native.mkdir()
env = dict(os.environ, CODEX_HOME=str(native))
installed = output / "Test App.app/Contents/Resources/teamcross"
package = output / "plugin"
timeline = []
process = None
sequence = 0
responses = queue.Queue()


def record(event, **fields):
    timeline.append(dict(event=event, at=time.time(), **fields))


def replace(source):
    installed.parent.mkdir(parents=True, exist_ok=True)
    stage = installed.with_name(".replacement")
    shutil.copy2(source, stage)
    os.replace(stage, installed)


def run(binary, *argv):
    result = subprocess.run([str(binary), *map(str, argv)], env=env,
                            capture_output=True, text=True, check=True, timeout=50)
    return json.loads(result.stdout)


def rpc(method, params):
    global sequence
    sequence += 1
    process.stdin.write(json.dumps(dict(jsonrpc="2.0", id=sequence, method=method, params=params)) + "\n")
    process.stdin.flush()
    while True:
        result = responses.get(timeout=50)
        if result.get("id") == sequence:
            assert "error" not in result, result
            return result["result"]


def read(path):
    result = rpc("tools/call", {"name": "teamcross_ui_read", "arguments": {"path": path}})
    assert not result.get("isError"), result
    return result.get("structuredContent") or json.loads(result["content"][0]["text"])


def status():
    result = run(installed, "status", "--json", "--data-dir", data)
    return {k: result.get(k) for k in ("running", "pid", "version", "commit", "executable")}


try:
    replace(old)
    exported = run(installed, "plugin", "export", "--plugin-dir", package, "--data-dir", data)
    with (output / "stdio.log").open("w") as log:
        process = subprocess.Popen([exported["binary"], "mcp", "--ui", "--data-dir", str(data)],
                                   env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                   stderr=log, text=True)
        def receive():
            for line in process.stdout:
                responses.put(json.loads(line))
        threading.Thread(target=receive, daemon=True).start()
        rpc("initialize", {"protocolVersion": "2025-06-18", "capabilities": {},
                           "clientInfo": {"name": "upgrade-order-test", "version": "1"}})
        legacy_html = rpc("resources/read", {"uri": "ui://teamcross/workspace-v1.html"})["contents"][0]["text"]
        (output / "legacy-panel.html").write_text(legacy_html)
        read("plugin/bootstrap")
        before = status()
        record("old_app_running", mcpPid=process.pid, core=before)
        run(installed, "stop", "--json", "--data-dir", data)
        assert not status()["running"]
        assert process.poll() is None
        record("old_app_stopped_host_mcp_retained", mcpPid=process.pid)
        replace(new)
        record("installation_replaced_app_not_started")
        # The same host MCP still returns its original shell; its first read
        # starts the installed Core, before any App startup/plugin sync.
        assert rpc("resources/read", {"uri": "ui://teamcross/workspace-v1.html"})["contents"][0]["text"] == legacy_html
        read("plugin/bootstrap")
        after = status()
        expected = run(new, "version", "--json")
        assert after["running"] and after["version"] == expected["version"]
        assert after["pid"] != before["pid"] and process.poll() is None
        record("plugin_opened_before_app", mcpPid=process.pid, core=after)
        # These are the two commands issued by the menu-bar App at startup.
        started = run(installed, "serve", "--no-open", "--json", "--data-dir", data)
        run(installed, "plugin", "export", "--plugin-dir", package, "--data-dir", data)
        assert started["service"]["pid"] == after["pid"] == status()["pid"]
        version = read("plugin/panel-version")
        bundle = read("plugin/panel-bundle?revision=" + version["revision"])
        digest = hashlib.sha256((bundle["style"] + "\0" + bundle["script"]).encode()).hexdigest()
        assert digest == version["revision"]
        assert "teamcross-panel-ready" in bundle["script"]
        assert process.poll() is None
        record("app_started_plugin_synced_same_core", mcpPid=process.pid, core=status(), revision=digest)
        (output / "result.json").write_text(json.dumps({"passed": True, "hostRestarted": False,
            "businessWrites": 0, "timeline": timeline}, indent=2) + "\n")
        print(json.dumps({"passed": True, "mcpPid": process.pid, "revision": digest, "output": str(output)}))
finally:
    if process is not None:
        process.stdin.close()
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.terminate()
            process.wait(timeout=5)
    subprocess.run([str(new), "stop", "--json", "--data-dir", str(data)],
                   env=env, capture_output=True, timeout=30)
