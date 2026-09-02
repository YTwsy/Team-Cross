import { describe, expect, it } from "vitest";

import { CodexAdapter } from "../src/adapters/codex.js";
import { MAX_POLL_CURSOR_BYTES, pollCodexSession } from "../src/lib/codex-poll.js";
import type { JsonlRpcProcess } from "../src/lib/jsonl-rpc-client.js";
import { MAX_REVIEW_RESULT_BYTES, reviewCapabilities } from "../src/lib/session-snapshot.js";
import type { SessionPollResult } from "../src/protocol.js";
import { BridgeServer } from "../src/server.js";

type RecordValue = Record<string, unknown>;
function turn(index: number, text = `message ${index}`): RecordValue {
  return { id: `turn-${index}`, status: "completed", itemsView: "full", items: [{ type: "agentMessage", id: `item-${index}`, text }] };
}

function fixture(initial: RecordValue[] = []) {
  const state = { turns: initial, calls: [] as Array<{ method: string; params: RecordValue }>, afterHead: undefined as (() => void) | undefined, invalidCursor: false };
  const request = async (method: string, params: RecordValue): Promise<unknown> => {
    state.calls.push({ method, params });
    if (method === "initialize") return { userAgent: "codex-test" };
    if (method === "thread/read") {
      expect(params).toEqual({ threadId: "native", includeTurns: false });
      return { thread: { id: "native", sessionId: "family", source: "cli", historyMode: "paginated", turns: [] } };
    }
    if (method !== "thread/turns/list") throw new Error(`Forbidden method: ${method}`);
    expect(params).toMatchObject({ threadId: "native", itemsView: "full" });
    if (params.sortDirection === "desc") {
      const data = structuredClone([...state.turns].reverse().slice(0, params.limit as number));
      const result = { data, nextCursor: state.turns.length > data.length ? "older" : null, backwardsCursor: data.length ? `anchor:${String(data[0]?.id)}` : null };
      state.afterHead?.();
      state.afterHead = undefined;
      return result;
    }
    if (state.invalidCursor) throw new Error("invalid native cursor");
    const [kind, id] = String(params.cursor).split(":");
    const at = state.turns.findIndex((value) => value.id === id);
    if (at < 0) throw new Error("missing anchor");
    const start = at + (kind === "after" ? 1 : 0);
    const data = structuredClone(state.turns.slice(start, start + (params.limit as number)));
    return { data, nextCursor: start + data.length < state.turns.length ? `after:${String(data.at(-1)?.id)}` : null, backwardsCursor: `reverse:${id}` };
  };
  const adapter = () => new CodexAdapter({ clientFactory: () => ({
    start() {}, notify() {}, async close() {}, request,
  }) as unknown as JsonlRpcProcess });
  return { state, request, adapter };
}

