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

describe("个人资源库", () => {
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
    expect(
      await screen.findByText("连接池等待下降，服务端耗时保持稳定。"),
    ).toBeInTheDocument();
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
    data.resources[1]!.availability = "unavailable";
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
