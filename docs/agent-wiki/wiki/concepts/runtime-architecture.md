# 运行时架构

本页解释托管位置、进程与调用入口之间的关系，详细模块和数据流由 [架构](../../sources/architecture.md) 维护，字段和方法由 [协议](../../sources/protocol.md) 维护。

Go Core 保存空间、已发布材料和本机管理状态。共享执行由 A 的专属原生运行时持有 fork，其他成员的本机 Core 连接 A；各自的个人 Agent 仍在自己的环境中运行。独立只读空间不要求共享执行运行时。工作台按需新建会话时，由创建者的 Core 维护运行时连接，不进入空间的共同执行对象；接收能力与当前连接范围由 [配对契约](../../sources/decisions/agent-pairing.md#身份接收能力与范围)维护。

CLI、App 和 MCP 按数据目录发现同一后台 Core；菜单栏外壳另有自己的实例协调。服务可用、外壳已打开、客户端已接通和模型已完成分别代表不同层次。App/Core 的发现与退出规则见 [分发与首次体验](../../sources/distribution-and-onboarding.md)，接收进程和空间角色的关系见 [工作台契约](../../sources/decisions/space-workbench.md)。

## 从哪里进入代码

| 职责 | 入口 |
| --- | --- |
| 命令、监听、Web 嵌入与关闭 | [cmd/teamcross/main.go](../../../../cmd/teamcross/main.go) |
| 创建、记录持久化、恢复与进程管理 | [collab/app.go](../../../../internal/collab/app.go) |
| 独立空间、冻结来源与材料版本 | [spaces.go](../../../../internal/collab/spaces.go)、[publication.go](../../../../internal/collab/publication.go)、[materials.go](../../../../internal/collab/materials.go) |
| 当前会话身份与本轮结束后分享 | [mcp/current.go](../../../../internal/mcp/current.go)、[collab/current_share.go](../../../../internal/collab/current_share.go)、[nativecodex/source_turn.go](../../../../internal/nativecodex/source_turn.go) |
| 原生 WebSocket RPC 和请求分发 | [nativecodex/process.go](../../../../internal/nativecodex/process.go) |
| 本机管理、共享与接收端代理 | [http.go](../../../../internal/collab/http.go)、[network.go](../../../../internal/collab/network.go) |
| 接收配对、空间请求与独立会话 | [agents.go](../../../../internal/collab/agents.go)、[workbench.go](../../../../internal/collab/workbench.go)、[space_receivers.go](../../../../internal/collab/space_receivers.go) |
| 工具、工作目录与 TLS/传输 | [mcp](../../../../internal/mcp/)、[workspace](../../../../internal/workspace/)、[sharing](../../../../internal/sharing/) |
| Core 生命周期与 App 实例协调 | [service.go](../../../../internal/service/service.go)、[AppInstance.swift](../../../../apps/macos/AppInstance.swift) |

## 按边界追溯来源

创建与恢复、原目录与 worktree、释放后的保留规则看 [目录与生命周期](../../sources/decisions/workspace-and-lifecycle.md)。原生客户端、账户路由和模型设置看 [原生客户端](../../sources/decisions/native-clients-and-models.md)，受限/信任配置继承看 [协作模式](../../sources/decisions/runtime-modes.md)。

共享前的 LAN/Tailcat 选择、成员凭据和输入协调看 [输入与共享](../../sources/decisions/input-and-sharing.md)。它们共用授权层，但网络环境须单独验证。Claude 的单 worker、个人历史和控制路径看 [Claude 接入](../../sources/decisions/claude-native-tui.md)，不能直接套用 Codex app-server 的全部语义。

修改对应路径时按 [验证门槛](../../sources/validation/test-gates.md) 选择检查；已有证据从 [证据入口](../../sources/validation/evidence-map.md) 定向读取。协议替身、真实客户端、App 外壳与跨机传输的结果分别判断。

相关导航：[产品模型](product-model-and-glossary.md) · [WebGUI 与 MCP](webgui-and-mcp.md) · [验证层次](validation-gates.md)
