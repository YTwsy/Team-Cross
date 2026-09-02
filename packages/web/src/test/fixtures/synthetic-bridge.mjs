// Browser smoke fixture only. It never reads host sessions or launches Providers.
// Every runs.* call is rejected, so review smoke tests cannot execute an Agent.
import { createInterface } from "node:readline";

const timestamp = "2026-09-03T00:00:00Z";
const session = {
  provider: "codex",
  sessionId: "synthetic-review-session",
  title: "Synthetic · parser review",
  cwd: "/synthetic/not-a-real-repository",
  updatedAt: timestamp,
};
const source = {
  ...session,
  identityKind: "thread.id",
  surface: "cli",
  providerVersion: "synthetic-fixture",
  nativeIds: { threadId: session.sessionId },
};
const snapshot = {
  source,
  capturedAt: timestamp,
  truncated: false,
  warnings: ["Synthetic browser fixture — not a real Provider Session"],
  entries: [
    {
      id: "synthetic-user-1",
      kind: "message",
      role: "user",
      text: "Please review how empty parser input is handled.",
    },
    {
      id: "synthetic-tool-1",
      kind: "tool",
      role: "tool",
      text: "Synthetic tool result: parser.test.ts passed 3 cases.",
    },
    {
      id: "synthetic-assistant-1",
      kind: "message",
      role: "assistant",
      text: "The parser now rejects an empty input with a clear error. Please review the wording before continuing.",
    },
  ],
  capabilities: {
    read: true,
    follow: false,
    open: false,
    resume: false,
    takeControl: false,
    reason: "Synthetic fixture has no execution or native UI capabilities",
  },
};

const input = createInterface({ input: process.stdin });
input.on("line", (line) => {
  let request;
  try {
    request = JSON.parse(line);
    process.stderr.write(`synthetic-bridge method=${request.method}\n`);
    let result;
    switch (request.method) {
      case "bridge.ping":
      case "bridge.info":
        result = {
          ok: true,
          name: "teamcross-agent-bridge",
          version: "synthetic",
          protocolVersion: 1,
          providers: ["codex"],
        };
        break;
      case "sessions.listStored":
        result = request.params.provider === "codex" ? [session] : [];
        break;
      case "sessions.snapshot":
        if (
          request.params.provider !== "codex" ||
          request.params.sessionId !== session.sessionId
        )
          throw new Error("Unknown synthetic session");
        result = snapshot;
        break;
      default:
        throw new Error(
          "Synthetic review fixture forbids execution and unknown methods",
        );
    }
    process.stdout.write(
      `${JSON.stringify({ jsonrpc: "2.0", id: request.id, result })}\n`,
    );
  } catch (error) {
    process.stdout.write(
      `${JSON.stringify({ jsonrpc: "2.0", id: request?.id, error: { code: -32601, message: error.message } })}\n`,
    );
  }
});
