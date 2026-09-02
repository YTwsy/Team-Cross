import { createInterface } from "node:readline";
import type { Writable } from "node:stream";

import { BridgeRpcError, BridgeServer } from "./server.js";
import type { JsonRpcFailure, JsonRpcRequest, JsonRpcSuccess } from "./protocol.js";

type JsonRecord = Record<string, unknown>;

export interface StdioBridgeOptions {
  input?: NodeJS.ReadableStream;
  output?: Writable;
  diagnostics?: Writable;
  server?: BridgeServer;
}

export async function runStdioBridge(options: StdioBridgeOptions = {}): Promise<void> {
  const input = options.input ?? process.stdin;
  const output = options.output ?? process.stdout;
  const diagnostics = options.diagnostics ?? process.stderr;
  const server = options.server ?? new BridgeServer({
    onDiagnostic: (message) => diagnostics.write(`${message}\n`),
  });
  server.setEventSink((event) => {
    writeLine(output, { jsonrpc: "2.0", method: "event", params: event });
  });

  const lines = createInterface({ input, crlfDelay: Infinity });
  const pending = new Set<Promise<void>>();
  for await (const line of lines) {
    if (line.trim() === "") continue;
    const operation = handleLine(server, output, line).finally(() => pending.delete(operation));
    pending.add(operation);
  }
  await Promise.allSettled([...pending]);
  await server.shutdown();
}

async function handleLine(server: BridgeServer, output: Writable, line: string): Promise<void> {
  let request: JsonRpcRequest;
  try {
    const parsed: unknown = JSON.parse(line);
    if (!isRequest(parsed)) throw new BridgeRpcError(-32600, "invalid JSON-RPC request");
    request = parsed;
  } catch (error) {
    const failure: JsonRpcFailure = {
      jsonrpc: "2.0",
      id: null,
      error: rpcError(error, -32700),
    };
    writeLine(output, failure);
    return;
  }

  try {
    const result = await server.handle(request.method, request.params);
    const success: JsonRpcSuccess = { jsonrpc: "2.0", id: request.id, result };
    writeLine(output, success);
  } catch (error) {
    const failure: JsonRpcFailure = {
      jsonrpc: "2.0",
      id: request.id,
      error: rpcError(error, -32603),
    };
    writeLine(output, failure);
  }
}

function isRequest(value: unknown): value is JsonRpcRequest {
  return isRecord(value)
    && value.jsonrpc === "2.0"
    && (typeof value.id === "string" || typeof value.id === "number")
    && typeof value.method === "string";
}

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function rpcError(error: unknown, fallbackCode: number): JsonRpcFailure["error"] {
  if (error instanceof BridgeRpcError) {
    return {
      code: error.code,
      message: error.message,
      ...(error.data === undefined ? {} : { data: error.data }),
    };
  }
  return {
    code: fallbackCode,
    message: error instanceof Error ? error.message : String(error),
  };
}

function writeLine(output: Writable, value: unknown): void {
  output.write(`${JSON.stringify(value)}\n`);
}
