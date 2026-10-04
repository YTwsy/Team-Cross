# 原生客户端与模型设置决策

## 两种参与方式

直接 TUI 与专用 Desktop 连接同一个协作 fork；个人 Codex TUI/Desktop 或 Claude Code TUI 通过 MCP 辅助。共享会话在 A 执行，辅助会话保留自己的本地上下文，双方按需交换选定信息。

采用 Codex app-server 作为原生协议入口，让历史、输入、事件和审批沿原生会话继续。Team Cross 不需要先定义能容纳各类 Provider 完整历史的新会话格式。Codex 为现有主线；Claude 的实验性原生 job 接入与限制另见 [Claude 原生 TUI](claude-native-tui.md)。

专用 Desktop 使用独立 `CODEX_HOME` 和应用数据目录，避免与普通 Desktop 的实例状态混用。其指定 WebSocket 启动入口仍需按客户端版本实测；共用 app-server 不足以证明所有 Desktop 菜单或全局功能均兼容。

邀请者在 Codex fork 创建成功后，可从详情页点击“在个人 Codex 中打开”，通过 `codex://threads/<sessionId>` 在已有个人 Desktop 中定位协作。该入口解决外部新建 fork 尚未出现在列表中的定位问题，由邀请者主动点击；创建、协作者输入/完成和输入交接均不自动打开或切换窗口。共享已关闭且 `runtimeState=released` 时显示“在个人 Codex 中继续”。个人 Desktop 不接入共享网关，深链接不保证持续消息同步，也不提供额外只读限制；共享期间的受控输入继续使用直接客户端或辅助 MCP，普通 Desktop 续写需要原生写入权已释放。入口不恢复运行时或为查看历史提前释放共享。

Claude 不增加等价的 Desktop 深链接。受限模式由隔离 runtime 中的原生 `--fork-session` 创建；首次业务输入持久化后，只把该 fork 的 transcript 发布到 A 的个人 Claude Code CLI/TUI history home，因此会出现在 `/resume`，共享释放后可由 A 继续。零输入 fork 不以 transcript 落盘作为创建门槛。普通个人 `/resume` 不经过 Team Cross 输入协调，因此共享运行时仍活跃时应使用直接 TUI attach，不在个人历史中并行续写同一 session。

