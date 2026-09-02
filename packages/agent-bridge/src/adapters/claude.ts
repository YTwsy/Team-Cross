import { randomUUID } from "node:crypto";

import { AsyncQueue } from "../lib/async-queue.js";
import {
  createBridgeEvent,
  type BridgeEventType,
  type RunDescriptor,
  type SessionsListStoredParams,
  type SessionsReadStoredParams,
  type StoredSession,
  type SessionSnapshot,
} from "../protocol.js";
import { DEFAULT_SNAPSHOT_LIMIT, normalizeClaudeSnapshot } from "../lib/session-snapshot.js";
import type { AgentAdapter, AgentRun, EventSink } from "./types.js";
import { serializeImportedContext } from "./types.js";

type JsonRecord = Record<string, unknown>;

interface ClaudeQuery extends AsyncIterable<unknown> {
  interrupt(): Promise<void>;
  close(): void;
}

interface ClaudeSdk {
  query(input: { prompt: AsyncIterable<unknown>; options: JsonRecord }): ClaudeQuery;
  listSessions(options?: JsonRecord): Promise<unknown[]>;
  getSessionMessages(sessionId: string, options?: JsonRecord): Promise<unknown[]>;
  getSessionInfo?(sessionId: string, options?: JsonRecord): Promise<unknown>;
}

export interface ClaudeAdapterOptions {
  sdkLoader?: () => Promise<ClaudeSdk>;
  env?: NodeJS.ProcessEnv;
  onDiagnostic?: (message: string) => void;
}

export class ClaudeAdapter implements AgentAdapter {
  readonly provider = "claude" as const;
  private readonly runs = new Set<ClaudeRun>();
  private sdkPromise: Promise<ClaudeSdk> | undefined;

  constructor(private readonly options: ClaudeAdapterOptions = {}) {}

  async listStored(params: SessionsListStoredParams): Promise<StoredSession[]> {
    // SDK history metadata does not establish whether a session originated in CLI or Desktop.
    if (params.surface !== undefined && params.surface !== "unknown") return [];
    const sdk = await this.loadSdk();
    const sessions = await sdk.listSessions({
      ...(params.cwd === undefined ? {} : { dir: params.cwd }),
      limit: params.limit ?? 100,
      includeWorktrees: true,
    });
    return sessions.flatMap((value): StoredSession[] => {
      if (!isRecord(value)) return [];
      const sessionId = stringField(value, "sessionId");
      if (!sessionId) return [];
      return [{
        provider: "claude",
        sessionId,
        identityKind: "sessionId",
        surface: "unknown",
        ...(stringField(value, "summary") ? { title: stringField(value, "summary") } : {}),
        ...(stringField(value, "cwd") ? { cwd: stringField(value, "cwd") } : {}),
        ...(epochMillisToIso(numberField(value, "createdAt"))
          ? { createdAt: epochMillisToIso(numberField(value, "createdAt")) }
          : {}),
        ...(epochMillisToIso(numberField(value, "lastModified"))
          ? { updatedAt: epochMillisToIso(numberField(value, "lastModified")) }
          : {}),
        metadata: {
          customTitle: value.customTitle,
          firstPrompt: value.firstPrompt,
          gitBranch: value.gitBranch,
          tag: value.tag,
          fileSize: value.fileSize,
        },
      }];
    });
  }

  async readStored(params: SessionsReadStoredParams): Promise<unknown> {
    const sdk = await this.loadSdk();
    const messages = await sdk.getSessionMessages(params.sessionId, {
      ...(params.cwd === undefined ? {} : { dir: params.cwd }),
      ...(params.limit === undefined ? {} : { limit: params.limit }),
    });
    return { sessionId: params.sessionId, messages };
  }

