import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { InFlowConnections } from "../components/InFlowConnections";
import { setAPITransport } from "../api";
import { setConversationPanel } from "../currentConversation";
import { setLanguageInfo } from "../i18n";
import { beginRefreshState, endRefreshState } from "../refreshState";

let writes: { path: string; body: any }[];
let known: boolean;
let enabled: boolean;
let fail: boolean;
const source = {
  id: "source-a",
  name: "我的分析会话",
  cwd: "/work/project",
  preview: "private content",
  updatedAt: 1,
};
beforeEach(() => {
  writes = [];
  known = true;
  enabled = false;
  fail = false;
  beginRefreshState("inflow-test");
  setLanguageInfo({ mode: "zh-CN", resolved: "zh-CN" });
  setAPITransport(async (path, body) => {
    if (body !== undefined) {
      writes.push({ path, body });
      if (fail) throw new TypeError("connection lost");
      enabled = true;
      return { linked: true, inFlow: true, targetId: "target-a" } as any;
    }
    if (path.startsWith("sources?"))
      return { data: path.includes("provider=claude") ? [] : [source] } as any;
    return { known, linked: enabled, inFlow: enabled, receiving: false } as any;
  });
});
afterEach(() => {
  setAPITransport(undefined);
  setConversationPanel(false);
  endRefreshState();
});

it("enables an explicitly selected local conversation without pairing or model input", async () => {
  const changed = vi.fn();
  render(
    <InFlowConnections
      spaceId="joined-member-space"
      disabled={false}
      onChanged={changed}
    />,
  );
  expect(writes).toEqual([]);
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "选择本机会话" }));
  const dialog = screen.getByRole("dialog");
  const enable = within(dialog).getByRole("button", { name: "开启顺流感知" });
  expect(enable).toBeDisabled();
  await user.click(await screen.findByRole("radio", { name: /我的分析会话/ }));
  expect(writes).toEqual([]);
  await user.click(enable);
  await waitFor(() => expect(changed).toHaveBeenCalledOnce());
  expect(writes).toEqual([
    {
      path: "collaborations/joined-member-space/workbench/inflow-connect",
      body: { provider: "codex", sourceId: "source-a" },
    },
  ]);
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it("does not reuse the selected source after switching provider", async () => {
  render(
    <InFlowConnections spaceId="space" disabled={false} onChanged={() => {}} />,
  );
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "选择本机会话" }));
  await user.click(await screen.findByRole("radio", { name: /我的分析会话/ }));
  await user.click(
    screen.getByRole("button", { name: "Claude Code · 实验性" }),
  );
  expect(screen.getByRole("button", { name: "开启顺流感知" })).toBeDisabled();
  await screen.findByText("没有找到来源会话");
  expect(writes).toEqual([]);
});

it("uses an app write with no supplied identity and no conversation message", async () => {
  setConversationPanel(true); // The host need not offer ui/message.
  const changed = vi.fn();
  render(
    <InFlowConnections spaceId="space" disabled={false} onChanged={changed} />,
  );
  const button = screen.getByRole("button", { name: "让当前会话接收空间更新" });
  await waitFor(() => expect(button).toBeEnabled());
  expect(writes).toEqual([]);
  await userEvent.click(button);
  await screen.findByRole("button", { name: "当前会话已开启顺流感知" });
  expect(writes).toEqual([
    { path: "plugin/current-inflow", body: { spaceId: "space" } },
  ]);
  expect(changed).toHaveBeenCalledOnce();
});

it("offers local selection when the host has no conversation identity", async () => {
  known = false;
  setConversationPanel(true);
  render(
    <InFlowConnections spaceId="space" disabled={false} onChanged={() => {}} />,
  );
  await screen.findByText("宿主未提供当前会话身份，请从本机会话列表选择。");
  expect(
    screen.getByRole("button", { name: "让当前会话接收空间更新" }),
  ).toBeDisabled();
  expect(screen.getByRole("button", { name: "选择本机会话" })).toBeEnabled();
  expect(writes).toEqual([]);
});

it("preserves an uncertain local choice without automatically repeating the write", async () => {
  fail = true;
  const changed = vi.fn();
  render(
    <InFlowConnections spaceId="space" disabled={false} onChanged={changed} />,
  );
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "选择本机会话" }));
  await user.click(await screen.findByRole("radio", { name: /我的分析会话/ }));
  await user.click(screen.getByRole("button", { name: "开启顺流感知" }));
  await screen.findByText("connection lost");
  expect(writes).toHaveLength(1);
  expect(changed).not.toHaveBeenCalled();
  expect(screen.getByRole("radio", { name: /我的分析会话/ })).toHaveAttribute(
    "aria-checked",
    "true",
  );
  fail = false;
  await user.click(screen.getByRole("button", { name: "开启顺流感知" }));
  expect(writes).toHaveLength(2);
  expect(writes[1]).toEqual(writes[0]);
});

it("explains the other member's local setup and passive delivery conditions", async () => {
  render(
    <InFlowConnections spaceId="space" disabled={false} onChanged={() => {}} />,
  );
  await userEvent.click(screen.getByText("自己或其他协作者如何配置？"));
  expect(
    screen.getByText(/每位成员在自己的 Team Cross 中加入同一个空间/),
  ).toBeVisible();
  expect(
    screen.getByText(/正常调用 Team Cross 工具时即可附带更新/),
  ).toBeVisible();
  expect(
    screen.getByText(/启用并信任 Team Cross 的 UserPromptSubmit Hook/),
  ).toBeVisible();
  expect(screen.getByRole("link", { name: "打开本机设置" })).toHaveAttribute(
    "href",
    "#/settings",
  );
  expect(writes).toEqual([]);
});
