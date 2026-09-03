import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { CodexAdapter } from "../src/adapters/codex.js";
import { ClaudeAdapter } from "../src/adapters/claude.js";
import type { AgentRun } from "../src/adapters/types.js";
import { AsyncQueue } from "../src/lib/async-queue.js";
import type { JsonlRpcProcess } from "../src/lib/jsonl-rpc-client.js";
import type { BridgeEvent } from "../src/protocol.js";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
const params = { runId: "run-close", worktree: "/tmp/synthetic-worktree", networkEnabled: false };

async function codexFixture() {
  let callbacks!: ConstructorParameters<typeof JsonlRpcProcess>[0];
  const calls: Array<{ method: string; params: unknown }> = [];
  const responses: Array<{ id: string | number; result?: unknown; error?: string }> = [];
  const events: BridgeEvent[] = [];
  const behavior = {
    start: async (): Promise<unknown> => ({ turn: { id: "turn-1" } }),
    interrupt: async (_params: unknown): Promise<unknown> => ({}),
    steer: async (): Promise<unknown> => ({}),
    respondError: (_id: string | number) => {},
  };
  const adapter = new CodexAdapter({ closeTimeoutMs: 50, clientFactory: (options) => {
    callbacks = options;
    return {
      start() {}, notify() {}, async close() {},
      respond(id: string | number, result: unknown) { responses.push({ id, result }); },
      respondError(id: string | number, _code: number, error: string) {
        behavior.respondError(id); responses.push({ id, error });
      },
      async request(method: string, input: unknown) {
        calls.push({ method, params: input });
        if (method === "initialize") return { userAgent: "synthetic-codex" };
        if (method === "thread/start") return { thread: { id: "thread-1" } };
        if (method === "turn/start") return await behavior.start();
        if (method === "turn/interrupt") return await behavior.interrupt(input);
        if (method === "turn/steer") return await behavior.steer();
        throw new Error(`unexpected synthetic method ${method}`);
      },
    } as unknown as JsonlRpcProcess;
  } });
  const run = await adapter.createRun({ ...params, provider: "codex" }, (event) => events.push(event));
  const notification = (method: string, turnId: string, status?: string, threadId = "thread-1") => {
    callbacks.onNotification?.({ method, params: { threadId, turn: { id: turnId, status } } });
  };
  const completed = (turnId = "turn-1", status = "interrupted", threadId = "thread-1") => notification("turn/completed", turnId, status, threadId);
  const input = (id: string | number, method = "item/tool/requestUserInput") => callbacks.onRequest?.({
    id, method, params: { threadId: "thread-1", turnId: "turn-1", questions: [{ id: "question-1" }] },
  });
  return { adapter, run, calls, responses, events, behavior, notification, completed, input };
}

type ClaudeSdk = Awaited<ReturnType<NonNullable<NonNullable<ConstructorParameters<typeof ClaudeAdapter>[0]>["sdkLoader"]>>>;
async function claudeFixture() {
  const messages = new AsyncQueue<unknown>();
  const events: BridgeEvent[] = [];
  const calls: string[] = [];
  const behavior = {
    close: () => {}, interrupt: async () => {}, drain: async () => {},
    cleanup: async (): Promise<IteratorResult<unknown>> => ({ done: true, value: undefined }),
  };
  const query = {
    async interrupt() { calls.push("interrupt"); await behavior.interrupt(); },
    close() { calls.push("close"); behavior.close(); },
    async return() { calls.push("return"); return await behavior.cleanup(); },
    async *[Symbol.asyncIterator]() {
      for await (const message of messages) {
        if (message instanceof Error) throw message;
        yield message;
      }
      await behavior.drain();
    },
  };
  const sdk: ClaudeSdk = {
    query() { return query; },
    async listSessions() { throw new Error("must not enumerate history"); },
    async getSessionMessages() { throw new Error("must not read history"); },
  };
  const adapter = new ClaudeAdapter({ closeTimeoutMs: 50, env: { ANTHROPIC_API_KEY: "synthetic-only" }, sdkLoader: async () => sdk });
  const run = await adapter.createRun({ ...params, provider: "claude" }, (event) => events.push(event));
  return { adapter, run, messages, calls, events, behavior };
}

