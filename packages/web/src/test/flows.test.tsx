import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Create } from "../components/Create";
import { Home } from "../components/Home";
import { Join } from "../components/Join";
import { Clients } from "../components/Clients";
import { Detail } from "../components/Detail";
import { Settings } from "../components/Settings";
import type { Collaboration } from "../types";
import { api } from "../api";
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
  localStorage.clear();
  sessionStorage.clear();
  location.hash = "/";
});
describe("产品路径", () => {
  it("安装版本变化时保留运行版本并提示重启，不自动停止服务", async () => {
    mockFetch(() => ({
      version: "0.1.1",
      installedVersion: "0.1.2",
      cli: {
        command: "/opt/homebrew/bin/teamcross",
        executable: "/Applications/Team Cross.app/Contents/Resources/teamcross",
        source: "app",
      },
      mcpCommand: "codex mcp add teamcross -- /app/teamcross mcp",
    }));
    render(<Settings theme="light" setTheme={() => {}} />);
    expect(await screen.findByText(/重新打开以应用更新/)).toHaveAttribute(
      "role",
      "status",
    );
    expect(screen.getByText("0.1.2")).toBeVisible();
    expect(screen.getByText("/opt/homebrew/bin/teamcross")).toBeVisible();
    expect(calls.every((call) => call.body === undefined)).toBe(true);
  });
  it("详情显示运行时确认的模型与推理强度", async () => {
    mockFetch((path) =>
      path.includes("/context")
        ? { thread: { turns: [] } }
        : {
            ...collaboration,
            model: "fixture-selected-model",
            reasoningEffort: "xhigh",
          },
    );
    const user = userEvent.setup();
    render(<Detail id="c1" />);
    await user.click(await screen.findByText("技术信息"));
    expect(screen.getByText("fixture-selected-model")).toBeVisible();
    expect(screen.getByText("xhigh")).toBeVisible();
    expect(screen.queryByText("gpt-5.6-luna")).not.toBeInTheDocument();
  });
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
  it("加入后不会显示邀请码期限，离线成员仍保留身份", async () => {
    mockFetch((path) =>
      path.includes("/context")
        ? { thread: { turns: [] } }
        : {
            ...collaboration,
            role: "owner",
            participantJoined: true,
            participantOnline: false,
            invitationState: "joined",
          },
    );
    render(<Detail id="c1" />);
    expect(await screen.findByText("已加入 · 暂时离线")).toBeVisible();
    expect(
      screen.getByText("同事已加入，访问持续有效，直到主动离开或结束共享。"),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "邀请同事" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText(/邀请有效至/)).not.toBeInTheDocument();
  });
  it("未使用的过期邀请可重新生成，不结束本地会话", async () => {
    mockFetch((path) =>
      path.includes("/context")
        ? { thread: { turns: [] } }
        : {
            ...collaboration,
            role: "owner",
            participantJoined: false,
            invitationState: "expired",
          },
    );
    render(<Detail id="c1" />);
    expect(
      await screen.findByText("邀请尚未使用且已到期，可以重新邀请同事。"),
    ).toBeVisible();
    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "邀请同事" }));
    expect(calls.some((c) => c.body?.action === "share")).toBe(true);
    expect(calls.some((c) => c.body?.action === "end")).toBe(false);
  });
  it("会话释放后读取上下文不恢复运行时，明确操作才恢复", async () => {
    mockFetch((path) =>
      path.includes("/context")
        ? {
            thread: {
              turns: [
                {
                  id: "retained",
                  items: [{ type: "agentMessage", text: "释放后保留的对话" }],
                },
              ],
            },
          }
        : {
            ...collaboration,
            role: "owner",
            online: false,
            sharing: false,
            runtimeState: "released",
          },
    );
    render(<Detail id="c1" />);
    expect(await screen.findByText("释放后保留的对话")).toBeVisible();
    expect(calls.every((c) => c.body === undefined)).toBe(true);
    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "恢复并打开 Codex" }));
    expect(calls.filter((c) => c.body?.action === "start")).toHaveLength(1);
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
    await user.click(screen.getByRole("button", { name: /创建并邀请/ }));
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
    fireEvent.change(
      screen.getByRole("textbox", { name: "邀请码或 App 链接" }),
      {
        target: { value: "tcx2.expired" },
      },
    );
    fireEvent.click(screen.getByRole("button", { name: /查看邀请信息/ }));
    expect(await screen.findByRole("alert")).toHaveTextContent("邀请已到期");
    expect(
      screen.getByRole("textbox", { name: "邀请码或 App 链接" }),
    ).toHaveValue("tcx2.expired");
    expect(screen.getByText(/双方需在同一局域网/)).toBeVisible();
  });
  it("等待交接时禁止直接打开，但允许辅助入口读取", async () => {
    mockFetch(() => ({
      mcpConfigured: true,
      binary: "/codex",
      desktopApp: "/Codex.app",
    }));
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
    await screen.findByText("MCP 配置已保存");
    expect(
      screen
        .getAllByRole("button", { name: "打开" })
        .every((b) => !(b as HTMLButtonElement).disabled),
    ).toBe(true);
    expect(calls.every((c) => c.body === undefined)).toBe(true);
  });
});

