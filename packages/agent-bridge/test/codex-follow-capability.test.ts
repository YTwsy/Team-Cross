import { describe, expect, it } from "vitest";

import { CodexFollowCapability, CodexFollowCapabilityError, MAX_CODEX_FOLLOW_PROBES, type CodexFollowRequest } from "../src/lib/codex-follow-capability.js";
import { pollCodexSession } from "../src/lib/codex-poll.js";
import { normalizeCodexSnapshot, reviewCapabilities } from "../src/lib/session-snapshot.js";

type RecordValue = Record<string, unknown>;
function nativeTurn(items = [{ id: "item", type: "agentMessage", text: "saved" }]) {
  return { id: "turn", itemsView: "full", status: "completed", items };
}
function nativePage() {
  return { data: [nativeTurn()], nextCursor: null, backwardsCursor: "opaque-boundary" };
}
interface ReaderState {
  head: unknown;
  replay: unknown;
  metadata: RecordValue;
  wrongID?: string;
  failure?: Error;
  calls: Array<{ method: string; params: RecordValue }>;
}
function reader() {
  const state: ReaderState = { head: nativePage(), replay: nativePage(), metadata: { historyMode: "paginated", source: "vscode", cliVersion: "future-unlisted-build" }, calls: [] };
  const request: CodexFollowRequest = async (method, params) => {
    state.calls.push({ method, params });
    if (state.failure) throw state.failure;
    if (method === "thread/read") return { thread: { ...state.metadata, id: state.wrongID ?? params.threadId } };
    if (method === "thread/turns/list") return structuredClone(params.sortDirection === "desc" ? state.head : state.replay);
    throw new Error(`Forbidden method ${method}`);
  };
  return { state, request, capability: new CodexFollowCapability(request) };
}

