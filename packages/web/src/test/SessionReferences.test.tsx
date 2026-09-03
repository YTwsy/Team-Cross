import { act, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api";
import { SessionReview } from "../components/SessionReview";
import type { SessionSnapshot } from "../types";
import { snapshot } from "./reviewFixtures";

vi.mock("../api", () => ({
  api: {
    sessionSnapshot: vi.fn(),
    startSessionFollow: vi.fn(),
    stopSessionFollow: vi.fn(),
    openNativeSession: vi.fn(),
  },
}));
beforeEach(() => vi.clearAllMocks());
const base = {
  threadId: snapshot.threadId,
  annotations: [],
  onAnnotate: vi.fn(),
};
const latest: SessionSnapshot = {
  ...snapshot,
  id: "snapshot-latest",
  entries: [
    { id: "latest-entry", kind: "message", text: "Latest unrelated text" },
  ],
};
const old: SessionSnapshot = {
  ...snapshot,
  id: "snapshot-old",
  entries: [
    { id: "old-entry", kind: "message", text: "Exact old quoted text" },
  ],
};

describe("Exact Session snapshot references", () => {
  it("keeps reading the cached selected window when remote detail replaces it with a latest window", () => {
    const view = render(<SessionReview {...base} snapshots={[snapshot]} />);
    view.rerender(
      <SessionReview
        {...base}
        snapshots={[latest]}
        nativeLive={{
          followId: "follow-1",
          state: "active",
          latestSnapshotId: latest.id,
          entryKinds: ["message"],
        }}
      />,
    );
    expect(screen.getByRole("combobox", { name: "Session 快照" })).toHaveValue(
      snapshot.id,
    );
    expect(screen.getByText("Visible request")).toBeInTheDocument();
    expect(screen.queryByText("Latest unrelated text")).not.toBeInTheDocument();
    expect(api.sessionSnapshot).not.toHaveBeenCalled();
  });

  it("loads a missing annotation reference by exact identity without showing latest content meanwhile", async () => {
    let resolve!: (value: SessionSnapshot) => void;
    vi.mocked(api.sessionSnapshot).mockReturnValue(
      new Promise((done) => {
        resolve = done;
      }),
    );
    render(
      <SessionReview
        {...base}
        snapshots={[latest]}
        reference={{
          threadId: snapshot.threadId,
          snapshotId: old.id,
          entryId: "old-entry",
          requestId: 1,
        }}
      />,
    );
    expect(api.sessionSnapshot).toHaveBeenCalledWith(snapshot.threadId, old.id);
    expect(screen.queryByText("Latest unrelated text")).not.toBeInTheDocument();
    expect(screen.getByText(/正在读取获授权的引用快照/)).toBeInTheDocument();
    await act(async () => resolve(old));
    expect(screen.getByText("Exact old quoted text")).toBeInTheDocument();
    expect(
      screen.getByText("Exact old quoted text").closest("article"),
    ).toHaveClass("referenced-entry");
    expect(screen.getByRole("combobox", { name: "Session 快照" })).toHaveValue(
      old.id,
    );
  });

  it("reports forbidden or missing exact references and only retries on explicit request", async () => {
    const user = userEvent.setup();
    vi.mocked(api.sessionSnapshot).mockRejectedValue(
      new Error("Snapshot is not in this Share"),
    );
    render(
      <SessionReview
        {...base}
        snapshots={[latest]}
        reference={{
          threadId: snapshot.threadId,
          snapshotId: old.id,
          entryId: "old-entry",
          requestId: 1,
        }}
      />,
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Snapshot is not in this Share",
    );
    expect(screen.getByRole("alert")).toHaveTextContent(old.id);
    expect(screen.queryByText("Latest unrelated text")).not.toBeInTheDocument();
    expect(api.sessionSnapshot).toHaveBeenCalledTimes(1);
    await user.click(screen.getByRole("button", { name: "重试读取此快照" }));
    expect(api.sessionSnapshot).toHaveBeenCalledTimes(2);
  });

  it("rejects mismatched snapshot identity and ignores a request completing for a previous Thread", async () => {
    vi.mocked(api.sessionSnapshot).mockResolvedValueOnce(latest);
    const view = render(
      <SessionReview
        {...base}
        snapshots={[latest]}
        reference={{
          threadId: snapshot.threadId,
          snapshotId: old.id,
          requestId: 1,
        }}
      />,
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "快照身份不匹配",
    );
    let resolve!: (value: SessionSnapshot) => void;
    vi.mocked(api.sessionSnapshot).mockReturnValue(
      new Promise((done) => {
        resolve = done;
      }),
    );
    view.rerender(
      <SessionReview
        {...base}
        snapshots={[latest]}
        reference={{
          threadId: snapshot.threadId,
          snapshotId: old.id,
          requestId: 2,
        }}
      />,
    );
    view.rerender(
      <SessionReview
        {...base}
        threadId="thread-other"
        snapshots={[{ ...latest, threadId: "thread-other" }]}
      />,
    );
    await act(async () => resolve(old));
    expect(screen.queryByText("Exact old quoted text")).not.toBeInTheDocument();
    expect(screen.getByText("Latest unrelated text")).toBeInTheDocument();
  });

  it("bounds cached windows to three while leaving the selected snapshot readable", async () => {
    const user = userEvent.setup();
    const snapshots = [1, 2, 3, 4].map((number) => ({
      ...snapshot,
      id: `window-${number}`,
      source: { ...snapshot.source, title: `Window ${number}` },
    }));
    const view = render(<SessionReview {...base} snapshots={snapshots} />);
    const select = screen.getByRole("combobox", { name: "Session 快照" });
    for (const id of ["window-1", "window-2", "window-3"])
      await user.selectOptions(select, id);
    view.rerender(<SessionReview {...base} snapshots={[latest]} />);
    expect(within(select).getAllByRole("option")).toHaveLength(4);
    await user.selectOptions(select, latest.id);
    expect(
      within(select).queryByRole("option", { name: /Window 1/ }),
    ).not.toBeInTheDocument();
    expect(within(select).getAllByRole("option")).toHaveLength(3);
    expect(screen.getByText("Latest unrelated text")).toBeInTheDocument();
  });

  it("makes unavailable entry anchors explicit instead of substituting another message", () => {
    render(
      <SessionReview
        {...base}
        snapshots={[snapshot]}
        reference={{
          threadId: snapshot.threadId,
          snapshotId: snapshot.id,
          entryId: "hidden-entry",
          requestId: 1,
        }}
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "没有获授权的引用记录 hidden-entry",
    );
    expect(screen.getByRole("alert")).toHaveTextContent("不会用其他记录代替");
  });
});
