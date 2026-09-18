import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { Detail } from "../components/Detail";
import { Create } from "../components/Create";
import type { Collaboration } from "../types";

const space = {
  id: "space",
  title: "审阅空间",
  host: "A 的 Mac",
  role: "owner",
  selfId: "owner",
  hasExecution: false,
  state: "ready",
  sharing: false,
  reachable: true,
  annotations: [],
  materials: [],
  members: [],
} as unknown as Collaboration;
const json = (value: unknown) =>
  new Response(JSON.stringify(value), {
    headers: { "Content-Type": "application/json" },
  });
beforeEach(() => {
  vi.restoreAllMocks();
  sessionStorage.clear();
  localStorage.clear();
});

it.each([false, true])(
  "closes an empty read-only space, including sharing=%s, and can reopen it",
  async (sharing) => {
    const user = userEvent.setup();
    let current = {
      ...space,
      sharing,
      invitation: sharing ? "reusable" : undefined,
      invitationId: "link",
    };
    const calls: { path: string; body: Record<string, unknown> }[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input),
          body = init?.body ? JSON.parse(String(init.body)) : {};
        if (init?.method === "POST") {
          calls.push({ path, body });
          if (body.action === "end")
            current = {
              ...current,
              state: "ended",
              sharing: false,
              invitation: undefined,
            };
          if (path.endsWith("/invitations"))
            current = {
              ...current,
              state: "ready",
              sharing: true,
              invitation: "reopened",
            };
        }
        return json(current);
      }),
    );
    render(<Detail id="space" />);
    await user.click(await screen.findByRole("button", { name: "关闭空间" }));
    await user.click(screen.getByRole("button", { name: "确认关闭空间" }));
    await screen.findByText(
      "空间已关闭。材料和讨论保留，重新开放后可以继续邀请同事。",
    );
    expect(calls.some((c) => c.body.action === "end")).toBe(true);
    expect(
      screen.queryByRole("button", { name: "关闭空间" }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "重新开放空间" }));
    await user.click(screen.getByRole("button", { name: "生成邀请链接" }));
    expect(
      await screen.findByRole("button", { name: "复制邀请链接" }),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "关闭空间" })).toBeVisible();
  },
);

it("keeps one reusable invitation and separates link rotation and closure from ending the space", async () => {
  const user = userEvent.setup();
  let current = {
    ...space,
    sharing: true,
    invitation: "same-link",
    invitationId: "link",
    invitationState: "active",
  };
  const bodies: Record<string, unknown>[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input),
        body = init?.body ? JSON.parse(String(init.body)) : {};
      if (init?.method === "POST") {
        bodies.push(body);
        if (path.endsWith("/invitations"))
          current = {
            ...current,
            invitation: "new-link",
            invitationId: "new",
            invitationState: "active",
          };
        if (path.endsWith("/revoke-invitation"))
          current = { ...current, invitation: "", invitationState: "revoked" };
      }
      return json(current);
    }),
  );
  render(<Detail id="space" />);
  await user.click(await screen.findByRole("button", { name: "邀请成员" }));
  expect(screen.getByRole("button", { name: "复制邀请链接" })).toBeVisible();
  expect(screen.queryByText(/为另一位.*生成邀请/)).not.toBeInTheDocument();
  expect(bodies).toHaveLength(0);
  await user.click(screen.getByRole("button", { name: "重置邀请链接" }));
  expect(bodies[0]).toMatchObject({
    reset: true,
    transport: "lan",
    requestId: expect.any(String),
  });
  await user.click(screen.getByRole("button", { name: "关闭链接加入" }));
  await screen.findByRole("button", { name: "重新开放链接" });
  expect(bodies[1]).toEqual({ invitationId: "new" });
  expect(bodies.some((b) => b.action === "end")).toBe(false);
});

it("enables execution in the existing space without sharing again or generating a second invitation", async () => {
  const user = userEvent.setup();
  const calls: { path: string; body: Record<string, unknown> }[] = [];
  const source = {
    id: "source",
    name: "来源会话",
    preview: "上下文",
    cwd: "/project",
    updatedAt: 1,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input),
        body = init?.body ? JSON.parse(String(init.body)) : {};
      calls.push({ path, body });
      if (path.startsWith("/api/sources")) return json({ data: [source] });
      if (path === "/api/preview")
        return json({
          source,
          previewHash: "confirmed",
          workspace: {
            sourceCwd: "/project",
            repo: "/project",
            head: "abc",
            branch: "main",
          },
        });
      return json({ ...space, hasExecution: true });
    }),
  );
  render(<Create spaceId="space" />);
  await user.click(await screen.findByRole("radio", { name: /来源会话/ }));
  await user.click(screen.getByRole("button", { name: /下一步/ }));
  const enable = await screen.findByRole("button", { name: "启用共同执行" });
  await waitFor(() => expect(enable).toBeEnabled());
  expect(screen.queryByText("选择连接方式")).not.toBeInTheDocument();
  await user.click(enable);
  expect(
    calls.find((c) => c.path === "/api/collaborations")?.body,
  ).toMatchObject({ spaceId: "space", previewHash: "confirmed" });
  expect(
    calls.some(
      (c) => c.path.endsWith("/action") || c.path.endsWith("/invitations"),
    ),
  ).toBe(false);
  expect(sessionStorage.getItem("teamcross.invite.space")).toBeNull();
});

it("keeps a member in discussion after execution is enabled until execution access is granted", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      json({
        ...space,
        role: "remote",
        executionAvailable: true,
        sharing: true,
      }),
    ),
  );
  render(<Detail id="space" />);
  await screen.findByText(/发起者已启用共同执行/);
  expect(
    screen.queryByRole("button", { name: "申请输入" }),
  ).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "附上这次调查" })).toBeEnabled();
  expect(screen.queryByText("这次共享已结束")).not.toBeInTheDocument();
});
