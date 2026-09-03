import { randomUUID } from "node:crypto";

import {
  createBridgeEvent,
  type BridgeEventType,
  type RunDescriptor,
  type SessionsListStoredParams,
  type SessionsReadStoredParams,
  type StoredSession,
  type SessionSnapshot,
  type SessionsPollParams,
  type SessionPollResult,
} from "../protocol.js";
import { pollCodexSession } from "../lib/codex-poll.js";
import { closeDeadline, withinCloseDeadline } from "../lib/close-deadline.js";
import { codexSurface, DEFAULT_SNAPSHOT_LIMIT, normalizeCodexSnapshot } from "../lib/session-snapshot.js";
import {
  JsonlRpcProcess,
  type InboundRpcRequest,
  type RpcNotification,
} from "../lib/jsonl-rpc-client.js";
import type { AgentAdapter, AgentRun, EventSink } from "./types.js";
import { serializeImportedContext } from "./types.js";

type JsonRecord = Record<string, unknown>;

export interface CodexAdapterOptions {
  command?: string;
  env?: NodeJS.ProcessEnv;
  onDiagnostic?: (message: string) => void;
  clientFactory?: (options: ConstructorParameters<typeof JsonlRpcProcess>[0]) => JsonlRpcProcess;
  closeTimeoutMs?: number;
}

interface MappedCodexEvent {
  type: BridgeEventType;
  turnId?: string;
  messageId?: string;
  toolId?: string;
  data: Record<string, unknown>;
}

export class CodexAdapter implements AgentAdapter {
  readonly provider = "codex" as const;
  private client: JsonlRpcProcess | undefined;
  private clientPromise: Promise<JsonlRpcProcess> | undefined;
  private readonly runsByThread = new Map<string, CodexRun>();
  private providerVersion: string | undefined;

  constructor(private readonly options: CodexAdapterOptions = {}) {}

  async listStored(params: SessionsListStoredParams): Promise<StoredSession[]> {
    // No currently verified provider source value uniquely identifies Desktop.
    if (params.surface === "desktop") return [];
    const client = await this.ensureClient();
    const response = await client.request("thread/list", {
      limit: params.limit ?? 100,
      ...(params.cwd === undefined ? {} : { cwd: params.cwd }),
      sortKey: "updated_at",
      sortDirection: "desc",
      useStateDbOnly: true,
      // Omission defaults to CLI/VS Code only. appServer includes Desktop and other clients.
      sourceKinds: params.surface === "cli" ? ["cli", "exec"]
        : params.surface === "vscode" ? ["vscode"]
        : params.surface === "app-server" ? ["appServer"]
        : params.surface === "unknown" ? ["subAgent", "subAgentReview", "subAgentCompact", "subAgentThreadSpawn", "subAgentOther", "unknown"]
        : ["cli", "vscode", "exec", "appServer", "subAgent", "subAgentReview", "subAgentCompact", "subAgentThreadSpawn", "subAgentOther", "unknown"],
    });
    const data = recordArray(isRecord(response) ? response.data : undefined);
    return data.flatMap((thread): StoredSession[] => {
      const sessionId = stringField(thread, "id");
      if (!sessionId) return [];
      const surface = codexSurface(thread.source);
      if (params.surface !== undefined && params.surface !== surface) return [];
      const createdAt = epochSecondsToIso(numberField(thread, "createdAt"));
      const updatedAt = epochSecondsToIso(numberField(thread, "updatedAt"));
      return [{
        provider: "codex",
        sessionId,
        identityKind: "thread.id",
        surface,
        ...(stringField(thread, "name") ?? stringField(thread, "preview")
          ? { title: stringField(thread, "name") ?? stringField(thread, "preview") }
          : {}),
        ...(stringField(thread, "cwd") ? { cwd: stringField(thread, "cwd") } : {}),
        ...(createdAt ? { createdAt } : {}),
        ...(updatedAt ? { updatedAt } : {}),
        metadata: {
          modelProvider: thread.modelProvider,
          status: thread.status,
          cliVersion: thread.cliVersion,
          source: thread.source,
          threadId: sessionId,
          sessionTreeId: thread.sessionId,
        },
      }];
    });
  }

