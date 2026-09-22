import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "../App";
import { Library, SelectionTray } from "../components/Library";
import {
  LibraryProvider,
  ResourceActions,
  sameReference,
  type LibraryResource,
  type LibraryView,
} from "../library";

let data: LibraryView;
let calls: { path: string; body: any }[];
let collaboration: any;
const timestamp = "2026-09-22T03:00:00Z";
const material: LibraryResource = {
  key: "material-key",
  reference: {
    spaceId: "space",
    kind: "material",
    materialId: "m1",
    version: 1,
  },
  title: "连接池调查",
  spaceTitle: "超时问题",
  sessionId: "source",
  sessionTitle: "连接池调查",
  provider: "codex",
  author: "发起者",
  summary: "版本 1 · 公开 1 轮",
  updatedAt: timestamp,
  favorite: false,
  selected: false,
  annotated: true,
  availability: "available",
  replyCount: 0,
};
const annotation: LibraryResource = {
  ...material,
  key: "note-key",
  reference: { spaceId: "space", kind: "annotation", annotationId: "a1" },
  title: "请核对采样范围",
  summary: "连接池等待下降",
  replyCount: 1,
};
const context: LibraryResource = {
  ...material,
  key: "context-key",
  reference: { spaceId: "space", kind: "context" },
  title: "协作上下文",
  summary: "实时内容",
  sessionId: "fork",
  sessionTitle: "超时问题",
  annotated: false,
};
const note = {
  id: "a1",
  text: "请核对采样范围",
  author: "发起者",
  authorId: "owner",
  createdAt: timestamp,
  target: {
    kind: "material",
    materialId: "m1",
    version: 1,
    turnId: "turn",
    itemId: "answer",
    quote: "连接池等待下降",
    startOffset: 0,
    endOffset: 7,
  },
  replies: [
    {
      id: "r1",
      requestId: "reply",
      text: "已核对同一组样本",
      author: "同事",
      createdAt: timestamp,
    },
  ],
};
const page = {
  materialId: "m1",
  version: {
    version: 1,
    title: material.title,
    provider: "codex",
    sourceId: "source",
    turnCount: 1,
    createdAt: timestamp,
  },
  scope: "stream",
  pageEndsAtTurnBoundary: true,
  turns: [{ id: "turn", label: "验证结果" }],
  segments: [
    {
      turnId: "turn",
      itemId: "answer",
      type: "agentMessage",
      text: "连接池等待下降，服务端耗时保持稳定。",
      startOffset: 0,
      endOffset: 19,
      length: 19,
    },
  ],
  nextCursor: "",
};
beforeEach(() => {
  calls = [];
  data = {
    resources: structuredClone([material, annotation, context]),
    selection: [],
  };
  collaboration = {
    id: "space",
    title: "超时问题",
    selfId: "owner",
    role: "owner",
    writer: "owner",
    hasExecution: true,
    state: "ready",
    online: true,
    reachable: true,
    provider: "codex",
    busy: false,
    approvals: 0,
    sessionId: "fork",
    sequence: 0,
    materials: [
      {
        id: "m1",
        author: "发起者",
        authorId: "owner",
        versions: [page.version],
      },
    ],
    annotations: [note],
  };
  localStorage.clear();
  location.hash = "/library";
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const path = url.replace("/api/", ""),
        body = init?.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ path, body });
      let result: unknown = {};
      if (path === "library/state") {
        const resource = data.resources.find((r) =>
          body.key
            ? r.key === body.key
            : sameReference(r.reference, body.reference),
        );
        if (body.action === "clear") {
          data.resources.forEach((r) => (r.selected = false));
          data.selection = [];
        } else if (resource && body.action === "select") {
          resource.selected = body.enabled;
          data.selection = data.resources
            .filter((r) => r.selected)
            .map((r) => r.key);
        } else if (resource && body.action === "favorite")
          resource.favorite = body.enabled;
        else if (resource && body.action === "visit") {
          resource.openedAt = new Date().toISOString();
          data.resources = [
            resource,
            ...data.resources.filter((r) => r.key !== resource.key),
          ];
        }
        result = data;
      } else if (path === "library") result = data;
      else if (path === "library/read")
        result = { resource: annotation, content: note };
      else if (path === "library/bundles")
        result = {
          code: "TC-TESTBUNDLE",
          references: body.references,
          expiresAt: "2026-09-29T03:00:00Z",
        };
      else if (path === "collaborations/space") result = collaboration;
      else if (path === "collaborations/space/read-material") result = page;
      else if (path.includes("context?kind=history"))
        result = {
          thread: {},
          pageCursor: "current-page",
          contentHash: "current-history",
          scope: "stream",
          turns: [{ id: "live-turn", label: "共享会话的后续验证" }],
          segments: [
            {
              ...page.segments[0],
              turnId: "live-turn",
              text: "共享会话的后续验证",
              endOffset: 10,
              length: 10,
            },
          ],
        };
      else if (path === "collaborations/space/rpc")
        result = { turn: { id: "new-turn" } };
      else if (path.includes("context?kind=events")) result = { events: [] };
      else if (path === "info")
        result = {
          host: "本机",
          version: "test",
          binary: "/codex",
          dataDir: "/test",
          mcpCommand: "teamcross mcp",
          mcpConfigured: true,
          mcpClients: {
            codex: { configured: true, command: "" },
            claude: { configured: false, command: "" },
          },
        };
      return { ok: true, json: async () => structuredClone(result) };
    }),
  );
});
function mount(quick = false) {
  return render(
    <LibraryProvider>
      <Library quick={quick} />
      <SelectionTray quick={quick} />
    </LibraryProvider>,
  );
}
async function select(title: string) {
  await userEvent.click(
    await screen.findByRole("checkbox", { name: `选择 ${title}` }),
  );
}
function findMaterialParagraph() {
  return screen.findByText(
    (_, element) =>
      element?.tagName === "P" &&
      element.textContent === "连接池等待下降，服务端耗时保持稳定。",
  );
}

