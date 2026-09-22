import { useState } from "react";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { MarkdownText } from "../components/MarkdownText";
import { ReaderMessage } from "../components/Reading";
import { Annotations, type AnnotationRequest } from "../components/Annotations";
import { MaterialReferencePicker } from "../components/MaterialReferences";
import { Materials } from "../components/Materials";
import {
  codeOffsets,
  joinMaterialSegments,
  mappedSelection,
  mergeMaterialSegments,
  textOffsets,
} from "../reading";
import type {
  AnnotationTarget,
  Collaboration,
  Material,
  MaterialPage,
  MaterialReference,
} from "../types";

afterEach(() => {
  window.getSelection()?.removeAllRanges();
  vi.unstubAllGlobals();
});

function select(
  start: Node,
  from: number,
  end = start,
  to = start.textContent!.length,
) {
  const range = document.createRange();
  range.setStart(start, from);
  range.setEnd(end, to);
  const selection = window.getSelection()!;
  selection.removeAllRanges();
  selection.addRange(range);
}

it("maps repeated Chinese, emoji, Markdown wrappers and cross-node selections to exact raw UTF-16 offsets", () => {
  const source = "前 **重复🙂** 与 [链接](https://example.com) 后 **重复🙂**";
  const { container } = render(<MarkdownText source={source} />);
  const strong = container.querySelectorAll("strong span");
  select(strong[1]!.firstChild!, 0);
  expect(mappedSelection(container, source)).toEqual({
    quote: "重复🙂",
    startOffset: source.lastIndexOf("重复🙂"),
    endOffset: source.lastIndexOf("重复🙂") + 4,
  });
  const link = screen.getByRole("link").firstChild!.firstChild!;
  select(strong[0]!.firstChild!, 0, link, 2);
  const quote = "重复🙂** 与 [链接";
  expect(mappedSelection(container, source)).toEqual({
    quote,
    startOffset: 4,
    endOffset: 4 + quote.length,
  });
});

it("maps decoded entities, escapes, CRLF and normalized inline code without guessing", () => {
  expect(textOffsets("&amp;\\*🙂\r\n", "&*🙂\n", 3)).toEqual([
    3, 8, 10, 11, 12, 14,
  ]);
  expect(textOffsets("different", "text", 0)).toBeUndefined();
  expect(codeOffsets("`\r\nfoo\r\n`", "foo", 10, false)).toEqual([
    13, 14, 15, 16,
  ]);
  const source = "实体 &amp; \\* 与 `代码🙂`";
  const { container } = render(<MarkdownText source={source} />);
  const first = container.querySelector("[data-source-start]")!.firstChild!;
  select(first, 3, first, 6);
  expect(mappedSelection(container, source)?.quote).toBe("&amp; \\*");
  const code = container.querySelector("code span")!.firstChild!;
  select(code, 0);
  expect(mappedSelection(container, source)).toEqual({
    quote: "代码🙂",
    startOffset: source.indexOf("代码"),
    endOffset: source.indexOf("代码") + 4,
  });
});

it.each([
  "```ts\nconst x = 1;\n```",
  "  ```ts\n  const x = 1;\n  ```",
  "```ts\r\nconst x = 1;\r\n```",
  "    const x = 1;\n",
])("keeps code offsets while code is highlighted: %s", async (source) => {
  const { container } = render(<MarkdownText source={source} />);
  const code = container.querySelector("code")!;
  await waitFor(() =>
    expect(code.querySelector("[data-source-start]")).not.toBeNull(),
  );
  const nodes = Array.from(code.querySelectorAll("[data-source-start]"));
  const first = nodes[0]!.firstChild!,
    last = nodes.at(-1)!.firstChild!;
  select(first, 0, last, last.textContent!.length);
  const result = mappedSelection(container, source)!;
  expect(result.startOffset).toBe(source.indexOf("const"));
  expect(result.quote.replace(/\r/g, "").trimEnd()).toBe("const x = 1;");
  expect(source.slice(result.startOffset, result.endOffset)).toBe(result.quote);
});

