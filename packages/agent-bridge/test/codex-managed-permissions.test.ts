import { describe, expect, it } from "vitest";
import { CodexAdapter } from "../src/adapters/codex.js";
import type { AgentAdapter } from "../src/adapters/types.js";
import type { JsonlRpcProcess, JsonlRpcProcessOptions } from "../src/lib/jsonl-rpc-client.js";
import type { BridgeEvent } from "../src/protocol.js";
import { isToolMetadataMethod, syntheticToolMetadata, verifiedThreadResponse } from "./codex-permission-fixture.js";

type Params = Parameters<AgentAdapter["createRun"]>[0];
const base: Params = { provider: "codex", runId: "run-1", worktree: "/tmp/synthetic-worktree", model: "gpt-5.6-luna" };
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => { resolve = yes; });
  return { promise, resolve };
}
class FakeClient {
  calls: Array<{ method: string; params: Record<string, unknown> }> = [];
  responses: Array<{ id: string | number; result?: unknown; error?: string }> = [];
  closeCount = 0;
  response = (params: unknown): unknown => verifiedThreadResponse(params, this.id);
  hook?: (method: string, input: Record<string, unknown>) => Promise<unknown>;
  closeHook?: () => Promise<void>;
  constructor(readonly options: JsonlRpcProcessOptions, readonly id: string) {}
  start() {}
  notify() {}
  respond(id: string | number, result: unknown) { this.responses.push({ id, result }); }
  respondError(id: string | number, _code: number, error: string) { this.responses.push({ id, error }); }
  async close() { this.closeCount++; await this.closeHook?.(); this.options.onExit?.(new Error("synthetic close")); }
  async request(method: string, params: Record<string, unknown>): Promise<unknown> {
    this.calls.push({ method, params });
    if (this.hook) return await this.hook(method, params);
    return this.defaultResponse(method, params);
  }
  defaultResponse(method: string, params: Record<string, unknown>): unknown {
    if (isToolMetadataMethod(method)) return syntheticToolMetadata(method);
    if (method === "initialize") return { userAgent: `synthetic-${this.id}` };
    if (method === "thread/start" || method === "thread/fork") return this.response(params);
    if (method === "turn/start") return { turn: { id: "turn-1" } };
    if (method === "turn/interrupt") { this.notifyTurn("turn/completed", "interrupted"); return {}; }
    if (method === "thread/unsubscribe") return {};
    if (method === "thread/backgroundTerminals/list") return { data: [], nextCursor: null };
    if (method === "thread/read") return { thread: { id: params.threadId, source: "cli", turns: [] } };
    throw new Error(`unexpected synthetic method ${method}`);
  }
  notifyTurn(method: string, status?: string, threadId = this.id) {
    this.options.onNotification?.({ method, params: { threadId, turn: { id: "turn-1", status } } });
  }
  input(id: string, threadId = this.id) {
    this.options.onRequest?.({ id, method: "item/tool/requestUserInput", params: { threadId, questions: [{ id: "q" }] } });
  }
}
function fixture(setup?: (client: FakeClient, index: number) => void) {
  const clients: FakeClient[] = [];
  const events: BridgeEvent[] = [];
  const diagnostics: string[] = [];
  const adapter = new CodexAdapter({ onDiagnostic: (message) => diagnostics.push(message), clientFactory: (options) => {
    const client = new FakeClient(options, `thread-${clients.length + 1}`);
    setup?.(client, clients.length); clients.push(client);
    return client as unknown as JsonlRpcProcess;
  } });
  return { adapter, clients, events, diagnostics, create: (params: Partial<Params> = {}) => adapter.createRun({ ...base, ...params }, (event) => events.push(event)) };
}

