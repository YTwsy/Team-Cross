#!/usr/bin/env python3
"""Exercise App replacement, legacy plugin processes and idle Core upgrades.

Uses actual old/new executables and disposable data, no model or user profile.
--old-binary may be a pre-bootstrap release; new and next must contain this fix
and report distinct commits (preferably with the same version).
"""
import argparse
import concurrent.futures
import json
import os
from pathlib import Path
import select
import shutil
import subprocess
import time
import urllib.request

parser = argparse.ArgumentParser()
for name in ("old-binary", "new-binary", "next-binary", "codex-bin", "output"):
    parser.add_argument("--" + name, required=True)
args = parser.parse_args()
old, new, next_binary, codex, root = [Path(value).resolve() for value in
    (args.old_binary, args.new_binary, args.next_binary, args.codex_bin, args.output)]
root.mkdir(parents=True, exist_ok=False)
native_home = root / "empty-native-home"
native_home.mkdir()
env = dict(os.environ, CODEX_HOME=str(native_home))
data = root / "data with spaces"
data.mkdir()
installed = root / "Team Cross's App.app/Contents/Resources/teamcross"
package = root / "plugin"
processes, directories = [], [data]
sequence = 0


def replace(source, destination=installed):
    destination.parent.mkdir(parents=True, exist_ok=True)
    stage = destination.with_name(".upgrade-stage")
    shutil.copy2(source, stage)
    os.replace(stage, destination)


def run(binary, *argv):
    result = subprocess.run([str(binary), *map(str, argv)], env=env,
                            text=True, capture_output=True, timeout=35, check=True)
    return json.loads(result.stdout)


def status(directory=data):
    return run(new, "status", "--json", "--data-dir", directory)


def stop(directory=data):
    subprocess.run([str(new), "stop", "--force", "--data-dir", str(directory)],
                   env=env, capture_output=True, timeout=30, check=True)


def start_mcp(binary):
    log = open(root / f"mcp-{len(processes)}.log", "w")
    process = subprocess.Popen([str(binary), "mcp", "--ui", "--data-dir", str(data)],
                               env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                               stderr=log, text=True)
    processes.append((process, log))
    rpc(process, "initialize", {})
    return process


def rpc(process, method, params):
    global sequence
    sequence += 1
    process.stdin.write(json.dumps({"jsonrpc": "2.0", "id": sequence,
                                   "method": method, "params": params}) + "\n")
    process.stdin.flush()
    deadline = time.monotonic() + 35
    while time.monotonic() < deadline:
        if not select.select([process.stdout], [], [], max(0, deadline-time.monotonic()))[0]:
            break
        line = process.stdout.readline()
        assert line, f"MCP exited: {process.poll()}"
        response = json.loads(line)
        if response.get("id") == sequence:
            assert "error" not in response, response
            return response["result"]
    raise AssertionError("MCP response timed out")


def info(process):
    result = rpc(process, "tools/call", {"name": "teamcross_ui_read", "arguments": {"path": "info"}})
    assert not result.get("isError"), result
    return result.get("structuredContent") or json.loads(result["content"][0]["text"])


def api(directory, path, body=None):
    c = json.loads((directory / "connection.json").read_text())
    request = urllib.request.Request(c["url"] + "/api/" + path,
        data=None if body is None else json.dumps(body).encode(),
        headers={"Authorization": "Bearer " + c["token"], "Content-Type": "application/json"})
    return json.load(urllib.request.urlopen(request, timeout=15))


