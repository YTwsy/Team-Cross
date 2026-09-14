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
| 安装、App 外壳与首次体验 | [分发与首次体验](../sources/distribution-and-onboarding.md) | [公共启动器](../../../internal/service/service.go)、[App](../../../apps/macos/TeamCross.swift)、[构建](../../../scripts/build-release.py) |

## 完整事实来源

- [项目简报](../sources/project-brief.md)：定位、支持范围与主要边界。
- [核心模型与词汇](../sources/product-core-and-glossary.md)、[产品流程](../sources/product-flows.md)：产品语义与用户动作。
- [架构](../sources/architecture.md)、[协议](../sources/protocol.md)：完整职责、数据流、字段与路由。
- [目录与生命周期决策](../sources/decisions/workspace-and-lifecycle.md)、[原生客户端与模型决策](../sources/decisions/native-clients-and-models.md)、[输入与共享决策](../sources/decisions/input-and-sharing.md)：取舍及重新评估条件。
- [验证契约](../sources/validation/test-gates.md)、[2026-09-10 安装与首次体验](../sources/validation/onboarding-macos-2026-09-10.md)、[2026-09-09 原生协作](../sources/validation/native-collaboration-2026-09-09.md)：应做什么和已验证什么。
- [2026-09-10 WebGUI 上下文刷新](../sources/validation/webgui-context-refresh-2026-09-10.md)：自动及手动刷新、阅读位置回归与浏览器证据。
- [2026-09-11 WebGUI 原处批注](../sources/validation/webgui-annotations-2026-09-11.md)：原文定位、批注快照、MCP 往返、变化提示与浏览器证据。
- [2026-09-13 批注闭环](../sources/validation/annotations-2026-09-13.md)：直接原生 TUI 的读取/回复/恢复、MCP 隔离、内嵌编辑与单层回复、浏览器证据及 Desktop 尚未覆盖的窗口边界。
- [2026-09-13 个人 Desktop 定位 fork](../sources/validation/personal-desktop-2026-09-13.md)：邀请者主动打开、原生 ID 与输入归属边界、48 项 Web 回归、浏览器到打开请求链路及 Desktop 窗口未覆盖范围。
- [2026-09-14 Claude 个人 CLI/TUI 历史](../sources/validation/claude-personal-history-2026-09-14.md)：原生 fork 的单文件个人 history 发布、`/resume` picker 可见、个人配置与其他历史隔离，以及同机双 Core 的审批、恢复和生命周期证据。
- [2026-09-10 邀请与原生会话释放](../sources/validation/membership-and-release-2026-09-10.md)：成员期限、断线恢复、原生写入锁和客户端关闭边界。
- [2026-09-11 v0.1.1 分发](../sources/validation/distribution-v0.1.1-2026-09-11.md)：App 命令注册、Homebrew 互斥与回滚、同机真实运行时和页面复核。
- [2026-09-11 菜单栏 App 跨副本去重](../sources/validation/app-instance-2026-09-11.md)：真实 App 副本并发、邀请转交、确认去重、无响应恢复、Core 保留与本机修复。
- [2026-09-12 个人 Claude 辅助 MCP](../sources/validation/claude-assist-2026-09-12.md)：个人配置、真实工具读写、输入归属、去重、访问撤销与页面打开。
- [2026-09-12 Claude 验收](../sources/validation/claude-native-tui-2026-09-12.md)：旧独立 home / 离线 JSONL 实现的真实 Luna、TUI/MCP 共用 worker、审批交接与恢复。
- [Claude 原生 TUI 接入](../sources/decisions/claude-native-tui.md)：实验性 Provider、后台 job、原生审批、接力与明确能力边界。
- [主线迁移记录](../../../IMPLEMENTATION.md)：旧现场归档与提交来源。

## 维护

本索引和短页面引用来源与代码，不另建产品规则。实现影响长期判断时，同步来源和对应短页面；维护方法见 [Agent Wiki 说明](../README.md)。旧原型的归档材料只用于考据，当前产品范围由新来源文档与用户已确认的决定确定。

安装和首次体验：[分发与首次体验](../sources/distribution-and-onboarding.md)。
