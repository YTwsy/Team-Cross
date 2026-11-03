import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, afterEach, expect, it, vi } from "vitest";
import {
  SpaceWorkbench,
  SpaceRequestButton,
} from "../components/SpaceWorkbench";
import type { Collaboration } from "../types";
import type { SpaceReceiver, SpaceRequest, WorkbenchView } from "../workbench";
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
const briefRef = {
  spaceId: ref.spaceId,
  materialId: ref.materialId,
  version: ref.version,
};
let view: WorkbenchView;
let calls: { path: string; body: any }[];
let lost: boolean;
let pairs: { id: string; name: string; state: string }[];
let receivers: SpaceReceiver[];
beforeEach(() => {
  lost = false;
  calls = [];
  pairs = [];
  receivers = [];
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
      decisions: [{ text: "固定材料版本", sources: [briefRef] }],
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
      else if (path.includes("/shadows/view"))
        out = { selfId: "owner", shadows: [], total: 0 };
      else if (path === "agent-pairings") out = pairs;
      else if (path.startsWith("space-receivers?")) out = receivers;
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

it("drains a paused receiver before exposing Desktop continuation and same-session resume", async () => {
  receivers = [
    {
      id: "receiver-handoff",
      spaceId: "space",
      name: "交接助手",
      state: "ready",
      sessionId: "native-saved-id",
      online: true,
      busy: true,
      activeTurnId: "native-turn-id",
      approvals: 0,
      pairingId: "same-pair",
    },
  ];
  const user = userEvent.setup();
  render(
    <SpaceWorkbench collaboration={c} onLocate={() => {}}>
      reader
    </SpaceWorkbench>,
  );
  await user.click(screen.getByRole("tab", { name: "参与会话" }));
  await screen.findByRole("button", { name: "暂停接收并释放" });
  expect(
    screen.queryByRole("button", { name: "在 Codex Desktop 中继续" }),
  ).not.toBeInTheDocument();
  receivers[0]!.receivingPaused = true;
  receivers[0]!.releasePending = true;
  await user.click(screen.getByRole("button", { name: "暂停接收并释放" }));
  await screen.findByText("正在释放");
  expect(calls.filter((c) => c.body?.action)).toEqual([
    {
      path: "space-receivers/receiver-handoff/action",
      body: { action: "pause" },
    },
  ]);
  expect(
    screen.queryByRole("button", { name: "恢复接收会话" }),
  ).not.toBeInTheDocument();
  receivers[0]!.online = false;
  receivers[0]!.busy = false;
  receivers[0]!.activeTurnId = undefined;
  receivers[0]!.releasePending = false;
  await user.click(screen.getByRole("button", { name: "停止当前轮次" }));
  await screen.findByText("已暂停接收");
  expect(calls.find((c) => c.body?.action === "interrupt")?.body).toEqual({
    action: "interrupt",
    turnId: "native-turn-id",
    requestId: expect.any(String),
  });
  await user.click(
    screen.getByRole("button", { name: "在 Codex Desktop 中继续" }),
  );
  expect(calls.find((c) => c.body?.action === "open-desktop")?.path).toBe(
    "space-receivers/receiver-handoff/action",
  );
  receivers[0]!.online = true;
  receivers[0]!.receivingPaused = false;
  await user.click(screen.getByRole("button", { name: "恢复接收会话" }));
  await screen.findByRole("button", { name: "暂停接收并释放" });
  expect(calls.find((c) => c.body?.action === "start")?.path).toBe(
    "space-receivers/receiver-handoff/action",
  );
  expect(
    calls.some(
      (c) =>
        c.path.endsWith("/workbench/create-receiver") ||
        c.path.endsWith("/workbench/send"),
    ),
  ).toBe(false);
});

it("shows the recovered CLI and retries the existing receiver only on an explicit click", async () => {
  receivers = [
    {
      id: "receiver-original",
      spaceId: "space",
      name: "空间协作助手",
      state: "error",
      sessionId: "",
      online: false,
      busy: false,
      approvals: 0,
      pairingId: "original",
      binary: "/Applications/ChatGPT.app/new/codex",
      canRetryCreation: true,
      clientRecovered: true,
    },
  ];
  render(
    <SpaceWorkbench collaboration={c} onLocate={() => {}}>
      reader
    </SpaceWorkbench>,
  );
  await userEvent.click(screen.getByRole("tab", { name: "参与会话" }));
  expect(
    await screen.findByText("/Applications/ChatGPT.app/new/codex"),
  ).toBeVisible();
  expect(
    screen.getByText("Codex CLI 已恢复，尚未创建原生会话。可以重试创建。"),
  ).toBeVisible();
  expect(screen.queryByText("Codex 路径不可用")).not.toBeInTheDocument();
  expect(calls.filter((call) => call.body)).toEqual([]);
  await userEvent.click(
    screen.getByRole("button", { name: "重试创建接收会话" }),
  );
  expect(calls.filter((call) => call.body)).toEqual([
    {
      path: "space-receivers/receiver-original/action",
      body: { action: "retry" },
    },
  ]);
});

it("keeps receiver creation disabled when the current CLI is missing", async () => {
  receivers = [
    {
      id: "receiver-original",
      spaceId: "space",
      name: "空间协作助手",
      state: "error",
      sessionId: "",
      online: false,
      busy: false,
      approvals: 0,
      pairingId: "original",
      canRetryCreation: true,
      clientError: "Codex CLI 文件不存在",
    },
  ];
  render(
    <SpaceWorkbench collaboration={c} onLocate={() => {}}>
      reader
    </SpaceWorkbench>,
  );
  await userEvent.click(screen.getByRole("tab", { name: "参与会话" }));
  expect(await screen.findByText("Codex CLI 文件不存在")).toBeVisible();
  expect(
    screen.getByRole("button", { name: "重试创建接收会话" }),
  ).toBeDisabled();
  expect(calls.filter((call) => call.body)).toEqual([]);
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
  await userEvent.click(screen.getByRole("tab", { name: "空间概览" }));
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
  await userEvent.click(screen.getByRole("button", { name: "保存简报" }));
  await waitFor(() =>
    expect(calls.filter((call) => call.body)).toHaveLength(1),
  );
  expect(
    calls.find((call) => call.body)!.body.brief.decisions[0].sources,
  ).toEqual([briefRef, { ...briefRef, version: 2 }]);
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
  const overview = screen.getByRole("tab", { name: "空间概览" });
  overview.focus();
  await userEvent.keyboard("{ArrowRight}");
  expect(screen.getByRole("tab", { name: /本机数字分身/ })).toHaveFocus();
  await userEvent.keyboard("{ArrowRight}");
  expect(screen.getByRole("tab", { name: /协作请求/ })).toHaveFocus();
  expect(reader).not.toBeVisible();
  await userEvent.click(overview);
  await userEvent.click(screen.getByRole("button", { name: /已确认材料/ }));
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
  await userEvent.click(screen.getByRole("button", { name: "保存简报" }));
  await waitFor(() =>
    expect(calls.some((c) => c.path.endsWith("/workbench/brief"))).toBe(true),
  );
  expect(
    calls.find((c) => c.path.endsWith("/workbench/brief"))!.body,
  ).toMatchObject({
    baseRevision: 3,
    brief: {
      topic: "核对新的验证范围",
      decisions: [{ text: "固定材料版本", sources: [briefRef] }],
    },
  });
});

it("submits the material and public annotation confirmed in the unified picker", async () => {
  const note = {
    id: "public-note",
    author: "同事",
    text: "核对重试边界",
    createdAt: "",
  };
  render(
    <SpaceWorkbench
      collaboration={{ ...c, annotations: [note] }}
      onLocate={() => {}}
    >
      <SpaceRequestButton references={[ref]} />
    </SpaceWorkbench>,
  );
  await userEvent.click(screen.getByRole("button", { name: "发起空间协作" }));
  const request = screen.getByRole("dialog");
  await userEvent.click(
    within(request).getByRole("button", { name: "添加引用" }),
  );
  const picker = screen.getByRole("dialog", { name: "添加引用" });
  await userEvent.click(within(picker).getByRole("button", { name: /^批注/ }));
  await userEvent.click(
    within(picker).getByRole("checkbox", { name: "选择 核对重试边界" }),
  );
  await userEvent.click(
    within(picker).getByRole("button", { name: "添加 2 项引用" }),
  );
  expect(calls.filter((call) => call.body)).toHaveLength(0);
  await userEvent.selectOptions(
    await within(request).findByRole("combobox", { name: "接收会话" }),
    "peer",
  );
  await userEvent.click(
    within(request).getByRole("button", { name: "确认发送" }),
  );
  const sent = calls.filter((call) => call.path.endsWith("/workbench/send"));
  expect(sent).toHaveLength(1);
  expect(sent[0]!.body.references).toEqual([
    ref,
    { spaceId: c.id, kind: "annotation", annotationId: note.id },
  ]);
  expect(
    within(request).getByRole("button", { name: "添加引用" }),
  ).toBeDisabled();
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
  expect(screen.getByRole("textbox", { name: "决定与建议 2" })).toHaveValue(
    request.summary,
  );
  expect(calls.some((c) => c.path.endsWith("/workbench/brief") && c.body)).toBe(
    false,
  );
  await userEvent.click(screen.getByRole("button", { name: "保存简报" }));
  await waitFor(() =>
    expect(
      calls.some((c) => c.path.endsWith("/workbench/brief") && c.body),
    ).toBe(true),
  );
  const saved = calls.find(
    (c) => c.path.endsWith("/workbench/brief") && c.body,
  )!.body;
  expect(saved.baseRevision).toBe(3);
  expect(saved.brief.decisions[1]).toMatchObject({
    itemId: expect.any(String),
    status: "proposed",
    text: request.summary,
    sources: [briefRef],
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

it("keeps item identity, status and other progress while editing the brief", async () => {
  view.brief.decisions[0] = {
    ...view.brief.decisions[0]!,
    itemId: "contract",
    status: "proposed",
    updatedBy: {
      memberId: "owner",
      name: "Owner shadow",
      kind: "shadow",
      shadowId: "shadow",
    },
  };
  view.brief.progress = [
    {
      itemId: "peer-progress",
      text: "Peer is validating",
      status: "blocked",
      sources: [],
    },
  ];
  render(
    <SpaceWorkbench collaboration={c} onLocate={() => {}}>
      <p>reader</p>
    </SpaceWorkbench>,
  );
  await userEvent.click(screen.getByRole("tab", { name: "空间简报" }));
  expect(await screen.findByText("建议 · Owner shadow")).toBeVisible();
  expect(screen.getByText("受阻")).toBeVisible();
  await userEvent.click(screen.getByRole("button", { name: "编辑简报" }));
  const statuses = screen.getAllByRole("combobox", { name: "条目状态" });
  await userEvent.selectOptions(statuses[0]!, "disputed");
  await userEvent.click(screen.getByRole("button", { name: "保存简报" }));
  await waitFor(() =>
    expect(calls.some((call) => call.path.endsWith("/workbench/brief"))).toBe(
      true,
    ),
  );
  const saved = calls.find((call) =>
    call.path.endsWith("/workbench/brief"),
  )!.body;
  expect(saved.requestId).toMatch(/^[0-9a-f-]{36}$/);
  expect(saved.brief.decisions[0]).toMatchObject({
    itemId: "contract",
    status: "disputed",
  });
  expect(saved.brief.progress).toEqual(view.brief.progress);
});

it("merges a concurrent brief without discarding the draft or new peer progress", async () => {
  const original = globalThis.fetch;
  const base = structuredClone(view.brief);
  let conflicted = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (String(url).includes("/workbench/brief-read")) {
        return {
          ok: true,
          json: async () => ({
            brief: String(url).includes("revision=") ? base : view.brief,
          }),
        };
      }
      if (
        !conflicted &&
        String(url).endsWith("/workbench/brief") &&
        init?.body
      ) {
        conflicted = true;
        calls.push({ path: String(url), body: JSON.parse(String(init.body)) });
        view.brief = {
          ...view.brief,
          revision: 4,
          topic: "Concurrent peer topic",
          decisions: [
            {
              ...view.brief.decisions[0]!,
              text: "Peer edited decision",
              sources: null as unknown as [],
            },
          ],
          progress: [
            {
              itemId: "peer-progress",
              text: "New peer progress",
              status: "in_progress",
              sources: [],
            },
          ],
        };
        return {
          ok: true,
          json: async () => ({
            state: "conflict",
            brief: view.brief,
            conflicts: ["$topic"],
          }),
        };
      }
      return original(url, init);
    }),
  );
  render(
    <SpaceWorkbench collaboration={c} onLocate={() => {}}>
      <p>reader</p>
    </SpaceWorkbench>,
  );
  await userEvent.click(screen.getByRole("tab", { name: "空间简报" }));
  await userEvent.click(
    await screen.findByRole("button", { name: "编辑简报" }),
  );
  await userEvent.clear(screen.getByRole("textbox", { name: "当前议题" }));
  await userEvent.type(
    screen.getByRole("textbox", { name: "当前议题" }),
    "My retained draft",
  );
  await userEvent.clear(screen.getByRole("textbox", { name: "决定与建议 1" }));
  await userEvent.type(
    screen.getByRole("textbox", { name: "决定与建议 1" }),
    "My decision",
  );
  await userEvent.click(screen.getByRole("button", { name: "保存简报" }));
  const dialog = await screen.findByRole("dialog", { name: "合并简报修改" });
  expect(within(dialog).getByText("My retained draft")).toBeVisible();
  expect(within(dialog).getByText("Concurrent peer topic")).toBeVisible();
  expect(within(dialog).getByText("Peer edited decision")).toBeVisible();
  await userEvent.click(within(dialog).getByRole("button", { name: "关闭" }));
  expect(screen.getByRole("textbox", { name: "当前议题" })).toHaveValue(
    "My retained draft",
  );
  expect(
    calls.filter((call) => call.path.endsWith("/workbench/brief") && call.body),
  ).toHaveLength(1);
  await userEvent.click(screen.getByRole("button", { name: "合并最新内容" }));
  await userEvent.click(
    (await screen.findAllByRole("button", { name: "保留我的修改" }))[0]!,
  );
  await userEvent.click(screen.getByRole("button", { name: "保留我的修改" }));
  expect(screen.getByRole("textbox", { name: "工作进展 1" })).toHaveValue(
    "New peer progress",
  );
  await userEvent.click(screen.getByRole("button", { name: "保存简报" }));
  await waitFor(() =>
    expect(screen.queryByText("草稿尚未保存")).not.toBeInTheDocument(),
  );
  const posts = calls.filter(
    (call) => call.path.endsWith("/workbench/brief") && call.body,
  );
  expect(posts).toHaveLength(2);
  expect(posts[1]!.body.baseRevision).toBe(4);
  expect(posts[1]!.body.requestId).not.toBe(posts[0]!.body.requestId);
  expect(view.brief.topic).toBe("My retained draft");
  expect(view.brief.decisions[0]!.text).toBe("My decision");
  expect(view.brief.progress![0]!.text).toBe("New peer progress");
});

it("reconciles a lost brief response by command ID without submitting again", async () => {
  const original = globalThis.fetch;
  let requestId = "";
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (String(url).endsWith("/workbench/brief") && init?.body) {
        requestId = JSON.parse(String(init.body)).requestId;
        await original(url, init);
        throw new Error("lost response");
      }
      if (String(url).includes("/workbench/brief-read?requestId=")) {
        calls.push({ path: String(url), body: undefined });
        return {
          ok: true,
          json: async () => ({
            state: "saved",
            requestId,
            revision: 4,
            brief: view.brief,
          }),
        };
      }
      return original(url, init);
    }),
  );
  render(
    <SpaceWorkbench collaboration={c} onLocate={() => {}}>
      <p>reader</p>
    </SpaceWorkbench>,
  );
  await userEvent.click(screen.getByRole("tab", { name: "空间简报" }));
  await userEvent.click(
    await screen.findByRole("button", { name: "编辑简报" }),
  );
  await userEvent.click(screen.getByRole("button", { name: "保存简报" }));
  await waitFor(() =>
    expect(screen.queryByText("草稿尚未保存")).not.toBeInTheDocument(),
  );
  expect(
    calls.filter((call) => call.path.endsWith("/workbench/brief") && call.body),
  ).toHaveLength(1);
  expect(
    calls.some((call) =>
      call.path.endsWith(`/workbench/brief-read?requestId=${requestId}`),
    ),
  ).toBe(true);
});
