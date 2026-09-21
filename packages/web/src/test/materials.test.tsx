import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { Detail } from "../components/Detail";
import { Annotations } from "../components/Annotations";
import { Materials } from "../components/Materials";
import type { Collaboration, Material } from "../types";

const material: Material = {
  id: "m1",
  author: "A",
  authorId: "owner",
  versions: [
    {
      version: 1,
      title: "网络验证",
      provider: "codex",
      sourceId: "source",
      startTurnId: "t1",
      endTurnId: "t2",
      turnCount: 2,
      hash: "hash",
      createdAt: new Date().toISOString(),
      noticeCount: 0,
    },
  ],
};
const readonly = {
  id: "space",
  title: "多人调查",
  role: "owner",
  selfId: "owner",
  hasExecution: false,
  reachable: true,
  state: "ready",
  sharing: false,
  host: "A 的 Mac",
  annotations: [],
  materials: [material],
} as unknown as Collaboration;
const json = (data: unknown, status = 200) =>
  new Response(JSON.stringify(data), {
    status,
    headers: { "Content-Type": "application/json" },
  });
beforeEach(() => {
  vi.restoreAllMocks();
  localStorage.clear();
  sessionStorage.clear();
});

it("a read-only space lists metadata without native context or automatic material reads", async () => {
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input);
      calls.push(path);
      if (path === "/api/collaborations/space") return json(readonly);
      return json({});
    }),
  );
  render(<Detail id="space" />);
  await screen.findByRole("heading", { name: "多人调查" });
  expect(screen.getByRole("button", { name: "发布会话材料" })).toBeEnabled();
  expect(
    screen.queryByRole("button", { name: /恢复运行时|申请输入|接回输入/ }),
  ).not.toBeInTheDocument();
  expect(
    calls.some((c) => c.includes("context") || c.includes("read-material")),
  ).toBe(false);
});

it("folds long tool output and reads the selected item without advancing the stream", async () => {
  const user = userEvent.setup(),
    bodies: Record<string, unknown>[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path.endsWith("/read-material")) {
        const body = JSON.parse(String(init?.body)) as Record<string, unknown>;
        bodies.push(body);
        if (body.itemId)
          return json({
            materialId: "m1",
            version: material.versions[0],
            scope: "item",
            itemComplete: true,
            pageEndsAtTurnBoundary: false,
            nextCursor: "",
            segments: [
              {
                turnId: "t1",
                itemId: "tool",
                type: "toolResult",
                text: "-TAIL",
                startOffset: 5,
                endOffset: 10,
                length: 10,
              },
            ],
          });
        return json({
          materialId: "m1",
          version: material.versions[0],
          scope: "stream",
          pageEndsAtTurnBoundary: true,
          nextCursor: "",
          turns: [{ id: "t1", label: "验证工具输出" }],
          segments: [
            {
              turnId: "t1",
              itemId: "tool",
              type: "toolResult",
              text: "HEAD-",
              startOffset: 0,
              endOffset: 5,
              length: 10,
              collapsed: true,
              notice: "已折叠，共 10 字",
            },
          ],
        });
      }
      return json({});
    }),
  );
  render(
    <Materials
      collaboration={readonly}
      reload={() => {}}
      onAnnotate={() => {}}
    />,
  );
  await user.click(screen.getByRole("button", { name: "阅读材料" }));
  expect(
    (await screen.findAllByText(/工具输出 · 共 10 字 · 已显示 5 字/))[0],
  ).toBeVisible();
  await user.click(screen.getByRole("button", { name: "读取完整输出" }));
  await waitFor(() =>
    expect(
      screen.queryByRole("button", { name: "读取完整输出" }),
    ).not.toBeInTheDocument(),
  );
  expect(
    screen.getByText("HEAD--TAIL", { selector: "[data-source-start]" }),
  ).toBeInTheDocument();
  expect(bodies[0]).toMatchObject({ includeOutline: true });
  expect(bodies[1]).toMatchObject({
    turnId: "t1",
    itemId: "tool",
    startOffset: 5,
  });
  expect(bodies[1]).not.toHaveProperty("cursor");
});

