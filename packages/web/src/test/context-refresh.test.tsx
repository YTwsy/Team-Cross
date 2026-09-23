import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Detail } from "../components/Detail";
import type { Collaboration } from "../types";

const collaboration: Collaboration = {
  id: "scroll-test",
  title: "上下文刷新",
  role: "owner",
  writer: "owner",
  online: true,
  sharing: true,
  executionCwd: "/fixture",
  host: "测试 Mac",
  workspaceMode: "existing",
  state: "ready",
  annotations: [],
  approvals: 0,
  sourceId: "source",
  sessionId: "fork",
  repo: "/fixture",
  head: "abcdef",
  branch: "main",
  createdAt: "2026-09-10T00:00:00Z",
  updatedAt: "2026-09-10T00:00:00Z",
  busy: false,
  connected: false,
  epoch: 1,
  sequence: 0,
};

function response(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), { status });
}

function history(text = "已加载的对话") {
  return response({
    thread: {},
    contentHash: text,
    pageCursor: "page",
    nextCursor: "",
    scope: "stream",
    sourcePageComplete: true,
    pageEndsAtTurnBoundary: true,
    turns: [{ id: "turn-1", label: text }],
    segments: [
      {
        turnId: "turn-1",
        itemId: "answer",
        type: "agentMessage",
        text,
        startOffset: 0,
        endOffset: text.length,
        length: text.length,
      },
    ],
  });
}

function outlinedHistory(turn: number, nextCursor = "remaining") {
  const text = `第 ${turn} 轮正文`;
  return response({
    thread: {},
    contentHash: "outlined-page",
    pageCursor: "page-fixed",
    nextCursor,
    scope: "stream",
    sourcePageComplete: !nextCursor,
    pageEndsAtTurnBoundary: true,
    turns: [1, 2, 3].map((i) => ({ id: `turn-${i}`, label: `第 ${i} 轮目录` })),
    segments: [
      {
        turnId: `turn-${turn}`,
        itemId: `question-${turn}`,
        type: "userMessage",
        text,
        startOffset: 0,
        endOffset: text.length,
        length: text.length,
      },
    ],
  });
}

function deferred() {
  let resolve!: (value: Response) => void;
  const promise = new Promise<Response>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

let current: Collaboration;
let readContext: (url: URL) => Response | Promise<Response>;
let contextRequests: string[];

beforeEach(() => {
  vi.useFakeTimers();
  sessionStorage.clear();
  current = { ...collaboration };
  contextRequests = [];
  readContext = () => history();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      if (path.includes("/context?")) {
        contextRequests.push(path);
        return readContext(new URL(path, "http://localhost"));
      }
      return response(current);
    }),
  );
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

async function openDetail() {
  await act(async () => {
    render(<Detail id={current.id} />);
  });
}

async function poll() {
  current = { ...current, sequence: current.sequence + 1 };
  await act(async () => {
    await vi.advanceTimersByTimeAsync(2500);
  });
}

