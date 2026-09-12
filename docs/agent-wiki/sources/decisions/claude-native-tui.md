# Claude Code 原生 TUI 接入

Claude Code 是新增的实验性 Provider。Codex 原有 app-server 路径继续保留；Claude 使用 A 上一个原生后台 job，Team Cross 控制端与参与者的原生 TUI 都连接这个执行端。当前最低要求 Claude Code `2.1.268`，双方需使用包含此适配的 Team Cross 构建。按数值比较 major/minor/patch，接受不低于此版本的正式版本，拒绝旧版、无效版本和预发布版本。`2.1.268` 保留为已实测基线；更高版本通过版本检查不等于完成该版本的真实协议验收。TUI 本机连接层返回检测到的实际 CLI 版本，不伪装为基线版本。

## 用户入口与能力边界

在创建页选择「Claude Code · 实验性」，从 A 的 `CLAUDE_CONFIG_DIR`（未设置时 `~/.claude`）读取来源。选择原目录或新 worktree 后创建新 fork；详情中的「打开 Claude Code」准备原生 TUI，或显示可复制的启动命令。设置页的 Claude CLI 路径及 `--claude-bin` 可覆盖自动检测。

| 入口 | 当前行为 |
| --- | --- |
| 原生 TUI | 共享历史、文本输入、原生审批、补充与中断；执行在 A |
| WebGUI | 选择来源、创建、加入、输入交接、状态、轻量历史、文件、改动、批注、恢复 |
| MCP / 控制 API | 读取上下文、保存批注、空闲时发送一段文本；发送保留 `requestId` |
| 审批、补充、中断、模型设置 API | 返回 `native_client_required`，要求当前输入者使用 Claude 原生 TUI |
| Claude Desktop / 全局客户端功能 | 未接入；不将 Codex Desktop 连接到 Claude worker |

Claude 的 `capabilities` 随状态返回；调用者应先检查实际支持的入口。`nativeWaiting` 报告 worker 的交互等待状态，不把它转换成伪造的 Codex 审批 ID 或待审批数量。Claude 不提供 Codex 的完整结构化事件流；WebGUI 保留轻量历史，完整工具过程以原生 TUI 为准。新建 Claude worker 内置仅面向当前协作的 `teamcross_annotations` MCP，原生 TUI 中可要求 Agent 读取批注和回复，并答复原批注。WebGUI 与个人辅助 MCP 使用同一份讨论数据。保存不会主动启动模型。

## 个人 Claude Code 辅助模式

设置页与客户端入口可以选择个人 Codex 或 Claude Code。两者均将 `teamcross mcp --data-dir <本机数据目录>` 注册到使用者本机；工具连接该本机 Core，由它访问已加入的协作。辅助客户端 Provider 与目标协作 Provider 分开选择，个人 Claude 可辅助 Codex 协作，个人 Codex 也可辅助 Claude 协作。目标 `capabilities` 和输入归属继续决定允许的操作。