it("renders semantic tables and text without executing HTML or loading remote images", () => {
  const { container } = render(
    <MarkdownText
      source={
        "# 标题\n\n| 指标 | 数值 |\n| --- | --- |\n| P95 | 30 |\n\n![截图](https://example.com/tracking.png)\n\n<script>alert(1)</script>\n\n[危险](javascript:alert(1))"
      }
    />,
  );
  expect(screen.getByRole("heading", { name: "标题" })).toBeVisible();
  expect(screen.getByRole("table")).toHaveTextContent("P95");
  expect(
    container.querySelector("img,script,a[href^='javascript:']"),
  ).toBeNull();
  expect(screen.getByText(/未加载外部内容/)).toBeVisible();
});

it("keeps the highlighted code DOM and its selection stable when discussion state refreshes", async () => {
  const source = "```go\nfmt.Println(42)\n```";
  const { container, rerender } = render(<MarkdownText source={source} />);
  await waitFor(() =>
    expect(
      container.querySelector('code [style*="--syntax-dark"]'),
    ).not.toBeNull(),
  );
  const code = container.querySelector("code")!;
  const spans = code.querySelectorAll("[data-source-start]");
  select(
    spans[0]!.firstChild!,
    0,
    spans[spans.length - 1]!.firstChild!,
    spans[spans.length - 1]!.textContent!.length,
  );
  const before = mappedSelection(container, source);
  rerender(<MarkdownText source={source} highlights={[]} />);
  expect(container.querySelector("code")).toBe(code);
  expect(mappedSelection(container, source)).toEqual(before);
  expect(before?.quote.trim()).toBe("fmt.Println(42)");
});

it("joins only contiguous segments of the same message without mutating the API page", () => {
  const segments: MaterialPage["segments"] = [
    {
      turnId: "t",
      itemId: "i",
      type: "agentMessage",
      text: "```ts\n",
      startOffset: 0,
      endOffset: 6,
      length: 11,
      collapsed: true,
      notice: "已折叠，共 11 字",
    },
    {
      turnId: "t",
      itemId: "i",
      type: "agentMessage",
      text: "1\n```",
      startOffset: 6,
      endOffset: 11,
      length: 11,
    },
    {
      turnId: "t",
      itemId: "j",
      type: "agentMessage",
      text: "后续",
      startOffset: 0,
      endOffset: 2,
      length: 2,
    },
  ];
  expect(joinMaterialSegments(segments)).toHaveLength(2);
  expect(joinMaterialSegments(segments)[0]).toMatchObject({
    text: "```ts\n1\n```",
    startOffset: 0,
    endOffset: 11,
    collapsed: false,
  });
  expect(joinMaterialSegments(segments)[0]?.notice).toBeUndefined();
  expect(segments[0]!.text).toBe("```ts\n");
});

it("clears the folded preview notice while retaining incomplete item pagination", () => {
  const preview: MaterialPage["segments"] = [
    {
      turnId: "t",
      itemId: "tool",
      type: "toolResult",
      text: "head",
      startOffset: 0,
      endOffset: 4,
      length: 20000,
      collapsed: true,
      notice: "已折叠，共 20000 字",
      readHint: "按条读取",
    },
  ];
  const merged = mergeMaterialSegments(preview, [
    {
      turnId: "t",
      itemId: "tool",
      type: "toolResult",
      text: " body",
      startOffset: 4,
      endOffset: 16000,
      length: 20000,
    },
  ]);
  expect(merged).toEqual([
    expect.objectContaining({
      text: "head body",
      endOffset: 16000,
      length: 20000,
      collapsed: true,
    }),
  ]);
  expect(merged[0]?.notice).toBeUndefined();
  expect(merged[0]?.readHint).toBeUndefined();
  expect(preview[0]?.notice).toBe("已折叠，共 20000 字");
});

function NotesHarness() {
  const [request, setRequest] = useState<AnnotationRequest>();
  const target: AnnotationTarget = {
    kind: "history",
    sessionId: "s",
    turnId: "t",
    itemId: "i",
    quote: "前 **批注🙂** 后",
  };
  return (
    <>
      <ReaderMessage
        label="助手回复"
        source={target.quote}
        target={target}
        onAnnotate={(target, origin) =>
          setRequest({ target, origin, serial: Date.now() })
        }
      />
      <Annotations
        id="space"
        annotations={[]}
        request={request}
        disabled={false}
        onSaved={() => {}}
        onLocate={() => {}}
      />
    </>
  );
}

