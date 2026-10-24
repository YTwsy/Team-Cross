import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ShadowQuestions } from "../components/ShadowQuestions";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
const question = {
  id: "q",
  text: "旧客户端支持到哪个版本？",
  delivery: "saved",
  state: "waiting",
  source: {
    spaceId: "space",
    annotationId: "sent-root",
    replyId: "actual-question",
  },
  totalTasks: 2,
  activeTasks: 2,
  relatedQuestions: 1,
  updatedAt: "",
};

it("reads details on demand and keeps each task's condition and next step separate", async () => {
  const reads: string[] = [];
  const writes: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      reads.push(String(url));
      if (init?.method && init.method !== "GET") writes.push(init);
      const body = String(url).includes("/question?")
        ? {
            ...question,
            condition: "确认兼容范围",
            respondsTo: [
              {
                source: {
                  spaceId: "space",
                  annotationId: "original-root",
                  replyId: "original-question",
                },
                question: "原来的兼容问题",
              },
            ],
            totalConditions: 2,
            revision: "one",
            tasks: [
              {
                id: "migration",
                title: "迁移任务",
                version: 1,
                state: "running",
                dependencyId: "d1",
                condition: "确认迁移版本",
                dependencyState: "satisfied",
                active: true,
                nextStep: "调整迁移实现",
              },
              {
                id: "docs",
                title: "文档任务",
                version: 2,
                state: "waiting",
                dependencyId: "d2",
                condition: "确认支持期限",
                dependencyState: "waiting",
                answerReceived: true,
                active: true,
                nextStep: "补齐支持说明",
              },
            ],
          }
        : { items: [question], total: 1, revision: "one" };
      return { ok: true, json: async () => body };
    }),
  );
  render(<ShadowQuestions spaceId="space" shadowId="own-shadow" />);
  fireEvent.click(await screen.findByRole("button", { name: question.text }));
  expect(await screen.findByText("原来的兼容问题")).toBeInTheDocument();
  expect(screen.getByText("确认迁移版本")).toBeInTheDocument();
  expect(screen.getByText("确认支持期限")).toBeInTheDocument();
  expect(screen.getByText("此条件已满足")).toBeInTheDocument();
  expect(screen.getByText("答复已到，待分身核对")).toBeInTheDocument();
  expect(screen.getByText("接下来：调整迁移实现")).toBeInTheDocument();
  expect(screen.getByText("接下来：补齐支持说明")).toBeInTheDocument();
  const detailRead = reads.findIndex((url) => url.includes("/question?"));
  expect(detailRead).toBeGreaterThan(0);
  expect(reads[detailRead]).toContain("shadowId=own-shadow&id=q");
  expect(writes).toEqual([]);
});

it("rebuilds an invalidated page and never labels an unsent parent as the sent question", async () => {
  const reads: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      reads.push(String(url));
      if (String(url).includes("cursor=next"))
        return { ok: false, status: 409, text: async () => "提问记录已变化" };
      if (String(url).includes("/question?"))
        return {
          ok: true,
          json: async () => ({
            ...question,
            source: undefined,
            state: "delivery_unknown",
            delivery: "unknown",
            condition: "需要版本",
            respondsTo: [],
            tasks: [],
            totalConditions: 0,
            revision: "one",
          }),
        };
      return {
        ok: true,
        json: async () => ({
          items: [question],
          total: 2,
          revision: "one",
          nextCursor: "next",
        }),
      };
    }),
  );
  render(<ShadowQuestions spaceId="space" shadowId="own-shadow" />);
  fireEvent.click(await screen.findByRole("button", { name: "下一页提问" }));
  await waitFor(() =>
    expect(
      screen.queryByRole("button", { name: question.text }),
    ).not.toBeInTheDocument(),
  );
  fireEvent.click(screen.getByRole("button", { name: "返回最新" }));
  fireEvent.click(await screen.findByRole("button", { name: question.text }));
  expect(await screen.findByText("发送状态待核对")).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "查看已发问题" }),
  ).not.toBeInTheDocument();
  expect(reads.some((url) => url.includes("activity/read"))).toBe(false);
});
