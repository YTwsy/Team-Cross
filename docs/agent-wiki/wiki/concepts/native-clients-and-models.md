# 原生客户端与模型

先读 [客户端与模型决策](../../sources/decisions/native-clients-and-models.md)。直接 TUI/Desktop 连接共享 fork；个人 Codex TUI/Desktop 或 Claude Code TUI 通过 MCP 辅助，各自持有自己的上下文。

## 代码定位

- [launch.go](../../../../internal/collab/launch.go)：检测客户端、生成启动计划和 MCP 配置入口。
- [local_client.go](../../../../internal/collab/local_client.go)：本机账户与偏好路由。
- [process.go](../../../../internal/nativecodex/process.go)：原生进程、独立客户端配置与 TUI 命令。
- [model.go](../../../../internal/collab/model.go)、[rpc.go](../../../../internal/collab/rpc.go)：模型继承、原生确认、设置通知与写入约束。

## 修改时守住的边界

Desktop 使用独立应用数据目录和指定 WebSocket 入口。进程启动成功不能代替历史、审批和 A 上执行的验证；首次登录或引导是单独的客户端状态。

B 的账户操作在 B 本机处理，不能修改 A 的账户或返回 A 的认证 token。共享代码执行仍在 A。

模型选择从来源、持久化会话与当前输入者的原生配置取得。设置失败不更新显示；离线显示最近确认值；不要把真实测试的 Luna/low 配置写入产品运行时。

## 检查入口

[模型回归](../../../../internal/collab/model_test.go) · [启动配置回归](../../../../internal/nativecodex/process_test.go) · [本机登录与输入测试](../../../../internal/collab/collab_test.go) · [真实客户端门槛](../../sources/validation/test-gates.md)

相关任务：[运行时](runtime-architecture.md) · [输入与共享](input-and-sharing.md) · [本地 MCP](webgui-and-mcp.md)

直接客户端区分已连接与已打开对应共享会话；MCP 配置采用稳定 opt/App 路径。首次引导与安装入口见 [分发与首次体验](../../sources/distribution-and-onboarding.md)。

Claude 为实验性原生 TUI 接入，最低版本、后台 job、恢复、路由和权限边界见 [Claude 接入契约](../../sources/decisions/claude-native-tui.md)。修改时从 [nativeclaude](../../../../internal/nativeclaude/) 与 [Claude 协作适配](../../../../internal/collab/claude.go) 进入，不能套用 Codex 的结构化审批、模型设置或 Desktop 语义。

个人辅助客户端独立于目标协作 Provider。Claude user 范围配置与项目覆盖检测见 [mcp.go](../../../../internal/nativeclaude/mcp.go)；通用安装和客户端状态见 [MCPConnection.tsx](../../../../packages/web/src/components/MCPConnection.tsx)。协议探测与实际 Codex / Claude 调用分开显示，模型和登录继续由各自本机设置决定。
