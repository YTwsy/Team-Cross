import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Create } from "../components/Create";
import { Home } from "../components/Home";
import { Join } from "../components/Join";
import { Clients } from "../components/Clients";
import { Detail } from "../components/Detail";
import type { Collaboration } from "../types";
const source = {
  id: "a9b38240-2d98-4c64-9467-2339b3c9a522",
  name: "讨论协作入口",
  preview: "已有上下文",
  cwd: "/project/src",
  updatedAt: Date.now() / 1000,
};
const collaboration = {
  id: "c1",
  title: "协作",
  role: "remote",
  writer: "owner",
  online: true,
  sharing: true,
  executionCwd: "/project",
  host: "A 的 Mac",
  workspaceMode: "existing",
  state: "ready",
  annotations: [],
  approvals: 0,
  sourceId: "source",
  sessionId: "fork",
  repo: "/project",
  head: "abcdef",
  branch: "main",
  createdAt: new Date().toISOString(),
  updatedAt: new Date().toISOString(),
  busy: false,
  connected: false,
  epoch: 1,
  sequence: 0,
  model: "gpt-5.6-luna",
} satisfies Collaboration;
const calls: { path: string; body: any }[] = [];
function mockFetch(handler: (path: string, body: any) => unknown) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const path = url.replace("/api/", "");
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ path, body });
      const result = handler(path, body);
      return {
        ok: !(result instanceof Error),
        status: result instanceof Error ? 400 : 200,
        json: async () =>
          result instanceof Error ? { error: result.message } : result,
      };
    }),
  );
}
beforeEach(() => {
  calls.length = 0;
  location.hash = "/";
});
describe("产品路径", () => {
  it.each([
    ["ended", "这次共享已结束"],
    ["expired", "这份邀请已到期"],
  ])("%s 时明确要求新邀请，不再提示等待连接", async (state, heading) => {
    mockFetch(() => ({
      ...collaboration,
      state,
      online: false,
      sharing: false,
      error: "这份邀请不再有效",
    }));
    render(<Detail id="c1" />);
    expect(await screen.findByRole("heading", { name: heading })).toBeVisible();
    expect(
      screen.getByRole("link", { name: /使用新邀请加入/ }),
    ).toHaveAttribute("href", "#/join");
    expect(screen.queryByText("正在等待发起者连接")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "打开 Codex" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /用自己的 Codex 辅助/ }),
    ).toBeDisabled();
    expect(calls.every((c) => c.body === undefined)).toBe(true);
  });
  it("首页首次使用展示两个明确入口", async () => {
    mockFetch(() => []);
    render(<Home />);
    expect(screen.getByRole("link", { name: /发起协作/ })).toHaveAttribute(
      "href",
      "#/create",
    );
    expect(screen.getByRole("link", { name: /加入协作/ })).toHaveAttribute(
      "href",
      "#/join",
    );
    expect(await screen.findByText("下一次协作，从这里开始")).toBeVisible();
  });
  it("创建通过确认起点传递模式，且没有未跟踪文件选择", async () => {
    mockFetch((path, body) =>
      path.startsWith("sources")
        ? { data: [source] }
        : path === "preview"
          ? {
              source,
              previewHash: body.workspaceMode,
              workspace: {
                sourceCwd: source.cwd,
                head: "abcdef123456",
                branch: "main",
                dirty: true,
              },
              targetDirectory: "/data/collaborations/id/worktree/src",
            }
          : { id: "new" },
    );
    const user = userEvent.setup();
    render(<Create />);
    await user.click(
      await screen.findByRole("radio", { name: /讨论协作入口/ }),
    );
    await user.click(screen.getByRole("button", { name: /下一步/ }));
    await screen.findByText("当前目录有未提交内容，将保留在原处供协作使用。");
    await user.click(screen.getByRole("radio", { name: /创建新 worktree/ }));
    await screen.findByText("/data/collaborations/id/worktree/src");
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /创建协作/ }));
    await waitFor(() => expect(location.hash).toBe("#/collaborations/new"));
    const request = calls.find((c) => c.path === "collaborations")!.body;
    expect(request.workspaceMode).toBe("worktree");
    expect(request.previewHash).toBe("worktree");
    expect(request).not.toHaveProperty("untrackedFiles");
    expect(calls.some((c) => c.path.endsWith("/rpc"))).toBe(false);
  });
  it("失效邀请给出下一步并保留输入供修正", async () => {
    mockFetch(() => new Error("邀请已到期，请让发起者重新分享"));
    render(<Join />);
    fireEvent.change(screen.getByRole("textbox", { name: "邀请内容" }), {
      target: { value: "tcx2.expired" },
    });
    fireEvent.click(screen.getByRole("button", { name: /连接并查看协作/ }));
    expect(await screen.findByRole("alert")).toHaveTextContent("邀请已到期");
    expect(screen.getByRole("textbox", { name: "邀请内容" })).toHaveValue(
      "tcx2.expired",
    );
    expect(screen.getByText(/请确认邀请仍有效/)).toBeVisible();
  });
  it("等待交接时禁止直接打开，但允许辅助入口读取", async () => {
    mockFetch(() => ({ mcpConfigured: true }));
    const user = userEvent.setup();
    render(<Clients collaboration={collaboration} />);
    expect(
      screen
        .getAllByRole("button", { name: "打开" })
        .every((b) => (b as HTMLButtonElement).disabled),
    ).toBe(true);
    await user.click(
      screen.getByRole("button", { name: "用自己的 Codex 辅助" }),
    );
    await screen.findByText("本机 MCP 已接入");
    expect(
      screen
        .getAllByRole("button", { name: "打开" })
        .every((b) => !(b as HTMLButtonElement).disabled),
    ).toBe(true);
    expect(calls.every((c) => c.body === undefined)).toBe(true);
  });
});
