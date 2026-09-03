import { randomUUID } from "node:crypto";
import { setTimeout as delay } from "node:timers/promises";
import { describe, expect, it, vi } from "vitest";

import { JsonlRpcProcess, type JsonlRpcProcessOptions } from "../src/lib/jsonl-rpc-client.js";

// Dedicated Node children only; never a Provider or an existing host process.
function childScript(onTerm: string, onTrigger = "") {
  return `
    const readline = require('node:readline');
    const hold = setInterval(() => {}, 1000);
    let signals = 0;
    process.on('SIGTERM', () => { signals++; ${onTerm} });
    const rl = readline.createInterface({ input: process.stdin });
    rl.on('line', line => {
      const message = JSON.parse(line);
      if (message.method === 'ready') process.stdout.write(JSON.stringify({ id: message.id, result: { pid: process.pid } }) + '\\n');
      if (message.method === 'trigger') { ${onTrigger} }
      if (message.method === 'crash') process.exit(17);
    });
  `;
}

function clientFor(script: string, options: Partial<JsonlRpcProcessOptions> = {}) {
  return new JsonlRpcProcess({ command: process.execPath, args: ["-e", script], closeTimeoutMs: 2_000, ...options });
}

async function ready(client: JsonlRpcProcess): Promise<number> {
  client.start();
  const response = await client.request("ready", {}) as { pid: number };
  return response.pid;
}

describe("JSONL owned-process close confirmation", () => {
  it("waits for actual exit and merges concurrent close attempts", async () => {
    const exited = vi.fn();
    const signals: string[] = [];
    const client = clientFor(childScript("process.stderr.write('signal:' + signals + '\\n'); setTimeout(() => process.exit(0), 150);"), {
      onExit: exited, onStderr: (line) => signals.push(line),
    });
    const pid = await ready(client);
    try {
      const first = client.close();
      const second = client.close();
      expect(second).toBe(first);
      await delay(30);
      expect(exited).not.toHaveBeenCalled();
      expect(() => process.kill(pid, 0)).not.toThrow();
      await first;
      expect(exited).toHaveBeenCalledOnce();
      expect(signals).toEqual(["signal:1"]);
      expect(() => process.kill(pid, 0)).toThrow();
      await client.close();
      expect(exited).toHaveBeenCalledOnce();
    } finally { await client.close(); }
  });

  it("times out without declaring exit and retries the same still-owned process", async () => {
    const exited = vi.fn();
    const signals: string[] = [];
    const client = clientFor(childScript("process.stderr.write('signal:' + signals + '\\n'); if (signals >= 2) process.exit(0);"), {
      closeTimeoutMs: 100, onExit: exited, onStderr: (line) => signals.push(line),
    });
    const pid = await ready(client);
    try {
      await expect(client.close()).rejects.toThrow(/timed out.*subprocess exit.*unconfirmed/);
      expect(exited).not.toHaveBeenCalled();
      expect(() => process.kill(pid, 0)).not.toThrow();
      expect(() => client.start()).toThrow(/closing or closed/);
      await expect(client.request("ready", {})).rejects.toThrow(/closing or closed/);
      await client.close();
      expect(exited).toHaveBeenCalledOnce();
      expect(signals).toEqual(["signal:1", "signal:2"]);
      expect(() => process.kill(pid, 0)).toThrow();
    } finally { await client.close(); }
  });

  it("fences new writes and promptly rejects pending work throughout close", async () => {
    const client = clientFor(childScript("setTimeout(() => process.exit(0), 80);"));
    await ready(client);
    try {
      const pending = expect(client.request("never-replies", {})).rejects.toThrow(/is closing/);
      const closing = client.close();
      await expect(client.request("ready", {})).rejects.toThrow(/closing or closed/);
      expect(() => client.notify("new-work")).toThrow(/closing or closed/);
      expect(() => client.respond("old-request", {})).not.toThrow();
      expect(() => client.respondError("old-request", -1, "late")).not.toThrow();
      await Promise.all([pending, closing]);
      expect(() => client.notify("after-close")).toThrow(/closing or closed/);
      expect(() => client.start()).toThrow(/closing or closed/);
    } finally { await client.close(); }
  });

  it("drops late inbound routes and already-delivered asynchronous responses during close", async () => {
    let started!: () => void;
    let responded!: () => void;
    const routeStarted = new Promise<void>((resolve) => { started = resolve; });
    const responseFinished = new Promise<void>((resolve) => { responded = resolve; });
    const notifications = vi.fn();
    let closing: Promise<void> | undefined;
    let client!: JsonlRpcProcess;
    const onRequest = vi.fn((request: { id: string | number }) => {
      closing = client.close();
      started();
      setTimeout(() => {
        client.respond(request.id, {});
        client.respondError(request.id, -1, "late error");
        responded();
      }, 20);
    });
    client = clientFor(childScript(
      "process.stdout.write(JSON.stringify({id:'late',method:'approval',params:{}})+'\\n'); process.stdout.write(JSON.stringify({method:'late-notification'})+'\\n'); setTimeout(() => process.exit(0), 80);",
      "process.stdout.write(JSON.stringify({id:'first',method:'approval',params:{}})+'\\n');",
    ), { onRequest, onNotification: notifications });
    await ready(client);
    try {
      client.notify("trigger");
      await routeStarted;
      await Promise.all([responseFinished, closing]);
      expect(onRequest).toHaveBeenCalledOnce();
      expect(notifications).not.toHaveBeenCalled();
    } finally { await client.close(); }
  });

  it("settles an asynchronous spawn failure that has no exit event", async () => {
    const exited = vi.fn();
    const client = new JsonlRpcProcess({ command: `/private/tmp/teamcross-missing-${randomUUID()}`, args: [], onExit: exited, closeTimeoutMs: 100 });
    client.start();
    await expect(client.request("ready", {})).rejects.toThrow(/ENOENT/);
    await expect(client.close()).resolves.toBeUndefined();
    expect(exited).toHaveBeenCalledOnce();
    await client.close();
    expect(exited).toHaveBeenCalledOnce();
  });

  it("closes safely after a synchronous spawn rejection and before any start", async () => {
    const invalid = new JsonlRpcProcess({ command: "invalid\0command", args: [] });
    expect(() => invalid.start()).toThrow();
    await expect(invalid.close()).resolves.toBeUndefined();
    const neverStarted = clientFor(childScript("process.exit(0);"));
    await neverStarted.close();
    expect(() => neverStarted.start()).toThrow(/closing or closed/);
  });

  it("keeps explicit restart after an unexpected exit distinct from deliberate close", async () => {
    let crashed!: () => void;
    const crash = new Promise<void>((resolve) => { crashed = resolve; });
    const exited = vi.fn(() => crashed());
    const client = clientFor(childScript("process.exit(0);"), { onExit: exited });
    const firstPid = await ready(client);
    client.notify("crash");
    await crash;
    const secondPid = await ready(client);
    try {
      expect(secondPid).not.toBe(firstPid);
      await client.close();
      expect(exited).toHaveBeenCalledTimes(2);
    } finally { await client.close(); }
  });
});
