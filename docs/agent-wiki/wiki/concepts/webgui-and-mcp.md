# WebGUI 与本地 MCP

WebGUI 是协作管理界面；完整对话和执行交互在 Codex 客户端。MCP 让普通本地会话访问已发起或加入的协作。先核对 [产品流程](../../sources/product-flows.md) 与 [协议](../../sources/protocol.md)。

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

主要动作随连接、输入交接、运行和审批状态改变。批注可独立保存，读取或批注不隐式开始模型轮次。页面展示轻量历史，完整历史交给 Codex；MCP 可继续分页读取。

MCP 一次性配置连接本机 Core，后续通过工具定位协作。工具写入走现有 RPC 与输入协调，不另建绕过控制的发送通道；stdout 只输出协议消息。

浅色为主要设计基准，支持系统/浅色/深色主题。检查空状态、失败、长路径、小窗口、焦点和键盘；更新 Web 源码后提交重建的嵌入式资源。

## 检查入口

[前端路径测试](../../../../packages/web/src/test/flows.test.tsx) · [MCP 测试](../../../../internal/mcp/server_test.go) · [浏览器与工程门槛](../../sources/validation/test-gates.md)

相关任务：[产品模型](product-model-and-glossary.md) · [输入与共享](input-and-sharing.md) · [原生客户端](native-clients-and-models.md)
