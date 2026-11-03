import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, afterEach, expect, it, vi } from "vitest";
import { SpaceShadows } from "../components/SpaceShadows";
import type { Shadow, ShadowSettings } from "../shadows";
import {
  beginRefreshState,
  endRefreshState,
  captureRefreshState,
  hasRefreshDrafts,
} from "../refreshState";

let shadow: Shadow;
let sent: { shadowId: string; message: { id: string; text: string } }[];
let lose: boolean;
let created: {
  requestId: string;
  name: string;
  cwd: string;
  settings: ShadowSettings;
}[];
let paths: string[];
beforeEach(() => {
  window.localStorage.clear();
  paths = [];
  sent = [];
  created = [];
  lose = true;
  HTMLElement.prototype.scrollIntoView = vi.fn();
  shadow = {
    id: "shadow",
    spaceId: "space",
    name: "同事分身",
    owner: { id: "B", name: "成员 B" },
    enabled: true,
    online: true,
    busy: false,
    current: "s1",
    messages: [],
    segments: [
      {
        id: "s1",
        number: 1,
        inputTokens: 4000,
        contextWindow: 100000,
        compactions: 0,
      },
    ],
    memories: [],
    jobs: [],
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      paths.push(url);
      if (url.includes("/models?"))
        return {
          ok: true,
          json: async () => ({
            defaults: {
              model: "native-default",
              modelProvider: "fixture",
              reasoningEffort: "low",
            },
            models: [
              {
                model: "native-default",
                displayName: "Native default",
                defaultReasoningEffort: "low",
                supportedReasoningEfforts: [
                  { reasoningEffort: "low" },
                  { reasoningEffort: "high" },
                ],
              },
              {
                model: "other-model",
                displayName: "Other model",
                defaultReasoningEffort: "medium",
                supportedReasoningEfforts: [{ reasoningEffort: "medium" }],
              },
            ],
          }),
        };
      if (url.endsWith("/create")) {
        created.push(JSON.parse(String(init?.body)));
        throw new TypeError("lost create response");
      }
      if (url.endsWith("/send")) {
        const body = JSON.parse(String(init?.body));
        sent.push(body);
        if (lose) throw new TypeError("lost response");
        shadow.messages = [
          {
            id: body.message.id,
            text: body.message.text,
            actor: { id: "C", name: "成员 C" },
            role: "user",
            state: "delivered",
            createdAt: new Date().toISOString(),
            receivedAt: new Date().toISOString(),
          },
        ];
        return { ok: true, json: async () => ({ state: "delivered" }) };
      }
      return {
        ok: true,
        json: async () => ({
          selfId: "C",
          shadows: [shadow],
          total: shadow.messages?.length || 0,
        }),
      };
    }),
  );
});
afterEach(() => {
  cleanup();
  endRefreshState();
  vi.unstubAllGlobals();
});

it("keeps an uncertain message and retries only on click with the same id", async () => {
  const user = userEvent.setup();
  render(<SpaceShadows spaceId="space" disabled={false} />);
  await user.type(
    await screen.findByLabelText("给分身发消息"),
    "继续刚才的问题",
  );
  await user.click(screen.getByRole("button", { name: "发送" }));
  expect(await screen.findByText("未确认送达")).toBeInTheDocument();
  expect(screen.queryByText("已送达")).not.toBeInTheDocument();
  expect(sent).toHaveLength(1);
  const original = sent[0];
  lose = false;
  await user.click(screen.getByRole("button", { name: "重发" }));
  expect(await screen.findByText("已送达")).toBeInTheDocument();
  expect(sent).toEqual([original, original]);
  expect(screen.getAllByText("继续刚才的问题")).toHaveLength(1);
  expect(paths.some((p) => p.includes("/shadows/shadow/local"))).toBe(false);
});

it("shows task and member memory state without requiring a topic choice", async () => {
  shadow.jobs = [
    {
      id: "job",
      title: "后台核对",
      state: "running",
      version: 2,
      threadId: "w1",
    },
  ];
  shadow.memories = [
    {
      id: "mem",
      text: "简短回答",
      scope: "member",
      subjectId: "C",
      state: "active",
      revision: 3,
      sourceIds: ["source"],
    },
  ];
  const user = userEvent.setup();
  render(<SpaceShadows spaceId="space" disabled={false} />);
  await user.click(await screen.findByRole("button", { name: "会话与任务" }));
  const dialog = screen.getByRole("dialog", { name: "会话、任务与记忆" });
  expect(within(dialog).getByText("S1")).toBeVisible();
  expect(within(dialog).getByText("W1")).toBeVisible();
  expect(within(dialog).getByText("空闲 · 可继续聊天")).toBeVisible();
  expect(await screen.findByText("后台核对")).toBeInTheDocument();
  expect(screen.getByText(/我的偏好/)).toBeInTheDocument();
  expect(screen.getByText("简短回答")).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "新问题" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "追问" }),
  ).not.toBeInTheDocument();
  await waitFor(() => expect(sent).toHaveLength(0));
  fireEvent(dialog, new Event("cancel", { cancelable: true }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "会话与任务" })).toHaveFocus(),
  );
  expect(screen.getByLabelText("给分身发消息")).toBeEnabled();
});

