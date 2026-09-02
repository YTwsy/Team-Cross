import { describe, expect, it } from "vitest";

import { ClaudeAdapter } from "../src/adapters/claude.js";
import { CodexAdapter } from "../src/adapters/codex.js";
import type { JsonlRpcProcess } from "../src/lib/jsonl-rpc-client.js";
import { MAX_REVIEW_RESULT_BYTES, normalizeClaudeSnapshot, normalizeCodexSnapshot } from "../src/lib/session-snapshot.js";
import { BridgeServer } from "../src/server.js";
import type { SessionSnapshot } from "../src/protocol.js";

const capturedAt = "2026-09-03T00:00:00.000Z";

function codexHistory(items: unknown[]) {
  return { thread: { id: "conversation-a", sessionId: "root-family", source: "cli", name: "Review me", cwd: "/original", turns: [{ id: "turn-a", items }] } };
}

describe("native history review snapshots", () => {
  it("preserves the actual Codex conversation identity and stable anchors across captures", () => {
    const input = codexHistory([
      { type: "userMessage", id: "user-a", content: [{ type: "text", text: "Explain the failure" }] },
      { type: "commandExecution", id: "tool-a", command: "go test ./...", aggregatedOutput: "PASS", exitCode: 0 },
      { type: "agentMessage", id: "assistant-a", text: "The failure is fixed" },
    ]);
    const snapshot = normalizeCodexSnapshot(input, "conversation-a", { capturedAt });
    expect(snapshot.source).toMatchObject({
      sessionId: "conversation-a", identityKind: "thread.id", surface: "cli",
      nativeIds: { threadId: "conversation-a", sessionId: "root-family" },
    });
    expect(snapshot.entries.map((entry) => entry.kind)).toEqual(["message", "tool", "message"]);
    expect(snapshot.entries[1]).toMatchObject({ turnId: "turn-a", sourceId: "tool-a", text: "go test ./...\nPASS\nExit code: 0" });
    expect(snapshot.truncated).toBe(false);
    expect(normalizeCodexSnapshot(input, "conversation-a").entries).toEqual(snapshot.entries);
    expect(snapshot.capabilities).toMatchObject({ read: true, follow: false, open: false, resume: false, takeControl: false });
  });

  it("does not confuse appServer origin with Desktop and rejects identity mismatches", () => {
    const input = codexHistory([]);
    input.thread.source = "appServer";
    expect(normalizeCodexSnapshot(input, "conversation-a").source.surface).toBe("app-server");
    expect(() => normalizeCodexSnapshot(input, "root-family")).toThrow(/conversation identity/);
    expect(() => normalizeCodexSnapshot({}, "conversation-a")).toThrow(/conversation identity/);
  });

  it("reports unsupported and truncated material without exposing unknown payloads", () => {
    const snapshot = normalizeCodexSnapshot(codexHistory([
      { type: "reasoning", id: "private", content: "MUST_NOT_BE_IMPORTED" },
      { type: "userMessage", id: "image", content: [{ type: "image", url: "data:SECRET" }] },
      { type: "agentMessage", id: "long", text: "x".repeat(70_000) },
      { type: "contextCompaction", id: "compacted" },
    ]), "conversation-a");
    expect(snapshot.truncated).toBe(true);
    expect(snapshot.warnings.length).toBeGreaterThan(0);
    expect(JSON.stringify(snapshot)).not.toContain("MUST_NOT_BE_IMPORTED");
    expect(JSON.stringify(snapshot)).not.toContain("data:SECRET");
    expect(snapshot.entries.find((entry) => entry.sourceId === "long")?.text.length).toBeLessThan(65_000);
    const limited = normalizeCodexSnapshot(codexHistory([
      { type: "agentMessage", id: "first", text: "first" },
      { type: "agentMessage", id: "second", text: "second" },
    ]), "conversation-a", { limit: 1 });
    expect(limited.entries).toHaveLength(1);
    expect(limited.truncated).toBe(true);
    expect(limited.warnings).not.toHaveLength(0);
  });

  it("includes known text/structured tool output but excludes tool private metadata", () => {
    const snapshot = normalizeCodexSnapshot(codexHistory([
      { type: "functionCallOutput", id: "f1", name: "exec", output: [{ type: "input_text", text: "stdout" }] },
      { type: "dynamicToolCall", id: "d1", tool: "inspect", contentItems: [{ type: "inputText", text: "dynamic output" }] },
      { type: "mcpToolCall", id: "m1", server: "server", tool: "read", result: { content: [{ type: "text", text: "result" }], structuredContent: { count: 1 }, _meta: { private: "MUST_NOT_IMPORT" } } },
    ]), "conversation-a");
    expect(snapshot.entries[0]?.text).toContain("stdout");
    expect(snapshot.entries[1]?.text).toContain("dynamic output");
    expect(snapshot.entries[2]?.text).toContain('{"count":1}');
    expect(JSON.stringify(snapshot)).not.toContain("MUST_NOT_IMPORT");
  });

  it("normalizes Claude messages and tool results, keeping duplicate anonymous entries distinct", () => {
    const snapshot = normalizeClaudeSnapshot([
      { type: "user", uuid: "u1", session_id: "s1", message: { content: "Please inspect" } },
      { type: "assistant", uuid: "a1", session_id: "s1", message: { content: [
        { type: "text", text: "Inspecting now" },
        { type: "tool_use", id: "tool-1", name: "Read", input: { file_path: "main.go" } },
      ] } },
      { type: "user", uuid: "u2", session_id: "s1", message: { content: [{ type: "tool_result", tool_use_id: "tool-1", content: [{ type: "text", text: "package main" }] }] } },
      { type: "assistant", message: { content: "same" } },
      { type: "assistant", message: { content: "same" } },
    ], "s1", { capturedAt });
    expect(snapshot.entries.map((entry) => entry.kind)).toEqual(["message", "message", "tool", "tool", "message", "message"]);
    expect(snapshot.entries[3]?.text).toBe("package main");
    expect(snapshot.entries[3]?.sourceId).toBe("u2");
    expect(new Set(snapshot.entries.map((entry) => entry.id)).size).toBe(6);
    expect(snapshot.source).toMatchObject({ identityKind: "sessionId", surface: "unknown" });
    expect(snapshot.truncated).toBe(false);
    expect(() => normalizeClaudeSnapshot([{ session_id: "wrong" }], "s1")).toThrow(/conversation identity/);
  });

  it("reports unavailable Claude history and non-text blocks honestly", () => {
    expect(normalizeClaudeSnapshot([], "s1").truncated).toBe(true);
    const snapshot = normalizeClaudeSnapshot([{ type: "assistant", uuid: "a1", message: { content: [{ type: "thinking", thinking: "PRIVATE" }] } }], "s1");
    expect(snapshot.truncated).toBe(true);
    expect(JSON.stringify(snapshot)).not.toContain("PRIVATE");
  });

  it.each(["codex", "claude"])("bounds %s snapshot JSON bytes and clearly marks omitted escaped text", (provider) => {
    const text = "\u0000".repeat(50_000);
    const snapshot = provider === "codex"
      ? normalizeCodexSnapshot(codexHistory(Array.from({ length: 40 }, (_, index) => ({ type: "commandExecution", id: `tool-${index}`, aggregatedOutput: text }))), "conversation-a")
      : normalizeClaudeSnapshot(Array.from({ length: 40 }, (_, index) => ({ type: "user", uuid: `tool-${index}`, message: { content: [{ type: "tool_result", content: text }] } })), "conversation-a");
    expect(Buffer.byteLength(JSON.stringify(snapshot))).toBeLessThanOrEqual(MAX_REVIEW_RESULT_BYTES);
    expect(snapshot.truncated).toBe(true);
    expect(snapshot.warnings.join(" ")).toContain("transport byte limit");
    expect(snapshot.entries.length).toBeLessThan(40);
    expect(snapshot.entries[0]?.text).toContain("truncated to fit review transport");
    expect(snapshot.entries.at(-1)).toMatchObject({ sourceId: "tool-39", text });
    expect(snapshot.source.sessionId).toBe("conversation-a");
    expect(snapshot.capabilities).toMatchObject({ follow: false, resume: false, takeControl: false });
  });

  it("rejects unrepresentable metadata without changing its native identity", () => {
    const input = codexHistory([]);
    input.thread.name = "\u0000".repeat(MAX_REVIEW_RESULT_BYTES / 6 + 1);
    expect(() => normalizeCodexSnapshot(input, "conversation-a")).toThrow("metadata exceeds the transport byte limit");
    expect(input.thread.id).toBe("conversation-a");
  });
});