function expectUnclosed(run: AgentRun, events: BridgeEvent[]) {
  expect(run.descriptor.status).not.toBe("closed");
  expect(events.filter((event) => event.type === "run.closed")).toHaveLength(0);
  expect(run.descriptor.capabilities).toEqual({ send: false, steer: false, interrupt: false, inputResponse: false });
}
async function expectFenced(run: AgentRun) {
  await expect(run.send("must not execute")).rejects.toThrow(/fenced/);
  await expect(run.steer("must not execute")).rejects.toThrow(/fenced/);
  await expect(run.interrupt()).rejects.toThrow(/fenced/);
  await expect(run.importContext({}, "must not queue")).rejects.toThrow(/fenced/);
}
beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe("Codex managed close confirmation", () => {
  it("does not ACK interruption-only close; fences pending/late input and keeps routing for retry", async () => {
    const f = await codexFixture();
    await f.run.send("synthetic"); f.input("pending");
    const requestId = f.events.find((event) => event.type === "input.requested")!.inputRequestId!;
    const first = f.run.close(); const concurrent = f.run.close();
    const failures = Promise.all([expect(first).rejects.toThrow(/terminal turn\/completed/), expect(concurrent).rejects.toThrow(/terminal turn\/completed/)]);
    await expectFenced(f.run);
    await expect(f.run.respondInput(requestId, "must not execute")).rejects.toThrow(/fenced/);
    f.input("late"); f.input("approval", "item/commandExecution/requestApproval");
    expect(f.events.filter((event) => event.type === "input.requested")).toHaveLength(1);
    expect(f.responses).toEqual([
      { id: "pending", error: "Team Cross run is closing" },
      { id: "late", error: "Team Cross run is closing" },
      { id: "approval", result: { decision: "decline" } },
    ]);
    await vi.advanceTimersByTimeAsync(51); await failures;
    expectUnclosed(f.run, f.events);
    expect(f.calls.filter((call) => call.method === "turn/interrupt")).toHaveLength(1);
    f.completed(); await vi.advanceTimersByTimeAsync(0);
    expectUnclosed(f.run, f.events); // A timed-out close must not emit late success.
    await f.run.close(); await f.adapter.shutdown();
    expect(f.events.filter((event) => event.type === "run.closed")).toHaveLength(1);
  });

  it.each(["completed", "interrupted", "failed"])("accepts exact %s before interrupt ACK, but still awaits that ACK", async (status) => {
    const f = await codexFixture(); await f.run.send("synthetic");
    const ack = deferred<unknown>();
    f.behavior.interrupt = async () => { f.completed("turn-1", status); return await ack.promise; };
    const closing = f.run.close(); await vi.advanceTimersByTimeAsync(0);
    expectUnclosed(f.run, f.events);
    ack.resolve({}); await closing;
    expect(f.run.descriptor.status).toBe("closed");
  });

  it("ignores other thread/turn and nonterminal completed notifications", async () => {
    const f = await codexFixture(); await f.run.send("synthetic");
    const failure = expect(f.run.close()).rejects.toThrow(/terminal turn\/completed/);
    f.completed("turn-1", "interrupted", "different-thread");
    f.completed("different-turn"); f.completed("turn-1", "inProgress");
    expect(f.run.descriptor.status).toBe("running");
    await vi.advanceTimersByTimeAsync(51); await failure;
    f.behavior.interrupt = async () => { f.completed(); return {}; };
    await f.run.close();
    expect(f.calls.filter((call) => call.method === "turn/interrupt")).toHaveLength(2);
  });

  it("does not swallow an interrupt ACK failure even when exact terminal arrived first", async () => {
    const f = await codexFixture(); await f.run.send("synthetic");
    f.behavior.interrupt = async () => { f.completed(); throw new Error("lost interrupt ACK"); };
    await expect(f.run.close()).rejects.toThrow("lost interrupt ACK");
    expectUnclosed(f.run, f.events);
    await f.run.close();
    expect(f.calls.filter((call) => call.method === "turn/interrupt")).toHaveLength(1);
    expect(f.events.filter((event) => event.type === "run.closed")).toHaveLength(1);
  });

  it("bounds a missing interrupt ACK and requires explicit retry after a late ACK/terminal", async () => {
    const f = await codexFixture(); await f.run.send("synthetic");
    const ack = deferred<unknown>(); f.behavior.interrupt = () => ack.promise;
    const failure = expect(f.run.close()).rejects.toThrow(/interrupt ACK/);
    await vi.advanceTimersByTimeAsync(51); await failure;
    f.completed(); ack.resolve({}); await vi.advanceTimersByTimeAsync(0);
    expectUnclosed(f.run, f.events); await expectFenced(f.run);
    await f.run.close();
    expect(f.events.filter((event) => event.type === "run.closed")).toHaveLength(1);
  });

  it("propagates interrupt failure to concurrent callers, then retries with retained routing", async () => {
    const f = await codexFixture(); await f.run.send("synthetic");
    f.behavior.interrupt = async () => { throw new Error("synthetic interrupt failed"); };
    const first = f.run.close(); const second = f.run.close();
    await Promise.all([expect(first).rejects.toThrow("synthetic interrupt failed"), expect(second).rejects.toThrow("synthetic interrupt failed")]);
    expectUnclosed(f.run, f.events);
    expect(f.calls.filter((call) => call.method === "turn/interrupt")).toHaveLength(1);
    f.behavior.interrupt = async () => { f.completed(); return {}; };
    await f.adapter.shutdown();
    expect(f.events.filter((event) => event.type === "run.closed")).toHaveLength(1);
  });

  it("does not forget another started turn while closing the first", async () => {
    const f = await codexFixture(); await f.run.send("synthetic");
    f.behavior.interrupt = async (input) => {
      if ((input as { turnId: string }).turnId === "turn-1") { f.notification("turn/started", "turn-2"); f.completed(); }
      return {};
    };
    const closing = f.run.close(); await vi.advanceTimersByTimeAsync(0);
    expectUnclosed(f.run, f.events);
    expect(f.calls.filter((call) => call.method === "turn/interrupt").map((call) => call.params)).toEqual([
      { threadId: "thread-1", turnId: "turn-1" }, { threadId: "thread-1", turnId: "turn-2" },
    ]);
    f.completed("turn-2"); await closing;
  });

  it("awaits in-flight turn/start identity before closing, and rejects duplicate sends", async () => {
    const f = await codexFixture(); const start = deferred<unknown>();
    f.behavior.start = () => start.promise;
    const sending = f.run.send("synthetic");
    await expect(f.run.send("duplicate")).rejects.toThrow(/unconfirmed/);
    const closing = f.run.close();
    expect(f.calls.filter((call) => call.method === "turn/interrupt")).toHaveLength(0);
    expectUnclosed(f.run, f.events);
    f.behavior.interrupt = async () => { f.completed("turn-late"); return {}; };
    start.resolve({ turn: { id: "turn-late" } }); await sending; await closing;
    expect(f.calls.at(-1)).toEqual({ method: "turn/interrupt", params: { threadId: "thread-1", turnId: "turn-late" } });
  });

  it("does not resurrect completion before start response or on a late duplicate start", async () => {
    const f = await codexFixture();
    f.behavior.start = async () => { f.completed(); return { turn: { id: "turn-1" } }; };
    await f.run.send("synthetic"); f.notification("turn/started", "turn-1");
    await f.run.close();
    expect(f.calls.filter((call) => call.method === "turn/interrupt")).toHaveLength(0);
  });

  it("keeps late start replies fenced after timeout; retry stops the newly identified turn", async () => {
    const f = await codexFixture(); const start = deferred<unknown>();
    f.behavior.start = () => start.promise;
    const sending = f.run.send("synthetic");
    const failure = expect(f.run.close()).rejects.toThrow(/turn\/start identity/);
    await vi.advanceTimersByTimeAsync(51); await failure;
    start.resolve({ turn: { id: "turn-1" } }); await sending;
    await expectFenced(f.run); expectUnclosed(f.run, f.events);
    f.behavior.interrupt = async () => { f.completed(); return {}; };
    await f.run.close();
  });

  it("never ACKs a failed turn/start with unknown acceptance/identity as idle and closed", async () => {
    const f = await codexFixture();
    f.behavior.start = async () => { throw new Error("lost start response"); };
    await expect(f.run.send("synthetic")).rejects.toThrow("lost start response");
    await expect(f.run.close()).rejects.toThrow(/outcome is unknown/);
    await expect(f.run.close()).rejects.toThrow(/outcome is unknown/);
    expectUnclosed(f.run, f.events); await expectFenced(f.run);
  });

  it("awaits pre-fence steer and retains pending input cancellation after RPC write failure", async () => {
    const f = await codexFixture(); await f.run.send("synthetic");
    const ack = deferred<unknown>(); f.behavior.steer = () => ack.promise;
    const steering = f.run.steer("synthetic"); f.input("pending");
    f.behavior.respondError = () => { throw new Error("stdin unavailable"); };
    await expect(f.run.close()).rejects.toThrow("stdin unavailable"); expectUnclosed(f.run, f.events);
    f.behavior.respondError = () => {};
    f.behavior.interrupt = async () => { f.completed(); return {}; };
    const closing = f.run.close(); await vi.advanceTimersByTimeAsync(0);
    expectUnclosed(f.run, f.events);
    expect(f.calls.filter((call) => call.method === "turn/interrupt")).toHaveLength(0);
    ack.resolve({}); await steering; await closing;
    expect(f.responses).toEqual([{ id: "pending", error: "Team Cross run is closing" }]);
  });
});

