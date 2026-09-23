# Agent Wiki 索引

这里是 Team Cross 当前 Codex 原生协作主线的任务导航。从根目录 [AGENTS.md](../../../AGENTS.md) 和 [README](../../../README.md) 进入后，按修改范围选读；不必每次加载全部来源文档。

面向用户的详细操作见 [使用指南](../../user-guide.md)，本地开发与构建入口见 [开发与验证](../../development.md)。

## 按任务阅读

| 当前任务 | 优先阅读 | 主要实现 |
| --- | --- | --- |
| 判断产品范围、命名或用户流程 | [产品模型与词汇](concepts/product-model-and-glossary.md) | [协作类型](../../../internal/collab/types.go)、[WebGUI](../../../packages/web/src/) |
| 修改只读分享、三人以上与多会话材料 | [协作空间](concepts/collaboration-spaces.md) | [空间](../../../internal/collab/spaces.go)、[材料](../../../internal/collab/materials.go)、[MCP](../../../internal/mcp/materials.go) |
| 调整模块、启动或数据路径 | [运行时架构](concepts/runtime-architecture.md) | [CLI](../../../cmd/teamcross/main.go)、[协作核心](../../../internal/collab/app.go) |
| 修改原目录、worktree、fork 或恢复 | [目录与生命周期](concepts/workspace-and-lifecycle.md) | [workspace](../../../internal/workspace/)、[app.go](../../../internal/collab/app.go) |
| 修改 TUI/Desktop、登录、模型或原生接入 | [原生客户端与模型](concepts/native-clients-and-models.md) | [nativecodex](../../../internal/nativecodex/)、[本机路由](../../../internal/collab/local_client.go) |
| 修改邀请、输入交接、审批或断线处理 | [输入协调与共享](concepts/input-and-sharing.md) | [rpc.go](../../../internal/collab/rpc.go)、[network.go](../../../internal/collab/network.go) |
| 修改页面、上下文、批注或辅助工具 | [WebGUI 与本地 MCP](concepts/webgui-and-mcp.md) | [WebGUI](../../../packages/web/src/)、[MCP](../../../internal/mcp/server.go) |
| 修改资源库、选择清单或菜单栏速览 | [资源库与速览](../sources/decisions/resource-library.md) | [资源库](../../../internal/collab/library.go)、[界面](../../../packages/web/src/components/Library.tsx)、[App](../../../apps/macos/TeamCross.swift) |
| 选择测试或判断验收结论 | [验证门槛](concepts/validation-gates.md) | [自动化与真实验证入口](../sources/validation/test-gates.md) |
| 安装、App 外壳与首次体验 | [分发与首次体验](../sources/distribution-and-onboarding.md) | [公共启动器](../../../internal/service/service.go)、[App](../../../apps/macos/TeamCross.swift)、[构建](../../../scripts/build-release.py) |

## 完整事实来源

- [2026-09-23 协作详情阅读布局反馈](../sources/validation/reading-layout-feedback-2026-09-23.md)：标签对齐、正文间距、概览切换位置、详情顶部和批注卡片布局、吸顶位置以及 Web 回归与截图。
- [2026-09-23 整页阅读与成员折叠](../sources/validation/reading-scroll-2026-09-23.md)：单一页面滚动、吸顶工具栏、段落位置恢复、成员过渡动画、119 项 Web 回归和 12 个窗口/主题视图。
- [2026-09-23 资源库浏览顺序稳定性](../sources/validation/library-browsing-2026-09-23.md)：操作与后台同步保持条目及分组位置、显式刷新重排、112 项 Web 回归和真实 Core 浏览器验证。
- [2026-09-23 分享范围吸顶操作区](../sources/validation/publication-controls-2026-09-23.md)：顶部集中范围、阅读及预览操作，保留可展开导出说明，113 项 Web 回归和滚动避让验证。

- [2026-09-23 v0.2.2 正式版发布](../sources/validation/release-v0.2.2-2026-09-23.md)：PR/main、annotated tag、Latest Release、arm64 资产与 provenance、稳定 Homebrew tap PR 和公共 smoke。

- [2026-09-22 材料卡片内阅读](../sources/validation/material-cards-2026-09-22.md)：卡片内展开、标题与操作去重、历史版本信息同步、110 项 Web 回归与响应式 DOM 检查。

- [2026-09-22 协作阅读布局与操作反馈](../sources/validation/reading-tabs-2026-09-22.md)：标题栏切换、稳定阅读区域、工具栏合并、收藏反馈、110 项 Web 回归与 12 个窗口/主题视图。

- [2026-09-22 个人资源库与资源速览](../sources/validation/resource-library-2026-09-22.md)：跨类型选择、内联读取入口、真实 MCP 读取、权限隔离、106 项 Web 回归与六组窗口/主题复核；原生速览桌面交互保留未验收边界。
- [个人资源库与资源速览决策](../sources/decisions/resource-library.md)：本机索引、固定引用编号、个人/共享入口、设置保留和菜单栏取舍。