describe("个人资源库", () => {
  it("keeps rows and groups in place after visits, selection, favorites and background refreshes", async () => {
    data.resources = structuredClone([context, material, annotation]);
    const { container } = mount();
    await screen.findByRole("button", { name: "阅读 连接池调查" });
    const rowOrder = () =>
      Array.from(container.querySelectorAll(".library-row-body"), (row) =>
        row.getAttribute("aria-label"),
      );
    const groupOrder = () =>
      Array.from(
        container.querySelectorAll(".library-group-heading strong"),
        (heading) => heading.textContent,
      );
    const initialRows = rowOrder(),
      initialGroups = groupOrder();

    await userEvent.click(
      screen.getByRole("button", { name: `阅读 ${annotation.title}` }),
    );
    await waitFor(() => expect(data.resources[0]!.key).toBe(annotation.key));
    expect(data.resources[0]!.openedAt).toBeTruthy();
    expect(rowOrder()).toEqual(initialRows);
    expect(groupOrder()).toEqual(initialGroups);

    await select(annotation.title);
    expect(
      screen.getByRole("checkbox", { name: `选择 ${annotation.title}` }),
    ).toBeChecked();
    await userEvent.click(
      screen.getByRole("button", {
        name: `收藏 ${context.title}`,
      }),
    );
    expect(
      await screen.findByRole("button", {
        name: `取消收藏 ${context.title}`,
        pressed: true,
      }),
    ).toBeInTheDocument();
    expect(rowOrder()).toEqual(initialRows);
    expect(groupOrder()).toEqual(initialGroups);

    data.resources[0]!.summary = "后台更新后的引用片段";
    fireEvent.focus(window);
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: `阅读 ${annotation.title}` }),
      ).toHaveTextContent("后台更新后的引用片段"),
    );
    expect(rowOrder()).toEqual(initialRows);
    expect(groupOrder()).toEqual(initialGroups);

    await userEvent.click(screen.getByRole("button", { name: "刷新" }));
    await waitFor(() =>
      expect(rowOrder()).toEqual([
        `阅读 ${annotation.title}`,
        `阅读 ${material.title}`,
        `阅读 ${context.title}`,
      ]),
    );
    expect(groupOrder()).toEqual([...initialGroups].reverse());
    expect(
      screen.getByRole("checkbox", { name: `选择 ${annotation.title}` }),
    ).toBeChecked();
  });

  it("applies resource additions, removals and access changes without reshuffling existing rows", async () => {
    data.resources = structuredClone([material, context, annotation]);
    const { container } = mount();
    await screen.findByRole("button", { name: `阅读 ${material.title}` });
    const added = {
      ...material,
      key: "new-material",
      reference: { ...material.reference, materialId: "m2" },
      title: "新增材料",
      sessionId: "new-source",
      sessionTitle: "新增会话",
    };
    data.resources = [
      added,
      { ...annotation, availability: "withdrawn", summary: "内容已撤回" },
      { ...context, favorite: true },
    ];
    fireEvent.focus(window);
    await screen.findByRole("button", { name: "阅读 新增材料" });
    expect(
      Array.from(container.querySelectorAll(".library-row-body"), (row) =>
        row.getAttribute("aria-label"),
      ),
    ).toEqual([
      `阅读 ${annotation.title}`,
      `阅读 ${context.title}`,
      "阅读 新增材料",
    ]);
    expect(
      screen.queryByRole("button", { name: `阅读 ${material.title}` }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: `阅读 ${annotation.title}` }),
    ).toHaveTextContent("内容已撤回");
    expect(
      screen.getByRole("checkbox", { name: `选择 ${annotation.title}` }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", {
        name: `取消收藏 ${context.title}`,
        pressed: true,
      }),
    ).toBeInTheDocument();
  });

  it("shows saved and unsaved favorite states and reports a failed update without changing the state", async () => {
    render(
      <LibraryProvider>
        <ResourceActions reference={material.reference} favorite />
      </LibraryProvider>,
    );
    await userEvent.click(await screen.findByRole("button", { name: "收藏" }));
    const saved = await screen.findByRole("button", {
      name: "取消收藏",
      pressed: true,
    });
    expect(saved).toHaveTextContent("已收藏");
    expect(saved).toHaveClass("is-favorite");
    expect(data.resources[0]!.favorite).toBe(true);

    vi.mocked(fetch).mockResolvedValueOnce(
      new Response(JSON.stringify({ error: "收藏保存失败，请重试" }), {
        status: 503,
      }),
    );
    await userEvent.click(saved);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "收藏保存失败，请重试",
    );
    expect(
      screen.getByRole("button", { name: "取消收藏", pressed: true }),
    ).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "取消收藏" }));
    expect(
      await screen.findByRole("button", {
        name: "收藏",
        pressed: false,
      }),
    ).toHaveTextContent("收藏");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(data.resources[0]!.favorite).toBe(false);
  });

  it("offers one material selection action and selects the version currently being read", async () => {
    collaboration.materials[0].versions.push({
      ...page.version,
      version: 2,
      title: "连接池调查修订版",
      turnCount: 3,
    });
    data.resources.push({
      ...material,
      key: "material-v2",
      reference: { ...material.reference, version: 2 },
    });
    location.hash = "/collaborations/space";
    render(<App />);
    await userEvent.click(
      await screen.findByRole("button", { name: "阅读材料" }),
    );
    const pane = screen.getByRole("tabpanel", { name: /已发布会话材料/ });
    expect(
      within(pane).getByRole("heading", { name: "连接池调查修订版" }),
    ).toBeVisible();
    expect(within(pane).getByText(/公开 3 轮/)).toBeVisible();
    expect(
      within(pane).getAllByRole("button", { name: "加入选择" }),
    ).toHaveLength(1);
    await userEvent.selectOptions(
      within(pane).getByRole("combobox", { name: "查看固定版本" }),
      "1",
    );
    expect(
      within(pane).getByRole("heading", { name: "连接池调查" }),
    ).toBeVisible();
    expect(
      within(pane).queryByText("连接池调查修订版"),
    ).not.toBeInTheDocument();
    expect(within(pane).getByText(/公开 1 轮/)).toBeVisible();
    await userEvent.click(
      within(pane).getByRole("button", { name: "加入选择" }),
    );
    await within(pane).findByRole("button", { name: "已加入选择" });
    expect(data.selection).toEqual(["material-key"]);
    await userEvent.click(within(pane).getByRole("button", { name: "收藏" }));
    await within(pane).findByRole("button", {
      name: "取消收藏",
      pressed: true,
    });
    expect(
      calls
        .filter(
          (c) => c.path === "library/state" && c.body.action === "favorite",
        )
        .at(-1)?.body.reference.version,
    ).toBe(1);
    expect(
      calls
        .filter((c) => c.path === "library/state" && c.body.action === "select")
        .at(-1)?.body.reference.version,
    ).toBe(1);
    await userEvent.click(
      within(pane).getByRole("button", { name: "收起材料" }),
    );
    expect(
      within(pane).getAllByRole("button", { name: "加入选择" }),
    ).toHaveLength(1);
    await userEvent.click(
      within(pane).getByRole("button", { name: "加入选择" }),
    );
    await waitFor(() =>
      expect(data.selection).toEqual(["material-key", "material-v2"]),
    );
  });

  it("switches between materials and context with keyboard access while retaining each reader", async () => {
    location.hash = "/collaborations/space";
    render(<App />);
    await userEvent.click(
      await screen.findByRole("button", { name: "阅读材料" }),
    );
    const text = await findMaterialParagraph();
    const materialPane = screen.getByRole("tabpanel", {
      name: /已发布会话材料/,
    });
    const header = screen
      .getByRole("tablist", {
        name: "协作阅读内容",
      })
      .closest<HTMLElement>(".reading-heading")!;
    expect(
      within(header).getByRole("button", { name: "发布会话材料" }),
    ).toBeVisible();
    expect(
      within(materialPane).queryByRole("heading", { name: /已发布会话材料/ }),
    ).not.toBeInTheDocument();
    expect(
      screen
        .getByRole("button", { name: "专注阅读" })
        .closest(".reading-heading"),
    ).not.toBeNull();
    expect(
      materialPane.querySelector(".reading-surface > .reader-toolbar"),
    ).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "专注阅读" }));
    await userEvent.click(screen.getByRole("tab", { name: "协作上下文" }));
    expect(text).not.toBeVisible();
    expect(
      within(header).getByRole("button", { name: "刷新上下文" }),
    ).toBeVisible();
    expect(
      within(header).queryByRole("button", { name: "发布会话材料" }),
    ).not.toBeInTheDocument();
    expect(
      await screen.findByText("共享会话的后续验证", {
        selector: "[data-source-start]",
      }),
    ).toBeVisible();
    expect(
      screen
        .getByRole("button", { name: "专注阅读" })
        .closest(".context-reader-heading"),
    ).not.toBeNull();
    await userEvent.click(screen.getByRole("tab", { name: "查看文件" }));
    await userEvent.type(
      screen.getByRole("textbox", { name: "相对执行目录的文件路径" }),
      "src/main.ts",
    );
    const contextTab = screen.getByRole("tab", {
      name: "协作上下文",
    });
    contextTab.focus();
    await userEvent.keyboard("{ArrowLeft}");
    expect(screen.getByRole("tab", { name: /已发布会话材料/ })).toHaveFocus();
    expect(text).toBeVisible();
    expect(
      screen.getByRole("button", { name: "退出专注阅读", pressed: true }),
    ).toBeVisible();
    expect(calls.filter((c) => c.path.endsWith("/read-material"))).toHaveLength(
      1,
    );
    await userEvent.keyboard("{End}");
    expect(contextTab).toHaveFocus();
    expect(
      screen.getByRole("textbox", { name: "相对执行目录的文件路径" }),
    ).toHaveValue("src/main.ts");
  });

  it("opens the matching reading tab when locating a material or live context annotation", async () => {
    collaboration.annotations.push({
      ...note,
      id: "live-note",
      text: "核对后续验证",
      target: {
        kind: "history",
        sessionId: "fork",
        turnId: "live-turn",
        itemId: "answer",
        quote: "共享会话的后续验证",
        startOffset: 0,
        endOffset: 10,
      },
    });
    location.hash = "/collaborations/space";
    render(<App />);
    const liveNote = await screen.findByLabelText("批注：核对后续验证");
    await userEvent.click(
      within(liveNote).getByRole("button", { name: /查看原位置/ }),
    );
    expect(
      screen.getByRole("tab", { name: "协作上下文", selected: true }),
    ).toBeVisible();
    const materialNote = screen.getByLabelText("批注：请核对采样范围");
    await userEvent.click(
      within(materialNote).getByRole("button", { name: /查看原位置/ }),
    );
    expect(
      screen.getByRole("tab", { name: /已发布会话材料/, selected: true }),
    ).toBeVisible();
    expect(await findMaterialParagraph()).toBeVisible();
  });

  it("recognizes a selected context excerpt after the Core normalizes target field order", async () => {
    data.resources[2]!.reference.target = {
      kind: "history",
      quote: "原文",
      turnId: "turn",
      itemId: "answer",
      endOffset: 2,
    };
    data.resources[2]!.selected = true;
    data.selection = ["context-key"];
    render(
      <LibraryProvider>
        <ResourceActions
          reference={{
            ...context.reference,
            target: {
              turnId: "turn",
              itemId: "answer",
              startOffset: 0,
              endOffset: 2,
              cursor: "",
              kind: "history",
              quote: "原文",
            },
          }}
        />
      </LibraryProvider>,
    );
    await userEvent.click(
      await screen.findByRole("button", { name: "已加入选择" }),
    );
    await waitFor(() => expect(data.selection).toEqual([]));
  });
  it("keeps Settings and Connections alongside the new resource library", async () => {
    render(<App />);
    const nav = screen.getByRole("navigation", { name: "主导航" });
    expect(
      within(nav).getByRole("link", { name: "协作空间" }),
    ).toBeInTheDocument();
    expect(within(nav).getByRole("link", { name: "资源库" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    await userEvent.click(
      within(nav).getByRole("link", { name: "设置与连接" }),
    );
    expect(
      await screen.findByRole("heading", { name: "设置与连接" }),
    ).toBeInTheDocument();
    expect(calls.some((c) => c.path === "info")).toBe(true);
  });
  it("separates reading from selection and preserves mixed selections through filters", async () => {
    mount();
    await userEvent.click(
      await screen.findByRole("button", { name: "阅读 连接池调查" }),
    );
    expect(await findMaterialParagraph()).toBeInTheDocument();
    expect(data.selection).toEqual([]);
    await select(material.title);
    await userEvent.click(screen.getByRole("button", { name: "批注" }));
    await select(annotation.title);
    await userEvent.type(
      screen.getByRole("textbox", { name: "搜索资源库" }),
      "没有这个关键词",
    );
    expect(await screen.findByText("没有找到匹配的内容")).toBeInTheDocument();
    expect(data.selection).toEqual(["material-key", "note-key"]);
    await userEvent.click(screen.getByRole("button", { name: /已选内容/ }));
    const tray = screen.getByRole("region", { name: "当前选择清单" });
    expect(within(tray).getByText("连接池调查")).toBeInTheDocument();
    expect(within(tray).getByText("请核对采样范围")).toBeInTheDocument();
    expect(calls.some((c) => c.path.endsWith("/rpc"))).toBe(false);
  });
  it("shows original quote and replies, and filters favorites without sending input", async () => {
    mount();
    await userEvent.click(
      await screen.findByRole("button", { name: "收藏 请核对采样范围" }),
    );
    await userEvent.click(screen.getByRole("button", { name: /已收藏/ }));
    expect(
      screen.queryByRole("button", { name: "阅读 连接池调查" }),
    ).not.toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: "阅读 请核对采样范围" }),
    );
    expect(await screen.findByText("已核对同一组样本")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /查看原位置/ }),
    ).toHaveTextContent("连接池等待下降");
    expect(calls.some((c) => c.path.endsWith("/rpc"))).toBe(false);
  });
  it("generates an inline immutable entry while keeping browsing and selection usable", async () => {
    mount();
    await select(material.title);
    await select(annotation.title);
    await userEvent.click(screen.getByRole("button", { name: "生成读取入口" }));
    expect(await screen.findByText("TC-TESTBUNDLE")).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    const tray = screen.getByRole("region", { name: "当前选择清单" });
    expect(
      within(tray).getByRole("region", { name: "个人 Agent 读取入口" }),
    ).toBeInTheDocument();
    await userEvent.click(screen.getByText("查看提示与所选内容"));
    expect(
      screen.getByRole("textbox", { name: "个人 Agent 读取提示" }),
    ).toHaveValue(
      "请使用 Team Cross 的 read_selection 读取 TC-TESTBUNDLE，核对原文后分析。",
    );
    const call = calls.find((c) => c.path === "library/bundles")!;
    expect(call.body.references).toEqual([
      material.reference,
      annotation.reference,
    ]);
    await select(context.title);
    expect(
      await screen.findByText("选择已变化，重新生成可更新入口。"),
    ).toBeInTheDocument();
    expect(calls.filter((c) => c.path === "library/bundles")).toHaveLength(1);
    await userEvent.type(
      screen.getByRole("textbox", { name: "搜索资源库" }),
      "继续阅读",
    );
    expect(await screen.findByText("没有找到匹配的内容")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "重新生成" }));
    await waitFor(() =>
      expect(calls.filter((c) => c.path === "library/bundles")).toHaveLength(2),
    );
    const regenerated = calls.filter((c) => c.path === "library/bundles")[1]!;
    expect(regenerated.body.references).toHaveLength(3);
    expect(regenerated.body.requestId).not.toBe(call.body.requestId);
    await userEvent.click(screen.getByRole("button", { name: "收起读取入口" }));
    expect(
      screen.queryByRole("region", { name: "个人 Agent 读取入口" }),
    ).not.toBeInTheDocument();
    expect(data.selection).toHaveLength(3);
    expect(calls.some((c) => c.path.endsWith("/rpc"))).toBe(false);
  });
  it("previews shared input and reports receipt separately from completion", async () => {
    mount();
    await select(annotation.title);
    await userEvent.click(
      screen.getByRole("button", { name: "发送到共享会话" }),
    );
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "确认发送" })).toBeEnabled(),
    );
    expect(calls.some((c) => c.path.endsWith("/rpc"))).toBe(false);
    await userEvent.click(screen.getByRole("button", { name: "确认发送" }));
    expect(
      await screen.findByText("共享会话已接收，尚未确认执行完成。"),
    ).toBeInTheDocument();
    const sent = calls.filter((c) => c.path.endsWith("/rpc"));
    expect(sent).toHaveLength(1);
    expect(sent[0]!.body.method).toBe("turn/start");
    expect(sent[0]!.body.params.input[0].text).toContain(
      '"annotationId": "a1"',
    );
  });
  it("blocks sending without input ownership", async () => {
    collaboration.writer = "someone-else";
    mount();
    await select(annotation.title);
    await userEvent.click(
      screen.getByRole("button", { name: "发送到共享会话" }),
    );
    expect(
      await screen.findByText(/当前输入权属于其他参与者/),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "确认发送" })).toBeDisabled();
    expect(calls.some((c) => c.path.endsWith("/rpc"))).toBe(false);
  });
  it("keeps mixed-space selection available to a personal Agent while blocking shared sends", async () => {
    data.resources[1]!.reference.spaceId = "another-space";
    mount();
    await select(material.title);
    await select(annotation.title);
    expect(
      screen.getByRole("button", { name: "发送到共享会话" }),
    ).toBeDisabled();
    expect(screen.getByRole("button", { name: "生成读取入口" })).toBeEnabled();
    expect(calls.some((c) => c.path.endsWith("/rpc"))).toBe(false);
  });
  it("removes cached previews on access loss but allows removing unavailable selections", async () => {
    mount();
    await select(annotation.title);
    await userEvent.click(
      screen.getByRole("button", { name: "阅读 请核对采样范围" }),
    );
    expect(await screen.findByText("已核对同一组样本")).toBeInTheDocument();
    data.resources.find((r) => r.key === annotation.key)!.availability =
      "unavailable";
    await userEvent.click(screen.getByRole("button", { name: "刷新" }));
    await waitFor(() =>
      expect(screen.queryByText("已核对同一组样本")).not.toBeInTheDocument(),
    );
    expect(screen.getByRole("button", { name: "生成读取入口" })).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: /已选内容/ }));
    await userEvent.click(
      screen.getByRole("button", { name: "移除 请核对采样范围" }),
    );
    await waitFor(() => expect(data.selection).toEqual([]));
  });
  it("uses the same selection in quick view and opens full reading through the native bridge", async () => {
    const postMessage = vi.fn();
    Object.defineProperty(window, "webkit", {
      configurable: true,
      value: { messageHandlers: { teamcross: { postMessage } } },
    });
    mount(true);
    await select(material.title);
    await userEvent.click(
      screen.getByRole("button", { name: "阅读 连接池调查" }),
    );
    expect(postMessage).toHaveBeenCalledWith({
      action: "open",
      route: "/library?item=material-key",
    });
    await userEvent.click(screen.getByRole("button", { name: "固定为浮窗" }));
    expect(postMessage).toHaveBeenCalledWith({ action: "pin" });
    fireEvent(window, new CustomEvent("teamcross-pinned", { detail: true }));
    expect(
      screen.getByRole("button", { name: "取消固定浮窗" }),
    ).toHaveAttribute("aria-pressed", "true");
    expect(data.selection).toEqual(["material-key"]);
    delete window.webkit;
  });
});
