import { randomUUID } from "node:crypto";
import { realpath, stat } from "node:fs/promises";

import { ClaudeAdapter } from "./adapters/claude.js";
import { CodexAdapter } from "./adapters/codex.js";
import { MockAdapter } from "./adapters/mock.js";
import { MAX_POLL_CURSOR_BYTES, MAX_POLL_ENTRIES } from "./lib/codex-poll.js";
import type { AgentAdapter, AgentRun, EventSink } from "./adapters/types.js";
import {
  BRIDGE_PROTOCOL_VERSION,
  type AgentProvider,
  type BridgeEvent,
  type RunDescriptor,
  type RunsCreateParams,
  type SessionsListStoredParams,
  type SessionsReadStoredParams,
  type SessionSurface,
} from "./protocol.js";

type JsonRecord = Record<string, unknown>;

export interface BridgeServerOptions {
  adapters?: Partial<Record<AgentProvider, AgentAdapter>>;
  onEvent?: EventSink;
  onDiagnostic?: (message: string) => void;
}

export class BridgeServer {
  private readonly adapters: Record<AgentProvider, AgentAdapter>;
  private readonly runs = new Map<string, AgentRun>();
  private eventSink: EventSink;

  constructor(options: BridgeServerOptions = {}) {
    this.eventSink = options.onEvent ?? (() => undefined);
    this.adapters = {
      mock: options.adapters?.mock ?? new MockAdapter(),
      codex: options.adapters?.codex ?? new CodexAdapter({
        onDiagnostic: options.onDiagnostic,
      }),
      claude: options.adapters?.claude ?? new ClaudeAdapter({
        onDiagnostic: options.onDiagnostic,
      }),
    };
  }

  setEventSink(sink: EventSink): void {
    this.eventSink = sink;
  }

  async handle(method: string, rawParams: unknown): Promise<unknown> {
    const params = record(rawParams);
    switch (method) {
      case "bridge.ping":
        return {
          ok: true,
          name: "teamcross-agent-bridge",
          version: "0.1.0",
          protocolVersion: BRIDGE_PROTOCOL_VERSION,
        };
      case "bridge.info":
        return {
          name: "teamcross-agent-bridge",
          version: "0.1.0",
          protocolVersion: BRIDGE_PROTOCOL_VERSION,
          providers: Object.keys(this.adapters),
          methods: [
            "bridge.ping",
            "sessions.listStored",
            "sessions.readStored",
            "sessions.snapshot",
            "sessions.poll",
            "runs.create",
            "runs.importContext",
            "runs.send",
            "runs.steer",
            "runs.interrupt",
            "runs.respondInput",
            "runs.close",
          ],
        };
      case "sessions.listStored": {
        const provider = storedProvider(params.provider);
        return await this.adapters[provider].listStored({
          provider,
          ...(params.surface === undefined ? {} : { surface: sessionSurface(params.surface) }),
          ...(optionalString(params.cwd) ? { cwd: optionalString(params.cwd) } : {}),
          ...(optionalNumber(params.limit) === undefined
            ? {}
            : { limit: optionalNumber(params.limit) }),
        } satisfies SessionsListStoredParams);
      }
      case "sessions.readStored": {
        const provider = storedProvider(params.provider);
        return await this.adapters[provider].readStored({
          provider,
          sessionId: requiredString(params.sessionId, "sessionId"),
          ...(optionalString(params.cwd) ? { cwd: optionalString(params.cwd) } : {}),
          ...(optionalNumber(params.limit) === undefined
            ? {}
            : { limit: optionalNumber(params.limit) }),
        } satisfies SessionsReadStoredParams);
      }
      case "sessions.snapshot": {
        const provider = storedProvider(params.provider);
        const adapter = this.adapters[provider];
        if (!adapter.snapshot) throw new BridgeRpcError(-32601, `${provider} does not support review snapshots`);
        return await adapter.snapshot({
          provider,
          sessionId: requiredString(params.sessionId, "sessionId"),
          ...(optionalString(params.cwd) ? { cwd: optionalString(params.cwd) } : {}),
          ...(params.limit === undefined ? {} : { limit: snapshotLimit(params.limit) }),
        });
      }
      case "sessions.poll": {
        const provider = storedProvider(params.provider);
        const adapter = this.adapters[provider];
        if (!adapter.poll) throw new BridgeRpcError(-32601, `${provider} does not implement read-only polling`);
        const sessionId = requiredString(params.sessionId, "sessionId");
        if (sessionId.length > 256) throw new BridgeRpcError(-32602, "sessionId exceeds 256 characters");
        if (params.cursor !== undefined && (typeof params.cursor !== "string" || Buffer.byteLength(params.cursor) > MAX_POLL_CURSOR_BYTES)) {
          throw new BridgeRpcError(-32602, "invalid or oversized poll cursor");
        }
        if (params.limit !== undefined && (typeof params.limit !== "number" || !Number.isInteger(params.limit) || params.limit < 1 || params.limit > MAX_POLL_ENTRIES)) {
          throw new BridgeRpcError(-32602, `poll limit must be between 1 and ${MAX_POLL_ENTRIES}`);
        }
        return await adapter.poll({ provider, sessionId,
          ...(params.cursor === undefined ? {} : { cursor: params.cursor as string }),
          ...(params.limit === undefined ? {} : { limit: params.limit as number }),
        });
      }
      case "runs.create":
        return await this.createRun(params);
      case "runs.importContext": {
        const run = this.run(params.runId);
        const source = isRecord(params.source) ? params.source : {};
        await run.importContext(source, params.context);
        return { imported: true };
      }
      case "runs.send":
        return await this.run(params.runId).send(requiredString(params.message, "message"));
      case "runs.steer":
        return await this.run(params.runId).steer(requiredString(params.message, "message"));
      case "runs.interrupt":
        await this.run(params.runId).interrupt();
        return { interrupted: true };
      case "runs.respondInput":
        await this.run(params.runId).respondInput(
          requiredString(params.inputRequestId, "inputRequestId"),
          params.response,
        );
        return { resolved: true };
      case "runs.close": {
        const runId = requiredString(params.runId, "runId");
        const run = this.run(runId);
        await run.close();
        this.runs.delete(runId);
        return { closed: true };
      }
      default:
        throw new BridgeRpcError(-32601, `method not found: ${method}`);
    }
  }