  async readStored(params: SessionsReadStoredParams): Promise<unknown> {
    const client = await this.ensureClient();
    return await client.request("thread/read", {
      threadId: params.sessionId,
      includeTurns: true,
    });
  }

  async snapshot(params: SessionsReadStoredParams): Promise<SessionSnapshot> {
    const client = await this.ensureClient();
    const metadata = await client.request("thread/read", { threadId: params.sessionId, includeTurns: false });
    const thread = isRecord(metadata) && isRecord(metadata.thread) ? metadata.thread : undefined;
    if (!thread || thread.id !== params.sessionId) throw new Error("Codex history returned a different or missing conversation identity");
    let raw = metadata;
    if (thread.historyMode === "paginated") {
      const turns: JsonRecord[] = [];
      const turnIds = new Set<string>();
      const cursors = new Set<string>();
      let cursor: string | undefined;
      let items = 0;
      let truncated = false;
      for (let page = 0; page < 100; page++) {
        const result = await client.request("thread/turns/list", {
          threadId: params.sessionId, limit: 50, sortDirection: "asc", itemsView: "full",
          ...(cursor === undefined ? {} : { cursor }),
        });
        if (!isRecord(result) || !Array.isArray(result.data)) throw new Error("Codex returned incompatible paginated history");
        for (const value of recordArray(result.data)) {
          const id = stringField(value, "id");
          if (id && turnIds.has(id)) { truncated = true; continue; }
          if (id) turnIds.add(id);
          turns.push(value);
          items += Array.isArray(value.items) ? value.items.length : 1;
        }
        cursor = stringField(result, "nextCursor");
        if (!cursor) break;
        if (cursors.has(cursor) || items >= (params.limit ?? DEFAULT_SNAPSHOT_LIMIT) || page === 99) { truncated = true; break; }
        cursors.add(cursor);
      }
      raw = { thread: { ...thread, turns, turnsTruncated: truncated } };
    } else {
      raw = await this.readStored(params);
    }
    return normalizeCodexSnapshot(raw, params.sessionId, {
      ...(params.limit === undefined ? {} : { limit: params.limit }),
      ...(this.providerVersion === undefined ? {} : { providerVersion: this.providerVersion }),
    });
  }

  async poll(params: SessionsPollParams): Promise<SessionPollResult> {
    return await pollCodexSession(async (method, input) => {
      const client = await this.ensureClient();
      return await client.request(method, input);
    }, params, () => this.providerVersion ?? "unknown");
  }

  async createRun(
    params: Parameters<AgentAdapter["createRun"]>[0],
    emit: EventSink,
  ): Promise<AgentRun> {
    const client = await this.ensureClient();
    const common = {
      ...(params.model === undefined ? {} : { model: params.model }),
      cwd: params.worktree,
      runtimeWorkspaceRoots: [params.worktree],
      approvalPolicy: "never",
      sandbox: "workspace-write",
      ephemeral: false,
    };
    const response = params.forkFromSessionId
      ? await client.request("thread/fork", {
          threadId: params.forkFromSessionId,
          ...common,
          excludeTurns: true,
          deferGoalContinuation: true,
        })
      : await client.request("thread/start", common);
    const thread = isRecord(response) && isRecord(response.thread) ? response.thread : undefined;
    const threadId = thread ? stringField(thread, "id") : undefined;
    if (!threadId) throw new Error("Codex thread/start returned no thread id");

    const run = new CodexRun({
      runId: params.runId,
      threadId,
      worktree: params.worktree,
      networkEnabled: Boolean(params.networkEnabled),
      ...(params.model === undefined ? {} : { model: params.model }),
      client,
      emit,
      closeTimeoutMs: this.options.closeTimeoutMs,
      onClosed: () => this.runsByThread.delete(threadId),
    });
    this.runsByThread.set(threadId, run);
    run.announce();
    if (params.initialPrompt) void run.send(params.initialPrompt).catch((error) => run.reportError(error));
    return run;
  }

  async shutdown(): Promise<void> {
    for (const run of [...this.runsByThread.values()]) await run.close();
    this.runsByThread.clear();
    await this.client?.close();
    this.client = undefined;
    this.clientPromise = undefined;
  }

