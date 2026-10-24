import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { DefaultShadow } from "../components/DefaultShadow";
import { ActivityProcessing } from "../components/ActivityProcessing";
import { MemberActivity } from "../components/MemberActivity";
import type { Shadow } from "../shadows";
import { endRefreshState } from "../refreshState";

let writes: any[], fail: boolean, route: any;
const shadows = [
  { id: "one", name: "分析分身", owner: { id: "owner" } },
  { id: "two", name: "资料分身", owner: { id: "owner" } },
  { id: "foreign", name: "别人的分身", owner: { id: "peer" } },
] as Shadow[];
beforeEach(() => {
  writes = [];
  fail = false;
  route = {
    route: { shadowId: "one", revision: 4, enabled: true },
    runtime: { shadowId: "one", name: "分析分身", state: "ready" },
    localShadows: 2,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.body) {
        const body = JSON.parse(String(init.body));
        writes.push(body);
        if (fail) throw new Error("lost response");
        route = {
          ...route,
          route: { ...route.route, shadowId: body.shadowId, revision: 5 },
        };
      }
      return { ok: true, json: async () => route };
    }),
  );
});
afterEach(() => {
  cleanup();
  endRefreshState();
  vi.unstubAllGlobals();
});

it("changes the default once without a second confirmation and offers only owned shadows", async () => {
  render(
    <DefaultShadow
      spaceId="space"
      selfId="owner"
      shadows={shadows}
      disabled={false}
    />,
  );
  const select = await screen.findByLabelText("默认分身");
  await waitFor(() => expect(select).not.toBeDisabled());
  expect(
    screen.queryByRole("option", { name: "别人的分身" }),
  ).not.toBeInTheDocument();
  fireEvent.change(select, { target: { value: "two" } });
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    shadowId: "two",
    baseRevision: 4,
    requestId: expect.any(String),
  });
  await waitFor(() => expect(select).toHaveValue("two"));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it("keeps the same route command when its response is lost even after refresh", async () => {
  fail = true;
  render(
    <DefaultShadow
      spaceId="space-retry"
      selfId="owner"
      shadows={shadows}
      disabled={false}
    />,
  );
  const select = await screen.findByLabelText("默认分身");
  await waitFor(() => expect(select).not.toBeDisabled());
  fireEvent.change(select, { target: { value: "two" } });
  const retry = await screen.findByRole("button", { name: "重试设置" });
  expect(select).toBeDisabled();
  route = { ...route, route: { shadowId: "two", revision: 5, enabled: true } };
  fail = false;
  fireEvent.click(retry);
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[1]).toEqual(writes[0]);
  await waitFor(() => expect(select).not.toBeDisabled());
});

it("releases an obsolete pending choice when another route revision has won", async () => {
  fail = true;
  render(
    <DefaultShadow
      spaceId="route-conflict"
      selfId="owner"
      shadows={shadows}
      disabled={false}
    />,
  );
  const select = await screen.findByLabelText("默认分身");
  await waitFor(() => expect(select).not.toBeDisabled());
  route = { ...route, route: { shadowId: "one", revision: 6, enabled: true } };
  fireEvent.change(select, { target: { value: "two" } });
  await screen.findByText("接手设置已变化，请重新选择分身。");
  expect(select).toBeEnabled();
  expect(select).toHaveValue("one");
  expect(
    screen.queryByRole("button", { name: "重试设置" }),
  ).not.toBeInTheDocument();
  expect(writes).toHaveLength(1);
});

it("distinguishes receipts from completion and lets the user inspect the exact source", () => {
  const onOpen = vi.fn();
  const source = {
    spaceId: "space",
    annotationId: "root",
    replyId: "question",
  };
  render(
    <ActivityProcessing
      info={{
        state: "received",
        shadowName: "分析分身",
        totalJobs: 1,
        totalActions: 1,
        jobs: [
          {
            id: "job",
            shadowId: "one",
            title: "核对兼容性",
            state: "needs_followup",
            version: 1,
          },
        ],
        actions: [
          {
            id: "action",
            tool: "send_space_request",
            state: "saved",
            sources: [source],
            updatedAt: "2026-10-24T00:00:00Z",
          },
        ],
      }}
      onOpen={onOpen}
    />,
  );
  expect(screen.getByText("分析分身 · 分身已接收")).toBeInTheDocument();
  expect(screen.queryByText("关联任务已完成")).not.toBeInTheDocument();
  expect(
    screen.getByText("取得发送回执不代表对方已完成工作。"),
  ).toBeInTheDocument();
  fireEvent.click(screen.getByText("查看讨论来源"));
  expect(onOpen).toHaveBeenCalledWith(source);
  expect(writes).toHaveLength(0);
});

