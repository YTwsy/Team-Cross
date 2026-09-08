# 实验分支验收记录

验证日期：2026-09-09。对象：`codex/session-collaboration` 的新 Go Core、WebGUI、原生网关与 MCP。

本记录把工程检查、浏览器检查、真实客户端检查分开。所有真实模型调用均使用 `gpt-5.6-luna`；A/B 运行在同一台 Mac 的独立服务与配置目录中。没有第二台 Mac 的实际 LAN 结果，不将同机 TLS 连接等同于跨设备验收。

## 环境与保存范围

- Codex CLI `0.153.1`，Desktop `26.901.31953`，Go `1.25.3`，Node `24.18`。
- 独立实验工作目录：原仓库的 `.worktrees/session-collaboration`。
- 原工作区快照：`.local-backups/before-session-collaboration-20260909-002633`，保留 Git HEAD、暂存与未暂存 patch、状态及工作文件副本。
- 真实客户端使用专门测试仓库、原生来源会话和测试数据目录。登录材料仅在本机使用，不写入仓库和验收文档。
- Desktop 中的打开历史与直接发送由用户实际操作；Core 的状态、原生事件和目标文件由测试端核对。

## 八种客户端组合

| 执行目录模式 | 入口 | 实际结果 |
| --- | --- | --- |
| 原目录 | 直接 TUI | B 的 TUI 经本机代理连接 A；读取 `baseline.txt` 为 `unstaged`，读取 Desktop 创建的证明文件，执行目录为 A 的 `repo/src` |
| 新 worktree | 直接 TUI | 进入同一 worktree fork，读到 `baseline.txt` 为 `committed` 与 `TEAMCROSS_WORKTREE_OK` |
| 原目录 | 直接 Desktop | 用户打开历史并发送文件操作；A 的 `repo/src/desktop-proof.txt` 为 `TEAMCROSS_DESKTOP_OK`，来源未提交现场可见 |
| 新 worktree | 直接 Desktop | B 专用 Desktop 经本机代理连接 A；A 的 worktree 中 `desktop-worktree-proof.txt` 为 `TEAMCROSS_DESKTOP_WORKTREE_OK`，baseline 为 `committed` |
| 原目录 | 辅助 TUI | 普通本地会话通过 MCP 列出协作、读取 baseline 与结果；通过工具向共享原生会话发送输入 |
| 新 worktree | 辅助 TUI | 普通本地会话读取 worktree 上下文，并经 B 的 MCP 向已加入协作发送输入、读回 `TEAMCROSS_REMOTE_MCP_OK` |
| 原目录 | 辅助 Desktop | 用户在独立本地会话使用 MCP，读到 A 原目录、`unstaged` baseline，并读回共享会话的 `TEAMCROSS_ASSIST_DESKTOP_OK` |
| 新 worktree | 辅助 Desktop | 同一辅助会话读取 A 的 worktree、`committed` baseline，并读回另一共享会话的 `TEAMCROSS_ASSIST_DESKTOP_OK` |

辅助 Desktop 测试期间，原目录的直接 TUI 与 worktree 的直接 Desktop 保持连接。两个共享会话都完成了工具发送的轮次，验证了独立本地上下文与直接客户端可以并行参与。

测试来源为“Team Cross · 客户端协作验证”。初始两个 fork 的 ID 均不同于来源，也互不相同；原目录和 worktree 的相对 `src` 子目录映射均正确。恢复已有协作继续保存的 fork，不产生新的会话或目录。

## 目录、输入与生命周期

