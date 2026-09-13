#!/usr/bin/python3
"""Proxy real Core lifecycle calls; record dummy invites without opening a browser."""
import fcntl
import json
import os
import pathlib
import subprocess
import sys
import time

args = sys.argv[1:]
entry = {"parent": os.getppid(), "command": args[0], "args": args}
if args[0] == "join":
    entry["invitation"] = sys.stdin.read()
with open(os.environ["TEAMCROSS_FIXTURE_CALLS"], "a") as log:
    fcntl.flock(log, fcntl.LOCK_EX)
    log.write(json.dumps(entry) + "\n")
if args[0] == "join":
    time.sleep(0.3)  # Another launch can arrive while the first request is busy.
    print("{}")
else:
    result = subprocess.run([os.environ["TEAMCROSS_FIXTURE_CORE"], *args], capture_output=True, text=True)
    if result.returncode:
        sys.stderr.write(result.stderr)
        sys.exit(result.returncode)
    value = json.loads(result.stdout)
    if args[0] == "serve":
        value.pop("url", None)  # No test browser tabs or custom URL handlers.
    if args[0] == "status":
        # Verify a secondary exits without entering the active-session quit path.
        value["active"] = 0 if pathlib.Path(os.environ["TEAMCROSS_FIXTURE_IDLE"]).exists() else 2
    print(json.dumps(value))
