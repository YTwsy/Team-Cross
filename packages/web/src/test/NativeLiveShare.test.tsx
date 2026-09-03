import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { SharePanel } from "../components/SharePanel";
import type { SessionFollow } from "../types";
import { snapshot } from "./reviewFixtures";

const follow: SessionFollow = {
  id: "follow-1",
  threadId: snapshot.threadId,
  source: snapshot.source,
  sourceSnapshotId: snapshot.id,
  currentSnapshotId: snapshot.id,
  state: "active",
  epoch: 2,
  updatedAt: snapshot.capturedAt,
  lastPolledAt: snapshot.capturedAt,
  gaps: [],
};
function props() {
  return {
    canManage: true,
    snapshots: [snapshot],
    sessionFollows: [follow],
    participants: [],
    onCreate: vi.fn().mockResolvedValue(undefined),
    onRevoke: vi.fn(),
    onRevokeControl: vi.fn(),
  };
}
async function previewMessages(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByLabelText(/单独启用原生实时分享/));
  await user.selectOptions(
    screen.getByLabelText("实时分享的 Follow"),
    follow.id,
  );
  await user.click(screen.getByLabelText("消息：用户输入与 Agent 回复"));
  await user.click(screen.getByRole("button", { name: "刷新并预览当前窗口" }));
}
async function confirmBoth(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByLabelText(/我已检查此固定窗口/));
  await user.click(screen.getByLabelText(/我另行允许后续窗口/));
}

