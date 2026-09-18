# 产品模型与词汇

Team Cross 从已有 Codex 来源会话创建新的协作 fork，在 A 的执行目录中继续；B 可直接操作，也可用自己的本地 Codex 通过工具辅助。两种入口都支持 TUI 和 Desktop。

上文是当前实现。下一阶段已确认独立只读分享与首版三人及以上参与，开始该改造先读 [协作空间任务页](collaboration-spaces.md) 和 [完整契约](../../sources/decisions/collaboration-spaces.md)；不要把只读材料与可操作 fork 混为同一个对象。

## 先读

[项目简报](../../sources/project-brief.md) · [完整词汇](../../sources/product-core-and-glossary.md) · [用户流程](../../sources/product-flows.md)

## 修改时守住的边界

- Team Cross 协作 `id`、来源 `sourceId`、协作原生 `sessionId` 分别表示不同对象。
- 当前 Session 分享请求的 `requestId` 同时是拟创建的协作 `id`；登记不代表 fork 或邀请已经完成。核对原生调用身份后等待固定本轮结束，来源变化或重启不自动重放。
- 原生协议里的 Thread/Turn 不代表恢复旧产品的 Thread/Round/managed Run。
- “创建新会话”和“使用哪个目录”分别处理；原目录模式也必须创建新的会话 ID。
- 受限模式的 Claude fork 由原生命令在隔离 runtime 中创建，并只把这个新 transcript 发布到 A 的个人 CLI/TUI history；B 不取得其他 Provider 历史。
- 发起和结束不要求问题描述、摘要、独立 Evidence 或成果验收；批注不自动注入模型。
- 输入者是协作控制状态，不自动添加到 Provider prompt。
- 邀请绑定用户显式选择的 `lan|tailcat`，只限制首次加入；加入资格、当前在线和输入归属分别管理，关闭客户端或断线不撤销成员。
- 对话与执行留在 Codex；WebGUI 提供协作管理、必要上下文和反馈。

## 代码与检查

类型从 [types.go](../../../../internal/collab/types.go) 开始，界面入口在 [App.tsx](../../../../packages/web/src/App.tsx) 和 [页面组件](../../../../packages/web/src/components/)。命名变更应同时检查来源、API 字段、界面文案与 [产品路径测试](../../../../packages/web/src/test/flows.test.tsx)。

相关任务：[目录与生命周期](workspace-and-lifecycle.md) · [WebGUI 与 MCP](webgui-and-mcp.md) · [验证](validation-gates.md)

当前 Provider 包含 Codex 与实验性的 Claude Code 原生 TUI。两者共享协作、目录与输入归属语义；客户端和控制方法支持范围按 [Claude 接入契约](../../sources/decisions/claude-native-tui.md) 区分。
