import { createHash } from "node:crypto";

import type { SessionEntry, SessionPollResult, SessionsPollParams } from "../protocol.js";
import { boundReviewResult, normalizeCodexSnapshot } from "./session-snapshot.js";

type RecordValue = Record<string, unknown>;
type Request = (method: string, params: RecordValue) => Promise<unknown>;
export const MAX_POLL_CURSOR_BYTES = 128 * 1024;
export const MAX_POLL_ENTRIES = 500;
const MAX_NATIVE_CURSOR_BYTES = 8192;
const MAX_TEXT = 2_000_000;
const PAGE_SIZE = 20;
const MAX_PAGES = 10;
interface Cursor {
  version: 1;
  provider: "codex";
  sessionId: string;
  readerVersion: string;
  native: string | null;
  boundary: string | null;
  fingerprints: Record<string, string>;
}
interface Page { data: RecordValue[]; nextCursor: string | null; backwardsCursor: string | null }
class PollGapError extends Error {}

/** Only persisted reads: never resume, subscribe, list other Sessions or send input. */
export async function pollCodexSession(request: Request, params: SessionsPollParams, readerVersion: () => string): Promise<SessionPollResult> {
  if (params.provider !== "codex" || !params.sessionId || params.sessionId.length > 256) throw new Error("invalid Codex poll identity");
  const limit = params.limit ?? 200;
  if (!Number.isInteger(limit) || limit < 1 || limit > MAX_POLL_ENTRIES) throw new Error("invalid poll limit");
  const previous = decodeCursor(params.cursor, params.sessionId);
  const metadata = await request("thread/read", { threadId: params.sessionId, includeTurns: false });
  if (!isRecord(metadata) || !isRecord(metadata.thread) || metadata.thread.id !== params.sessionId) throw new Error("Codex poll returned a different conversation identity");
  if (Buffer.byteLength(JSON.stringify(metadata)) > 256 * 1024) throw new Error("Codex poll metadata exceeds limit");
  if (readerVersion().length > 1024) throw new Error("Codex reader version exceeds limit");
  const thread = metadata.thread;
  const source = normalizeCodexSnapshot({ thread: { ...thread, turns: [] } }, params.sessionId, { providerVersion: readerVersion() }).source;
  const capturedAt = new Date().toISOString();
  // Capture the upper boundary BEFORE walking forwards. Later appends must not
  // make a new cursor skip changes that this response has not actually read.
  const head = page(await request("thread/turns/list", { threadId: params.sessionId, sortDirection: "desc", itemsView: "full", limit: PAGE_SIZE }));
  const newest = head.data[0]?.id as string | undefined;
  const gaps = new Set<string>();
  const warnings = new Set<string>();
  let reset = previous === undefined;
  let turns = [...head.data].reverse();
  if (previous && previous.readerVersion !== readerVersion()) {
    reset = true;
    gaps.add("Provider reader version changed; rebuilt the newest checkpoint.");
  } else if (previous?.boundary) {
    if (!newest || !previous.native) {
      reset = true;
      gaps.add("Previously observed history is unavailable; rebuilt the newest checkpoint.");
    } else {
      try {
        const collected = new Map<string, RecordValue>();
        const cursors = new Set<string>();
        let native = previous.native;
        let reached = false;
        for (let index = 0; index < MAX_PAGES; index++) {
          if (cursors.has(native)) throw new PollGapError("repeated pagination cursor");
          cursors.add(native);
          const next = page(await request("thread/turns/list", { threadId: params.sessionId, sortDirection: "asc", itemsView: "full", limit: PAGE_SIZE, cursor: native }));
          for (const turn of next.data) {
            const id = turn.id as string;
            collected.set(id, turn);
            if (id === newest) { reached = true; break; }
          }
          if (reached) break;
          if (!next.nextCursor) throw new PollGapError("checkpoint boundary is missing");
          native = next.nextCursor;
        }
        if (!reached || !collected.has(previous.boundary)) throw new PollGapError("history gap or pagination budget exceeded");
        turns = [...collected.values()];
      } catch (error) {
        // A transport failure is not evidence that history vanished. Let Core
        // retain its durable cursor and retry after reconnect, without skipping
        // recoverable backlog by replacing it with the current tail.
        if (!isHistoryGap(error)) throw error;
        reset = true;
        gaps.add("Incremental history could not reach its boundary; rebuilt the newest checkpoint. Some updates may be missing.");
      }
    }
  } else if (previous && head.nextCursor) {
    reset = true;
    gaps.add("History grew beyond the first bounded checkpoint after an empty capture; older updates may be missing.");
  }
  const normalized = normalizeTail(thread, turns, params.sessionId, limit, capturedAt);
  for (const warning of normalized.warnings) warnings.add(warning);
  if (normalized.truncated) gaps.add("Some observed items were omitted or truncated by the text-review limits.");
  const boundaryEntries = normalized.entries.filter((entry) => entry.turnId === previous?.boundary);
  // An anchor can be rewritten (rollback, compaction or item removal). A delta
  // must not leave removed items pretending to remain current. Sealed captures
  // are owned by Core and are never altered by this reset signal.
  if (previous?.boundary && !normalized.truncated && Object.keys(previous.fingerprints).some((id) => !boundaryEntries.some((entry) => entry.id === id))) {
    reset = true;
    gaps.add("Previously observed boundary items were removed; rebuilt the current Follow view.");
  }
  if (reset && previous) {
    const checkpoint = normalizeTail(thread, [...head.data].reverse(), params.sessionId, limit, capturedAt);
    normalized.entries = checkpoint.entries;
    normalized.truncated ||= checkpoint.truncated;
    for (const warning of checkpoint.warnings) warnings.add(warning);
  }
  const latestBoundary = normalized.entries.filter((entry) => entry.turnId === newest);
  const nextCursor: Cursor = {
    version: 1, provider: "codex", sessionId: params.sessionId, readerVersion: readerVersion(),
    native: head.backwardsCursor, boundary: newest ?? null,
    fingerprints: Object.fromEntries(latestBoundary.map((entry) => [entry.id, fingerprint(entry)])),
  };
  if (newest && !head.backwardsCursor) {
    gaps.add("Provider did not return a reversible boundary cursor; the next poll will rebuild its checkpoint.");
  }
  if (reset && head.nextCursor) warnings.add("Newest checkpoint only: older persisted history is outside this Follow window.");
  const entries = reset || !previous ? normalized.entries : normalized.entries.filter((entry) => previous.fingerprints[entry.id] !== fingerprint(entry));
  const cursor = Buffer.from(JSON.stringify(nextCursor)).toString("base64url");
  if (Buffer.byteLength(cursor) > MAX_POLL_CURSOR_BYTES) throw new Error("generated poll cursor exceeds limit");
  const result = { source, capturedAt, entries, cursor, reset, gaps: [...gaps].slice(0, 20), truncated: normalized.truncated || gaps.size > 0 || (reset && head.nextCursor !== null), warnings: [...warnings].slice(0, 100) };
  const bounded = boundReviewResult(result);
  if (bounded !== result) {
    // Keep the fixed native checkpoint, but never fingerprint omitted or fuller
    // text as already delivered. Refreshing this boundary can later upsert the
    // complete text; unchanged entries from the previous cursor stay known.
    const delivered = new Map(bounded.entries.map((entry) => [entry.id, entry]));
    for (const entry of entries.filter((entry) => entry.turnId === newest)) {
      const actual = delivered.get(entry.id);
      if (actual) nextCursor.fingerprints[entry.id] = fingerprint(actual);
      else delete nextCursor.fingerprints[entry.id];
    }
    // This encoding is no larger: existing SHA digests are replaced or removed.
    bounded.cursor = Buffer.from(JSON.stringify(nextCursor)).toString("base64url");
  }
  return bounded;
}