it("distinguishes current S from history and work that has not created a session", async () => {
  shadow.current = "s2";
  shadow.segments = [
    {
      id: "s1",
      number: 1,
      inputTokens: 95000,
      contextWindow: 100000,
      compactions: 2,
      closedAt: "2026-11-03T09:00:00Z",
    },
    {
      id: "s2",
      number: 2,
      inputTokens: 4000,
      usageValid: true,
      contextWindow: 100000,
      compactions: 0,
    },
  ];
  shadow.jobs = [
    {
      id: "pending",
      title: "等待执行位置",
      state: "queued",
      version: 1,
      createdAt: "2026-11-03T09:01:00Z",
    },
    {
      id: "work",
      title: "独立核对",
      state: "waiting",
      version: 2,
      threadId: "native-work",
      createdAt: "2026-11-03T09:00:00Z",
    },
    {
      id: "coordination",
      title: "旧协作事项",
      kind: "coordination",
      state: "waiting",
      version: 1,
      threadId: "old-decision",
    },
  ];
  const user = userEvent.setup();
  render(<SpaceShadows spaceId="space" disabled={false} />);
  await user.click(await screen.findByRole("button", { name: "会话与任务" }));
  expect(screen.getByText("S2")).toBeVisible();
  expect(screen.getByText("S1")).not.toBeVisible();
  expect(
    screen.getByRole("progressbar", { name: "主会话上下文用量" }),
  ).toHaveAttribute("value", "4000");
  expect(
    screen.getByRole("article", { name: "W1 · 独立核对" }),
  ).toHaveTextContent("等待新信息");
  expect(
    screen.getByRole("article", { name: "待建会话 · 等待执行位置" }),
  ).toHaveTextContent("尚未创建工作会话");
  expect(screen.queryByText("W2")).not.toBeInTheDocument();
  expect(screen.queryByText("W3")).not.toBeInTheDocument();
  await user.click(screen.getByText(/以前的主会话段/));
  expect(screen.getByText("S1")).toBeVisible();
  expect(screen.getByText("已交接")).toBeVisible();
  expect(paths.some((p) => p.includes("/shadows/shadow/local"))).toBe(false);
  expect(sent).toHaveLength(0);
});

it("does not invent sessions or measured usage before native creation and reporting", async () => {
  shadow.current = "";
  shadow.segments = [];
  const user = userEvent.setup();
  render(<SpaceShadows spaceId="space" disabled={false} />);
  await user.click(await screen.findByRole("button", { name: "会话与任务" }));
  expect(screen.getByText("尚未创建主会话")).toBeVisible();
  expect(screen.getByText("尚未创建工作会话")).toBeVisible();
  expect(screen.queryByText("S1")).not.toBeInTheDocument();
  expect(screen.queryByText("W1")).not.toBeInTheDocument();
  expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
  expect(created).toHaveLength(0);
  expect(sent).toHaveLength(0);
});

it("keeps the last main session visible without presenting stale usage as a measurement", async () => {
  shadow.online = false;
  shadow.busy = true;
  shadow.jobs = [
    {
      id: "work",
      title: "原生结束但未完成",
      state: "needs_followup",
      version: 1,
      threadId: "w1",
      result: "还需核对最后一项",
    },
  ];
  const user = userEvent.setup();
  render(<SpaceShadows spaceId="space" disabled={false} />);
  await user.click(await screen.findByRole("button", { name: "会话与任务" }));
  expect(screen.getByText("离线 · 最后记录")).toBeVisible();
  expect(screen.getByText("等待原生端报告当前上下文用量。")).toBeVisible();
  expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
  expect(
    screen.getByRole("article", { name: "W1 · 原生结束但未完成" }),
  ).toHaveTextContent("需要继续处理");
  expect(screen.queryByText("已完成")).not.toBeInTheDocument();
});

