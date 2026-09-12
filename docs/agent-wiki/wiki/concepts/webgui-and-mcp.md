# WebGUI 与本地 MCP

WebGUI 是协作管理界面；完整对话和执行交互在对应原生客户端。MCP 让普通本地会话访问已发起或加入的协作。先核对 [产品流程](../../sources/product-flows.md) 与 [协议](../../sources/protocol.md)。

## 页面与工具入口

| 任务 | 代码入口 |
| --- | --- |
| 首页、创建、详情、加入、设置 | [components](../../../../packages/web/src/components/)、[App.tsx](../../../../packages/web/src/App.tsx) |
| API 与状态类型 | [api.ts](../../../../packages/web/src/api.ts)、[types.ts](../../../../packages/web/src/types.ts) |
| 视觉、键盘与通用组件 | [styles.css](../../../../packages/web/src/styles.css)、[ui.tsx](../../../../packages/web/src/components/ui.tsx) |
| 本机管理与客户端启动 | [http.go](../../../../internal/collab/http.go)、[launch.go](../../../../internal/collab/launch.go) |
| STDIO 工具与协议输入 | [mcp/server.go](../../../../internal/mcp/server.go) |

## 修改时守住的边界

创建用两张卡解释原目录和干净 worktree，不加入未跟踪文件选择器。执行主机、目录、输入归属与下一步动作保持清楚；协议 ID、版本和地址按需展开。

主要动作随连接、输入交接、运行和审批状态改变。批注可独立保存，读取或批注不隐式开始模型轮次。页面展示轻量历史，完整历史交给原生客户端；MCP 可继续分页读取。

批注从对话文字或代码行发起，自动携带原文与结构化定位，编辑失败保留当前页草稿；点击批注可查找原文，变化后展示引用片段而不猜测新位置。页面内编辑取代弹窗，原文和整体意见分别保留当前页草稿；原批注下可展开单层回复，刷新期间保留正在填写的内容。个人 MCP 通过 `read_context kind=annotations` 读取，并可 `reply_to_annotation`。共享运行时自动提供当前协作的 `read_annotations` 和 `reply_to_annotation`，直接客户端可以在原会话内处理讨论。字段与快照边界以 [协议](../../sources/protocol.md#批注引用) 为准；交互实现见 [Context.tsx](../../../../packages/web/src/components/Context.tsx) / [Annotations.tsx](../../../../packages/web/src/components/Annotations.tsx)，回归见 [批注测试](../../../../packages/web/src/test/annotations.test.tsx)。

同一上下文刷新时保留内容与阅读位置，首次加载或切换资源才显示加载占位；具体行为见 [上下文阅读](../../sources/product-flows.md#上下文阅读)，回归入口见 [上下文刷新测试](../../../../packages/web/src/test/context-refresh.test.tsx)。

个人 Codex / Claude Code 的 MCP 一次性配置连接本机 Core，后续通过工具定位协作。工具写入走现有 RPC 与输入协调，不另建绕过控制的发送通道；stdout 只输出协议消息。

浅色为主要设计基准，支持系统/浅色/深色主题。检查空状态、失败、长路径、小窗口、焦点和键盘；更新 Web 源码后提交重建的嵌入式资源。

## 检查入口

[前端路径测试](../../../../packages/web/src/test/flows.test.tsx) · [MCP 测试](../../../../internal/mcp/server_test.go) · [浏览器与工程门槛](../../sources/validation/test-gates.md)

相关任务：[产品模型](product-model-and-glossary.md) · [输入与共享](input-and-sharing.md) · [原生客户端](native-clients-and-models.md)

首次流程支持创建并邀请、App 邀请确认后进入上下文，以及 MCP 配置/协议探测/实际调用分别展示。完整规则见 [分发与首次体验](../../sources/distribution-and-onboarding.md)。

个人客户端选择、配置覆盖检测与独立调用证据的完整规则见 [Claude 辅助模式](../../sources/decisions/claude-native-tui.md#个人-claude-code-辅助模式)。辅助客户端的 Provider 不改变目标协作能力；共享 Claude worker 仍通过直接 TUI 处理审批、中断与补充。
