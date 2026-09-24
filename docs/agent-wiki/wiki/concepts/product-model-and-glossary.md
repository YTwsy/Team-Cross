# 产品模型与词汇

Team Cross 空间保存成员、已发布会话材料与讨论；可独立只读分享，也可关联一个新的原生 fork 在 A 的目录中共同执行。各人的个人 Agent 按需读取材料并参与讨论。

设计从已有 Session 和会话记录入手，让人的经验和判断能及时加入正在进行的工作，并保留各自的工具。个人资源库也让当前 Agent 按选择接上此前分享或参与的上下文；本机托管、连接方式与设计原因见 [项目简报](../../sources/project-brief.md#设计出发点)。

空间及三人以上流程先读 [协作空间任务页](collaboration-spaces.md) 和 [完整契约](../../sources/decisions/collaboration-spaces.md)；不要把只读材料与可操作 fork 混为同一个对象。

## 先读

[项目简报](../../sources/project-brief.md) · [完整词汇](../../sources/product-core-and-glossary.md) · [用户流程](../../sources/product-flows.md)

项目许可证为 [GPL-3.0-only](../../../../LICENSE)，适用范围见 [项目简报](../../sources/project-brief.md#开源协议)。

## 修改时守住的边界

- Team Cross 协作 `id`、来源 `sourceId`、协作原生 `sessionId` 分别表示不同对象。
- 只读发布以连续完整轮次为分享范围；目录点击与建议阅读起点均不改变范围。编辑私有草稿与确认范围预览分别读取，细节见 [分享范围与确认](../../sources/product-flows.md#分享范围与确认)。
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
