import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { Publisher, StartSpace } from "../components/Publisher";
import type {
  Material,
  PublicationDraftPage,
  PublicationDraftSummary,
} from "../types";

const draft: PublicationDraftSummary = {
  id: "draft",
  hash: "frozen",
  title: "重连调查",
  provider: "codex",
  sourceId: "source",
  frozenAt: "2026-09-21T00:00:00Z",
  startTurnId: "t0",
  endTurnId: "t5",
  turnCount: 6,
  noticeCount: 1,
  turns: Array.from({ length: 6 }, (_, i) => ({
    id: `t${i}`,
    label: `提问 ${i + 1}`,
    status: "completed",
    itemCount: 2,
    noticeCount: i === 1 ? 1 : 0,
  })),
};
const material: Material = {
  id: "material",
  author: "A",
  authorId: "owner",
  versions: [
    {
      version: 1,
      title: "重连调查",
      provider: "codex",
      sourceId: "source",
      startTurnId: "t1",
      endTurnId: "t3",
      turnCount: 3,
      hash: "previous",
      createdAt: draft.frozenAt,
      noticeCount: 1,
    },
  ],
};
const json = (data: unknown, status = 200) =>
  new Response(JSON.stringify(data), {
    status,
    headers: { "Content-Type": "application/json" },
  });
type Body = Record<string, any>;
type Call = { path: string; body: Body; signal?: AbortSignal | null };
function fixture(
  override?: (
    call: Call,
  ) => Promise<Response | undefined> | Response | undefined,
  source = draft,
) {
  const calls: Call[] = [],
    drafts = new Map([[source.id, source]]);
  let previews = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const call = {
        path: String(input),
        body: init?.body ? JSON.parse(String(init.body)) : {},
        signal: init?.signal,
      };
      calls.push(call);
      const custom = await override?.(call);
      if (custom) return custom;
      const { path, body } = call;
      if (path.startsWith("/api/sources"))
        return json({
          data: [
            {
              id: "source",
              name: "重连调查",
              preview: "调查内容",
              cwd: "/project",
              updatedAt: 1789948800,
            },
          ],
        });
      if (path === "/api/publications/source") return json(source);
      if (path === "/api/publications/preview") {
        const from = source.turns.findIndex((t) => t.id === body.startTurnId),
          end = source.turns.findIndex((t) => t.id === body.endTurnId);
        const selected = source.turns.slice(from, end + 1),
          id = `preview-${++previews}`;
        const preview = {
          ...source,
          id,
          hash: id,
          title: body.title.trim(),
          startTurnId: body.startTurnId,
          endTurnId: body.endTurnId,
          readingStartId: body.readingStartId,
          turns: selected,
          turnCount: selected.length,
        };
        drafts.set(id, preview);
        return json(preview);
      }
      if (path === "/api/publications/read-draft") {
        const d = drafts.get(body.draftId)!;
        const from = body.cursor
          ? Number(body.cursor)
          : Math.max(
              0,
              d.turns.findIndex((t) => t.id === body.turnId),
            );
        const selected = d.turns.slice(from, from + 2);
        const page: PublicationDraftPage = {
          draftId: d.id,
          hash: d.hash,
          scope: body.itemId ? "item" : "stream",
          pageEndsAtTurnBoundary: true,
          nextCursor: from + 2 < d.turns.length ? String(from + 2) : "",
          segments: selected.flatMap((t) => [
            {
              turnId: t.id,
              itemId: `u-${t.id}`,
              type: "userMessage",
              text: `正文 ${t.id}`,
              startOffset: 0,
              endOffset: 5,
              length: 5,
            },
            {
              turnId: t.id,
              itemId: `a-${t.id}`,
              type: "agentMessage",
              text: `**答复 ${t.id}**`,
              startOffset: 0,
              endOffset: 9,
              length: 9,
              notice: t.noticeCount ? "附件未导出" : undefined,
            },
          ]),
        };
        return json(page);
      }
      if (path === "/api/spaces") return json({ id: "space" });
      if (path.endsWith("/publication-status"))
        return json({ state: "not_found" });
      if (path.endsWith("/materials"))
        return json({ materialId: "new", version: 1, state: "published" });
      return json({});
    }),
  );
  return { calls, drafts };
}
async function openPublisher() {
  const user = userEvent.setup();
  await user.click(await screen.findByRole("radio", { name: /重连调查/ }));
  await user.click(screen.getByRole("button", { name: "选择公开范围" }));
  await screen.findByText("正文 t0");
  return user;
}
function turn(number: number) {
  return within(screen.getByRole("region", { name: `第 ${number} 轮` }));
}
function directory() {
  return within(screen.getByRole("navigation", { name: "会话目录" }));
}
beforeEach(() => {
  vi.restoreAllMocks();
  localStorage.clear();
  sessionStorage.clear();
  HTMLElement.prototype.scrollIntoView = vi.fn();
});

