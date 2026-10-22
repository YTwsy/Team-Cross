#!/usr/bin/env python3
"""Create a dedicated Luna fixture and run native shadow continuity acceptance.

Only a new home/repository is written. Authentication is referenced by symlink;
personal sessions/configuration are never used as test state.
"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys

MCP = r'''
import json, pathlib, sys, time
repo = pathlib.Path(__file__).parent / "repo"
for line in sys.stdin:
    q = json.loads(line)
    if "id" not in q:
        continue
    method = q.get("method")
    if method == "initialize":
        result = {"protocolVersion": q["params"]["protocolVersion"], "capabilities": {"tools": {}}, "serverInfo": {"name": "shadow-fixture", "version": "1"}}
    elif method == "tools/list":
        result = {"tools": [{"name": name, "description": description, "inputSchema": {"type": "object", "properties": {}, "additionalProperties": False}} for name, description in [
            ("fixture_probe", "Return inherited MCP fixture marker. Read-only."),
            ("fixture_wait_and_read", "Wait up to 180 seconds for the fixture gate, then read fixture evidence. No arguments; only reads the isolated fixture repository.")]]}
    elif method == "tools/call" and q["params"]["name"] in ("fixture_probe", "fixture_wait_and_read"):
        text = "INHERITED_MCP_MARKER"
        if q["params"]["name"] == "fixture_wait_and_read":
            deadline = time.monotonic() + 180
            while not (repo / "allow-worker-complete").exists() and time.monotonic() < deadline:
                time.sleep(.2)
            text = (repo / "worker-evidence.txt").read_text() if (repo / "allow-worker-complete").exists() else "FIXTURE_GATE_TIMEOUT"
        result = {"content": [{"type": "text", "text": text}]}
    else:
        print(json.dumps({"jsonrpc": "2.0", "id": q["id"], "error": {"code": -32601, "message": "Unknown fixture method/tool"}}), flush=True)
        continue
    print(json.dumps({"jsonrpc": "2.0", "id": q["id"], "result": result}), flush=True)
'''


def run(args):
    root = args.fixture_dir.resolve()
    root.mkdir(mode=0o700)  # Existing fixtures are deliberately rejected.
    home = root / "codex-home"
    home.mkdir()
    (root / "repo").mkdir()
    (home / "auth.json").symlink_to(args.codex_auth.resolve())
    (root / "fixture-mcp.py").write_text(MCP)
    (root / "repo" / "worker-evidence.txt").write_text("WORKER_EVIDENCE_MARKER: isolated fixture evidence, preserved across S1 to S2.\n")
    (home / "config.toml").write_text(
        'model = "gpt-5.6-luna"\nmodel_reasoning_effort = "low"\napproval_policy = "on-request"\nsandbox_mode = "workspace-write"\n'
        'developer_instructions = "Append INHERITED_CONFIG_MARKER to ordinary final member replies. Do not append it to structured JSON, handoff checkpoints or HANDOFF_READY bootstrap acknowledgements."\n'
        '[mcp_servers.shadow_fixture]\ncommand = ' + json.dumps(sys.executable) + '\nargs = [' + json.dumps(str(root / "fixture-mcp.py")) + ']\ntool_timeout_sec = 240\n'
    )
    env = {key: value for key, value in os.environ.items() if not key.startswith(("CODEX_", "CLAUDE_", "ANTHROPIC_")) and key != "CLAUDECODE"}
    env.update(TEAMCROSS_SHADOW_NATIVE_DIR=str(root), TEAMCROSS_SHADOW_CODEX_BIN=str(args.codex_bin.resolve()))
    subprocess.run(["go", "test", "./internal/shadow", "-run", "^TestNativeShadowAcceptance$", "-v", "-count=1", "-timeout=12m"], cwd=Path(__file__).resolve().parent.parent, env=env, check=True)
    print("Native acceptance report: " + str(root / "report.json"))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--fixture-dir", required=True, type=Path)
    parser.add_argument("--codex-bin", required=True, type=Path)
    parser.add_argument("--codex-auth", required=True, type=Path)
    run(parser.parse_args())