describe("Codex persisted-poll runtime capability", () => {
  it("verifies only the exact Session with three bounded reads, without UI or version allowlists", async () => {
    const { state, capability } = reader();
    await expect(capability.probe("native")).resolves.toMatchObject({ supported: true });
    expect(state.calls).toEqual([
      { method: "thread/read", params: { threadId: "native", includeTurns: false } },
      { method: "thread/turns/list", params: { threadId: "native", sortDirection: "desc", itemsView: "full", limit: 1 } },
      { method: "thread/turns/list", params: { threadId: "native", sortDirection: "asc", itemsView: "full", limit: 1, cursor: "opaque-boundary" } },
    ]);
    expect(reviewCapabilities("codex")).toMatchObject({ follow: false, open: false, resume: false, takeControl: false });
  });

  it("accepts stable reasoning IDs but leaves their omission/truncation to the normalizer", async () => {
    const { state, capability } = reader();
    const turn = nativeTurn([{ id: "reasoning", type: "reasoning", text: "PRIVATE" }]);
    state.head = state.replay = { ...nativePage(), data: [turn] };
    await expect(capability.probe("native")).resolves.toMatchObject({ supported: true });
    const snapshot = normalizeCodexSnapshot({ thread: { id: "native", turns: [turn] } }, "native");
    expect(snapshot.truncated).toBe(true);
    expect(JSON.stringify(snapshot)).not.toContain("PRIVATE");
  });

  it("allows an active boundary to append or update content without changing existing item identities", async () => {
    const { state, capability } = reader();
    state.replay = { ...nativePage(), data: [nativeTurn([{ id: "item", type: "agentMessage", text: "updated" }, { id: "next", type: "agentMessage", text: "new" }])] };
    await expect(capability.probe("native")).resolves.toMatchObject({ supported: true });
  });

  it.each<{ name: string; mutate: (state: ReaderState) => void }>([
    { name: "wrong Session", mutate: (s) => { s.wrongID = "another"; } },
    { name: "missing paginated contract", mutate: (s) => { s.metadata.historyMode = "inline"; } },
    { name: "empty Session", mutate: (s) => { s.head = { data: [], nextCursor: null, backwardsCursor: null }; } },
    { name: "no persisted items", mutate: (s) => { s.head = { ...nativePage(), data: [nativeTurn([])] }; } },
    { name: "missing reverse boundary", mutate: (s) => { s.head = { ...nativePage(), backwardsCursor: null }; } },
    { name: "missing cursor shape", mutate: (s) => { s.head = { data: [nativeTurn()], nextCursor: null }; } },
    { name: "oversized native cursor", mutate: (s) => { s.head = { ...nativePage(), backwardsCursor: "x".repeat(8193) }; } },
    { name: "summary Turn", mutate: (s) => { s.head = { ...nativePage(), data: [{ ...nativeTurn(), itemsView: "summary" }] }; } },
    { name: "missing item identity", mutate: (s) => { s.head = { ...nativePage(), data: [{ ...nativeTurn(), items: [{ type: "agentMessage", text: "anonymous" }] }] }; } },
    { name: "duplicate item identity", mutate: (s) => { s.head = { ...nativePage(), data: [nativeTurn([...nativeTurn().items, ...nativeTurn().items])] }; } },
    { name: "missing item kind", mutate: (s) => { s.head = { ...nativePage(), data: [{ ...nativeTurn(), items: [{ id: "item" }] }] }; } },
    { name: "over-budget page", mutate: (s) => { s.head = { ...nativePage(), data: [nativeTurn(), nativeTurn()] }; } },
    { name: "different replay boundary", mutate: (s) => { s.replay = { ...nativePage(), data: [{ ...nativeTurn(), id: "other-turn" }] }; } },
    { name: "removed replay item", mutate: (s) => { s.replay = { ...nativePage(), data: [nativeTurn([])] }; } },
    { name: "changed replay item kind", mutate: (s) => { s.replay = { ...nativePage(), data: [nativeTurn([{ id: "item", type: "reasoning", text: "changed kind" }])] }; } },
  ])("fails closed for $name", async ({ mutate }) => {
    const { state, capability } = reader();
    mutate(state);
    const result = await capability.probe("native");
    expect(result.supported).toBe(false);
    expect(result.reason).not.toBe("");
  });

  it("never caches failure permanently or exposes Provider error content", async () => {
    const { state, capability } = reader();
    state.failure = new Error("PRIVATE_PATH_AND_PROVIDER_ERROR");
    const rejected = await capability.probe("native");
    expect(rejected.supported).toBe(false);
    expect(rejected.reason).not.toContain("PRIVATE");
    state.failure = undefined;
    await expect(capability.probe("native")).resolves.toMatchObject({ supported: true });
    expect(state.calls).toHaveLength(4);
  });

  it("shares concurrent verification and bounds successful Session caching", async () => {
    const { state, capability } = reader();
    const results = await Promise.all(Array.from({ length: 8 }, () => capability.probe("native")));
    expect(results.every((value) => value.supported)).toBe(true);
    expect(state.calls).toHaveLength(3);
    for (let index = 0; index < MAX_CODEX_FOLLOW_PROBES; index++) {
      await expect(capability.probe(`session-${index}`)).resolves.toMatchObject({ supported: true });
    }
    const before = state.calls.length;
    await capability.probe(`session-${MAX_CODEX_FOLLOW_PROBES - 1}`);
    expect(state.calls).toHaveLength(before);
    await capability.probe("native");
    expect(state.calls).toHaveLength(before + 3);
  });

  it("bounds pending checks and refuses a retired generation's late success", async () => {
    const { state, request } = reader();
    let release!: () => void;
    const wait = new Promise<void>((resolve) => { release = resolve; });
    const capability = new CodexFollowCapability(async (method, params) => { await wait; return request(method, params); });
    const pending = Array.from({ length: MAX_CODEX_FOLLOW_PROBES }, (_, index) => capability.probe(`pending-${index}`));
    await expect(capability.probe("overflow")).resolves.toMatchObject({ supported: false, reason: expect.stringContaining("busy") });
    capability.retire();
    release();
    expect((await Promise.all(pending)).every((result) => !result.supported)).toBe(true);
    const count = state.calls.length;
    await expect(capability.probe("pending-0")).resolves.toMatchObject({ supported: false });
    expect(state.calls).toHaveLength(count);
    await expect(new CodexFollowCapability(request).probe("pending-0")).resolves.toMatchObject({ supported: true });
  });

  it.each([
    ["thread/resume", { threadId: "native" }],
    ["thread/list", {}],
    ["thread/read", { threadId: "other", includeTurns: false }],
    ["thread/read", { threadId: "native", includeTurns: true }],
    ["thread/read", { threadId: "native", includeTurns: false, extra: true }],
    ["thread/turns/list", { threadId: "native", itemsView: "summary", sortDirection: "desc", limit: 20 }],
    ["thread/turns/list", { threadId: "native", itemsView: "full", sortDirection: "desc", limit: 21 }],
  ])("rejects an out-of-contract request before any probe: %s %j", async (method, params) => {
    const { state, capability } = reader();
    await expect(capability.wrapRequest("native")(method as string, params as RecordValue)).rejects.toBeInstanceOf(CodexFollowCapabilityError);
    expect(state.calls).toEqual([]);
  });

  it.each([
    { ...nativePage(), data: [{ ...nativeTurn(), itemsView: "summary" }] },
    { ...nativePage(), data: [{ ...nativeTurn(), items: [{ type: "reasoning" }] }] },
    { ...nativePage(), backwardsCursor: null },
  ])("continuously rejects malformed later pages and invalidates cached capability", async (bad) => {
    const { state, capability } = reader();
    await capability.require("native");
    state.head = bad;
    await expect(capability.wrapRequest("native")("thread/turns/list", { threadId: "native", itemsView: "full", sortDirection: "desc", limit: 20 })).rejects.toBeInstanceOf(CodexFollowCapabilityError);
    const before = state.calls.length;
    state.head = nativePage();
    await expect(capability.probe("native")).resolves.toMatchObject({ supported: true });
    expect(state.calls).toHaveLength(before + 3);
  });

  it("does not let page-format failures become a gap reset or consume a saved poll cursor", async () => {
    const { state, capability } = reader();
    const request = capability.wrapRequest("native");
    const first = await pollCodexSession(request, { provider: "codex", sessionId: "native" }, () => "reader");
    const saved = first.cursor;
    state.replay = { ...nativePage(), data: [{ ...nativeTurn(), itemsView: "summary" }] };
    await expect(pollCodexSession(request, { provider: "codex", sessionId: "native", cursor: saved }, () => "reader")).rejects.toBeInstanceOf(CodexFollowCapabilityError);
    expect(first.cursor).toBe(saved);
    state.replay = nativePage();
    const recovered = await pollCodexSession(request, { provider: "codex", sessionId: "native", cursor: saved }, () => "reader");
    expect(recovered.entries).toEqual([]);
    expect(recovered.reset).toBe(false);
  });

  it("rejects a read that completes after its generation was retired", async () => {
    const { request } = reader();
    let block = false;
    let release!: () => void;
    let entered!: () => void;
    const waiting = new Promise<void>((resolve) => { release = resolve; });
    const started = new Promise<void>((resolve) => { entered = resolve; });
    const capability = new CodexFollowCapability(async (method, params) => {
      if (block) { entered(); await waiting; }
      return request(method, params);
    });
    await capability.require("native");
    block = true;
    const pending = capability.wrapRequest("native")("thread/read", { threadId: "native", includeTurns: false });
    await started;
    capability.retire();
    release();
    await expect(pending).rejects.toBeInstanceOf(CodexFollowCapabilityError);
  });
});
