import { useState } from "react";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { Materials } from "../components/Materials";
import { SpaceWorkbench } from "../components/SpaceWorkbench";
import { SpaceReferencePicker } from "../components/SpaceReferencePicker";
import { Annotations, Discussion } from "../components/Annotations";
import type { Collaboration, Material } from "../types";
import type { LibraryReference } from "../library";

const material = (id: string, title: string): Material => ({
  id,
  author: "Alice",
  authorId: "owner",
  versions: [1, 2].map((version) => ({
    version,
    title,
    provider: "codex",
    sourceId: `source-${id}`,
    startTurnId: "t1",
    endTurnId: "t1",
    turnCount: 1,
    hash: `hash-${id}-${version}`,
    createdAt: "2026-10-20T00:00:00Z",
    noticeCount: 0,
  })),
});
const c = {
  id: "space",
  title: "界面验证",
  role: "owner",
  selfId: "owner",
  state: "ready",
  hasExecution: false,
  materials: [material("one", "重试调查"), material("two", "并发复核")],
  annotations: [
    { id: "public", author: "Bob", text: "请复核断线场景", createdAt: "" },
    {
      id: "private-context",
      author: "Bob",
      text: "原生上下文意见",
      createdAt: "",
      target: { kind: "history", quote: "private history" },
    },
  ],
} as unknown as Collaboration;
const ref: LibraryReference = {
  spaceId: c.id,
  kind: "material",
  materialId: "one",
  version: 1,
};
afterEach(() => vi.unstubAllGlobals());

it("discloses secondary comment actions and keeps the reply draft when dismissing them", async () => {
  const user = userEvent.setup();
  render(
    <Discussion
      id={c.id}
      annotation={{
        ...c.annotations![0]!,
        createdAt: "2026-10-20T00:00:00Z",
      }}
      disabled={false}
      onSaved={() => {}}
      onLocate={() => {}}
    />,
  );
  const more = screen.getByLabelText("更多批注操作");
  expect(
    screen.getByRole("button", { name: "复制处理提示" }),
  ).not.toBeVisible();
  await user.click(more);
  expect(screen.getByRole("button", { name: "复制处理提示" })).toBeVisible();
  await user.keyboard("{Escape}");
  expect(more).toHaveFocus();
  expect(
    screen.getByRole("button", { name: "复制处理提示" }),
  ).not.toBeVisible();
  await user.click(more);
  await user.click(screen.getByRole("button", { name: "回复" }));
  expect(
    screen.getByRole("button", { name: "复制处理提示" }),
  ).not.toBeVisible();
  await user.type(screen.getByLabelText("回复这条批注"), "保留这段回复草稿");
  await user.click(more);
  await user.keyboard("{Escape}");
  expect(screen.getByLabelText("回复这条批注")).toHaveValue("保留这段回复草稿");
});

function mockReads() {
  const reads: { materialId: string; version: number }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      let data: unknown = {};
      if (String(input).endsWith("/read-material")) {
        const body = JSON.parse(String(init?.body));
        reads.push(body);
        data = {
          materialId: body.materialId,
          version: c
            .materials!.find((m) => m.id === body.materialId)!
            .versions.find((v) => v.version === body.version),
          segments: [
            {
              turnId: "t1",
              itemId: "answer",
              type: "agentMessage",
              text: `${body.materialId} version ${body.version}`,
              startOffset: 0,
              endOffset: 13,
              length: 13,
            },
          ],
          turns: [{ id: "t1", label: "分析" }],
          nextCursor: "",
        };
      }
      return new Response(JSON.stringify(data));
    }),
  );
  return reads;
}

it("retains each material's fixed version and cached reading when navigating and filtering", async () => {
  const user = userEvent.setup();
  const reads = mockReads();
  render(
    <SpaceWorkbench collaboration={c} onLocate={() => {}}>
      <Materials collaboration={c} reload={() => {}} onAnnotate={() => {}} />
    </SpaceWorkbench>,
  );
  await screen.findByText("one version 2", { selector: "[data-source-start]" });
  await user.selectOptions(screen.getByLabelText("查看固定版本"), "1");
  await screen.findByText("one version 1", { selector: "[data-source-start]" });
  await user.click(screen.getByRole("button", { name: "阅读 并发复核" }));
  await screen.findByText("two version 2", { selector: "[data-source-start]" });
  await user.click(screen.getByRole("button", { name: "阅读 重试调查" }));
  expect(screen.getByLabelText("查看固定版本")).toHaveValue("1");
  expect(
    screen.getByText("one version 1", { selector: "[data-source-start]" }),
  ).toBeVisible();
  await user.click(screen.getByRole("button", { name: "搜索材料" }));
  await user.type(screen.getByLabelText("搜索材料标题或作者"), "并发");
  expect(
    screen.queryByRole("button", { name: "阅读 重试调查" }),
  ).not.toBeInTheDocument();
  expect(
    screen.getByText("one version 1", { selector: "[data-source-start]" }),
  ).toBeVisible();
  expect(reads).toEqual([
    expect.objectContaining({ materialId: "one", version: 2 }),
    expect.objectContaining({ materialId: "one", version: 1 }),
    expect.objectContaining({ materialId: "two", version: 2 }),
  ]);
});