it("keeps scope, the active reader tools, export notices and a single next action in the shared controls", async () => {
  fixture();
  render(<Publisher onPublished={() => {}} />);
  const user = await openPublisher();
  const controls = within(
    screen.getByRole("region", { name: "分享范围与操作" }),
  );
  expect(controls.getByText("已选第 1–6 轮，共 6 轮")).toBeVisible();
  expect(controls.getByRole("button", { name: "预览分享内容" })).toBeEnabled();
  expect(screen.getAllByRole("button", { name: "预览分享内容" })).toHaveLength(
    1,
  );
  expect(screen.queryByText(/将分享第/)).not.toBeInTheDocument();
  expect(
    controls.getByText("点击目录查看正文，在每轮开头选择范围"),
  ).toBeVisible();
  const notice = controls.getByText("1 处导出说明");
  const explanation = controls.getByText(/这里按说明条目计数/);
  expect(explanation).not.toBeVisible();
  await user.click(notice);
  expect(explanation).toBeVisible();
  await user.click(controls.getByRole("button", { name: "专注阅读" }));
  expect(
    controls.getByRole("button", { name: "退出专注阅读" }),
  ).toHaveAttribute("aria-pressed", "true");
  await user.click(controls.getByRole("button", { name: "预览分享内容" }));
  await waitFor(() =>
    expect(
      controls.getByRole("button", { name: "创建只读空间并发布" }),
    ).toBeEnabled(),
  );
  expect(
    controls.queryByText("点击目录查看正文，在每轮开头选择范围"),
  ).not.toBeInTheDocument();
  expect(
    controls.getByText("同事只能读取下方范围，折叠的工具过程也会分享"),
  ).toBeVisible();
  expect(screen.getAllByRole("button", { name: "专注阅读" })).toHaveLength(1);
  expect(
    screen.queryByRole("button", { name: "退出专注阅读" }),
  ).not.toBeInTheDocument();
  await user.click(controls.getByRole("button", { name: "返回调整范围" }));
  expect(
    controls.getByRole("button", { name: "退出专注阅读" }),
  ).toHaveAttribute("aria-pressed", "true");
  await user.click(turn(1).getByRole("button", { name: "到这轮结束" }));
  expect(controls.queryByText("1 处导出说明")).not.toBeInTheDocument();
  await user.click(controls.getByRole("button", { name: "全部已结束对话" }));
  expect(controls.getByText("1 处导出说明")).toBeVisible();
  expect(controls.getByText(/这里按说明条目计数/)).not.toBeVisible();
});

it("reads compact private drafts, navigates independently of scope, and renders Markdown without annotation actions", async () => {
  const { calls } = fixture();
  render(<Publisher onPublished={() => {}} />);
  const user = await openPublisher();
  expect(calls.find((c) => c.path.endsWith("/source"))?.body).toMatchObject({
    compact: true,
    sourceId: "source",
  });
  expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  expect(screen.queryByLabelText("分享标题")).not.toBeInTheDocument();
  expect(screen.getByText("答复 t0").closest("strong")).not.toBeNull();
  expect(
    screen.queryByRole("button", { name: "批注这条消息" }),
  ).not.toBeInTheDocument();
  await user.click(directory().getByRole("button", { name: /提问 5/ }));
  await screen.findByText("正文 t4");
  expect(screen.getByText("已选第 1–6 轮，共 6 轮")).toBeVisible();
  expect(calls.at(-1)?.body).toEqual({ draftId: "draft", turnId: "t4" });
  expect(directory().getByRole("button", { name: /提问 5/ })).toHaveAttribute(
    "aria-current",
    "true",
  );
  await user.click(turn(5).getByRole("button", { name: "从这轮开始" }));
  expect(screen.getByText("已选第 5–6 轮，共 2 轮")).toBeVisible();
  await user.click(turn(5).getByRole("button", { name: "到这轮结束" }));
  expect(screen.getByText("已选第 5–5 轮，共 1 轮")).toBeVisible();
});

it("keeps scope separate from the reading recommendation and resets only a recommendation outside new boundaries", async () => {
  fixture();
  render(<Publisher onPublished={() => {}} />);
  const user = await openPublisher();
  await user.click(turn(2).getByRole("button", { name: "建议同事从这轮读起" }));
  expect(screen.getByText("已选第 1–6 轮，共 6 轮")).toBeVisible();
  expect(screen.getByText("建议从第 2 轮读起 · 分享范围不变")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "最近 3 轮" }));
  expect(screen.getByText("已选第 4–6 轮，共 3 轮")).toBeVisible();
  expect(screen.getByText(/原建议阅读起点已移出范围/)).toBeVisible();
  await user.click(turn(1).getByRole("button", { name: "到这轮结束" }));
  expect(screen.getByText("已选第 1–1 轮，共 1 轮")).toBeVisible();
});