it("places the narrow-screen editor directly after the selected paragraph and retains its draft when closed", async () => {
  vi.stubGlobal(
    "matchMedia",
    vi.fn(() => ({
      matches: true,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  );
  const user = userEvent.setup();
  const { container } = render(<NotesHarness />);
  const message = container.querySelector<HTMLElement>("[data-message-text]")!;
  select(message.querySelector("strong span")!.firstChild!, 0);
  fireEvent.mouseUp(message);
  await user.click(screen.getByRole("button", { name: "批注所选内容" }));
  const card = screen.getByRole("region", { name: "原文批注" });
  expect(card).toHaveClass("annotation-inline");
  expect(card.parentElement?.previousElementSibling?.tagName).toBe("P");
  expect(within(card).getByLabelText("你的意见")).toHaveFocus();
  await user.type(within(card).getByLabelText("你的意见"), "窄屏草稿");
  expect(
    screen.queryByRole("button", { name: "批注所选内容" }),
  ).not.toBeInTheDocument();
  await user.keyboard("{Escape}");
  await user.click(screen.getByRole("button", { name: /继续草稿/ }));
  expect(screen.getByLabelText("你的意见")).toHaveValue("窄屏草稿");
});

it("keeps an anchored draft on Escape and save failure, then saves the same exact source range", async () => {
  const user = userEvent.setup(),
    calls: Record<string, unknown>[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_path, init) => {
      calls.push(JSON.parse(init.body));
      return new Response(
        JSON.stringify(
          calls.length === 1
            ? { error: "暂时失败" }
            : { id: "a", text: "保留草稿" },
        ),
        { status: calls.length === 1 ? 503 : 200 },
      );
    }),
  );
  const { container } = render(<NotesHarness />);
  const message = container.querySelector<HTMLElement>("[data-message-text]")!;
  select(message.querySelector("strong span")!.firstChild!, 0);
  fireEvent.mouseUp(message);
  await user.click(screen.getByRole("button", { name: "批注所选内容" }));
  const card = screen.getByRole("region", { name: "原文批注" });
  expect(within(card).getByLabelText("你的意见")).toHaveFocus();
  await user.type(within(card).getByLabelText("你的意见"), "保留草稿");
  await user.keyboard("{Escape}");
  expect(
    screen.queryByRole("region", { name: "原文批注" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: /继续草稿/ }));
  expect(screen.getByLabelText("你的意见")).toHaveValue("保留草稿");
  await user.click(screen.getByRole("button", { name: "保存批注" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("暂时失败");
  expect(screen.getByLabelText("你的意见")).toHaveValue("保留草稿");
  await user.click(screen.getByRole("button", { name: "保存批注" }));
  await waitFor(() => expect(calls).toHaveLength(2));
  expect(calls[1]).toEqual(calls[0]);
  expect(calls[0]).toMatchObject({
    text: "保留草稿",
    target: { quote: "批注🙂", startOffset: 4, endOffset: 8, sessionId: "s" },
  });
});

const material: Material = {
  id: "m",
  author: "林",
  authorId: "owner",
  versions: [1, 2].map((version) => ({
    version,
    title: "连接池分析",
    provider: "codex",
    sourceId: "s",
    startTurnId: "t",
    endTurnId: "t",
    turnCount: 1,
    hash: "h",
    createdAt: "2026-09-18T00:00:00Z",
    noticeCount: 0,
  })),
};
function ReferencesHarness() {
  const [value, setValue] = useState<MaterialReference[]>([]);
  return (
    <MaterialReferencePicker
      materials={[material]}
      spaceId="space"
      value={value}
      onChange={setValue}
      disabled={false}
    />
  );
}

it("searches references, defaults to the latest version, previews only on request and allows a pinned historical version", async () => {
  const user = userEvent.setup(),
    fetcher = vi.fn(
      async () =>
        new Response(JSON.stringify({ segments: [{ text: "预览正文" }] })),
    );
  vi.stubGlobal("fetch", fetcher);
  render(<ReferencesHarness />);
  expect(screen.queryByRole("searchbox")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "引用材料" }));
  expect(screen.getByRole("searchbox")).toHaveFocus();
  expect(screen.getByRole("button", { name: /连接池分析 · v2/ })).toBeVisible();
  expect(
    screen.getByRole("button", { name: /连接池分析 · v1/ }),
  ).not.toBeVisible();
  await user.type(screen.getByRole("searchbox"), "没有");
  expect(screen.getByText("没有找到匹配的材料。")).toBeVisible();
  await user.clear(screen.getByRole("searchbox"));
  await user.type(screen.getByRole("searchbox"), "林");
  expect(fetcher).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "预览 连接池分析 v2" }));
  expect(await screen.findByText("预览正文")).toBeVisible();
  expect(fetcher).toHaveBeenCalledTimes(1);
  await user.click(screen.getByText("选择历史版本（1）"));
  await user.click(screen.getByRole("button", { name: /连接池分析 · v1/ }));
  expect(
    screen.getByRole("button", { name: "移除引用 连接池分析 v1" }),
  ).toBeVisible();
  expect(screen.queryByRole("searchbox")).not.toBeInTheDocument();
});