describe("首次使用连续路径", () => {
  it("写入断线只报告结果不明，不自动重放", async () => {
    const failed = vi.fn().mockRejectedValue(new TypeError("Failed to fetch"));
    vi.stubGlobal("fetch", failed);
    await expect(
      api("collaborations/c1/action", { action: "share" }),
    ).rejects.toMatchObject({
      code: "core_unreachable",
      recovery: "请重新连接后先查看操作结果，避免重复提交",
    });
    expect(failed).toHaveBeenCalledTimes(1);
  });
  it("fork 成功而分享失败时保留协作，转到原协作重试邀请", async () => {
    mockFetch((path) =>
      path.startsWith("sources")
        ? { data: [source] }
        : path === "preview"
          ? {
              source,
              previewHash: "confirmed",
              workspace: {
                sourceCwd: source.cwd,
                head: "abcdef",
                branch: "main",
                dirty: true,
              },
              targetDirectory: source.cwd,
            }
          : path.endsWith("/action")
            ? new Error("共享端口暂时不可用")
            : { ...collaboration, id: "retained-fork", role: "owner" },
    );
    const user = userEvent.setup();
    render(<Create />);
    await user.click(
      await screen.findByRole("radio", { name: /讨论协作入口/ }),
    );
    await user.click(screen.getByRole("button", { name: /下一步/ }));
    await screen.findByText("当前目录有未提交内容，将保留在原处供协作使用。");
    await user.click(screen.getByRole("button", { name: /创建并邀请/ }));
    await waitFor(() =>
      expect(location.hash).toBe("#/collaborations/retained-fork"),
    );
    expect(calls.filter((c) => c.path === "collaborations")).toHaveLength(1);
    expect(sessionStorage.getItem("teamcross.create.retained-fork")).toContain(
      "共享端口暂时不可用",
    );
  });
  it("邀请预览不启动 Codex，确认后直接进入上下文", async () => {
    mockFetch((path) =>
      path === "invitations/preview"
        ? { title: "共享任务", host: "A", expiresAt: new Date().toISOString() }
        : { id: "joined-1" },
    );
    const user = userEvent.setup();
    render(<Join pendingId="opaque-local-id" />);
    await screen.findByText("共享任务");
    expect(calls.map((c) => c.path)).toEqual(["invitations/preview"]);
    await user.click(screen.getByRole("button", { name: /确认加入/ }));
    await waitFor(() =>
      expect(location.hash).toBe("#/collaborations/joined-1"),
    );
    expect(calls[1]).toEqual({
      path: "join",
      body: { pendingId: "opaque-local-id" },
    });
  });
  it("申请输入不发送模型指令，也不自动交接", async () => {
    mockFetch((path) =>
      path.includes("/context") ? { thread: { turns: [] } } : collaboration,
    );
    const user = userEvent.setup();
    render(<Detail id="c1" />);
    await user.click(await screen.findByRole("button", { name: "申请输入" }));
    await waitFor(() =>
      expect(calls.some((c) => c.body?.action === "request_input")).toBe(true),
    );
    expect(
      calls.some(
        (c) => c.path.endsWith("/rpc") || c.body?.action === "handoff",
      ),
    ).toBe(false);
  });
  it("客户端连接不等于共享会话已打开", async () => {
    mockFetch(() => ({ binary: "/codex", desktopApp: "/Codex.app" }));
    render(
      <Clients
        collaboration={{
          ...collaboration,
          writer: "remote",
          connected: true,
          clientState: "connected",
        }}
      />,
    );
    expect(screen.getByText(/等待在 Codex 中打开共享会话/)).toBeVisible();
    expect(screen.queryByText("共享会话已打开。")).not.toBeInTheDocument();
  });
});