  async snapshot(params: SessionsReadStoredParams): Promise<SessionSnapshot> {
    const sdk = await this.loadSdk();
    const limit = params.limit ?? DEFAULT_SNAPSHOT_LIMIT;
    const directory = params.cwd === undefined ? {} : { dir: params.cwd };
    // One extra message detects truncation without enumerating unrelated sessions or starting a query.
    const messages = await sdk.getSessionMessages(params.sessionId, {
      ...directory, limit: limit + 1, includeSystemMessages: true,
    });
    const metadata = await sdk.getSessionInfo?.(params.sessionId, directory);
    return normalizeClaudeSnapshot(messages.slice(0, limit), params.sessionId, {
      limit, metadata, providerTruncated: messages.length > limit,
      ...(params.cwd === undefined ? {} : { cwd: params.cwd }),
    });
  }

  async createRun(
    params: Parameters<AgentAdapter["createRun"]>[0],
    emit: EventSink,
  ): Promise<AgentRun> {
    assertClaudeCredentials(this.options.env ?? process.env);
    const sdk = await this.loadSdk();
    const run = new ClaudeRun({
      runId: params.runId,
      worktree: params.worktree,
      networkEnabled: Boolean(params.networkEnabled),
      ...(params.model === undefined ? {} : { model: params.model }),
      sdk,
      env: this.options.env ?? process.env,
      emit,
      onClosed: () => this.runs.delete(run),
      onDiagnostic: this.options.onDiagnostic,
    });
    this.runs.add(run);
    run.start();
    if (params.initialPrompt) void run.send(params.initialPrompt);
    return run;
  }

  async shutdown(): Promise<void> {
    for (const run of [...this.runs]) await run.close();
    this.runs.clear();
  }

  private async loadSdk(): Promise<ClaudeSdk> {
    this.sdkPromise ??= this.options.sdkLoader?.() ?? importClaudeSdk();
    return await this.sdkPromise;
  }
}

interface ClaudeRunOptions {
  runId: string;
  worktree: string;
  networkEnabled: boolean;
  model?: string;
  sdk: ClaudeSdk;
  env: NodeJS.ProcessEnv;
  emit: EventSink;
  onClosed: () => void;
  onDiagnostic?: (message: string) => void;
}

class ClaudeRun implements AgentRun {
  readonly descriptor: RunDescriptor;
  private readonly input = new AsyncQueue<unknown>();
  private query: ClaudeQuery | undefined;
  private receiveLoop: Promise<void> | undefined;
  private activeTurnId: string | undefined;
  private importedContext: string[] = [];
  private readonly tools = new Map<number, { id: string; name: string; input: string }>();

  constructor(private readonly options: ClaudeRunOptions) {
    this.descriptor = {
      runId: options.runId,
      provider: "claude",
      // No native identity exists until the SDK reports system/init.
      sessionId: "",
      worktree: options.worktree,
      networkEnabled: options.networkEnabled,
      ...(options.model === undefined ? {} : { model: options.model }),
      status: "starting",
      capabilities: { send: true, steer: true, interrupt: true, inputResponse: false },
    };
  }

  start(): void {
    const query = this.options.sdk.query({
      prompt: this.input,
      options: this.buildSdkOptions(),
    });
    this.query = query;
    this.emit("run.started", {
      data: {
        sessionId: this.descriptor.sessionId,
        streamingInput: true,
        permissionMode: "dontAsk",
        sandbox: "required",
      },
    });
    this.status("starting");
    this.receiveLoop = this.receive(query);
  }

  async importContext(source: Record<string, unknown>, context: unknown): Promise<void> {
    this.importedContext.push(serializeImportedContext(source, context));
  }

  async send(message: string): Promise<{ turnId: string }> {
    if (this.descriptor.status === "closed") throw new Error("run is closed");
    if (this.activeTurnId) throw new Error("a Claude turn is active; use runs.steer");
    const turnId = randomUUID();
    const prompt = this.withImportedContext(message);
    this.activeTurnId = turnId;
    this.descriptor.status = "running";
    this.emit("turn.started", { turnId, data: { trigger: "send" } });
    this.status("running", turnId);
    this.input.push({
      type: "user",
      message: { role: "user", content: prompt },
      parent_tool_use_id: null,
    });
    return { turnId };
  }