  private async ensureClient(): Promise<JsonlRpcProcess> {
    if (this.client) return this.client;
    if (this.clientPromise) return await this.clientPromise;
    this.clientPromise = this.startClient();
    try {
      this.client = await this.clientPromise;
      return this.client;
    } finally {
      this.clientPromise = undefined;
    }
  }

  private async startClient(): Promise<JsonlRpcProcess> {
    const factory = this.options.clientFactory ?? ((options) => new JsonlRpcProcess(options));
    let client!: JsonlRpcProcess;
    client = factory({
      command: this.options.command ?? process.env.TEAMCROSS_CODEX_BIN ?? "codex",
      args: ["app-server", "--stdio"],
      env: this.options.env ?? process.env,
      requestTimeoutMs: 45_000,
      onNotification: (notification) => this.routeNotification(notification),
      onRequest: (request) => this.routeRequest(client, request),
      onStderr: (line) => this.options.onDiagnostic?.(`[codex] ${line}`),
      onExit: (error) => {
        for (const run of this.runsByThread.values()) run.reportError(error);
        this.client = undefined;
      },
    } as ConstructorParameters<typeof JsonlRpcProcess>[0]);
    client.start();
    const initialize = await client.request("initialize", {
      clientInfo: { name: "teamcross", title: "Team Cross", version: "0.1.0" },
      capabilities: {
        experimentalApi: true,
        requestAttestation: false,
        optOutNotificationMethods: [],
      },
    });
    if (!isRecord(initialize) || typeof initialize.userAgent !== "string") {
      await client.close();
      throw new Error("Codex app-server initialize response is incompatible");
    }
    client.notify("initialized");
    this.providerVersion = initialize.userAgent;
    // Probe only the explicitly selected history operation; initialization must
    // not enumerate unrelated personal Sessions as a compatibility check.
    this.options.onDiagnostic?.(`[codex] initialized ${initialize.userAgent}`);
    return client;
  }

  private routeNotification(notification: RpcNotification): void {
    const params = isRecord(notification.params) ? notification.params : {};
    const threadId = stringField(params, "threadId")
      ?? (isRecord(params.thread) ? stringField(params.thread, "id") : undefined);
    if (!threadId) return;
    const run = this.runsByThread.get(threadId);
    if (!run) return;
    run.onNotification(notification.method, params);
  }

  private routeRequest(client: JsonlRpcProcess, request: InboundRpcRequest): void {
    const params = isRecord(request.params) ? request.params : {};
    const threadId = stringField(params, "threadId");
    const run = threadId ? this.runsByThread.get(threadId) : undefined;
    if (request.method === "item/tool/requestUserInput" && run) {
      run.onInputRequest(request.id, params);
      return;
    }
    if (
      request.method === "item/commandExecution/requestApproval"
      || request.method === "item/fileChange/requestApproval"
      || request.method === "execCommandApproval"
      || request.method === "applyPatchApproval"
    ) {
      // approvalPolicy=never should prevent this path; fail closed if an older server asks anyway.
      client.respond(request.id, { decision: "decline" });
      return;
    }
    client.respondError(request.id, -32601, `Team Cross does not implement ${request.method}`);
  }
}

interface CodexRunOptions {
  runId: string;
  threadId: string;
  worktree: string;
  networkEnabled: boolean;
  model?: string;
  client: JsonlRpcProcess;
  emit: EventSink;
  onClosed: () => void;
  closeTimeoutMs?: number;
}

class CodexRun implements AgentRun {
  readonly descriptor: RunDescriptor;
  private activeTurnId: string | undefined;
  private readonly activeTurns = new Set<string>();
  private readonly terminalTurns = new Set<string>();
  private readonly terminalWaiters = new Map<string, () => void>();
  private pendingStart: Promise<{ turnId: string }> | undefined;
  private readonly pendingSteers = new Set<Promise<unknown>>();
  private startUncertain = false;
  private closing = false;
  private closeAttempt: Promise<void> | undefined;
  private importedContext: string[] = [];
  private readonly pendingInputs = new Map<string, {
    rpcId: string | number;
    questionIds: string[];
  }>();

  constructor(private readonly options: CodexRunOptions) {
    this.descriptor = {
      runId: options.runId,
      provider: "codex",
      sessionId: options.threadId,
      worktree: options.worktree,
      networkEnabled: options.networkEnabled,
      ...(options.model === undefined ? {} : { model: options.model }),
      status: "starting",
      capabilities: { send: true, steer: true, interrupt: true, inputResponse: true },
    };
  }

