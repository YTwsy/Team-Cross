export const BRIDGE_PROTOCOL_VERSION = 1 as const;

export type AgentProvider = "mock" | "codex" | "claude";

export type SessionSurface = "cli" | "desktop" | "vscode" | "app-server" | "unknown";

/** Provider-owned identity. Codex thread.sessionId is a family root, not the conversation ID. */
export interface SessionRef {
  provider: Exclude<AgentProvider, "mock">;
  sessionId: string;
  identityKind: "thread.id" | "sessionId";
  surface: SessionSurface;
  /** Bounded Provider source discriminator; not UI identity or a capability grant. */
  providerSource?: string;
  title?: string;
  cwd?: string;
  nativeIds?: Record<string, string>;
  providerVersion?: string;
}

export interface SessionCapabilities {
  read: boolean;
  follow: boolean;
  open: boolean;
  resume: boolean;
  takeControl: boolean;
  reason?: string;
}

export interface SessionEntry {
  id: string;
  kind: "message" | "tool" | "notice";
  role?: "user" | "assistant" | "system" | "tool";
  text: string;
  sourceId?: string;
  turnId?: string;
}

/** Read-only candidate; Go Core assigns the durable snapshot ID and stores it immutably. */
export interface SessionSnapshot {
  source: SessionRef;
  capturedAt: string;
  entries: SessionEntry[];
  truncated: boolean;
  warnings: string[];
  capabilities: SessionCapabilities;
}

export interface SessionsPollParams {
  provider: Exclude<AgentProvider, "mock">;
  sessionId: string;
  cursor?: string;
  limit?: number;
}

/** Technical read-only polling result, not proof of native Follow acceptance. */
export interface SessionPollResult {
  source: SessionRef;
  capturedAt: string;
  /** Stable-ID upserts, never a claim to contain the complete history. */
  entries: SessionEntry[];
  cursor: string;
  /** Replace only the mutable Follow view; immutable captures remain intact. */
  reset: boolean;
  gaps: string[];
  truncated: boolean;
  warnings: string[];
}

export const bridgeEventTypes = [
  "run.started",
  "run.status",
  "run.closed",
  "turn.started",
  "turn.completed",
  "message.delta",
  "message.completed",
  "tool.started",
  "tool.completed",
  "file.changed",
  "input.requested",
  "input.resolved",
  "run.error",
] as const;

export type BridgeEventType = (typeof bridgeEventTypes)[number];

export interface BridgeEvent {
  type: BridgeEventType;
  runId: string;
  provider: AgentProvider;
  timestamp: string;
  turnId?: string;
  messageId?: string;
  toolId?: string;
  inputRequestId?: string;
  data: Record<string, unknown>;
}

export interface StoredSession {
  provider: Exclude<AgentProvider, "mock">;
  sessionId: string;
  title?: string;
  cwd?: string;
  createdAt?: string;
  updatedAt?: string;
  identityKind?: SessionRef["identityKind"];
  surface?: SessionSurface;
  metadata?: Record<string, unknown>;
}

export interface RunDescriptor {
  runId: string;
  provider: AgentProvider;
  sessionId: string;
  worktree: string;
  networkEnabled: boolean;
  model?: string;
  status: "starting" | "idle" | "running" | "closed" | "error";
  capabilities: {
    send: boolean;
    steer: boolean;
    interrupt: boolean;
    inputResponse: boolean;
  };
}

export interface RunsCreateParams {
  runId?: string;
  provider: AgentProvider;
  worktree: string;
  networkEnabled?: boolean;
  model?: string;
  initialPrompt?: string;
  /** Reserved for forks of Team Cross-managed Codex runs, never history import. */
  forkFromSessionId?: string;
}

export interface SessionsListStoredParams {
  provider: Exclude<AgentProvider, "mock">;
  cwd?: string;
  limit?: number;
  surface?: SessionSurface;
}

export interface SessionsReadStoredParams {
  provider: Exclude<AgentProvider, "mock">;
  sessionId: string;
  cwd?: string;
  limit?: number;
}

export interface RunsImportContextParams {
  runId: string;
  source: {
    provider?: AgentProvider;
    sessionId?: string;
    label?: string;
  };
  context: unknown;
}

export interface RunsMessageParams {
  runId: string;
  message: string;
}

export interface RunsInterruptParams {
  runId: string;
}

export interface RunsRespondInputParams {
  runId: string;
  inputRequestId: string;
  response: unknown;
}

export interface RunsCloseParams {
  runId: string;
}

export type BridgeMethod =
  | "bridge.ping"
  | "bridge.info"
  | "sessions.listStored"
  | "sessions.readStored"
  | "sessions.snapshot"
  | "sessions.poll"
  | "runs.create"
  | "runs.importContext"
  | "runs.send"
  | "runs.steer"
  | "runs.interrupt"
  | "runs.respondInput"
  | "runs.close";

export interface JsonRpcRequest {
  jsonrpc: "2.0";
  id: string | number;
  method: string;
  params?: unknown;
}

export interface JsonRpcNotification {
  jsonrpc: "2.0";
  method: string;
  params?: unknown;
}

export interface JsonRpcSuccess {
  jsonrpc: "2.0";
  id: string | number;
  result: unknown;
}

export interface JsonRpcFailure {
  jsonrpc: "2.0";
  id: string | number | null;
  error: {
    code: number;
    message: string;
    data?: unknown;
  };
}

export type JsonRpcResponse = JsonRpcSuccess | JsonRpcFailure;

export function createBridgeEvent(
  provider: AgentProvider,
  runId: string,
  type: BridgeEventType,
  fields: Omit<Partial<BridgeEvent>, "type" | "provider" | "runId" | "timestamp"> = {},
): BridgeEvent {
  return {
    type,
    provider,
    runId,
    timestamp: new Date().toISOString(),
    data: fields.data ?? {},
    ...(fields.turnId === undefined ? {} : { turnId: fields.turnId }),
    ...(fields.messageId === undefined ? {} : { messageId: fields.messageId }),
    ...(fields.toolId === undefined ? {} : { toolId: fields.toolId }),
    ...(fields.inputRequestId === undefined
      ? {}
      : { inputRequestId: fields.inputRequestId }),
  };
}
