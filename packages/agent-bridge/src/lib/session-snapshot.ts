import { createHash } from "node:crypto";

import type { SessionCapabilities, SessionEntry, SessionRef, SessionSnapshot, SessionSurface } from "../protocol.js";

type RecordValue = Record<string, unknown>;
const MAX_ENTRY_TEXT = 64_000;
const MAX_TOTAL_TEXT = 2_000_000;
export const DEFAULT_SNAPSHOT_LIMIT = 2_000;

/** Read success does not prove passive subscription, precise native navigation or exclusive writing. */
export function reviewCapabilities(provider: SessionRef["provider"]): SessionCapabilities {
  return {
    read: true, follow: false, open: false, resume: false, takeControl: false,
    reason: provider === "codex"
      ? "Stored history only. Passive Follow and exact native UI navigation are not verified; same-conversation resume into an isolated worktree, native Writer fencing and handback must pass CLI and Desktop gates before enablement."
      : "Stored history only. Claude native Follow, exact UI navigation, isolated same-session resume, exclusive Writer fencing and handback have not been verified.",
  };
}

export function codexSurface(source: unknown): SessionSurface {
  const kind = typeof source === "string" ? source : isRecord(source) ? string(source.type) : undefined;
  if (kind === "cli" || kind === "exec") return "cli";
  if (kind === "vscode") return "vscode";
  // appServer is also used by integrations; it does not prove Desktop origin.
  if (kind === "appServer") return "app-server";
  return "unknown";
}

export function normalizeCodexSnapshot(
  raw: unknown,
  sessionId: string,
  options: { limit?: number; capturedAt?: string; providerVersion?: string } = {},
): SessionSnapshot {
  const thread = isRecord(raw) && isRecord(raw.thread) ? raw.thread : undefined;
  if (!thread || thread.id !== sessionId) throw new Error("Codex history returned a different or missing conversation identity");
  const source: SessionRef = {
    provider: "codex", sessionId, identityKind: "thread.id", surface: codexSurface(thread.source),
    nativeIds: { threadId: sessionId, ...(string(thread.sessionId) ? { sessionId: string(thread.sessionId)! } : {}) },
    ...(string(thread.name) ?? string(thread.preview) ? { title: string(thread.name) ?? string(thread.preview) } : {}),
    ...(string(thread.cwd) ? { cwd: string(thread.cwd) } : {}),
    ...(string(thread.cliVersion) ?? options.providerVersion ? { providerVersion: string(thread.cliVersion) ?? options.providerVersion } : {}),
  };
  const builder = new SnapshotBuilder(source, options.limit, options.capturedAt);
  if (!Array.isArray(thread.turns)) builder.omitted("missing-turns", "Provider returned no turn history; this is not a complete transcript.");
  for (const turnValue of array(thread.turns)) {
    if (!isRecord(turnValue)) { builder.omitted("malformed-turn", "A malformed turn was omitted."); continue; }
    const turnId = string(turnValue.id);
    if (turnValue.itemsView === "notLoaded" || turnValue.itemsView === "summary") {
      builder.omitted(`partial-items:${turnId ?? "unknown"}`, "Provider returned summarized or unloaded turn items.");
    }
    if (turnValue.status === "inProgress") builder.omitted(`active-turn:${turnId ?? "unknown"}`, "Captured during an active turn; later output is not included.");
    if (!Array.isArray(turnValue.items)) builder.omitted(`missing-items:${turnId ?? "unknown"}`, "A turn's items were not loaded.");
    for (const itemValue of array(turnValue.items)) {
      if (!isRecord(itemValue)) { builder.omitted("malformed-item", "A malformed history item was omitted."); continue; }
      const type = string(itemValue.type) ?? "unknown";
      const sourceId = string(itemValue.id);
      const anchor = { ...(sourceId ? { sourceId } : {}), ...(turnId ? { turnId } : {}) };
      if (type === "userMessage") {
        builder.add({ kind: "message", role: "user", text: visibleContent(itemValue.content, builder), ...anchor });
      } else if (type === "agentMessage" || type === "plan") {
        builder.add({ kind: "message", role: "assistant", text: string(itemValue.text) ?? "", ...anchor });
      } else if (type === "commandExecution") {
        builder.add({ kind: "tool", role: "tool", text: joinText([
          string(itemValue.command), string(itemValue.aggregatedOutput),
          itemValue.exitCode === undefined || itemValue.exitCode === null ? undefined : `Exit code: ${String(itemValue.exitCode)}`,
        ]), ...anchor });
      } else if (type === "functionCallOutput") {
        builder.add({ kind: "tool", role: "tool", text: joinText([string(itemValue.namespace), string(itemValue.name), visibleContent(itemValue.output, builder)]), ...anchor });
      } else if (type === "fileChange") {
        const changes = array(itemValue.changes).filter(isRecord).map((change) => joinText([string(change.path), string(change.diff)]));
        builder.add({ kind: "tool", role: "tool", text: joinText(["File changes (historical evidence, not the current checkout)", ...changes]), ...anchor });
      } else if (type === "mcpToolCall" || type === "dynamicToolCall") {
        const result = isRecord(itemValue.result) ? itemValue.result : undefined;
        builder.add({ kind: "tool", role: "tool", text: joinText([
          string(itemValue.server), string(itemValue.tool), string(itemValue.name),
          itemValue.arguments === undefined ? undefined : JSON.stringify(itemValue.arguments),
          visibleContent(result?.content ?? itemValue.contentItems ?? itemValue.content, builder),
          result?.structuredContent === undefined || result.structuredContent === null ? undefined : JSON.stringify(result.structuredContent),
          isRecord(itemValue.error) ? string(itemValue.error.message) : string(itemValue.error),
        ]), ...anchor });
      } else if (type === "webSearch") {
        builder.add({ kind: "tool", role: "tool", text: joinText(["Web search", string(itemValue.query)]), ...anchor });
        if (array(itemValue.results).length > 0) builder.omitted(sourceId ?? "web-results", "Structured web search results are not included in the text review.", anchor);
      } else if (type === "collabAgentToolCall") {
        builder.add({ kind: "tool", role: "tool", text: joinText([string(itemValue.tool), string(itemValue.prompt)]), ...anchor });
        builder.omitted(sourceId ?? "subagent", "Child-agent conversations are separate sources and are not included in this snapshot.", anchor);
      } else if (type === "enteredReviewMode" || type === "exitedReviewMode") {
        builder.add({ kind: "notice", text: joinText([type, string(itemValue.review)]), ...anchor });
      } else if (type === "contextCompaction") {
        builder.omitted(sourceId ?? "compaction", "Provider compacted this context; earlier source material may be unavailable.", anchor);
      } else {
        // Never dump unknown objects: they may contain private reasoning, credentials or inline media.
        builder.omitted(sourceId ?? `unsupported:${type}`, `History item ${type} is not included in the text review.`, anchor);
      }
    }
    if (isRecord(turnValue.error) && string(turnValue.error.message)) {
      builder.add({ kind: "notice", text: string(turnValue.error.message)!, ...(turnId ? { turnId } : {}) });
    }
  }
  if (thread.nextCursor || thread.turnsTruncated === true) builder.omitted("provider-pagination", "Provider reports additional history outside this snapshot.");
  return builder.finish();
}

