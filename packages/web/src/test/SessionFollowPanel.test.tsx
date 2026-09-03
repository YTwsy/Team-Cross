import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api";
import { SessionFollowPanel } from "../components/SessionFollowPanel";
import type { SessionFollow } from "../types";
import { snapshot } from "./reviewFixtures";

vi.mock("../api", () => ({
  api: { startSessionFollow: vi.fn(), stopSessionFollow: vi.fn() },
}));
const follow: SessionFollow = {
  id: "follow-1",
  threadId: "thread-1",
  source: snapshot.source,
  sourceSnapshotId: snapshot.id,
  currentSnapshotId: snapshot.id,
  state: "active",
  epoch: 1,
  updatedAt: snapshot.capturedAt,
  gaps: [],
};
const supportedSnapshot = {
  ...snapshot,
  capabilities: { ...snapshot.capabilities, follow: true },
};
const baseProps = {
  threadId: "thread-1",
  snapshot,
  follows: [],
  canManage: true,
  availableSnapshotIds: [snapshot.id],
  onChanged: vi.fn(),
  onSelectSnapshot: vi.fn(),
};

beforeEach(() => vi.clearAllMocks());
afterEach(() => vi.useRealTimers());

describe("SessionFollowPanel", () => {
  it("keeps unsupported Follow disabled and never mutates on mount or click", async () => {
    const user = userEvent.setup();
    render(<SessionFollowPanel {...baseProps} />);
    expect(
      screen.getByRole("button", { name: "开始只读 Follow" }),
    ).toBeDisabled();
    expect(screen.getByRole("checkbox")).toBeDisabled();
    expect(
      screen.getByText(
        /当前来源不能启用 Follow：Native writer fencing is not verified/,
      ),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "开始只读 Follow" }));
    expect(api.startSessionFollow).not.toHaveBeenCalled();
    expect(api.stopSessionFollow).not.toHaveBeenCalled();
    expect(screen.getByText(/不创建 Run、不发送 prompt/)).toBeInTheDocument();
    expect(screen.getByText(/新快照不会扩大当前 Share/)).toBeInTheDocument();
  });

  it("requires explicit confirmation to start and allows stopping after capability becomes false", async () => {
    const user = userEvent.setup();
    const onChanged = vi.fn();
    vi.mocked(api.startSessionFollow).mockResolvedValue(follow);
    vi.mocked(api.stopSessionFollow).mockResolvedValue({
      ...follow,
      state: "stopped",
      epoch: 2,
    });
    const view = render(
      <SessionFollowPanel
        {...baseProps}
        snapshot={supportedSnapshot}
        onChanged={onChanged}
      />,
    );
    expect(api.startSessionFollow).not.toHaveBeenCalled();
    expect(
      screen.getByRole("button", { name: "开始只读 Follow" }),
    ).toBeDisabled();
    await user.click(screen.getByRole("checkbox"));
    await user.click(screen.getByRole("button", { name: "开始只读 Follow" }));
    await waitFor(() => expect(onChanged).toHaveBeenCalledWith(follow));
    expect(api.startSessionFollow).toHaveBeenCalledWith(
      "thread-1",
      "snapshot-1",
    );
    view.rerender(
      <SessionFollowPanel
        {...baseProps}
        follows={[follow]}
        onChanged={onChanged}
      />,
    );
    expect(
      screen.queryByRole("button", { name: "开始只读 Follow" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "停止 Follow" })).toBeEnabled();
    await user.click(screen.getByRole("button", { name: "停止 Follow" }));
    await waitFor(() =>
      expect(api.stopSessionFollow).toHaveBeenCalledWith(
        "thread-1",
        "follow-1",
      ),
    );
    expect(onChanged).toHaveBeenLastCalledWith(
      expect.objectContaining({ state: "stopped", epoch: 2 }),
    );
  });

  it("does not offer start or stop to remote viewers even if Follow data is supplied", () => {
    render(
      <SessionFollowPanel
        {...baseProps}
        snapshot={supportedSnapshot}
        follows={[follow]}
        canManage={false}
      />,
    );
    expect(
      screen.queryByRole("button", { name: /开始只读 Follow|停止 Follow/ }),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(screen.getByText(/只有主机 Owner/)).toBeInTheDocument();
    expect(api.startSessionFollow).not.toHaveBeenCalled();
    expect(api.stopSessionFollow).not.toHaveBeenCalled();
  });

  it("shows retry state, last read time, current snapshot and explicit data gaps without polling", async () => {
    vi.useFakeTimers();
    render(
      <SessionFollowPanel
        {...baseProps}
        follows={[
          {
            ...follow,
            state: "retrying",
            currentSnapshotId: "snapshot-2",
            reason: "Read-only source disconnected",
            lastPolledAt: "2026-09-03T00:01:00Z",
            gaps: ["Earlier records are unavailable"],
          },
        ]}
        availableSnapshotIds={[snapshot.id, "snapshot-2"]}
      />,
    );
    expect(screen.getByText("等待重试 · retrying")).toBeInTheDocument();
    expect(screen.getByText("snapshot-2")).toBeInTheDocument();
    expect(
      screen.getByText("Read-only source disconnected"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Earlier records are unavailable"),
    ).toBeInTheDocument();
    expect(
      screen.getByText(new Date("2026-09-03T00:01:00Z").toLocaleString()),
    ).toBeInTheDocument();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(120_000);
    });
    expect(api.startSessionFollow).not.toHaveBeenCalled();
    expect(api.stopSessionFollow).not.toHaveBeenCalled();
  });

  it("retains stopped history and exposes an explicit link to a newly captured snapshot", async () => {
    const onSelectSnapshot = vi.fn();
    const user = userEvent.setup();
    render(
      <SessionFollowPanel
        {...baseProps}
        follows={[
          { ...follow, state: "stopped", currentSnapshotId: "snapshot-2" },
        ]}
        availableSnapshotIds={[snapshot.id, "snapshot-2"]}
        onSelectSnapshot={onSelectSnapshot}
      />,
    );
    expect(
      screen.getByText(/已捕获的不可变快照与批注继续保留/),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "停止 Follow" }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "查看跟随快照" }));
    expect(onSelectSnapshot).toHaveBeenCalledWith("snapshot-2");
  });

  it("reports mutation failures without automatically retrying", async () => {
    const user = userEvent.setup();
    vi.mocked(api.stopSessionFollow).mockRejectedValue(
      new Error("source unreachable"),
    );
    render(<SessionFollowPanel {...baseProps} follows={[follow]} />);
    await user.click(screen.getByRole("button", { name: "停止 Follow" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "source unreachable",
    );
    expect(api.stopSessionFollow).toHaveBeenCalledTimes(1);
    expect(baseProps.onChanged).not.toHaveBeenCalled();
  });

  it("ignores a late mutation response after unmount", async () => {
    const user = userEvent.setup();
    const onChanged = vi.fn();
    let resolve!: (value: SessionFollow) => void;
    vi.mocked(api.startSessionFollow).mockReturnValue(
      new Promise((done) => {
        resolve = done;
      }),
    );
    const view = render(
      <SessionFollowPanel
        {...baseProps}
        snapshot={supportedSnapshot}
        onChanged={onChanged}
      />,
    );
    await user.click(screen.getByRole("checkbox"));
    await user.click(screen.getByRole("button", { name: "开始只读 Follow" }));
    view.unmount();
    await act(async () => resolve(follow));
    expect(onChanged).not.toHaveBeenCalled();
  });
});
