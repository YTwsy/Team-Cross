import { act, render, screen, within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { ReadOnlySpace } from "../components/ReadOnlySpace";
import { sameReference } from "../library";
import type { Collaboration } from "../types";

const child = (id: string): Collaboration => ({
  id,
  spaceId: "space",
  executionId: id,
  title: `会话 ${id}`,
  role: "owner",
  selfId: "owner",
  hasExecution: true,

  provider: "codex",
  state: "ready",
  online: true,
  writer: "owner",
  epoch: 1,
  sequence: 0,
  approvals: 0,
  annotations: [],
  materials: [],
  sessionId: `native-${id}`,
  sourceId: "source",
  busy: false,
  executionCwd: `/fixture/${id}`,
  workspaceMode: "worktree",
  repo: "/fixture",
  head: "fixture-head",
  branch: id,
  host: "Fixture Mac",
  createdAt: "2026-10-21T00:00:00Z",
  updatedAt: "2026-10-21T00:00:00Z",
  sharing: false,
  connected: false,
});
const base: Collaboration = {
  id: "space",
  title: "多会话空间",
  role: "owner",
  selfId: "owner",
  state: "ready",
  hasExecution: false,
  annotations: [],
  materials: [],
  members: [],
  sessions: [child("one"), child("two")],
} as unknown as Collaboration;
let calls: { path: string; body: any }[];
let save: (body: any) => Promise<Response>;
const json = (value: unknown) => new Response(JSON.stringify(value));
beforeEach(() => {
  sessionStorage.clear();
  localStorage.clear();
  calls = [];
  save = async (body) =>
    json({ ...body, id: "saved", createdAt: new Date().toISOString() });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string, init?: RequestInit) => {
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ path: input, body });
      if (input.includes("space-receivers") || input.endsWith("agent-pairings"))
        return json([]);
      if (input.endsWith("/annotations") && body) return save(body);
      if (input.includes("/context?"))
        return json({
          thread: {},
          turns: [],
          segments: [],
          contentHash: input,
        });
      if (input.includes("/workbench/view"))
        return json({
          targets: [],
          requests: [],
          total: 0,
          brief: { revision: 0, topic: "", decisions: [], questions: [] },
          assistant: { epoch: 0, state: "disabled" },
        });
      return json(base);
    }),
  );
});

it("keeps each session's comment and file draft while the URL selects an exact session", async () => {
  const user = userEvent.setup();
  const props = {
    collaboration: structuredClone(base),
    update: vi.fn(),
    reload: vi.fn(),
  };
  const view = render(<ReadOnlySpace {...props} executionId="one" />);
  await user.click(screen.getByRole("button", { name: /^评论/ }));
  await user.type(
    screen.getByRole("textbox", { name: "你的意见" }),
    "只属于 one 的草稿",
  );
  await user.click(screen.getByRole("tab", { name: "查看文件" }));
  await user.type(
    screen.getByRole("textbox", { name: "相对执行目录的文件路径" }),
    "first.go",
  );
  await user.click(
    within(screen.getByRole("navigation", { name: "协作会话" })).getByRole(
      "button",
      { name: /会话 two/ },
    ),
  );
  expect(location.hash).toBe("#/collaborations/space/sessions/two");
  view.rerender(<ReadOnlySpace {...props} executionId="two" />);
  expect(screen.getByRole("textbox", { name: "你的意见" })).toHaveValue("");
  await user.type(
    screen.getByRole("textbox", { name: "你的意见" }),
    "只属于 two 的草稿",
  );
  view.rerender(<ReadOnlySpace {...props} executionId="one" />);
  expect(screen.getByRole("textbox", { name: "你的意见" })).toHaveValue(
    "只属于 one 的草稿",
  );
  expect(
    screen.getByRole("textbox", { name: "相对执行目录的文件路径" }),
  ).toHaveValue("first.go");
  expect(calls.filter((c) => c.body)).toHaveLength(0);
});

