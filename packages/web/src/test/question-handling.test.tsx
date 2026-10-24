import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemberActivity } from "../components/MemberActivity";
import { QuestionHandling } from "../components/QuestionHandling";
import type { QuestionHandling as Handling } from "../waiting";

const source = {
  spaceId: "space",
  annotationId: "discussion",
  replyId: "question",
};
let handling: Handling;
let writes: { path: string; body: any }[];
let failSend: boolean;
let reads: string[];
beforeEach(() => {
  writes = [];
  reads = [];
  failSend = false;
  handling = {
    id: "handling",
    source,
    handler: "shadow",
    revision: 0,
    question: "Which version?",
    state: "active",
    canHandoff: true,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const path = String(url);
      let value: any = {};
      if (init?.body) {
        const body = JSON.parse(String(init.body));
        writes.push({ path, body });
        if (failSend) throw new Error("lost response");
        if (path.endsWith("/attention/handling")) {
          handling = {
            ...handling,
            handler: body.handler,
            revision: handling.revision + 1,
          };
          value = handling;
        }
      } else {
        reads.push(path);
        if (path.includes("/attention/handling?")) value = handling;
        else if (path.includes("/attention?"))
          value = {
            waiting: {
              items: [
                {
                  id: "question",
                  kind: "discussion",
                  direction: "waiting_others",
                  state: "waiting",
                  source,
                  question: "Which version?",
                  tasks: [
                    {
                      shadowId: "shadow",
                      jobId: "job",
                      title: "Prepare release notes",
                      state: "waiting",
                      dependencyId: "d",
                      condition: "Confirm the supported version",
                    },
                  ],
                  totalTasks: 1,
                  updatedAt: "2026-10-24T10:00:00Z",
                },
              ],
              total: 1,
              revision: "v1",
            },
            localShadows: 1,
            route: { shadowId: "shadow", enabled: true, revision: 1 },
          };
        else if (path.includes("/activity?"))
          value = {
            spaceId: "space",
            title: "Release",
            selfId: "owner",
            groups: [],
            unread: 3,
            snapshot: "s1",
          };
        else if (path.includes("/context?"))
          value = {
            annotations: [
              {
                id: "discussion",
                text: "Compatibility",
                author: "Colleague",
                createdAt: "2026-10-24T10:00:00Z",
                replies: [
                  {
                    id: "question",
                    text: "Which version?",
                    author: "Colleague",
                    createdAt: "2026-10-24T10:00:00Z",
                  },
                ],
              },
            ],
          };
        else if (path.includes("follows")) value = [];
      }
      return { ok: true, status: 200, json: async () => value };
    }),
  );
});
afterEach(() => vi.unstubAllGlobals());

it("takes over and hands back the exact reply without a confirmation step", async () => {
  render(<QuestionHandling source={source} />);
  fireEvent.click(await screen.findByRole("button", { name: "这条我来" }));
  await screen.findByRole("button", { name: "交回分身" });
  expect(writes).toHaveLength(1);
  expect(writes[0]!.body).toMatchObject({
    source,
    handler: "owner",
    baseRevision: 0,
  });
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "交回分身" }));
  await screen.findByRole("button", { name: "这条我来" });
  expect(writes[1]!.body).toMatchObject({
    source,
    handler: "shadow",
    baseRevision: 1,
  });
});

it("reuses an uncertain command identity instead of submitting another handoff", async () => {
  failSend = true;
  render(<QuestionHandling source={source} />);
  fireEvent.click(await screen.findByRole("button", { name: "这条我来" }));
  await screen.findByText(/本机服务连接已中断/);
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "这条我来" })).toBeEnabled(),
  );
  failSend = false;
  fireEvent.click(screen.getByRole("button", { name: "这条我来" }));
  await screen.findByRole("button", { name: "交回分身" });
  expect(writes).toHaveLength(2);
  expect(writes[0]!.body.requestId).toBe(writes[1]!.body.requestId);
});

it("shows actual waiting conditions and opens their source without marking notifications read", async () => {
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
  render(<MemberActivity spaceId="space" />);
  fireEvent.click(await screen.findByRole("button", { name: /查看全部动态/ }));
  fireEvent.click(screen.getByRole("button", { name: "我等别人" }));
  await screen.findByText("Prepare release notes");
  expect(screen.getByText("Confirm the supported version")).toBeInTheDocument();
  expect(screen.queryByLabelText("仅未读")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "本空间全部已读" }),
  ).not.toBeInTheDocument();
  expect(
    reads.some(
      (p) => p.includes("/activity?") && p.includes("category=waiting_others"),
    ),
  ).toBe(false);
  fireEvent.click(screen.getByRole("button", { name: "查看原问题" }));
  await screen.findByRole("button", { name: "这条我来" });
  expect(reads.some((p) => p.includes("annotationId=discussion"))).toBe(true);
  expect(writes).toHaveLength(0);
});
