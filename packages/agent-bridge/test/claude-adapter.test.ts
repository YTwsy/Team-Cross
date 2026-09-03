import { describe, expect, it, vi } from "vitest";

import { ClaudeAdapter } from "../src/adapters/claude.js";
import type { AgentAdapter } from "../src/adapters/types.js";
import { AsyncQueue } from "../src/lib/async-queue.js";
import type { BridgeEvent } from "../src/protocol.js";

type ClaudeAdapterOptions = NonNullable<ConstructorParameters<typeof ClaudeAdapter>[0]>;
type ClaudeSdkLoader = NonNullable<ClaudeAdapterOptions["sdkLoader"]>;
type ClaudeSdk = Awaited<ReturnType<ClaudeSdkLoader>>;

describe("Claude adapter policy", () => {
  it("binds native identity only on system/init and never fabricates or replaces it", async () => {
    const messages = new AsyncQueue<unknown>();
    const events: BridgeEvent[] = [];
    const sdk = {
      query() { return {
        async interrupt() {}, close() { messages.close(); },
        [Symbol.asyncIterator]() { return messages[Symbol.asyncIterator](); },
      }; },
      async listSessions() { return []; }, async getSessionMessages() { return []; },
    } as ClaudeSdk;
    const adapter = new ClaudeAdapter({ env: { ANTHROPIC_API_KEY: "test-only-key" }, sdkLoader: async () => sdk });
    const run = await adapter.createRun(createParams(), (event) => events.push(event));
    expect(run.descriptor.sessionId).toBe("");
    messages.push({ type: "assistant", session_id: "not-an-init", message: { content: [] } });
    messages.push({ type: "system", subtype: "init", session_id: "real-native-id" });
    await vi.waitFor(() => expect(run.descriptor.sessionId).toBe("real-native-id"));
    expect(JSON.stringify(events)).not.toContain("claude-pending-");
    messages.push({ type: "system", subtype: "init", session_id: "different-native-id" });
    await vi.waitFor(() => expect(run.descriptor.status).toBe("error"));
    expect(run.descriptor.sessionId).toBe("real-native-id");
    expect(events.at(-1)).toMatchObject({ type: "run.error", data: { message: expect.stringContaining("different native Session identity") } });
    await expect(adapter.shutdown()).rejects.toThrow("different native Session identity");
    await adapter.shutdown();
  });

  it("requires supported host credentials", async () => {
    const adapter = new ClaudeAdapter({
      env: { PATH: process.env.PATH },
      sdkLoader: async () => fakeSdk(() => undefined),
    });
    await expect(adapter.createRun(createParams(), () => undefined)).rejects.toThrow(
      /ANTHROPIC_API_KEY/,
    );
  });

  it("uses Streaming Input with locked-down tools and sandbox settings", async () => {
    let capturedOptions: Record<string, unknown> | undefined;
    const adapter = new ClaudeAdapter({
      env: { ...process.env, ANTHROPIC_API_KEY: "test-only-key" },
      sdkLoader: async () => fakeSdk((options) => {
        capturedOptions = options;
      }),
    });
    const run = await adapter.createRun(createParams(), () => undefined);

    expect(capturedOptions).toMatchObject({
      cwd: "/tmp/teamcross-worktree",
      permissionMode: "dontAsk",
      tools: ["Read", "Glob", "Grep", "Edit", "Write", "Bash"],
      sandbox: {
        enabled: true,
        failIfUnavailable: true,
        allowUnsandboxedCommands: false,
        network: { strictAllowlist: true, deniedDomains: ["*"] },
      },
      settings: {
        permissions: {
          defaultMode: "dontAsk",
          allow: expect.arrayContaining(["Edit(./**)", "Write(./**)"]),
        },
        sandbox: {
          filesystem: { allowWrite: ["/tmp/teamcross-worktree"] },
          credentials: {
            envVars: expect.arrayContaining([
              { name: "ANTHROPIC_API_KEY", mode: "deny" },
            ]),
          },
        },
      },
      allowedTools: expect.arrayContaining(["Edit(./**)", "Write(./**)"]),
    });
    await run.close();
    await adapter.shutdown();
  });
});

function createParams(): Parameters<AgentAdapter["createRun"]>[0] {
  return {
    runId: "claude-run",
    provider: "claude",
    worktree: "/tmp/teamcross-worktree",
    networkEnabled: false,
  };
}

function fakeSdk(capture: (options: Record<string, unknown>) => void): ClaudeSdk {
  // The adapter intentionally consumes the SDK through a narrow runtime interface;
  // keep the fake structural so API compatibility is tested at that boundary.
  let finish!: () => void;
  const finished = new Promise<void>((resolve) => {
    finish = resolve;
  });
  const query = {
    async interrupt() {},
    close() { finish(); },
    async *[Symbol.asyncIterator]() {
      await finished;
    },
  };
  return {
    query({ options }: { options: Record<string, unknown> }) {
      capture(options);
      return query;
    },
    async listSessions() { return []; },
    async getSessionMessages() { return []; },
  } as ClaudeSdk;
}