  async steer(message: string): Promise<{ turnId: string }> {
    await this.interrupt();
    return await this.send(message);
  }

  async interrupt(): Promise<void> {
    const turnId = this.activeTurnId;
    if (!turnId || !this.query) return;
    await this.query.interrupt();
    if (this.activeTurnId === turnId) {
      this.activeTurnId = undefined;
      this.emit("turn.completed", { turnId, data: { status: "interrupted" } });
      this.descriptor.status = "idle";
      this.status("idle", turnId);
    }
  }

  async respondInput(): Promise<void> {
    throw new Error(
      "Claude runs use permissionMode=dontAsk; answer clarifications with runs.send or runs.steer",
    );
  }

  async close(): Promise<void> {
    if (this.descriptor.status === "closed") return;
    await this.interrupt().catch(() => undefined);
    this.descriptor.status = "closed";
    this.input.close();
    this.query?.close();
    this.query = undefined;
    await this.receiveLoop?.catch(() => undefined);
    this.emit("run.closed", { data: {} });
    this.options.onClosed();
  }

  private buildSdkOptions(): JsonRecord {
    const inheritedEnv: NodeJS.ProcessEnv = {
      ...this.options.env,
      CLAUDE_AGENT_SDK_CLIENT_APP: "teamcross/0.1.0",
    };
    const availableTools = ["Read", "Glob", "Grep", "Edit", "Write", "Bash"];
    if (this.options.networkEnabled) availableTools.push("WebFetch");

    const network = this.options.networkEnabled
      ? {
          allowedDomains: ["*"],
          deniedDomains: [],
          strictAllowlist: true,
          allowLocalBinding: true,
        }
      : {
          allowedDomains: [],
          deniedDomains: ["*"],
          strictAllowlist: true,
          allowLocalBinding: false,
        };

    const sandbox = {
      enabled: true,
      failIfUnavailable: true,
      autoAllowBashIfSandboxed: true,
      allowUnsandboxedCommands: false,
      network,
    };

    const permissionAllow = [
      "Read(./**)",
      "Glob",
      "Grep",
      "Edit(./**)",
      "Write(./**)",
      "Bash",
    ];
    if (this.options.networkEnabled) permissionAllow.push("WebFetch(domain:*)");

    return {
      cwd: this.options.worktree,
      ...(this.options.model === undefined ? {} : { model: this.options.model }),
      permissionMode: "dontAsk",
      tools: availableTools,
      allowedTools: permissionAllow,
      settingSources: [],
      includePartialMessages: true,
      persistSession: true,
      env: inheritedEnv,
      sandbox,
      settings: {
        permissions: { defaultMode: "dontAsk", allow: permissionAllow, ask: [] },
        sandbox: {
          ...sandbox,
          filesystem: { allowWrite: [this.options.worktree] },
          credentials: {
            envVars: [
              { name: "ANTHROPIC_API_KEY", mode: "deny" },
              { name: "ANTHROPIC_AUTH_TOKEN", mode: "deny" },
              { name: "CLAUDE_CODE_OAUTH_TOKEN", mode: "deny" },
            ],
          },
        },
      },
      // Hooks run in this bridge process. Bash commands lose model credentials before execution.
      hooks: {
        PreToolUse: [{
          matcher: "Bash",
          hooks: [async (hookInput: unknown) => sanitizeBashHook(hookInput)],
        }],
        PostToolUse: [{
          matcher: "Edit|Write",
          hooks: [async (hookInput: unknown) => {
            this.onFileToolHook(hookInput);
            return {};
          }],
        }],
      },
    };
  }

  private async receive(query: ClaudeQuery): Promise<void> {
    try {
      for await (const message of query) this.onMessage(message);
      if (this.descriptor.status !== "closed") {
        throw new Error("Claude streaming session ended unexpectedly");
      }
    } catch (error) {
      if (this.descriptor.status === "closed") return;
      this.descriptor.status = "error";
      this.emit("run.error", { data: { message: errorMessage(error) } });
      this.options.onDiagnostic?.(`[claude] ${errorMessage(error)}`);
    }
  }

