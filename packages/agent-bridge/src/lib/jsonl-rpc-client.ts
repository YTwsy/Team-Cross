import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { createInterface } from "node:readline";

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

  constructor(private readonly options: JsonlRpcProcessOptions) {
    this.timeoutMs = options.requestTimeoutMs ?? 30_000;
  }

  start(): void {
    if (this.child) return;
    const child = spawn(this.options.command, this.options.args, {
      cwd: this.options.cwd,
      env: this.options.env,
      stdio: ["pipe", "pipe", "pipe"],
    });
    this.child = child;

    const stdout = createInterface({ input: child.stdout, crlfDelay: Infinity });
    stdout.on("line", (line) => this.onLine(line));

    const stderr = createInterface({ input: child.stderr, crlfDelay: Infinity });
    stderr.on("line", (line) => this.options.onStderr?.(line));

    child.once("error", (cause) => this.failAll(new Error(
      `failed to start ${this.options.command}: ${cause.message}`,
      { cause },
    )));
    child.once("exit", (code, signal) => {
      const error = new Error(
        `${this.options.command} exited (${signal ? `signal ${signal}` : `code ${String(code)}`})`,
      );
      this.failAll(error);
      this.child = undefined;
      this.options.onExit?.(error);
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
    this.write({ jsonrpc: "2.0", id, method, params });
    return await result;
  }

  notify(method: string, params?: unknown): void {
    this.runningChild();
    this.write({ jsonrpc: "2.0", method, ...(params === undefined ? {} : { params }) });
  }

  respond(id: RpcId, result: unknown): void {
    this.runningChild();
    this.write({ jsonrpc: "2.0", id, result });
  }

  respondError(id: RpcId, code: number, message: string, data?: unknown): void {
    this.runningChild();
    this.write({
      jsonrpc: "2.0",
      id,
      error: { code, message, ...(data === undefined ? {} : { data }) },
    });
  }

  async close(): Promise<void> {
    const child = this.child;
    if (!child) return;
    this.child = undefined;
    child.stdin.end();
    if (child.exitCode === null && child.signalCode === null) {
      child.kill("SIGTERM");
    }
    this.failAll(new Error(`${this.options.command} was closed`));
  }

  private runningChild(): ChildProcessWithoutNullStreams {
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