export function normalizeClaudeSnapshot(
  messages: unknown[],
  sessionId: string,
  options: { limit?: number; capturedAt?: string; cwd?: string; metadata?: unknown; providerTruncated?: boolean } = {},
): SessionSnapshot {
  const metadata = isRecord(options.metadata) ? options.metadata : {};
  if (metadata.sessionId !== undefined && metadata.sessionId !== sessionId) throw new Error("Claude metadata returned a different conversation identity");
  const builder = new SnapshotBuilder({
    provider: "claude", sessionId, identityKind: "sessionId", surface: "unknown", nativeIds: { sessionId },
    ...(string(metadata.customTitle) ?? string(metadata.summary) ? { title: string(metadata.customTitle) ?? string(metadata.summary) } : {}),
    ...(string(metadata.cwd) ?? options.cwd ? { cwd: string(metadata.cwd) ?? options.cwd } : {}),
  }, options.limit, options.capturedAt);
  for (const value of messages) {
    if (!isRecord(value)) { builder.omitted("malformed-message", "A malformed Claude message was omitted."); continue; }
    if (value.session_id !== undefined && value.session_id !== sessionId) throw new Error("Claude history returned a different conversation identity");
    const sourceId = string(value.uuid);
    const message = isRecord(value.message) ? value.message : {};
    const role = value.type === "user" ? "user" : value.type === "assistant" ? "assistant" : "system";
    const content = message.content ?? (typeof value.message === "string" ? value.message : undefined);
    if (typeof content === "string") {
      builder.add({ kind: role === "system" ? "notice" : "message", role, text: content, ...(sourceId ? { sourceId } : {}) });
      continue;
    }
    if (!Array.isArray(content)) { builder.omitted(sourceId ?? "missing-content", "A Claude message has no readable content."); continue; }
    for (const [index, block] of content.entries()) {
      const anchor = sourceId ? { sourceId } : {};
      if (!isRecord(block)) { builder.omitted(sourceId ?? "invalid-content", "A malformed content block was omitted."); continue; }
      if (block.type === "text") {
        builder.add({ kind: role === "system" ? "notice" : "message", role, text: string(block.text) ?? "", ...anchor }, String(index));
      } else if (block.type === "tool_use") {
        builder.add({ kind: "tool", role: "assistant", text: joinText([string(block.name), block.input === undefined ? undefined : JSON.stringify(block.input)]), ...anchor }, String(index));
      } else if (block.type === "tool_result") {
        builder.add({ kind: "tool", role: "tool", text: visibleContent(block.content, builder), ...anchor }, String(index));
      } else {
        builder.omitted(sourceId ? `${sourceId}:${index}` : "unsupported-content", `Claude content ${string(block.type) ?? "unknown"} is not included in the text review.`, anchor);
      }
    }
  }
  if (options.providerTruncated) builder.omitted("provider-pagination", "Additional Claude messages exceed the requested snapshot limit.");
  if (messages.length === 0) builder.omitted("empty-history", "No messages were returned; the session may be empty, unavailable or missing.");
  return builder.finish();
}