function normalizeTail(thread: RecordValue, turns: RecordValue[], sessionId: string, limit: number, capturedAt: string): { entries: SessionEntry[]; warnings: string[]; truncated: boolean } {
  let entries: SessionEntry[] = [];
  const warnings = new Set<string>();
  let truncated = false;
  let remainingText = MAX_TEXT;
  for (const turn of [...turns].reverse()) {
    if (entries.length >= limit || remainingText <= 0) { truncated = true; break; }
    const items = Array.isArray(turn.items) ? turn.items : undefined;
    if (items && items.length > MAX_POLL_ENTRIES) truncated = true;
    const itemIds = new Set<string>();
    for (const item of items ?? []) {
      if (!isRecord(item) || typeof item.id !== "string" || !item.id || itemIds.has(item.id)) {
        truncated = true;
        warnings.add("Some native items lack unique stable identities; their inferred text anchors may change.");
      } else itemIds.add(item.id);
    }
    if (turn.status === "inProgress") warnings.add("Provider Turn is active; this read contains persisted output, not a subscribed event stream.");
    // Active-turn notices are capture-time observations, not durable native
    // items. Avoid creating and then deleting a synthetic item on completion.
    const capture = normalizeCodexSnapshot({ thread: { ...thread, turns: [{ ...turn, status: turn.status === "inProgress" ? "completed" : turn.status, ...(items ? { items: items.slice(-MAX_POLL_ENTRIES) } : {}) }] } }, sessionId, { capturedAt, limit: 10_000 });
    truncated ||= capture.truncated;
    for (const warning of capture.warnings) warnings.add(warning);
    const available = limit - entries.length;
    if (capture.entries.length > available) truncated = true;
    const selected = capture.entries.slice(-available);
    const chunk: SessionEntry[] = [];
    for (const entry of [...selected].reverse()) {
      if (remainingText <= 0) { truncated = true; break; }
      const text = entry.text.slice(0, remainingText);
      if (text !== entry.text) truncated = true;
      chunk.unshift({ ...entry, text });
      remainingText -= text.length;
    }
    entries = [...chunk, ...entries];
  }
  if (truncated) warnings.add("Follow checkpoint is partial; some history or text is outside the bounded result.");
  return { entries, warnings: [...warnings], truncated };
}

