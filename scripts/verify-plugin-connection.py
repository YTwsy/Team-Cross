#!/usr/bin/env python3
"""Real native CLI/cache checks in a fresh profile; no model or user App control."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument("--teamcross-bin", required=True)
parser.add_argument("--codex-bin", required=True)
parser.add_argument("--output", required=True)
args = parser.parse_args()
output = Path(args.output).resolve()
output.mkdir(mode=0o700, parents=True, exist_ok=False)
binary, codex = str(Path(args.teamcross_bin).resolve()), str(Path(args.codex_bin).resolve())
home, package = output / "native-home", output / "existing-source"
data, other = output / "original-core", output / "different-core"
workspace = output / "workspace"
workspace.mkdir()
env = {**os.environ, "CODEX_HOME": str(home)}
report, clients = {}, []


def run(*command):
    p = subprocess.run(command, cwd=output, env=env, capture_output=True, text=True, timeout=60)
    if p.returncode:
        raise AssertionError({"command": command, "stderr": p.stderr})
    return json.loads(p.stdout)


def plugin(action):
    return run(binary, "plugin", action, "--data-dir", str(other), "--codex-bin", codex)


def native():
    spec = importlib.util.spec_from_file_location("native_rpc", ROOT / "scripts/chatgpt-plugin-probes/native_rpc.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    client = module.NativeRPC([["--disable", "apps"]], {
        "plugins": {"teamcross@teamcross-local": {"enabled": True}, "foreign@sentinel": {"enabled": False}},
        "model": "gpt-5.6-luna", "web_search": "disabled",
    }, plugin_servers=["teamcross-ui"], codex_bin=codex, cwd=workspace, env=env)
    clients.append(client)
    client.initialize()
    tid = client.rpc("thread/start", {"cwd": str(workspace), "ephemeral": True,
        "model": "gpt-5.6-luna", "sandbox": "read-only", "approvalPolicy": "never"})["result"]["thread"]["id"]
    inventory = client.rpc("mcpServerStatus/list", {"threadId": tid})["result"]["data"]
    active = [s for s in inventory if s["runtimeStatus"] != "disabled"]
    assert len(active) == 1 and active[0]["pluginId"] == "teamcross@teamcross-local", inventory
    server = active[0]["name"]
    html = client.rpc("mcpServer/resource/read", {"threadId": tid, "server": server,
        "uri": "ui://teamcross/workspace-v1.html"})["result"]["contents"][0]["text"]
    assert html == (ROOT / "internal/mcpassets/dist/panel.html").read_text()
    report["resourceSha256"] = hashlib.sha256(html.encode()).hexdigest()

    def bootstrap():
        result = client.rpc("mcpServer/tool/call", {"threadId": tid, "server": server,
            "tool": "teamcross_ui_read", "arguments": {"path": "plugin/bootstrap"}})["result"]
        assert not result.get("isError"), result
    return bootstrap


try:
    assert plugin("sync")["state"] == "not_enabled"
    assert not home.exists(), "automatic first launch created a native profile"
    assert plugin("connection-status")["state"] == "not_installed"
    assert not home.exists(), "status created a native profile"
    home.mkdir()
    config = home / "config.toml"
    config.write_text('[mcp_servers.foreign_sentinel]\ncommand="/usr/bin/false"\nenabled=false\n\n[plugins."foreign@sentinel"]\nenabled=false\n')
    # Adopt a real earlier manual installation, preserving both its source and
    # binding even though Settings is opened from a different local Core.
    run(binary, "plugin", "install", "--plugin-dir", str(package), "--data-dir", str(data), "--codex-bin", codex)
    (package / "keep.txt").write_text("keep")
    connected = plugin("connect")
    assert connected["root"] == str(package) and connected["dataDir"] == str(data)
    assert connected["differentData"] and connected["reloadRequired"] and connected["autoUpdate"], connected
    old_bootstrap = native()
    # Loading the MCP and HTML alone is not a WebGUI bootstrap receipt.
    assert plugin("connection-status")["reloadRequired"]
    old_bootstrap()
    assert not plugin("connection-status")["reloadRequired"]
    generation = json.loads((home / "teamcross-plugin/connection.json").read_text())["generation"]
    plugin("sync")
    assert json.loads((home / "teamcross-plugin/connection.json").read_text())["generation"] == generation
    refreshed = plugin("upgrade")  # legacy command targets the managed source
    assert refreshed["reloadRequired"] and refreshed["root"] == str(package), refreshed
    old_bootstrap()
    assert plugin("connection-status")["reloadRequired"], "old cached process acknowledged the new package"
    new_bootstrap = native()
    new_bootstrap()
    assert plugin("connection-status")["state"] == "installed"
    # External removal is respected on the next automatic check.
    run(codex, "plugin", "remove", "teamcross@teamcross-local", "--json")
    assert plugin("sync")["state"] == "disconnected"
    assert not plugin("connection-status")["installed"]
    assert plugin("connect")["installed"]
    assert not plugin("remove")["autoUpdate"]
    assert plugin("sync")["state"] == "not_enabled"
    assert (package / "keep.txt").read_text() == "keep" and data.exists()
    assert "foreign_sentinel" in config.read_text() and "foreign@sentinel" in config.read_text()
    remaining = run(codex, "plugin", "marketplace", "list", "--json")
    assert not any(m["name"] == "teamcross-local" for m in remaining["marketplaces"])
    report.update(result="passed", optInRequired=True, existingBindingPreserved=True,
        existingSourcePreserved=True, unchangedStartupDoesNotRefresh=True,
        oldCachedProcessCannotAcknowledge=True, newUIBootstrapObserved=True,
        nativeResourceMatchesBuild=True, externalRemovalRespected=True,
        legacyCommandsUseManagedSource=True, unrelatedConfigurationPreserved=True,
        dataAndPackagePreserved=True, modelTurnStarted=False)
finally:
    for client in clients:
        client.close()
    run(binary, "stop", "--data-dir", str(data), "--json")
    report["coreStopped"] = not run(binary, "status", "--data-dir", str(data), "--json").get("running")
    (output / "result.json").write_text(json.dumps(report, indent=2) + "\n")
print(json.dumps(report, indent=2))
