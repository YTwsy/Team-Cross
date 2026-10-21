"""Small client for the installed, stdio app-server's documented JSON protocol."""
import copy
import json
import queue
import shutil
import subprocess
import threading
from pathlib import Path

ROOT = Path(__file__).resolve().parent
PLUGIN = ROOT / "local-probe"


def toml(value):
    if isinstance(value, dict):
        return "{" + ",".join(json.dumps(key) + "=" + toml(item) for key, item in value.items()) + "}"
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, list):
        return "[" + ",".join(toml(item) for item in value) + "]"
    return json.dumps(value)


def fixture_server():
    server = json.loads((PLUGIN / ".mcp.json").read_text())["mcpServers"]["local-probe"]
    server["cwd"] = str(PLUGIN)
    return server


class NativeRPC:
    def __init__(self, flags=None, config=None, plugin_servers=(), codex_bin=None, cwd=None, env=None):
        # -c tables merge with the user's config. Disable each existing server
        # explicitly instead of assuming an empty table replaces that config.
        codex_bin = codex_bin or shutil.which("codex")
        cwd = cwd or ROOT
        configured = subprocess.run([codex_bin, "mcp", "list", "--json"],
                                    cwd=cwd, env=env, capture_output=True, text=True, check=True)
        names = [item["name"] for item in json.loads(configured.stdout)]
        config = dict(config or {})
        selected = config.pop("mcp_servers", {})
        # Use a complete inert stdio declaration. An enabled-only override can
        # replace a transport declaration instead of preserving its command.
        servers = {name: {"command": "/usr/bin/false", "enabled": False} for name in names
                   if name not in plugin_servers}
        servers.update({name: {**settings, "enabled": True} for name, settings in selected.items()})
        config["mcp_servers"] = servers
        command = [codex_bin, "app-server", "--listen", "stdio://"]
        for flag in flags or []:
            command += flag
        for key, value in (config or {}).items():
            command += ["-c", key + "=" + toml(value)]
        self.command = command
        self.process = subprocess.Popen(command, cwd=cwd, stdin=subprocess.PIPE,
                                        stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, env=env)
        self.responses = queue.Queue()
        self.stderr = []
        self.trace = []
        self.notifications = []
        self.next_id = 0
        threading.Thread(target=self._stdout, daemon=True).start()
        threading.Thread(target=self._stderr, daemon=True).start()

    def _stdout(self):
        for line in self.process.stdout:
            try:
                message = json.loads(line)
            except ValueError:
                continue
            if "id" in message and "method" in message:
                # Test clients do not approve writes or provide credentials.
                self.process.stdin.write(json.dumps({"id": message["id"], "error": {
                    "code": -32601, "message": "Unsupported by the synthetic probe client"}}) + "\n")
                self.process.stdin.flush()
                self.notifications.append({"method": "probe/declinedServerRequest", "params": {
                    "method": message["method"]}})
            elif "id" in message:
                self.responses.put(message)
            else:
                self.notifications.append(message)
        self.responses.put({"probeProcessExited": True})

    def _stderr(self):
        for line in self.process.stderr:
            self.stderr.append(line.rstrip())

    def rpc(self, method, params, timeout=30):
        self.next_id += 1
        request = {"id": self.next_id, "method": method, "params": params}
        self.process.stdin.write(json.dumps(request) + "\n")
        self.process.stdin.flush()
        response = self.responses.get(timeout=timeout)
        if response.get("probeProcessExited"):
            self.process.wait(timeout=2)
            raise RuntimeError({"exitCode": self.process.returncode, "stderr": self.stderr})
        assert response["id"] == request["id"], response
        saved = copy.deepcopy(response)
        if method == "mcpServer/resource/read" and "result" in saved:
            for item in saved["result"].get("contents", []):
                if "text" in item:
                    item["text"] = "[HTML omitted; checked against the local file]"
        self.trace.append({"method": method, "params": params, "response": saved})
        return response

    def initialize(self):
        response = self.rpc("initialize", {
            "clientInfo": {"name": "teamcross_local_probe", "version": "0.2.0"},
            "capabilities": {"experimentalApi": True}})
        assert "result" in response, response
        self.process.stdin.write(json.dumps({"method": "initialized", "params": {}}) + "\n")
        self.process.stdin.flush()
        return response["result"]

    def close(self):
        self.process.stdin.close()
        try:
            self.process.wait(timeout=8)
        except subprocess.TimeoutExpired:
            self.process.terminate()
            try:
                self.process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=5)
        return self.process.returncode
