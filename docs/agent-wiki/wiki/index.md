# Agent Wiki 索引

这里是 Team Cross 当前 Codex 原生协作主线的任务导航。从根目录 [AGENTS.md](../../../AGENTS.md) 和 [README](../../../README.md) 进入后，按修改范围选读；不必每次加载全部来源文档。

## 按任务阅读

| 当前任务 | 优先阅读 | 主要实现 |
| --- | --- | --- |
| 判断产品范围、命名或用户流程 | [产品模型与词汇](concepts/product-model-and-glossary.md) | [协作类型](../../../internal/collab/types.go)、[WebGUI](../../../packages/web/src/) |
| 调整模块、启动或数据路径 | [运行时架构](concepts/runtime-architecture.md) | [CLI](../../../cmd/teamcross/main.go)、[协作核心](../../../internal/collab/app.go) |
| 修改原目录、worktree、fork 或恢复 | [目录与生命周期](concepts/workspace-and-lifecycle.md) | [workspace](../../../internal/workspace/)、[app.go](../../../internal/collab/app.go) |
| 修改 TUI/Desktop、登录、模型或原生接入 | [原生客户端与模型](concepts/native-clients-and-models.md) | [nativecodex](../../../internal/nativecodex/)、[本机路由](../../../internal/collab/local_client.go) |
| 修改邀请、输入交接、审批或断线处理 | [输入协调与共享](concepts/input-and-sharing.md) | [rpc.go](../../../internal/collab/rpc.go)、[network.go](../../../internal/collab/network.go) |
| 修改页面、上下文、批注或辅助工具 | [WebGUI 与本地 MCP](concepts/webgui-and-mcp.md) | [WebGUI](../../../packages/web/src/)、[MCP](../../../internal/mcp/server.go) |
| 选择测试或判断验收结论 | [验证门槛](concepts/validation-gates.md) | [自动化与真实验证入口](../sources/validation/test-gates.md) |

## 完整事实来源

- [项目简报](../sources/project-brief.md)：定位、支持范围与主要边界。
- [核心模型与词汇](../sources/product-core-and-glossary.md)、[产品流程](../sources/product-flows.md)：产品语义与用户动作。
- [架构](../sources/architecture.md)、[协议](../sources/protocol.md)：完整职责、数据流、字段与路由。
- [目录与生命周期决策](../sources/decisions/workspace-and-lifecycle.md)、[原生客户端与模型决策](../sources/decisions/native-clients-and-models.md)、[输入与共享决策](../sources/decisions/input-and-sharing.md)：取舍及重新评估条件。
- [验证契约](../sources/validation/test-gates.md)、[2026-09-09 验收记录](../sources/validation/native-collaboration-2026-09-09.md)：应做什么和已验证什么。
- [主线迁移记录](../../../IMPLEMENTATION.md)：旧现场归档与提交来源。

## 维护

本索引和短页面引用来源与代码，不另建产品规则。实现影响长期判断时，同步来源和对应短页面；维护方法见 [Agent Wiki 说明](../README.md)。旧原型的归档材料只用于考据，当前产品范围由新来源文档与用户已确认的决定确定。