  getRun(runId: string): RunDescriptor | undefined {
    return this.runs.get(runId)?.descriptor;
  }

  async shutdown(): Promise<void> {
    for (const run of this.runs.values()) await run.close();
    this.runs.clear();
    await Promise.all(Object.values(this.adapters).map(async (adapter) => adapter.shutdown()));
  }

  private async createRun(params: JsonRecord): Promise<RunDescriptor> {
    const provider = agentProvider(params.provider);
    const worktree = await validateWorktree(requiredString(params.worktree, "worktree"));
    const runId = optionalString(params.runId) ?? randomUUID();
    if (this.runs.has(runId)) throw new BridgeRpcError(-32602, `run already exists: ${runId}`);
    const createParams: RunsCreateParams & { runId: string; worktree: string } = {
      runId,
      provider,
      worktree,
      networkEnabled: params.networkEnabled === true,
      ...(optionalString(params.model) ? { model: optionalString(params.model) } : {}),
      ...(optionalString(params.initialPrompt)
        ? { initialPrompt: optionalString(params.initialPrompt) }
        : {}),
      ...(optionalString(params.forkFromSessionId)
        ? { forkFromSessionId: optionalString(params.forkFromSessionId) }
        : {}),
    };
    const run = await this.adapters[provider].createRun(createParams, (event: BridgeEvent) => {
      this.eventSink(event);
    });
    this.runs.set(runId, run);
    return run.descriptor;
  }

  private run(value: unknown): AgentRun {
    const runId = requiredString(value, "runId");
    const run = this.runs.get(runId);
    if (!run) throw new BridgeRpcError(-32602, `unknown run: ${runId}`);
    return run;
  }
}

export class BridgeRpcError extends Error {
  constructor(
    readonly code: number,
    message: string,
    readonly data?: unknown,
  ) {
    super(message);
  }
}

async function validateWorktree(value: string): Promise<string> {
  const resolved = await realpath(value).catch(() => undefined);
  if (!resolved) throw new BridgeRpcError(-32602, `worktree does not exist: ${value}`);
  const info = await stat(resolved);
  if (!info.isDirectory()) throw new BridgeRpcError(-32602, `worktree is not a directory: ${value}`);
  return resolved;
}

function agentProvider(value: unknown): AgentProvider {
  if (value === "mock" || value === "codex" || value === "claude") return value;
  throw new BridgeRpcError(-32602, `unsupported provider: ${String(value)}`);
}

function storedProvider(value: unknown): "codex" | "claude" {
  if (value === "codex" || value === "claude") return value;
  throw new BridgeRpcError(-32602, `stored sessions require codex or claude, got ${String(value)}`);
}

function record(value: unknown): JsonRecord {
  if (value === undefined || value === null) return {};
  if (!isRecord(value)) throw new BridgeRpcError(-32602, "params must be an object");
  return value;
}

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function requiredString(value: unknown, name: string): string {
  if (typeof value !== "string" || value === "") {
    throw new BridgeRpcError(-32602, `${name} must be a non-empty string`);
  }
  return value;
}

function optionalString(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

function optionalNumber(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function sessionSurface(value: unknown): SessionSurface {
  if (value === "cli" || value === "desktop" || value === "vscode" || value === "app-server" || value === "unknown") return value;
  throw new BridgeRpcError(-32602, "unsupported session surface");
}

function snapshotLimit(value: unknown): number {
  if (typeof value !== "number" || !Number.isInteger(value) || value < 1 || value > 10_000) {
    throw new BridgeRpcError(-32602, "limit must be an integer between 1 and 10000");
  }
  return value;
}
