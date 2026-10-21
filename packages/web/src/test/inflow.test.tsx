import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { InFlowControl, LeaveSpaceNote } from "../components/InFlow";

afterEach(() => vi.unstubAllGlobals());

it("only changes the exact selected execution and waits for explicit opt-in", async () => {
  const calls: any[] = [];
  const changed = vi.fn();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      calls.push({ url, body: JSON.parse(String(init.body)) });
      return { ok: true, json: async () => ({ enabled: true }) };
    }),
  );
  render(
    <InFlowControl
      spaceId="space"
      executionId="execution-a"
      enabled={false}
      canConfigure
      disabled={false}
      onChanged={changed}
    />,
  );
  expect(calls).toHaveLength(0);
  await userEvent.click(screen.getByRole("checkbox", { name: "顺流感知" }));
  await waitFor(() => expect(changed).toHaveBeenCalledOnce());
  expect(calls).toEqual([
    {
      url: "/api/collaborations/space/workbench/inflow-config",
      body: { executionId: "execution-a", enabled: true },
    },
  ]);
});

it("lets another member read the preference without changing it", () => {
  render(
    <InFlowControl
      spaceId="space"
      targetId="other"
      enabled
      canConfigure={false}
      disabled={false}
      onChanged={() => {}}
    />,
  );
  expect(screen.getByRole("checkbox", { name: "顺流感知" })).toBeChecked();
  expect(screen.getByRole("checkbox", { name: "顺流感知" })).toBeDisabled();
});

it("saves a targeted note without starting execution and reconciles a lost response by the same ID", async () => {
  const calls: any[] = [];
  const changed = vi.fn();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      const body = JSON.parse(String(init.body));
      calls.push({ url, body });
      if (url.endsWith("inflow-note")) throw new TypeError("lost");
      return {
        ok: true,
        json: async () => ({
          id: body.requestId,
          text: "留到下次",
          targetId: "linked-only",
        }),
      };
    }),
  );
  render(
    <LeaveSpaceNote
      spaceId="space"
      targetId="linked-only"
      name="个人会话"
      enabled
      disabled={false}
      notes={[]}
      onChanged={changed}
    />,
  );
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "留言" }));
  await user.type(
    screen.getByRole("textbox", { name: "留言正文" }),
    "留到下次",
  );
  await user.click(screen.getByRole("button", { name: "保存留言" }));
  await screen.findByRole("button", { name: "核对留言状态" });
  expect(screen.getByRole("textbox", { name: "留言正文" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "核对留言状态" }));
  await waitFor(() => expect(changed).toHaveBeenCalledOnce());
  expect(calls).toHaveLength(2);
  expect(calls[0].body).toMatchObject({
    targetId: "linked-only",
    instruction: "留到下次",
  });
  expect(calls[1]).toEqual({
    url: "/api/collaborations/space/workbench/inflow-note-read",
    body: { requestId: calls[0].body.requestId },
  });
});

it("shows delivered as transport state and blocks new notes until enabled", async () => {
  render(
    <LeaveSpaceNote
      spaceId="space"
      targetId="peer"
      name="会话"
      enabled={false}
      disabled={false}
      notes={[
        {
          id: "note",
          targetId: "peer",
          text: "旧留言",
          actor: { memberId: "owner", name: "成员", kind: "human" },
          createdAt: "2026-10-21T10:00:00Z",
          deliveredAt: "2026-10-21T10:01:00Z",
        },
      ]}
      onChanged={() => {}}
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: "留言" }));
  expect(screen.getByRole("button", { name: "保存留言" })).toBeDisabled();
  expect(screen.getByText(/已投递表示提醒已写出/)).toBeVisible();
  expect(screen.getByText("旧留言")).toBeVisible();
});

it("retries a missing note only on an explicit click, with its original ID and content", async () => {
  const calls: any[] = [];
  const changed = vi.fn();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      const body = JSON.parse(String(init.body));
      calls.push({ url, body });
      if (calls.length === 1) throw new TypeError("lost");
      if (calls.length === 2)
        return {
          ok: false,
          json: async () => ({
            code: "inflow_note_not_found",
            error: "没有找到空间留言",
          }),
        };
      return { ok: true, json: async () => ({ id: body.requestId }) };
    }),
  );
  render(
    <LeaveSpaceNote
      spaceId="space"
      targetId="target"
      name="会话"
      enabled
      disabled={false}
      notes={[]}
      onChanged={changed}
    />,
  );
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "留言" }));
  await user.type(
    screen.getByRole("textbox", { name: "留言正文" }),
    "相同内容",
  );
  await user.click(screen.getByRole("button", { name: "保存留言" }));
  await user.click(await screen.findByRole("button", { name: "核对留言状态" }));
  const retry = await screen.findByRole("button", { name: "重试保存留言" });
  expect(calls).toHaveLength(2);
  await user.click(retry);
  await waitFor(() => expect(changed).toHaveBeenCalledOnce());
  expect(calls[2]).toEqual(calls[0]);
});

it("keeps a definitely rejected draft editable", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => ({
      ok: false,
      json: async () => ({
        code: "inflow_note_rejected",
        error: "接收会话尚未开启顺流感知",
      }),
    })),
  );
  render(
    <LeaveSpaceNote
      spaceId="space"
      targetId="target"
      name="会话"
      enabled
      disabled={false}
      notes={[]}
      onChanged={() => {}}
    />,
  );
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "留言" }));
  await user.type(
    screen.getByRole("textbox", { name: "留言正文" }),
    "保留草稿",
  );
  await user.click(screen.getByRole("button", { name: "保存留言" }));
  await screen.findByText("接收会话尚未开启顺流感知");
  expect(screen.getByRole("textbox", { name: "留言正文" })).toBeEnabled();
  expect(screen.getByRole("textbox", { name: "留言正文" })).toHaveValue(
    "保留草稿",
  );
  expect(
    screen.queryByRole("button", { name: "核对留言状态" }),
  ).not.toBeInTheDocument();
});
