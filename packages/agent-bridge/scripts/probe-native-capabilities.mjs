#!/usr/bin/env node
import { execFile } from "node:child_process";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { promisify } from "node:util";

// Static protocol inspection only. This deliberately never connects to an existing daemon,
// lists histories, starts/resumes a session, sends a Turn or changes the user's configuration.
if (process.env.TEAMCROSS_NATIVE_CAPABILITY_PROBE !== "1") {
  process.stderr.write("Set TEAMCROSS_NATIVE_CAPABILITY_PROBE=1 to inspect the installed Codex protocol. This does not run native-session acceptance tests.\n");
  process.exitCode = 2;
} else {
  const run = promisify(execFile);
  const binary = process.env.TEAMCROSS_CODEX_BIN || "codex";
  const directory = await mkdtemp(join(tmpdir(), "teamcross-native-protocol-"));
  try {
    const options = { timeout: 30_000, maxBuffer: 1024 * 1024 };
    const { stdout: version } = await run(binary, ["--version"], options);
    await run(binary, ["app-server", "generate-ts", "--experimental", "--out", directory], options);
    const requests = await readFile(join(directory, "ClientRequest.ts"), "utf8");
    const resume = await readFile(join(directory, "v2", "ThreadResumeParams.ts"), "utf8");
    const methods = [...requests.matchAll(/"method":\s*"([^"]+)"/g)].map((match) => match[1]);
    const methodPresent = (method) => methods.includes(method);
    process.stdout.write(JSON.stringify({
      provider: "codex", version: version.trim(), probe: "static-protocol-only",
      observed: {
        read: methodPresent("thread/read"),
        paginatedHistory: methodPresent("thread/turns/list") && methodPresent("thread/items/list"),
        resumeMethod: methodPresent("thread/resume"),
        resumeCwdOverride: /\bcwd\?:/.test(resume),
        unsubscribe: methodPresent("thread/unsubscribe"),
      },
      acceptance: { cli: "not-run", desktop: "not-run" },
      capabilities: {
        follow: false, open: false, resume: false, takeControl: false,
        reason: "Protocol presence does not prove passive Follow, exact UI navigation, isolated same-ID restoration, exclusive native Writer fencing or handback. Separate CLI and Desktop acceptance is required.",
      },
    }, null, 2) + "\n");
  } catch (error) {
    process.stderr.write(`Native protocol probe failed: ${error instanceof Error ? error.message : String(error)}\n`);
    process.exitCode = 1;
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
}
