import { cleanup, render, screen, waitFor } from "@testing-library/react";
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
    { id: "job", title: "后台核对", state: "running", version: 2 },
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
  await user.click(await screen.findByRole("button", { name: "记忆与任务" }));
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
