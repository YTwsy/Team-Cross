import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { createInterface } from "node:readline";
import { closeDeadline, withinCloseDeadline } from "./close-deadline.js";

type RpcId = string | number;

interface PendingRequest {
  resolve: (value: unknown) => void;
  reject: (error: Error) => void;
  timer: NodeJS.Timeout;
}

export interface InboundRpcRequest {
  id: RpcId;
  method: string;
  params: unknown;
}

export interface RpcNotification {
  method: string;
  params: unknown;
}

export interface JsonlRpcProcessOptions {
  command: string;
  args: string[];
  cwd?: string;
  env?: NodeJS.ProcessEnv;
  requestTimeoutMs?: number;
  closeTimeoutMs?: number;
  onNotification?: (notification: RpcNotification) => void;
  onRequest?: (request: InboundRpcRequest) => void;
  onStderr?: (line: string) => void;
  onExit?: (error: Error) => void;
}

export class JsonlRpcProcess {
  private child: ChildProcessWithoutNullStreams | undefined;
  private nextId = 1;
  private readonly pending = new Map<RpcId, PendingRequest>();
  private readonly timeoutMs: number;
  private closing = false;
  private closeAttempt: Promise<void> | undefined;
  private childExited: Promise<void> | undefined;

  constructor(private readonly options: JsonlRpcProcessOptions) {
    this.timeoutMs = options.requestTimeoutMs ?? 30_000;
  }

  start(): void {
    if (this.closing) throw new Error(`${this.options.command} is closing or closed`);
    if (this.child) return;
    const child = spawn(this.options.command, this.options.args, {
      cwd: this.options.cwd,
      env: this.options.env,
      stdio: ["pipe", "pipe", "pipe"],
    });
    this.child = child;
    let resolveExit!: () => void;
    this.childExited = new Promise<void>((resolve) => { resolveExit = resolve; });
    let finished = false;
    const finish = (error: Error) => {
      if (finished) return;
      finished = true;
      if (this.child === child) {
        this.failAll(error);
        this.child = undefined;
      }
      resolveExit();
      this.options.onExit?.(error);
    };

    const stdout = createInterface({ input: child.stdout, crlfDelay: Infinity });
    stdout.on("line", (line) => {
      if (this.child === child && !this.closing) this.onLine(line);
    });

    const stderr = createInterface({ input: child.stderr, crlfDelay: Infinity });
    stderr.on("line", (line) => this.options.onStderr?.(line));

    // A failed spawn has no PID and may never emit exit. An error signalling an
    // existing child is NOT proof of exit; keep that child fenced and retryable.
    child.on("error", (cause) => {
      const error = new Error(`failed to run ${this.options.command}: ${cause.message}`, { cause });
      if (child.pid === undefined) finish(error);
      else if (this.child === child) this.failAll(error);
    });
    child.stdin.on("error", (cause) => {
      if (this.child === child) this.failAll(new Error(`${this.options.command} stdin failed`, { cause }));
    });
    child.once("exit", (code, signal) => {
      const error = new Error(
        `${this.options.command} exited (${signal ? `signal ${signal}` : `code ${String(code)}`})`,
      );
      finish(error);
    });
  }

