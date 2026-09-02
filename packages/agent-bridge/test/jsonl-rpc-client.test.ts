import { describe, expect, it } from "vitest";

import { JsonlRpcProcess } from "../src/lib/jsonl-rpc-client.js";

describe("JSONL process client", () => {
  it("handles a JSON-RPC response", async () => {
    const script = [
      "const rl=require('readline').createInterface({input:process.stdin});",
      "rl.once('line', line => {",
      " const request=JSON.parse(line);",
      " process.stdout.write(JSON.stringify({jsonrpc:'2.0',id:request.id,result:{ok:true}})+'\\n');",
      "});",
    ].join("");
    const client = new JsonlRpcProcess({ command: process.execPath, args: ["-e", script] });
    client.start();
    await expect(client.request("probe", {})).resolves.toEqual({ ok: true });
    await client.close();
  });

  it("rejects pending work when the bridge subprocess crashes", async () => {
    const script = "process.stdin.once('data',()=>process.exit(17));";
    const client = new JsonlRpcProcess({ command: process.execPath, args: ["-e", script] });
    client.start();
    await expect(client.request("probe", {})).rejects.toThrow(/exited.*code 17/);
    await client.close();
  });
});
