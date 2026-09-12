# 2026-09-13 批注读取、内嵌编辑与回复验证

本轮覆盖共享原生运行时的批注工具、WebGUI 内嵌编辑和单层回复。环境为 macOS 14.8.5 / arm64、Go 1.25.3、Node 24.18.0、pnpm 11.19.0。使用当前工作区构建的 CLI 与嵌入式 production Web 资源；不代表已安装 App 或后续版本已通过。

## 工程检查

- 全部实际 Go 源码包的 test 与 vet 通过。仓库已有 `bin/go-mod` 使 `go test ./...` 扫描到主模块外的依赖缓存；保留该目录，使用 `./cmd/... ./internal/...` 覆盖源码。
- race test 覆盖 `internal/collab`、`internal/mcp`、`internal/sharing`、`internal/nativecodex`、`internal/nativeclaude`、`internal/service`、`internal/cliinstall`，全部通过。
- Web 类型检查、38 项测试和 production build 通过；9 项批注测试覆盖引用、草稿、单层回复、中文输入法快捷键和失败重试。
- 已更新 `internal/webassets/dist`，当前 CLI 构建通过。文档链接、源码入口与 `git diff --check` 通过。

[回复回归](../../../../internal/collab/annotation_reply_test.go) 覆盖持久化、主机生成作者、拒绝伪造回复、拒绝回复 ID/其他协作作为根、请求去重、并发读取快照、保存失败回滚、成员无需模型输入权即可回复及结束后的访问撤销。

[运行时 STDIO 回归](../../../../internal/mcp/runtime_test.go) 与 [协作回归](../../../../internal/collab/annotation_reply_test.go) 覆盖仅有两个工具、拒绝额外参数和其他操作、协作凭据与管理凭据分离、不启动 Core、跨协作拒绝、Provider 作者标识及明确恢复或重新共享后重新授权。原生客户端空闲释放调度与批注访问分开判断，避免发起者恢复后被误拒绝。

## 实际原生客户端

使用 [verify-annotations.py](../../../../scripts/verify-annotations.py)，每轮建立空测试仓库、专用原生来源和同机两个真实 Core，邀请 B 后交接输入。实际模型仅为 `gpt-5.6-luna`；Claude 请求由 loopback guard 逐次检查。没有使用普通用户会话。

| 入口 | 版本与实际结果 |
| --- | --- |
| 直接 Codex TUI | CLI 0.153.4；B 读取批注及人工回复，返回其中未在 prompt 中提供的两个标记，并通过 `reply_to_annotation` 保存为 Codex 回复；结束共享、恢复同一 session 后再次读取成功 |
| 直接 Claude Code TUI | CLI 2.1.268；相同读取、回复与原 session 恢复流程通过，作者为 Claude Code；首次读取与回复的两个原生 MCP 确认在实际 TUI 中逐项接受 |
| 专用 Codex Desktop | 隔离 app-data 的真实进程已启动，B 网关报告 `connected=true`；未取得 `session_ready` 或窗口内调用批注工具的证据，不能记为完整 Desktop 通过 |

Codex 回复测试接受的是该批注工具、该请求 ID 的原生 elicitation，通过 B 的控制 API 返回 `accept`。这证明审批转发后的工具执行，不代表 Codex 原生审批弹窗已做 UI 验收。测试脚本在原生 TUI 回显完整草稿后才按 Enter；只确认网关 `session_ready` 不足以证明编辑框已准备好提交。

Desktop 窗口复核受环境限制：Computer Use 明确禁止操作 `com.openai.codex`，因此未尝试绕过限制。已按专用 app-data 路径核对并关闭本次 Desktop 主进程和 9 个辅助进程，复查无残留。两个 TUI 的结果不能代替 Desktop 窗口验收。

最终 Codex 通过轮次位于 `/private/tmp/teamcross-annotations-codex-20260913e`，Claude 位于 `/private/tmp/teamcross-annotations-claude-20260913b`；`evidence/summary.json`、批注结果、历史及 PTY 记录保留在各自专用目录。Desktop 与 Core 重启证据位于 Codex 的 `20260913b` fixture。已关闭本次测试 TUI、Core、app-server、Desktop 和浏览器；保留测试会话与仓库供复核。

## 配置隔离与恢复

实际 CLI 验证了 Codex MCP 表的跨层合并行为。继承服务必须逐项设为 `enabled=false`，不能以新表替换为前提；包含点号的服务名放在 TOML 表内引用，避免命令行路径拆分产生错误条目。同名个人 HTTP 服务保持禁用，批注 STDIO 使用未占用的后缀名称。

最终真实 Codex 用例特意保留一个启用的个人测试 MCP 配置：运行时状态仍列出该名称，但没有握手信息和工具；唯一完成握手并暴露工具的是批注服务。个人配置文件不变。

已在同一测试协作上重启 Core，再使用原协作凭据和 STDIO 启动参数读取已保存的两条批注，证明地址重读路径可用。结束共享后的工具访问由独立访问状态关闭，明确恢复后才重新开放。

本版本创建的 Claude worker 恢复时沿用原生保存的 MCP 启动配置。此前没有批注工具的旧 worker 不改写原生 job 状态，需要新建协作；不将这一限制包装为透明升级。

## 实际浏览器

使用真实 Core 和真实已保存批注，先在内置浏览器验证交互，再使用独立 Chrome 152.0.7977.83 / Playwright 生成最终截图。

- 点击代码行的 `+` 后，引用位置与原文直接进入页面内编辑框，焦点为 `annotation-text`，页面没有批注编辑 dialog。
- `⌘ + Enter` 实际保存了带文件引用的批注；展开回复并保存后，原批注数仍为 2，回复显示在对应原批注下。
- 引用意见与整体意见分别保留草稿；收起再展开回复保留草稿；刷新上下文后两类正文仍在。浏览器整页刷新或离开页面不承诺保留。
- 1440、1024、768 CSS 像素及浅色、深色共 6 个组合，核对实际视口、无文档水平溢出和无编辑弹窗，实际查看全部截图；原文卡片、长中文正文、回复层级与保存按钮可读可达。截图关闭动画，避免捕获主题切换的过渡色。
- 独立浏览器控制台无错误和警告。保存失败、相同请求重试与 IME 不误提交由前端自动化回归覆盖。

最终截图和本次复核材料位于工作区 `output/playwright/annotations-2026-09-13/`，不属于发布包。内置浏览器截图缩放失真后已用独立 Chrome 截图替换；没有将失真截图用于布局通过结论。

## 能力边界

结果属于同机 A/B 验证，不是两台 Mac LAN。没有验证其他模型或 Desktop 窗口内实际工具执行，也没有升级用户已安装的 App。保存意见和回复不会自动启动或补充模型轮次；用户在直接客户端要求处理批注后，Agent 才读取当前协作的批注工具。

字段、范围与生命周期见 [协议](../protocol.md#批注回复)，用户操作见 [产品流程](../product-flows.md#上下文阅读)。
