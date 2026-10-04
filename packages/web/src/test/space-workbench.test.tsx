import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, afterEach, expect, it, vi } from "vitest";
import {
  SpaceWorkbench,
  SpaceRequestButton,
} from "../components/SpaceWorkbench";
import type { Collaboration } from "../types";
import type { SpaceRequest, WorkbenchView } from "../workbench";
import {
  setCurrentConversation,
  type DeliveryState,
} from "../currentConversation";

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
let pairs: { id: string; name: string; state: string }[];
beforeEach(() => {
  lost = false;
  calls = [];
  pairs = [];
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
      else if (path === "agent-pairings") out = pairs;
      else if (path.startsWith("space-receivers?")) out = [];
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
afterEach(() => {
  vi.unstubAllGlobals();
  setCurrentConversation(undefined);
});

it("adds an existing conversation only after selection and confirmation in the add dialog", async () => {
  pairs = [{ id: "paired", name: "本机分析", state: "paired" }];
  render(
    <SpaceWorkbench collaboration={c} onLocate={() => {}}>
      reader
    </SpaceWorkbench>,
  );
  await userEvent.click(screen.getByRole("tab", { name: "参与会话" }));
  await userEvent.click(
    await screen.findByRole("button", { name: "添加会话" }),
  );
  const dialog = screen.getByRole("dialog", { name: "添加参与会话" });
  expect(
    within(dialog).getByRole("button", { name: "关联到本空间" }),
  ).toBeDisabled();
  await userEvent.click(
    within(dialog).getByRole("radio", { name: /新建独立会话/ }),
  );
  await userEvent.type(
    within(dialog).getByRole("textbox", { name: "接收会话名称" }),
    " UI draft",
  );
  await userEvent.click(
    within(dialog).getByRole("radio", { name: /关联已有会话/ }),
  );
  expect(calls.filter((call) => call.body)).toHaveLength(0);
  expect(
    within(dialog).queryByRole("button", { name: "创建并关联接收会话" }),
  ).not.toBeInTheDocument();
  await userEvent.selectOptions(
    within(dialog).getByRole("combobox", { name: "本机已配对会话" }),
    "paired",
  );
  await userEvent.click(
    within(dialog).getByRole("button", { name: "关联到本空间" }),
  );
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(calls.filter((call) => call.body)).toEqual([
    {
      path: "collaborations/space/workbench/register",
      body: { pairingId: "paired" },
    },
  ]);
});

it("connects the current conversation without a name step and keeps the submitted state distinct from receiving", async () => {
  let state: DeliveryState | undefined;
  const connect = vi.fn(async () => {
    state = "sent";
  });
  setCurrentConversation({ status: () => state, send: vi.fn(), connect });
  render(
    <SpaceWorkbench collaboration={c} onLocate={() => {}}>
      reader
    </SpaceWorkbench>,
  );
  await userEvent.click(screen.getByRole("tab", { name: "参与会话" }));
  const button = await screen.findByRole("button", { name: "连接当前会话" });
  expect(
    screen.getByRole("textbox", { name: "接收会话名称" }),
  ).not.toBeVisible();
  expect(connect).not.toHaveBeenCalled();
  await userEvent.click(button);
  expect(connect).toHaveBeenCalledExactlyOnceWith("space", "");
  expect(
    await screen.findByText(
      "连接要求已带回当前对话，请查看身份与接收能力的确认结果。",
    ),
  ).toBeVisible();
  expect(button).toBeDisabled();
});

it("keeps chosen fixed sources and an unsaved brief while searching sources and changing tabs", async () => {
  const published = structuredClone(c);
  published.materials![0]!.versions = Array.from({ length: 7 }, (_, i) => ({
    ...published.materials![0]!.versions[0]!,
    version: i + 1,
  }));
  render(
    <SpaceWorkbench collaboration={published} onLocate={() => {}}>
      reader
    </SpaceWorkbench>,
  );
  await userEvent.click(screen.getByRole("tab", { name: "空间简报" }));
  await userEvent.click(
    await screen.findByRole("button", { name: "编辑简报" }),
  );
  await userEvent.click(screen.getByText("关联已发布来源"));
  await userEvent.type(
    screen.getByRole("searchbox", { name: "搜索已发布来源" }),
    "版本 2",
  );
  await userEvent.click(
    screen.getByRole("checkbox", { name: "已确认材料 · 版本 2" }),
  );
  await userEvent.click(screen.getByRole("tab", { name: "阅读与讨论" }));
  await userEvent.click(screen.getByRole("tab", { name: "空间简报" }));
  expect(screen.getByText("草稿尚未保存")).toBeVisible();
  expect(calls.filter((call) => call.body)).toHaveLength(0);
  await userEvent.clear(
    screen.getByRole("searchbox", { name: "搜索已发布来源" }),
  );
  expect(
    screen.getByRole("checkbox", { name: "已确认材料 · 版本 1" }),
  ).toBeChecked();
  expect(
    screen.getByRole("checkbox", { name: "已确认材料 · 版本 2" }),
  ).toBeChecked();
  await userEvent.click(
    screen.getByRole("button", { name: "保存确认后的简报" }),
  );
  await waitFor(() =>
    expect(calls.filter((call) => call.body)).toHaveLength(1),
  );
  expect(
    calls.find((call) => call.body)!.body.brief.decisions[0].sources,
  ).toEqual([ref, { ...ref, version: 2 }]);
});

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

it("keeps source progress nearby and proposes results without saving a decision automatically", async () => {
  const request = {
    id: "request-done",
    targetId: "peer",
    actor: { memberId: "owner", name: "Member", kind: "human" },
    references: [ref],
    instruction: "Review retries",
    intent: "analyze",
    state: "completed",
    summary: "Test one uncertain delivery before retrying",
    briefRevision: 2,
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
  } as SpaceRequest;
  view.requests = [request];
  view.total = 1;
  render(
    <SpaceWorkbench collaboration={c} onLocate={() => {}}>
      <SpaceRequestButton references={[ref]} />
    </SpaceWorkbench>,
  );
  await userEvent.click(
    await screen.findByRole("button", {
      name: /相关请求：Agent 已报告处理完成/,
    }),
  );
  const dialog = await screen.findByRole("dialog");
  await userEvent.click(
    await within(dialog).findByRole("button", { name: "拟为决定" }),
  );
  expect(screen.getByRole("tab", { name: "空间简报" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  expect(screen.getByRole("textbox", { name: "已确认决定 2" })).toHaveValue(
    request.summary,
  );
  expect(calls.some((c) => c.path.endsWith("/workbench/brief") && c.body)).toBe(
    false,
  );
  await userEvent.click(
    screen.getByRole("button", { name: "保存确认后的简报" }),
  );
  await waitFor(() =>
    expect(
      calls.some((c) => c.path.endsWith("/workbench/brief") && c.body),
    ).toBe(true),
  );
  const saved = calls.find(
    (c) => c.path.endsWith("/workbench/brief") && c.body,
  )!.body;
  expect(saved.baseRevision).toBe(3);
  expect(saved.brief.decisions[1]).toEqual({
    text: request.summary,
    sources: [ref],
    requestId: request.id,
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
