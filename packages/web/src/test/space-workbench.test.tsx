import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, afterEach, expect, it, vi } from "vitest";
import {
  SpaceWorkbench,
  SpaceRequestButton,
} from "../components/SpaceWorkbench";
import type { Collaboration } from "../types";
import type { SpaceRequest, WorkbenchView } from "../workbench";

const c = {
  id: "space",
  title: "研究空间",
  role: "owner",
  state: "ready",
  materials: [
    {
      id: "material",
      author: "同事",
      versions: [{ version: 1, title: "已确认材料" }],
    },
  ],
  annotations: [],
} as unknown as Collaboration;
const ref = {
  spaceId: "space",
  kind: "material" as const,
  materialId: "material",
  version: 1,
};
let view: WorkbenchView;
let calls: { path: string; body: any }[];
let lost: boolean;
beforeEach(() => {
  lost = false;
  calls = [];
  view = {
    spaceId: "space",
    selfId: "owner",
    targets: [
      {
        id: "peer",
        name: "同事的分析会话",
        member: "同事 B",
        memberId: "member-b",
        provider: "codex",
        available: true,
      },
    ],
    requests: [],
    total: 0,
    brief: {
      revision: 3,
      topic: "验证重试策略",
      decisions: [{ text: "固定材料版本", sources: [ref] }],
      questions: [],
      updatedBy: { memberId: "owner", name: "发起者", kind: "human" },
      updatedAt: new Date().toISOString(),
    },
    assistant: { epoch: 0, revision: 0, state: "disabled" },
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const path = url.replace("/api/", ""),
        body = init?.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ path, body });
      let out: any = {};
      if (path.includes("/workbench/view")) out = view;
      else if (path === "agent-pairings" || path.startsWith("space-receivers?"))
        out = [];
      else if (path.endsWith("/workbench/send")) {
        const request: SpaceRequest = {
          ...body,
          id: body.requestId,
          actor: { memberId: "owner", name: "发起者", kind: "human" },
          state: lost ? "received" : "queued",
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
        };
        view.requests.unshift(request);
        view.total++;
        out = request;
        if (lost) throw new TypeError("lost response");
      } else if (path.includes("/workbench/request?"))
        out = view.requests.find(
          (r) =>
            r.id === new URLSearchParams(path.split("?")[1]).get("requestId"),
        );
      else if (path.endsWith("/workbench/brief")) {
        view.brief = { ...body.brief, revision: body.baseRevision + 1 };
        out = view.brief;
      } else if (path.endsWith("/workbench/assistant")) {
        view.assistant = {
          targetId: body.targetId,
          state: body.state,
          epoch: body.baseEpoch + 1,
          bootstrapId: body.requestId,
          revision: view.brief.revision,
        };
        out = view.assistant;
      }
      return { ok: true, json: async () => structuredClone(out) };
    }),
  );
});
afterEach(() => vi.unstubAllGlobals());

it("shows a paused assistant as unavailable in the participating conversations", async () => {
  view.assistant = { epoch: 1, revision: 3, state: "paused", targetId: "peer" };
  render(
    <SpaceWorkbench collaboration={c} onLocate={() => {}}>
      材料原文
    </SpaceWorkbench>,
  );
  await userEvent.click(screen.getByRole("tab", { name: "参与会话" }));
  const panel = await screen.findByRole("tabpanel", { name: "参与会话" });
  expect(within(panel).getByText("已暂停")).toBeVisible();
  expect(
    within(panel).getByRole("button", { name: "发起协作请求" }),
  ).toBeDisabled();
  expect(calls.filter((c) => c.body)).toHaveLength(0);
});

it("keeps the reader mounted while tabs and keyboard navigation expose shared work", async () => {
  render(
    <SpaceWorkbench collaboration={c} onLocate={() => {}}>
      <div data-testid="reader">
        已发布原文
        <SpaceRequestButton references={[ref]} />
      </div>
    </SpaceWorkbench>,
  );
  await screen.findByRole("button", { name: "专用会话 · 未启用" });
  const reader = screen.getByTestId("reader");
  const reading = screen.getByRole("tab", { name: "阅读与讨论" });
  reading.focus();
  await userEvent.keyboard("{ArrowRight}");
  expect(screen.getByRole("tab", { name: /协作请求/ })).toHaveFocus();
  expect(reader).not.toBeVisible();
  await userEvent.click(reading);
  expect(screen.getByTestId("reader")).toBe(reader);
  expect(reader).toBeVisible();
  expect(calls.filter((c) => c.body)).toHaveLength(0);
});