describe("managed Codex explicit permission profiles", () => {
  it("selects unique immutable off/on profiles for each new Session and every Turn", async () => {
    const f = fixture();
    const off = await f.create(); const on = await f.create({ runId: "run-2", networkEnabled: true });
    await off.send("synthetic off"); await on.send("synthetic on");
    const profiles = f.clients.map((client, index) => {
      const create = client.calls.find((call) => call.method === "thread/start")!.params;
      expect(create).toMatchObject({ model: "gpt-5.6-luna", allowProviderModelFallback: false, approvalPolicy: "never", runtimeWorkspaceRoots: [base.worktree] });
      expect(create).not.toHaveProperty("sandbox");
      const environments = [{ environmentId: "local", cwd: base.worktree, runtimeWorkspaceRoots: [base.worktree] }];
      expect(create).toMatchObject({ environments, dynamicTools: [], selectedCapabilityRoots: [] });
      expect(client.calls.find((call) => call.method === "turn/start")!.params).toMatchObject({ permissions: create.permissions, model: "gpt-5.6-luna", cwd: base.worktree, runtimeWorkspaceRoots: [base.worktree], approvalPolicy: "never", environments });
      expect(client.calls.find((call) => call.method === "turn/start")!.params).not.toHaveProperty("sandboxPolicy");
      expect(client.options.args.slice(0, 4)).toEqual(["app-server", "--stdio", "-c", `permissions.${String(create.permissions)}={extends=":workspace",filesystem={":tmpdir"="read",":slash_tmp"="read"},network={enabled=${index === 1}}}`]);
      expect(client.options.cwd).toBe(base.worktree);
      expect(client.options.args).toContain("mcp_servers={}");
      expect(client.options.args).toContain("features.code_mode_host=true");
      expect(client.options.args).toContain("features.code_mode=false");
      expect(client.options.args).not.toContain("--code-mode-host");
      expect((create.config as any).features).toMatchObject({ code_mode_host: true, code_mode: false, apps: false, plugins: false });
      expect((create.config as any).mcp_servers).toEqual({});
      return create.permissions;
    });
    expect(profiles[0]).not.toBe(profiles[1]);
    await f.adapter.shutdown();
    expect(f.clients.map((client) => client.closeCount)).toEqual([1, 1]);
    expect(f.events.some((event) => event.type === "run.error")).toBe(false);
  });

  it.each([
    ["missing provenance", (v: any) => { delete v.activePermissionProfile; }],
    ["wrong profile", (v: any) => { v.activePermissionProfile.id = "default"; }],
    ["user profile inheritance", (v: any) => { v.activePermissionProfile.extends = "user-profile"; }],
    ["different cwd", (v: any) => { v.cwd = "/tmp/outside"; }],
    ["missing runtime roots", (v: any) => { delete v.runtimeWorkspaceRoots; }],
    ["additional runtime root", (v: any) => { v.runtimeWorkspaceRoots = [base.worktree, "/tmp/outside"]; }],
    ["approval escalation", (v: any) => { v.approvalPolicy = "on-request"; }],
    ["network expansion", (v: any) => { v.sandbox.networkAccess = true; }],
    ["TMPDIR write", (v: any) => { v.sandbox.excludeTmpdirEnvVar = false; }],
    ["slash tmp write", (v: any) => { v.sandbox.excludeSlashTmp = false; }],
    ["unrestricted sandbox", (v: any) => { v.sandbox.type = "dangerFullAccess"; }],
    ["additional writable root", (v: any) => { v.sandbox.writableRoots = ["/tmp/outside"]; }],
    ["model fallback", (v: any) => { v.model = "another-model"; }],
  ])("refuses %s before announcing or sending even an initial prompt", async (_name, mutate) => {
    const f = fixture((client) => { client.response = (params) => { const value = verifiedThreadResponse(params, client.id); mutate(value); return value; }; });
    await expect(f.create({ initialPrompt: "MUST NOT SEND" })).rejects.toThrow(/execution refused/);
    expect(f.events).toEqual([]);
    expect(f.clients[0]!.calls.filter((call) => !isToolMetadataMethod(call.method)).map((call) => call.method)).toEqual(["initialize", "thread/start", "thread/unsubscribe"]);
    expect(f.clients[0]!.closeCount).toBe(1);
    await f.adapter.shutdown();
  });

  it("refuses unsupported or organization-denied permissions without a legacy retry, but reading still works", async () => {
    const f = fixture((client, index) => {
      if (index !== 0) return;
      client.hook = async (method, params) => {
        if (method === "thread/start") throw new Error("permission profile is disallowed by managed requirements");
        return client.defaultResponse(method, params);
      };
    });
    await expect(f.create()).rejects.toThrow(/execution refused/);
    expect(f.clients[0]!.calls.filter((call) => !isToolMetadataMethod(call.method)).map((call) => call.method)).toEqual(["initialize", "thread/start"]);
    expect(f.clients[0]!.closeCount).toBe(1);
    const snapshot = await f.adapter.snapshot({ provider: "codex", sessionId: "known-history" });
    expect(snapshot.source.providerVersion).toBe("synthetic-thread-2");
    expect(f.clients[1]!.options.args).toEqual(["app-server", "--stdio"]);
    expect(f.clients[1]!.options.cwd).toBeUndefined();
    expect(f.clients[1]!.calls.map((call) => call.method)).toEqual(["initialize", "thread/read", "thread/read", "thread/read"]);
    expect(snapshot.capabilities.follow).toBe(false);
    await f.adapter.shutdown();
  });

  it("refuses unverified native Fork before starting a client; Team Cross Round continuation creates a new Session", async () => {
    const f = fixture();
    await expect(f.create({ forkFromSessionId: "source-id", initialPrompt: "must not send" })).rejects.toThrow(/Continue from Round/);
    expect(f.clients).toEqual([]); expect(f.events).toEqual([]);
    await f.adapter.shutdown();
  });

  it("keeps failed creation/duplicate identities and client exits isolated from an existing Run", async () => {
    const f = fixture((client, index) => {
      if (index === 1) client.response = (params) => ({ ...verifiedThreadResponse(params, client.id), activePermissionProfile: null });
      if (index === 2) client.response = (params) => verifiedThreadResponse(params, "thread-1");
    });
    const old = await f.create();
    await expect(f.create({ runId: "bad-policy" })).rejects.toThrow(/execution refused/);
    await expect(f.create({ runId: "duplicate" })).rejects.toThrow(/new conversation identity/);
    expect(f.clients[1]!.calls.at(-1)).toEqual({ method: "thread/unsubscribe", params: { threadId: "thread-2" } });
    expect(f.clients[2]!.calls.some((call) => call.method === "thread/unsubscribe")).toBe(false);
    expect(old.descriptor.status).toBe("idle"); expect(f.clients[0]!.closeCount).toBe(0);
    await old.send("still managed"); await f.adapter.shutdown();
  });

  it("routes requests/notifications only on their owning client and does not broadcast reader errors", async () => {
    const f = fixture(); const first = await f.create(); const second = await f.create({ runId: "run-2" });
    await f.adapter.readStored({ provider: "codex", sessionId: "known-history" });
    const [a, b, reader] = f.clients as [FakeClient, FakeClient, FakeClient];
    reader.options.onExit?.(new Error("reader failed"));
    reader.notifyTurn("turn/started", undefined, a.id); reader.input("reader-request", a.id);
    b.notifyTurn("turn/started", undefined, a.id); b.input("wrong-owner", a.id);
    expect(first.descriptor.status).toBe("idle"); expect(second.descriptor.status).toBe("idle");
    expect(f.events.filter((event) => event.type === "run.error")).toHaveLength(0);
    expect(reader.responses).toMatchObject([{ id: "reader-request", error: expect.any(String) }]);
    expect(b.responses).toMatchObject([{ id: "wrong-owner", error: expect.any(String) }]);
    a.input("own-request"); const input = f.events.find((event) => event.type === "input.requested")!;
    await first.respondInput(input.inputRequestId!, { answer: "synthetic" });
    expect(a.responses.at(-1)).toMatchObject({ id: "own-request", result: expect.any(Object) });
    b.options.onExit?.(new Error("only second failed"));
    expect(first.descriptor.status).toBe("idle"); expect(second.descriptor.status).toBe("error");
    expect(f.events.filter((event) => event.type === "run.error").map((event) => event.runId)).toEqual(["run-2"]);
    await f.adapter.shutdown();
  });

  it("cleans incompatible initialization without creating a Session", async () => {
    const f = fixture((client) => { client.hook = async () => ({}); });
    await expect(f.create()).rejects.toThrow(/execution refused/);
    expect(f.clients[0]!.calls.map((call) => call.method)).toEqual(["initialize"]);
    expect(f.clients[0]!.closeCount).toBe(1);
  });

  it("fences a client-disposal failure and retries without emitting a false closed event", async () => {
    const f = fixture(); const run = await f.create();
    f.clients[0]!.closeHook = async () => { throw new Error("client disposal failed"); };
    await expect(run.close()).rejects.toThrow(/disposal failed/);
    await expect(run.send("fenced")).rejects.toThrow(/fenced/);
    expect(f.events.filter((event) => event.type === "run.closed")).toHaveLength(0);
    f.clients[0]!.closeHook = undefined;
    await Promise.all([run.close(), run.close(), f.adapter.shutdown()]);
    expect(f.events.filter((event) => event.type === "run.closed")).toHaveLength(1);
    expect(f.clients[0]!.closeCount).toBe(2);
  });

  it("shutdown waits for an in-flight zero-prompt creation, discards it, and blocks new creation", async () => {
    const started = deferred<void>(); const result = deferred<unknown>();
    const f = fixture((client) => { client.hook = async (method, params) => {
      if (method === "thread/start") { started.resolve(); return await result.promise; }
      return client.defaultResponse(method, params);
    }; });
    const creating = f.create({ initialPrompt: "must not send after shutdown" });
    const failed = expect(creating).rejects.toThrow(/shutting down/);
    await started.promise;
    const shutdown = f.adapter.shutdown(); const concurrent = f.adapter.shutdown();
    await expect(f.create({ runId: "too-late" })).rejects.toThrow(/shutting down/);
    result.resolve(verifiedThreadResponse(f.clients[0]!.calls.find((call) => call.method === "thread/start")!.params, "thread-1"));
    await Promise.all([failed, shutdown, concurrent]);
    expect(f.events).toEqual([]);
    expect(f.clients[0]!.calls.filter((call) => !isToolMetadataMethod(call.method)).map((call) => call.method)).toEqual(["initialize", "thread/start", "thread/unsubscribe"]);
    expect(f.clients[0]!.closeCount).toBe(1);
  });

  it("uses bounded correctly escaped MCP keys in one rebuilt client and repeats restrictions on the new Session", async () => {
    const keys = ['synthetic.with.dot', 'quote"slash\\name'];
    const f = fixture((client, index) => { client.hook = async (method, params) => {
      if (method === "config/read") {
        const value = syntheticToolMetadata(method) as any;
        value.config.mcp_servers = Object.fromEntries(keys.map((key) => [key, { enabled: index !== 0 ? false : true, http_headers: { Authorization: "SECRET MUST NOT PROPAGATE" } }]));
        return value;
      }
      return client.defaultResponse(method, params);
    }; });
    const run = await f.create();
    expect(f.clients).toHaveLength(2);
    expect(f.clients[0]!.closeCount).toBe(1);
    expect(f.clients[0]!.calls.some((call) => call.method === "thread/start")).toBe(false);
    // Empty tables do not clear inherited keys; they still force the rebuild.
    expect(f.clients[0]!.options.args).toContain("mcp_servers={}");
    expect(f.clients[1]!.options.args).toContain('mcp_servers={"synthetic.with.dot"={enabled=false},"quote\\"slash\\\\name"={enabled=false}}');
    const created = f.clients[1]!.calls.find((call) => call.method === "thread/start")!.params;
    expect((created.config as any).mcp_servers).toEqual(Object.fromEntries(keys.map((key) => [key, { enabled: false }])));
    expect(JSON.stringify([f.events, f.diagnostics, f.clients[1]!.options.args, created])).not.toContain("SECRET");
    await run.close(); await f.adapter.shutdown();
    expect(f.clients.map((client) => client.closeCount)).toEqual([1, 1]);
  });

  it("refuses a newly enabled server in the final client without another rebuild or any Session", async () => {
    const f = fixture((client, index) => { client.hook = async (method, params) => {
      if (method === "config/read") {
        const value = syntheticToolMetadata(method) as any;
        value.config.mcp_servers = index === 0 ? { known: { enabled: true } } : { known: { enabled: false }, unexpected: { enabled: true } };
        return value;
      }
      return client.defaultResponse(method, params);
    }; });
    await expect(f.create()).rejects.toThrow(/MCP configuration/);
    expect(f.clients).toHaveLength(2);
    expect(f.clients.every((client) => !client.calls.some((call) => call.method === "thread/start"))).toBe(true);
    expect(f.clients.map((client) => client.closeCount)).toEqual([1, 1]);
    expect(f.events).toEqual([]);
  });

  it.each(["invalid-key", "too-many", "missing-feature", "runtime-feature-on", "managed-hooks", "config-error"])("rejects %s without leaking provider configuration or creating a Session", async (kind) => {
    const f = fixture((client) => { client.hook = async (method, params) => {
      client.options.onStderr?.("SECRET configured header value");
      if (method === "config/read") {
        if (kind === "config-error") throw new Error("SECRET provider config parse error");
        const value = syntheticToolMetadata(method) as any;
        if (kind === "invalid-key") value.config.mcp_servers = { ['unsafe\nkey']: { enabled: true } };
        if (kind === "too-many") value.config.mcp_servers = Object.fromEntries(Array.from({ length: 129 }, (_, i) => [`synthetic-${i}`, { enabled: false }]));
        if (kind === "missing-feature") delete value.config.features.browser_use;
        return value;
      }
      if (method === "experimentalFeature/list" && kind === "runtime-feature-on") return { data: [{ name: "browser_use", enabled: true }], nextCursor: null };
      if (method === "configRequirements/read" && kind === "managed-hooks") return { requirements: { hooks: { marker: "SECRET managed command" } } };
      return client.defaultResponse(method, params);
    }; });
    const failure = await f.create().catch((error: Error) => error);
    expect(failure).toBeInstanceOf(Error);
    expect(String(failure)).toContain("execution refused");
    expect(JSON.stringify([String(failure), f.events, f.diagnostics])).not.toContain("SECRET");
    expect(f.clients).toHaveLength(1); expect(f.clients[0]!.closeCount).toBe(1);
    expect(f.clients[0]!.calls.some((call) => call.method === "thread/start")).toBe(false);
  });

  it.each(["config-missing", "config-disabled", "runtime-missing", "runtime-disabled"])("rejects %s local execution-host metadata before creating or prompting", async (kind) => {
    const f = fixture(client => { client.hook = async (method, params) => {
      const value = client.defaultResponse(method, params) as any;
      if (method === "config/read") {
        if (kind === "config-missing") delete value.config.features.code_mode_host;
        if (kind === "config-disabled") value.config.features.code_mode_host = false;
      }
      if (method === "experimentalFeature/list") {
        if (kind === "runtime-missing") value.data = value.data.filter((feature: any) => feature.name !== "code_mode_host");
        if (kind === "runtime-disabled") value.data.find((feature: any) => feature.name === "code_mode_host").enabled = false;
      }
      return value;
    }; });
    await expect(f.create({ initialPrompt: "must not send" })).rejects.toThrow(/execution refused/);
    expect(f.clients[0]!.calls.some(call => ["thread/start", "turn/start"].includes(call.method))).toBe(false);
    expect(f.clients[0]!.closeCount).toBe(1);
    expect(f.events).toEqual([]);
    await f.adapter.shutdown();
  });

  it("rejects a loaded Session with a connected MCP tool before announcing or sending", async () => {
    const f = fixture((client) => { client.hook = async (method, params) => {
      if (method === "mcpServerStatus/list") return { data: [{ runtimeStatus: "connected", tools: { hidden: {} }, resources: [], resourceTemplates: [] }], nextCursor: null };
      return client.defaultResponse(method, params);
    }; });
    await expect(f.create({ initialPrompt: "must not send" })).rejects.toThrow(/MCP tools/);
    expect(f.events).toEqual([]); expect(f.clients[0]!.calls.at(-1)?.method).toBe("thread/unsubscribe");
    expect(f.clients[0]!.calls.some((call) => call.method === "turn/start")).toBe(false);
  });

  it("fences late tool-scope changes before dispatch and can close without an invented unknown Turn", async () => {
    const f = fixture(); const run = await f.create();
    f.clients[0]!.hook = async (method, params) => {
      if (method === "config/read") throw new Error("SECRET changed configuration");
      return f.clients[0]!.defaultResponse(method, params);
    };
    await expect(run.send("must not dispatch")).rejects.toThrow(/execution refused/);
    await expect(run.send("still fenced")).rejects.toThrow(/fenced/);
    expect(f.clients[0]!.calls.some((call) => call.method === "turn/start")).toBe(false);
    await run.close(); expect(run.descriptor.status).toBe("closed");
  });

  it("closes during the pre-dispatch tool check without ever starting a Turn", async () => {
    const f = fixture(); const run = await f.create(); const entered = deferred<void>(); const result = deferred<unknown>();
    f.clients[0]!.hook = async (method, params) => {
      if (method === "config/read") { entered.resolve(); return await result.promise; }
      return f.clients[0]!.defaultResponse(method, params);
    };
    const sending = run.send("never dispatch"); const failure = expect(sending).rejects.toThrow(/closing/);
    await entered.promise; const closing = run.close(); result.resolve(syntheticToolMetadata("config/read"));
    await Promise.all([failure, closing]);
    expect(f.clients[0]!.calls.some((call) => call.method === "turn/start")).toBe(false);
    expect(run.descriptor.status).toBe("closed");
  });

  it("shutdown during candidate metadata check closes only that candidate without rebuilding", async () => {
    const entered = deferred<void>(); const result = deferred<unknown>();
    const f = fixture((client) => { client.hook = async (method, params) => {
      if (method === "config/read") { entered.resolve(); return await result.promise; }
      return client.defaultResponse(method, params);
    }; });
    const creating = f.create(); const failed = expect(creating).rejects.toThrow(/shutting down/);
    await entered.promise; const closing = f.adapter.shutdown();
    const response = syntheticToolMetadata("config/read") as any; response.config.mcp_servers = { synthetic: { enabled: true } }; result.resolve(response);
    await Promise.all([failed, closing]);
    expect(f.clients).toHaveLength(1); expect(f.clients[0]!.closeCount).toBe(1);
    expect(f.clients[0]!.calls.some((call) => call.method === "thread/start")).toBe(false);
  });
});