class SnapshotBuilder {
  private readonly entries: SessionEntry[] = [];
  private readonly warnings = new Set<string>();
  private readonly occurrences = new Map<string, number>();
  private truncated = false;
  private totalText = 0;

  constructor(private readonly source: SessionRef, private readonly limit = DEFAULT_SNAPSHOT_LIMIT, private readonly capturedAt = new Date().toISOString()) {}

  add(entry: Omit<SessionEntry, "id">, part = ""): void {
    if (this.entries.length >= this.limit || this.totalText >= MAX_TOTAL_TEXT) {
      this.truncated = true;
      this.warn("Snapshot text or entry limit reached; later content is not included.");
      return;
    }
    const textLimit = Math.min(MAX_ENTRY_TEXT, MAX_TOTAL_TEXT - this.totalText);
    let text = entry.text;
    if (text.length > textLimit) {
      text = text.slice(0, textLimit) + "\n[Text truncated]";
      this.truncated = true;
      this.warn("One or more entries contain truncated text.");
    }
    const key = JSON.stringify([this.source.provider, this.source.sessionId, entry.turnId ?? "", entry.sourceId ?? "", part, entry.kind, entry.role ?? "", entry.sourceId ? "" : entry.text]);
    const occurrence = this.occurrences.get(key) ?? 0;
    this.occurrences.set(key, occurrence + 1);
    const id = `entry-${createHash("sha256").update(key).update(`:${occurrence}`).digest("hex")}`;
    this.entries.push({ ...entry, text, id });
    this.totalText += text.length;
  }

  omitted(key: string, text: string, anchor: Partial<Pick<SessionEntry, "sourceId" | "turnId">> = {}): void {
    this.truncated = true;
    this.warn(text);
    this.add({ kind: "notice", text, sourceId: `notice:${key}`, ...anchor });
  }

  finish(): SessionSnapshot {
    return { source: this.source, capturedAt: this.capturedAt, entries: this.entries, truncated: this.truncated, warnings: [...this.warnings], capabilities: reviewCapabilities(this.source.provider) };
  }

  private warn(text: string): void {
    if (this.warnings.size < 100) this.warnings.add(text.slice(0, 500));
  }
}

function visibleContent(value: unknown, builder: SnapshotBuilder): string {
  if (typeof value === "string") return value;
  const parts: string[] = [];
  for (const part of array(value)) {
    if (isRecord(part) && (part.type === "text" || part.type === "inputText" || part.type === "input_text" || part.type === "output_text") && typeof part.text === "string") parts.push(part.text);
    else builder.omitted("non-text-content", "Non-text or unsupported content was omitted from the text review.");
  }
  return parts.join("\n");
}

function string(value: unknown): string | undefined { return typeof value === "string" && value !== "" ? value : undefined; }
function array(value: unknown): unknown[] { return Array.isArray(value) ? value : []; }
function isRecord(value: unknown): value is RecordValue { return typeof value === "object" && value !== null && !Array.isArray(value); }
function joinText(parts: Array<string | undefined>): string { return parts.filter((part): part is string => part !== undefined && part !== "").join("\n"); }