describe("协作上下文刷新", () => {
  it.each(["owner", "remote"] as const)(
    "%s 的对话在状态刷新期间保留容器和阅读位置，并显示更新结果",
    async (role) => {
      current.role = role;
      await openDetail();
      const message = screen.getByText("已加载的对话", {
        selector: "[data-source-start]",
      });
      const container = message.closest(".history")!;
      container.scrollTop = 160;
      const next = deferred();
      readContext = () => next.promise;

      await poll();
      expect(contextRequests).toHaveLength(2);
      expect(message).toBeVisible();
      expect(container).toBeInTheDocument();
      expect(screen.queryByText("正在加载…")).not.toBeInTheDocument();

      await act(async () => {
        next.resolve(history("对话有了新内容"));
      });
      expect(message).toBeVisible();
      expect(
        screen.queryByText("对话有了新内容", {
          selector: "[data-source-start]",
        }),
      ).not.toBeInTheDocument();
      await act(async () => {
        fireEvent.click(screen.getByRole("button", { name: "显示新内容" }));
      });
      expect(
        screen
          .getByText("对话有了新内容", { selector: "[data-source-start]" })
          .closest(".history"),
      ).toBe(container);
      expect(container.scrollTop).toBe(160);
    },
  );

  it("手动刷新失败保留对话，重试后原位更新", async () => {
    await openDetail();
    const message = screen.getByText("已加载的对话", {
      selector: "[data-source-start]",
    });
    const container = message.closest(".history");
    const next = deferred();
    readContext = () => next.promise;

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "刷新上下文" }));
    });
    expect(message).toBeVisible();
    await act(async () => {
      next.resolve(response({ error: "上下文读取失败" }, 503));
    });
    expect(screen.getByRole("alert")).toHaveTextContent("上下文读取失败");
    expect(message).toBeVisible();

    readContext = () => history("重试后的对话");
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "重试" }));
    });
    expect(
      screen
        .getByText("重试后的对话", { selector: "[data-source-start]" })
        .closest(".history"),
    ).toBe(container);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("切换标签和文件时清除前一个资源，不把旧内容当作新结果", async () => {
    await openDetail();
    const changes = deferred();
    readContext = () => changes.promise;
    await act(async () => {
      fireEvent.click(screen.getByRole("tab", { name: "代码改动" }));
    });
    expect(
      screen.queryByText("已加载的对话", { selector: "[data-source-start]" }),
    ).not.toBeInTheDocument();
    expect(screen.getByText("正在加载…")).toBeVisible();
    await act(async () => {
      changes.resolve(
        response({ diff: "+代码改动", status: "M src/a.ts", stat: "" }),
      );
    });
    expect(screen.getByText("+代码改动")).toBeVisible();

    await act(async () => {
      fireEvent.click(screen.getByRole("tab", { name: "查看文件" }));
    });
    expect(screen.queryByText("+代码改动")).not.toBeInTheDocument();
    readContext = () => response({ text: "第一个文件的内容" });
    const input = screen.getByRole("textbox", {
      name: "相对执行目录的文件路径",
    });
    await act(async () => {
      fireEvent.change(input, { target: { value: "src/a.ts" } });
      fireEvent.submit(input.closest("form")!);
    });
    expect(screen.getByText("第一个文件的内容")).toBeVisible();

    const file = deferred();
    readContext = () => file.promise;
    await act(async () => {
      fireEvent.change(input, { target: { value: "src/b.ts" } });
      fireEvent.submit(input.closest("form")!);
    });
    expect(screen.queryByText("第一个文件的内容")).not.toBeInTheDocument();
    expect(screen.getByText("正在加载…")).toBeVisible();
    await act(async () => {
      file.resolve(response({ text: "第二个文件的内容" }));
    });
    expect(screen.getByText("第二个文件的内容")).toBeVisible();
  });

  it("在概览处切换上下文标签时保留当前页面位置", async () => {
    let pageY = 160;
    vi.spyOn(window, "scrollY", "get").mockImplementation(() => pageY);
    vi.spyOn(window, "scrollTo").mockImplementation((options) => {
      const target = options as number | ScrollToOptions;
      if (typeof target === "object") pageY = target.top || 0;
    });
    readContext = (url) =>
      url.searchParams.get("kind") === "changes"
        ? response({ diff: "+新改动", status: "M src/a.ts", stat: "" })
        : history();
    await openDetail();
    const workspace = document.querySelector<HTMLElement>(
      ".collaboration-reading",
    )!;
    vi.spyOn(workspace, "getBoundingClientRect").mockImplementation(
      () => ({ top: 500 - pageY, height: 1200 }) as DOMRect,
    );
    await act(async () => {
      fireEvent.click(screen.getByRole("tab", { name: "协作上下文" }));
    });

    pageY = 210;
    await act(async () => {
      fireEvent.click(screen.getByRole("tab", { name: "代码改动" }));
    });
    expect(screen.getByText("+新改动")).toBeVisible();
    expect(pageY).toBe(210);

    pageY = 250;
    await act(async () => {
      fireEvent.click(screen.getByRole("tab", { name: "最近对话" }));
    });
    expect(pageY).toBe(250);

    pageY = 290;
    await act(async () => {
      fireEvent.click(screen.getByRole("tab", { name: "代码改动" }));
    });
    expect(pageY).toBe(290);
  });

  it("较早的刷新响应不会覆盖后来的对话", async () => {
    await openDetail();
    const older = deferred();
    readContext = () => older.promise;
    await poll();
    readContext = () => history("最新对话");
    await poll();
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "显示新内容" }));
    });
    expect(
      screen.getByText("最新对话", { selector: "[data-source-start]" }),
    ).toBeVisible();
    await act(async () => {
      older.resolve(history("过期对话"));
    });
    expect(
      screen.getByText("最新对话", { selector: "[data-source-start]" }),
    ).toBeVisible();
    expect(
      screen.queryByText("过期对话", { selector: "[data-source-start]" }),
    ).not.toBeInTheDocument();
  });

  it("长工具输出可按 pageCursor 和 item 坐标继续读取", async () => {
    readContext = (url) => {
      if (url.searchParams.get("itemId") === "tool")
        return response({
          thread: {},
          contentHash: "stable-page",
          pageCursor: "page-fixed",
          nextCursor: "",
          scope: "item",
          itemComplete: true,
          pageEndsAtTurnBoundary: true,
          segments: [
            {
              turnId: "turn-1",
              itemId: "tool",
              type: "commandExecution",
              text: "后半段与测试汇总。",
              startOffset: 4,
              endOffset: 13,
              length: 13,
            },
          ],
        });
      return response({
        thread: {},
        contentHash: "stable-page",
        pageCursor: "page-fixed",
        nextCursor: "",
        scope: "stream",
        sourcePageComplete: true,
        pageEndsAtTurnBoundary: true,
        turns: [{ id: "turn-1", label: "工具调用" }],
        segments: [
          {
            turnId: "turn-1",
            itemId: "tool",
            type: "commandExecution",
            text: "开头日志",
            startOffset: 0,
            endOffset: 4,
            length: 13,
            collapsed: true,
          },
        ],
      });
    };
    await openDetail();
    expect(screen.getAllByText(/命令与结果 · 共 13 字/).length).toBeGreaterThan(
      0,
    );

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "读取完整输出" }));
    });

    const request = new URL(contextRequests.at(-1)!, "http://localhost");
    expect(request.searchParams.get("cursor")).toBe("page-fixed");
    expect(request.searchParams.get("turnId")).toBe("turn-1");
    expect(request.searchParams.get("itemId")).toBe("tool");
    expect(request.searchParams.get("startOffset")).toBe("4");
    expect(
      screen.getByText("开头日志后半段与测试汇总。", {
        selector: "[data-source-start]",
      }),
    ).toBeInTheDocument();
  });
});

