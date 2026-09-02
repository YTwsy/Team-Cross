import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Composer } from "../components/Composer";
import type { AgentRun } from "../types";

const idleRun: AgentRun = {
  id: "run-1",
  provider: "codex",
  status: "idle",
  networkEnabled: false,
};

const callbacks = () => ({
  onSend: vi.fn().mockResolvedValue(undefined),
  onSteer: vi.fn().mockResolvedValue(undefined),
  onInterrupt: vi.fn().mockResolvedValue(undefined),
  onRespondInput: vi.fn().mockResolvedValue(undefined),
  onRequestControl: vi.fn().mockResolvedValue(undefined),
});

describe("Composer permissions", () => {
  it("keeps observers read-only and exposes only the control request", async () => {
    const handlers = callbacks();
    const user = userEvent.setup();
    render(<Composer role="observer" run={idleRun} {...handlers} />);

    expect(screen.getByText("Observer mode")).toBeInTheDocument();
    expect(screen.queryByLabelText("Message to Agent")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Send" }),
    ).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Request control" }));
    expect(handlers.onRequestControl).toHaveBeenCalledOnce();
    expect(handlers.onSend).not.toHaveBeenCalled();
  });

  it("sends when idle, then exposes steer and interrupt for a running turn", async () => {
    const handlers = callbacks();
    const user = userEvent.setup();
    const { rerender } = render(
      <Composer role="controller" run={idleRun} {...handlers} />,
    );

    await user.type(
      screen.getByLabelText("Message to Agent"),
      "  inspect the retry path  ",
    );
    await user.click(screen.getByRole("button", { name: "Send" }));
    expect(handlers.onSend).toHaveBeenCalledWith("inspect the retry path");

    rerender(
      <Composer
        role="controller"
        run={{ ...idleRun, status: "running" }}
        {...handlers}
      />,
    );
    await user.type(
      screen.getByLabelText("Message to Agent"),
      "focus on the lease",
    );
    await user.click(screen.getByRole("button", { name: "Steer" }));
    expect(handlers.onSteer).toHaveBeenCalledWith("focus on the lease");

    await user.click(screen.getByRole("button", { name: "Interrupt" }));
    expect(handlers.onInterrupt).toHaveBeenCalledOnce();
  });

  it("shows a blocking Agent question and submits its response", async () => {
    const handlers = callbacks();
    const user = userEvent.setup();
    render(
      <Composer
        pendingInput={{
          id: "input-1",
          blocking: true,
          questions: [
            {
              id: "choice",
              header: "Approach",
              question: "Which implementation should I use?",
              options: [
                { label: "Minimal", description: "Keep the API small" },
              ],
            },
          ],
        }}
        role="controller"
        run={{ ...idleRun, status: "running" }}
        {...handlers}
      />,
    );

    expect(
      screen.getByText("Which implementation should I use?"),
    ).toBeInTheDocument();
    expect(screen.getByText("Turn paused")).toBeInTheDocument();
    await user.type(
      screen.getByLabelText("Response to Agent input"),
      "Minimal",
    );
    await user.click(screen.getByRole("button", { name: "Respond" }));
    expect(handlers.onRespondInput).toHaveBeenCalledWith("input-1", "Minimal");
    expect(handlers.onSend).not.toHaveBeenCalled();
  });
});