  announce(): void {
    this.descriptor.status = "idle";
    this.emit("run.started", {
      data: {
        sessionId: this.descriptor.sessionId,
        transport: "stdio",
        approvalPolicy: "never",
        sandbox: "workspaceWrite",
      },
    });
    this.status("idle");
  }

  async importContext(source: Record<string, unknown>, context: unknown): Promise<void> {
    this.assertInputOpen();
    this.importedContext.push(serializeImportedContext(source, context));
  }

  async send(message: string): Promise<{ turnId: string }> {
    this.assertInputOpen();
    if (this.activeTurns.size || this.pendingStart || this.startUncertain) throw new Error("a Codex turn is active or unconfirmed; use runs.steer");
    const pending = this.startTurn(message);
    this.pendingStart = pending;
    try {
      return await pending;
    } catch (error) {
      // A failed/timeout response does not prove turn/start was never accepted.
      this.startUncertain = true;
      throw error;
    } finally {
      if (this.pendingStart === pending) this.pendingStart = undefined;
    }
  }

  private async startTurn(message: string): Promise<{ turnId: string }> {
    const prompt = this.withImportedContext(message);
    const response = await this.options.client.request("turn/start", {
      threadId: this.options.threadId,
      input: [{ type: "text", text: prompt, text_elements: [] }],
      cwd: this.options.worktree,
      runtimeWorkspaceRoots: [this.options.worktree],
      approvalPolicy: "never",
      sandboxPolicy: {
        type: "workspaceWrite",
        writableRoots: [this.options.worktree],
        networkAccess: this.options.networkEnabled,
        excludeTmpdirEnvVar: true,
        excludeSlashTmp: true,
      },
    });
    const turn = isRecord(response) && isRecord(response.turn) ? response.turn : undefined;
    const turnId = turn ? stringField(turn, "id") : undefined;
    if (!turnId) throw new Error("Codex turn/start returned no turn id");
    // Notifications can arrive before the turn/start response.
    if (!this.terminalTurns.has(turnId)) {
      this.activeTurns.add(turnId);
      this.activeTurnId = turnId;
      this.descriptor.status = "running";
    }
    return { turnId };
  }

  async steer(message: string): Promise<{ turnId: string }> {
    this.assertInputOpen();
    const turnId = this.activeTurnId;
    if (!turnId) throw new Error("no active Codex turn to steer");
    const pending = this.options.client.request("turn/steer", {
      threadId: this.options.threadId,
      expectedTurnId: turnId,
      input: [{ type: "text", text: message, text_elements: [] }],
    });
    this.pendingSteers.add(pending);
    try {
      await pending;
    } finally {
      this.pendingSteers.delete(pending);
    }
    return { turnId };
  }

  async interrupt(): Promise<void> {
    this.assertInputOpen();
    const turnId = this.activeTurnId;
    if (!turnId) return;
    await this.options.client.request("turn/interrupt", {
      threadId: this.options.threadId,
      turnId,
    });
  }

  async respondInput(inputRequestId: string, response: unknown): Promise<void> {
    this.assertInputOpen();
    const pending = this.pendingInputs.get(inputRequestId);
    if (!pending) throw new Error(`unknown Codex input request: ${inputRequestId}`);
    this.pendingInputs.delete(inputRequestId);
    this.options.client.respond(pending.rpcId, normalizeInputResponse(response, pending.questionIds));
    this.emit("input.resolved", { inputRequestId, data: { response } });
  }

  async close(): Promise<void> {
    if (this.descriptor.status === "closed") return;
    if (this.closeAttempt) return await this.closeAttempt;
    // This fence survives every failure; a retry may only finish closing.
    this.closing = true;
    this.descriptor.capabilities = { send: false, steer: false, interrupt: false, inputResponse: false };
    const attempt = this.finishClose();
    this.closeAttempt = attempt;
    try {
      await attempt;
    } finally {
      if (this.closeAttempt === attempt) this.closeAttempt = undefined;
    }
  }

