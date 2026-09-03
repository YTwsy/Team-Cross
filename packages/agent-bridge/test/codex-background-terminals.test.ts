import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { CodexBackgroundTerminals, nativeProcessExists } from "../src/lib/codex-background-terminals.js";

const terminal = { processId: "14338", itemId: "synthetic-exec-1", osPid: 2_000_000_001 };
const empty = { data: [], nextCursor: null };
function fixture() {
  const calls: Array<{ method: string; params: Record<string, unknown> }> = [];
  const behavior = {
    list: async (): Promise<unknown> => empty,
    terminate: async (): Promise<unknown> => ({ terminated: true }),
    exists: (_pid: number): boolean => false,
  };
  const gate = new CodexBackgroundTerminals("only-this-thread", async (method, params) => {
    calls.push({ method, params });
    if (method === "thread/backgroundTerminals/list") return await behavior.list();
    if (method === "thread/backgroundTerminals/terminate") return await behavior.terminate();
    throw new Error("unexpected RPC");
  }, (pid) => behavior.exists(pid));
  return { gate, behavior, calls };
}
beforeEach(() => vi.useFakeTimers());
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); });

describe("Codex background terminal exit gate", () => {
  it("confirms empty exact-Thread lists without executing or cleaning anything", async () => {
    const f = fixture(); await f.gate.confirmStopped(Date.now() + 50);
    expect(f.calls).toEqual(Array.from({ length: 2 }, () => ({ method: "thread/backgroundTerminals/list", params: { threadId: "only-this-thread", limit: 64 } })));
  });

  it.each([null, undefined, "123", 0, -1, 1, 1.5, 2_147_483_648, process.pid])("retains unverifiable PID %s across empty-list retries and never terminates it", async (osPid) => {
    const f = fixture(); f.behavior.list = async () => ({ data: [{ ...terminal, osPid }], nextCursor: null });
    await expect(f.gate.confirmStopped(Date.now() + 50)).rejects.toThrow(/no verifiable/);
    f.behavior.list = async () => empty;
    await expect(f.gate.confirmStopped(Date.now() + 50)).rejects.toThrow(/no verifiable/);
    expect(f.calls.every((call) => call.method.endsWith("/list"))).toBe(true);
  });

  it("requires actual disappearance even after terminate ACK and Provider registry removal, retaining IDs for retry", async () => {
    const f = fixture(); let live = true; let registered = true;
    f.behavior.exists = (pid) => { expect(pid).toBe(terminal.osPid); return live; };
    f.behavior.list = async () => registered ? { data: [terminal], nextCursor: null } : empty;
    f.behavior.terminate = async () => { registered = false; return { terminated: true }; };
    const failed = expect(f.gate.confirmStopped(Date.now() + 50)).rejects.toThrow(/process exit/);
    await vi.advanceTimersByTimeAsync(51); await failed;
    const retried = expect(f.gate.confirmStopped(Date.now() + 50)).rejects.toThrow(/process exit/);
    await vi.advanceTimersByTimeAsync(51); await retried;
    expect(f.calls.filter((call) => call.method.endsWith("/terminate"))).toEqual(Array.from({ length: 2 }, () => ({
      method: "thread/backgroundTerminals/terminate", params: { threadId: "only-this-thread", processId: terminal.processId },
    })));
    live = false; await f.gate.confirmStopped(Date.now() + 50);
  });

  it("accepts only Provider termination followed by OS exit and a fresh empty list", async () => {
    const f = fixture(); let live = true;
    f.behavior.list = async () => live ? { data: [terminal], nextCursor: null } : empty;
    f.behavior.exists = () => live;
    f.behavior.terminate = async () => { live = false; return { terminated: true }; };
    await f.gate.confirmStopped(Date.now() + 50);
    expect(f.calls.map((call) => call.method)).toEqual(["thread/backgroundTerminals/list", "thread/backgroundTerminals/terminate", "thread/backgroundTerminals/list"]);
  });

  it.each([{}, null, { terminated: false }, { terminated: "true" }])("rejects unconfirmed terminate result %j", async (response) => {
    const f = fixture(); f.behavior.list = async () => ({ data: [terminal], nextCursor: null });
    f.behavior.exists = () => true; f.behavior.terminate = async () => response;
    await expect(f.gate.confirmStopped(Date.now() + 50)).rejects.toThrow(/not acknowledged/);
  });

  it.each([{}, null, { data: [], nextCursor: undefined }, { data: "empty", nextCursor: null }, { data: [], nextCursor: "x".repeat(4097) }])("rejects malformed list %j", async (response) => {
    const f = fixture(); f.behavior.list = async () => response;
    await expect(f.gate.confirmStopped(Date.now() + 50)).rejects.toThrow(/malformed/);
  });

  it("checks bounded pagination and captures every terminal identity", async () => {
    const f = fixture(); let page = 0;
    f.behavior.list = async () => ++page === 1 ? { data: [terminal], nextCursor: "next" }
      : page === 2 ? { data: [{ ...terminal, processId: "other", itemId: "other", osPid: null }], nextCursor: null } : empty;
    await expect(f.gate.confirmStopped(Date.now() + 50)).rejects.toThrow(/no verifiable/);
    expect(f.calls[1]?.params).toEqual({ threadId: "only-this-thread", limit: 64, cursor: "next" });
    await expect(f.gate.confirmStopped(Date.now() + 50)).rejects.toThrow(/no verifiable/);
  });

  it("does not allow a process identity to change across retries", async () => {
    const f = fixture(); f.behavior.list = async () => ({ data: [terminal], nextCursor: null });
    f.behavior.exists = () => true; f.behavior.terminate = async () => ({ terminated: false });
    await expect(f.gate.confirmStopped(Date.now() + 50)).rejects.toThrow(/not acknowledged/);
    f.behavior.list = async () => ({ data: [{ ...terminal, itemId: "reused-process" }], nextCursor: null });
    await expect(f.gate.confirmStopped(Date.now() + 50)).rejects.toThrow(/no verifiable/);
  });

  it("rejects a new terminal seen during final verification", async () => {
    const f = fixture(); let page = 0;
    f.behavior.list = async () => ++page === 1 ? empty : { data: [terminal], nextCursor: null };
    await expect(f.gate.confirmStopped(Date.now() + 50)).rejects.toThrow(/appeared during close/);
  });

  it("bounds unsupported or hung RPCs without revealing Provider error bodies", async () => {
    const f = fixture(); f.behavior.list = async () => { throw new Error("PRIVATE command/config body"); };
    await expect(f.gate.confirmStopped(Date.now() + 50)).rejects.toThrow("Codex background terminal RPC failed or timed out; termination is unconfirmed");
    f.behavior.list = () => new Promise(() => {});
    const failed = expect(f.gate.confirmStopped(Date.now() + 50)).rejects.toThrow(/timed out/);
    await vi.advanceTimersByTimeAsync(51); await failed;
  });

  it("uses only signal zero and treats only ESRCH as exit, not EPERM", () => {
    const kill = vi.spyOn(process, "kill").mockReturnValue(true);
    expect(nativeProcessExists(terminal.osPid)).toBe(true);
    expect(kill).toHaveBeenCalledWith(terminal.osPid, 0);
    kill.mockImplementation(() => { throw Object.assign(new Error("not found"), { code: "ESRCH" }); });
    expect(nativeProcessExists(terminal.osPid)).toBe(false);
    kill.mockImplementation(() => { throw Object.assign(new Error("private system message"), { code: "EPERM" }); });
    expect(() => nativeProcessExists(terminal.osPid)).toThrow("Codex background terminal process exit cannot be verified");
  });
});
