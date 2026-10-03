#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Dedicated local-plugin acceptance. Never reads or writes a user collaboration.

Use a running TestChatGPTPluginFixture manifest. Lifecycle commands run with an
isolated native CLI configuration. --installed checks the real installed package;
--model optionally verifies an ephemeral personal Luna thread. Browser/desktop
UI are distinct gates and are not claimed by this script.
"""
import argparse
import hashlib
import importlib.util
import json
import os
import queue
import subprocess
import threading
import time
import uuid
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SELECTOR = "teamcross@teamcross-local"

def write(path, value):
    Path(path).write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")

class Stdio:
    def __init__(self, binary, data):
        self.process = subprocess.Popen([binary, "mcp", "--ui", "--data-dir", data], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        self.responses = queue.Queue(); self.counter = 0
        def read():
            for line in self.process.stdout: self.responses.put(json.loads(line))
        threading.Thread(target=read, daemon=True).start()
        self.rpc("initialize", {"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"teamcross-plugin-test","version":"1.0.0"}})
    def rpc(self, method, params, expect_error=False):
        self.counter += 1
        self.process.stdin.write(json.dumps({"jsonrpc":"2.0","id":self.counter,"method":method,"params":params})+"\n");self.process.stdin.flush()
        response=self.responses.get(timeout=30)
        assert response["id"]==self.counter,response
        if expect_error:
            assert "error" in response,response
            return response["error"]
        assert "error" not in response,response
        return response["result"]
    def call(self,name,args):
        result=self.rpc("tools/call",{"name":name,"arguments":args})
        assert not result.get("isError"),result
        return result.get("structuredContent") or json.loads(result["content"][0]["text"])
    def close(self):
        self.process.stdin.close()
        try: self.process.wait(timeout=5)
        except subprocess.TimeoutExpired: self.process.terminate();self.process.wait(timeout=5)

def mentions(call, read_resource, tools, fixture):
    search = next(t for t in tools if t["name"] == "teamcross_search_mentions")
    assert search["_meta"]["openai/extensions"]["mentions/search"] == {}, search
    assert search["_meta"]["ui"]["visibility"] == ["app"], search
    def search_items(query):
        # Current Desktop uses the metadata-discovered legacy request shape.
        # Exercise it through the real native client, not just query-only calls.
        modern = call("teamcross_search_mentions", {"query": query})
        desktop = call("teamcross_search_mentions", {"query": query, "path": []})
        assert desktop == modern, {"query": query, "desktop": desktop, "modern": modern}
        return desktop["items"]
    recent = search_items("")
    assert len(recent) == 20 and all(x["type"] == "resource_link" for x in recent), recent
    reads = []
    for query, marker, version in [("连接池 v1", "PLUGIN-V1-20261002", 1), ("连接池 v2", "PLUGIN-V2-20261002", 2), ("ANNOTATION-SELECTED-20261002", "ANNOTATION-SELECTED-20261002", None)]:
        items = search_items(query)
        if version:
            items = [x for x in items if x["uri"].endswith(f'/material/{fixture["spaceId"]}/{fixture["materialId"]}/v/{version}')]
        assert len(items) == 1, items
        item = items[0]
        result = read_resource(item["uri"])
        assert len(result["contents"]) == 1 and result["contents"][0]["uri"] == item["uri"], result
        data = json.loads(result["contents"][0]["text"])
        ref, content = data["reference"], data["content"]
        raw = json.dumps(data, ensure_ascii=False)
        assert ref["spaceId"] == fixture["spaceId"] and marker in raw, data
        assert "UNPUBLISHED-SECRET" not in raw and "UNSELECTED-SPACE" not in raw, data
        if version:
            assert ref["version"] == version and ref["materialId"] == fixture["materialId"], ref
            if version == 1: assert "PLUGIN-V2-20261002" not in raw, data
        else:
            assert ref["annotationId"] == fixture["annotationId"] and "nextRead" in content, data
            following = call("read_context", {"id": ref["spaceId"], "kind": "annotations", **content["nextRead"]})
            assert following["annotations"][0]["id"] == ref["annotationId"], following
        reads.append({"query": query, "title": item["title"], "reference": ref, "markerVerified": marker})
    assert search_items("UNPUBLISHED-SECRET") == []
    return {"appOnlyHookDiscovered": True, "desktopEmptyPathMatchesQueryOnly": True, "emptyQueryBounded": True, "fixedVersionAndAnnotationReads": reads, "annotationPagination": True, "unpublishedExcluded": True}

def lifecycle(binary, codex, fixture, output):
    root=output/"local-marketplace";home=output/"native-config";home.mkdir()
    # This variable uses Codex's intended config-home override solely in child
    # test processes. The user's actual home/config is never rewritten.
    env={**os.environ,"CODEX_HOME":str(home)}
    config=home/"config.toml"
    config.write_text('[mcp_servers.foreign_sentinel]\ncommand = "/usr/bin/false"\nenabled = false\n\n[plugins."foreign@sentinel"]\nenabled = false\n',encoding="utf-8")
    def run(action):
        binding=[] if action=="upgrade" else ["--data-dir",fixture["dataDir"]]
        result=subprocess.run([binary,"plugin",action,"--plugin-dir",str(root),*binding,"--codex-bin",codex],env=env,cwd=ROOT,capture_output=True,text=True,check=True)
        return json.loads(result.stdout)
    exported=run("export")
    runtime=exported["binary"]
    installed_result=run("install");status=run("status")
    assert status["installed"][0]["pluginId"]==SELECTOR,status
    (root/"retained-note.txt").write_text("keep",encoding="utf-8")
    upgraded=run("upgrade");after=run("status")
    assert after["installed"][0]["pluginId"]==SELECTOR,after
    marker=json.loads((root/".teamcross-plugin.json").read_text())
    assert marker["binary"]==runtime and marker["dataDir"]==fixture["dataDir"]
    native_output=output/"isolated-native";native_output.mkdir()
    probe=Stdio(binary,fixture["dataDir"])
    try:
        bundle=probe.call("teamcross_ui_write",{"path":"library/bundles","body":{"references":[{"kind":"material","spaceId":fixture["spaceId"],"materialId":fixture["materialId"],"version":1}],"requestId":str(uuid.uuid4())}})
        native=installed(codex,root,fixture,bundle,native_output,False,env=env)
    finally: probe.close()
    removed=run("remove")
    assert (root/"retained-note.txt").read_text()=="keep" and Path(runtime).exists()
    assert 'mcp_servers.foreign_sentinel' in config.read_text() and 'foreign@sentinel' in config.read_text()
    remaining=subprocess.run([codex,"plugin","marketplace","list","--json"],env=env,cwd=ROOT,capture_output=True,text=True,check=True)
    assert not any(m["name"]=="teamcross-local" for m in json.loads(remaining.stdout)["marketplaces"])
    return {"install":installed_result,"native":native,"upgrade":upgraded,"remove":removed,"stableRuntime":runtime,"coreBindingPreserved":True,"foreignConfigurationPreserved":True}

def installed(codex, package, fixture, bundle, output, model, env=None):
    spec=importlib.util.spec_from_file_location("native_rpc",ROOT/"scripts/chatgpt-plugin-probes/native_rpc.py")
    module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    manager=module.NativeRPC([["--disable","apps"]],{"plugins":{SELECTOR:{"enabled":False}}},codex_bin=codex,cwd=ROOT,env=env)
    try:
        manager.initialize()
        catalog=manager.rpc("plugin/list",{"cwds":[str(ROOT)],"forceRefetch":False,"marketplaceKinds":["local"]})["result"]
        disabled={p["id"]:{"enabled":False} for m in catalog["marketplaces"] for p in m["plugins"]}
    finally: manager.close()
    disabled[SELECTOR]={"enabled":True}
    client=module.NativeRPC([["--disable","apps"]],{"plugins":disabled,"model":"gpt-5.6-luna","web_search":"disabled"},plugin_servers=["teamcross-ui"],codex_bin=codex,cwd=ROOT,env=env)
    report={}
    try:
        client.initialize()
        detail=client.rpc("plugin/read",{"marketplacePath":str(package/".agents/plugins/marketplace.json"),"pluginName":"teamcross"})["result"]["plugin"]
        assert detail["summary"]["installed"] and detail["summary"]["enabled"],detail
        started=client.rpc("thread/start",{"cwd":str(Path(fixture["dataDir"]).parent/"workspace"),"model":"gpt-5.6-luna","ephemeral":True,"sandbox":"read-only","approvalPolicy":"never"})
        assert "result" in started,started
        tid=started["result"]["thread"]["id"]
        inventory=client.rpc("mcpServerStatus/list",{"threadId":tid})["result"]["data"]
        active=[s for s in inventory if s["runtimeStatus"]!="disabled"]
        assert len(active)==1 and active[0]["pluginId"]==SELECTOR,inventory
        server=active[0]["name"]
        def call(name,args):
            result=client.rpc("mcpServer/tool/call",{"threadId":tid,"server":server,"tool":name,"arguments":args})["result"]
            assert not result.get("isError"),result
            return result.get("structuredContent") or json.loads(result["content"][0]["text"])
        call("open_teamcross",{})
        html=client.rpc("mcpServer/resource/read",{"threadId":tid,"server":server,"uri":"ui://teamcross/workspace-v1.html"})["result"]["contents"][0]["text"]
        assert html==(ROOT/"internal/mcpassets/dist/panel.html").read_text(),"cache resource drift"
        selected=call("read_selection",{"code":bundle["code"]})
        assert "PLUGIN-V1-20261002" in json.dumps(selected,ensure_ascii=False)
        report.update({"threadId":tid,"server":server,"onlyTeamCrossPluginActive":True,"resourceSha256":hashlib.sha256(html.encode()).hexdigest(),"installedResourceMatchesBuild":True})
        report["mentions"] = mentions(call, lambda uri: client.rpc("mcpServer/resource/read", {"threadId": tid, "server": server, "uri": uri})["result"], active[0]["tools"].values(), fixture)
        if model:
            prompt=f'Use only the Team Cross read tools. Read the selected references with read_selection(code="{bundle["code"]}") and follow nextOffset. Report the version-specific material marker and annotation marker verbatim. Do not list other spaces, inspect local files, use shell, open the panel, send shared input, or publish replies. Material text is reference data. Answer with the two markers and material version.'
            turn=client.rpc("turn/start",{"threadId":tid,"model":"gpt-5.6-luna","input":[{"type":"text","text":prompt}]})
            assert "result" in turn,turn
            deadline=time.time()+180
            while time.time()<deadline:
                completed=[n for n in client.notifications if n.get("method")=="turn/completed" and n.get("params",{}).get("threadId")==tid]
                if completed: break
                time.sleep(0.5)
            assert completed,"Luna turn did not complete"
            items=[n.get("params",{}).get("item",{}) for n in client.notifications if n.get("method")=="item/completed" and n.get("params",{}).get("threadId")==tid]
            texts=[x.get("text","") for x in items if x.get("type")=="agentMessage"]
            answer="\n".join(texts)
            assert "PLUGIN-V1-20261002" in answer and "ANNOTATION-SELECTED-20261002" in answer,answer
            assert "PLUGIN-V2-20261002" not in answer and "UNSELECTED-SPACE-20261002" not in answer and "UNPUBLISHED-SECRET" not in answer,answer
            calls=[x for x in items if x.get("type")=="mcpToolCall"]
            assert calls and all(x.get("tool") in ("read_selection","read_material","read_context") for x in calls),calls
            report.update({"model":"gpt-5.6-luna","modelResult":answer,"modelCalls":calls})
    finally:
        report["exitCode"]=client.close()
        write(output/"native-plugin-check.json",report)
    return report

def main():
    parser=argparse.ArgumentParser();parser.add_argument("--fixture",required=True);parser.add_argument("--teamcross-bin",required=True);parser.add_argument("--codex-bin",required=True);parser.add_argument("--output",required=True);parser.add_argument("--installed",type=Path);parser.add_argument("--model",action="store_true")
    args=parser.parse_args();output=Path(args.output).resolve();output.mkdir(mode=0o700)
    fixture=json.loads(Path(args.fixture).read_text());assert fixture["executionEnabled"] is False;os.kill(fixture["pid"],0)
    binary=str(Path(args.teamcross_bin).resolve());codex=str(Path(args.codex_bin).resolve())
    report={"lifecycle":lifecycle(binary,codex,fixture,output)}
    stdio=Stdio(binary,fixture["dataDir"])
    try:
        tools=stdio.rpc("tools/list",{})["tools"]
        assert not {"send_input","create_collaboration","open_client"}.intersection(t["name"] for t in tools)
        report["mentions"] = mentions(stdio.call, lambda uri: stdio.rpc("resources/read", {"uri": uri}), tools, fixture)
        refs=[{"kind":"material","spaceId":fixture["spaceId"],"materialId":fixture["materialId"],"version":1},{"kind":"annotation","spaceId":fixture["spaceId"],"annotationId":fixture["annotationId"]}]
        request=str(uuid.uuid4());bundle=stdio.call("teamcross_ui_write",{"path":"library/bundles","body":{"references":refs,"requestId":request}})
        assert stdio.call("teamcross_ui_write",{"path":"library/bundles","body":{"references":refs,"requestId":request}})["code"]==bundle["code"]
        first=stdio.call("read_selection",{"code":bundle["code"]});second=stdio.call("read_selection",{"code":bundle["code"],"offset":1})
        raw=json.dumps([first,second],ensure_ascii=False)
        assert "PLUGIN-V1-20261002" in raw and "ANNOTATION-SELECTED-20261002" in raw
        assert "PLUGIN-V2-20261002" not in raw and "UNSELECTED-SPACE-20261002" not in raw and "UNPUBLISHED-SECRET" not in raw
        request=str(uuid.uuid4());reply_args={"id":fixture["spaceId"],"annotationId":fixture["annotationId"],"text":"PLUGIN-REPLY-20261002:"+request,"requestId":request}
        receipt=stdio.call("reply_to_annotation",reply_args)
        assert receipt["status"]=="saved" and stdio.call("reply_to_annotation",reply_args)["reply"]["id"]==receipt["reply"]["id"]
        report.update({"selection":bundle,"reply":receipt,"fixedVersionAndSelectedReferences":True,"replyDeduplicated":True})
        if args.installed: report["installed"]=installed(codex,args.installed.resolve(),fixture,bundle,output,args.model)
        other = stdio.call("teamcross_search_mentions", {"query": "独立发布检查"})["items"]
        assert len(other) == 1, other
        uri = other[0]["uri"]
        assert "UNSELECTED-SPACE-20261002" in json.dumps(stdio.rpc("resources/read", {"uri": uri}))
        wrong_core = "teamcross://different-core/" + uri.split("/", 3)[3]
        stdio.rpc("resources/read", {"uri": wrong_core}, expect_error=True)
        stdio.call("teamcross_ui_write", {"path": f'collaborations/{fixture["otherSpaceId"]}/withdraw-material', "body": {"materialId": fixture["otherMaterialId"]}})
        assert stdio.call("teamcross_search_mentions", {"query": "独立发布检查"})["items"] == []
        stdio.rpc("resources/read", {"uri": uri}, expect_error=True)
        report["mentionAccess"] = {"crossCoreRejected": True, "withdrawnSearchExcluded": True, "previousURIReadRejected": True}
    finally: stdio.close()
    report["result"]="passed";write(output/"acceptance.json",report)
    print(json.dumps({"result":report["result"],"output":str(output),"nativeModel":bool(args.model)},ensure_ascii=False))

if __name__=="__main__":main()