it("turns the open material button into collapse and matches the reading panel close action", async () => {
  const user = userEvent.setup();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      if (String(input).endsWith("/read-material"))
        return json({
          materialId: "m1",
          version: 1,
          turns: [{ id: "t1", label: "验证" }],
          segments: [
            {
              turnId: "t1",
              itemId: "i1",
              type: "agentMessage",
              text: "已公开正文",
              startOffset: 0,
              endOffset: 5,
            },
          ],
        });
      return json({});
    }),
  );
  render(
    <Materials
      collaboration={readonly}
      reload={() => {}}
      onAnnotate={() => {}}
    />,
  );
  await user.click(screen.getByRole("button", { name: "阅读材料" }));
  expect(await screen.findByRole("button", { name: "收起材料" })).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "阅读材料" }),
  ).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "收起正文" })).toBeVisible();
  expect(document.querySelector(".material-reading")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "收起材料" }));
  expect(document.querySelector(".material-reading")).toBeNull();
  expect(
    screen.queryByRole("button", { name: "收起正文" }),
  ).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "阅读材料" })).toBeVisible();
});

it("keeps material provenance on the reader toolbar until the source details are opened", async () => {
  const user = userEvent.setup();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      if (String(input).endsWith("/read-material"))
        return json({
          materialId: "m1",
          version: 1,
          turns: [{ id: "t1", label: "验证" }],
          segments: [
            {
              turnId: "t1",
              itemId: "i1",
              type: "agentMessage",
              text: "已公开正文",
              startOffset: 0,
              endOffset: 5,
            },
          ],
        });
      return json({});
    }),
  );
  render(
    <Materials
      collaboration={readonly}
      reload={() => {}}
      onAnnotate={() => {}}
    />,
  );
  await user.click(screen.getByRole("button", { name: "阅读材料" }));
  const summary = await screen.findByText(/codex · 公开 2 轮 · 版本 1/);
  const details = summary.closest("details.reader-provenance");
  expect(details).toBeTruthy();
  expect(details?.parentElement).toHaveClass("material-reader-toolbar");
  expect((details as HTMLDetailsElement).open).toBe(false);
  await user.click(summary);
  expect((details as HTMLDetailsElement).open).toBe(true);
  expect(screen.getByText("来源 source · 0 处导出说明")).toBeVisible();
});

it("an ended read-only membership offers a new invitation without execution controls", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      json({ ...readonly, role: "remote", state: "ended", reachable: false }),
    ),
  );
  render(<Detail id="space" />);
  expect(
    await screen.findByRole("link", { name: "使用邀请链接加入" }),
  ).toBeVisible();
  expect(
    screen.queryByRole("heading", { name: "协作上下文" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByText(/受限模式/)).not.toBeInTheDocument();
});

it("keeps an attachment pinned when a newer material version arrives", async () => {
  const user = userEvent.setup(),
    saved = vi.fn(),
    calls: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_: RequestInfo | URL, init?: RequestInit) => {
      calls.push(JSON.parse(String(init?.body)));
      return json({
        id: "note",
        text: "意见",
        author: "A",
        createdAt: new Date().toISOString(),
        replies: [],
      });
    }),
  );
  const annotation = {
    id: "note",
    text: "意见",
    author: "A",
    createdAt: new Date().toISOString(),
  };
  const { rerender } = render(
    <Annotations
      id="space"
      annotations={[annotation]}
      materials={[material]}
      disabled={false}
      onSaved={saved}
      onLocate={() => {}}
    />,
  );
  await user.click(screen.getByRole("button", { name: "回复" }));
  await user.click(screen.getAllByRole("button", { name: "引用材料" })[1]!);
  await user.click(screen.getByRole("button", { name: /网络验证 · v1/ }));
  const updated = {
    ...material,
    versions: [...material.versions, { ...material.versions[0]!, version: 2 }],
  };
  rerender(
    <Annotations
      id="space"
      annotations={[annotation]}
      materials={[updated]}
      disabled={false}
      onSaved={saved}
      onLocate={() => {}}
    />,
  );
  fireEvent.change(screen.getByLabelText("回复这条批注"), {
    target: { value: "核对这个版本" },
  });
  await user.click(screen.getByRole("button", { name: "保存回复" }));
  await waitFor(() => expect(saved).toHaveBeenCalled());
  expect(calls[0]).toMatchObject({
    materials: [{ materialId: "m1", version: 1 }],
  });
});