it("restores an unconfirmed send as a manual retry without sending on mount", async () => {
  const id = "saved-id";
  window.localStorage.setItem(
    "teamcross-shadow-pending:space:shadow",
    JSON.stringify([
      {
        id,
        actor: { id: "C", name: "成员 C" },
        role: "user",
        text: "先前的消息",
        state: "sending",
        createdAt: new Date().toISOString(),
      },
    ]),
  );
  render(<SpaceShadows spaceId="space" disabled={false} />);
  expect(await screen.findByText("先前的消息")).toBeInTheDocument();
  expect(
    await screen.findByRole("button", { name: "重发" }),
  ).toBeInTheDocument();
  expect(sent).toHaveLength(0);
});

it("preserves an unsent shadow draft across panel refresh without sending it", async () => {
  beginRefreshState("core-a");
  const user = userEvent.setup();
  const first = render(<SpaceShadows spaceId="space" disabled={false} />);
  await user.type(await screen.findByLabelText("给分身发消息"), "待补充的要求");
  expect(hasRefreshDrafts()).toBe(true);
  const saved = captureRefreshState();
  first.unmount();
  endRefreshState();
  beginRefreshState("core-a", saved);
  const restored = render(<SpaceShadows spaceId="space" disabled={false} />);
  expect(await screen.findByLabelText("给分身发消息")).toHaveValue(
    "待补充的要求",
  );
  expect(hasRefreshDrafts()).toBe(true);
  expect(sent).toHaveLength(0);
  restored.unmount();
  endRefreshState();
  beginRefreshState("core-b", saved);
  render(<SpaceShadows spaceId="space" disabled={false} />);
  expect(await screen.findByLabelText("给分身发消息")).toHaveValue("");
  expect(sent).toHaveLength(0);
});

it("restores an uncertain creation with the same request id and only retries on click", async () => {
  beginRefreshState("core-a");
  const user = userEvent.setup();
  const first = render(<SpaceShadows spaceId="space" disabled={false} />);
  await user.click(screen.getByRole("button", { name: "创建我的分身" }));
  await user.type(screen.getByLabelText("分身名称"), "我的分身");
  await user.type(screen.getByLabelText("工作目录"), "/fixture/project");
  await waitFor(() =>
    expect(screen.getByLabelText("模型")).toHaveValue("native-default"),
  );
  await user.selectOptions(screen.getByLabelText("推理强度"), "high");
  await user.click(screen.getByRole("button", { name: "创建分身" }));
  await screen.findByRole("alert");
  expect(created).toHaveLength(1);
  const original = created[0]!;
  expect(original.settings).toEqual({
    model: "native-default",
    modelProvider: "fixture",
    reasoningEffort: "high",
  });
  const saved = captureRefreshState();
  first.unmount();
  endRefreshState();
  beginRefreshState("core-a", saved);
  render(<SpaceShadows spaceId="space" disabled={false} />);
  expect(await screen.findByLabelText("分身名称")).toHaveValue("我的分身");
  expect(screen.getByLabelText("工作目录")).toHaveValue("/fixture/project");
  expect(screen.getByLabelText("工作目录")).toBeDisabled();
  expect(screen.getByLabelText("模型")).toHaveValue("native-default");
  expect(screen.getByLabelText("推理强度")).toHaveValue("high");
  expect(screen.getByLabelText("模型")).toBeDisabled();
  expect(created).toEqual([original]);
  await user.click(screen.getByRole("button", { name: "核对并重试创建" }));
  await screen.findByRole("alert");
  expect(created).toEqual([original, original]);
});

it("uses per-model effort capabilities and invalidates the catalog when cwd changes", async () => {
  const user = userEvent.setup();
  render(<SpaceShadows spaceId="space" disabled={false} />);
  await user.click(screen.getByRole("button", { name: "创建我的分身" }));
  await user.type(screen.getByLabelText("分身名称"), "选择一次");
  await user.type(screen.getByLabelText("工作目录"), "/fixture");
  await waitFor(() =>
    expect(screen.getByLabelText("模型")).toHaveValue("native-default"),
  );
  await user.selectOptions(screen.getByLabelText("推理强度"), "high");
  await user.selectOptions(screen.getByLabelText("模型"), "other-model");
  expect(screen.getByLabelText("推理强度")).toHaveValue("medium");
  expect(
    screen.queryByRole("option", { name: "high" }),
  ).not.toBeInTheDocument();
  await user.clear(screen.getByLabelText("工作目录"));
  expect(screen.getByRole("button", { name: "创建分身" })).toBeDisabled();
  expect(created).toHaveLength(0);
});