it("sends the exact reading reference to a space target and queries the same ID after a lost response", async () => {
  lost = true;
  render(
    <SpaceWorkbench collaboration={c} onLocate={() => {}}>
      <SpaceRequestButton references={[ref]} />
    </SpaceWorkbench>,
  );
  await userEvent.click(screen.getByRole("button", { name: "发起空间协作" }));
  const dialog = screen.getByRole("dialog");
  expect(
    within(dialog).getByText("请求、所选引用和结果摘要将对本空间成员可见。"),
  ).toBeVisible();
  await userEvent.selectOptions(
    await within(dialog).findByRole("combobox", { name: "接收会话" }),
    "peer",
  );
  await userEvent.click(
    within(dialog).getByRole("button", { name: "确认发送" }),
  );
  expect(
    await within(dialog).findByText("Agent 已读取请求，尚未报告处理完成"),
  ).toBeVisible();
  const sends = calls.filter((c) => c.path.endsWith("/workbench/send"));
  expect(sends).toHaveLength(1);
  expect(sends[0]!.body).toMatchObject({
    targetId: "peer",
    references: [ref],
    intent: "analyze",
  });
  expect(
    calls.some((c) =>
      c.path.endsWith(`request?requestId=${sends[0]!.body.requestId}`),
    ),
  ).toBe(true);
  expect(
    within(dialog).queryByRole("button", { name: "确认发送" }),
  ).not.toBeInTheDocument();
});

it("preserves brief sources and submits the revision the member reviewed", async () => {
  const locate = vi.fn();
  render(
    <SpaceWorkbench collaboration={c} onLocate={locate}>
      <p>reader</p>
    </SpaceWorkbench>,
  );
  await userEvent.click(screen.getByRole("tab", { name: "空间简报" }));
  await userEvent.click(
    await screen.findByRole("button", { name: /已确认材料 · 版本 1/ }),
  );
  expect(locate).toHaveBeenCalledWith(ref);
  await userEvent.click(screen.getByRole("tab", { name: "空间简报" }));
  await userEvent.click(screen.getByRole("button", { name: "编辑简报" }));
  const topic = screen.getByRole("textbox", { name: "当前议题" });
  await userEvent.clear(topic);
  await userEvent.type(topic, "核对新的验证范围");
  await userEvent.click(
    screen.getByRole("button", { name: "保存确认后的简报" }),
  );
  await waitFor(() =>
    expect(calls.some((c) => c.path.endsWith("/workbench/brief"))).toBe(true),
  );
  expect(
    calls.find((c) => c.path.endsWith("/workbench/brief"))!.body,
  ).toMatchObject({
    baseRevision: 3,
    brief: {
      topic: "核对新的验证范围",
      decisions: [{ text: "固定材料版本", sources: [ref] }],
    },
  });
});

it("reviews the bootstrap before explicit enable and does not call it ready at submission", async () => {
  render(
    <SpaceWorkbench collaboration={c} onLocate={() => {}}>
      <p>reader</p>
    </SpaceWorkbench>,
  );
  await userEvent.click(
    await screen.findByRole("button", { name: "专用会话 · 未启用" }),
  );
  const dialog = screen.getByRole("dialog");
  expect(within(dialog).getByText("启动时会交给它的信息")).toBeVisible();
  expect(calls.filter((c) => c.body)).toHaveLength(0);
  await userEvent.selectOptions(
    within(dialog).getByRole("combobox", { name: "专用接收会话" }),
    "peer",
  );
  await userEvent.click(
    within(dialog).getByRole("button", { name: "启用并发送启动简报" }),
  );
  expect(await within(dialog).findByText(/等待读取与接手确认/)).toBeVisible();
  const posted = calls.filter((c) => c.path.endsWith("/workbench/assistant"));
  expect(posted).toHaveLength(1);
  expect(posted[0]!.body).toMatchObject({
    targetId: "peer",
    state: "initializing",
    baseEpoch: 0,
  });
  expect(screen.queryByText("已接手")).not.toBeInTheDocument();
});