it("keeps a paginated code block intact and reopens the same material from its in-page reading cache", async () => {
  const user = userEvent.setup();
  const source = '```go\nfmt.Println("保留分页")\n```';
  const mid = source.indexOf("Print");
  const fetcher = vi.fn(async (_url, init) => {
    const next = JSON.parse(init.body).cursor;
    return new Response(
      JSON.stringify({
        materialId: "m",
        version: 2,
        turns: [{ id: "t", label: "分析" }],
        segments: [
          {
            turnId: "t",
            itemId: "i",
            type: "agentMessage",
            text: next ? source.slice(mid) : source.slice(0, mid),
            startOffset: next ? mid : 0,
            endOffset: next ? source.length : mid,
          },
        ],
        nextCursor: next ? undefined : "next",
      }),
    );
  });
  vi.stubGlobal("fetch", fetcher);
  vi.stubGlobal("scrollBy", vi.fn());
  const c = {
    id: "space",
    role: "owner",
    state: "ready",
    materials: [material],
    annotations: [],
  } as unknown as Collaboration;
  const { container } = render(
    <Materials collaboration={c} reload={() => {}} onAnnotate={() => {}} />,
  );
  expect(fetcher).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "阅读材料" }));
  await user.click(await screen.findByRole("button", { name: "继续读取正文" }));
  await waitFor(() =>
    expect(container.querySelector(".reader-code code")).toHaveTextContent(
      'fmt.Println("保留分页")',
    ),
  );
  expect(container.querySelectorAll(".reader-message")).toHaveLength(1);
  await user.click(screen.getByRole("button", { name: "收起材料" }));
  await user.click(screen.getByRole("button", { name: "阅读材料" }));
  await waitFor(() =>
    expect(container.querySelector(".reader-code code")).toHaveTextContent(
      "保留分页",
    ),
  );
  expect(fetcher).toHaveBeenCalledTimes(2);
});

it("places tool expansion inside the fold summary and keeps the process open", async () => {
  const user = userEvent.setup();
  const onExpand = vi.fn();
  render(
    <ReaderMessage
      label="命令与结果 · 共 10 字 · 已显示 5 字"
      source="HEAD--"
      target={{
        kind: "history",
        sessionId: "s",
        turnId: "t",
        itemId: "tool",
        quote: "HEAD--",
      }}
      onAnnotate={() => {}}
      onExpand={onExpand}
      shownLength={5}
    />,
  );
  const details = document.querySelector("details.reader-tool");
  const summary = details?.querySelector("summary");
  expect(details).toBeTruthy();
  expect((details as HTMLDetailsElement).open).toBe(false);
  expect(
    within(summary!).getByRole("button", { name: "读取完整输出" }),
  ).toBeVisible();
  await user.click(screen.getByRole("button", { name: "读取完整输出" }));
  expect(onExpand).toHaveBeenCalledTimes(1);
  expect((details as HTMLDetailsElement).open).toBe(true);
  await user.click(screen.getByRole("button", { name: "读取完整输出" }));
  expect(onExpand).toHaveBeenCalledTimes(2);
  expect((details as HTMLDetailsElement).open).toBe(true);
});
