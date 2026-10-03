# WebGUI 与本地 MCP

本页解释人和 Agent 如何使用同一份空间内容，并按问题定位来源和代码。界面行为由 [产品流程](../../sources/product-flows.md) 维护，工具与数据字段由 [协议](../../sources/protocol.md) 维护；这里不另存完整交互规范或验收状态。

WebGUI 负责协作管理、材料阅读和原文讨论；菜单栏速览提供较轻的查找入口。个人 MCP 让自己的 Agent 访问已发起或加入的空间，共享运行时的工具则绑定当前空间。它们通过 Core 使用同一套权限和原文引用，默认阅读视图可以不同。完整 Agent 对话与执行交互仍在对应原生客户端。

ChatGPT 本机插件复用 WebGUI，通过 UI MCP 桥调用 Core；插件界面能打开、模型能读到引用、会话能主动接收请求是三个需要分别判断的环节。输入框 mention 用于选择引用，发送给明确配对目标则沿 [接收会话契约](../../sources/decisions/agent-pairing.md) 处理。不要从页面、资源或工具已经加载推导后续环节已通过。

## 按问题查来源

| 问题 | 主要维护位置 | 实现入口 |
| --- | --- | --- |
| 来源选择、发布范围与确认 | [分享范围与确认](../../sources/product-flows.md#分享范围与确认)、[空间契约](../../sources/decisions/collaboration-spaces.md) | [Publisher](../../../../packages/web/src/components/Publisher.tsx)、[PublicationReader](../../../../packages/web/src/components/PublicationReader.tsx) |
| 正文、目录、阅读位置与批注编辑 | [上下文阅读](../../sources/product-flows.md#上下文阅读)、[批注引用](../../sources/protocol.md#批注引用) | [Reading](../../../../packages/web/src/components/Reading.tsx)、[Context](../../../../packages/web/src/components/Context.tsx)、[原文位置映射](../../../../packages/web/src/reading.ts) |
| Agent 默认读取、工具输出和分页 | [按需读取协议](../../sources/protocol.md#agent-按需读取视图) | [读取投影](../../../../internal/collab/agent_read.go)、[批注读取](../../../../internal/collab/agent_annotations.go) |
| 资源选择、固定引用编号与菜单栏速览 | [资源库契约](../../sources/decisions/resource-library.md)、[资源库协议](../../sources/protocol.md#个人资源库) | [Library](../../../../packages/web/src/components/Library.tsx)、[Core](../../../../internal/collab/library.go)、[App](../../../../apps/macos/TeamCross.swift) |
| 配对、明确发送和请求回执 | [配对契约](../../sources/decisions/agent-pairing.md)、[空间工作台](../../sources/decisions/space-workbench.md) | [配对 Core](../../../../internal/collab/agents.go)、[工作台](../../../../packages/web/src/components/SpaceWorkbench.tsx) |
| ChatGPT 面板、带回对话与 mention | [本机插件契约](../../sources/decisions/chatgpt-local-plugin.md) | [UI MCP](../../../../internal/mcp/ui.go)、[mentions](../../../../internal/mcp/mentions.go)、[宿主桥](../../../../packages/web/src/plugin/bridge.ts) |
| 个人 MCP 管理与当前 Session 分享 | [管理协议](../../sources/protocol.md#个人-mcp-管理入口)、[当前 Session](../../sources/protocol.md#当前-session-与延后分享) | [管理工具](../../../../internal/mcp/management.go)、[来源工具](../../../../internal/mcp/current.go) |
| 首页快照、界面语言与主题 | [首页与创建](../../sources/product-flows.md#首页与创建入口)、[界面语言](../../sources/product-flows.md#界面语言)、[开发约束](../../sources/development.md#前端与本机界面) | [App.tsx](../../../../packages/web/src/App.tsx)、[i18n.ts](../../../../packages/web/src/i18n.ts)、[样式](../../../../packages/web/src/styles.css) |
| 安装、邀请打开与个人客户端配置 | [分发与首次体验](../../sources/distribution-and-onboarding.md)、[Claude 辅助模式](../../sources/decisions/claude-native-tui.md#个人-claude-code-辅助模式) | [公共启动器](../../../../internal/service/service.go)、[客户端启动](../../../../internal/collab/launch.go) |

## 阅读与发送的关系

材料是已发布的固定范围，执行上下文是经授权读取的活历史；共用阅读组件不使两者具有相同的存储或版本语义。批注保留原文引用，资源选择把明确引用交给后续读取。读取和保存讨论都不会自动开始模型轮次；需要处理时，再明确选择目标、要求与发送动作。

工具写入、直接客户端输入和工作台请求分别沿对应授权路径进入 Core。辅助客户端的 Provider 不决定目标运行时支持什么，也不使普通 MCP 自动获得 Channel 或原生接收连接。判断接收边界时，继续读取目标的领域契约。

## 检查入口

改动时从 [验证门槛](../../sources/validation/test-gates.md) 选择工程、浏览器和原生客户端检查；判断已有验收时从 [证据入口](../../sources/validation/evidence-map.md) 读取具体版本报告。浏览器宿主替身、原生工具加载和用户桌面操作分别报告。

相关导航：[产品模型](product-model-and-glossary.md) · [协作空间](collaboration-spaces.md) · [运行时架构](runtime-architecture.md)
