import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, afterEach, expect, it, vi } from "vitest";
import { SpaceShadows } from "../components/SpaceShadows";
import type { Shadow } from "../shadows";

let shadow: Shadow;
let sent: { shadowId: string; message: { id: string; text: string } }[];
let lose: boolean;
let paths: string[];
beforeEach(() => {
  window.localStorage.clear();
  paths = [];
  sent = [];
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
afterEach(() => vi.unstubAllGlobals());

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