describe("协作对话目录导航", () => {
  let scrolledTurns: (string | undefined)[];
  beforeEach(() => {
    scrolledTurns = [];
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
      configurable: true,
      value: function (this: HTMLElement) {
        scrolledTurns.push(this.dataset.readerTurn);
      },
    });
    readContext = () => outlinedHistory(1);
  });
  afterEach(() => {
    Reflect.deleteProperty(HTMLElement.prototype, "scrollIntoView");
  });

  it("读取未加载的轮次后定位，已加载轮次直接跳转，继续分页和返回最近对话清除定位参数", async () => {
    await openDetail();
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "专注阅读" }));
    });
    const next = deferred();
    readContext = () => next.promise;
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "目录 3" }));
    });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /02 第 2 轮目录/ }));
    });
    const request = new URL(contextRequests.at(-1)!, "http://localhost");
    expect(request.searchParams.get("cursor")).toBe("page-fixed");
    expect(request.searchParams.get("turnId")).toBe("turn-2");
    expect(request.searchParams.has("itemId")).toBe(false);
    expect(screen.getByText("正在加载…")).toBeVisible();
    expect(scrolledTurns).toEqual([]);
    await act(async () => next.resolve(outlinedHistory(2, "after-turn-2")));
    expect(
      screen.getByText("第 2 轮正文", { selector: "[data-source-start]" }),
    ).toBeVisible();
    expect(scrolledTurns).toEqual(["turn-2"]);
    expect(
      screen.getByRole("button", { name: "退出专注阅读" }),
    ).toHaveAttribute("aria-pressed", "true");
    const requestCount = contextRequests.length;
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "目录 3" }));
    });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /02 第 2 轮正文/ }));
    });
    expect(contextRequests).toHaveLength(requestCount);
    expect(scrolledTurns).toEqual(["turn-2", "turn-2"]);

    readContext = () => outlinedHistory(3, "");
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "继续读取本页" }));
    });
    const continuation = new URL(contextRequests.at(-1)!, "http://localhost");
    expect(continuation.searchParams.get("cursor")).toBe("after-turn-2");
    expect(continuation.searchParams.has("turnId")).toBe(false);
    expect(
      screen.getByText("第 3 轮正文", { selector: "[data-source-start]" }),
    ).toBeVisible();

    readContext = () => outlinedHistory(1);
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "回到最近对话" }));
    });
    const recent = new URL(contextRequests.at(-1)!, "http://localhost");
    expect(recent.searchParams.get("cursor")).toBe("");
    expect(recent.searchParams.has("turnId")).toBe(false);
    expect(
      screen.getByText("第 1 轮正文", { selector: "[data-source-start]" }),
    ).toBeVisible();
    expect(scrolledTurns).toEqual(["turn-2", "turn-2"]);
  });

  it("目标轮次读取失败可以重试，完成后仍定位到所选轮次", async () => {
    await openDetail();
    readContext = () => response({ error: "目标轮次读取失败" }, 503);
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "目录 3" }));
    });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /03 第 3 轮目录/ }));
    });
    expect(screen.getByRole("alert")).toHaveTextContent("目标轮次读取失败");
    expect(scrolledTurns).toEqual([]);

    readContext = () => outlinedHistory(3, "");
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "重试" }));
    });
    expect(
      new URL(contextRequests.at(-1)!, "http://localhost").searchParams.get(
        "turnId",
      ),
    ).toBe("turn-3");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(scrolledTurns).toEqual(["turn-3"]);
  });

  it("定位尚未完成时切换标签，不让迟到响应滚动页面或覆盖当前内容", async () => {
    await openDetail();
    const next = deferred();
    readContext = () => next.promise;
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "目录 3" }));
    });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /02 第 2 轮目录/ }));
    });
    readContext = () =>
      response({ diff: "+当前代码改动", status: "M src/a.ts", stat: "" });
    await act(async () => {
      fireEvent.click(screen.getByRole("tab", { name: "代码改动" }));
    });
    await act(async () => next.resolve(outlinedHistory(2)));
    expect(screen.getByText("+当前代码改动")).toBeVisible();
    expect(
      screen.queryByText("第 2 轮正文", { selector: "[data-source-start]" }),
    ).not.toBeInTheDocument();
    expect(scrolledTurns).toEqual([]);
  });
});
