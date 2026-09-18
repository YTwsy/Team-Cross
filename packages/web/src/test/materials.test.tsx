import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { Detail } from "../components/Detail";
import { Publisher } from "../components/Publisher";
import { Annotations } from "../components/Annotations";
import type { Collaboration, Material, PublicationDraft } from "../types";

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
const draft: PublicationDraft = {
  id: "draft",
  hash: "all",
  title: "调查过程",
  provider: "codex",
  sourceId: "source",
  frozenAt: new Date().toISOString(),
  startTurnId: "t0",
  endTurnId: "t2",
  turns: [0, 1, 2].map((i) => ({
    id: `t${i}`,
    status: "completed",
    items: [
      {
        id: `i${i}`,
        type: "userMessage",
        text: i === 1 ? "验证超时" : "本机未选择历史",
      },
    ],
  })),
};
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
  expect(screen.getByRole("button", { name: "附上这次调查" })).toBeEnabled();
  expect(
    screen.queryByRole("button", { name: /恢复运行时|申请输入|接回输入/ }),
  ).not.toBeInTheDocument();
  expect(
    calls.some((c) => c.includes("context") || c.includes("read-material")),
  ).toBe(false);
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

it("publishes the reviewed range and reuses its request after a lost response", async () => {
  const user = userEvent.setup(),
    published = vi.fn(),
    bodies: Record<string, unknown>[] = [];
  let attempts = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input),
        body = init?.body ? JSON.parse(String(init.body)) : {};
      if (path.startsWith("/api/sources"))
        return json({
          data: [{ id: "source", name: "调查过程", preview: "本机历史" }],
        });
      if (path === "/api/publications/source") return json(draft);
      if (path === "/api/publications/preview") {
        bodies.push(body);
        return json({
          ...draft,
          id: "reviewed",
          hash: "selected",
          startTurnId: body.startTurnId,
          endTurnId: body.endTurnId,
          turns: [draft.turns[1]],
        });
      }
      if (path === "/api/collaborations/space/materials") {
        bodies.push(body);
        if (++attempts === 1) throw new Error("response lost");
        return json({ materialId: "new", version: 1, state: "published" });
      }
      if (path === "/api/collaborations/space/publication-status")
        return json({ state: "not_found" });
      return json({});
    }),
  );
  render(<Publisher spaceId="space" onPublished={published} />);
  await user.click(await screen.findByRole("radio", { name: /调查过程/ }));
  await user.click(screen.getByRole("button", { name: "选择公开范围" }));
  await user.selectOptions(
    await screen.findByLabelText("从这次提问开始"),
    "t1",
  );
  await user.selectOptions(screen.getByLabelText("公开到这里为止"), "t1");
  await user.click(screen.getByRole("button", { name: "预览实际公开内容" }));
  await user.click(await screen.findByRole("button", { name: "发布到空间" }));
  await user.click(await screen.findByRole("button", { name: "查询发布结果" }));
  await screen.findByText(/尚未查到已保存结果/);
  await user.click(screen.getByRole("button", { name: "发布到空间" }));
  await waitFor(() => expect(published).toHaveBeenCalled());
  expect(bodies[0]).toMatchObject({
    startTurnId: "t1",
    endTurnId: "t1",
    draftId: "draft",
  });
  expect(bodies[1]).toEqual(bodies[2]);
  expect(bodies[1]).toMatchObject({
    previewId: "reviewed",
    previewHash: "selected",
  });
  expect(bodies[1]).not.toHaveProperty("sourceId");
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
  const selectors = screen.getAllByLabelText("附上已发布调查");
  await user.selectOptions(selectors[1]!, "m1:1");
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

it("keeps the previous publication boundaries when preparing a new version", async () => {
  const user = userEvent.setup();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => json(draft)),
  );
  render(
    <Publisher spaceId="space" material={material} onPublished={() => {}} />,
  );
  await user.click(screen.getByRole("button", { name: "选择公开范围" }));
  expect(await screen.findByLabelText("从这次提问开始")).toHaveValue("t1");
  expect(screen.getByLabelText("公开到这里为止")).toHaveValue("t2");
});

it("requires explicit reselection when the previous boundary is missing", async () => {
  const user = userEvent.setup();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      json({ ...draft, turns: [draft.turns[0], draft.turns[2]] }),
    ),
  );
  render(
    <Publisher spaceId="space" material={material} onPublished={() => {}} />,
  );
  await user.click(screen.getByRole("button", { name: "选择公开范围" }));
  expect(await screen.findByLabelText("从这次提问开始")).toHaveValue("");
  expect(
    screen.getByRole("button", { name: "预览实际公开内容" }),
  ).toBeDisabled();
  expect(screen.getByText(/不会自动扩大公开范围/)).toBeVisible();
});
