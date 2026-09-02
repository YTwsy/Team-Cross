import { appendFile } from "node:fs/promises";
import { join } from "node:path";
import { randomUUID } from "node:crypto";

import { createBridgeEvent, type RunDescriptor } from "../protocol.js";
import type { AgentAdapter, AgentRun, EventSink } from "./types.js";
import { serializeImportedContext } from "./types.js";

export class MockAdapter implements AgentAdapter {
  readonly provider = "mock" as const;

  async listStored(): Promise<never[]> {
    return [];
  }

  async readStored(): Promise<never> {
    throw new Error("the mock provider has no stored sessions");
  }

  async createRun(
    params: Parameters<AgentAdapter["createRun"]>[0],
    emit: EventSink,
  ): Promise<AgentRun> {
    const run = new MockRun(params.runId, params.worktree, Boolean(params.networkEnabled), emit);
    run.announce();
    if (params.initialPrompt) void run.send(params.initialPrompt);
    return run;
  }

  async shutdown(): Promise<void> {}
}

class MockRun implements AgentRun {
  readonly descriptor: RunDescriptor;
  private importedContext: string[] = [];
  private active:
    | { controller: AbortController; promise: Promise<void>; turnId: string }
    | undefined;
  private pendingInput:
    | { id: string; resolve: (value: unknown) => void; reject: (error: Error) => void }
    | undefined;

  constructor(
    runId: string,
    worktree: string,
    networkEnabled: boolean,
    private readonly emit: EventSink,
  ) {
    this.descriptor = {
      runId,
      provider: "mock",
      sessionId: `mock-${runId}`,
      worktree,
      networkEnabled,
      status: "starting",
      capabilities: { send: true, steer: true, interrupt: true, inputResponse: true },
    };
  }

  announce(): void {
    this.descriptor.status = "idle";
    this.emit(createBridgeEvent("mock", this.descriptor.runId, "run.started", {
      data: { sessionId: this.descriptor.sessionId, mock: true },
    }));
    this.status("idle");
  }

  async importContext(source: Record<string, unknown>, context: unknown): Promise<void> {
    this.importedContext.push(serializeImportedContext(source, context));
  }

  async send(message: string): Promise<{ turnId: string }> {
    if (this.descriptor.status === "closed") throw new Error("run is closed");
    if (this.active) throw new Error("a turn is already active; use runs.steer");
    const turnId = randomUUID();
    const controller = new AbortController();
    const promise = this.executeTurn(turnId, message, controller.signal).finally(() => {
      if (this.active?.turnId === turnId) this.active = undefined;
    });
    this.active = { controller, promise, turnId };
    return { turnId };
  }

  async steer(message: string): Promise<{ turnId: string }> {
    await this.interrupt();
    return await this.send(message);
  }

  async interrupt(): Promise<void> {
    const active = this.active;
    if (!active) return;
    active.controller.abort();
    await active.promise;
  }

  async respondInput(inputRequestId: string, response: unknown): Promise<void> {
    const pending = this.pendingInput;
    if (!pending || pending.id !== inputRequestId) {
      throw new Error(`unknown mock input request: ${inputRequestId}`);
    }
    this.pendingInput = undefined;
    pending.resolve(response);
    this.emit(createBridgeEvent("mock", this.descriptor.runId, "input.resolved", {
      inputRequestId,
      data: { response },
    }));
  }

  async close(): Promise<void> {
    if (this.descriptor.status === "closed") return;
    await this.interrupt();
    this.pendingInput?.reject(new Error("run closed"));
    this.pendingInput = undefined;
    this.descriptor.status = "closed";
    this.emit(createBridgeEvent("mock", this.descriptor.runId, "run.closed", {
      data: {},
    }));
  }

