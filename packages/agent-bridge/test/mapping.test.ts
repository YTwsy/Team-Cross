import { describe, expect, it } from "vitest";

import { mapClaudeMessage, sanitizeBashHook } from "../src/adapters/claude.js";
import { mapCodexNotification } from "../src/adapters/codex.js";

describe("Codex event mapping", () => {
  it.each(["userMessage", "reasoning", "plan", "hookPrompt", "contextCompaction", "enteredReviewMode", "exitedReviewMode", "subAgentActivity", "futureUnknownItem"])("does not invent a tool lifecycle for %s", (type) => {
    for (const method of ["item/started", "item/completed"]) {
      expect(mapCodexNotification(method, { threadId: "thread", turnId: "turn", item: {
        type, id: "non-tool", text: "/bin/sleep 20", content: [{ type: "text", text: "/bin/sleep 20" }],
      } })).toEqual([]);
    }
  });

  it("distinguishes prompt text from an actual commandExecution item", () => {
    const text = "/bin/sleep 20";
    expect(mapCodexNotification("item/started", { item: { type: "agentMessage", id: "assistant", text } })).toEqual([]);
    expect(mapCodexNotification("item/completed", { item: { type: "agentMessage", id: "assistant", text } })[0]?.type).toBe("message.completed");
    expect(mapCodexNotification("item/started", { item: { type: "commandExecution", id: "exec", command: text } })).toEqual([
      expect.objectContaining({ type: "tool.started", toolId: "exec", data: { name: "Bash", item: { type: "commandExecution", id: "exec", command: text } } }),
    ]);
  });

  it("maps streaming text and file changes to the common event model", () => {
    expect(mapCodexNotification("item/agentMessage/delta", {
      threadId: "thread",
      turnId: "turn",
      itemId: "message",
      delta: "hello",
    })).toEqual([expect.objectContaining({
      type: "message.delta",
      turnId: "turn",
      messageId: "message",
      data: { delta: "hello" },
    })]);

    const fileEvents = mapCodexNotification("item/completed", {
      threadId: "thread",
      turnId: "turn",
      item: {
        type: "fileChange",
        id: "tool",
        changes: [{ path: "src/main.ts", kind: "update" }],
        status: "completed",
      },
    });
    expect(fileEvents.map((event) => event.type)).toEqual(["tool.completed", "file.changed"]);
  });
});

describe("Claude event mapping", () => {
  it("maps token deltas, tools, and completion", () => {
    const tools = new Map<number, { id: string; name: string; input: string }>();
    const start = mapClaudeMessage({
      type: "stream_event",
      event: {
        type: "content_block_start",
        index: 0,
        content_block: { type: "tool_use", id: "tool-1", name: "Read", input: {} },
      },
    }, "turn", tools);
    expect(start).toEqual([expect.objectContaining({ type: "tool.started", toolId: "tool-1" })]);

    const delta = mapClaudeMessage({
      type: "stream_event",
      uuid: "message-1",
      event: { type: "content_block_delta", delta: { type: "text_delta", text: "hi" } },
    }, "turn", tools);
    expect(delta).toEqual([expect.objectContaining({
      type: "message.delta",
      messageId: "message-1",
      data: { delta: "hi" },
    })]);

    const result = mapClaudeMessage({
      type: "result",
      subtype: "success",
      result: "done",
    }, "turn", tools);
    expect(result).toEqual([expect.objectContaining({
      type: "turn.completed",
      data: expect.objectContaining({ status: "completed" }),
    })]);
  });

  it("strips model credentials from Bash subprocesses", () => {
    const output = sanitizeBashHook({
      tool_name: "Bash",
      tool_input: { command: "npm test" },
    });
    expect(output).toMatchObject({
      hookSpecificOutput: {
        updatedInput: {
          command: "unset ANTHROPIC_API_KEY ANTHROPIC_AUTH_TOKEN CLAUDE_CODE_OAUTH_TOKEN; npm test",
        },
      },
    });
  });
});