  private async finishClose(): Promise<void> {
    const deadline = closeDeadline(this.options.closeTimeoutMs);
    for (const [id, pending] of this.pendingInputs) {
      this.options.client.respondError(pending.rpcId, -32000, "Team Cross run is closing");
      this.pendingInputs.delete(id);
    }
    if (this.pendingStart) await withinCloseDeadline(this.pendingStart, deadline, "turn/start identity");
    if (this.startUncertain) throw new Error("Codex turn/start outcome is unknown; termination is unconfirmed");
    for (const pending of this.pendingSteers) await withinCloseDeadline(pending, deadline, "in-flight turn/steer");
    while (this.activeTurns.size) {
      const turnId = this.activeTurns.values().next().value!;
      // Register before interrupt: exact terminal notification may precede its ACK.
      const terminal = new Promise<void>((resolve) => this.terminalWaiters.set(turnId, resolve));
      try {
        await withinCloseDeadline(this.options.client.request("turn/interrupt", {
          threadId: this.options.threadId, turnId,
        }, Math.max(1, deadline - Date.now())), deadline, `interrupt ACK for ${turnId}`);
        if (this.activeTurns.has(turnId)) await withinCloseDeadline(terminal, deadline, `terminal turn/completed for ${turnId}`);
      } finally {
        this.terminalWaiters.delete(turnId);
      }
    }
    this.emit("run.closed", { data: {} });
    this.descriptor.status = "closed";
    this.options.onClosed();
  }

  onNotification(method: string, params: JsonRecord): void {
    if (this.descriptor.status === "closed") return;
    const turnId = stringField(params, "turnId")
      ?? (isRecord(params.turn) ? stringField(params.turn, "id") : undefined);
    if (method === "turn/started" && turnId) {
      if (this.terminalTurns.has(turnId)) return;
      this.activeTurns.add(turnId);
      this.activeTurnId = turnId;
      this.descriptor.status = "running";
    } else if (method === "turn/completed") {
      const status = isRecord(params.turn) ? params.turn.status : undefined;
      if (!turnId || (status !== "completed" && status !== "interrupted" && status !== "failed")) return;
      this.terminalTurns.add(turnId);
      if (this.terminalTurns.size > 64) this.terminalTurns.delete(this.terminalTurns.values().next().value!);
      this.activeTurns.delete(turnId);
      this.terminalWaiters.get(turnId)?.();
      if (turnId === this.activeTurnId) {
        this.activeTurnId = this.activeTurns.values().next().value;
        this.descriptor.status = this.activeTurnId ? "running" : "idle";
      }
    }
    for (const mapped of mapCodexNotification(method, params)) {
      this.emit(mapped.type, mapped);
    }
    if (method === "turn/completed" && this.activeTurns.size === 0) this.status("idle", turnId);
  }

  onInputRequest(rpcId: string | number, params: JsonRecord): void {
    if (this.closing || this.descriptor.status === "closed") {
      this.options.client.respondError(rpcId, -32000, "Team Cross run is closing");
      return;
    }
    const inputRequestId = `codex-input-${String(rpcId)}-${randomUUID()}`;
    const questions = recordArray(params.questions);
    this.pendingInputs.set(inputRequestId, {
      rpcId,
      questionIds: questions.flatMap((question) => {
        const id = stringField(question, "id");
        return id ? [id] : [];
      }),
    });
    this.emit("input.requested", {
      turnId: stringField(params, "turnId"),
      inputRequestId,
      data: {
        questions,
        blocking: params.isBlocking === true,
        itemId: params.itemId,
      },
    });
  }

  reportError(error: unknown): void {
    if (this.descriptor.status === "closed") return;
    this.descriptor.status = "error";
    this.emit("run.error", { data: { message: errorMessage(error) } });
  }

  private withImportedContext(message: string): string {
    if (this.importedContext.length === 0) return message;
    return `${this.importedContext.splice(0).join("\n\n")}\n\n<user-request>\n${message}\n</user-request>`;
  }

  private assertInputOpen(): void {
    if (this.closing || this.descriptor.status === "closed") throw new Error("run is closing or closed; input is fenced");
  }

  private status(status: RunDescriptor["status"], turnId?: string): void {
    this.emit("run.status", {
      ...(turnId === undefined ? {} : { turnId }),
      data: { status },
    });
  }

