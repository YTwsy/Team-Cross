import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api";
import { ContinuationPanel } from "../components/ContinuationPanel";
import { executableThread, reviewThread } from "./reviewFixtures";

vi.mock("../api", () => ({
  api: {
    continueFromRound: vi.fn(),
    forkThread: vi.fn(),
    exportBundle: vi.fn(),
  },
}));
beforeEach(() => vi.clearAllMocks());
describe("Continue from Round", () => {
  it("requires Owner confirmation and forces historical continuation to Fork", async () => {
    vi.mocked(api.continueFromRound).mockResolvedValue({
      ...executableThread,
      id: "new-thread",
    });
    const onResult = vi.fn();
    const user = userEvent.setup();
    render(<ContinuationPanel thread={executableThread} onResult={onResult} />);
    await user.selectOptions(screen.getByLabelText("交接快照"), "round-0");
    await user.click(screen.getByText("在本机创建新 Session"));
    expect(screen.getByLabelText("创建独立 Fork Thread")).toBeChecked();
    expect(screen.getByLabelText("创建独立 Fork Thread")).toBeDisabled();
    await user.type(screen.getByLabelText("首条指令"), "Fix regression");
    expect(
      screen.getByRole("button", { name: "创建新 Session 并继续" }),
    ).toBeDisabled();
    await user.click(screen.getByLabelText(/我授权以上新 Session/));
    await user.click(
      screen.getByRole("button", { name: "创建新 Session 并继续" }),
    );
    await waitFor(() =>
      expect(api.continueFromRound).toHaveBeenCalledWith("thread-1", {
        roundId: "round-0",
        provider: "codex",
        prompt: "Fix regression",
        networkEnabled: false,
        expectedRevision: 4,
        fork: true,
      }),
    );
    expect(onResult).toHaveBeenCalledWith(
      expect.objectContaining({ id: "new-thread" }),
    );
  });

  it("cannot start a new Agent from reference-only history", () => {
    render(<ContinuationPanel thread={reviewThread} onResult={vi.fn()} />);
    expect(
      screen.queryByRole("button", { name: /创建新 Session/ }),
    ).not.toBeInTheDocument();
    expect(screen.getByText(/没有可执行的代码基线/)).toBeInTheDocument();
  });

  it("does not describe a read-only Session Round as an executable code snapshot", () => {
    render(
      <ContinuationPanel
        thread={{
          ...reviewThread,
          rounds: [
            {
              id: "review-round",
              sequence: 0,
              summary: "Session-only review",
              createdAt: reviewThread.updatedAt,
            },
          ],
        }}
        onResult={vi.fn()}
      />,
    );
    expect(screen.queryByText("在本机创建新 Session")).not.toBeInTheDocument();
    expect(screen.queryByText("导出离线交接包")).not.toBeInTheDocument();
    expect(screen.getByText(/没有可执行的代码基线/)).toBeInTheDocument();
  });

  it("Fork alone does not call Continue", async () => {
    vi.mocked(api.forkThread).mockResolvedValue({
      ...executableThread,
      id: "new-thread",
    });
    const user = userEvent.setup();
    render(<ContinuationPanel thread={executableThread} onResult={vi.fn()} />);
    await user.click(
      screen.getByRole("button", { name: "仅 Fork Thread，不启动 Agent" }),
    );
    expect(api.forkThread).toHaveBeenCalledWith("thread-1", "round-1");
    expect(api.continueFromRound).not.toHaveBeenCalled();
  });
});