it("a late save stays bound to its original session and leaving clears cached content", async () => {
  const user = userEvent.setup();
  let resolve!: (value: Response) => void;
  save = () =>
    new Promise((done) => {
      resolve = done;
    });
  const space = structuredClone(base);
  const props = { collaboration: space, update: vi.fn(), reload: vi.fn() };
  const view = render(<ReadOnlySpace {...props} executionId="one" />);
  await user.click(screen.getByRole("button", { name: /^评论/ }));
  await user.type(
    screen.getByRole("textbox", { name: "你的意见" }),
    "保存到 one",
  );
  await user.click(screen.getByRole("button", { name: "保存批注" }));
  view.rerender(<ReadOnlySpace {...props} executionId="two" />);
  await user.type(
    screen.getByRole("textbox", { name: "你的意见" }),
    "two 的草稿不清除",
  );
  resolve(json({ id: "late", text: "保存到 one", executionId: "one" }));
  await waitFor(() => expect(props.reload).toHaveBeenCalled());
  expect(calls.find((c) => c.body?.text === "保存到 one")?.path).toBe(
    "/api/collaborations/space/sessions/one/annotations",
  );
  expect(screen.getByRole("textbox", { name: "你的意见" })).toHaveValue(
    "two 的草稿不清除",
  );
  space.role = "remote";
  space.selfId = "member";
  space.state = "ended";
  view.rerender(<ReadOnlySpace {...props} executionId="one" />);
  expect(screen.getByText("这次共享已结束")).toBeVisible();
  expect(
    screen.queryByRole("textbox", { name: "你的意见" }),
  ).not.toBeInTheDocument();
});

it("never treats identical excerpts in sibling sessions as the same resource", () => {
  const reference = {
    spaceId: "space",
    executionId: "one",
    kind: "context" as const,
  };
  expect(sameReference(reference, { ...reference, executionId: "two" })).toBe(
    false,
  );
});

it("shows actionable session status in the overview without opening management", async () => {
  const user = userEvent.setup();
  const space = structuredClone(base);
  space.sessions![1]!.approvals = 1;
  render(
    <ReadOnlySpace
      collaboration={space}
      update={vi.fn()}
      reload={vi.fn()}
      executionId="one"
    />,
  );
  await user.click(screen.getByRole("tab", { name: "空间概览" }));
  const overview = screen.getByRole("tabpanel", { name: "空间概览" });
  expect(within(overview).getByText("1 个需要处理")).toBeVisible();
  await user.click(
    within(overview).getByRole("button", { name: /会话 two.*等待回应/ }),
  );
  expect(location.hash).toBe("#/collaborations/space/sessions/two");
  expect(screen.getByRole("tabpanel", { name: "阅读与讨论" })).toBeVisible();
});

it("opens the right-hand comments on wide screens and keeps manual choices across sessions and sizes", async () => {
  const user = userEvent.setup();
  const listeners = new Set<() => void>();
  let wide = true;
  vi.stubGlobal("matchMedia", (query: string) => ({
    get matches() {
      return query === "(min-width: 1101px)" && wide;
    },
    media: query,
    addEventListener: (_: string, listener: () => void) =>
      listeners.add(listener),
    removeEventListener: (_: string, listener: () => void) =>
      listeners.delete(listener),
  }));
  const props = {
    collaboration: structuredClone(base),
    update: vi.fn(),
    reload: vi.fn(),
  };
  const view = render(<ReadOnlySpace {...props} executionId="one" />);
  expect(screen.getByRole("complementary", { name: "会话评论" })).toBeVisible();
  await user.type(
    screen.getByRole("textbox", { name: "你的意见" }),
    "宽屏草稿",
  );
  await user.click(
    within(screen.getByRole("complementary", { name: "会话评论" })).getByRole(
      "button",
      { name: "收起评论" },
    ),
  );
  view.rerender(<ReadOnlySpace {...props} executionId="two" />);
  expect(
    screen.queryByRole("complementary", { name: "会话评论" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: /^评论/ }));
  view.rerender(<ReadOnlySpace {...props} executionId="one" />);
  expect(screen.getByRole("textbox", { name: "你的意见" })).toHaveValue(
    "宽屏草稿",
  );
  await act(async () => {
    wide = false;
    listeners.forEach((listener) => listener());
  });
  expect(
    screen.queryByRole("complementary", { name: "会话评论" }),
  ).not.toBeInTheDocument();
  await act(async () => {
    wide = true;
    listeners.forEach((listener) => listener());
  });
  expect(screen.getByRole("textbox", { name: "你的意见" })).toHaveValue(
    "宽屏草稿",
  );
  view.rerender(<ReadOnlySpace {...props} />);
  expect(screen.getByRole("complementary", { name: "空间评论" })).toBeVisible();
  expect(screen.getByRole("textbox", { name: "你的意见" })).toHaveValue("");
});