  async request(method: string, params: unknown, timeoutMs = this.timeoutMs): Promise<unknown> {
    this.runningChild();
    const id = this.nextId++;
    const result = new Promise<unknown>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`JSON-RPC request timed out: ${method}`));
      }, timeoutMs);
      timer.unref();
      this.pending.set(id, { resolve, reject, timer });
    });
    try {
      this.write({ jsonrpc: "2.0", id, method, params });
    } catch (cause) {
      const pending = this.pending.get(id);
      if (pending) {
        clearTimeout(pending.timer);
        this.pending.delete(id);
        pending.reject(cause instanceof Error ? cause : new Error(String(cause)));
      }
    }
    return await result;
  }

  notify(method: string, params?: unknown): void {
    this.runningChild();
    this.write({ jsonrpc: "2.0", method, ...(params === undefined ? {} : { params }) });
  }

  respond(id: RpcId, result: unknown): void {
    // An already delivered inbound request may complete asynchronously during
    // close. Discard its late response without I/O or an uncaught callback error.
    if (this.closing) return;
    this.runningChild();
    this.write({ jsonrpc: "2.0", id, result });
  }

  respondError(id: RpcId, code: number, message: string, data?: unknown): void {
    if (this.closing) return;
    this.runningChild();
    this.write({
      jsonrpc: "2.0",
      id,
      error: { code, message, ...(data === undefined ? {} : { data }) },
    });
  }

  close(): Promise<void> {
    if (this.closeAttempt) return this.closeAttempt;
    this.closing = true;
    this.failAll(new Error(`${this.options.command} is closing`));
    const child = this.child;
    if (!child) return Promise.resolve();
    const pending = this.closeChild(child, this.childExited!).finally(() => {
      if (this.closeAttempt === pending) this.closeAttempt = undefined;
    });
    this.closeAttempt = pending;
    return pending;
  }

  private async closeChild(child: ChildProcessWithoutNullStreams, exited: Promise<void>): Promise<void> {
    const deadline = closeDeadline(this.options.closeTimeoutMs);
    if (!child.stdin.destroyed && !child.stdin.writableEnded) child.stdin.end();
    if (child.exitCode === null && child.signalCode === null) {
      // Only signal the ChildProcess this instance spawned. A sent signal is
      // not an acknowledgement; never discard its identity before actual exit.
      child.kill("SIGTERM");
    }
    await withinCloseDeadline(exited, deadline, "subprocess exit");
  }

  private runningChild(): ChildProcessWithoutNullStreams {
    if (this.closing) throw new Error(`${this.options.command} is closing or closed`);
    if (!this.child) throw new Error(`${this.options.command} is not running`);
    return this.child;
  }

  private write(message: unknown): void {
    this.runningChild().stdin.write(`${JSON.stringify(message)}\n`);
  }

  private onLine(line: string): void {
    if (line.trim() === "") return;
    let message: Record<string, unknown>;
    try {
      const parsed: unknown = JSON.parse(line);
      if (!isRecord(parsed)) throw new Error("message is not an object");
      message = parsed;
    } catch (cause) {
      this.options.onStderr?.(`invalid JSON-RPC output: ${String(cause)}: ${line}`);
      return;
    }

    const id = isRpcId(message.id) ? message.id : undefined;
    if (id !== undefined && typeof message.method === "string") {
      this.options.onRequest?.({ id, method: message.method, params: message.params });
      return;
    }
    if (id !== undefined && ("result" in message || "error" in message)) {
      const pending = this.pending.get(id);
      if (!pending) return;
      clearTimeout(pending.timer);
      this.pending.delete(id);
      if ("error" in message && message.error !== undefined) {
        pending.reject(rpcError(message.error));
      } else {
        pending.resolve(message.result);
      }
      return;
    }
    if (typeof message.method === "string") {
      this.options.onNotification?.({ method: message.method, params: message.params });
    }
  }

  private failAll(error: Error): void {
    for (const pending of this.pending.values()) {
      clearTimeout(pending.timer);
      pending.reject(error);
    }
    this.pending.clear();
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isRpcId(value: unknown): value is RpcId {
  return typeof value === "string" || typeof value === "number";
}

function rpcError(value: unknown): Error {
  if (!isRecord(value)) return new Error(`JSON-RPC error: ${JSON.stringify(value)}`);
  const message = typeof value.message === "string" ? value.message : JSON.stringify(value);
  const code = typeof value.code === "number" ? ` (${value.code})` : "";
  const error = new Error(`JSON-RPC error${code}: ${message}`);
  if ("data" in value) Object.assign(error, { data: value.data });
  return error;
}