  private emit(
    type: BridgeEventType,
    fields: Omit<Partial<ReturnType<typeof createBridgeEvent>>, "type" | "provider" | "runId" | "timestamp">,
  ): void {
    this.options.emit(createBridgeEvent("codex", this.descriptor.runId, type, fields));
  }
}

export function mapCodexNotification(method: string, params: JsonRecord): MappedCodexEvent[] {
  const turnId = stringField(params, "turnId")
    ?? (isRecord(params.turn) ? stringField(params.turn, "id") : undefined);
  const base = turnId ? { turnId } : {};
  if (method === "turn/started") {
    return [{ type: "turn.started", ...base, data: { turn: params.turn } }];
  }
  if (method === "turn/completed") {
    const turn = isRecord(params.turn) ? params.turn : {};
    const events: MappedCodexEvent[] = [{
      type: "turn.completed",
      ...base,
      data: { status: turn.status, error: turn.error },
    }];
    if (turn.status === "failed") {
      events.unshift({
        type: "run.error",
        ...base,
        data: { message: errorMessage(turn.error ?? "Codex turn failed") },
      });
    }
    return events;
  }
  if (method === "item/agentMessage/delta") {
    return [{
      type: "message.delta",
      ...base,
      ...(stringField(params, "itemId") ? { messageId: stringField(params, "itemId") } : {}),
      data: { delta: params.delta },
    }];
  }
  if (method === "item/started" || method === "item/completed") {
    const item = isRecord(params.item) ? params.item : {};
    const itemType = stringField(item, "type") ?? "unknown";
    const itemId = stringField(item, "id");
    if (itemType === "agentMessage") {
      if (method === "item/started") return [];
      return [{
        type: "message.completed",
        ...base,
        ...(itemId ? { messageId: itemId } : {}),
        data: { text: item.text, phase: item.phase },
      }];
    }
    const event: MappedCodexEvent = {
      type: method === "item/started" ? "tool.started" : "tool.completed",
      ...base,
      ...(itemId ? { toolId: itemId } : {}),
      data: { name: codexToolName(itemType, item), item },
    };
    if (method === "item/completed" && itemType === "fileChange") {
      return [event, {
        type: "file.changed",
        ...base,
        data: { changes: item.changes, status: item.status },
      }];
    }
    return [event];
  }
  if (method === "item/commandExecution/outputDelta") {
    return [{
      type: "run.status",
      ...base,
      ...(stringField(params, "itemId") ? { toolId: stringField(params, "itemId") } : {}),
      data: { status: "tool-output", delta: params.delta },
    }];
  }
  if (method === "item/fileChange/patchUpdated" || method === "fs/changed") {
    return [{ type: "file.changed", ...base, data: { ...params } }];
  }
  if (method === "thread/status/changed") {
    return [{ type: "run.status", ...base, data: { status: params.status } }];
  }
  if (method === "error") {
    return [{ type: "run.error", ...base, data: { ...params } }];
  }
  return [];
}

function normalizeInputResponse(response: unknown, questionIds: string[]): unknown {
  if (isRecord(response) && isRecord(response.answers)) return response;
  const values = Array.isArray(response)
    ? response.map(String)
    : [typeof response === "string" ? response : JSON.stringify(response)];
  return {
    answers: Object.fromEntries(questionIds.map((id) => [id, { answers: values }])),
  };
}

function codexToolName(itemType: string, item: JsonRecord): string {
  if (itemType === "commandExecution") return "Bash";
  if (itemType === "fileChange") return "FileChange";
  if (itemType === "mcpToolCall") return `${String(item.server)}.${String(item.tool)}`;
  if (itemType === "dynamicToolCall") return String(item.tool ?? "DynamicTool");
  return itemType;
}

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function recordArray(value: unknown): JsonRecord[] {
  return Array.isArray(value) ? value.filter(isRecord) : [];
}

function stringField(record: JsonRecord, key: string): string | undefined {
  const value = record[key];
  return typeof value === "string" && value !== "" ? value : undefined;
}

function numberField(record: JsonRecord, key: string): number | undefined {
  const value = record[key];
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function epochSecondsToIso(value: number | undefined): string | undefined {
  return value === undefined ? undefined : new Date(value * 1000).toISOString();
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (isRecord(error) && typeof error.message === "string") return error.message;
  return typeof error === "string" ? error : JSON.stringify(error);
}