it("loads task details only when the processing disclosure is expanded", async () => {
  const reads: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      reads.push(String(url));
      if (init?.body) writes.push(init.body);
      return {
        ok: true,
        json: async () => ({
          state: "waiting_others",
          totalJobs: 1,
          totalActions: 0,
          jobs: [
            {
              id: "j",
              shadowId: "one",
              title: "读取原版本并继续",
              state: "waiting",
              version: 1,
            },
          ],
        }),
      };
    }),
  );
  render(
    <ActivityProcessing
      event={{ spaceId: "space", id: "event-1" }}
      info={{
        state: "waiting_others",
        details: true,
        totalJobs: 1,
        totalActions: 0,
      }}
      onOpen={() => {}}
    />,
  );
  expect(reads).toHaveLength(0);
  fireEvent.click(screen.getByText("等待同事或工作结果"));
  expect(await screen.findByText(/读取原版本并继续/)).toBeInTheDocument();
  expect(reads).toEqual([
    "/api/collaborations/space/attention/event?eventId=event-1",
  ]);
  expect(writes).toHaveLength(0);
});

it("renders separate processing states for events in one group without marking them read", async () => {
  const first = {
    id: "old",
    seq: 1,
    kind: "reply",
    source: { spaceId: "space", annotationId: "root", replyId: "old" },
    subject: { kind: "discussion", id: "root" },
    actor: { name: "同事", kind: "human" },
    createdAt: "2026-10-24T00:00:00Z",
    preview: "旧问题",
    reasons: ["mention"],
    unread: true,
    processing: { state: "completed", totalJobs: 1, totalActions: 0 },
  };
  const next = {
    ...first,
    id: "new",
    seq: 2,
    preview: "新问题",
    source: { ...first.source, replyId: "new" },
    processing: { state: "received", totalJobs: 0, totalActions: 0 },
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.body) writes.push(JSON.parse(String(init.body)));
      return {
        ok: true,
        json: async () => ({
          spaces: [
            {
              spaceId: "space",
              title: "发布空间",
              selfId: "owner",
              groups: [
                {
                  id: "root",
                  latest: next,
                  attention: next,
                  events: [first, next],
                  count: 2,
                  unread: 2,
                },
              ],
              unread: 1,
              snapshot: "snapshot",
            },
          ],
        }),
      };
    }),
  );
  render(<MemberActivity />);
  expect(await screen.findAllByText("分身已接收")).toHaveLength(2);
  fireEvent.click(screen.getByText("2 条动态"));
  expect(screen.getByText("关联任务已完成")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
});

it("opens an action's different discussion without consuming the original unread event", async () => {
  const reads: string[] = [];
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      callback: (
        entries: { isIntersecting: boolean; target: Element }[],
      ) => void;
      constructor(
        callback: (
          entries: { isIntersecting: boolean; target: Element }[],
        ) => void,
      ) {
        this.callback = callback;
      }
      observe(target: Element) {
        this.callback([{ isIntersecting: true, target }]);
      }
      unobserve() {}
      disconnect() {}
    },
  );
  const item = {
    id: "original-unread",
    seq: 1,
    kind: "reply",
    unread: true,
    source: { spaceId: "space", annotationId: "original" },
    subject: { kind: "discussion", id: "original" },
    actor: { kind: "human", name: "同事" },
    createdAt: "2026-10-24T00:00:00Z",
    preview: "旧通知内容",
    reasons: ["mention"],
    processing: {
      state: "processed",
      details: true,
      totalJobs: 0,
      totalActions: 1,
    },
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const path = String(url);
      reads.push(path);
      if (init?.body) writes.push(JSON.parse(String(init.body)));
      let value: any = {};
      if (path.includes("/activity?"))
        value = {
          spaces: [
            {
              spaceId: "space",
              title: "发布",
              selfId: "owner",
              unread: 1,
              groups: [
                {
                  id: "original",
                  latest: item,
                  events: [item],
                  count: 1,
                  unread: 1,
                },
              ],
            },
          ],
        };
      else if (path.includes("/attention/event?"))
        value = {
          state: "processed",
          totalJobs: 0,
          totalActions: 1,
          actions: [
            {
              id: "a",
              tool: "reply_to_annotation",
              state: "saved",
              updatedAt: item.createdAt,
              sources: [
                {
                  spaceId: "space",
                  annotationId: "actual-question",
                  replyId: "question",
                },
              ],
            },
          ],
        };
      else if (path.includes("/context?"))
        value = {
          annotations: [
            {
              id: "actual-question",
              text: "实际讨论",
              author: "同事",
              createdAt: item.createdAt,
              replies: [
                {
                  id: "question",
                  text: "实际提问",
                  author: "分身",
                  createdAt: item.createdAt,
                },
              ],
            },
          ],
        };
      else if (path.includes("/attention/handling?"))
        value = { handler: "shadow", revision: 0, canHandoff: false };
      return { ok: true, json: async () => value };
    }),
  );
  render(<MemberActivity />);
  fireEvent.click(await screen.findByText("分身已处理"));
  fireEvent.click(await screen.findByText("查看讨论来源"));
  await screen.findByText("实际提问");
  expect(reads).toContain(
    "/api/collaborations/space/context?kind=annotations&annotationId=actual-question",
  );
  expect(
    reads.some(
      (path) =>
        path.includes("/follows?") &&
        path.includes("subjectId=actual-question"),
    ),
  ).toBe(true);
  expect(writes).toHaveLength(0);
});
