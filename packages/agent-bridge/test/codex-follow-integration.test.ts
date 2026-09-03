import { describe, expect, it } from "vitest";
import { CodexAdapter } from "../src/adapters/codex.js";
import { CodexFollowCapabilityError } from "../src/lib/codex-follow-capability.js";
import type { JsonlRpcProcess, JsonlRpcProcessOptions } from "../src/lib/jsonl-rpc-client.js";
import { isToolMetadataMethod, syntheticToolMetadata, verifiedThreadResponse } from "./codex-permission-fixture.js";

type Input = Record<string, unknown>;
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>((yes) => { resolve = yes; }); return { promise, resolve }; }
class ReaderClient {
  calls: Array<{ method: string; input: Input }> = [];
  backwards = true;
  malformed = false;
  hook?: (method: string, input: Input) => Promise<unknown>;
  constructor(readonly options: JsonlRpcProcessOptions, readonly index: number) {}
  start() {}
  notify() {}
  respond() {}
  respondError() {}
  async close() { this.exit(); }
  exit() { this.options.onExit?.(new Error("synthetic reader exit")); }
  async request(method: string, input: Input): Promise<unknown> {
    this.calls.push({ method, input });
    return this.hook ? await this.hook(method, input) : this.value(method, input);
  }
  value(method: string, input: Input): unknown {
    if (method === "initialize") return { userAgent: `reader-${this.index}` };
    if (isToolMetadataMethod(method)) return syntheticToolMetadata(method);
    if (method === "thread/start") return verifiedThreadResponse(input, `managed-${this.index}`);
    if (method === "thread/backgroundTerminals/list") return { data: [], nextCursor: null };
    if (method === "thread/read") return { thread: { id: input.threadId, source: "cli", historyMode: "paginated", turns: [] } };
    if (method === "thread/turns/list") return {
      data: [{ id: "turn-1", status: "completed", itemsView: this.malformed ? "notLoaded" : "full", items: [{ id: "item-1", type: "agentMessage", text: "saved output" }] }],
      nextCursor: null, backwardsCursor: this.backwards ? "boundary" : null,
    };
    throw new Error(`Unexpected method ${method}`);
  }
}
function fixture(setup?: (client: ReaderClient) => void) {
  const clients: ReaderClient[] = [];
  const adapter = new CodexAdapter({ clientFactory: (options) => {
    const client = new ReaderClient(options, clients.length + 1); setup?.(client); clients.push(client);
    return client as unknown as JsonlRpcProcess;
  } });
  return { adapter, clients };
}
const source = { provider: "codex" as const, sessionId: "dedicated-native" };

describe("Codex captured-reader Follow integration", () => {
  it("enables only persisted Follow after a reversible exact-Session probe, with identical poll provenance", async () => {
    const f = fixture();
    const snapshot = await f.adapter.snapshot(source);
    expect(snapshot.capabilities).toMatchObject({ read: true, follow: true, open: false, resume: false, takeControl: false });
    expect(snapshot.capabilities.reason).toContain("not an event subscription or input permission");
    const poll = await f.adapter.poll(source);
    expect(poll.source).toEqual(snapshot.source);
    expect(poll.entries[0]?.id).toBe(snapshot.entries[0]?.id);
    expect(f.clients).toHaveLength(1);
    expect(f.clients[0]!.calls.filter(call => call.method === "thread/turns/list" && call.input.limit === 1)).toHaveLength(2);
    expect(f.clients[0]!.calls.every(call => ["initialize", "thread/read", "thread/turns/list"].includes(call.method))).toBe(true);
    await f.adapter.shutdown();
  });

  it("keeps readable snapshots but refuses Follow when reversible full-item history is unproven", async () => {
    const f = fixture(client => { client.backwards = false; });
    const snapshot = await f.adapter.snapshot(source);
    expect(snapshot.entries.some(entry => entry.text === "saved output")).toBe(true);
    expect(snapshot.capabilities.follow).toBe(false);
    await expect(f.adapter.poll(source)).rejects.toBeInstanceOf(CodexFollowCapabilityError);
    expect(f.clients[0]!.calls.every(call => ["initialize", "thread/read", "thread/turns/list"].includes(call.method))).toBe(true);
    await f.adapter.shutdown();
  });

  it("revalidates a new reader generation and never treats a malformed page as a cursor reset", async () => {
    const f = fixture();
    const first = await f.adapter.poll(source);
    f.clients[0]!.malformed = true;
    await expect(f.adapter.poll({ ...source, cursor: first.cursor })).rejects.toBeInstanceOf(CodexFollowCapabilityError);
    f.clients[0]!.exit();
    const resumed = await f.adapter.poll({ ...source, cursor: first.cursor });
    expect(f.clients).toHaveLength(2);
    expect(resumed.reset).toBe(true);
    expect(resumed.gaps.join(" ")).toContain("reader version changed");
    expect(f.clients[1]!.calls.filter(call => call.method === "thread/turns/list" && call.input.limit === 1)).toHaveLength(2);
    await f.adapter.shutdown();
  });

  it("rejects late poll output after exit without silently moving the in-flight request to the replacement client", async () => {
    const f = fixture();
    const first = await f.adapter.poll(source);
    const gate = deferred<unknown>(); const entered = deferred<void>();
    f.clients[0]!.hook = async (method, input) => {
      if (method === "thread/turns/list" && input.limit === 20) { entered.resolve(); return await gate.promise; }
      return f.clients[0]!.value(method, input);
    };
    const pending = f.adapter.poll({ ...source, cursor: first.cursor });
    await entered.promise; f.clients[0]!.exit();
    const fresh = await f.adapter.snapshot(source);
    gate.resolve(f.clients[0]!.value("thread/turns/list", {}));
    await expect(pending).rejects.toBeInstanceOf(CodexFollowCapabilityError);
    expect(fresh.capabilities.follow).toBe(true);
    expect(fresh.source.providerVersion).toBe("reader-2");
    await f.adapter.shutdown();
  });

  it("does not let independent managed client creation or exit alter reader provenance or probe cache", async () => {
    const f = fixture();
    const snapshot = await f.adapter.snapshot(source);
    const run = await f.adapter.createRun({ provider: "codex", runId: "synthetic", worktree: "/tmp/synthetic", model: "gpt-5.6-luna" }, () => {});
    await run.close();
    const poll = await f.adapter.poll(source);
    expect(poll.source).toEqual(snapshot.source);
    expect(f.clients[0]!.calls.filter(call => call.method === "thread/turns/list" && call.input.limit === 1)).toHaveLength(2);
    expect(f.clients[0]!.calls.every(call => ["initialize", "thread/read", "thread/turns/list"].includes(call.method))).toBe(true);
    await f.adapter.shutdown();
  });

  it("retires an in-flight snapshot capability probe as soon as shutdown starts", async () => {
    const gate = deferred<unknown>(); const entered = deferred<void>();
    const f = fixture(client => { client.hook = async (method, input) => {
      if (method === "thread/turns/list" && input.limit === 1) { entered.resolve(); return await gate.promise; }
      return client.value(method, input);
    }; });
    const pending = f.adapter.snapshot(source);
    await entered.promise; await f.adapter.shutdown();
    gate.resolve(f.clients[0]!.value("thread/turns/list", {}));
    await expect(pending).rejects.toBeInstanceOf(CodexFollowCapabilityError);
  });
});