- [2026-09-21 分享范围阅读与确认](../sources/validation/publication-range-2026-09-21.md)：目录定位与范围分离、紧凑草稿分页、确认范围隔离、96 项 Web 回归和真实浏览器发布流程。

- [2026-09-21 v0.2.1 正式版发布](../sources/validation/release-v0.2.1-2026-09-21.md)：PR/main、annotated tag、Latest Release、arm64 资产与 provenance、稳定 Homebrew tap PR 和公共 smoke。
- [2026-09-20 协作对话目录定位](../sources/validation/context-outline-2026-09-20.md)：未加载轮次的读取与定位、专注状态、窄屏留白及工程验证。
- [2026-09-20 内容寻址材料与按条读取](../sources/validation/content-addressed-materials-2026-09-20.md)：schema 3 清单/blob、可续传发布、材料与活上下文折叠、Agent/MCP 按条读取、83 项 Web 回归和真实浏览器闭环。
- [2026-09-19 空间邀请与关闭](../sources/validation/space-invitations-2026-09-19.md)：一个链接多人加入、原空间启用执行、成员访问与无成员关闭。
- [2026-09-18 只读空间与会话材料](../sources/validation/materials-2026-09-18.md)：公开范围、固定版本、按需阅读、三 Core 浏览器流程与能力边界。

- [2026-09-18 多成员基础](../sources/validation/multi-member-2026-09-18.md)：多邀请、三成员隔离、输入交接、WebGUI 与同机验证边界。

- [只读分享与多人协作空间](../sources/decisions/collaboration-spaces.md)：用户确认的空间组织方式、设计基线、已实现范围与限制；具体测试证据见对应验收记录。

- [2026-09-16 个人 Agent 与 CLI](../sources/validation/agent-cli-collaboration-2026-09-16.md)：输入接力入口、Core 统一条件与各批次实际验证边界。

- [项目简报](../sources/project-brief.md)：定位、支持范围与主要边界。
- [核心模型与词汇](../sources/product-core-and-glossary.md)、[产品流程](../sources/product-flows.md)：产品语义与用户动作。
- [架构](../sources/architecture.md)、[协议](../sources/protocol.md)：完整职责、数据流、字段与路由。
- [协作模式决策](../sources/decisions/runtime-modes.md)：创建时固定的受限/信任模式、原生配置继承、两种 Provider 的生命周期和权限边界。
- [目录与生命周期决策](../sources/decisions/workspace-and-lifecycle.md)、[原生客户端与模型决策](../sources/decisions/native-clients-and-models.md)、[输入与共享决策](../sources/decisions/input-and-sharing.md)：取舍及重新评估条件。
- [验证契约](../sources/validation/test-gates.md)、[2026-09-10 安装与首次体验](../sources/validation/onboarding-macos-2026-09-10.md)、[2026-09-09 原生协作](../sources/validation/native-collaboration-2026-09-09.md)：应做什么和已验证什么。
- [2026-09-16 v0.1.7-rc.1 发布](../sources/validation/release-v0.1.7-rc.1-2026-09-16.md)：信任模式 PR/main、tag、公开 arm64 资产、provenance、Homebrew RC，以及检查登记竞态与恢复。
- [2026-09-16 信任模式](../sources/validation/trusted-runtime-2026-09-16.md)：Codex / Claude 个人配置继承、原生确认、固定模式、同 session 恢复、受限回归、工程与浏览器验证。
- [2026-09-16 App 图标与 v0.1.6-rc.3 发布](../sources/validation/app-icon-release-v0.1.6-rc.3-2026-09-16.md)：非透明主体范围、sRGB/alpha、传统 ICNS、tag、公开资产、provenance、Homebrew RC 与 macOS 26 显示验收边界。
- [2026-09-15 GitHub CI、unsigned 发布与 v0.1.6-rc.1/v0.1.6-rc.2](../sources/validation/github-ci-release-2026-09-15.md)：PR/main 门槛、正式版本号策略、托管 arm64 构建、GitHub App、受保护 Homebrew tap、校验和、provenance、tag 与公开 Pre-release。
- [2026-09-15 Tailcat 显式连接方式](../sources/validation/tailcat-transport-2026-09-15.md)：`tcx3`、Go 1.27.1、同机 Tailcat 数据面、WebGUI、arm64 构建链及尚待两台 Mac 完成的网络门槛。
- [2026-09-14 Codex 动态 MCP 与运行时恢复](../sources/validation/codex-runtime-mcp-2026-09-14.md)：CLI 版本变化后的插件动态 MCP 回归、隔离配置修复、真实 Luna 创建与同 session 恢复边界。
- [2026-09-19 材料阅读与原文旁批注](../sources/validation/reading-and-annotations-2026-09-19.md)：Markdown 阅读器、UTF-16 原文映射、响应式旁批、固定版本引用、79 项 Web 回归与三 Core 浏览器闭环。
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
