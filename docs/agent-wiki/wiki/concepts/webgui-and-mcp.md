# WebGUI 与本地 MCP

WebGUI 提供协作管理、材料阅读与原文讨论；执行交互在对应原生客户端。MCP 让普通本地会话访问已发起或加入的协作。先核对 [产品流程](../../sources/product-flows.md) 与 [协议](../../sources/protocol.md)。

独立只读分享、多会话材料与三人以上入口见 [协作空间任务页](collaboration-spaces.md)。公开范围预览必须覆盖实际发布的工具输出和附件，页面隐藏不能代替 Core、MCP/CLI 与运行时工具共同的数据范围限制；导出缺失与截断必须显式标记。

个人 MCP 和 CLI 也支持选择来源、预览、创建、邀请、加入、直接客户端启动及结束/恢复；[管理工具](../../../../internal/mcp/management.go) 统一映射现有 Core API。个人 MCP 还可通过 [当前来源工具](../../../../internal/mcp/current.go) 核对调用者 Session、预览并登记本轮结束后的分享；无法核对身份时明确选择来源，不猜最近会话。登记后结束本轮，再查询状态和取回邀请；等待可取消，重启不自动重放。邀请失败后只重试分享，不重复 fork。共享运行时的内置批注 MCP 保持当前协作范围。

## 页面与工具入口

| 任务 | 代码入口 |
| --- | --- |
| 首页、创建、详情、加入、设置 | [components](../../../../packages/web/src/components/)、[App.tsx](../../../../packages/web/src/App.tsx) |
| API 与状态类型 | [api.ts](../../../../packages/web/src/api.ts)、[types.ts](../../../../packages/web/src/types.ts) |
| 视觉、键盘与通用组件 | [styles.css](../../../../packages/web/src/styles.css)、[ui.tsx](../../../../packages/web/src/components/ui.tsx) |
| 本机管理与客户端启动 | [http.go](../../../../internal/collab/http.go)、[launch.go](../../../../internal/collab/launch.go) |
| STDIO 工具与协议输入 | [mcp/server.go](../../../../internal/mcp/server.go) |
| 终端协作查询与输入管理 | [collaboration.go](../../../../cmd/teamcross/collaboration.go) |
| 只读入口、公开范围与固定版本阅读 | [Publisher.tsx](../../../../packages/web/src/components/Publisher.tsx)、[PublicationReader.tsx](../../../../packages/web/src/components/PublicationReader.tsx)、[ReadOnlySpace.tsx](../../../../packages/web/src/components/ReadOnlySpace.tsx)、[Materials.tsx](../../../../packages/web/src/components/Materials.tsx) |

## 修改时守住的边界