describe("Claude managed close confirmation", () => {
  it("keeps SDK close throw retryable despite late init, and never substitutes interrupt ACK", async () => {
    const f = await claudeFixture(); await f.run.send("synthetic");
    f.behavior.close = () => { throw new Error("synthetic SDK close failed"); };
    await expect(f.run.close()).rejects.toThrow("synthetic SDK close failed");
    f.messages.push({ type: "system", subtype: "init", session_id: "synthetic-native-id" });
    await vi.advanceTimersByTimeAsync(0); expectUnclosed(f.run, f.events); await expectFenced(f.run);
    expect(f.calls).toEqual(["close"]);
    f.behavior.close = () => f.messages.close(); await f.adapter.shutdown();
    expect(f.calls).toEqual(["close", "close", "return"]);
    expect(f.events.filter((event) => event.type === "run.closed")).toHaveLength(1);
    expect(f.events.filter((event) => event.type === "turn.completed")).toHaveLength(0);
  });

  it("requires receive loop termination and never emits delayed success after timeout", async () => {
    const f = await claudeFixture();
    const failure = expect(f.run.close()).rejects.toThrow(/receive loop termination/);
    await vi.advanceTimersByTimeAsync(51); await failure;
    expectUnclosed(f.run, f.events); await expectFenced(f.run);
    f.messages.close(); await vi.advanceTimersByTimeAsync(0); expectUnclosed(f.run, f.events);
    await f.run.close(); expect(f.calls).toEqual(["close", "close", "return"]);
    expect(f.events.filter((event) => event.type === "run.closed")).toHaveLength(1);
  });

  it("coalesces concurrent close while the receive loop is draining", async () => {
    const f = await claudeFixture(); const drain = deferred<void>();
    f.behavior.drain = () => drain.promise; f.behavior.close = () => f.messages.close();
    const first = f.run.close(); const second = f.run.close();
    await vi.advanceTimersByTimeAsync(0); expectUnclosed(f.run, f.events);
    expect(f.calls).toEqual(["close"]);
    drain.resolve(); await Promise.all([first, second]);
    expect(f.calls).toEqual(["close", "return"]);
    expect(f.events.filter((event) => event.type === "run.closed")).toHaveLength(1);
  });

  it("surfaces receive failure, then requires explicit close/cleanup retry before ACK", async () => {
    const f = await claudeFixture();
    f.behavior.close = () => f.messages.push(new Error("synthetic receive failure"));
    await expect(f.run.close()).rejects.toThrow("synthetic receive failure"); expectUnclosed(f.run, f.events);
    expect(f.events.at(-1)).toMatchObject({ type: "run.error", data: { message: "synthetic receive failure" } });
    f.behavior.close = () => f.messages.close(); await f.run.close();
    expect(f.calls).toEqual(["close", "close", "return"]);
    expect(f.events.filter((event) => event.type === "run.closed")).toHaveLength(1);
  });

  it("rejects cleanup timeout/failure/incomplete result even after receive completion", async () => {
    const f = await claudeFixture(); const cleanup = deferred<IteratorResult<unknown>>();
    f.behavior.close = () => f.messages.close(); f.behavior.cleanup = () => cleanup.promise;
    const timeout = expect(f.run.close()).rejects.toThrow(/query cleanup/);
    await vi.advanceTimersByTimeAsync(51); await timeout;
    cleanup.resolve({ done: true, value: undefined }); await vi.advanceTimersByTimeAsync(0); expectUnclosed(f.run, f.events);
    f.behavior.cleanup = async () => { throw new Error("cleanup failed"); };
    await expect(f.run.close()).rejects.toThrow("cleanup failed"); expectUnclosed(f.run, f.events);
    f.behavior.cleanup = async () => ({ done: false, value: undefined });
    await expect(f.run.close()).rejects.toThrow(/did not finish/);
    f.behavior.cleanup = async () => ({ done: true, value: undefined }); await f.run.close();
    expect(f.events.filter((event) => event.type === "run.closed")).toHaveLength(1);
  });

  it("does not resurrect closed status on late pre-fence interrupt ACK", async () => {
    const f = await claudeFixture(); const ack = deferred<void>();
    f.behavior.interrupt = () => ack.promise; await f.run.send("synthetic");
    const interruption = f.run.interrupt(); f.behavior.close = () => f.messages.close();
    await f.run.close(); ack.resolve(); await interruption;
    expect(f.run.descriptor.status).toBe("closed"); expect(f.events.at(-1)?.type).toBe("run.closed");
  });
});
