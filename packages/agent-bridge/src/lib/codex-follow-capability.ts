type RecordValue = Record<string, unknown>;
export type CodexFollowRequest = (method: string, params: RecordValue) => Promise<unknown>;
export interface CodexFollowProbe { supported: boolean; reason: string }

export const MAX_CODEX_FOLLOW_PROBES = 128;
const MAX_METADATA_BYTES = 256 * 1024;
const MAX_PAGE_BYTES = 4 * 1024 * 1024;
const MAX_PAGE_ITEMS = 10_000;
const SUPPORTED_REASON = "Verified read-only persisted-poll for this Session and reader; synchronizes saved text and tool output, not an event subscription or input permission.";

/** Never let the poller's native cursor-gap fallback consume a format failure. */
export class CodexFollowCapabilityError extends Error {
  constructor(readonly reason: string) {
    super("Codex persisted-poll contract validation failed");
    this.name = "CodexFollowCapabilityError";
  }
}

interface FullTurn extends RecordValue { id: string; items: Array<RecordValue & { id: string; type: string }> }
interface FullPage { data: FullTurn[]; nextCursor: string | null; backwardsCursor: string | null }

/** One instance belongs to ONE captured app-server client generation. */
export class CodexFollowCapability {
  private retired = false;
  private readonly supported = new Map<string, true>();
  private readonly pending = new Map<string, Promise<CodexFollowProbe>>();

  constructor(private readonly request: CodexFollowRequest) {}

  /** Retire on exit/close; even an in-flight response cannot revive this reader. */
  retire(): void {
    this.retired = true;
    this.supported.clear();
    this.pending.clear();
  }

  async probe(sessionId: string): Promise<CodexFollowProbe> {
    if (this.retired) return unavailable("The original read-only reader has closed; validate a new reader.");
    if (!validID(sessionId)) return unavailable("A precise bounded native Session identity is required.");
    if (this.supported.has(sessionId)) {
      this.supported.delete(sessionId);
      this.supported.set(sessionId, true);
      return { supported: true, reason: SUPPORTED_REASON };
    }
    const pending = this.pending.get(sessionId);
    if (pending) return await pending;
    if (this.pending.size >= MAX_CODEX_FOLLOW_PROBES) return unavailable("Read-only capability checks are busy; retry later.");
    const operation = this.runProbe(sessionId).then((): CodexFollowProbe => {
      this.assertCurrent();
      this.supported.set(sessionId, true);
      while (this.supported.size > MAX_CODEX_FOLLOW_PROBES) this.supported.delete(this.supported.keys().next().value!);
      return { supported: true, reason: SUPPORTED_REASON };
    }).catch((error: unknown): CodexFollowProbe => {
      this.supported.delete(sessionId);
      return unavailable(error instanceof CodexFollowCapabilityError
        ? error.reason
        : "The current reader could not validate read-only persisted polling; retry when the source is available.");
    }).finally(() => {
      if (this.pending.get(sessionId) === operation) this.pending.delete(sessionId);
    });
    this.pending.set(sessionId, operation);
    return await operation;
  }

  async require(sessionId: string): Promise<void> {
    const result = await this.probe(sessionId);
    if (!result.supported) throw new CodexFollowCapabilityError(result.reason);
    this.assertCurrent();
  }

  /** Validate every later poll page; no execution or another Session is allowed. */
  wrapRequest(sessionId: string): CodexFollowRequest {
    return async (method, params) => {
      validateRequest(method, params, sessionId);
      await this.require(sessionId);
      try {
        const value = await this.currentRequest(method, params);
        if (method === "thread/read") validateMetadata(value, sessionId);
        else validatePage(value, params.limit as number, params.sortDirection === "desc");
        return value;
      } catch (error) {
        // A schema failure or disconnect does not authorize the next attempt.
        // Native cursor expiry still reaches the poller's explicit gap handling.
        this.supported.delete(sessionId);
        throw error;
      }
    };
  }

  private assertCurrent(): void {
    if (this.retired) fail("The original read-only reader has closed; validate a new reader.");
  }

  private async currentRequest(method: string, params: RecordValue): Promise<unknown> {
    this.assertCurrent();
    const value = await this.request(method, params);
    this.assertCurrent();
    return value;
  }

