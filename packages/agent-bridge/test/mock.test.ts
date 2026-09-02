import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";

import type { BridgeEvent } from "../src/protocol.js";
import { BridgeServer } from "../src/server.js";

const temporaryDirectories: string[] = [];

afterEach(async () => {
  await Promise.all(temporaryDirectories.splice(0).map(async (directory) => {
    await rm(directory, { recursive: true, force: true });
  }));
});

describe("mock bridge run", () => {
  it("emits a complete turn and makes an isolated file change", async () => {
    const worktree = await temporaryDirectory();
    const events: BridgeEvent[] = [];
    const server = new BridgeServer({ onEvent: (event) => events.push(event) });

    const descriptor = await server.handle("runs.create", {
      runId: "mock-run",
      provider: "mock",
      worktree,
      networkEnabled: false,
    });
    expect(descriptor).toMatchObject({ runId: "mock-run", provider: "mock", status: "idle" });

    const result = await server.handle("runs.send", {
      runId: "mock-run",
      message: "change one file",
    }) as { turnId: string };
    await waitFor(() => events.some((event) => (
      event.type === "turn.completed" && event.turnId === result.turnId
    )));

    expect(events.map((event) => event.type)).toEqual(expect.arrayContaining([
      "run.started",
      "turn.started",
      "message.delta",
      "message.completed",
      "tool.started",
      "tool.completed",
      "file.changed",
      "turn.completed",
    ]));
    expect(await readFile(join(worktree, ".teamcross-mock-output.txt"), "utf8"))
      .toContain("prompt=change one file");
    await server.shutdown();
  });

  it("round-trips a blocking input request", async () => {
    const worktree = await temporaryDirectory();
    const events: BridgeEvent[] = [];
    const server = new BridgeServer({ onEvent: (event) => events.push(event) });
    await server.handle("runs.create", { runId: "input-run", provider: "mock", worktree });
    await server.handle("runs.send", { runId: "input-run", message: "[[input]] choose" });

    await waitFor(() => events.some((event) => event.type === "input.requested"));
    const request = events.find((event) => event.type === "input.requested");
    expect(request?.inputRequestId).toBeTruthy();
    await server.handle("runs.respondInput", {
      runId: "input-run",
      inputRequestId: request?.inputRequestId,
      response: "continue",
    });
    await waitFor(() => events.some((event) => event.type === "turn.completed"));
    expect(events.some((event) => event.type === "input.resolved")).toBe(true);
    await server.shutdown();
  });
});

async function temporaryDirectory(): Promise<string> {
  const directory = await mkdtemp(join(tmpdir(), "teamcross-bridge-test-"));
  temporaryDirectories.push(directory);
  return directory;
}

async function waitFor(predicate: () => boolean, timeoutMs = 2_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!predicate()) {
    if (Date.now() > deadline) throw new Error("timed out waiting for bridge event");
    await new Promise((resolve) => setTimeout(resolve, 5));
  }
}
