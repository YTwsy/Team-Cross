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
            (repo / "worker-gate-entered").write_text("fixture_wait_and_read started\n")
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
        'model = "gpt-5.6-luna"\nmodel_reasoning_effort = "medium"\napproval_policy = "on-request"\nsandbox_mode = "workspace-write"\n'
        'developer_instructions = "Append INHERITED_CONFIG_MARKER to ordinary final member replies. Do not append it to structured JSON, handoff checkpoints or HANDOFF_READY bootstrap acknowledgements."\n'
        '[mcp_servers.shadow_fixture]\ncommand = ' + json.dumps(sys.executable) + '\nargs = [' + json.dumps(str(root / "fixture-mcp.py")) + ']\ntool_timeout_sec = 240\n'
    )
    env = {key: value for key, value in os.environ.items() if not key.startswith(("CODEX_", "CLAUDE_", "ANTHROPIC_")) and key != "CLAUDECODE"}
    env.update(TEAMCROSS_SHADOW_NATIVE_DIR=str(root), TEAMCROSS_SHADOW_CODEX_BIN=str(args.codex_bin.resolve()))
    test = "TestNativeShadowResultSources" if args.result_sources else "TestNativeShadowBrief" if args.brief else "TestNativeShadowTaskSources" if args.task_sources else "TestNativeShadowTaskUnderstanding" if args.task_understanding else "TestNativeShadowTaskContexts" if args.task_contexts else "TestNativeShadowTaskQuestion" if args.task_question else "TestNativeShadowTaskFollow" if args.task_follow else "TestNativeShadowWatch" if args.watch else "TestNativeShadowToolsetUpgrade" if args.toolset_upgrade else "TestNativeShadowAcceptance"
    subprocess.run(["go", "test", "./internal/collab" if args.brief or args.result_sources else "./internal/shadow", "-run", "^" + test + "$", "-v", "-count=1", "-timeout=12m"], cwd=Path(__file__).resolve().parent.parent, env=env, check=True)
    print("Native acceptance report: " + str(root / "report.json"))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--fixture-dir", required=True, type=Path)
    parser.add_argument("--codex-bin", required=True, type=Path)
    parser.add_argument("--codex-auth", required=True, type=Path)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--result-sources", action="store_true", help="Verify exact historical shadow results and messages through authenticated Core tools")
    mode.add_argument("--brief", action="store_true", help="Verify a native shadow merges a concurrent brief update through authenticated Core transport")
    mode.add_argument("--toolset-upgrade", action="store_true", help="Verify main/worker tool registration upgrades and exact native recovery")
    mode.add_argument("--watch", action="store_true", help="Verify real watch tool calls, bounded understanding, pause and restart continuation")
    mode.add_argument("--task-follow", action="store_true", help="Verify real worker interruption, follow goal replan and exact worker recovery")
    mode.add_argument("--task-question", action="store_true", help="Verify real question tools, lost receipt reconciliation and durable task continuation")
    mode.add_argument("--task-contexts", action="store_true", help="Verify multiple linked facts, durable context waiting and exact worker continuation after restart")
    mode.add_argument("--task-sources", action="store_true", help="Verify exact source changes after mute and restart, durable understanding and fixed evidence scope")
    mode.add_argument("--task-understanding", action="store_true", help="Verify real answer reconciliation into durable facts and continuation of the same task after restart")
    run(parser.parse_args())