it("folds comments without losing an unsent draft or posting it", async () => {
  const user = userEvent.setup();
  mockReads();
  render(
    <SpaceWorkbench
      collaboration={c}
      onLocate={() => {}}
      comments={
        <Annotations
          id={c.id}
          annotations={[]}
          disabled={false}
          onSaved={() => {}}
          onLocate={() => {}}
        />
      }
    >
      <Materials collaboration={c} reload={() => {}} onAnnotate={() => {}} />
    </SpaceWorkbench>,
  );
  await user.click(screen.getByRole("button", { name: /^评论/ }));
  await user.type(screen.getByLabelText("你的意见"), "稍后核对，先保留");
  await user.click(
    within(screen.getByRole("complementary", { name: "空间评论" })).getByRole(
      "button",
      { name: "收起评论" },
    ),
  );
  expect(
    screen.queryByRole("complementary", { name: "空间评论" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: /^评论/ }));
  expect(screen.getByLabelText("你的意见")).toHaveValue("稍后核对，先保留");
  expect(
    vi
      .mocked(fetch)
      .mock.calls.some(([url]) => String(url).endsWith("/annotations")),
  ).toBe(false);
});

it("previews another version without changing selected references, and commits material plus public note together", async () => {
  const user = userEvent.setup();
  const reads = mockReads();
  const changed = vi.fn();
  function Picker() {
    const [refs, setRefs] = useState([ref]);
    return (
      <SpaceReferencePicker
        collaboration={c}
        value={refs}
        disabled={false}
        onChange={(next) => {
          setRefs(next);
          changed(next);
        }}
      />
    );
  }
  render(<Picker />);
  await user.click(screen.getByRole("button", { name: "添加引用" }));
  const dialog = screen.getByRole("dialog", { name: "添加引用" });
  await user.selectOptions(within(dialog).getByLabelText("引用预览版本"), "2");
  expect(reads).toHaveLength(0);
  await user.click(
    within(dialog).getByRole("button", { name: "预览所选版本" }),
  );
  await within(dialog).findByText("one version 2");
  expect(changed).not.toHaveBeenCalled();
  expect(within(dialog).getByText("已选 v1 · 固定引用")).toBeVisible();
  await user.click(within(dialog).getByRole("button", { name: /^批注/ }));
  expect(within(dialog).queryByText("原生上下文意见")).not.toBeInTheDocument();
  await user.click(
    within(dialog).getByRole("checkbox", { name: "选择 请复核断线场景" }),
  );
  await user.click(
    within(dialog).getByRole("button", { name: "添加 2 项引用" }),
  );
  expect(changed).toHaveBeenLastCalledWith([
    ref,
    { spaceId: c.id, kind: "annotation", annotationId: "public" },
  ]);
  await user.click(screen.getByRole("button", { name: "添加引用" }));
  await user.selectOptions(screen.getByLabelText("引用预览版本"), "2");
  await user.click(screen.getByRole("button", { name: "选择 v2" }));
  await user.click(screen.getByRole("button", { name: "取消" }));
  expect(changed).toHaveBeenCalledTimes(1);
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
});

it("adds fixed references in the reading dock before opening a request, without sending input", async () => {
  const user = userEvent.setup();
  mockReads();
  render(
    <SpaceWorkbench collaboration={c} onLocate={() => {}}>
      <Materials collaboration={c} reload={() => {}} onAnnotate={() => {}} />
    </SpaceWorkbench>,
  );
  await screen.findByText("one version 2", { selector: "[data-source-start]" });
  await user.selectOptions(screen.getByLabelText("查看固定版本"), "1");
  const dock = screen.getByRole("region", { name: "基于材料发起请求" });
  await user.type(within(dock).getByRole("textbox"), "结合原批注核对断线恢复");
  await user.click(within(dock).getByRole("button", { name: "添加引用" }));
  const picker = screen.getByRole("dialog", { name: "添加引用" });
  expect(within(picker).getByText("已选 v1 · 固定引用")).toBeVisible();
  await user.click(within(picker).getByRole("button", { name: /^批注/ }));
  await user.click(
    within(picker).getByRole("checkbox", { name: "选择 请复核断线场景" }),
  );
  await user.click(
    within(picker).getByRole("button", { name: "添加 2 项引用" }),
  );
  await user.click(within(dock).getByRole("button", { name: "发起请求" }));
  const composer = screen.getByRole("dialog", { name: "发起空间协作" });
  expect(within(composer).getByLabelText("Agent 处理要求")).toHaveValue(
    "结合原批注核对断线恢复",
  );
  expect(within(composer).getByText("重试调查 · v1")).toBeVisible();
  expect(within(composer).getByText("请复核断线场景")).toBeVisible();
  expect(
    vi
      .mocked(fetch)
      .mock.calls.filter(
        ([url, init]) =>
          init?.method === "POST" && !String(url).endsWith("/read-material"),
      ),
  ).toHaveLength(0);
});