  private async executeTurn(turnId: string, rawMessage: string, signal: AbortSignal): Promise<void> {
    this.descriptor.status = "running";
    this.emit(createBridgeEvent("mock", this.descriptor.runId, "turn.started", {
      turnId,
      data: { trigger: "send" },
    }));
    this.status("running", turnId);

    try {
      await abortableDelay(4, signal);
      let message = rawMessage;
      if (this.importedContext.length > 0) {
        message = `${this.importedContext.splice(0).join("\n\n")}\n\n${message}`;
      }

      let inputResponse: unknown;
      if (rawMessage.includes("[[input]]")) {
        const inputRequestId = `mock-input-${turnId}`;
        inputResponse = await this.waitForInput(inputRequestId, turnId, signal);
      }

      const output = `Mock Agent received: ${rawMessage}`;
      const midpoint = Math.max(1, Math.floor(output.length / 2));
      for (const delta of [output.slice(0, midpoint), output.slice(midpoint)]) {
        if (delta === "") continue;
        await abortableDelay(3, signal);
        this.emit(createBridgeEvent("mock", this.descriptor.runId, "message.delta", {
          turnId,
          messageId: `mock-message-${turnId}`,
          data: { delta },
        }));
      }

      this.emit(createBridgeEvent("mock", this.descriptor.runId, "message.completed", {
        turnId,
        messageId: `mock-message-${turnId}`,
        data: { text: output },
      }));

      const toolId = `mock-write-${turnId}`;
      const relativePath = ".teamcross-mock-output.txt";
      this.emit(createBridgeEvent("mock", this.descriptor.runId, "tool.started", {
        turnId,
        toolId,
        data: { name: "Write", input: { path: relativePath } },
      }));
      await appendFile(
        join(this.descriptor.worktree, relativePath),
        [
          `turn=${turnId}`,
          `prompt=${message.replaceAll("\n", "\\n")}`,
          ...(inputResponse === undefined
            ? []
            : [`input=${JSON.stringify(inputResponse)}`]),
          "",
        ].join("\n"),
        "utf8",
      );
      this.emit(createBridgeEvent("mock", this.descriptor.runId, "tool.completed", {
        turnId,
        toolId,
        data: { name: "Write", success: true },
      }));
      this.emit(createBridgeEvent("mock", this.descriptor.runId, "file.changed", {
        turnId,
        data: { path: relativePath, kind: "write" },
      }));
      this.emit(createBridgeEvent("mock", this.descriptor.runId, "turn.completed", {
        turnId,
        data: { status: "completed" },
      }));
    } catch (error) {
      if (signal.aborted) {
        this.emit(createBridgeEvent("mock", this.descriptor.runId, "turn.completed", {
          turnId,
          data: { status: "interrupted" },
        }));
      } else {
        this.emit(createBridgeEvent("mock", this.descriptor.runId, "run.error", {
          turnId,
          data: { message: errorMessage(error) },
        }));
      }
    } finally {
      if (this.pendingInput?.id === `mock-input-${turnId}`) {
        this.pendingInput.reject(new Error("input request cancelled"));
        this.pendingInput = undefined;
      }
      this.descriptor.status = "idle";
      this.status("idle", turnId);
    }
  }

  private async waitForInput(
    inputRequestId: string,
    turnId: string,
    signal: AbortSignal,
  ): Promise<unknown> {
    this.emit(createBridgeEvent("mock", this.descriptor.runId, "input.requested", {
      turnId,
      inputRequestId,
      data: {
        questions: [{ id: "answer", question: "Mock input requested" }],
        blocking: true,
      },
    }));
    return await new Promise<unknown>((resolve, reject) => {
      const onAbort = (): void => reject(new Error("input request interrupted"));
      signal.addEventListener("abort", onAbort, { once: true });
      this.pendingInput = {
        id: inputRequestId,
        resolve: (value) => {
          signal.removeEventListener("abort", onAbort);
          resolve(value);
        },
        reject: (error) => {
          signal.removeEventListener("abort", onAbort);
          reject(error);
        },
      };
    });
  }

  private status(status: RunDescriptor["status"], turnId?: string): void {
    this.emit(createBridgeEvent("mock", this.descriptor.runId, "run.status", {
      ...(turnId === undefined ? {} : { turnId }),
      data: { status },
    }));
  }
}

async function abortableDelay(ms: number, signal: AbortSignal): Promise<void> {
  if (signal.aborted) throw new Error("aborted");
  await new Promise<void>((resolve, reject) => {
    const timer = setTimeout(() => {
      signal.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    const onAbort = (): void => {
      clearTimeout(timer);
      reject(new Error("aborted"));
    };
    signal.addEventListener("abort", onAbort, { once: true });
  });
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
