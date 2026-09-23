import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Create } from "../components/Create";
import { StartSpace } from "../components/Publisher";
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
function emptyHistory() {
  return {
    thread: {},
    contentHash: "empty",
    pageCursor: "page",
    nextCursor: "",
    scope: "stream",
    sourcePageComplete: true,
    pageEndsAtTurnBoundary: true,
    turns: [],
    segments: [],
  };
}
function historyMessage(text: string, turnId = "turn") {
  return {
    ...emptyHistory(),
    contentHash: text,
    turns: [{ id: turnId, label: text }],
    segments: [
      {
        turnId,
        itemId: "answer",
        type: "agentMessage",
        text,
        startOffset: 0,
        endOffset: text.length,
        length: text.length,
      },
    ],
  };
}
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

describe("邀请者在个人 Codex 中打开协作", () => {
  it("同事正在操作时不自动跳转，点击才打开已有 fork", async () => {
    mockFetch((path) => {
      if (path.endsWith("/personal-desktop"))
        return {
          launched: true,
          note: "已请求个人 Codex 打开此协作会话，请在 Desktop 中查看。",
        };
      if (path.includes("/context")) return emptyHistory();
      return {
        ...collaboration,
        role: "owner",
        writer: "remote",
        busy: true,
        sequence: 20,
        runtimeState: "running",
      };
    });
    const user = userEvent.setup();
    render(<Detail id="c1" />);
    const button = await screen.findByRole("button", {
      name: "在个人 Codex 中打开",
    });
    expect(button).toBeEnabled();
    expect(calls.every((call) => call.body === undefined)).toBe(true);
    await user.click(button);
    expect(
      await screen.findByText(/已请求个人 Codex 打开此协作会话/),
    ).toHaveAttribute("role", "status");
    expect(calls.filter((call) => call.body !== undefined)).toEqual([
      { path: "collaborations/c1/personal-desktop", body: { launch: true } },
    ]);
  });

  it.each([
    { role: "remote" },
    { role: "owner", provider: "claude" },
    { role: "owner", sessionId: "" },
    { role: "owner", sessionId: collaboration.sourceId },
  ])("不显示不适用的个人 Codex 入口：%j", async (overrides) => {
    mockFetch((path) =>
      path.includes("/context")
        ? emptyHistory()
        : { ...collaboration, ...overrides },
    );
    render(<Detail id="c1" />);
    await screen.findByRole("heading", { name: "协作" });
    expect(
      screen.queryByRole("button", { name: /在个人 Codex 中/ }),
    ).not.toBeInTheDocument();
    expect(calls.every((call) => call.body === undefined)).toBe(true);
  });

  it.each([
    ["running", false, "在个人 Codex 中打开"],
    ["releasing", false, "在个人 Codex 中打开"],
    ["released", true, "在个人 Codex 中打开"],
    ["released", false, "在个人 Codex 中继续"],
  ])(
    "仅释放后提供继续入口：%s / sharing=%s",
    async (runtimeState, sharing, label) => {
      mockFetch((path) =>
        path.includes("/context")
          ? emptyHistory()
          : {
              ...collaboration,
              role: "owner",
              online: runtimeState === "running",
              runtimeState,
              sharing,
            },
      );
      render(<Detail id="c1" />);
      expect(await screen.findByRole("button", { name: label })).toBeEnabled();
    },
  );

  it("打开失败可重试，不误报已打开或恢复运行时", async () => {
    let attempts = 0;
    mockFetch((path) => {
      if (path.endsWith("/personal-desktop")) {
        attempts++;
        return attempts === 1
          ? new Error("未找到 Codex Desktop")
          : { launched: true, note: "已请求个人 Codex 打开此协作会话。" };
      }
      return path.includes("/context")
        ? emptyHistory()
        : { ...collaboration, role: "owner", online: false };
    });
    const user = userEvent.setup();
    render(<Detail id="c1" />);
    const button = await screen.findByRole("button", {
      name: "在个人 Codex 中打开",
    });
    await user.click(button);
    await screen.findByText("未找到 Codex Desktop");
    expect(screen.queryByText(/已请求个人 Codex/)).not.toBeInTheDocument();
    await user.click(button);
    await screen.findByText(/已请求个人 Codex/);
    expect(calls.filter((call) => call.body !== undefined)).toHaveLength(2);
    expect(calls.some((call) => call.path.endsWith("/action"))).toBe(false);
  });
});

