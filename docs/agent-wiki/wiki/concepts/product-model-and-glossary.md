# 产品模型与词汇

Team Cross 从已有 Codex 来源会话创建新的协作 fork，在 A 的执行目录中继续；B 可直接操作，也可用自己的本地 Codex 通过工具辅助。两种入口都支持 TUI 和 Desktop。

## 先读

[项目简报](../../sources/project-brief.md) · [完整词汇](../../sources/product-core-and-glossary.md) · [用户流程](../../sources/product-flows.md)

## 修改时守住的边界

- Team Cross 协作 `id`、来源 `sourceId`、协作原生 `sessionId` 分别表示不同对象。
- 原生协议里的 Thread/Turn 不代表恢复旧产品的 Thread/Round/managed Run。
- “创建新会话”和“使用哪个目录”分别处理；原目录模式也必须创建新的会话 ID。
- 发起和结束不要求问题描述、摘要、独立 Evidence 或成果验收；批注不自动注入模型。
- 输入者是协作控制状态，不自动添加到 Provider prompt。
- 邀请只限制首次加入；加入资格、当前在线和输入归属分别管理，关闭客户端或断线不撤销成员。
- 对话与执行留在 Codex；WebGUI 提供协作管理、必要上下文和反馈。

## 代码与检查

类型从 [types.go](../../../../internal/collab/types.go) 开始，界面入口在 [App.tsx](../../../../packages/web/src/App.tsx) 和 [页面组件](../../../../packages/web/src/components/)。命名变更应同时检查来源、API 字段、界面文案与 [产品路径测试](../../../../packages/web/src/test/flows.test.tsx)。

相关任务：[目录与生命周期](workspace-and-lifecycle.md) · [WebGUI 与 MCP](webgui-and-mcp.md) · [验证](validation-gates.md)