个人辅助客户端与目标协作的 Provider 独立。安装、配置检测、普通 TUI 打开与实际调用证据复用本机 Core；具体权限与控制能力仍以目标协作为准。Claude 个人配置和验证边界见 [Claude 个人辅助模式](claude-native-tui.md#个人-claude-code-辅助模式)。

共享运行时自动加载当前协作的批注读取与回复工具，直接客户端无需另开个人辅助会话；读取与保存回复本身不另起模型轮次。该工具集不含发送输入或选择其他协作的能力。Codex 创建和恢复按进程配置接入，Claude 沿用新协作的原生启动配置；旧 worker 的接入边界见 [批注工具协议](../protocol.md#共享运行时的批注工具)。

## CLI 发现与配置

Team Cross 使用本机已有的 Codex 安装，不下载、安装或管理 Codex 的更新。手动 CLI 配置优先于 `TEAMCROSS_CODEX_BIN`；没有手动配置时，环境变量仍是明确的覆盖。自动发现依次检查已选择 Desktop 应用的内置 CLI、当前进程 PATH、系统/用户 Applications 中的 ChatGPT/Codex 应用、常见命令目录，以及登录 Shell 的 PATH。最后一项用于 Finder/App 启动时缺少 nvm 等目录的情况，探测总时限为 3 秒，结果缓存 1 分钟；用户重新检测会立即刷新这项缓存。

应用内探测只识别已知布局：`Contents/Resources/codex` 和 `Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex`，自动发现优先使用后者。已保存的路径失效时，只有它属于已知应用内布局、且 Desktop 未指定其他应用，才在同一 `.app` 中重新定位。普通自定义路径或环境变量失效会明确报告，不静默切换到另一份安装。探测要求文件可执行，忽略 PATH 中的相对目录、无效符号链接与普通文本文件。

设置中的原始值与当前检测结果分开返回和显示。留空代表自动发现；保存语言、Desktop 或 Claude 配置时，不把检测到的 CLI、应用和 Claude 路径自动写成固定值。旧应用内路径自动恢复后仍保留原配置供用户检查，用户可明确选择恢复自动发现并保存。重新检测读取已保存配置，不提交或覆盖未保存的表单。CLI 版本可读取仅显示“CLI 可运行”，不代表专用 Desktop 已连接、已登录或完成原生协议验收。

## 本机账户与共享执行

本机代理处理客户端账户登录、完成通知与偏好；A 的协作网关负责共享会话和执行。不能把 B 的登录请求转成修改 A 的账户，也不能将 A 的认证 token 或完整个人配置返回 B。

初始化、登录、历史显示、直接输入、审批和代码执行需分别验证。首次专用 Desktop 可能出现登录或引导页，应给出明确下一步，不能把“应用进程已启动”当成已加入会话。

## 模型继承与选择

创建读取来源确认的模型、Provider 和推理强度；缺少信息时由 A 的 Codex 配置决定。恢复读取原生协作会话最新持久化设置，后续遵循当前输入者的原生客户端选择。

带模型配置的恢复和设置修改属于写入，同样受输入归属约束。执行目录绑定继续有效；受限模式固定权限配置，信任模式继承原生权限并允许当前输入者选择原生权限。完整规则见 [协作模式](runtime-modes.md)。

界面根据成功后的原生读取与设置通知更新。请求失败不显示为已生效；离线显示最近确认的设置。这是会话配置，不承诺逐轮模型遥测。

真实模型验证限定 `gpt-5.6-luna` 是测试约束。产品进程、启动命令和客户端配置不得据此强制模型或 `low` 推理强度。

## 重新评估条件

Codex 版本变化时重做原生客户端验收；引入其他 Provider 时单独设计接入契约；支持新的客户端全局功能时补本机与远端职责验证。

## 实现与验证

| 修改范围 | 入口 |
| --- | --- |
| 客户端检测、启动计划与隔离配置 | [discovery.go](../../../../internal/nativecodex/discovery.go)、[launch.go](../../../../internal/collab/launch.go)、[process.go](../../../../internal/nativecodex/process.go)、[NativeClientSettings.tsx](../../../../packages/web/src/components/NativeClientSettings.tsx) |
| 个人 Desktop 深链接 | [personal_desktop.go](../../../../internal/collab/personal_desktop.go)；系统打开成功不代表页面或后续对话已同步 |
| 本机账户、偏好与信任模式 hook 确认 | [local_client.go](../../../../internal/collab/local_client.go)，转发边界见 [批注工具与运行时配置协议](../protocol.md#共享运行时的批注工具) |
| 模型继承、确认与写入约束 | [model.go](../../../../internal/collab/model.go)、[rpc.go](../../../../internal/collab/rpc.go) |
| Claude 原生 TUI 与个人辅助配置 | [nativeclaude](../../../../internal/nativeclaude/)、[claude.go](../../../../internal/collab/claude.go)、[mcp.go](../../../../internal/nativeclaude/mcp.go)；按 [Claude 接入契约](claude-native-tui.md) 区分能力，不套用 Codex 的审批、模型设置或 Desktop 语义 |
| 通用 MCP 安装与客户端状态 | [MCPConnection.tsx](../../../../packages/web/src/components/MCPConnection.tsx)；稳定路径、协议探测和实际调用分别按 [首次体验](../distribution-and-onboarding.md#首次协作) 检查 |

检查 [模型回归](../../../../internal/collab/model_test.go)、[启动配置回归](../../../../internal/nativecodex/process_test.go) 与 [本机登录和输入测试](../../../../internal/collab/collab_test.go)，再按 [验证门槛](../validation/test-gates.md) 选择实际客户端验收。
