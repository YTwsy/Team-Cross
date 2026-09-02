#!/usr/bin/env node

import { pathToFileURL } from "node:url";

import { runStdioBridge } from "./stdio.js";

export * from "./protocol.js";
export * from "./server.js";

const entrypoint = process.argv[1];
if (entrypoint && import.meta.url === pathToFileURL(entrypoint).href) {
  runStdioBridge().catch((error: unknown) => {
    process.stderr.write(`teamcross-agent-bridge: ${error instanceof Error ? error.message : String(error)}\n`);
    process.exitCode = 1;
  });
}
