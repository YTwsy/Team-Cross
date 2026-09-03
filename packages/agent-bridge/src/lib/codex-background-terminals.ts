import { withinCloseDeadline } from "./close-deadline.js";

type Request = (method: string, params: Record<string, unknown>, timeoutMs?: number) => Promise<unknown>;
type Terminal = { processId: string; itemId: string; osPid: number | null };
const MAX_TERMINALS = 256;
const PAGE_SIZE = 64;

/** A zero signal is observation only. Permission errors are never evidence of exit. */
export function nativeProcessExists(pid: number): boolean {
  try { process.kill(pid, 0); return true; }
  catch (error) {
    if (isRecord(error) && error.code === "ESRCH") return false;
    throw new Error("Codex background terminal process exit cannot be verified");
  }
}

/** Scope is only terminals registered to this private, locally bound managed Thread. */
export class CodexBackgroundTerminals {
  // Keep identities across failed closes: Provider cleanup can remove its registry
  // before an OS process exits, so a later empty list cannot erase our uncertainty.
  private readonly observed = new Map<string, Terminal>();
  private identityUncertain = false;

  constructor(
    private readonly threadId: string,
    private readonly request: Request,
    private readonly processExists: (pid: number) => boolean = nativeProcessExists,
  ) {}

  async confirmStopped(deadline: number): Promise<void> {
    await this.list(deadline);
    if (this.identityUncertain || [...this.observed.values()].some((terminal) => terminal.osPid === null)) {
      // Do not clean/terminate without a verifiable identity. Current Codex builds
      // may report osPid:null; retaining their registry permits later diagnostics.
      throw new Error("Codex background terminal has no verifiable local process identity; termination is unconfirmed");
    }
    for (const terminal of this.observed.values()) {
      if (!this.processExists(terminal.osPid!)) continue;
      const response = await this.call("thread/backgroundTerminals/terminate", {
        threadId: this.threadId, processId: terminal.processId,
      }, deadline);
      if (!isRecord(response) || response.terminated !== true) {
        throw new Error("Codex background terminal termination was not acknowledged; termination is unconfirmed");
      }
    }
    // terminated:true and item/completed are not OS exit receipts. Only ESRCH for
    // every previously observed PID permits this gate to advance. PID reuse fails
    // conservatively. No terminating signal, process enumeration, or OS kill is used.
    while ([...this.observed.values()].some((terminal) => this.processExists(terminal.osPid!))) {
      await withinCloseDeadline(new Promise<void>((resolve) => setTimeout(resolve, 20)), deadline, "background terminal process exit");
    }
    if ((await this.list(deadline)).length !== 0) {
      throw new Error("Codex background terminal appeared during close; termination is unconfirmed");
    }
  }

  private async list(deadline: number): Promise<Terminal[]> {
    let cursor: string | undefined;
    const cursors = new Set<string>();
    const found: Terminal[] = [];
    const pageIds = new Set<string>();
    for (let page = 0; page < MAX_TERMINALS / PAGE_SIZE; page++) {
      const response = await this.call("thread/backgroundTerminals/list", {
        threadId: this.threadId, limit: PAGE_SIZE, ...(cursor === undefined ? {} : { cursor }),
      }, deadline);
      if (!isRecord(response) || !Array.isArray(response.data) || response.data.length > PAGE_SIZE
        || !(response.nextCursor === null || validId(response.nextCursor, 4096))) {
        throw new Error("Codex background terminal list is unsupported or malformed; termination is unconfirmed");
      }
      for (const raw of response.data) {
        if (!isRecord(raw) || !validId(raw.processId, 128) || !validId(raw.itemId, 256) || pageIds.has(raw.processId)) {
          this.identityUncertain = true;
          throw new Error("Codex background terminal identity is malformed; termination is unconfirmed");
        }
        const osPid = Number.isSafeInteger(raw.osPid) && Number(raw.osPid) > 1 && Number(raw.osPid) <= 2_147_483_647
          && raw.osPid !== process.pid ? Number(raw.osPid) : null;
        const terminal: Terminal = { processId: raw.processId, itemId: raw.itemId, osPid };
        const previous = this.observed.get(terminal.processId);
        if (previous && (previous.itemId !== terminal.itemId || previous.osPid !== terminal.osPid)) this.identityUncertain = true;
        if (!previous) {
          if (this.observed.size >= MAX_TERMINALS) {
            this.identityUncertain = true;
            throw new Error("Codex background terminal identity budget exceeded; termination is unconfirmed");
          }
          this.observed.set(terminal.processId, terminal);
        }
        pageIds.add(terminal.processId);
        found.push(terminal);
      }
      if (response.nextCursor === null) return found;
      if (cursors.has(response.nextCursor)) throw new Error("Codex background terminal pagination repeated; termination is unconfirmed");
      cursors.add(response.nextCursor);
      cursor = response.nextCursor;
    }
    throw new Error("Codex background terminal pagination exceeded its limit; termination is unconfirmed");
  }

  private async call(method: string, params: Record<string, unknown>, deadline: number): Promise<unknown> {
    if (Date.now() >= deadline) throw new Error("Codex background terminal close deadline expired; termination is unconfirmed");
    // Do not include Provider error bodies: they can echo commands or local paths.
    try {
      return await withinCloseDeadline(this.request(method, params, Math.max(1, deadline - Date.now())), deadline, "background terminal RPC");
    } catch {
      throw new Error("Codex background terminal RPC failed or timed out; termination is unconfirmed");
    }
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
function validId(value: unknown, maxBytes: number): value is string {
  return typeof value === "string" && value.length > 0 && Buffer.byteLength(value, "utf8") <= maxBytes && !/[\u0000-\u001f\u007f]/.test(value);
}
