import { describe, expect, it } from "vitest";

import { CodexAdapter } from "../src/adapters/codex.js";
import type { AgentAdapter } from "../src/adapters/types.js";
import type { JsonlRpcProcess } from "../src/lib/jsonl-rpc-client.js";
import { isToolMetadataMethod, syntheticToolMetadata, verifiedThreadResponse } from "./codex-permission-fixture.js";

describe("Codex adapter requests", () => {
  it("initializes without listing history and constrains each turn to the worktree", async () => {
    const calls: Array<{ method: string; params: unknown }> = [];
    let callbacks!: ConstructorParameters<typeof JsonlRpcProcess>[0];
    const fake = {
      start() {},
      notify() {},
      respond() {},
      respondError() {},
      async close() {},
      async request(method: string, params: unknown) {
        calls.push({ method, params });
        if (isToolMetadataMethod(method)) return syntheticToolMetadata(method);
        if (method === "initialize") return { userAgent: "codex-test", codexHome: "/tmp" };
        if (method === "thread/list") return { data: [], nextCursor: null };
        if (method === "thread/backgroundTerminals/list") return { data: [], nextCursor: null };
        if (method === "thread/start") return verifiedThreadResponse(params);
        if (method === "turn/start") return { turn: { id: "turn-1" } };
        if (method === "turn/interrupt") {
          callbacks.onNotification?.({ method: "turn/completed", params: { threadId: "thread-1", turn: { id: "turn-1", status: "interrupted" } } });
          return {};
        }
        throw new Error(`unexpected method ${method}`);
      },
    };
    const adapter = new CodexAdapter({
      clientFactory: (options) => { callbacks = options; return fake as unknown as JsonlRpcProcess; },
    });
    const params: Parameters<AgentAdapter["createRun"]>[0] = {
      runId: "run-1",
      provider: "codex",
      worktree: "/tmp/teamcross-worktree",
      networkEnabled: false,
    };
    const run = await adapter.createRun(params, () => undefined);
    await run.send("fix the test");

    expect(calls.filter((call) => !isToolMetadataMethod(call.method)).map((call) => call.method)).toEqual([
      "initialize",
      "thread/start",
      "turn/start",
    ]);
    expect(calls.at(-1)?.params).toMatchObject({
      cwd: "/tmp/teamcross-worktree",
      runtimeWorkspaceRoots: ["/tmp/teamcross-worktree"],
      approvalPolicy: "never",
      environments: [{ environmentId: "local", cwd: params.worktree, runtimeWorkspaceRoots: [params.worktree] }],
      permissions: expect.stringMatching(/^teamcross-[a-f0-9-]+-offline$/),
    });
    expect(calls.at(-1)?.params).not.toHaveProperty("sandboxPolicy");
    expect(calls.find((call) => call.method === "thread/start")?.params).not.toHaveProperty("sandbox");
    expect(callbacks.args[3]).toMatch(/extends=":workspace".*filesystem=.*":tmpdir"="read".*":slash_tmp"="read".*network=\{enabled=false\}/);
    expect(callbacks.cwd).toBe(params.worktree);
    await run.close();
    await adapter.shutdown();
  });
});
