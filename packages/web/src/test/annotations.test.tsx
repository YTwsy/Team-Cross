import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Detail } from "../components/Detail";
import {
  codeLines,
  codeTargetMatches,
  diffLines,
  lineTarget,
} from "../annotations";
import type { Annotation, AnnotationTarget, Collaboration } from "../types";

const hash = "a".repeat(64);
const base = "b".repeat(40);
const original = "请检查中文🙂，再检查中文🙂。";
let current: Collaboration;
let calls: { path: string; body: any }[];
let file: { path: string; text: string; contentHash: string };
let rejectSave: boolean;
let history: (cursor: string | null) => unknown;
const note = (target: AnnotationTarget): Annotation => ({
  id: "saved",
  text: "请核对这里",
  target,
  author: "协作者",
  createdAt: new Date().toISOString(),
});

beforeEach(() => {
  sessionStorage.clear();
  current = {
    id: "annotations",
    title: "批注协作",
    role: "remote",
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
    head: base,
    branch: "main",
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
    busy: false,
    connected: false,
    epoch: 1,
    sequence: 0,
  };
  calls = [];
  rejectSave = false;
  file = {
    path: "src/示例.ts",
    text: "first()\nsecond()\nthird()",
    contentHash: hash,
  };
  history = () => ({
    thread: {
      turns: [
        {
          id: "turn",
          items: [{ id: "item", type: "agentMessage", text: original }],
        },
      ],
    },
    nextCursor: null,
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string, init?: RequestInit) => {
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ path, body });
      const url = new URL(path, "http://localhost");
      if (body && path.endsWith("/annotations")) {
        if (rejectSave)
          return new Response(
            JSON.stringify({ error: "保存失败，请检查连接" }),
            { status: 503 },
          );
        const saved = { ...note(body.target), text: body.text };
        current = { ...current, annotations: [...current.annotations, saved] };
        return new Response(JSON.stringify(saved));
      }
      if (url.searchParams.get("kind") === "history")
        return new Response(
          JSON.stringify(history(url.searchParams.get("cursor"))),
        );
      if (url.searchParams.get("kind") === "file")
        return new Response(JSON.stringify(file));
      return new Response(JSON.stringify(current));
    }),
  );
});
afterEach(() => {
  window.getSelection()?.removeAllRanges();
  vi.unstubAllGlobals();
});

function select(
  start: Node,
  startOffset: number,
  end: Node,
  endOffset: number,
  target: HTMLElement,
) {
  const range = document.createRange();
  range.setStart(start, startOffset);
  range.setEnd(end, endOffset);
  window.getSelection()?.removeAllRanges();
  window.getSelection()?.addRange(range);
  fireEvent.mouseUp(target);
}

