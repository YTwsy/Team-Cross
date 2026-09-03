import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api, subscribeEvents } from "../api";
import { ThreadWorkspace } from "../components/ThreadWorkspace";
import type {
  AppInfo,
  SessionFollow,
  ThreadDetail,
  TimelineEvent,
} from "../types";
import {
  executableThread,
  reviewThread,
  snapshot as reviewSnapshot,
} from "./reviewFixtures";
import { codeReview } from "./codeReviewFixtures";

vi.mock("../api", () => ({
  api: {
    thread: vi.fn(),
    control: vi.fn(),
    addAnnotation: vi.fn(),
    attachEvidence: vi.fn(),
    interrupt: vi.fn(),
    respondInput: vi.fn(),
    send: vi.fn(),
    steer: vi.fn(),
    switchAgent: vi.fn(),
    createShare: vi.fn(),
    revokeControl: vi.fn(),
    revokeShare: vi.fn(),
    importSession: vi.fn(),
    storedSessions: vi.fn(),
    feedback: vi.fn(),
    startSessionFollow: vi.fn(),
    stopSessionFollow: vi.fn(),
    sessionSnapshot: vi.fn(),
    openNativeSession: vi.fn(),
    roundCode: vi.fn(),
  },
  subscribeEvents: vi.fn(),
}));

function futureIso(offsetMs = 60_000): string {
  return new Date(Date.now() + offsetMs).toISOString();
}

const info: AppInfo = {
  version: "0.1.0",
  mode: "join",
  role: "observer",
  participantId: "participant-1",
  doctor: [],
};

function thread(overrides: Partial<ThreadDetail> = {}): ThreadDetail {
  return {
    id: "thread-1",
    title: "Stable collaboration",
    repo: "/repo",
    branch: "main",
    status: "active",
    updatedAt: "2026-09-02T00:00:00Z",
    revision: 3,
    worktree: "/worktree",
    goal: "Keep collaborators connected",
    progress: "Captured",
    blocker: "",
    tried: "",
    questions: "",
    git: {
      head: "0123456789abcdef",
      branch: "main",
      unborn: false,
      status: "",
      stagedPatch: "",
      unstagedPatch: "",
      untracked: [],
    },
    rounds: [],
    events: [
      {
        seq: 1,
        type: "run.started",
        createdAt: "2026-09-02T00:00:00Z",
        payload: {},
      },
    ],
    annotations: [],
    evidence: [],
    agentRun: {
      id: "run-1",
      provider: "mock",
      status: "idle",
      networkEnabled: false,
    },
    participants: [],
    ...overrides,
  };
}

