import { Readable, Writable } from "node:stream";
import { describe, expect, it } from "vitest";

import { BridgeServer } from "../src/server.js";
import { runStdioBridge } from "../src/stdio.js";

describe("stdio bridge", () => {
  it("serves JSON-RPC and reports protocol capabilities", async () => {
    let output = "";
    const sink = new Writable({
      write(chunk, _encoding, callback) {
        output += String(chunk);
        callback();
      },
    });
    await runStdioBridge({
      input: Readable.from([
        `${JSON.stringify({ jsonrpc: "2.0", id: 0, method: "bridge.ping", params: {} })}\n`,
        `${JSON.stringify({ jsonrpc: "2.0", id: 1, method: "bridge.info", params: {} })}\n`,
      ]),
      output: sink,
      server: new BridgeServer(),
    });
    const responses = output.trim().split("\n").map((line) => JSON.parse(line) as Record<string, unknown>);
    expect(responses).toEqual(expect.arrayContaining([expect.objectContaining({
      jsonrpc: "2.0",
      id: 0,
      result: expect.objectContaining({
        ok: true,
        name: "teamcross-agent-bridge",
        protocolVersion: 1,
      }),
    }), expect.objectContaining({
      jsonrpc: "2.0",
      id: 1,
      result: expect.objectContaining({ name: "teamcross-agent-bridge", protocolVersion: 1 }),
    })]));
  });
});