Claude 使用原生 `mcp add --transport stdio --scope user` 写入 user 范围；默认配置位于 `~/.claude.json`，显式 `CLAUDE_CONFIG_DIR` 时位于该目录下的 `.claude.json`。已有相同条目不重复写入；更换 Team Cross 路径或数据目录时只更新 user 范围的 `teamcross`，失败尝试恢复原条目，其他 server 和个人设置由 Claude 原生配置写入机制保留。当前目录的 local / project 同名条目或显式禁用会明确报错，不自动覆盖用户选择。配置范围与优先级依据 [Claude MCP 文档](https://code.claude.com/docs/en/mcp)。

检测仅读配置，不执行任意 MCP server。页面区分配置已保存、Team Cross STDIO 协议探测、实际客户端工具调用；最后一项来自 MCP `initialize.clientInfo.name` 和随后的 `tools/call`，Codex / Claude 分别记录，本次 Core 运行中有效。未知客户端不归到任一 Provider，独立协议探测不产生实际调用证据。客户端名称只是诊断信息，不用于授权。其他工作目录配置、组织策略或已有客户端未重载，仍以实际工具调用结果为准。

打开个人 Claude 是在本机辅助目录启动普通 TUI，沿用本机个人配置、登录、模型与上下文，不带共享 sessionId、attach、模型覆盖或临时 MCP 配置。个人会话有自己的 ID；本地对话由本机模型处理，工具发往共享会话的输入仍由 A 的共享运行时执行。MCP 工具调用本身的原生确认在个人客户端完成；目标 Claude worker 的命令审批、补充、中断仍需当前输入者通过直接 TUI 完成。共享 worker 使用自身受限的批注 MCP，不因安装个人 MCP 自动加载完整辅助工具。

## 单一执行端

A 每次协作使用独立的 `collaborations/<id>/claude-home`，保存所选来源的不可变快照、A 的路由配置和该协作原生 job。先参照官方 Agent SDK `0.3.268` 的离线 fork 规则生成新会话 JSONL，再使用 `--resume <fork> --bg` 启动原生 worker，不发送业务 prompt。创建哈希还绑定来源 JSONL 指纹；复制时再次检查，拒绝过期起点。

原生 `--fork-session --bg` 会延迟生成新 JSONL，首次输入前反复停止和恢复可能丢失来源引用。适配在交付前持久化新的消息 UUID、父链映射与 `forkedFrom`，排除 sidechain/progress，保留原生 fork 需要的附件和元数据，并绑定用户选定的执行目录。工作目录编码与已验证版本一致。新会话文件以 0600 独占创建、同步落盘后才启动 worker；文件缺失或损坏直接报错，不通过来源历史回退掩盖空会话。

恢复先检查协作 worker 是否仍存活，Core 异常退出后可重新绑定同一 worker；已保存的 done 进度标签不代表进程仍存活；已停止时只运行 `--resume <协作 sessionId> --bg`，让原生 job 使用自身保存的启动状态。额外传入创建时的模型、设置等参数会触发新副本，不能在恢复时重用整套创建参数。新版本创建的 worker 沿用持久化批注 MCP 配置；旧 worker 不通过改写 job 状态补装，需新建协作。恢复后校验 sessionId 与执行目录，不接受另一个会话。

Claude ACP 的 SDK `query()` / `canUseTool` 模式有助于理解结构化控制与权限回调，但它会持有自己的 SDK 执行进程。这里采用原生 job 的终端入口，避免与 TUI 并排启动第二个执行端。该路径不依赖安装 Node Agent SDK 或 ACP adapter。

## 传输与输入归属

```text
B 的 claude attach
  → B 的协作专用 Unix socket
  → B Core 的 TLS + 成员凭据连接
  → A Core 的输入者 / epoch 检查
  → A 的原生后台 job Unix socket
```

本机连接层只提供 `nudge/has/attach/resize`，且 `has/attach/resize` 绑定单个 job。它不实现 daemon 管理、任意 job 查询或 `reply` 穿透。A 自己的原生 TUI 同样使用受控入口。参与者目录只保存本机引导与 job 定位信息，不复制 A 的原生对话和 API 凭据。

现有成员认证与 TLS 主机指纹继续保护远端连接。每个终端输入包在转发前检查当前输入者、直接连接、epoch、运行时和成员有效性；检查与有界写入在同一锁内完成。收回或交还输入关闭旧连接，审批界面仍由同一个 worker 持有，新输入者重新 attach 后继续处理。已发送到 worker 的按键不能撤销；交接生效之后的旧连接输入不再转发。

终端能力只转发渲染字段，不将 B 的 editor/browser 命令、tmux socket 或未知字段带到 A。连接成功与历史显示分开：只有收到 A 握手 nonce 对应、且 `msgsLoaded > 0` 的 `content_paint` 才显示 `session_ready`。

控制端发送使用该 job 的一次性 `reply`，`reply/resize` 在 A 本机读取协作专用 `daemon/control.key` 完成原生认证；该 key 不经过 B，也不写入返回结果。仅空闲时允许发送；原生拒绝与传输结果不明分别记录为 `failed` / `unknown`。成功 ACK 表示接收，执行结果需读后续历史和状态。终端连接层不缓存或重放输入，原生客户端的新连接也不会触发 Team Cross 自动重发旧文本。

## 账户、模型与权限

模型和推理强度继承来源已保存值；未保存时使用 A 的原生设置。后续由当前输入者在 TUI 选择。页面只在原生历史确认后更新显示，因此尚未发送下一轮时可能显示最近一次已确认值。测试中的 `gpt-5.6-luna` 不写入产品默认值。

路由配置只在 A 的协作配置中保存，采用 0600 权限；仅继承 `ANTHROPIC_*`、明确的模型列表和 HTTP 代理环境字段，不继承 shell 启动变量、个人 hooks 或插件配置。创建时禁用 hooks/插件/其他外部 MCP/Chrome 集成，只加载内置的当前协作批注 MCP，工具范围为 Bash、Read、Write、Edit、Glob、Grep、AskUserQuestion，默认 manual 模式，Bash/Write/Edit 需原生确认。

这套权限是 Claude 自身的权限机制，不等同于 Codex 的操作系统权限配置。当前不承诺 Claude Computer Use、插件、其他自定义 MCP、全局 slash 命令或 OS 沙箱与 Codex 等价；原生 TUI 的设置功能也不构成防止参与者改变权限的安全沙箱。分享前应了解当前实验性范围。API 路由已实测；OAuth、Keychain 登录流程需独立验证。

共享开放期间断开 TUI 保留 worker。结束共享后等待原生 busy/等待交互结束、受理中的请求和直接连接清空，再停止协作专用 job 与 supervisor。仅停止有本次协作所有权标记的配置目录；历史、原目录和新 worktree 继续保留。实时 `status` 优先于可能在中断后保留的 `state=working` 进度标签。状态轮询之间最后一次终端写入有短暂释放保护。本机连接层在 Core 异常退出后，仅清理带匹配所有权记录、无进程监听且 inode 未变化的 Unix socket。

## 实现与验证入口

- [原生进程与恢复](../../../../internal/nativeclaude/process.go)、[离线 fork](../../../../internal/nativeclaude/fork.go)、[历史读取](../../../../internal/nativeclaude/history.go)、[本机 TUI 连接层](../../../../internal/nativeclaude/client.go)。
- [协作适配](../../../../internal/collab/claude.go)、[终端输入网关](../../../../internal/collab/claude_terminal.go)、[交接回归](../../../../internal/collab/claude_test.go)。
- [原生 TUI 实测脚本](../../../../scripts/verify-claude-native.py)、[个人辅助实测脚本](../../../../scripts/verify-claude-assist.py)、[验证门槛](../validation/test-gates.md)。实际结果见 [原生 TUI 验收](../validation/claude-native-tui-2026-09-12.md) 与 [个人辅助 MCP 验收](../validation/claude-assist-2026-09-12.md)。