  private onMessage(message: unknown): void {
    if (!isRecord(message)) return;
    const sessionId = stringField(message, "session_id");
    if (message.type === "system" && message.subtype === "init" && sessionId) {
      if (this.descriptor.sessionId && this.descriptor.sessionId !== sessionId) {
        throw new Error("Claude reported a different native Session identity");
      }
      this.descriptor.sessionId = sessionId;
    }
    const turnId = this.activeTurnId;
    for (const mapped of mapClaudeMessage(message, turnId, this.tools)) {
      this.emit(mapped.type, mapped);
    }
    if (message.type === "system" && message.subtype === "init") {
      this.descriptor.status = this.activeTurnId ? "running" : "idle";
      this.status(this.descriptor.status, this.activeTurnId);
    }
    if (message.type === "result" && turnId) {
      this.activeTurnId = undefined;
      if (this.descriptor.status !== "closed") {
        this.descriptor.status = message.subtype === "success" ? "idle" : "error";
        this.status(this.descriptor.status, turnId);
      }
    }
  }

  private onFileToolHook(hookInput: unknown): void {
    if (!isRecord(hookInput)) return;
    const input = isRecord(hookInput.tool_input) ? hookInput.tool_input : {};
    const path = stringField(input, "file_path") ?? stringField(input, "path");
    this.emit("file.changed", {
      ...(this.activeTurnId ? { turnId: this.activeTurnId } : {}),
      data: {
        ...(path ? { path } : {}),
        kind: hookInput.tool_name === "Edit" ? "edit" : "write",
      },
    });
  }

  private withImportedContext(message: string): string {
    if (this.importedContext.length === 0) return message;
    return `${this.importedContext.splice(0).join("\n\n")}\n\n<user-request>\n${message}\n</user-request>`;
  }

  private status(status: RunDescriptor["status"], turnId?: string): void {
    this.emit("run.status", {
      ...(turnId === undefined ? {} : { turnId }),
      data: { status, sessionId: this.descriptor.sessionId },
    });
  }

  private emit(
    type: BridgeEventType,
    fields: Omit<Partial<ReturnType<typeof createBridgeEvent>>, "type" | "provider" | "runId" | "timestamp">,
  ): void {
    this.options.emit(createBridgeEvent("claude", this.descriptor.runId, type, fields));
  }
}

interface MappedClaudeEvent {
  type: BridgeEventType;
  turnId?: string;
  messageId?: string;
  toolId?: string;
  data: Record<string, unknown>;
}

