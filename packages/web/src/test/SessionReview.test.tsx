import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api";
import { SessionReview, SessionTranscript } from "../components/SessionReview";
import type { SessionFollow } from "../types";
import { snapshot } from "./reviewFixtures";

vi.mock("../api", () => ({
  api: {
    startSessionFollow: vi.fn(),
    stopSessionFollow: vi.fn(),
    sessionSnapshot: vi.fn(),
    openNativeSession: vi.fn(),
  },
}));
beforeEach(() => vi.clearAllMocks());
const follow: SessionFollow = {
  id: "follow-1",
  threadId: "thread-1",
  source: snapshot.source,
  sourceSnapshotId: snapshot.id,
  currentSnapshotId: "snapshot-2",
  state: "active",
  epoch: 1,
  updatedAt: snapshot.capturedAt,
  gaps: [],
};
const supportedSnapshot = {
  ...snapshot,
  capabilities: { ...snapshot.capabilities, follow: true },
};

describe("SessionReview", () => {
  it("keeps the reviewed snapshot stable when Follow adds a snapshot, until explicitly selected", async () => {
    const user = userEvent.setup();
    const props = { annotations: [], onAnnotate: vi.fn(), canManage: true };
    const view = render(<SessionReview {...props} snapshots={[]} />);
    view.rerender(<SessionReview {...props} snapshots={[snapshot]} />);
    const next = {
      ...snapshot,
      id: "snapshot-2",
      capturedAt: "2026-09-03T00:01:00Z",
      entries: [
        {
          id: "new-entry",
          kind: "message" as const,
          text: "New native output",
        },
      ],
    };
    view.rerender(
      <SessionReview
        {...props}
        snapshots={[snapshot, next]}
        follows={[follow]}
      />,
    );
    expect(screen.getByRole("combobox", { name: "Session 快照" })).toHaveValue(
      "snapshot-1",
    );
    expect(screen.getByText("Visible request")).toBeInTheDocument();
    expect(screen.queryByText("New native output")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "查看跟随快照" }));
    expect(screen.getByRole("combobox", { name: "Session 快照" })).toHaveValue(
      "snapshot-2",
    );
    expect(screen.getByText("New native output")).toBeInTheDocument();
    expect(api.startSessionFollow).not.toHaveBeenCalled();
  });

  it("resets explicit confirmation when selecting a different immutable snapshot", async () => {
    const user = userEvent.setup();
    render(
      <SessionReview
        snapshots={[
          supportedSnapshot,
          { ...supportedSnapshot, id: "snapshot-2" },
        ]}
        annotations={[]}
        onAnnotate={vi.fn()}
        canManage
      />,
    );
    await user.click(screen.getByRole("checkbox", { name: /确认只读跟随/ }));
    expect(
      screen.getByRole("button", { name: "开始只读 Follow" }),
    ).toBeEnabled();
    await user.selectOptions(
      screen.getByRole("combobox", { name: "Session 快照" }),
      "snapshot-1",
    );
    expect(
      screen.getByRole("checkbox", { name: /确认只读跟随/ }),
    ).not.toBeChecked();
    expect(
      screen.getByRole("button", { name: "开始只读 Follow" }),
    ).toBeDisabled();
    expect(api.startSessionFollow).not.toHaveBeenCalled();
  });

  it("resets state across Threads and ignores an earlier Thread's in-flight start result", async () => {
    const user = userEvent.setup();
    const onFollowChanged = vi.fn();
    let resolve!: (value: SessionFollow) => void;
    vi.mocked(api.startSessionFollow).mockReturnValue(
      new Promise((done) => {
        resolve = done;
      }),
    );
    const props = {
      annotations: [],
      onAnnotate: vi.fn(),
      canManage: true,
      onFollowChanged,
    };
    const view = render(
      <SessionReview
        {...props}
        threadId="thread-1"
        snapshots={[supportedSnapshot]}
      />,
    );
    await user.click(screen.getByRole("checkbox", { name: /确认只读跟随/ }));
    await user.click(screen.getByRole("button", { name: "开始只读 Follow" }));
    view.rerender(
      <SessionReview
        {...props}
        threadId="thread-2"
        snapshots={[
          {
            ...supportedSnapshot,
            threadId: "thread-2",
            id: "snapshot-other",
            source: { ...snapshot.source, sessionId: "native-other" },
          },
        ]}
      />,
    );
    expect(screen.getByRole("combobox", { name: "Session 快照" })).toHaveValue(
      "snapshot-other",
    );
    expect(
      screen.getByRole("checkbox", { name: /确认只读跟随/ }),
    ).not.toBeChecked();
    expect(
      screen.getByRole("button", { name: "开始只读 Follow" }),
    ).toBeDisabled();
    await act(async () => resolve(follow));
    expect(onFollowChanged).not.toHaveBeenCalled();
    expect(screen.queryByText("跟随中 · active")).not.toBeInTheDocument();
  });

  it("targets an immutable snapshot entry, not a visual row number", async () => {
    const onAnnotate = vi.fn();
    const user = userEvent.setup();
    render(
      <SessionReview
        snapshots={[snapshot]}
        annotations={[]}
        onAnnotate={onAnnotate}
      />,
    );
    await user.click(screen.getAllByRole("button", { name: "批注" })[1]!);
    expect(onAnnotate).toHaveBeenCalledWith({
      snapshotId: "snapshot-1",
      entryId: "entry-2",
    });
    expect(screen.getByText(/只读快照/)).toBeInTheDocument();
    expect(screen.getByText(/此快照不证明历史代码状态/)).toBeInTheDocument();
  });

  it("renders imported text without executing markup and shows truncation/capability boundaries", () => {
    render(
      <SessionTranscript
        preview={{
          ...snapshot,
          truncated: true,
          warnings: ["工具输出缺失"],
          entries: [
            {
              id: "unsafe",
              kind: "message",
              text: '<img src="x" onerror="alert(1)">',
            },
          ],
        }}
      />,
    );
    expect(
      screen.getByText('<img src="x" onerror="alert(1)">'),
    ).toBeInTheDocument();
    expect(screen.queryByRole("img")).not.toBeInTheDocument();
    expect(screen.getByText(/历史已截断/)).toBeInTheDocument();
    expect(screen.getByText("工具输出缺失")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /接管|Follow|Resume/ }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText("Native writer fencing is not verified"),
    ).toBeInTheDocument();
  });
});
