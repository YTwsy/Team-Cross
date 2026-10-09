# Agent Wiki 索引

按问题直接进入来源或代码；需要理解对象关系时选读下方概念页。仓库约束见
[AGENTS.md](../../../AGENTS.md)，产品概览与启动入口见 [README](../../../README.md)。

面向用户的详细操作见 [使用指南](../../user-guide.md)，本地开发与构建入口见 [开发与构建](../sources/development.md)。

## 按任务阅读

| 当前任务 | 主要来源 | 实现入口 |
| --- | --- | --- |
| 搭建环境、本地开发或构建 | [开发与构建](../sources/development.md) | [Makefile](../../../Makefile)、[工具链要求](../../../package.json) |
| 判断定位、产品范围或命名 | [项目简报](../sources/project-brief.md)、[核心词汇](../sources/product-core-and-glossary.md) | [协作类型](../../../internal/collab/types.go) |
| 修改只读分享、三人以上与多会话材料 | [空间契约](../sources/decisions/collaboration-spaces.md)、[材料协议](../sources/protocol.md#已发布会话材料) | [空间](../../../internal/collab/spaces.go)、[材料](../../../internal/collab/materials.go)、[MCP](../../../internal/mcp/materials.go) |
| 调整模块、启动或数据路径 | [架构](../sources/architecture.md)、[协议](../sources/protocol.md) | [CLI](../../../cmd/teamcross/main.go)、[协作核心](../../../internal/collab/app.go) |
| 修改原目录、worktree、fork 或恢复 | [目录与生命周期](../sources/decisions/workspace-and-lifecycle.md) | [workspace](../../../internal/workspace/)、[app.go](../../../internal/collab/app.go) |
| 修改 TUI/Desktop、登录、模型或原生接入 | [原生客户端与模型](../sources/decisions/native-clients-and-models.md) | [nativecodex](../../../internal/nativecodex/)、[本机路由](../../../internal/collab/local_client.go) |
| 修改受限/信任模式与原生配置继承 | [协作模式](../sources/decisions/runtime-modes.md) | [模式定义](../../../internal/runtimeconfig/mode.go)、[创建与恢复](../../../internal/collab/app.go) |
| 修改实验性 Claude TUI 或辅助入口 | [Claude 接入契约](../sources/decisions/claude-native-tui.md) | [nativeclaude](../../../internal/nativeclaude/)、[协作适配](../../../internal/collab/claude.go) |
| 修改邀请、输入交接、审批或断线处理 | [输入协调与共享](../sources/decisions/input-and-sharing.md) | [rpc.go](../../../internal/collab/rpc.go)、[network.go](../../../internal/collab/network.go) |
| 修改用户流程、页面、阅读、批注或界面语言 | [产品流程](../sources/product-flows.md)、[前端与本机界面](../sources/development.md#前端与本机界面) | [WebGUI](../../../packages/web/src/)、[流程测试](../../../packages/web/src/test/flows.test.tsx) |
| 修改 MCP 管理、当前 Session 分享或按需读取 | [协议](../sources/protocol.md)、[目录与生命周期](../sources/decisions/workspace-and-lifecycle.md) | [MCP](../../../internal/mcp/)、[延后分享](../../../internal/collab/current_share.go) |
| 修改资源库、选择清单或菜单栏速览 | [资源库与速览](../sources/decisions/resource-library.md) | [资源库](../../../internal/collab/library.go)、[界面](../../../packages/web/src/components/Library.tsx)、[App](../../../apps/macos/TeamCross.swift) |
| 修改会话配对与交给 Agent | [配对契约](../sources/decisions/agent-pairing.md) | [Core](../../../internal/collab/agents.go)、[MCP](../../../internal/mcp/agents.go)、[界面](../../../packages/web/src/components/AgentPairings.tsx) |
| 修改 ChatGPT 本机 WebGUI 插件或宿主消息桥 | [本机插件](../sources/decisions/chatgpt-local-plugin.md) | [UI MCP](../../../internal/mcp/ui.go)、[包管理](../../../internal/pluginpack/plugin.go)、[共同入口](../../../packages/web/src/main.tsx)、[插件环境](../../../packages/web/src/plugin/environment.ts) |
| 修改空间请求、简报或可选专用会话 | [空间工作台契约](../sources/decisions/space-workbench.md) | [空间业务](../../../internal/collab/workbench.go)、[接收会话](../../../internal/collab/space_receivers.go)、[工作台](../../../packages/web/src/components/SpaceWorkbench.tsx) |
| 修改持续关注、Cloud 事件或订阅回执 | [空间事件契约](../sources/decisions/space-events.md) | [MCP Events](../../../internal/mcpevents/)、[Core](../../../internal/collab/space_events.go)、[隔离网关](../../../cmd/teamcross/events.go) |
| 选择测试与通过标准 | [验证门槛](../sources/validation/test-gates.md) | 页内按改动范围选择自动化、客户端与网络入口 |
| 判断支持范围与实际验收结论 | [验证范围与证据入口](../sources/validation/evidence-map.md) | 先确定具体路径，再核对契约、实现和对应报告 |
| 安装、App 外壳与首次体验 | [分发与首次体验](../sources/distribution-and-onboarding.md) | [公共启动器](../../../internal/service/service.go)、[App](../../../apps/macos/TeamCross.swift)、[构建](../../../scripts/build-release.py) |
| 构建产物、发布或更新 Homebrew | [构建与发布](../sources/releasing.md) | [构建](../../../scripts/build-release.py)、[发布 workflow](../../../.github/workflows/release-unsigned.yml)、[Homebrew workflow](../../../.github/workflows/homebrew-publish.yml) |

## 按需理解概念关系

| 理解的问题 | 概念页 |
| --- | --- |
| 个人会话、材料与共同执行如何关联 | [产品模型与词汇](concepts/product-model-and-glossary.md) |
| 成员、公开范围、执行访问与输入权怎样区分 | [协作空间](concepts/collaboration-spaces.md) |
| 空间、Core、原生运行时与成员主机如何分工 | [运行时架构](concepts/runtime-architecture.md) |
| 人与 Agent 的入口怎样共用内容、分别接收请求 | [WebGUI 与 MCP](concepts/webgui-and-mcp.md) |
| 方法、实测结果与当前能力判断怎样关联 | [验证与证据](concepts/validation-gates.md) |

## 维护

文档职责、概念抽象与更新方式见 [Agent Wiki 说明](../README.md)。
验收判断从 [证据入口](../sources/validation/evidence-map.md) 定向读取报告；旧原型迁移从
[IMPLEMENTATION](../../../IMPLEMENTATION.md) 追溯。本索引维护当前入口，不累积历史报告清单。
