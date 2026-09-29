# Agent Wiki 索引

这里是 Team Cross 当前 Codex 原生协作主线的任务导航。遵守根目录 [AGENTS.md](../../../AGENTS.md)，按修改范围直接进入来源或代码；需要跨主题上下文时选读概念页。不熟悉产品或启动方式时再读 [README](../../../README.md)。

面向用户的详细操作见 [使用指南](../../user-guide.md)，本地开发与构建入口见 [开发与构建](../sources/development.md)。

## 按任务阅读

| 当前任务 | 优先阅读 | 主要实现 |
| --- | --- | --- |
| 搭建环境、本地开发或构建 | [开发与构建](../sources/development.md) | [Makefile](../../../Makefile)、[工具链要求](../../../package.json) |
| 判断产品范围、命名或用户流程 | [产品模型与词汇](concepts/product-model-and-glossary.md) | [协作类型](../../../internal/collab/types.go)、[WebGUI](../../../packages/web/src/) |
| 修改只读分享、三人以上与多会话材料 | [协作空间](concepts/collaboration-spaces.md) | [空间](../../../internal/collab/spaces.go)、[材料](../../../internal/collab/materials.go)、[MCP](../../../internal/mcp/materials.go) |
| 调整模块、启动或数据路径 | [运行时架构](concepts/runtime-architecture.md) | [CLI](../../../cmd/teamcross/main.go)、[协作核心](../../../internal/collab/app.go) |
| 修改原目录、worktree、fork 或恢复 | [目录与生命周期](../sources/decisions/workspace-and-lifecycle.md) | [workspace](../../../internal/workspace/)、[app.go](../../../internal/collab/app.go) |
| 修改 TUI/Desktop、登录、模型或原生接入 | [原生客户端与模型](../sources/decisions/native-clients-and-models.md) | [nativecodex](../../../internal/nativecodex/)、[本机路由](../../../internal/collab/local_client.go) |
| 修改邀请、输入交接、审批或断线处理 | [输入协调与共享](../sources/decisions/input-and-sharing.md) | [rpc.go](../../../internal/collab/rpc.go)、[network.go](../../../internal/collab/network.go) |
| 修改页面、上下文、批注或辅助工具 | [WebGUI 与本地 MCP](concepts/webgui-and-mcp.md) | [WebGUI](../../../packages/web/src/)、[MCP](../../../internal/mcp/server.go) |
| 修改资源库、选择清单或菜单栏速览 | [资源库与速览](../sources/decisions/resource-library.md) | [资源库](../../../internal/collab/library.go)、[界面](../../../packages/web/src/components/Library.tsx)、[App](../../../apps/macos/TeamCross.swift) |
| 选择测试或判断验收结论 | [验证门槛](concepts/validation-gates.md) | [自动化与真实验证入口](../sources/validation/test-gates.md) |
| 安装、App 外壳与首次体验 | [分发与首次体验](../sources/distribution-and-onboarding.md) | [公共启动器](../../../internal/service/service.go)、[App](../../../apps/macos/TeamCross.swift)、[构建](../../../scripts/build-release.py) |

## 完整事实来源

- [个人资源库与资源速览决策](../sources/decisions/resource-library.md)：本机索引、固定引用编号、个人/共享入口、设置保留和菜单栏取舍。
- [只读分享与多人协作空间](../sources/decisions/collaboration-spaces.md)：用户确认的空间组织方式、设计基线、已实现范围与限制；具体测试证据见对应验收记录。
- [项目简报](../sources/project-brief.md)：定位、支持范围与主要边界。
- [核心模型与词汇](../sources/product-core-and-glossary.md)、[产品流程](../sources/product-flows.md)：产品语义与用户动作。
- [架构](../sources/architecture.md)、[协议](../sources/protocol.md)：完整职责、数据流、字段与路由。
- [协作模式决策](../sources/decisions/runtime-modes.md)：创建时固定的受限/信任模式、原生配置继承、两种 Provider 的生命周期和权限边界。
- [目录与生命周期决策](../sources/decisions/workspace-and-lifecycle.md)、[原生客户端与模型决策](../sources/decisions/native-clients-and-models.md)、[输入与共享决策](../sources/decisions/input-and-sharing.md)：取舍及重新评估条件。
- [Claude 原生 TUI 接入](../sources/decisions/claude-native-tui.md)：实验性 Provider、后台 job、原生审批、接力与明确能力边界。
- [主线迁移记录](../../../IMPLEMENTATION.md)：旧现场归档与提交来源。

## 维护

本索引和概念页引用来源与代码，不另建产品规则。事实变化更新其主要来源，只有摘要内容或导航变化时才更新概念页与本索引；维护方法见 [Agent Wiki 说明](../README.md)。任务文件按需创建，收尾后移入 `docs/agent-wiki/tasks/finished_archived/`，不在本索引追加历史验收清单；具体规则见 [任务记录与历史归档](../README.md#任务记录)。旧原型与已结束任务的归档只用于定向追溯，当前产品范围由有效来源文档与用户已确认的决定确定。

安装和首次体验：[分发与首次体验](../sources/distribution-and-onboarding.md)。