describe("原处批注", () => {
  it("选中重复片段时保留准确消息与 UTF-16 范围，保存不发送模型输入", async () => {
    const user = userEvent.setup();
    render(<Detail id="annotations" />);
    const message = await screen.findByText(original);
    const start = original.lastIndexOf("中文");
    select(
      message.firstChild!,
      start,
      message.firstChild!,
      start + "中文🙂".length,
      message,
    );
    await user.click(screen.getByRole("button", { name: "批注所选内容" }));
    expect(screen.getByRole("dialog")).toHaveTextContent("中文🙂");
    await user.type(screen.getByLabelText("你的意见"), "请解释第二处");
    fireEvent.keyDown(screen.getByLabelText("你的意见"), {
      key: "Enter",
      metaKey: true,
    });
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    const writes = calls.filter((call) => call.body);
    expect(writes).toHaveLength(1);
    expect(writes[0]!.path).toMatch(/\/annotations$/);
    expect(writes[0]!.body.target).toEqual({
      kind: "history",
      sessionId: "fork",
      turnId: "turn",
      itemId: "item",
      startOffset: start,
      endOffset: start + 4,
      quote: "中文🙂",
    });
    expect(screen.getByText("已保存，协作者和工具可读取。")).toBeVisible();
  });

  it("连续文件行自动引用，返回旧批注时不高亮已变化的同号行", async () => {
    const user = userEvent.setup();
    render(<Detail id="annotations" />);
    await screen.findByText(original);
    await user.click(screen.getByRole("tab", { name: "查看文件" }));
    await user.type(screen.getByLabelText("相对执行目录的文件路径"), file.path);
    await user.click(screen.getByRole("button", { name: "查看" }));
    const second = await screen.findByText("second()");
    const third = screen.getByText("third()");
    select(second.firstChild!, 0, third.firstChild!, 7, third);
    await user.click(screen.getByRole("button", { name: "批注所选内容" }));
    await user.type(screen.getByLabelText("你的意见"), "核对这两行");
    await user.click(screen.getByRole("button", { name: "保存批注" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    expect(calls.find((call) => call.body)?.body.target).toMatchObject({
      kind: "file",
      path: "src/示例.ts",
      startLine: 2,
      endLine: 3,
      quote: "second()\nthird()",
      contentHash: hash,
    });
    file = {
      ...file,
      text: "first()\nreplacement()\nthird()",
      contentHash: "c".repeat(64),
    };
    await user.click(screen.getByRole("button", { name: /查看原位置/ }));
    await screen.findByText(
      "文件或改动已变化，以下保留批注时的原文。请核对当前内容后再处理。",
    );
    expect(document.querySelector("[data-annotation-highlight]")).toBeNull();
    expect(screen.getByText("replacement()")).toBeVisible();
  });

  it("保存失败保留正文和引用，关闭后可继续草稿", async () => {
    const user = userEvent.setup();
    rejectSave = true;
    render(<Detail id="annotations" />);
    await user.click(
      await screen.findByRole("button", { name: "批注这条消息" }),
    );
    await user.type(screen.getByLabelText("你的意见"), "保留的草稿");
    await user.click(screen.getByRole("button", { name: "保存批注" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("保存失败");
    expect(screen.getByLabelText("你的意见")).toHaveValue("保留的草稿");
    await user.click(screen.getByRole("button", { name: "暂存草稿" }));
    await user.click(screen.getByRole("button", { name: "继续草稿" }));
    expect(screen.getByLabelText("你的意见")).toHaveValue("保留的草稿");
    expect(
      within(screen.getByRole("dialog")).getByText(original),
    ).toBeVisible();
  });

  it("历史批注离开最近八轮后按游标查找并高亮原片段", async () => {
    const user = userEvent.setup();
    const oldPage = history(null);
    const cursors: (string | null)[] = [];
    history = (cursor) => {
      cursors.push(cursor);
      return cursor === "older-page"
        ? oldPage
        : {
            thread: {
              turns: [
                {
                  id: "recent",
                  items: [
                    {
                      id: "recent-item",
                      type: "agentMessage",
                      text: "较新的消息",
                    },
                  ],
                },
              ],
            },
            nextCursor: "older-page",
          };
    };
    current.annotations = [
      note({
        kind: "history",
        sessionId: "fork",
        turnId: "turn",
        itemId: "item",
        startOffset: 0,
        endOffset: original.length,
        quote: original,
      }),
    ];
    render(<Detail id="annotations" />);
    await user.click(await screen.findByRole("button", { name: /查看原位置/ }));
    await screen.findByText("已定位到批注原文。");
    expect(cursors).toContain("older-page");
    expect(
      document.querySelector("mark[data-annotation-highlight]"),
    ).toHaveTextContent(original);
  });
});

describe("diff 原位置", () => {
  const diff =
    "diff --git a/src/file.ts b/src/file.ts\n--- a/src/file.ts\n+++ b/src/file.ts\n@@ -10,2 +20,2 @@\n-old()\n+new()\n unchanged()\n@@ -50 +60 @@\n-lastOld()\n+lastNew()\n";
  it("旧行使用旧文件行号，新增行使用新文件行号，不跨一侧或 hunk 引用", () => {
    const lines = diffLines(diff);
    const old = lines.findIndex((line) => line.text === "old()");
    const added = lines.findIndex((line) => line.text === "new()");
    const meta = {
      kind: "changes" as const,
      contentHash: hash,
      baseRevision: base,
    };
    expect(lineTarget(lines, old, old, meta)).toMatchObject({
      path: "src/file.ts",
      side: "old",
      startLine: 10,
      endLine: 10,
      quote: "old()",
    });
    expect(lineTarget(lines, added, added + 1, meta)).toMatchObject({
      side: "new",
      startLine: 20,
      endLine: 21,
      quote: "new()\nunchanged()",
    });
    expect(lineTarget(lines, old, added, meta)).toBeUndefined();
    expect(lineTarget(lines, added, added + 3, meta)).toBeUndefined();
  });
  it("还原 Git 的中文和制表符转义路径", () => {
    const lines = diffLines(
      'diff --git "a/\\344\\270\\255\\346\\226\\207\\t.ts" "b/\\344\\270\\255\\346\\226\\207\\t.ts"\n--- "a/\\344\\270\\255\\346\\226\\207\\t.ts"\n+++ "b/\\344\\270\\255\\346\\226\\207\\t.ts"\n@@ -1 +1 @@\n-old\n+new',
    );
    expect(lines.at(-1)).toMatchObject({
      path: "中文\t.ts",
      number: 1,
      side: "new",
      text: "new",
    });
  });
  it("相同的行号或相同的文字都不能代替版本核对", () => {
    const lines = codeLines("first\nsecond", "file.ts");
    const target = lineTarget(lines, 1, 1, {
      kind: "file",
      contentHash: hash,
    })!;
    expect(codeTargetMatches(target, lines, hash)).toBe(true);
    expect(codeTargetMatches(target, lines, "c".repeat(64))).toBe(false);
    expect(
      codeTargetMatches(
        target,
        codeLines("first\nreplacement", "file.ts"),
        hash,
      ),
    ).toBe(false);
  });
});