  private async runProbe(sessionId: string): Promise<void> {
    validateMetadata(await this.currentRequest("thread/read", { threadId: sessionId, includeTurns: false }), sessionId);
    const head = validatePage(await this.currentRequest("thread/turns/list", {
      threadId: sessionId, sortDirection: "desc", itemsView: "full", limit: 1,
    }), 1, true);
    const boundary = head.data[0];
    if (!boundary || boundary.items.length === 0) fail("No persisted item is available to validate a reversible read-only boundary yet.");
    const back = validatePage(await this.currentRequest("thread/turns/list", {
      threadId: sessionId, sortDirection: "asc", itemsView: "full", limit: 1, cursor: head.backwardsCursor,
    }), 1, false);
    const replay = back.data[0];
    if (!replay || replay.id !== boundary.id) fail("The read-only boundary did not replay as the same native Turn.");
    const identities = new Map(replay.items.map((item) => [item.id, item.type]));
    if (boundary.items.some((item) => identities.get(item.id) !== item.type)) fail("Native item identities changed during boundary verification; retry a stable checkpoint.");
  }
}

function validateMetadata(value: unknown, sessionId: string): void {
  if (!isRecord(value) || !isRecord(value.thread) || value.thread.id !== sessionId) fail("The reader returned a different or missing native Session identity.");
  if (jsonBytes(value) > MAX_METADATA_BYTES || value.thread.historyMode !== "paginated") fail("This Session does not expose the supported bounded paginated history contract.");
}

function validatePage(value: unknown, limit: number, requireReversible: boolean): FullPage {
  if (!isRecord(value) || !Array.isArray(value.data) || value.data.length > limit || jsonBytes(value) > MAX_PAGE_BYTES) fail("The reader returned an incompatible or oversized full-item page.");
  if (!nullableCursor(value.nextCursor) || !nullableCursor(value.backwardsCursor)) fail("The reader returned incompatible pagination metadata.");
  if (requireReversible && value.data.length > 0 && value.backwardsCursor === null) fail("The latest persisted page has no reversible boundary.");
  const turnIds = new Set<string>();
  let itemCount = 0;
  for (const turn of value.data) {
    if (!isRecord(turn) || !validID(turn.id) || turnIds.has(turn.id) || turn.itemsView !== "full" || !Array.isArray(turn.items)) fail("Full native Turns with distinct stable identities are required.");
    turnIds.add(turn.id);
    itemCount += turn.items.length;
    if (itemCount > MAX_PAGE_ITEMS) fail("The full-item page exceeds the read-only validation budget.");
    const itemIds = new Set<string>();
    for (const item of turn.items) {
      if (!isRecord(item) || !validID(item.id) || itemIds.has(item.id) || typeof item.type !== "string" || !item.type || item.type.length > 128) fail("Full native items with distinct stable identities and kinds are required.");
      itemIds.add(item.id);
      // reasoning and other omitted text kinds still have valid native IDs.
      // Content rendering, truncation and gap notices remain the normalizer's job.
    }
  }
  return value as unknown as FullPage;
}

function validateRequest(method: string, params: RecordValue, sessionId: string): void {
  if (!validID(sessionId) || params.threadId !== sessionId) fail("Read-only polling must remain bound to its exact native Session.");
  if (method === "thread/read") {
    if (params.includeTurns !== false || Object.keys(params).some((key) => !["threadId", "includeTurns"].includes(key))) fail("Only the targeted read-only metadata request is allowed.");
  } else if (method === "thread/turns/list") {
    if (params.itemsView !== "full" || !["asc", "desc"].includes(String(params.sortDirection)) || !Number.isInteger(params.limit) || Number(params.limit) < 1 || Number(params.limit) > 20 ||
      ("cursor" in params && !validCursor(params.cursor)) || Object.keys(params).some((key) => !["threadId", "itemsView", "sortDirection", "limit", "cursor"].includes(key))) fail("Only bounded full-item read-only pagination is allowed.");
  } else fail("This operation is outside the read-only persisted-poll contract.");
}

function unavailable(reason: string): CodexFollowProbe { return { supported: false, reason }; }
function fail(reason: string): never { throw new CodexFollowCapabilityError(reason); }
function validID(value: unknown): value is string { return typeof value === "string" && value.length > 0 && value.length <= 256 && value.trim().length > 0 && !/[\u0000-\u001f\u007f]/.test(value); }
function validCursor(value: unknown): value is string { return typeof value === "string" && value.length > 0 && Buffer.byteLength(value) <= 8192; }
function nullableCursor(value: unknown): value is string | null { return value === null || validCursor(value); }
function jsonBytes(value: unknown): number { return Buffer.byteLength(JSON.stringify(value)); }
function isRecord(value: unknown): value is RecordValue { return typeof value === "object" && value !== null && !Array.isArray(value); }
