# 原生客户端与模型

先读 [客户端与模型决策](../../sources/decisions/native-clients-and-models.md)。直接 TUI/Desktop 连接共享 fork；个人 Codex TUI/Desktop 或 Claude Code TUI 通过 MCP 辅助，各自持有自己的上下文。

## 代码定位

- [launch.go](../../../../internal/collab/launch.go)：检测客户端、生成启动计划和 MCP 配置入口。
- [personal_desktop.go](../../../../internal/collab/personal_desktop.go)：邀请者通过持久化 fork ID 在个人 Desktop 中定位协作；只在点击时打开，不交接输入或恢复运行时。
- [local_client.go](../../../../internal/collab/local_client.go)：本机账户与偏好路由。
- [process.go](../../../../internal/nativecodex/process.go)：原生进程、独立客户端配置与 TUI 命令。
- [model.go](../../../../internal/collab/model.go)、[rpc.go](../../../../internal/collab/rpc.go)：模型继承、原生确认、设置通知与写入约束。

## 修改时守住的边界

Desktop 使用独立应用数据目录和指定 WebSocket 入口。进程启动成功不能代替历史、审批和 A 上执行的验证；首次登录或引导是单独的客户端状态。

详情中的“在个人 Codex 中打开”使用普通 Desktop 的深链接，与直接客户端入口分开；创建成功即提供，远端活动不触发页面跳转。共享关闭且运行时释放后才显示“在个人 Codex 中继续”。系统打开请求成功不等于页面或后续对话同步成功。

Claude fork 不使用 Desktop 深链接：原生 fork 首次持久化后，Team Cross 只把这一个 transcript 发布到 A 的个人 CLI/TUI `/resume` 历史。协作运行时仍占用该 session 时通过 Team Cross 直接 TUI attach；释放后才从个人历史继续。独立 `claude-runtime` 只保存所选来源快照、本次 fork、scoped settings、认证快照和 daemon/job 状态，不挂载其他个人 history；Team Cross 不构造 Provider transcript。

B 的账户操作在 B 本机处理，不能修改 A 的账户或返回 A 的认证 token。共享代码执行仍在 A。

模型选择从来源、持久化会话与当前输入者的原生配置取得。设置失败不更新显示；离线显示最近确认值；不要把真实测试的 Luna/low 配置写入产品运行时。

共享运行时的批注工具绑定当前协作，直接 TUI/Desktop 可读取并回复原批注，不启动额外模型轮次。Codex 创建/恢复接入；Claude 新建后沿用原生启动配置，旧 worker 需新建协作。凭据和结束/恢复边界见 [协议](../../sources/protocol.md#共享运行时的批注工具)。

## 检查入口

[模型回归](../../../../internal/collab/model_test.go) · [启动配置回归](../../../../internal/nativecodex/process_test.go) · [本机登录与输入测试](../../../../internal/collab/collab_test.go) · [真实客户端门槛](../../sources/validation/test-gates.md)

相关任务：[运行时](runtime-architecture.md) · [输入与共享](input-and-sharing.md) · [本地 MCP](webgui-and-mcp.md)

直接客户端区分已连接与已打开对应共享会话；MCP 配置采用稳定 opt/App 路径。首次引导与安装入口见 [分发与首次体验](../../sources/distribution-and-onboarding.md)。

Claude 为实验性原生 TUI 接入，最低版本、后台 job、恢复、路由和权限边界见 [Claude 接入契约](../../sources/decisions/claude-native-tui.md)。修改时从 [nativeclaude](../../../../internal/nativeclaude/) 与 [Claude 协作适配](../../../../internal/collab/claude.go) 进入，不能套用 Codex 的结构化审批、模型设置或 Desktop 语义。

个人辅助客户端独立于目标协作 Provider。Claude user 范围配置与项目覆盖检测见 [mcp.go](../../../../internal/nativeclaude/mcp.go)；通用安装和客户端状态见 [MCPConnection.tsx](../../../../packages/web/src/components/MCPConnection.tsx)。协议探测与实际 Codex / Claude 调用分开显示，模型和登录继续由各自本机设置决定。
