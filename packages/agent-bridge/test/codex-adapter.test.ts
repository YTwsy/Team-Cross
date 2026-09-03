import { describe, expect, it } from "vitest";

import { CodexAdapter } from "../src/adapters/codex.js";
import type { AgentAdapter } from "../src/adapters/types.js";
import type { JsonlRpcProcess } from "../src/lib/jsonl-rpc-client.js";

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
        if (method === "initialize") return { userAgent: "codex-test", codexHome: "/tmp" };
        if (method === "thread/list") return { data: [], nextCursor: null };
        if (method === "thread/start") return { thread: { id: "thread-1" } };
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

    expect(calls.map((call) => call.method)).toEqual([
      "initialize",
      "thread/start",
      "turn/start",
    ]);
    expect(calls.at(-1)?.params).toMatchObject({
      cwd: "/tmp/teamcross-worktree",
      runtimeWorkspaceRoots: ["/tmp/teamcross-worktree"],
      approvalPolicy: "never",
      sandboxPolicy: {
        type: "workspaceWrite",
        writableRoots: ["/tmp/teamcross-worktree"],
        networkAccess: false,
        excludeTmpdirEnvVar: true,
        excludeSlashTmp: true,
      },
    });
    await run.close();
    await adapter.shutdown();
  });
});