| 检查 | 证据与结论 |
| --- | --- |
| 创建保留原目录 | 工作区测试比较 HEAD、分支、暂存 diff、未暂存 diff 与文件；原目录模式创建前后保持一致 |
| 干净 worktree | staged、unstaged、untracked、ignored 四类测试内容均未复制；只出现已确认提交的内容 |
| 无隐式业务指令 | 浏览、预览、创建的协议测试未调用 `turn/*`；真实原生 fork 后由显式测试输入触发执行 |
| 输入协调 | 直接连接与 MCP 共享输入归属；拒绝无归属写入、重复开始轮次和已失效 epoch；相同请求 ID 返回已有响应 |
| 原生审批 | `item/commandExecution/requestApproval` 在 B 的 Desktop 显示，MCP `respond_to_request` 允许一次；A 返回 `serverRequest/resolved`，`printf` 退出码 0，输出 `TEAMCROSS_APPROVAL_OK`，待审批数归零 |
| 客户端切换 | TUI 与专用 Desktop 均进入指定 fork；一个直接客户端连接规则通过集成测试验证 |
| 结束共享 | 原生连接关闭、工具访问被撤销；在线 B 收到结束状态。会话与代码保留。接收端重启保留已观察到的结束状态 |
| 邀请失效 | 服务拒绝到期/撤销凭据；界面展示原因和获取新邀请的入口，保留用户输入供修正 |
| 恢复与重连 | 重启 Core 后显式恢复原生运行时，保持同一 fork ID、目录和 Git 分支；不会自动重放不明写入 |
| 客户端账户分流 | 本机账户读取、登录请求及完成通知通过独立路由；测试确认 B 的账户操作不会发送到 A，A 的 token 与个人配置不会通过共享网关返回 |

## 浏览器与视觉检查

通过实际浏览器操作新首页、来源选择、两张目录卡、确认起点、创建、详情、批注、邀请、B 端加入、客户端入口和 MCP 设置。浏览器创建了一次真实 worktree 协作并保存批注。

首页、创建、详情、加入、加入后的入口选择和设置分别在 **1440、1024、768 CSS 像素**及浅色、深色下查看截图。以页面实际 `innerWidth` 核对视口，检查水平溢出；长执行路径可换行，主要操作在窄窗口可达。截图保存在本地 `output/playwright/`，不作为运行时资源提交。

键盘验证包含来源单选的 Space 操作、下一步 Enter、弹窗 Escape、关闭弹窗后的触发按钮焦点恢复。另检查首次空列表、无效邀请、断线及结束共享状态。结束共享页面明确显示关闭原因与“使用新邀请加入”，不会持续提示等待连接。

## 工程门槛

```sh
go test ./...
go vet ./...
go test -race ./internal/collab ./internal/mcp ./internal/sharing
pnpm --filter @teamcross/web check
pnpm --filter @teamcross/web test
pnpm --filter @teamcross/web build
go build -o bin/teamcross ./cmd/teamcross
```

全部门槛通过。前端包含 6 项产品路径测试，包括结束/到期状态回归；生产资源已更新到 `internal/webassets/dist`。原生真实模型用例默认跳过，通过以下显式入口在专用目录运行，并已完成一次成功验收：

```sh
TEAMCROSS_LIVE_DIR=/private/tmp/teamcross-fresh-fixture \
  go test ./internal/collab -run '^TestLiveCodex$' -v -count=1 -timeout=7m
```

该目录应为新建测试位置；真实模型测试会建立 Git 仓库、来源会话、两个 fork 并执行专门的证明文件操作。常规测试不消耗模型调用。

## 仍需独立验收

| 范围 | 当前边界与下一步 |
| --- | --- |
| 两台 Mac 的 LAN | 尚未执行。A/B 分别启动 Core，不使用 `--test-loopback`；交换邀请，完成上述八种入口、审批、断线重连与结束访问，核对执行目录始终在 A |
| 跨设备网络条件 | 单独记录实际 IP、网络接口、系统防火墙与主机休眠恢复。当前没有 Tailnet/Tailcat 验收，也不包含这些传输实现 |
| Desktop 版本兼容 | 指定 WebSocket 启动为当前本机版本的实验入口；升级后重做登录、历史、直接输入、审批与 A 上执行的检查 |
| Desktop 全局功能 | 本轮覆盖协作会话与本地 MCP；不承诺专用实例的语音、Marketplace、云任务或其他全局菜单功能 |
| 模型选择 | 实验共享运行时固定为 Luna。开放其他模型前，应把实际运行时与客户端显示、能力差异一起验收 |

测试结束后关闭本次启动的专用 Desktop、TUI、Core、app-server 和测试浏览器页面；测试仓库、会话和截图保留，普通 Codex 与用户其他浏览器内容保留。
