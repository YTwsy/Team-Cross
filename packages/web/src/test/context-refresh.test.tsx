import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
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
    thread: {
      turns: [{ id: "turn-1", items: [{ type: "agentMessage", text }] }],
    },
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
});