it("confirms only the server-scoped preview, starts at its first turn, and retains the editor when returning", async () => {
  const { calls } = fixture();
  render(<Publisher onPublished={() => {}} />);
  const user = await openPublisher();
  await user.click(turn(2).getByRole("button", { name: "到这轮结束" }));
  await user.click(turn(2).getByRole("button", { name: "建议同事从这轮读起" }));
  const original = screen.getByText("正文 t0");
  await user.click(screen.getByRole("button", { name: "预览分享内容" }));
  await screen.findByRole("button", { name: "创建只读空间并发布" });
  await waitFor(() =>
    expect(
      calls.filter((c) => c.path.endsWith("/read-draft")).at(-1)?.body,
    ).toEqual({ draftId: "preview-1", turnId: "t0" }),
  );
  expect(
    screen.getByRole("navigation", { name: "分享内容目录" }),
  ).not.toHaveTextContent("提问 3");
  expect(screen.getByLabelText("分享标题")).toHaveValue("重连调查");
  await user.click(screen.getByRole("button", { name: "返回调整范围" }));
  expect(screen.getByText("正文 t0")).toBe(original);
  expect(screen.getByText("已选第 1–2 轮，共 2 轮")).toBeVisible();
  expect(calls.filter((c) => c.path.endsWith("/source"))).toHaveLength(1);
});

it("requires a refreshed confirmation after editing the title and keeps request identity after a lost publish response", async () => {
  let attempts = 0;
  const published = vi.fn();
  const { calls } = fixture(({ path }) => {
    if (path.endsWith("/materials") && ++attempts === 1)
      throw new Error("response lost");
    return undefined;
  });
  render(<Publisher spaceId="space" onPublished={published} />);
  const user = await openPublisher();
  await user.click(turn(2).getByRole("button", { name: "从这轮开始" }));
  await user.click(turn(2).getByRole("button", { name: "到这轮结束" }));
  await user.click(screen.getByRole("button", { name: "预览分享内容" }));
  await screen.findByRole("button", { name: "发布到空间" });
  await user.clear(screen.getByLabelText("分享标题"));
  await user.type(screen.getByLabelText("分享标题"), "确认后的标题");
  expect(
    screen.queryByRole("button", { name: "发布到空间" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "更新分享预览" }));
  await user.click(await screen.findByRole("button", { name: "发布到空间" }));
  await user.click(await screen.findByRole("button", { name: "查询发布结果" }));
  await screen.findByText(/尚未查到已保存结果/);
  expect(screen.getByRole("button", { name: "返回调整范围" })).toBeDisabled();
  expect(screen.getByLabelText("分享标题")).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "发布到空间" }));
  await waitFor(() => expect(published).toHaveBeenCalled());
  const writes = calls.filter((c) => c.path.endsWith("/materials"));
  expect(writes[0]?.body).toEqual(writes[1]?.body);
  expect(writes[0]?.body).toMatchObject({
    previewId: "preview-2",
    previewHash: "preview-2",
  });
  expect(calls.find((c) => c.path.endsWith("/preview"))?.body).toMatchObject({
    startTurnId: "t1",
    endTurnId: "t1",
    compact: true,
  });
});

it("keeps the previous publication boundaries when preparing a new version", async () => {
  fixture();
  render(
    <Publisher spaceId="space" material={material} onPublished={() => {}} />,
  );
  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "选择公开范围" }));
  expect(await screen.findByText("已选第 2–4 轮，共 3 轮")).toBeVisible();
});

it("requires explicit reselection when the previous boundary is missing", async () => {
  fixture(undefined, {
    ...draft,
    turns: draft.turns.filter((t) => t.id !== "t1"),
  });
  render(
    <Publisher spaceId="space" material={material} onPublished={() => {}} />,
  );
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "选择公开范围" }));
  expect(await screen.findByText(/不会自动扩大公开范围/)).toBeVisible();
  expect(screen.getByRole("button", { name: "预览分享内容" })).toBeDisabled();
  await screen.findByText("正文 t0");
  await user.click(turn(1).getByRole("button", { name: "从这轮开始" }));
  expect(screen.getByRole("button", { name: "预览分享内容" })).toBeEnabled();
});