来源固定后收起形态卡片，用目录与正文选择连续整轮范围。目录只定位，建议阅读起点只导航；底部持续显示起止和轮数。确认页读取 Core 按范围生成的私有预览，正文成功加载后才能发布；返回编辑保留阅读状态。完整行为见 [分享范围与确认](../../sources/product-flows.md#分享范围与确认)，回归见 [发布交互测试](../../../../packages/web/src/test/publication.test.tsx)。

只读流程选择明确来源、冻结历史、起止范围、实际内容预览后发布。更新默认保留上一版范围；边界缺失须重新选，不能退回全部历史。材料正文只按需展开，引用绑定版本，个人和共享 Agent 通过 `list_materials/read_material` 读取；个人 MCP 还可用 `read_publication_draft` 读取私有冻结草稿，共享运行时仍保持四个工具且不枚举个人来源。批注和回复可以附多份材料。

`read_material` 首页显式请求轮次目录；默认流完整给出用户/助手消息，长工具输出只给精确前缀、总长度和读取提示。WebGUI 折叠条内的“读取完整输出”以及 Agent 的 `turnId + itemId + startOffset` 都切换到单条分页，按条游标不会溢出到其他 item；深处批注也直接定位该条，而不顺序吞下之前的日志。所有偏移继续使用 UTF-16，折叠投影不改变固定版本正文或批注位置。

发起页用两张卡片区分先分享讨论和直接一起执行，标题、形态卡片和下方表单与首页共用同一居中栏宽；来源客户端是会话列表筛选，不是与形态并列的主选择。切换形态或来源时两套表单共用同一占位，列表区不随加载塌缩。创建执行时用两张卡解释原目录和干净 worktree，不加入未跟踪文件选择器。执行主机、目录、输入归属与下一步动作保持清楚；协议 ID、版本和地址按需展开。

邀请面板在只读和执行详情中复用：复制同一链接、关闭链接加入、重置链接；成员列表负责执行访问与输入交接。只读空间始终可关闭，关闭状态持久化且可重新开放。

详情页把执行主机、目录、当前输入者和共享状态收在同一执行信息卡；共享详情按需浮层展开，不推动下方内容。右侧信息区先展示紧凑的参与成员，再展示批注；技术信息位于“协作上下文”的最后一个标签，不继续堆叠在右栏。

主要动作随连接、输入交接、运行和审批状态改变。批注可独立保存，读取或批注不隐式开始模型轮次。已发布材料和已授权上下文使用一致的消息视觉、轮对齐正文流、可读取的工具折叠与按条定位；`read_context kind=history` 用 `pageCursor + turnId + itemId + startOffset` 展开单条，Agent 无需顺序吞下整页日志。执行上下文仍按来源的 8 轮页面读取，只在内存中投影，不持久化为材料清单/blob，也没有固定版本语义；两者统一的是读取契约，不是存储生命周期。执行交互继续在原生客户端。

批注从对话文字或代码行发起，自动携带原文与结构化定位，编辑失败保留当前页草稿；点击批注可查找原文，变化后展示引用片段而不猜测新位置。页面内编辑取代弹窗，原文和整体意见分别保留当前页草稿；原批注下可展开单层回复，刷新期间保留正在填写的内容。个人 MCP 通过 `read_context kind=annotations` 读取，并可 `reply_to_annotation`。共享运行时自动提供当前协作的 `read_annotations` 和 `reply_to_annotation`，直接客户端可以在原会话内处理讨论。字段与快照边界以 [协议](../../sources/protocol.md#批注引用) 为准；交互实现见 [Context.tsx](../../../../packages/web/src/components/Context.tsx) / [Annotations.tsx](../../../../packages/web/src/components/Annotations.tsx)，回归见 [批注测试](../../../../packages/web/src/test/annotations.test.tsx)。

选区浮条打开原文旁编辑卡，窄屏在消息下方展开；编辑卡与批注面板共用草稿状态，收起和失败保留当前页草稿。会话新内容先提示，读者点击后更新；材料在当前页记住最近六个已读版本的位置。首次加载或切换资源才显示加载占位；具体行为见 [上下文阅读](../../sources/product-flows.md#上下文阅读)，回归入口见 [上下文刷新测试](../../../../packages/web/src/test/context-refresh.test.tsx)。

共用阅读实现见 [Reading.tsx](../../../../packages/web/src/components/Reading.tsx)、[MarkdownText.tsx](../../../../packages/web/src/components/MarkdownText.tsx) 与 [原文位置映射](../../../../packages/web/src/reading.ts)。改动排版时必须保留 UTF-16 原文定位；引用材料选择器默认只列元数据，预览按需读取。回归见 [阅读与引用测试](../../../../packages/web/src/test/reading.test.tsx)。

协作对话目录跳转时，已加载轮次直接滚动，尚未加载的轮次按当前 `pageCursor + turnId` 读取后定位；保留专注阅读选择，支持读取失败重试。分页、返回最近对话和批注定位不能沿用旧轮次参数。行为见 [上下文阅读](../../sources/product-flows.md#上下文阅读)，回归见 [上下文刷新测试](../../../../packages/web/src/test/context-refresh.test.tsx)。

首页最近协作先显示本机快照，不因已加入的旧协作失联而串行等待；远端状态在后台刷新并由下一次轮询更新。进入详情仍实时探测单个协作，MCP 需要确认当前状态时使用 `get_collaboration`。

个人 Codex / Claude Code 的 MCP 一次性配置连接本机 Core，后续通过工具定位协作。工具写入走现有 RPC 与输入协调，不另建绕过控制的发送通道；stdout 只输出协议消息。

浅色为主要设计基准，支持系统/浅色/深色主题。检查空状态、失败、长路径、小窗口、焦点和键盘；更新 Web 源码后提交重建的嵌入式资源。

## 检查入口

[前端路径测试](../../../../packages/web/src/test/flows.test.tsx) · [MCP 测试](../../../../internal/mcp/server_test.go) · [浏览器与工程门槛](../../sources/validation/test-gates.md)

相关任务：[产品模型](product-model-and-glossary.md) · [输入与共享](input-and-sharing.md) · [原生客户端](native-clients-and-models.md)

首次流程支持创建并邀请、App 邀请确认后进入上下文，以及 MCP 配置/协议探测/实际调用分别展示。完整规则见 [分发与首次体验](../../sources/distribution-and-onboarding.md)。

个人客户端选择、配置覆盖检测与独立调用证据的完整规则见 [Claude 辅助模式](../../sources/decisions/claude-native-tui.md#个人-claude-code-辅助模式)。辅助客户端的 Provider 不改变目标协作能力；共享 Claude worker 仍通过直接 TUI 处理审批、中断与补充。
