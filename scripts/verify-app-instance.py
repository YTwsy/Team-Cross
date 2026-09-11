#!/usr/bin/env python3
"""Exercise two real menu bar App copies with isolated Core and native URL events.

Requires a macOS GUI login session. Invites are dummy strings intercepted by a
fixture helper; Core status/start/stop use the packaged binary, without Codex.
"""
import argparse
import concurrent.futures
import json
import os
import pathlib
import plistlib
import shutil
import signal
import subprocess
import tempfile
import time
import uuid

ROOT = pathlib.Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument("app")
args = parser.parse_args()
original = pathlib.Path(args.app).resolve()


def run(*command, **kwargs):
    return subprocess.run(command, check=True, text=True, capture_output=True, timeout=30, **kwargs)


def wait_for(predicate, description, timeout=15):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        value = predicate()
        if value:
            return value
        time.sleep(0.1)
    raise AssertionError(description)


with tempfile.TemporaryDirectory(prefix="teamcross-app-instance-") as temp:
    root = pathlib.Path(temp).resolve()
    helper = original / "Contents/Resources/teamcross"
    driver = root / "driver"
    run("xcrun", "swiftc", "-module-cache-path", str(ROOT / "bin/swift-cache"),
        str(ROOT / "scripts/fixtures/app-instance-driver.swift"), "-o", str(driver))
    probe = root / "probe"
    run("xcrun", "swiftc", "-module-cache-path", str(ROOT / "bin/swift-cache"),
        str(ROOT / "apps/macos/AppInstance.swift"), str(ROOT / "scripts/fixtures/app-instance-probe.swift"), "-o", str(probe))
    apps = []
    fixture_id = "io.github.ytwsy.teamcross.fixture." + uuid.uuid4().hex
    for name in ["First copy", "Second copy"]:
        app = root / name / "Team Cross.app"
        shutil.copytree(original, app)
        info = plistlib.loads((app / "Contents/Info.plist").read_bytes())
        info["CFBundleIdentifier"] = fixture_id
        # Explicit launch events exercise teamcross:// without registering the
        # temporary copies as handlers for the user's real invitation links.
        info.pop("CFBundleURLTypes", None)
        (app / "Contents/Info.plist").write_bytes(plistlib.dumps(info))
        shutil.copy2(ROOT / "scripts/fixtures/app-instance-helper.py", app / "Contents/Resources/teamcross")
        (app / "Contents/Resources/teamcross").chmod(0o755)
        run("codesign", "--force", "--sign", "-", str(app))
        apps.append(app)
    data = root / "data with spaces"
    data.mkdir()
    alias = root / "data alias"
    alias.symlink_to(data, target_is_directory=True)
    calls = root / "calls.jsonl"
    calls.touch()
    idle = root / "allow-explicit-quit"
    env = os.environ.copy()
    env.update(TEAMCROSS_FIXTURE_CORE=str(helper), TEAMCROSS_FIXTURE_CALLS=str(calls),
               TEAMCROSS_FIXTURE_IDLE=str(idle))
    processes = {}
    data_directories = {data}

    def events(command):
        return [event for line in calls.read_text().splitlines()
                if (event := json.loads(line))["command"] == command]

    def alive(pid):
        result = subprocess.run(["ps", "-p", str(pid), "-o", "stat=,command="], capture_output=True, text=True)
        fields = result.stdout.strip().split(maxsplit=1)
        return bool(fields and not fields[0].startswith("Z") and str(root) in fields[-1])

    def status(directory=data):
        return json.loads(run(str(helper), "status", "--json", "--data-dir", str(directory)).stdout)

    def launch(index, directory=data, urls=(), reopen=False):
        launch_env = dict(env, TEAMCROSS_DATA_DIR=str(directory))
        pid = int(run(str(driver), "reopen" if reopen else "launch", str(apps[index]), *urls, env=launch_env).stdout)
        processes[pid] = apps[index]
        return pid

    def only_owner(pids):
        living = [pid for pid in pids if alive(pid)]
        return living[0] if len(living) == 1 else None

    try:
        initial = json.loads(run(str(helper), "serve", "--no-open", "--json", "--data-dir", str(data)).stdout)["service"]
        (data / "preserved.txt").write_text("keep")
        with concurrent.futures.ThreadPoolExecutor(max_workers=5) as pool:
            pids = list(pool.map(lambda i: launch(i % 2, alias if i % 2 else data), range(5)))
        owner = wait_for(lambda: only_owner(pids), "concurrent copies did not converge to one App")
        wait_for(lambda: len(events("serve")) >= 2, "Home request was not forwarded")
        assert {event["parent"] for event in events("serve")} == {owner}
        assert status()["pid"] == initial["pid"] and not events("stop")

        # Two URL batches reach the original process while its helper is busy.
        invites = [f"teamcross://join?invite=fixture-{uuid.uuid4().hex}" for _ in range(3)]
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            secondaries = list(pool.map(lambda urls: launch(1, alias, urls), [invites[:2], invites[2:]]))
        wait_for(lambda: all(not alive(pid) for pid in secondaries), "invitation App copies stayed running")
        wait_for(lambda: len(events("join")) == 3, "invitations were lost or duplicated")
        assert sorted(event["invitation"] for event in events("join")) == sorted(invites)
        assert {event["parent"] for event in events("join")} == {owner}
        assert status()["pid"] == initial["pid"] and not events("stop")

        # A repeated receipt request must not replay the invitation. Invalid
        # routes and oversized batches cannot enter the primary's queue.
        request_id = str(uuid.uuid4())
        invitation = "teamcross://join?invite=fixture-receipt"
        for _ in range(2):
            assert run(str(probe), str(alias), request_id, invitation).stdout.strip() == "true"
        wait_for(lambda: len(events("join")) == 4, "receipt retry replayed or lost an invitation")
        assert run(str(probe), str(data), str(uuid.uuid4()), "https://example.invalid").stdout.strip() == "false"
        assert run(str(probe), str(data), str(uuid.uuid4()), *([invitation] * 33)).stdout.strip() == "false"

        # An unresponsive owner still holds the shell lock. When it recovers,
        # retries of the same Home request are acknowledged only once.
        before = len(events("serve"))
        os.kill(owner, signal.SIGSTOP)
        try:
            waiting = launch(1, alias)
            time.sleep(2.5)
            assert alive(owner) and alive(waiting)
            assert len(events("serve")) == before and not events("stop")
        finally:
            os.kill(owner, signal.SIGCONT)
        wait_for(lambda: not alive(waiting), "secondary did not finish after owner recovered")
        wait_for(lambda: len(events("serve")) == before + 1, "Home receipt retry was lost or replayed")

        before = len(events("serve"))
        index = apps.index(processes[owner])
        assert launch(index, reopen=True) == owner
        wait_for(lambda: len(events("serve")) > before, "reopening the existing App did not open Home")

        # A deliberately separate data directory must keep its own menu/Core.
        independent = root / "independent-data"
        data_directories.add(independent)
        other = launch(1, independent)
        wait_for(lambda: status(independent).get("running"), "independent App failed to start")
        assert alive(owner) and alive(other) and status(independent)["pid"] != initial["pid"]

        # Kill only the verified fixture shell. Its Core and saved data survive;
        # the lock/IPC are reclaimed by a new copy without deleting lock files.
        assert alive(owner)
        os.kill(owner, signal.SIGKILL)
        wait_for(lambda: not alive(owner), "fixture owner did not exit")
        replacement = launch(1, alias)
        wait_for(lambda: any(event["parent"] == replacement for event in events("serve")), "App lock was not recovered")
        assert status()["pid"] == initial["pid"] and not events("stop")
        assert (data / "preserved.txt").read_text() == "keep"

        # Explicitly quitting a primary still stops its own Core normally.
        idle.touch()
        for pid in [replacement, other]:
            run(str(driver), "quit", str(pid))
            wait_for(lambda: not alive(pid), "explicit primary quit failed")
        assert not status().get("running") and not status(independent).get("running")
        assert len(events("stop")) == 2
        print(json.dumps({"concurrentAppCopies": True, "canonicalDataDirectory": True,
                          "homeForwarded": True, "invitationBatchesForwardedOnce": True,
                          "receiptRetryDeduplicated": True, "invalidRequestsRejected": True,
                          "unresponsiveOwnerRecovery": True,
                          "reopenExistingApp": True, "independentDataDirectories": True,
                          "secondaryExitPreservesCore": True, "shellCrashRecovery": True,
                          "explicitQuitStopsCore": True, "dataPreserved": True,
                          "realCodexOrLAN": False}))
    finally:
        # Every PID was returned for an explicit fixture path, and is checked
        # again before cleanup. Never terminate applications by name.
        for pid in processes:
            if alive(pid):
                os.kill(pid, signal.SIGKILL)
        for directory in data_directories:
            subprocess.run([str(helper), "stop", "--force", "--data-dir", str(directory)],
                           capture_output=True, timeout=30)