describe("Codex read-only polling", () => {
  it("starts at newest history then returns changed boundary entries and new items only", async () => {
    const { state, adapter } = fixture(Array.from({ length: 40 }, (_, index) => turn(index)));
    const bridge = new BridgeServer({ adapters: { codex: adapter() } });
    const first = await bridge.handle("sessions.poll", { provider: "codex", sessionId: "native" }) as SessionPollResult;
    expect(first.reset).toBe(true);
    expect(first.entries[0]?.turnId).toBe("turn-20");
    expect(first.entries.at(-1)?.turnId).toBe("turn-39");
    expect(first.warnings.join(" ")).toContain("older persisted history");
    const original = first.entries.at(-1)!;
    state.turns[39] = turn(39, "updated output");
    state.turns.push(turn(40));
    const second = await bridge.handle("sessions.poll", { provider: "codex", sessionId: "native", cursor: first.cursor }) as SessionPollResult;
    expect(second.reset).toBe(false);
    expect(second.gaps).toEqual([]);
    expect(second.entries).toHaveLength(2);
    expect(second.entries[0]).toMatchObject({ id: original.id, text: "updated output" });
    expect(second.entries[1]?.turnId).toBe("turn-40");
    const third = await bridge.handle("sessions.poll", { provider: "codex", sessionId: "native", cursor: second.cursor }) as SessionPollResult;
    expect(third.entries).toEqual([]);
    expect(third.reset).toBe(false);
    expect(state.calls.some((call) => /resume|subscribe|start$|thread\/list|fork/.test(call.method))).toBe(false);
    expect(reviewCapabilities("codex").follow).toBe(false);
    const info = await bridge.handle("bridge.info", {}) as { methods: string[] };
    expect(info.methods).toContain("sessions.poll");
    await bridge.shutdown();
    const restarted = new BridgeServer({ adapters: { codex: adapter() } });
    const restored = await restarted.handle("sessions.poll", { provider: "codex", sessionId: "native", cursor: third.cursor }) as SessionPollResult;
    expect(restored.entries).toEqual([]);
    expect(restored.reset).toBe(false);
    await restarted.shutdown();
  });

  it("walks multiple ascending pages and never skips appends made after the head checkpoint", async () => {
    const { state, request } = fixture([turn(0)]);
    const first = await pollCodexSession(request, { provider: "codex", sessionId: "native" }, () => "version");
    state.turns.push(...Array.from({ length: 45 }, (_, index) => turn(index + 1)));
    state.afterHead = () => state.turns.push(turn(46));
    const second = await pollCodexSession(request, { provider: "codex", sessionId: "native", cursor: first.cursor }, () => "version");
    expect(second.entries).toHaveLength(45);
    expect(second.entries.at(-1)?.turnId).toBe("turn-45");
    const third = await pollCodexSession(request, { provider: "codex", sessionId: "native", cursor: second.cursor }, () => "version");
    expect(third.entries.map((entry) => entry.turnId)).toEqual(["turn-46"]);
  });

  it("reports missing cursor/history and removed boundary items with an explicit reset", async () => {
    const { state, request } = fixture([turn(0)]);
    const first = await pollCodexSession(request, { provider: "codex", sessionId: "native" }, () => "version");
    state.invalidCursor = true;
    const lost = await pollCodexSession(request, { provider: "codex", sessionId: "native", cursor: first.cursor }, () => "version");
    expect(lost.reset).toBe(true);
    expect(lost.gaps.join(" ")).toContain("Incremental history");
    state.invalidCursor = false;
    state.turns[0] = { ...turn(0), items: [] };
    const removed = await pollCodexSession(request, { provider: "codex", sessionId: "native", cursor: first.cursor }, () => "version");
    expect(removed.reset).toBe(true);
    expect(removed.entries).toEqual([]);
    expect(removed.gaps.join(" ")).toContain("removed");
    state.turns = [];
    const missing = await pollCodexSession(request, { provider: "codex", sessionId: "native", cursor: first.cursor }, () => "version");
    expect(missing.reset).toBe(true);
    expect(missing.gaps.join(" ")).toContain("unavailable");
  });

  it("propagates transient disconnects so retry catches up from the unchanged durable cursor", async () => {
    const { state, request } = fixture([turn(0)]);
    const first = await pollCodexSession(request, { provider: "codex", sessionId: "native" }, () => "version");
    state.turns.push(...Array.from({ length: 45 }, (_, index) => turn(index + 1)));
    await expect(pollCodexSession(async (method, params) => {
      if (method === "thread/turns/list" && params.sortDirection === "asc") throw new Error("JSON-RPC process closed");
      return await request(method, params);
    }, { provider: "codex", sessionId: "native", cursor: first.cursor }, () => "version")).rejects.toThrow("process closed");
    const retried = await pollCodexSession(request, { provider: "codex", sessionId: "native", cursor: first.cursor }, () => "version");
    expect(retried.reset).toBe(false);
    expect(retried.gaps).toEqual([]);
    expect(retried.entries).toHaveLength(45);
    expect(retried.entries[0]?.turnId).toBe("turn-1");
  });

  it("bounds payload and cursor and rejects foreign cursors before any Provider read", async () => {
    const { state, request } = fixture(Array.from({ length: 5 }, (_, index) => turn(index, "x".repeat(70_000))));
    const first = await pollCodexSession(request, { provider: "codex", sessionId: "native", limit: 2 }, () => "version");
    expect(first.entries.map((entry) => entry.turnId)).toEqual(["turn-3", "turn-4"]);
    expect(first.entries.every((entry) => entry.text.length < 65_000)).toBe(true);
    expect(first.truncated).toBe(true);
    expect(Buffer.byteLength(first.cursor)).toBeLessThan(MAX_POLL_CURSOR_BYTES);
    const before = state.calls.length;
    for (const cursor of ["x".repeat(MAX_POLL_CURSOR_BYTES + 1), "!!!", Buffer.from(JSON.stringify({ version: 1, provider: "codex", sessionId: "another" })).toString("base64url")]) {
      await expect(pollCodexSession(request, { provider: "codex", sessionId: "native", cursor }, () => "version")).rejects.toThrow(/cursor/);
    }
    expect(state.calls).toHaveLength(before);
    await expect(pollCodexSession(request, { provider: "codex", sessionId: "native", limit: 501 }, () => "version")).rejects.toThrow(/limit/);
  });

  it("rebuilds after a reader version change and bounds excessive catch-up", async () => {
    const { state, request } = fixture([turn(0)]);
    const first = await pollCodexSession(request, { provider: "codex", sessionId: "native" }, () => "old");
    const upgrade = await pollCodexSession(request, { provider: "codex", sessionId: "native", cursor: first.cursor }, () => "new");
    expect(upgrade.reset).toBe(true);
    expect(upgrade.gaps.join(" ")).toContain("version changed");
    state.turns.push(...Array.from({ length: 250 }, (_, index) => turn(index + 1)));
    const bounded = await pollCodexSession(request, { provider: "codex", sessionId: "native", cursor: first.cursor }, () => "old");
    expect(bounded.reset).toBe(true);
    expect(bounded.gaps).not.toEqual([]);
    expect(bounded.entries.at(-1)?.turnId).toBe("turn-250");
    expect(state.calls.filter((call) => call.params.sortDirection === "asc")).toHaveLength(10);
  });

  it("bounds JSON-escaped tool output without moving the fixed checkpoint or killing the Bridge", async () => {
    const { state, adapter } = fixture([turn(0)]);
    const bridge = new BridgeServer({ adapters: { codex: adapter() } });
    const first = await bridge.handle("sessions.poll", { provider: "codex", sessionId: "native" }) as SessionPollResult;
    // Each native page is below 4 MiB; combining three pages would exceed the
    // Core JSONL line budget despite containing fewer than 2M characters.
    state.turns.push(...Array.from({ length: 60 }, (_, index) => ({
      ...turn(index + 1), items: [{ type: "commandExecution", id: `tool-${index + 1}`, aggregatedOutput: "\u0000".repeat(20_000) }],
    })));
    state.afterHead = () => state.turns.push(turn(61, "arrived after checkpoint"));
    const second = await bridge.handle("sessions.poll", { provider: "codex", sessionId: "native", cursor: first.cursor }) as SessionPollResult;
    expect(Buffer.byteLength(JSON.stringify(second))).toBeLessThanOrEqual(MAX_REVIEW_RESULT_BYTES);
    expect(second.truncated).toBe(true);
    expect(second.gaps.join(" ")).toContain("transport byte limit");
    expect(second.warnings.join(" ")).toContain("checkpoint is partial");
    expect(second.entries).not.toHaveLength(60);
    expect(second.entries[0]?.text).toContain("truncated to fit review transport");
    expect(second.entries.at(-1)).toMatchObject({ turnId: "turn-60", kind: "tool", text: "\u0000".repeat(20_000) });
    const saved = JSON.parse(Buffer.from(second.cursor, "base64url").toString("utf8")) as { boundary: string; native: string };
    expect(saved).toMatchObject({ boundary: "turn-60", native: "anchor:turn-60" });
    const third = await bridge.handle("sessions.poll", { provider: "codex", sessionId: "native", cursor: second.cursor }) as SessionPollResult;
    expect(third.entries.map((entry) => entry.turnId)).toEqual(["turn-61"]);
    expect(third.reset).toBe(false);
    await expect(bridge.handle("bridge.ping", {})).resolves.toMatchObject({ ok: true });
    expect(state.calls.some((call) => /resume|subscribe|start$|thread\/list|fork/.test(call.method))).toBe(false);
    await bridge.shutdown();
  });

  it("keeps active-Turn progress as upserts, not transient synthetic entries", async () => {
    const { state, request } = fixture([{ ...turn(0, "partial"), status: "inProgress" }]);
    const first = await pollCodexSession(request, { provider: "codex", sessionId: "native" }, () => "version");
    expect(first.entries).toHaveLength(1);
    expect(first.warnings.join(" ")).toContain("not a subscribed event stream");
    state.turns[0] = turn(0, "complete");
    const second = await pollCodexSession(request, { provider: "codex", sessionId: "native", cursor: first.cursor }, () => "version");
    expect(second.reset).toBe(false);
    expect(second.gaps).toEqual([]);
    expect(second.entries[0]).toMatchObject({ id: first.entries[0]?.id, text: "complete" });
  });

  it("keeps the newest items of a large Turn and marks unstable native item IDs", async () => {
    const large = { ...turn(0), items: Array.from({ length: 550 }, (_, index) => ({ type: "agentMessage", id: `large-${index}`, text: `latest ${index}` })) };
    const { state, request } = fixture([large]);
    const first = await pollCodexSession(request, { provider: "codex", sessionId: "native", limit: 200 }, () => "version");
    expect(first.entries).toHaveLength(200);
    expect(first.entries.at(-1)?.text).toBe("latest 549");
    expect(first.truncated).toBe(true);
    state.turns = [{ ...turn(0), items: [{ type: "agentMessage", text: "anonymous" }] }];
    const unstable = await pollCodexSession(request, { provider: "codex", sessionId: "native" }, () => "version");
    expect(unstable.gaps).not.toEqual([]);
    expect(unstable.warnings.join(" ")).toContain("unique stable identities");
  });

  it("stops repeated cursors and validates RPC input without calling an Agent", async () => {
    const { state, request, adapter } = fixture([turn(0)]);
    const first = await pollCodexSession(request, { provider: "codex", sessionId: "native" }, () => "version");
    state.turns.push(turn(1));
    let repeats = 0;
    const repeated = await pollCodexSession(async (method, params) => {
      if (method === "thread/turns/list" && params.sortDirection === "asc") {
        repeats++;
        return { data: [turn(0)], nextCursor: "stuck", backwardsCursor: "anchor:turn-0" };
      }
      return await request(method, params);
    }, { provider: "codex", sessionId: "native", cursor: first.cursor }, () => "version");
    expect(repeats).toBe(2);
    expect(repeated.reset).toBe(true);
    expect(repeated.gaps).not.toEqual([]);
    const bridge = new BridgeServer({ adapters: { codex: adapter() } });
    const count = state.calls.length;
    for (const input of [{ cursor: 1 }, { limit: 501 }, { sessionId: "x".repeat(257) }, { cursor: "x".repeat(MAX_POLL_CURSOR_BYTES + 1) }]) {
      await expect(bridge.handle("sessions.poll", { provider: "codex", sessionId: "native", ...input })).rejects.toThrow();
    }
    await expect(bridge.handle("sessions.poll", { provider: "claude", sessionId: "native" })).rejects.toThrow(/does not implement/);
    expect(state.calls).toHaveLength(count);
    await bridge.shutdown();
  });
});