describe("Native live Share consent", () => {
  it("defaults off, selects no categories and requires successful active Follow", async () => {
    const user = userEvent.setup();
    const value = props();
    const view = render(<SharePanel {...value} />);
    expect(screen.getByLabelText(/单独启用原生实时分享/)).not.toBeChecked();
    expect(
      screen.queryByLabelText("消息：用户输入与 Agent 回复"),
    ).not.toBeInTheDocument();
    await user.click(screen.getByLabelText(/单独启用原生实时分享/));
    expect(
      screen.getByLabelText("消息：用户输入与 Agent 回复"),
    ).not.toBeChecked();
    expect(
      screen.getByLabelText("工具：参数、结果、代码与路径"),
    ).not.toBeChecked();
    expect(screen.getByRole("button", { name: "Create share" })).toBeDisabled();
    view.rerender(
      <SharePanel
        {...value}
        sessionFollows={[
          { ...follow, state: "retrying", lastPolledAt: undefined },
        ]}
      />,
    );
    expect(
      screen.getByRole("button", { name: "刷新并预览当前窗口" }),
    ).toBeDisabled();
    expect(value.onCreate).not.toHaveBeenCalled();
  });

  it("requires separate current/future confirmation and keeps native live independent of static and managed scopes", async () => {
    const user = userEvent.setup();
    const value = props();
    render(<SharePanel {...value} canControl canIncludeCode />);
    await previewMessages(user);
    const preview = screen.getByRole("region", { name: "当前原生窗口预览" });
    expect(within(preview).getByText("Visible request")).toBeInTheDocument();
    expect(
      within(preview).queryByText("Private tool output"),
    ).not.toBeInTheDocument();
    expect(screen.getByText(/类别不是脱敏/)).toBeInTheDocument();
    await user.click(screen.getByLabelText(/我已检查此固定窗口/));
    expect(screen.getByRole("button", { name: "Create share" })).toBeDisabled();
    await user.click(screen.getByLabelText(/我另行允许后续窗口/));
    await user.selectOptions(
      screen.getByLabelText("分享的 Session 快照"),
      snapshot.id,
    );
    await user.click(screen.getByLabelText(/2\. tool/));
    await user.click(screen.getByRole("button", { name: "Create share" }));
    expect(value.onCreate).toHaveBeenCalledWith({
      allowDegraded: false,
      allowControl: false,
      scope: {
        snapshotId: snapshot.id,
        entryIds: ["entry-2"],
        evidenceIds: [],
        includeCode: false,
        includeEvents: false,
        nativeLive: {
          followId: follow.id,
          expectedSnapshotId: snapshot.id,
          entryKinds: ["message"],
          confirmCurrentAndFuture: true,
        },
      },
    });
  });

  it("freezes the preview and invalidates consent when the Follow window changes", async () => {
    const user = userEvent.setup();
    const value = props();
    const view = render(<SharePanel {...value} />);
    await previewMessages(user);
    await confirmBoth(user);
    expect(screen.getByRole("button", { name: "Create share" })).toBeEnabled();
    const next = {
      ...snapshot,
      id: "snapshot-2",
      entries: [
        { id: "new", kind: "message" as const, text: "New window secret" },
      ],
    };
    view.rerender(
      <SharePanel
        {...value}
        snapshots={[snapshot, next]}
        sessionFollows={[{ ...follow, currentSnapshotId: next.id }]}
      />,
    );
    expect(screen.getByRole("button", { name: "Create share" })).toBeDisabled();
    expect(screen.getByRole("alert")).toHaveTextContent(/下方是旧预览/);
    expect(screen.getByText("Visible request")).toBeInTheDocument();
    expect(screen.queryByText("New window secret")).not.toBeInTheDocument();
    expect(screen.getByLabelText(/我另行允许后续窗口/)).not.toBeChecked();
    await user.click(
      screen.getByRole("button", { name: "刷新并预览当前窗口" }),
    );
    expect(screen.getByText("New window secret")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create share" })).toBeDisabled();
    await confirmBoth(user);
    await user.click(screen.getByRole("button", { name: "Create share" }));
    expect(value.onCreate).toHaveBeenCalledWith(
      expect.objectContaining({
        scope: expect.objectContaining({
          nativeLive: expect.objectContaining({
            expectedSnapshotId: "snapshot-2",
          }),
        }),
      }),
    );
  });

  it("stops submission on Follow downgrade and does not silently omit an enabled live intent", async () => {
    const user = userEvent.setup();
    const value = props();
    const view = render(<SharePanel {...value} canIncludeCode />);
    await user.click(screen.getByLabelText(/包含当前已封存代码/));
    await previewMessages(user);
    await confirmBoth(user);
    view.rerender(
      <SharePanel
        {...value}
        canIncludeCode
        sessionFollows={[{ ...follow, state: "stopped", epoch: 3 }]}
      />,
    );
    expect(screen.getByRole("button", { name: "Create share" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Create share" }));
    expect(value.onCreate).not.toHaveBeenCalled();
    await user.click(screen.getByLabelText(/单独启用原生实时分享/));
    expect(screen.getByRole("button", { name: "Create share" })).toBeEnabled();
    await user.click(screen.getByRole("button", { name: "Create share" }));
    expect(value.onCreate).toHaveBeenCalledWith(
      expect.objectContaining({
        scope: expect.not.objectContaining({ nativeLive: expect.anything() }),
      }),
    );
  });

  it("invalidates preview and both confirmations when categories change", async () => {
    const user = userEvent.setup();
    render(<SharePanel {...props()} />);
    await previewMessages(user);
    await confirmBoth(user);
    await user.click(screen.getByLabelText("工具：参数、结果、代码与路径"));
    expect(
      screen.queryByLabelText(/我已检查此固定窗口/),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create share" })).toBeDisabled();
    await user.click(
      screen.getByRole("button", { name: "刷新并预览当前窗口" }),
    );
    expect(screen.getByText("Private tool output")).toBeInTheDocument();
    expect(screen.getByLabelText(/我另行允许后续窗口/)).not.toBeChecked();
  });

  it("shows shared native scope and limited status without exposing management controls to remote viewers", () => {
    render(
      <SharePanel
        {...props()}
        canManage={false}
        share={{
          id: "share-1",
          invite: "",
          expiresAt: snapshot.capturedAt,
          status: "active",
          transports: [],
          scope: {
            includeCode: false,
            includeEvents: false,
            nativeLive: {
              followId: follow.id,
              expectedSnapshotId: snapshot.id,
              entryKinds: ["message"],
              confirmCurrentAndFuture: true,
            },
          },
        }}
        nativeLive={{
          followId: follow.id,
          state: "limited",
          latestSnapshotId: "snapshot-2",
          entryKinds: ["message"],
          reason: "Delivery limit reached",
        }}
      />,
    );
    expect(
      screen.getByText(/已单独授权原生实时范围 · limited/),
    ).toBeInTheDocument();
    expect(screen.getByText(/Delivery limit reached/)).toBeInTheDocument();
    expect(screen.getByText(/静态分享内容不会自动扩大/)).toBeInTheDocument();
    expect(
      screen.queryByLabelText(/单独启用原生实时分享/),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Create share" }),
    ).not.toBeInTheDocument();
  });
});
