import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api";
import { ContinuationPanel } from "../components/ContinuationPanel";
import type { ThreadDetail } from "../types";
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

  it("pins the default Round and requires new consent when it becomes historical", async () => {
    vi.mocked(api.continueFromRound).mockResolvedValue(executableThread);
    const user = userEvent.setup();
    const { rerender } = render(
      <ContinuationPanel thread={executableThread} onResult={vi.fn()} />,
    );
    await user.click(screen.getByText("在本机创建新 Session"));
    await user.type(screen.getByLabelText("首条指令"), "Continue checked work");
    await user.click(screen.getByLabelText(/我授权以上新 Session/));
    const updated = {
      ...executableThread,
      revision: 5,
      rounds: [
        ...executableThread.rounds,
        { ...executableThread.rounds[1]!, id: "round-2", sequence: 2 },
      ],
    };
    rerender(<ContinuationPanel thread={updated} onResult={vi.fn()} />);

    expect(screen.getByLabelText("交接快照")).toHaveValue("round-1");
    expect(screen.getByLabelText("创建独立 Fork Thread")).toBeChecked();
    expect(screen.getByLabelText(/我授权以上新 Session/)).not.toBeChecked();
    const button = screen.getByRole("button", {
      name: "创建新 Session 并继续",
    });
    expect(button).toBeDisabled();
    await user.click(button);
    expect(api.continueFromRound).not.toHaveBeenCalled();
    await user.click(screen.getByLabelText(/我授权以上新 Session/));
    await user.click(button);
    expect(api.continueFromRound).toHaveBeenCalledWith(
      executableThread.id,
      expect.objectContaining({
        roundId: "round-1",
        expectedRevision: 5,
        fork: true,
      }),
    );
  });

  it.each([
    ["worktree", { worktree: "/isolated/replacement" }],
    ["baseline", { git: { ...executableThread.git, head: "new-baseline" } }],
    ["Thread identity", { id: "other-thread" }],
    ["read-only status", { readOnly: true }],
  ] satisfies [string, Partial<ThreadDetail>][])(
    "discards consent when %s changes, including after it changes back",
    async (_name, update) => {
      const user = userEvent.setup();
      const { rerender } = render(
        <ContinuationPanel thread={executableThread} onResult={vi.fn()} />,
      );
      await user.click(screen.getByText("在本机创建新 Session"));
      await user.type(screen.getByLabelText("首条指令"), "Reviewed work");
      await user.click(screen.getByLabelText(/我授权以上新 Session/));
      rerender(
        <ContinuationPanel
          thread={{ ...executableThread, ...update }}
          onResult={vi.fn()}
        />,
      );
      rerender(<ContinuationPanel thread={executableThread} onResult={vi.fn()} />);
      expect(screen.getByLabelText(/我授权以上新 Session/)).not.toBeChecked();
      expect(
        screen.getByRole("button", { name: "创建新 Session 并继续" }),
      ).toBeDisabled();
      expect(api.continueFromRound).not.toHaveBeenCalled();
    },
  );

  it("invalidates consent when Provider or the first instruction changes", async () => {
    const user = userEvent.setup();
    render(<ContinuationPanel thread={executableThread} onResult={vi.fn()} />);
    await user.click(screen.getByText("在本机创建新 Session"));
    await user.type(screen.getByLabelText("首条指令"), "Reviewed work");
    await user.click(screen.getByLabelText(/我授权以上新 Session/));
    await user.selectOptions(screen.getByLabelText("Provider"), "claude");
    expect(screen.getByLabelText(/我授权以上新 Session/)).not.toBeChecked();
    await user.click(screen.getByLabelText(/我授权以上新 Session/));
    await user.type(screen.getByLabelText("首条指令"), " changed");
    expect(screen.getByLabelText(/我授权以上新 Session/)).not.toBeChecked();
    expect(api.continueFromRound).not.toHaveBeenCalled();
  });

  it("does not invalidate a fixed plan for an unrelated revision refresh", async () => {
    const user = userEvent.setup();
    const { rerender } = render(
      <ContinuationPanel thread={executableThread} onResult={vi.fn()} />,
    );
    await user.click(screen.getByText("在本机创建新 Session"));
    await user.type(screen.getByLabelText("首条指令"), "Reviewed work");
    await user.click(screen.getByLabelText(/我授权以上新 Session/));
    rerender(
      <ContinuationPanel
        thread={{ ...executableThread, revision: 5 }}
        onResult={vi.fn()}
      />,
    );
    expect(screen.getByLabelText("交接快照")).toHaveValue("round-1");
    expect(screen.getByLabelText(/我授权以上新 Session/)).toBeChecked();
  });

  it("never falls forward when the selected Round is absent from a refresh", async () => {
    const user = userEvent.setup();
    const { rerender } = render(
      <ContinuationPanel thread={executableThread} onResult={vi.fn()} />,
    );
    await user.click(screen.getByText("在本机创建新 Session"));
    await user.type(screen.getByLabelText("首条指令"), "Reviewed work");
    await user.click(screen.getByLabelText(/我授权以上新 Session/));
    rerender(
      <ContinuationPanel
        thread={{ ...executableThread, rounds: [executableThread.rounds[0]!] }}
        onResult={vi.fn()}
      />,
    );
    expect(screen.getByText(/所选 Round 已不可用/)).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "创建新 Session 并继续" }),
    ).not.toBeInTheDocument();
    expect(api.continueFromRound).not.toHaveBeenCalled();
  });

  it("keeps bundle export on the explicitly confirmed immutable Round", async () => {
    vi.mocked(api.exportBundle).mockRejectedValue(new Error("Test skips download"));
    const user = userEvent.setup();
    const { rerender } = render(
      <ContinuationPanel thread={executableThread} onResult={vi.fn()} />,
    );
    await user.click(screen.getByText("导出离线交接包"));
    await user.click(screen.getByLabelText(/已检查代码与所选内容/));
    rerender(
      <ContinuationPanel
        thread={{
          ...executableThread,
          rounds: [
            ...executableThread.rounds,
            { ...executableThread.rounds[1]!, id: "round-2", sequence: 2 },
          ],
        }}
        onResult={vi.fn()}
      />,
    );
    expect(screen.getByLabelText("交接快照")).toHaveValue("round-1");
    await user.click(screen.getByRole("button", { name: "导出所选交接包" }));
    expect(api.exportBundle).toHaveBeenCalledWith(executableThread.id, {
      roundId: "round-1",
      evidenceIds: [],
      snapshotIds: [],
      confirmExport: true,
    });
  });

  it("discards export consent when its baseline or content selection changes", async () => {
    const user = userEvent.setup();
    const { rerender } = render(
      <ContinuationPanel thread={executableThread} onResult={vi.fn()} />,
    );
    await user.click(screen.getByText("导出离线交接包"));
    await user.click(screen.getByLabelText(/已检查代码与所选内容/));
    rerender(
      <ContinuationPanel
        thread={{
          ...executableThread,
          git: { ...executableThread.git, head: "different-baseline" },
        }}
        onResult={vi.fn()}
      />,
    );
    rerender(<ContinuationPanel thread={executableThread} onResult={vi.fn()} />);
    expect(screen.getByLabelText(/已检查代码与所选内容/)).not.toBeChecked();
    await user.click(screen.getByLabelText(/已检查代码与所选内容/));
    await user.click(screen.getByLabelText(/完整快照/));
    expect(screen.getByLabelText(/已检查代码与所选内容/)).not.toBeChecked();
    expect(api.exportBundle).not.toHaveBeenCalled();
  });
});
