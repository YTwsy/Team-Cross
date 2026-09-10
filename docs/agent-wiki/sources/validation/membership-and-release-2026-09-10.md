# 邀请加入资格与原生会话释放验收 · 2026-09-10

## 范围与环境

验证一小时邀请期限只约束首次加入，以及结束共享后空闲运行时释放。测试对象为 `389608e7b54891f83e483abbcc669aa2c35e6e56` 上的本地未提交修复，包含当前首次体验与上下文刷新改动；不是已发布安装包。

环境：macOS 14.8.5（23J423），Apple Silicon，Go 1.25.3，Codex CLI 0.153.1，Codex Desktop 26.901.31953。真实模型仅使用 `gpt-5.6-luna`，测试 home、仓库、Core 与客户端配置位于 `/private/tmp` 的专用目录；未对用户已有会话发送测试输入。

产品规则见 [输入决策](../decisions/input-and-sharing.md)、[生命周期决策](../decisions/workspace-and-lifecycle.md) 与 [协议](../protocol.md)。

## 工程与并发

通过：`go test ./...`、`go vet ./...`、协作/MCP/分享/原生进程/服务的相关 race test、Web check/test/build、Go 二进制构建和 `git diff --check`。Web 共 20 项测试，包含已有的 5 项上下文刷新测试；已重建 `internal/webassets/dist`。

新增回归入口：

- [membership_test.go](../../../../internal/sharing/membership_test.go)：未使用邀请到期拒绝首次加入；已加入者跨期限仍可访问；邀请码不能读取上下文或接纳另一人；相同加入幂等；主动离开与结束撤销访问。
- [lifecycle_test.go](../../../../internal/collab/lifecycle_test.go)：参与者跨期限重连与 B Core 重启；丢失加入响应后的只读恢复；接回输入后的旧连接拒绝写入；执行、审批、已接收 RPC、直接客户端与恢复过程对关闭的阻挡；旧进程通知隔离；释放后只读与同 ID 恢复。
- [flows.test.tsx](../../../../packages/web/src/test/flows.test.tsx)：已加入但离线的成员展示、过期未使用邀请重新生成、释放后读取上下文与显式恢复。

到期测试通过移动测试期限完成，不声称已等待一小时或完成真实 Mac 睡眠/唤醒验收。

## 真实原生进程

[TestLiveCodex](../../../../internal/collab/live_test.go) 在独立 home 和仓库完成 Luna 来源轮，以及原目录和 worktree 的文件写入证明。初次在嵌套工具沙箱运行时遇到测试写入权限请求；主机环境重新运行后通过，产品权限配置没有为测试放宽。

[真实生命周期检查](../../../../internal/collab/live_lifecycle_test.go) 分别确认两种目录模式：

1. 协作加载期间，独立 app-server 无法取得该原生会话的写入权。
2. 结束共享、无直接客户端且空闲后，后台进程退出；远端状态变为 `ended`。
3. 读取保留历史后，独立 app-server 仍能成功 `thread/resume`，证明只读操作没有重新占用原生写入锁。
4. 关闭独立进程后，Team Cross 恢复同一个 `sessionId`，模型仍为 Luna；没有新 fork 或新 worktree。

`TestLiveNativeHandoff` 复用该专用 fixture，不发送新 prompt。真实同机 A/B Core 通过 TLS 和本机 WebSocket 代理完成 B 接入、A 接回后接入，两次 `thread/resume` 均返回同一会话 ID 且 `canAcceptDirectInput=true`，最后空闲释放。该结果证明协议与原生进程路径，不代替 TUI/Desktop 窗口交互。

可复现命令与环境变量见 [验证门槛](test-gates.md)。最终测试 fixture 保留于 `/private/tmp/teamcross-membership-final-live-20260910`。

## Desktop 与浏览器

实际启动隔离的专用 Desktop，网关确认 `client=Codex Desktop`、`connected=true`、`clientState=connected`。结束共享后，A 保持 `online=true`、`releasePending=true`，B 立即进入 `ended`。核对测试 Desktop 的 PID 和独立应用数据目录后发送 SIGTERM，A 随后进入 `released`、`connected=false`、`online=false`，原生会话 ID 与历史保留。

电脑控制工具禁止自动操作 `com.openai.codex`。本轮未在 Desktop 窗口内点击任务、发送输入、回应审批或使用退出菜单，不能把进程连接状态写成 `session_ready`，也不能将协议回归扩大为完整 Desktop 验收。

真实浏览器使用两个本机测试 Core：预览邀请、确认加入、读取真实原生历史、成员持续有效提示、结束确认、B 结束状态、A 等待客户端关闭与释放状态均复核。检查 1440×1000、1024×900、768×1000 CSS 视口及深浅主题，文案与主要动作可见，页面 `scrollWidth` 不超过视口；浏览器未记录 error/warning。截图实际查看，保存在 `/private/tmp/teamcross-membership-fix-20260910`，包含 `joined-1440-light.png`、`joined-1024-dark.png`、`joined-768-dark.png`、`owner-released-1440-light.png`、`guest-ended-768-dark.png`。

## 交付边界

本轮是同机验证，未覆盖两台 Mac 的真实 LAN、真实睡眠后重连或完整客户端矩阵。新邀请能力为 `codex-collaboration-v2-membership`；旧邀请和旧成员凭据不能用于新协议，更新后双方需要使用新版并重新邀请。

本轮启动的测试 Core、专用 Desktop、原生 app-server、测试浏览器及其辅助进程在验证后按 PID/数据目录核对关闭。测试 fixture 和截图保留供复核，用户的原有 Codex、协作与工作目录未用于测试清理。