try:
    old_build, new_build, next_build = [run(binary, "version", "--json") for binary in (old, new, next_binary)]
    assert len({b["commit"] for b in (old_build, new_build, next_build)}) == 3
    replace(old)
    # Reproduce the original expired App CLI layout without modifying user settings.
    (data / "settings.json").write_text(json.dumps({"binary": "/Applications/ChatGPT.app/Contents/Resources/codex",
        "desktopApp": "/Applications/ChatGPT.app", "claudeBinary": "/usr/bin/false"}))
    (data / "preserved.txt").write_text("keep exact fixture data")
    exported = run(installed, "plugin", "export", "--plugin-dir", package, "--data-dir", data)
    runtime = Path(exported["binary"])
    legacy_mcp = start_mcp(runtime)
    before_info = info(legacy_mcp)
    before = status()
    assert before["commit"] == old_build["commit"] and before_info["codexError"], before

    # App update and its opted-in sync refresh the stable bootstrap, while the
    # old MCP stays alive. Concurrent App/CLI calls must converge to one Core.
    replace(new)
    run(installed, "plugin", "export", "--plugin-dir", package, "--data-dir", data)
    with concurrent.futures.ThreadPoolExecutor(max_workers=5) as pool:
        starts = list(pool.map(lambda _: run(installed, "serve", "--no-open", "--json", "--data-dir", data), range(5)))
    after = status()
    assert {s["service"]["pid"] for s in starts} == {after["pid"]}
    assert after["pid"] != before["pid"] and after["commit"] == new_build["commit"]
    assert before["url"] == after["url"]
    try:
        api(data, "control/upgrade", {"instance": "not-the-current-instance"})
        raise AssertionError("upgrade accepted another instance")
    except urllib.error.HTTPError as error:
        assert error.code == 409
    c = json.loads((data / "connection.json").read_text())
    for headers in ({}, {"Authorization": "Bearer " + c["token"], "Origin": c["url"]}):
        request = urllib.request.Request(c["url"] + "/api/control/upgrade",
            data=json.dumps({"instance": c["instance"]}).encode(), headers=headers)
        try:
            urllib.request.urlopen(request, timeout=10)
            raise AssertionError("upgrade accepted unauthenticated or browser request")
        except urllib.error.HTTPError as error:
            assert error.code == 403
    recovered = info(legacy_mcp)
    assert not recovered["codexError"] and recovered["binary"] == str(codex), recovered.get("codexError")
    assert legacy_mcp.poll() is None
    assert (data / "preserved.txt").read_text() == "keep exact fixture data"

    # A process loaded before the update can no longer resurrect its old Core.
    stop()
    assert not status()["running"]
    assert not info(legacy_mcp)["codexError"]
    assert status()["commit"] == new_build["commit"]
    assert status()["executable"] == str(installed)

    # Future App replacements work before any plugin resync. A resident MCP
    # also compares the on-disk executable, not its old in-memory buildinfo.
    stop()
    run(installed, "serve", "--no-open", "--json", "--data-dir", data,
        "--codex-bin", "/missing/obsolete-cli", "--repo", root, "--test-loopback")
    saved_cli = "/Applications/ChatGPT.app/Contents/Resources/codex"
    api(data, "settings", {"binary": saved_cli, "claudeBinary": "/usr/bin/false"})
    resident = start_mcp(runtime)
    info(resident)
    previous = status()
    copied_plugin = runtime.read_bytes()
    replace(next_binary)
    assert run(runtime, "version", "--json") == next_build
    assert not info(resident)["codexError"]
    assert status()["commit"] == next_build["commit"] and status()["pid"] != previous["pid"]
    assert "--test-loopback" in status()["startArgs"] and str(root) in status()["startArgs"]
    assert saved_cli in status()["startArgs"] and "/missing/obsolete-cli" not in status()["startArgs"]
    assert api(data, "info")["settings"]["binary"] == saved_cli
    assert runtime.read_bytes() == copied_plugin, "test unexpectedly synchronized the plugin"
    assert resident.poll() is None and legacy_mcp.poll() is None

    # A missing owning installation fails clearly, never silently executes the
    # older bundled copy. Restore it immediately for cleanup.
    moved = installed.with_name("temporarily-moved")
    installed.rename(moved)
    try:
        failed = subprocess.run([str(runtime), "version", "--json"], env=env, capture_output=True, text=True, timeout=10)
        assert failed.returncode and "installation is unavailable" in failed.stderr
    finally:
        moved.rename(installed)

    # A synthetic outstanding delivery protects a real Core despite active=0.
    # It is not evidence of a model turn or a real shared execution.
    busy = root / "busy-data"
    busy.mkdir()
    directories.append(busy)
    (busy / "agent-pairings.json").write_text(json.dumps({"pairings": {}, "requests": {
        "fixture-delivery": {"id": "fixture-delivery", "state": "submitted"}}}))
    busy_binary = root / "Busy App.app/Contents/Resources/teamcross"
    replace(new, busy_binary)
    run(busy_binary, "serve", "--no-open", "--json", "--data-dir", busy)
    busy_before = status(busy)
    assert busy_before["upgradeBlocked"]
    replace(next_binary, busy_binary)
    pending = run(busy_binary, "serve", "--no-open", "--json", "--data-dir", busy)["service"]
    assert pending["pid"] == busy_before["pid"] and pending["updatePending"]
    assert pending["installedCommit"] == next_build["commit"]
    assert api(busy, "info")["updatePending"]

    report = {"oldBuild": old_build, "newBuild": new_build, "nextBuild": next_build,
        "legacyCodexError": before_info["codexError"], "recoveredCodexVersion": recovered["codexVersion"],
        "sameVersionDifferentCommit": old_build["version"] == new_build["version"] == next_build["version"],
        "legacyPluginFirst": True, "concurrentUpgrade": True, "sameBrowserAddress": True,
        "residentLegacyMCP": True, "oldCoreResurrectionPrevented": True,
        "futureAppReplacementWithoutPluginSync": True, "residentMCPUsesDiskBuild": True,
        "missingInstallationFailsClosed": True, "outstandingDeliveryDefersUpgrade": True,
        "dataPreserved": True, "startupOptionsPreserved": True, "upgradeIdentityAndAuth": True, "modelInvoked": False}
    (root / "report.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report, indent=2))
finally:
    for process, log in processes:
        if process.poll() is None:
            process.stdin.close()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.terminate()
                process.wait(timeout=10)
        log.close()
    for directory in directories:
        if status(directory).get("running"):
            stop(directory)