export function mapClaudeMessage(
  message: JsonRecord,
  turnId: string | undefined,
  tools: Map<number, { id: string; name: string; input: string }> = new Map(),
): MappedClaudeEvent[] {
  const base = turnId ? { turnId } : {};
  if (message.type === "stream_event" && isRecord(message.event)) {
    const event = message.event;
    if (event.type === "content_block_delta" && isRecord(event.delta)) {
      if (event.delta.type === "text_delta" && typeof event.delta.text === "string") {
        return [{
          type: "message.delta",
          ...base,
          ...(stringField(message, "uuid") ? { messageId: stringField(message, "uuid") } : {}),
          data: { delta: event.delta.text },
        }];
      }
      if (event.delta.type === "input_json_delta" && typeof event.delta.partial_json === "string") {
        const index = numberField(event, "index");
        if (index !== undefined) {
          const tool = tools.get(index);
          if (tool) tool.input += event.delta.partial_json;
        }
      }
      return [];
    }
    if (event.type === "content_block_start" && isRecord(event.content_block)) {
      const block = event.content_block;
      if (block.type !== "tool_use") return [];
      const index = numberField(event, "index") ?? tools.size;
      const tool = {
        id: stringField(block, "id") ?? `claude-tool-${index}-${randomUUID()}`,
        name: stringField(block, "name") ?? "Tool",
        input: "",
      };
      tools.set(index, tool);
      return [{
        type: "tool.started",
        ...base,
        toolId: tool.id,
        data: { name: tool.name, input: block.input },
      }];
    }
    if (event.type === "content_block_stop") {
      const index = numberField(event, "index");
      if (index === undefined) return [];
      const tool = tools.get(index);
      if (!tool) return [];
      tools.delete(index);
      return [{
        type: "tool.completed",
        ...base,
        toolId: tool.id,
        data: { name: tool.name, inputJson: tool.input },
      }];
    }
    return [];
  }
  if (message.type === "assistant" && isRecord(message.message)) {
    const content = Array.isArray(message.message.content) ? message.message.content : [];
    const text = content
      .filter(isRecord)
      .filter((block) => block.type === "text" && typeof block.text === "string")
      .map((block) => String(block.text))
      .join("\n");
    return text === "" ? [] : [{
      type: "message.completed",
      ...base,
      ...(stringField(message, "uuid") ? { messageId: stringField(message, "uuid") } : {}),
      data: { text },
    }];
  }
  if (message.type === "result") {
    const success = message.subtype === "success";
    return [
      ...(success ? [] : [{
        type: "run.error" as const,
        ...base,
        data: { message: errorMessage(message.result ?? message.subtype) },
      }]),
      {
        type: "turn.completed",
        ...base,
        data: {
          status: success ? "completed" : "failed",
          result: message.result,
          usage: message.usage,
          costUsd: message.total_cost_usd,
        },
      },
    ];
  }
  if (message.type === "system" && message.subtype === "init") {
    return [{
      type: "run.status",
      ...base,
      data: { status: "ready", sessionId: message.session_id, tools: message.tools },
    }];
  }
  return [];
}

export function sanitizeBashHook(hookInput: unknown): JsonRecord {
  if (!isRecord(hookInput)) return {};
  const toolInput = isRecord(hookInput.tool_input) ? hookInput.tool_input : {};
  const command = stringField(toolInput, "command");
  if (!command) return {};
  const sanitized = `unset ANTHROPIC_API_KEY ANTHROPIC_AUTH_TOKEN CLAUDE_CODE_OAUTH_TOKEN; ${command}`;
  return {
    hookSpecificOutput: {
      hookEventName: "PreToolUse",
      permissionDecision: "allow",
      permissionDecisionReason: "Team Cross strips model credentials from Bash subprocesses",
      updatedInput: { ...toolInput, command: sanitized },
    },
  };
}

async function importClaudeSdk(): Promise<ClaudeSdk> {
  const module = await import("@anthropic-ai/claude-agent-sdk");
  if (
    typeof module.query !== "function"
    || typeof module.listSessions !== "function"
    || typeof module.getSessionMessages !== "function"
  ) {
    throw new Error("installed Claude Agent SDK is missing required APIs");
  }
  return module as unknown as ClaudeSdk;
}

function assertClaudeCredentials(env: NodeJS.ProcessEnv): void {
  if (env.ANTHROPIC_API_KEY) return;
  const cloudConfigured = env.CLAUDE_CODE_USE_BEDROCK === "1"
    || env.CLAUDE_CODE_USE_VERTEX === "1"
    || env.CLAUDE_CODE_USE_FOUNDRY === "1"
    || env.CLAUDE_CODE_USE_ANTHROPIC_AWS === "1";
  if (!cloudConfigured) {
    throw new Error(
      "Claude requires ANTHROPIC_API_KEY or an officially supported Bedrock, Vertex, Foundry, or Claude Platform credential configuration",
    );
  }
}

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function stringField(record: JsonRecord, key: string): string | undefined {
  const value = record[key];
  return typeof value === "string" && value !== "" ? value : undefined;
}

function numberField(record: JsonRecord, key: string): number | undefined {
  const value = record[key];
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function epochMillisToIso(value: number | undefined): string | undefined {
  return value === undefined ? undefined : new Date(value).toISOString();
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (isRecord(error) && typeof error.message === "string") return error.message;
  return typeof error === "string" ? error : JSON.stringify(error);
}