describe("ThreadWorkspace effects", () => {
  let onEvents: ((events: TimelineEvent[]) => void) | undefined;
  const unsubscribe = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    unsubscribe.mockClear();
    onEvents = undefined;
    vi.mocked(subscribeEvents).mockImplementation(
      (_id, _after, nextEvents, onConnection) => {
        onEvents = nextEvents;
        onConnection(true);
        return unsubscribe;
      },
    );
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("submits a code annotation with exact sealed Round, path, side and line", async () => {
    vi.mocked(api.thread).mockResolvedValue(executableThread);
    vi.mocked(api.roundCode).mockResolvedValue(codeReview);
    vi.mocked(api.addAnnotation).mockResolvedValue({
      ...executableThread,
      revision: 5,
    });
    const user = userEvent.setup();
    render(
      <ThreadWorkspace
        id="thread-1"
        info={{ ...info, mode: "host", role: "owner" }}
        onBack={vi.fn()}
      />,
    );
    await screen.findByText("Visible request");
    await user.click(screen.getByRole("button", { name: "diff" }));
    await user.click(
      await screen.findByRole("button", {
        name: "Annotate src/retry.ts old line 2",
      }),
    );
    expect(
      screen.getByText("批注 Round round-1 · old · src/retry.ts:2"),
    ).toBeInTheDocument();
    await user.type(
      screen.getByLabelText("Annotation body"),
      "Check the removed branch",
    );
    await user.click(screen.getByRole("button", { name: "Add annotation" }));
    expect(api.addAnnotation).toHaveBeenCalledWith(
      "thread-1",
      expect.objectContaining({
        body: "Check the removed branch",
        file: "src/retry.ts",
        line: 2,
        target: { roundId: "round-1", side: "old" },
      }),
    );
    expect(api.send).not.toHaveBeenCalled();
    expect(api.switchAgent).not.toHaveBeenCalled();
  });

  it("returns to an exact old code reference and does not re-anchor legacy comments", async () => {
    vi.mocked(api.thread).mockResolvedValue({
      ...executableThread,
      annotations: [
        {
          id: "anchored",
          author: "Owner",
          body: "Old code feedback",
          file: "src/retry.ts",
          line: 2,
          target: { roundId: "round-0", side: "old" },
          createdAt: "2026-09-03",
        },
        {
          id: "legacy",
          author: "Owner",
          body: "Legacy feedback",
          file: "src/retry.ts",
          line: 2,
          createdAt: "2026-09-03",
        },
      ],
    });
    vi.mocked(api.roundCode).mockResolvedValue({
      ...codeReview,
      roundId: "round-0",
    });
    const user = userEvent.setup();
    render(<ThreadWorkspace id="thread-1" info={info} onBack={vi.fn()} />);
    await screen.findByText("Visible request");
    await user.click(screen.getByRole("button", { name: /^annotations/ }));
    expect(
      screen.getByText(/历史代码批注（未绑定不可变定位）/),
    ).toBeInTheDocument();
    expect(
      screen.getAllByRole("button", { name: "查看引用代码" }),
    ).toHaveLength(1);
    await user.click(screen.getByRole("button", { name: "查看引用代码" }));
    await waitFor(() =>
      expect(
        screen
          .getByText("-export const connected = false;")
          .closest(".diff-line"),
      ).toHaveAttribute("aria-current", "location"),
    );
    expect(api.roundCode).toHaveBeenLastCalledWith("thread-1", "round-0");
    expect(api.roundCode).toHaveBeenCalledTimes(1);
    expect(
      screen.queryByRole("button", { name: /实时 worktree/ }),
    ).not.toBeInTheDocument();
  });

  it("reviews and annotates a snapshot without an Agent or a control request", async () => {
    vi.mocked(api.thread).mockResolvedValue(reviewThread);
    vi.mocked(api.addAnnotation).mockResolvedValue({
      ...reviewThread,
      revision: 5,
    });
    const user = userEvent.setup();
    render(<ThreadWorkspace id="thread-1" info={info} onBack={vi.fn()} />);
    expect(await screen.findByText("Visible request")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Request control" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Send" }),
    ).not.toBeInTheDocument();
    await user.click(screen.getAllByRole("button", { name: "批注" })[0]!);
    await user.type(
      screen.getByLabelText("Annotation body"),
      "Please clarify this decision",
    );
    await user.click(screen.getByRole("button", { name: "Add annotation" }));
    expect(api.addAnnotation).toHaveBeenCalledWith("thread-1", {
      body: "Please clarify this decision",
      target: { snapshotId: "snapshot-1", entryId: "entry-1" },
      expectedRevision: 4,
      leaseEpoch: 0,
    });
    expect(api.send).not.toHaveBeenCalled();
    expect(api.switchAgent).not.toHaveBeenCalled();
    expect(
      screen.queryByRole("button", { name: /开始只读 Follow|停止 Follow/ }),
    ).not.toBeInTheDocument();
    expect(api.startSessionFollow).not.toHaveBeenCalled();
    expect(api.stopSessionFollow).not.toHaveBeenCalled();
  });

  it("updates Follow through explicit actions and SSE while retaining the selected immutable snapshot", async () => {
    const followed: SessionFollow = {
      id: "follow-1",
      threadId: reviewThread.id,
      source: reviewSnapshot.source,
      sourceSnapshotId: reviewSnapshot.id,
      currentSnapshotId: reviewSnapshot.id,
      state: "active",
      epoch: 1,
      updatedAt: reviewThread.updatedAt,
      gaps: [],
    };
    const starting = {
      ...reviewThread,
      sessionSnapshots: [
        {
          ...reviewSnapshot,
          capabilities: { ...reviewSnapshot.capabilities, follow: true },
        },
      ],
    };
    vi.mocked(api.thread).mockResolvedValue(starting);
    vi.mocked(api.startSessionFollow).mockResolvedValue(followed);
    vi.mocked(api.stopSessionFollow).mockResolvedValue({
      ...followed,
      state: "stopped",
      epoch: 2,
      currentSnapshotId: "snapshot-2",
    });
    const user = userEvent.setup();
    render(
      <ThreadWorkspace
        id="thread-1"
        info={{ ...info, mode: "host", role: "owner" }}
        onBack={vi.fn()}
      />,
    );
    await screen.findByText("Visible request");
    await user.click(screen.getByRole("checkbox", { name: /确认只读跟随/ }));
    await user.click(screen.getByRole("button", { name: "开始只读 Follow" }));
    expect(await screen.findByText("跟随中 · active")).toBeInTheDocument();
    expect(api.startSessionFollow).toHaveBeenCalledWith(
      "thread-1",
      "snapshot-1",
    );
    vi.mocked(api.thread).mockResolvedValue({
      ...starting,
      revision: 5,
      sessionSnapshots: [
        reviewSnapshot,
        {
          ...reviewSnapshot,
          id: "snapshot-2",
          entries: [
            {
              id: "new-entry",
              kind: "message",
              text: "Followed native output",
            },
          ],
        },
      ],
      sessionFollows: [
        {
          ...followed,
          state: "retrying",
          currentSnapshotId: "snapshot-2",
          gaps: ["Source history has a gap"],
          reason: "Reader disconnected",
        },
      ],
    });
    act(() =>
      onEvents?.([
        {
          seq: 9,
          type: "session.follow.updated",
          createdAt: reviewThread.updatedAt,
          payload: { revision: 5 },
        },
      ]),
    );
    expect(await screen.findByText("等待重试 · retrying")).toBeInTheDocument();
    expect(screen.getByText("Source history has a gap")).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Session 快照" })).toHaveValue(
      "snapshot-1",
    );
    expect(
      screen.queryByText("Followed native output"),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "查看跟随快照" }));
    expect(screen.getByText("Followed native output")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "停止 Follow" }));
    expect(await screen.findByText("已停止 · stopped")).toBeInTheDocument();
    expect(api.stopSessionFollow).toHaveBeenCalledWith("thread-1", "follow-1");
    expect(api.thread).toHaveBeenCalledTimes(2);
    expect(subscribeEvents).toHaveBeenCalledTimes(1);
    expect(api.send).not.toHaveBeenCalled();
    expect(api.switchAgent).not.toHaveBeenCalled();
    expect(api.control).not.toHaveBeenCalled();
  });

  it("refreshes shared annotations after a sanitized revision-only event", async () => {
    vi.mocked(api.thread)
      .mockResolvedValueOnce(reviewThread)
      .mockResolvedValue({
        ...reviewThread,
        revision: 5,
        annotations: [
          {
            id: "annotation-1",
            author: "Reviewer",
            body: "New review",
            createdAt: reviewThread.updatedAt,
          },
        ],
      });
    render(<ThreadWorkspace id="thread-1" info={info} onBack={vi.fn()} />);
    await screen.findByText("Visible request");
    act(() =>
      onEvents?.([
        {
          seq: 8,
          type: "thread.updated",
          createdAt: reviewThread.updatedAt,
          payload: { revision: 5 },
        },
      ]),
    );
    await waitFor(() => expect(api.thread).toHaveBeenCalledTimes(2));
    expect(
      screen.getByRole("button", { name: "annotations 1" }),
    ).toBeInTheDocument();
    expect(subscribeEvents).toHaveBeenCalledTimes(1);
  });

  it("exports feedback for the Owner without sending it to the Agent", async () => {
    vi.mocked(api.thread).mockResolvedValue(reviewThread);
    vi.mocked(api.feedback).mockResolvedValue(
      "# Review\n[snapshot-1 / entry-1] Please clarify",
    );
    const user = userEvent.setup();
    render(
      <ThreadWorkspace
        id="thread-1"
        info={{ ...info, mode: "host", role: "owner" }}
        onBack={vi.fn()}
      />,
    );
    await user.click(
      await screen.findByRole("button", { name: "导出审阅反馈" }),
    );
    expect(
      await screen.findByRole("textbox", { name: "Markdown 反馈" }),
    ).toHaveValue("# Review\n[snapshot-1 / entry-1] Please clarify");
    expect(api.feedback).toHaveBeenCalledWith("thread-1");
    expect(api.send).not.toHaveBeenCalled();
  });

  it("opens an annotation's exact older snapshot and retains it after a sanitized live-window event", async () => {
    const referenced = {
      ...reviewSnapshot,
      id: "old-window",
      entries: [
        {
          id: "old-entry",
          kind: "message" as const,
          text: "Original reviewed statement",
        },
      ],
    };
    const initial = {
      ...reviewThread,
      annotations: [
        {
          id: "annotation-old",
          author: "Reviewer",
          body: "Explain the old statement",
          target: { snapshotId: referenced.id, entryId: "old-entry" },
          createdAt: reviewThread.updatedAt,
        },
      ],
    };
    vi.mocked(api.thread).mockResolvedValue(initial);
    vi.mocked(api.sessionSnapshot).mockResolvedValue(referenced);
    const user = userEvent.setup();
    render(<ThreadWorkspace id="thread-1" info={info} onBack={vi.fn()} />);
    await user.click(
      await screen.findByRole("button", { name: "annotations 1" }),
    );
    await user.click(screen.getByRole("button", { name: "查看引用快照" }));
    expect(
      await screen.findByText("Original reviewed statement"),
    ).toBeVisible();
    expect(api.sessionSnapshot).toHaveBeenCalledWith("thread-1", "old-window");
    vi.mocked(api.thread).mockResolvedValue({
      ...initial,
      revision: 5,
      sessionSnapshots: [
        {
          ...reviewSnapshot,
          id: "new-window",
          entries: [
            { id: "new-entry", kind: "message", text: "New window statement" },
          ],
        },
      ],
      nativeLive: {
        followId: "follow-1",
        state: "limited",
        latestSnapshotId: "new-window",
        entryKinds: ["message"],
        reason: "Delivery budget reached",
      },
    });
    act(() =>
      onEvents?.([
        {
          seq: 20,
          type: "shared.session.updated",
          createdAt: reviewThread.updatedAt,
          payload: { revision: 5, snapshotId: "new-window", state: "limited" },
        },
      ]),
    );
    expect(
      await screen.findByText("原生实时分享 · limited"),
    ).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Session 快照" })).toHaveValue(
      "old-window",
    );
    expect(screen.getByText("Original reviewed statement")).toBeVisible();
    expect(screen.queryByText("New window statement")).not.toBeInTheDocument();
    expect(api.openNativeSession).not.toHaveBeenCalled();
    expect(api.sessionSnapshot).toHaveBeenCalledTimes(1);
  });

  it("does not apply an older Share creation response after switching Threads", async () => {
    let resolve!: (value: ThreadDetail) => void;
    vi.mocked(api.createShare).mockReturnValue(
      new Promise((done) => {
        resolve = done;
      }),
    );
    const other = {
      ...reviewThread,
      id: "thread-2",
      title: "Other review",
      sessionSnapshots: [
        { ...reviewSnapshot, threadId: "thread-2", id: "other-snapshot" },
      ],
    };
    vi.mocked(api.thread).mockImplementation(async (id) =>
      id === "thread-1" ? reviewThread : other,
    );
    const user = userEvent.setup();
    const owner = { ...info, mode: "host" as const, role: "owner" as const };
    const view = render(
      <ThreadWorkspace id="thread-1" info={owner} onBack={vi.fn()} />,
    );
    await user.selectOptions(
      await screen.findByLabelText("分享的 Session 快照"),
      reviewSnapshot.id,
    );
    await user.click(screen.getByLabelText(/1\. user/));
    await user.click(screen.getByRole("button", { name: "Create share" }));
    expect(api.createShare).toHaveBeenCalledTimes(1);
    view.rerender(
      <ThreadWorkspace id="thread-2" info={owner} onBack={vi.fn()} />,
    );
    await screen.findByRole("heading", { name: "Other review" });
    await act(async () =>
      resolve({
        ...reviewThread,
        revision: 5,
        share: {
          id: "old-share",
          invite: "old-thread-invite",
          expiresAt: reviewThread.updatedAt,
          status: "active",
          transports: [],
        },
      }),
    );
    expect(
      screen.getByRole("heading", { name: "Other review" }),
    ).toBeInTheDocument();
    expect(screen.queryByText(/old-thread-invite/)).not.toBeInTheDocument();
    expect(screen.getByLabelText("分享的 Session 快照")).toHaveValue("");
    expect(screen.getByLabelText(/单独启用原生实时分享/)).not.toBeChecked();
  });

  it("keeps one SSE subscription as the cursor advances and refreshes the remote snapshot", async () => {
    const snapshot = thread();
    vi.mocked(api.thread).mockResolvedValue(snapshot);
    const view = render(
      <ThreadWorkspace id="thread-1" info={info} onBack={vi.fn()} />,
    );

    expect(
      await screen.findByRole("heading", { name: "Stable collaboration" }),
    ).toBeInTheDocument();
    await waitFor(() =>
      expect(subscribeEvents).toHaveBeenCalledWith(
        "thread-1",
        1,
        expect.any(Function),
        expect.any(Function),
      ),
    );
    expect(subscribeEvents).toHaveBeenCalledTimes(1);

    act(() => {
      onEvents?.([
        {
          seq: 2,
          type: "message.completed",
          createdAt: "2026-09-02T00:00:01Z",
          payload: { provider: "mock", text: "Remote update", revision: 4 },
        },
      ]);
    });

    expect(screen.getByText("Remote update")).toBeInTheDocument();
    expect(subscribeEvents).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(api.thread).toHaveBeenCalledTimes(2));
    expect(screen.getByText("Remote update")).toBeInTheDocument();

    view.unmount();
    expect(unsubscribe).toHaveBeenCalledOnce();
  });

  it("renews a controller lease with the latest snapshot without restarting its interval", async () => {
    vi.useFakeTimers();
    const initial = thread({
      revision: 7,
      controlLease: {
        participantId: "participant-1",
        epoch: 2,
        expiresAt: futureIso(),
      },
    });
    const renewed = thread({
      revision: 8,
      controlLease: {
        participantId: "participant-1",
        epoch: 3,
        expiresAt: futureIso(80_000),
      },
    });
    vi.mocked(api.thread).mockResolvedValue(initial);
    vi.mocked(api.control)
      .mockResolvedValueOnce(renewed)
      .mockResolvedValueOnce({ ...renewed, revision: 9 });

    render(<ThreadWorkspace id="thread-1" info={info} onBack={vi.fn()} />);
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(screen.getByRole("button", { name: "Send" })).toBeInTheDocument();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(20_000);
    });
    expect(api.control).toHaveBeenNthCalledWith(1, "thread-1", "renew", 7, 2);
    expect(subscribeEvents).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(20_000);
    });
    expect(api.control).toHaveBeenNthCalledWith(2, "thread-1", "renew", 8, 3);
    expect(subscribeEvents).toHaveBeenCalledTimes(1);
  });

  it("lets the host reclaim the remote control lease", async () => {
    const controlled = thread({
      revision: 11,
      participants: [
        {
          id: "participant-2",
          name: "Reviewer",
          role: "controller",
          transport: "tailcat",
          lastSeenAt: "2026-09-02T00:00:00Z",
        },
      ],
      controlLease: {
        participantId: "participant-2",
        epoch: 4,
        expiresAt: futureIso(),
      },
    });
    vi.mocked(api.thread).mockResolvedValue(controlled);
    vi.mocked(api.revokeControl).mockResolvedValue({
      ...controlled,
      revision: 12,
      participants: controlled.participants.map((participant) => ({
        ...participant,
        role: "observer",
      })),
      controlLease: undefined,
    });
    const user = userEvent.setup();

    render(
      <ThreadWorkspace
        id="thread-1"
        info={{ ...info, mode: "host", role: "owner" }}
        onBack={vi.fn()}
      />,
    );

    await user.click(
      await screen.findByRole("button", { name: "Reclaim control" }),
    );
    expect(api.revokeControl).toHaveBeenCalledWith("thread-1", 11);
  });

  it("does not let continuous SSE traffic postpone snapshot refresh", async () => {
    vi.useFakeTimers();
    vi.mocked(api.thread).mockResolvedValue(thread());

    render(
      <ThreadWorkspace
        id="thread-1"
        info={{ ...info, mode: "host", role: "owner" }}
        onBack={vi.fn()}
      />,
    );
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(api.thread).toHaveBeenCalledTimes(1);

    act(() => {
      onEvents?.([
        {
          seq: 2,
          type: "message.delta",
          createdAt: new Date().toISOString(),
          payload: { messageId: "message-1", delta: "one" },
        },
      ]);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(90);
    });
    act(() => {
      onEvents?.([
        {
          seq: 3,
          type: "message.delta",
          createdAt: new Date().toISOString(),
          payload: { messageId: "message-1", delta: " two" },
        },
      ]);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10);
    });

    expect(api.thread).toHaveBeenCalledTimes(2);
  });

  it("uses an event revision for the next command and ignores an expired lease", async () => {
    const expired = thread({
      revision: 4,
      controlLease: {
        participantId: "participant-1",
        epoch: 8,
        expiresAt: new Date(Date.now() - 1_000).toISOString(),
      },
    });
    vi.mocked(api.thread).mockResolvedValue(expired);
    const user = userEvent.setup();
    const view = render(
      <ThreadWorkspace id="thread-1" info={info} onBack={vi.fn()} />,
    );
    expect(await screen.findByText("Observer mode")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Send" }),
    ).not.toBeInTheDocument();
    view.unmount();

    vi.mocked(api.thread).mockResolvedValue(thread({ revision: 4 }));
    vi.mocked(api.send).mockResolvedValue(thread({ revision: 10 }));
    render(
      <ThreadWorkspace
        id="thread-1"
        info={{ ...info, mode: "host", role: "owner" }}
        onBack={vi.fn()}
      />,
    );
    expect(
      await screen.findByRole("button", { name: "Send" }),
    ).toBeInTheDocument();
    act(() => {
      onEvents?.([
        {
          seq: 2,
          type: "thread.updated",
          createdAt: new Date().toISOString(),
          payload: { revision: "9" },
        },
      ]);
    });
    await user.type(screen.getByLabelText("Message to Agent"), "continue");
    await user.click(screen.getByRole("button", { name: "Send" }));
    expect(api.send).toHaveBeenCalledWith("thread-1", "continue", 9, 0);
  });

  it("responds to the latest unresolved Agent input request", async () => {
    const pending = thread({
      revision: 6,
      events: [
        {
          seq: 2,
          type: "input.requested",
          createdAt: new Date().toISOString(),
          payload: {
            inputRequestId: "input-2",
            blocking: true,
            questions: [{ id: "answer", question: "Proceed with the patch?" }],
          },
        },
      ],
    });
    vi.mocked(api.thread).mockResolvedValue(pending);
    vi.mocked(api.respondInput).mockResolvedValue({
      ...pending,
      revision: 7,
      events: [
        ...pending.events,
        {
          seq: 3,
          type: "input.resolved",
          createdAt: new Date().toISOString(),
          payload: { inputRequestId: "input-2" },
        },
      ],
    });
    const user = userEvent.setup();

    render(
      <ThreadWorkspace
        id="thread-1"
        info={{ ...info, mode: "host", role: "owner" }}
        onBack={vi.fn()}
      />,
    );
    expect(
      await screen.findByText("Proceed with the patch?"),
    ).toBeInTheDocument();
    await user.type(screen.getByLabelText("Response to Agent input"), "Yes");
    await user.click(screen.getByRole("button", { name: "Respond" }));
    expect(api.respondInput).toHaveBeenCalledWith(
      "thread-1",
      "input-2",
      "Yes",
      6,
      0,
    );
  });

  it("clears a pending input request when its Turn is interrupted", async () => {
    vi.mocked(api.thread).mockResolvedValue(
      thread({
        revision: 8,
        events: [
          {
            seq: 2,
            type: "input.requested",
            createdAt: new Date().toISOString(),
            payload: {
              inputRequestId: "input-interrupted",
              turnId: "turn-interrupted",
              blocking: true,
              prompt: "This should no longer be pending",
            },
          },
          {
            seq: 3,
            type: "turn.completed",
            createdAt: new Date().toISOString(),
            payload: { turnId: "turn-interrupted", status: "interrupted" },
          },
        ],
      }),
    );

    render(
      <ThreadWorkspace
        id="thread-1"
        info={{ ...info, mode: "host", role: "owner" }}
        onBack={vi.fn()}
      />,
    );

    expect(
      await screen.findByRole("textbox", { name: "Message to Agent" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("Agent needs input")).not.toBeInTheDocument();
  });
});