describe("snapshot RPC is zero execution", () => {
  it("Codex snapshot calls only initialize and targeted thread/read", async () => {
    const calls: Array<{ method: string; params: unknown }> = [];
    const fake = {
      start() {}, notify() {}, async close() {},
      async request(method: string, params: unknown) {
        calls.push({ method, params });
        if (method === "initialize") return { userAgent: "codex-test" };
        if (method === "thread/list") return { data: [codexHistory([]).thread], nextCursor: null };
        if (method === "thread/read") return codexHistory([{ type: "agentMessage", id: "a1", text: "Only evidence" }]);
        throw new Error(`Forbidden execution method: ${method}`);
      },
    };
    const adapter = new CodexAdapter({ clientFactory: () => fake as unknown as JsonlRpcProcess });
    const server = new BridgeServer({ adapters: { codex: adapter } });
    const snapshot = await server.handle("sessions.snapshot", { provider: "codex", sessionId: "conversation-a" }) as SessionSnapshot;
    expect(calls.map((call) => call.method)).toEqual(["initialize", "thread/read", "thread/read"]);
    expect(calls.at(-1)?.params).toEqual({ threadId: "conversation-a", includeTurns: true });
    expect(snapshot.source.providerVersion).toBe("codex-test");
    const stored = await adapter.listStored({ provider: "codex" });
    expect(stored[0]).toMatchObject({ identityKind: "thread.id", surface: "cli", metadata: { threadId: "conversation-a", sessionTreeId: "root-family" } });
    expect(calls.at(-1)?.params).toMatchObject({ sourceKinds: expect.arrayContaining(["cli", "appServer", "unknown"]), useStateDbOnly: true });
    await expect(adapter.listStored({ provider: "codex", surface: "desktop" })).resolves.toEqual([]);
    await expect(server.handle("sessions.snapshot", { provider: "codex", sessionId: "conversation-a", limit: -1 })).rejects.toThrow(/limit/);
    await expect(server.handle("sessions.snapshot", { provider: "codex", sessionId: "conversation-a", limit: 10001 })).rejects.toThrow(/limit/);
    await server.shutdown();
  });

  it("reads paginated Codex history without resume and stops on repeated cursors", async () => {
    const calls: string[] = [];
    const fake = {
      start() {}, notify() {}, async close() {},
      async request(method: string, params: unknown) {
        calls.push(method);
        if (method === "initialize") return { userAgent: "codex-test" };
        if (method === "thread/list") return { data: [] };
        if (method === "thread/read") {
          expect(params).toMatchObject({ includeTurns: false });
          return { thread: { ...codexHistory([]).thread, historyMode: "paginated" } };
        }
        if (method === "thread/turns/list") {
          expect(params).toMatchObject({ sortDirection: "asc", itemsView: "full" });
          return { data: [{ id: "turn-a", items: [{ type: "agentMessage", id: "a", text: "saved" }], itemsView: "full" }], nextCursor: "repeated" };
        }
        throw new Error(`Forbidden execution method: ${method}`);
      },
    };
    const adapter = new CodexAdapter({ clientFactory: () => fake as unknown as JsonlRpcProcess });
    const snapshot = await adapter.snapshot({ provider: "codex", sessionId: "conversation-a" });
    expect(calls).toEqual(["initialize", "thread/read", "thread/turns/list", "thread/turns/list"]);
    expect(snapshot.entries.filter((entry) => entry.kind === "message")).toHaveLength(1);
    expect(snapshot.truncated).toBe(true);
    await adapter.shutdown();
  });

  it("Claude snapshot needs no credentials, query, resume or list-all lookup", async () => {
    const calls: string[] = [];
    const adapter = new ClaudeAdapter({ env: {}, sdkLoader: async () => ({
      query() { throw new Error("must not execute"); },
      async listSessions() { throw new Error("must not enumerate unrelated sessions"); },
      async getSessionInfo(sessionId) { calls.push(`info:${sessionId}`); return { sessionId, summary: "Review only" }; },
      async getSessionMessages(sessionId, options) {
        calls.push(`read:${sessionId}`);
        expect(options).toMatchObject({ limit: 2, includeSystemMessages: true });
        return [
          { type: "user", uuid: "first", message: { content: "one" } },
          { type: "assistant", uuid: "second", message: { content: "two" } },
        ];
      },
    }) });
    const server = new BridgeServer({ adapters: { claude: adapter } });
    const snapshot = await server.handle("sessions.snapshot", { provider: "claude", sessionId: "s1", limit: 1 }) as SessionSnapshot;
    expect(calls).toEqual(["read:s1", "info:s1"]);
    expect(snapshot.source.title).toBe("Review only");
    expect(snapshot.truncated).toBe(true);
    expect(snapshot.entries[0]?.text).toBe("one");
    const info = await server.handle("bridge.info", {}) as { methods: string[] };
    expect(info.methods).toContain("sessions.snapshot");
    await server.shutdown();
  });
});