function page(value: unknown): Page {
  if (!isRecord(value) || !Array.isArray(value.data) || value.data.length > PAGE_SIZE || Buffer.byteLength(JSON.stringify(value)) > 4 * 1024 * 1024) throw new Error("incompatible or oversized Codex poll page");
  const ids = new Set<string>();
  const data: RecordValue[] = [];
  for (const turn of value.data) {
    if (!isRecord(turn) || typeof turn.id !== "string" || !turn.id || turn.id.length > 256 || ids.has(turn.id)) throw new Error("invalid or duplicate Turn identity");
    ids.add(turn.id);
    data.push(turn);
  }
  return { data, nextCursor: nativeCursor(value.nextCursor), backwardsCursor: nativeCursor(value.backwardsCursor) };
}

function decodeCursor(value: string | undefined, sessionId: string): Cursor | undefined {
  if (value === undefined) return undefined;
  if (!value || Buffer.byteLength(value) > MAX_POLL_CURSOR_BYTES || !/^[A-Za-z0-9_-]+$/.test(value)) throw new Error("invalid or oversized poll cursor");
  let decoded: unknown;
  try { decoded = JSON.parse(Buffer.from(value, "base64url").toString("utf8")); } catch { throw new Error("invalid poll cursor"); }
  if (!isRecord(decoded) || decoded.version !== 1 || decoded.provider !== "codex" || decoded.sessionId !== sessionId) throw new Error("poll cursor belongs to a different Session or protocol version");
  if (typeof decoded.readerVersion !== "string" || decoded.readerVersion.length > 1024 || (decoded.boundary !== null && (typeof decoded.boundary !== "string" || decoded.boundary.length > 256))) throw new Error("invalid poll cursor metadata");
  if (!isRecord(decoded.fingerprints) || Object.keys(decoded.fingerprints).length > MAX_POLL_ENTRIES) throw new Error("invalid poll cursor fingerprints");
  for (const [id, hash] of Object.entries(decoded.fingerprints)) {
    if (!/^entry-[a-f0-9]{64}$/.test(id) || typeof hash !== "string" || !/^[a-f0-9]{64}$/.test(hash)) throw new Error("invalid poll cursor fingerprint");
  }
  return { version: 1, provider: "codex", sessionId, readerVersion: decoded.readerVersion, native: nativeCursor(decoded.native), boundary: decoded.boundary as string | null, fingerprints: decoded.fingerprints as Record<string, string> };
}

function nativeCursor(value: unknown): string | null {
  if (value === null || value === undefined) return null;
  if (typeof value !== "string" || !value || Buffer.byteLength(value) > MAX_NATIVE_CURSOR_BYTES) throw new Error("invalid native pagination cursor");
  return value;
}
function isHistoryGap(error: unknown): boolean {
  if (error instanceof PollGapError) return true;
  const message = error instanceof Error ? error.message : "";
  return /cursor|anchor/i.test(message) && /invalid|expired|not found|missing|unavailable|unknown/i.test(message);
}
function fingerprint(entry: SessionEntry): string { return createHash("sha256").update(JSON.stringify(entry)).digest("hex"); }
function isRecord(value: unknown): value is RecordValue { return typeof value === "object" && value !== null && !Array.isArray(value); }
