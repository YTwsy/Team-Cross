import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Timeline } from "../components/Timeline";
import type { TimelineEvent } from "../types";

function event(
  seq: number,
  type: string,
  payload: Record<string, unknown>,
): TimelineEvent {
  return {
    seq,
    type,
    payload,
    actor: "Owner",
    createdAt: "2026-09-02T00:00:00Z",
  };
}

describe("Timeline", () => {
  it("renders command.send as a user message", () => {
    render(
      <Timeline
        events={[event(1, "command.send", { text: "Inspect the retry path" })]}
        rounds={[]}
      />,
    );
    expect(screen.getByText("Inspect the retry path")).toBeInTheDocument();
    expect(screen.getByText("Owner")).toBeInTheDocument();
  });

  it("combines live deltas and replaces them with the completed message", () => {
    const deltas = [
      event(1, "message.delta", {
        messageId: "message-1",
        provider: "codex",
        delta: "Live ",
      }),
      event(2, "message.delta", {
        messageId: "message-1",
        provider: "codex",
        delta: "answer",
      }),
    ];
    const view = render(<Timeline events={deltas} rounds={[]} />);
    expect(screen.getByText("Live answer")).toBeInTheDocument();
    expect(screen.getByText("responding…")).toBeInTheDocument();

    view.rerender(
      <Timeline
        events={[
          ...deltas,
          event(3, "message.completed", {
            messageId: "message-1",
            provider: "codex",
            text: "Final answer",
          }),
        ]}
        rounds={[]}
      />,
    );
    expect(screen.queryByText("Live answer")).not.toBeInTheDocument();
    expect(screen.getByText("Final answer")).toBeInTheDocument();
  });
});
