import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api";
import { NativeSessionOpen } from "../components/NativeSessionOpen";
import type { NativeOpenResult } from "../types";
import { snapshot } from "./reviewFixtures";

vi.mock("../api", () => ({ api: { openNativeSession: vi.fn() } }));
beforeEach(() => vi.clearAllMocks());
const available = {
  ...snapshot,
  capabilities: { ...snapshot.capabilities, open: true },
};
const requested: NativeOpenResult = {
  status: "requested",
  provider: "codex",
  sessionId: snapshot.source.sessionId,
  target: "codex-desktop",
  message: "Request handed to operating system.",
};

describe("Native Session Open", () => {
  it("keeps production capability false disabled with its reason and no mutation", async () => {
    const user = userEvent.setup();
    render(
      <NativeSessionOpen
        threadId={snapshot.threadId}
        snapshot={snapshot}
        canManage
      />,
    );
    expect(
      screen.getByRole("button", { name: "请求在 Codex Desktop 打开" }),
    ).toBeDisabled();
    expect(screen.getByText(snapshot.capabilities.reason!)).toBeInTheDocument();
    await user.click(screen.getByRole("button"));
    expect(api.openNativeSession).not.toHaveBeenCalled();
  });
  it("requires confirming Desktop and native identity, and treats 202 only as request receipt", async () => {
    const user = userEvent.setup();
    vi.mocked(api.openNativeSession).mockResolvedValue(requested);
    render(
      <NativeSessionOpen
        threadId={snapshot.threadId}
        snapshot={available}
        canManage
      />,
    );
    expect(screen.getByText(snapshot.source.sessionId)).toBeInTheDocument();
    expect(screen.getByRole("button")).toBeDisabled();
    await user.click(screen.getByLabelText(/确认在 Codex Desktop/));
    await user.click(screen.getByRole("button"));
    expect(await screen.findByRole("status")).toHaveTextContent(
      "系统已接收打开请求；尚未验证 Desktop 已定位正确 Session。",
    );
    expect(api.openNativeSession).toHaveBeenCalledWith(
      snapshot.threadId,
      snapshot.id,
    );
    expect(screen.getByRole("checkbox")).not.toBeChecked();
  });
  it("does not let remote viewers or another Provider open even with a claimed true capability", () => {
    const view = render(
      <NativeSessionOpen
        threadId={snapshot.threadId}
        snapshot={available}
        canManage={false}
      />,
    );
    expect(screen.getByRole("button")).toBeDisabled();
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    view.rerender(
      <NativeSessionOpen
        threadId={snapshot.threadId}
        snapshot={{
          ...available,
          source: { ...snapshot.source, provider: "claude" },
        }}
        canManage
      />,
    );
    expect(screen.getByRole("button")).toBeDisabled();
    expect(api.openNativeSession).not.toHaveBeenCalled();
  });
  it("shows failures without retrying the native open request", async () => {
    const user = userEvent.setup();
    vi.mocked(api.openNativeSession).mockRejectedValue(
      new Error("Native writer is owned by Team Cross"),
    );
    render(
      <NativeSessionOpen
        threadId={snapshot.threadId}
        snapshot={available}
        canManage
      />,
    );
    await user.click(screen.getByRole("checkbox"));
    await user.click(screen.getByRole("button"));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Native writer is owned by Team Cross",
    );
    expect(api.openNativeSession).toHaveBeenCalledTimes(1);
  });
  it("ignores the old open result when the selected Session component is replaced", async () => {
    const user = userEvent.setup();
    let resolve!: (value: NativeOpenResult) => void;
    vi.mocked(api.openNativeSession).mockReturnValue(
      new Promise((done) => {
        resolve = done;
      }),
    );
    const view = render(
      <NativeSessionOpen
        key="one"
        threadId={snapshot.threadId}
        snapshot={available}
        canManage
      />,
    );
    await user.click(screen.getByRole("checkbox"));
    await user.click(screen.getByRole("button"));
    view.rerender(
      <NativeSessionOpen
        key="two"
        threadId="other-thread"
        snapshot={{
          ...available,
          id: "other-snapshot",
          source: { ...snapshot.source, sessionId: "other-native" },
        }}
        canManage
      />,
    );
    await act(async () => resolve(requested));
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    expect(screen.getByRole("checkbox")).not.toBeChecked();
    expect(screen.getByText("other-native")).toBeInTheDocument();
  });
});