it("ignores delayed navigation responses and retries the requested turn without changing selection", async () => {
  let resolve!: (r: Response) => void;
  let fail = true;
  const { calls } = fixture(({ path, body }) => {
    if (!path.endsWith("/read-draft")) return;
    if (body.turnId === "t2")
      return new Promise<Response>((r) => {
        resolve = r;
      });
    if (body.turnId === "t4" && fail) {
      fail = false;
      return json({ error: "读取失败" }, 500);
    }
  });
  render(<Publisher onPublished={() => {}} />);
  const user = await openPublisher();
  await user.click(directory().getByRole("button", { name: /提问 3/ }));
  await user.click(directory().getByRole("button", { name: /提问 5/ }));
  await screen.findByText("读取失败");
  await user.click(screen.getByRole("button", { name: "重试" }));
  await screen.findByText("正文 t4");
  resolve(
    json({
      draftId: "draft",
      hash: "frozen",
      segments: [
        {
          turnId: "t2",
          itemId: "late",
          type: "agentMessage",
          text: "过期的正文",
          startOffset: 0,
          endOffset: 5,
          length: 5,
        },
      ],
      nextCursor: "",
    }),
  );
  await waitFor(() =>
    expect(calls.find((c) => c.body.turnId === "t2")?.signal?.aborted).toBe(
      true,
    ),
  );
  expect(screen.queryByText("过期的正文")).not.toBeInTheDocument();
  expect(screen.getByText("已选第 1–6 轮，共 6 轮")).toBeVisible();
});

it("reads tool pages separately and preserves their expansion across range changes and confirmation", async () => {
  const { calls } = fixture(({ path, body }) => {
    if (!path.endsWith("/read-draft") || body.draftId !== "draft") return;
    if (body.cursor)
      return json({
        draftId: "draft",
        hash: "frozen",
        scope: "stream",
        nextCursor: "",
        pageEndsAtTurnBoundary: true,
        segments: [
          {
            turnId: "t1",
            itemId: "next",
            type: "userMessage",
            text: "下一轮",
            startOffset: 0,
            endOffset: 3,
            length: 3,
          },
        ],
      });
    return json({
      draftId: "draft",
      hash: "frozen",
      scope: body.itemId ? "item" : "stream",
      nextCursor: body.itemId ? "item-cursor" : "stream-cursor",
      pageEndsAtTurnBoundary: true,
      segments: [
        {
          turnId: "t0",
          itemId: "tool",
          type: "toolResult",
          text: body.itemId ? "尾部结果" : "工具前缀",
          startOffset: body.itemId ? 4 : 0,
          endOffset: body.itemId ? 8 : 4,
          length: body.itemId ? 8 : 8,
          collapsed: !body.itemId,
        },
        ...(!body.itemId
          ? [
              {
                turnId: "t0",
                itemId: "u",
                type: "userMessage",
                text: "正文 t0",
                startOffset: 0,
                endOffset: 5,
                length: 5,
              },
            ]
          : []),
      ],
    });
  });
  render(<Publisher onPublished={() => {}} />);
  const user = await openPublisher();
  await user.click(screen.getByRole("button", { name: "读取完整输出" }));
  expect(await screen.findByText("工具前缀尾部结果")).toBeVisible();
  const output = screen.getByText("工具前缀尾部结果");
  await user.click(turn(1).getByRole("button", { name: "到这轮结束" }));
  expect(output).toBeVisible();
  await user.click(screen.getByRole("button", { name: "预览分享内容" }));
  await user.click(await screen.findByRole("button", { name: "返回调整范围" }));
  expect(screen.getByText("工具前缀尾部结果")).toBe(output);
  expect(output).toBeVisible();
  await user.click(screen.getByRole("button", { name: "继续读取正文" }));
  expect(calls.at(-1)?.body).toEqual({
    draftId: "draft",
    cursor: "stream-cursor",
  });
  expect(calls.find((c) => c.body.itemId)?.body).toEqual({
    draftId: "draft",
    turnId: "t0",
    itemId: "tool",
    startOffset: 4,
  });
});

it("waits for confirmation body before enabling publication and allows retry after a failed read", async () => {
  let fail = true;
  fixture(({ path, body }) => {
    if (path.endsWith("/read-draft") && body.draftId === "preview-1" && fail) {
      fail = false;
      return json({ error: "确认正文读取失败" }, 500);
    }
  });
  render(<Publisher onPublished={() => {}} />);
  const user = await openPublisher();
  await user.click(screen.getByRole("button", { name: "预览分享内容" }));
  await screen.findByText("确认正文读取失败");
  expect(
    screen.getByRole("button", { name: "创建只读空间并发布" }),
  ).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "重试" }));
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "创建只读空间并发布" }),
    ).toBeEnabled(),
  );
});

it("collapses completed intent choices and restores them when choosing another source", async () => {
  fixture();
  render(<StartSpace />);
  const user = await openPublisher();
  expect(
    screen.queryByRole("radio", { name: /先分享讨论/ }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "重新选择来源" }));
  expect(screen.getByRole("radio", { name: /先分享讨论/ })).toBeChecked();
  expect(screen.getByRole("radio", { name: /重连调查/ })).toBeChecked();
});