describe("个人 Claude Code 辅助模式", () => {
  const info = {
    binary: "/codex",
    claudeBinary: "/claude",
    claudeVersion: "2.1.268 (Claude Code)",
    desktopApp: "/Codex.app",
    mcpProbed: true,
    mcpClients: {
      codex: {
        configured: true,
        command: "codex mcp add",
        observedAt: "2026-09-12T01:00:00Z",
      },
      claude: {
        configured: false,
        command: "claude mcp add",
        observedAt: "0001-01-01T00:00:00Z",
      },
    },
  };
  it("Codex 工具调用不会把 Claude 误报为已接入，安装明确选择个人客户端", async () => {
    let installed = false;
    mockFetch((path) => {
      if (path === "mcp/setup") {
        installed = true;
        return { ok: true };
      }
      if (path === "mcp/probe") return { ready: true };
      return {
        ...info,
        mcpClients: {
          ...info.mcpClients,
          claude: { ...info.mcpClients.claude, configured: installed },
        },
      };
    });
    const user = userEvent.setup();
    render(<Clients collaboration={collaboration} initial="assist" />);
    await user.click(
      await screen.findByRole("button", { name: "Claude Code" }),
    );
    expect(screen.getByText(/等待实际工具调用/)).toBeVisible();
    expect(screen.getByRole("button", { name: "打开" })).toBeDisabled();
    expect(
      screen.queryByRole("heading", { name: "Codex Desktop" }),
    ).not.toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: "接入本机 Claude Code" }),
    );
    await screen.findByText("Claude Code MCP 配置已保存");
    expect(calls.find((c) => c.path === "mcp/setup")?.body).toEqual({
      provider: "claude",
    });
    expect(screen.getByText(/等待实际工具调用/)).toBeVisible();
    expect(screen.getByRole("button", { name: "打开" })).toBeEnabled();
  });
  it("个人 Claude 可辅助 Codex 协作，等待交接时也能打开个人会话", async () => {
    mockFetch((path) =>
      path.endsWith("/assist")
        ? { command: "personal-claude-command", launched: false }
        : {
            ...info,
            mcpClients: {
              ...info.mcpClients,
              claude: { ...info.mcpClients.claude, configured: true },
            },
          },
    );
    const user = userEvent.setup();
    render(<Clients collaboration={collaboration} initial="assist" />);
    await user.click(
      await screen.findByRole("button", { name: "Claude Code" }),
    );
    await user.click(screen.getByRole("button", { name: "查看启动命令" }));
    await screen.findByText("personal-claude-command");
    expect(calls.find((c) => c.path.endsWith("/assist"))?.body).toEqual({
      provider: "claude",
      client: "tui",
      launch: false,
    });
    expect(
      calls.some(
        (c) =>
          c.path.endsWith("/open") ||
          c.path.endsWith("/action") ||
          c.path.endsWith("/rpc"),
      ),
    ).toBe(false);
    await user.click(screen.getByRole("button", { name: "Codex" }));
    expect(
      screen.queryByText("personal-claude-command"),
    ).not.toBeInTheDocument();
  });
  it("Claude 协作默认选择个人 Claude，缺少 Codex 不阻断辅助入口", async () => {
    mockFetch(() => ({
      ...info,
      codexError: "未安装 Codex",
      mcpClients: {
        ...info.mcpClients,
        claude: { ...info.mcpClients.claude, configured: true },
      },
    }));
    render(
      <Clients
        collaboration={{ ...collaboration, provider: "claude" }}
        initial="assist"
      />,
    );
    await screen.findByText("Claude Code MCP 配置已保存");
    expect(screen.getByRole("button", { name: "Claude Code" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByRole("button", { name: "打开" })).toBeEnabled();
    expect(screen.queryByText("未安装 Codex")).not.toBeInTheDocument();
  });
  it("设置页说明同名覆盖并禁止误启动", async () => {
    mockFetch(() => ({
      ...info,
      mcpClients: {
        ...info.mcpClients,
        claude: {
          ...info.mcpClients.claude,
          configError: "当前项目已禁用 teamcross",
        },
      },
    }));
    const user = userEvent.setup();
    render(
      <Settings
        theme="light"
        setTheme={() => {}}
        uiLanguage={{ mode: "auto", resolved: "zh-CN" }}
        onLanguageChange={() => {}}
      />,
    );
    await user.click(
      await screen.findByRole("button", { name: "Claude Code" }),
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "当前项目已禁用 teamcross",
    );
    expect(
      screen.getByRole("button", { name: "接入本机 Claude Code" }),
    ).toBeDisabled();
    expect(calls.every((c) => c.body === undefined)).toBe(true);
  });
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
    render(
      <Settings
        theme="light"
        setTheme={() => {}}
        uiLanguage={{ mode: "auto", resolved: "zh-CN" }}
        onLanguageChange={() => {}}
      />,
    );
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
        ? emptyHistory()
        : {
            ...collaboration,
            model: "fixture-selected-model",
            reasoningEffort: "xhigh",
          },
    );
    const user = userEvent.setup();
    render(<Detail id="c1" />);
    await user.click(await screen.findByRole("tab", { name: "技术信息" }));
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
      screen.getByRole("button", { name: /用自己的客户端辅助/ }),
    ).toBeDisabled();
    expect(calls.every((c) => c.body === undefined)).toBe(true);
  });
  it("加入后不会显示邀请码期限，离线成员仍保留身份", async () => {
    mockFetch((path) =>
      path.includes("/context")
        ? emptyHistory()
        : {
            ...collaboration,
            role: "owner",
            participantJoined: true,
            participantOnline: false,
            members: [
              {
                id: "b",
                name: "同事",
                active: true,
                online: false,
                inputRequested: false,
              },
            ],
            invitationState: "joined",
          },
    );
    const user = userEvent.setup();
    render(<Detail id="c1" />);
    expect(await screen.findByText("暂时离线")).toBeVisible();
    await user.click(screen.getByLabelText("查看共享状态详情"));
    expect(
      screen.getByText("同事已加入，访问持续有效，直到主动离开或结束共享。"),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "邀请成员" })).toBeEnabled();
    expect(screen.queryByText(/邀请有效至/)).not.toBeInTheDocument();
  });
  it("未使用的过期邀请可重新生成，不结束本地会话", async () => {
    mockFetch((path) =>
      path.includes("/context")
        ? emptyHistory()
        : {
            ...collaboration,
            role: "owner",
            participantJoined: false,
            invitationState: "expired",
          },
    );
    const user = userEvent.setup();
    render(<Detail id="c1" />);
    await user.click(await screen.findByLabelText("查看共享状态详情"));
    expect(
      await screen.findByText("邀请尚未使用且已到期，可以重新邀请同事。"),
    ).toBeVisible();
    await user.click(screen.getByRole("button", { name: "邀请成员" }));
    expect(screen.getByRole("radio", { name: /局域网/ })).toBeChecked();
    expect(
      screen.getByRole("radio", { name: /Tailcat 跨网络/ }),
    ).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "重新开放链接" }));
    expect(
      calls.some(
        (c) =>
          c.path.endsWith("/invitations") &&
          c.body?.transport === "lan" &&
          !!c.body?.requestId,
      ),
    ).toBe(true);
    expect(calls.some((c) => c.body?.action === "end")).toBe(false);
  });
  it("会话释放后读取上下文不恢复运行时，明确操作才恢复", async () => {
    mockFetch((path) =>
      path.includes("/context")
        ? historyMessage("释放后保留的对话", "retained")
        : {
            ...collaboration,
            role: "owner",
            online: false,
            sharing: false,
            runtimeState: "released",
          },
    );
    render(<Detail id="c1" />);
    expect(
      await screen.findByText("释放后保留的对话", {
        selector: "[data-source-start]",
      }),
    ).toBeVisible();
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
  it("发起页用卡片区分形态，来源客户端作为会话列表筛选", async () => {
    mockFetch((path) =>
      path.startsWith("sources")
        ? {
            data: path.includes("provider=claude")
              ? [{ ...source, id: "claude", name: "Claude 来源" }]
              : [source],
          }
        : {},
    );
    const user = userEvent.setup();
    render(<StartSpace />);
    expect(screen.getByRole("radio", { name: /先分享讨论/ })).toBeChecked();
    expect(screen.getByRole("group", { name: "这次要做什么" })).toBeVisible();
    expect(screen.getByRole("group", { name: "来源客户端" })).toBeVisible();
    expect(screen.getByRole("button", { name: "选择公开范围" })).toBeDisabled();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
    await user.click(
      await screen.findByRole("radio", { name: /讨论协作入口/ }),
    );
    expect(screen.getByRole("radio", { name: /讨论协作入口/ })).toBeChecked();
    await user.click(screen.getByRole("radio", { name: /直接一起执行/ }));
    expect(screen.getAllByRole("heading", { name: "发起协作" })).toHaveLength(
      1,
    );
    expect(screen.getAllByRole("link", { name: /协作空间/ })).toHaveLength(1);
    expect(screen.getByRole("heading", { name: "从哪里继续？" })).toBeVisible();
    expect(screen.getByRole("button", { name: /下一步/ })).toBeDisabled();
    expect(screen.getByRole("group", { name: "来源客户端" })).toBeVisible();
    await user.click(screen.getByRole("radio", { name: /先分享讨论/ }));
    expect(screen.getByRole("radio", { name: /讨论协作入口/ })).toBeChecked();
    expect(
      screen.queryByText(
        "Claude Code 历史接入仍是实验性能力。只读取已保存的会话。",
      ),
    ).not.toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: "Claude Code · 实验性" }),
    );
    expect(
      await screen.findByText(
        "Claude Code 历史接入仍是实验性能力。只读取已保存的会话。",
      ),
    ).toBeVisible();
    expect(
      await screen.findByRole("radio", { name: /Claude 来源/ }),
    ).toBeVisible();
    expect(
      screen.queryByRole("radio", { name: /讨论协作入口/ }),
    ).not.toBeInTheDocument();
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
    await user.click(
      screen.getByRole("radio", { name: /Tailcat 跨网络.*实验性/ }),
    );
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /创建并邀请/ }));
    await waitFor(() => expect(location.hash).toBe("#/collaborations/new"));
    const request = calls.find((c) => c.path === "collaborations")!.body;
    expect(request.workspaceMode).toBe("worktree");
    expect(request.runtimeMode).toBe("restricted");
    expect(request.previewHash).toBe("worktree");
    expect(request).not.toHaveProperty("untrackedFiles");
    expect(
      calls.some(
        (c) => c.body?.action === "share" && c.body?.transport === "tailcat",
      ),
    ).toBe(true);
    expect(sessionStorage.getItem("teamcross.transport.new")).toBe("tailcat");
    expect(calls.some((c) => c.path.endsWith("/rpc"))).toBe(false);
  });
  it.each(["codex", "claude"])(
    "%s 创建信任模式重新确认起点并固定授权",
    async (provider) => {
      mockFetch((path, body) =>
        path.startsWith("sources")
          ? { data: [source] }
          : path === "preview"
            ? {
                runtimeMode: body.runtimeMode,
                previewHash: body.runtimeMode,
                workspace: {
                  sourceCwd: source.cwd,
                  head: "abcdef",
                  branch: "main",
                  dirty: false,
                },
              }
            : {
                ...collaboration,
                id: "trusted-fork",
                provider,
                runtimeMode: "trusted",
              },
      );
      const user = userEvent.setup();
      render(<Create />);
      if (provider === "claude")
        await user.click(
          screen.getByRole("button", { name: "Claude Code · 实验性" }),
        );
      await user.click(
        await screen.findByRole("radio", { name: /讨论协作入口/ }),
      );
      await user.click(screen.getByRole("button", { name: /下一步/ }));
      await screen.findByText("确认起点");
      expect(screen.getByRole("radio", { name: /受限模式/ })).toBeChecked();
      const original = calls.find((c) => c.path === "preview")!.body;
      await user.click(screen.getByRole("radio", { name: /信任模式/ }));
      await waitFor(() =>
        expect(
          calls.filter((c) => c.path === "preview").at(-1)!.body.runtimeMode,
        ).toBe("trusted"),
      );
      await screen.findByText("确认起点");
      expect(screen.getByText(/模式创建后固定/)).toBeVisible();
      await user.click(screen.getByRole("button", { name: /创建并邀请/ }));
      await waitFor(() =>
        expect(location.hash).toBe("#/collaborations/trusted-fork"),
      );
      const input = calls.find((c) => c.path === "collaborations")!.body;
      expect(input).toMatchObject({
        provider,
        runtimeMode: "trusted",
        previewHash: "trusted",
      });
      expect(input.requestId).not.toBe(original.requestId);
    },
  );
  it("协作者详情显示固定的信任范围且不提供模式切换", async () => {
    mockFetch((path) =>
      path.includes("/context")
        ? {}
        : { ...collaboration, role: "remote", runtimeMode: "trusted" },
    );
    render(<Detail id="c1" />);
    const mode = await screen.findByLabelText("查看协作模式说明");
    expect(mode).toHaveTextContent("信任模式");
    expect(screen.getByText(/沿用邀请者的原生配置与权限/)).not.toBeVisible();
    fireEvent.click(mode);
    expect(await screen.findByText("信任模式 · 创建后固定")).toBeVisible();
    expect(screen.getByText(/沿用邀请者的原生配置与权限/)).toBeVisible();
    expect(
      screen.queryByRole("radio", { name: /模式/ }),
    ).not.toBeInTheDocument();
  });
  it("失效邀请给出下一步并保留输入供修正", async () => {
    mockFetch(() => new Error("邀请已到期，请让发起者重新分享"));
    render(<Join />);
    fireEvent.change(
      screen.getByRole("textbox", { name: "邀请码或 App 链接" }),
      {
        target: { value: "tcx3.expired" },
      },
    );
    fireEvent.click(screen.getByRole("button", { name: /查看邀请信息/ }));
    expect(await screen.findByRole("alert")).toHaveTextContent("邀请已到期");
    expect(
      screen.getByRole("textbox", { name: "邀请码或 App 链接" }),
    ).toHaveValue("tcx3.expired");
    expect(
      screen.getByText(/邀请会声明使用局域网或实验性 Tailcat/),
    ).toBeVisible();
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
      screen.getByRole("button", { name: "用自己的客户端辅助" }),
    );
    await screen.findByText("Codex MCP 配置已保存");
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
    await user.click(
      screen.getByRole("radio", { name: /Tailcat 跨网络.*实验性/ }),
    );
    await user.click(screen.getByRole("button", { name: /创建并邀请/ }));
    await waitFor(() =>
      expect(location.hash).toBe("#/collaborations/retained-fork"),
    );
    expect(calls.filter((c) => c.path === "collaborations")).toHaveLength(1);
    expect(sessionStorage.getItem("teamcross.create.retained-fork")).toContain(
      "共享端口暂时不可用",
    );
    expect(sessionStorage.getItem("teamcross.transport.retained-fork")).toBe(
      "tailcat",
    );
  });
  it("Tailcat 邀请准备中可显式取消，不切换到局域网", async () => {
    mockFetch((path) =>
      path.includes("/context")
        ? emptyHistory()
        : {
            ...collaboration,
            role: "owner",
            sharing: false,
            sharingPreparing: true,
            transport: "tailcat",
          },
    );
    const user = userEvent.setup();
    render(<Detail id="c1" />);
    await user.click(await screen.findByLabelText("查看共享状态详情"));
    await user.click(screen.getByRole("button", { name: "取消生成邀请" }));
    expect(
      screen.getByRole("heading", { name: "取消生成邀请？" }),
    ).toBeVisible();
    await user.click(screen.getByRole("button", { name: "取消生成" }));
    await waitFor(() =>
      expect(calls.some((c) => c.body?.action === "end")).toBe(true),
    );
    expect(calls.some((c) => c.body?.transport === "lan")).toBe(false);
  });
  it("邀请预览不启动 Codex，确认后直接进入上下文", async () => {
    mockFetch((path) =>
      path === "invitations/preview"
        ? {
            title: "共享任务",
            host: "A",
            expiresAt: new Date().toISOString(),
            transport: "tailcat",
          }
        : { id: "joined-1" },
    );
    const user = userEvent.setup();
    render(<Join pendingId="opaque-local-id" />);
    await screen.findByText("共享任务");
    expect(screen.getByText("连接方式：Tailcat 跨网络")).toBeVisible();
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
      path.includes("/context") ? emptyHistory() : collaboration,
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

describe("Claude 原生协作", () => {
  it("切换客户端清空旧来源，并在预览和创建中保留 provider", async () => {
    mockFetch((path) =>
      path.startsWith("sources")
        ? {
            data: [
              {
                ...source,
                name: path.includes("provider=claude")
                  ? "Claude 来源"
                  : "Codex 来源",
              },
            ],
          }
        : path === "preview"
          ? {
              previewHash: "claude-preview",
              workspace: {
                sourceCwd: source.cwd,
                head: "abcdef",
                branch: "main",
                dirty: false,
              },
            }
          : { ...collaboration, id: "claude-fork", provider: "claude" },
    );
    const user = userEvent.setup();
    render(<Create />);
    await user.click(await screen.findByRole("radio", { name: /Codex 来源/ }));
    await user.click(
      screen.getByRole("button", { name: "Claude Code · 实验性" }),
    );
    expect(screen.getByRole("button", { name: /下一步/ })).toBeDisabled();
    expect(
      screen.queryByRole("radio", { name: /Codex 来源/ }),
    ).not.toBeInTheDocument();
    await user.click(await screen.findByRole("radio", { name: /Claude 来源/ }));
    await user.click(screen.getByRole("button", { name: /下一步/ }));
    await screen.findByText("确认起点");
    await user.click(screen.getByRole("button", { name: /创建并邀请/ }));
    await waitFor(() =>
      expect(location.hash).toBe("#/collaborations/claude-fork"),
    );
    const writes = calls.filter((c) =>
      ["preview", "collaborations"].includes(c.path),
    );
    expect(writes).toHaveLength(2);
    expect(writes.every((c) => c.body.provider === "claude")).toBe(true);
  });
  it("过期的 Codex 分页结果不会出现在 Claude 列表", async () => {
    let resolvePage!: (value: unknown) => void;
    mockFetch((path) => {
      if (path.includes("cursor="))
        return new Promise((resolve) => {
          resolvePage = resolve;
        });
      return path.includes("provider=claude")
        ? { data: [{ ...source, id: "claude", name: "Claude 来源" }] }
        : { data: [source], nextCursor: "page-two" };
    });
    const user = userEvent.setup();
    render(<Create />);
    await user.click(await screen.findByRole("button", { name: "加载更多" }));
    await user.click(
      screen.getByRole("button", { name: "Claude Code · 实验性" }),
    );
    await screen.findByRole("radio", { name: /Claude 来源/ });
    resolvePage({
      data: [{ ...source, id: "late", name: "过期分页" }],
      nextCursor: null,
    });
    await waitFor(() =>
      expect(
        screen.queryByRole("radio", { name: /过期分页/ }),
      ).not.toBeInTheDocument(),
    );
  });
  it("Claude 仅显示原生 TUI，Codex 缺失不阻止打开 Claude", async () => {
    mockFetch(() => ({
      claudeBinary: "/claude",
      claudeVersion: "2.1.268",
      codexError: "未安装 Codex",
    }));
    render(
      <Clients
        collaboration={{
          ...collaboration,
          provider: "claude",
          writer: "remote",
        }}
      />,
    );
    expect(
      await screen.findByRole("heading", { name: "Claude Code TUI" }),
    ).toBeVisible();
    expect(
      screen.queryByRole("heading", { name: "Codex Desktop" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "打开" })).toBeEnabled();
    expect(screen.getByText(/原生 TUI 审批、补充和中断/)).toBeVisible();
  });
  it("Claude 等待审批时显示原生 TUI 回应提示", async () => {
    mockFetch((path) =>
      path.includes("/context")
        ? emptyHistory()
        : {
            ...collaboration,
            provider: "claude",
            nativeWaiting: "permission",
            busy: true,
          },
    );
    render(<Detail id="c1" />);
    expect(
      await screen.findByRole("heading", { name: "Claude Code 需要你的回应" }),
    ).toBeVisible();
    expect(
      screen.getByText("请在当前 Claude Code 客户端中查看并回应请求。"),
    ).toBeVisible();
    expect(screen.queryByText("Codex 正在执行")).not.toBeInTheDocument();
  });
});

describe("多人成员与邀请", () => {
  const members = [
    {
      id: "member-b",
      name: "Bob",
      active: true,
      online: true,
      inputRequested: true,
      joinedAt: "2026-09-18T00:00:00Z",
    },
    {
      id: "member-c",
      name: "Carol",
      active: true,
      online: true,
      inputRequested: true,
      joinedAt: "2026-09-18T00:00:01Z",
    },
  ];
  it("发起者明确交给 C，成员列表保留 B，已有人加入仍可继续邀请", async () => {
    mockFetch((path) =>
      path.includes("/context")
        ? emptyHistory()
        : {
            ...collaboration,
            role: "owner",
            selfId: "owner",
            members,
            participantJoined: true,
          },
    );
    const user = userEvent.setup();
    render(<Detail id="c1" />);
    await user.click(
      await screen.findByRole("button", { name: "将输入交给Carol" }),
    );
    expect(
      calls.some(
        (call) =>
          call.body?.action === "handoff" &&
          call.body.memberId === "member-c" &&
          call.body.epoch === 1,
      ),
    ).toBe(true);
    expect(screen.getByText("Bob")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "邀请成员" }));
    await user.click(screen.getByRole("button", { name: "重新开放链接" }));
    expect(
      calls.some(
        (call) => call.path.endsWith("/invitations") && !!call.body.requestId,
      ),
    ).toBe(true);
  });
  it("C 正在输入时 B 仍可申请输入，不能因同为受邀者而直接操作", async () => {
    mockFetch((path) =>
      path.includes("/context")
        ? emptyHistory()
        : { ...collaboration, selfId: "member-b", writer: "member-c", members },
    );
    const user = userEvent.setup();
    render(<Detail id="c1" />);
    await user.click(await screen.findByRole("button", { name: "申请输入" }));
    expect(calls.some((call) => call.body?.action === "request_input")).toBe(
      true,
    );
    expect(
      screen.queryByRole("button", { name: "打开 Codex" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "交还输入" }),
    ).not.toBeInTheDocument();
  });
});
