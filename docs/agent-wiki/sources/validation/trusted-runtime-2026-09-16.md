# 创建时固定的信任模式验证 · 2026-09-16

## 范围与环境

功能分支为 `codex/feature/trusted-runtime`，起点 `d3004d2`。产品选择与实现边界见 [协作模式决策](../decisions/runtime-modes.md)。本次覆盖 Codex 与实验性 Claude Code 的 `restricted` / `trusted` 创建、邀请、原生 TUI、个人工具继承及同会话恢复，不提供运行中切换。

实际环境为 macOS 14.8.5 / arm64、Go 1.27.1、Node 24.18.0、pnpm 11.19.0、Codex CLI `0.154.0-alpha.6.2`、Claude Code `2.1.270`。真实模型均使用专用仓库和测试 home，模型限定 `gpt-5.6-luna`。Claude 通过用户启动的本机模型路由及测试 loopback guard，逐个检查模型名称。

## 工程检查

以下检查全部通过：

- `go test ./...`
- `go vet ./...`
- `go test -race ./internal/collab ./internal/mcp ./internal/sharing ./internal/nativecodex ./internal/nativeclaude ./internal/service ./internal/cliinstall`
- Web check、53 项测试、production build；已更新 `internal/webassets/dist`
- `go build -o bin/teamcross ./cmd/teamcross`
- 文档相对链接与 `git diff --check`

Go 回归覆盖默认受限、非法模式、预览绑定、创建重试不可变、持久化和邀请模式、原生权限写入的输入归属、配置过滤、hook 信任确认及同 ID 恢复。Claude 另覆盖默认与显式配置目录、继承环境、精确 job 所有权、worker 退出等待以及保留个人 daemon。Web 回归覆盖两种 Provider 的信任模式创建与重新预览，详情和邀请展示固定模式。

## 真实原生客户端

使用 [verify-annotations.py](../../../../scripts/verify-annotations.py)，为两种 Provider 各执行一次 `--runtime-mode trusted` 和一次默认受限模式。四轮均返回 `annotation_acceptance_verified`，退出码为 0，无 cleanup error。

| 检查 | Codex 信任 | Claude 信任 | 默认受限回归 |
| --- | --- | --- | --- |
| 共享 fork 的直接 TUI 连接 | 通过 | 通过 | 两种 Provider 均通过 |
| 个人 MCP 实际调用与专用证明文件 | 通过 | 通过 | Codex 确认个人 MCP 禁用 |
| 个人 hook 实际执行 | 通过 | 通过 | 保持原有受限启动配置 |
| 原生确认后读取与回复批注 | 通过，1 次批注审批 | 通过，2 次工具审批 | 两种 Provider 均通过 |
| 结束并恢复同一个 session，再读取先前回复 | 通过 | 通过 | 两种 Provider 均通过 |

Codex 信任模式实际激活了 `personal_fixture`、`teamcross_annotations` 和原生提供的 `codex_apps`。只调用专用测试 MCP 与批注工具。测试来源由 `codex exec` 建立，原生保存的审批策略为 `never`；验收通过当前输入者的原生设置明确选择测试 permission profile 与 `on-request`，验证网络配置及审批继承，产品没有强制此策略或全局跳过权限。原生 hook 审阅在 TUI 完成后，确认信任状态保存到 A 的配置。

真实验证发现并修复两项协议/生命周期问题：

1. Codex 的 hook 确认须转发到 A，不能当作 B 的一般偏好写入；hooks/skills 响应还需保留客户端 cwd 查找键，避免远端 TUI 等待发现结果。
2. Claude 的 `stop` 返回后，daemon 可暂时报告 `alive=false,present=true`。此时立即 resume 会创建副本。现在等待精确 worker 同时不再 alive、不再 present，实测恢复后的 session ID 保持一致。

测试 hook 只向 fixture 写入标记；MCP helper 仅返回固定值并记录调用证明，未操作用户的外部服务。某个继承插件因缺少 OAuth 未能启动，保留原生授权要求，没有自动登录或调用该插件；因此不把配置继承扩大为所有第三方插件已经逐一验收。

## 浏览器与截图

Chrome 中使用真实测试 Core，完成来源选择、默认受限、切换为信任后重新预览、实际创建、邀请确认，以及两种 Provider 已完成轮次的详情读取。创建请求的主机记录为 `trusted`；详情和邀请页均展示“创建后固定”，没有模式切换控件。邀请确认截图不展示邀请码或 secret。

首页、创建、两种 Provider 详情、加入、设置和无效邀请状态均在 1440 / 1024 / 768 CSS 像素、浅色 / 深色下检查，`scrollWidth` 等于视口宽度；查看实际截图并核对长路径、换行、滚动和主要按钮。创建页使用键盘/表单输入完成名称与选择。两种详情及有效邀请页面未出现 JavaScript `pageerror`。

截图保存在本地忽略目录 `output/playwright/trusted-runtime/`。不把预期的 API 失败算作 JavaScript 错误：Claude fixture 刻意未配置 Codex；无效邀请返回错误；新建的零输入 Claude fork 在原生历史尚未落盘时，WebGUI 历史暂不可读。后者是已有原生持久化边界，本轮没有增加伪造历史或为了生成历史自动发送 prompt；完成首轮后的历史、批注与恢复读取已实测。

## 证据与收尾

本地四轮独立 fixture 为：

- `/private/tmp/teamcross-trusted-codex-20260916-h`
- `/private/tmp/teamcross-trusted-claude-20260916-h`
- `/private/tmp/teamcross-restricted-codex-20260916-final`
- `/private/tmp/teamcross-restricted-claude-20260916-final`

各目录的 `evidence/summary.json` 保存最终结果，Claude 的 `models_observed` 仅包含 `gpt-5.6-luna`。浏览器、测试 TUI、Core、app-server、Claude fixture daemon 与模型 guard 已退出；按本次 fixture 路径复查没有残留测试进程。测试目录与截图保留，用户已有 Core、个人客户端及本机模型路由未关闭。

## 结论边界

这是同一台 Mac 上两个真实 Core 的结果，不代表两台 Mac LAN 或 Tailcat 跨网验收。本轮没有完成专用 Codex Desktop 窗口内操作、实际浏览器/电脑控制工具、macOS 授权弹窗、组织策略或 Claude OAuth/Keychain 的专项验证。继承的是主机可解析的原生配置及可用环境，不是来源进程全部临时参数、环境变量与 Desktop 私有服务的精确快照。Claude 零输入 fork 的持久化与恢复限制仍遵循 [Claude 接入契约](../decisions/claude-native-tui.md)。
