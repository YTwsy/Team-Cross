# Team Cross

**Team Cross 让团队在不改变既有工作区和工作流的前提下，安全地分享、审阅、临时移交或继续一个 coding-agent Session，让同事用自己的客户端一起继续。**

Team Cross 是 macOS 上的本地协作工具。发起者选择一个来源会话，创建新的原生会话 fork，再向同事发送临时局域网邀请。会话、模型调用与代码执行保留在发起者的 Mac；参与者可以使用本机 TUI、专用 Desktop，或让自己的 Codex / Claude Code 通过工具辅助协作。

## 产品方向

> Team Cross 让团队在不改变既有工作区和工作流的前提下，安全地分享、审阅、临时移交或继续一个 coding-agent Session。

Team Cross 不替团队选择工作区、任务系统或仓库组织方式。它从不同工作流最终都会触达的
Agent Session 入手，为具体的一次工作增加共同视图、可定位批注、受限控制和可追溯接力，
同时让 Codex、Claude Code 等原生 UI 继续作为用户熟悉的个人执行界面。

一个很实用的产品判断规则：

> 它是否从一个具体 Session 出发，并帮助另一个人理解、审阅、控制或继续这次工作？

## 安装

面向 Apple Silicon、macOS 14 及以上。首个公开版本为 `v0.1.1`；CLI、App 和 Homebrew 使用相同版本。发布入口为 [GitHub Releases](https://github.com/YTwsy/Team-Cross/releases)，安装包和 tap 发布完成后可使用以下方式。

| 方式 | 安装与打开 |
| --- | --- |
| DMG | 打开构建产物中的 DMG，将 `Team Cross.app` 拖入“应用程序”，然后打开 App |
| Homebrew App | `brew install --cask YTwsy/teamcross/team-cross`，然后打开 App 或运行 `teamcross` |
| Homebrew CLI | `brew install YTwsy/teamcross/teamcross`，然后运行 `teamcross` |

安装后的运行不需要 Go、Node 或 pnpm。App 自带完整 CLI 和 Core，不要求 Homebrew。Cask 会同时安装 App 和 `teamcross` 命令；Formula 提供独立 CLI，两者选择一种。切换渠道前退出 Team Cross，通过原渠道卸载再安装另一种，协作数据和工作目录保留。

通过 DMG 安装后，在菜单栏选择“命令行工具…”即可安装 `teamcross`；默认入口为 `/usr/local/bin/teamcross`，需要时通过系统授权。已有 Homebrew 或其他来源的命令不会被覆盖。相同位置升级 App 后，命令自动使用新的内置 CLI；移除入口仍可正常使用 App。使用自定义 shell 的用户需确保命令目录在 `PATH` 中。

从 Formula 切换到 App 时，先退出服务，运行 `brew uninstall --formula --force teamcross` 移除所有已安装的 Formula 版本，再安装 Cask。反向切换使用 `brew uninstall --cask team-cross`；DMG 用户在移除 App 前先从菜单移除命令入口。以上卸载保留协作数据与工作目录。

首次打开如果受到系统提示，先尝试打开 App，再按 [Apple 的说明](https://support.apple.com/zh-cn/102445) 在“系统设置 → 隐私与安全性”中允许打开。构建清单记录每个安装包的签名和公证状态。

查看共享上下文不需要先安装 Codex 或准备本地仓库。发起协作、直接操作或使用 MCP 辅助时，按页面提示使用对应的 Codex 或 Claude Code。原生客户端窗口验证基线为 `codex-cli 0.153.1`、Codex Desktop `26.901.31953`；本轮另用 CLI `0.153.4` 通过真实运行时与网关回归。不同入口的实际覆盖见 [验收记录](docs/agent-wiki/sources/validation/distribution-v0.1.1-2026-09-11.md)，后续版本的实验接口需要重新验证。

## 打开 Team Cross

打开菜单栏 App，或执行：

```sh
teamcross serve
```

`teamcross` 与 `teamcross serve` 都会在后台启动或复用服务，打开浏览器后返回终端。无需传入协作 repo；实际执行目录由所选来源会话决定。重复打开不会创建另一套 Core，关闭浏览器或终端不会停止服务。

同一用户、同一数据目录的新版 App 副本也共用一个菜单栏入口。再次打开其他副本时，打开页面或邀请预览的请求会交给已有 App；第二份 App 仅退出自身，不停止协作服务。升级前仍应按安装说明退出旧版 App。

默认数据目录仍为 `~/Library/Application Support/Team Cross Next`。同一用户、同一规范化数据目录只运行一个 Core。默认端口 `43210` 被占用时自动选择可用端口，以启动输出为准；显式 `--listen` 冲突会报错。`--data-dir`、`--no-open`、`--codex-bin`、`--claude-bin` 和 `--desktop-app` 可用于高级配置，`--repo` 仅影响本机辅助上下文，不选择共享仓库。

```sh
teamcross status --json
teamcross doctor --json
teamcross cli-status --json
teamcross stop
# 有活动协作时，确认中断影响后使用：
teamcross stop --force
```

菜单栏“退出 Team Cross”会停止本机服务；有活动协作时先确认。退出参与者 B 的服务只断开 B，不结束 A 的运行时。会话、代码和 worktree 都保留。版本更新不会自动替换正在运行的 Core；不兼容时先从原版本退出服务。

## 发起协作

1. 在首页选择“发起协作”，搜索并选择 Codex 或实验性的 Claude Code 来源，再选择已有已完成对话的会话。
2. 选择执行目录，查看起点，按需修改自动生成的名称，选择“创建并邀请”。共享失败时保留已创建的协作，在详情重试邀请，不重复创建 fork。

| 目录模式 | 创建时发生什么 | 后续代码在哪里执行 |
| --- | --- | --- |
| 使用原目录 | 保留原 Git 分支、暂存区和全部现有文件，只创建新会话 fork | 来源会话的原目录 |
| 创建新 worktree | 从确认的 `HEAD` 检出独立目录，创建 `codex/collab-<短 ID>` 分支；不复制暂存、未暂存、未跟踪或忽略的内容 | 新 worktree 中对应的来源子目录 |

创建不发送业务指令。来源会话和 Git 起点发生变化时，需要重新查看起点。当前支持普通 Git 仓库；含 submodule 的仓库暂不支持。

Codex 协作创建成功后，发起者可在详情顶部点击“在个人 Codex 中打开”，直接定位新的协作会话，无需先在 Desktop 列表中找到它。创建、同事对话和输入交接不会自动切换你的页面。该入口打开你平时使用的 Desktop；共享期间的操作继续使用直接客户端或辅助工具。结束共享并释放会话后，入口变为“在个人 Codex 中继续”。系统打开请求成功不代表新对话已实时同步。

## 邀请与加入

发起者创建后取得包含 App 链接、邀请码和安装指引的邀请。已安装的接收者点击 `teamcross://join?invite=…`，确认协作名、主机和访问范围后进入上下文页。未安装者先安装，再次点击链接；也可在“加入协作”中粘贴原始邀请码或 App 链接。显式 CLI 加入会自动启动 Core 并直接进入详情：

```sh
teamcross join 'tcx2.…'
```

邀请使用临时 TLS 证书和主机指纹，一小时期限只限制首次加入，且一份邀请只接纳一人。成功加入后使用独立访问凭据，关闭客户端或暂时断线不撤销加入资格，B 重启 Core 后使用已保存凭据重新连接。B 主动离开、A 结束共享或退出/重启 A 的 Core 后，需要新邀请。双方需在同一局域网；发起者需保持 Team Cross 运行。真实睡眠恢复与跨设备网络的验证范围见验收记录。

加入后可以选择两种参与方式，之后也能同时打开另一种入口：

- **直接操作：** 使用本机 Codex TUI，或专用于该协作的独立 Codex Desktop。客户端连接同一个共享 fork，实际执行在发起者主机。同一协作只保留一个直接客户端，切换前关闭原直接客户端。
- **用自己的客户端辅助：** 选择个人 Codex（TUI/Desktop）或 Claude Code TUI，保持自己的普通会话，通过 Team Cross 工具选择目标、读取历史、查看文件和改动、参与输入或添加批注。这些会话拥有各自的本地上下文。

参与方在线状态由 Core 心跳维持，关闭浏览器不会被当作离开。接收者可申请或取消申请输入；发起者明确交接或接回，接收者也可以交还输入。申请本身不会自动交接或启动模型。直接客户端和辅助工具的写入统一检查输入归属，读取可以并行。Codex 协作可通过原生客户端或 MCP 补充、中断、回应审批；Claude 协作的这些操作使用原生 TUI。

界面分别展示启动请求、客户端连接和共享会话就绪状态；连接成功不等于会话已打开。专用 Desktop 第一次打开时可能需要完成登录或跳过引导，之后从左侧打开协作会话。它与原有 Desktop 使用独立应用数据目录；账户登录与客户端偏好在使用者本机处理，共享会话的模型调用仍由发起者运行时承担。此入口面向协作会话的历史、输入、审批与代码操作，Desktop 的其他全局功能不在首轮兼容承诺中。

## Claude Code 原生 TUI（实验性）

创建页选择「Claude Code · 实验性」。最低要求 Claude Code `2.1.268`，A/B 都需不低于该版本；`2.1.268` 是已实测基线，后续正式版本不因版本号不同而拒绝；可在设置页指定 CLI 路径，或启动时传入 `--claude-bin`。A 上一个原生后台 job 承担执行，当前输入者的原生 TUI 处理输入、审批与中断。读取历史、创建 fork 和恢复不会发送业务 prompt。

WebGUI 与辅助工具可查看上下文、添加批注，并在空闲时发送文本。Claude 的补充、中断、审批与模型选择目前需要在原生 TUI 中操作；共享 worker 内置当前协作的批注读取与回复工具，尚未接入 Claude Desktop、Computer Use、插件或其他自定义 MCP，也不承诺与 Codex 的 OS 权限隔离等价。Claude 使用 A 的 API 路由；OAuth / Keychain 登录流程尚未验收。详见 [Claude 接入契约](docs/agent-wiki/sources/decisions/claude-native-tui.md) 与 [2026-09-12 实测记录](docs/agent-wiki/sources/validation/claude-native-tui-2026-09-12.md)。

## 模型与推理强度

创建协作时继承来源会话已保存的模型、Provider 和推理强度；来源缺少相应信息时使用 A 的 Codex 配置。后续在原生客户端中选择模型或推理强度，协作网关保留这些选择，工具未指定设置时沿用共享会话当前配置。

恢复会读取该协作会话最新持久化的设置。详情中的“技术信息”展示 Codex 已确认的当前模型与推理强度；离线时标注为最近确认的模型。普通辅助 TUI/Desktop 的本地模型由使用者自行配置。`gpt-5.6-luna` 仅用于本仓库的真实模型测试。

## 一次性接入 MCP

在“设置与连接”或“用自己的客户端辅助”中选择 Codex / Claude Code，点击对应的“接入本机”按钮。也可以使用该页面给出的准确命令：

```sh
codex mcp add teamcross -- /absolute/path/to/teamcross mcp
claude mcp add --transport stdio --scope user teamcross -- /absolute/path/to/teamcross mcp
```

若使用自定义数据目录，在 `mcp` 后追加 `--data-dir /absolute/path`。配置使用稳定的 Homebrew `opt` 路径或已安装 App 内的绝对路径。Codex TUI 和 Desktop 共用配置；Claude 写入个人 user 范围配置，已有客户端请在 `/mcp` 中重新连接或重新打开。设置页分别显示配置、协议探测，以及 Codex / Claude 各自的实际工具调用证据；MCP 握手不启动 Core，首次工具调用可无浏览器启动 Core。

个人 Claude 与 Codex 复用同一套 Team Cross 工具，可以辅助任一 Provider 的协作；实际写入能力取决于目标协作及当前输入归属。个人对话与模型调用在本机，发往共享会话的任务在 A 执行。对应结果见 [个人 Claude MCP 验收](docs/agent-wiki/sources/validation/claude-assist-2026-09-12.md)。项目同名配置、显式禁用或组织策略可能影响工具加载，页面检测不覆盖其他目录。

可以告诉自己的客户端：“使用 Team Cross 列出协作，查看这次协作的上下文和当前状态。”工具支持：

| 工具 | 用途 |
| --- | --- |
| `list_collaborations` | 定位本机发起或已加入的协作 |
| `get_collaboration` | 查看执行主机、目录、输入者与运行状态 |
| `read_context` | 读取历史、事件、相对路径文件、当前 Git 改动与批注原文引用 |
| `send_input` | 开始一轮或对当前轮补充输入 |
| `interrupt_turn` | 中断指定当前轮 |
| `respond_to_request` | 回应原生审批或用户输入请求 |
| `add_annotation` | 保存整体意见，或带原文与结构化位置的批注；不自动注入模型 |
| `reply_to_annotation` | 回复已有原批注，返回原批注及全部回复；不创建嵌套批注 |

发送成功和执行完成分别表示不同状态。断线或响应超时时先查询实际结果，不自动重复写入。工具不会自动为输入添加参与者身份。

历史默认返回最近 8 轮；工具可将 `nextCursor` 传入 `read_context` 的 `cursor` 读取更早内容。WebGUI 可翻阅更早的一页，完整对话在 Codex 中查看。

WebGUI 的批注面板可以直接填写整体意见。选中对话文字、点击“批注这条消息”，或点击代码行旁的 `+`，会在同一页面的编辑框中展示引用位置和原文。拖选同一文件、同一侧的连续代码行可添加多行批注，不再弹出编辑窗口。不同引用分别保留当前页草稿，支持 `⌘/Ctrl + Enter` 保存。

每条批注下可展开“回复”，按时间顺序讨论原来的意见。回复只有一层，始终归属原批注；人工和共享 Agent 的作者分别显示。收起回复或保存失败会保留当前页草稿，重试使用相同请求标识避免重复保存。上下文刷新保留草稿；浏览器整页刷新或离开页面不保证保留草稿。

点击已保存批注的“查看原位置”可以返回对应对话或代码。代码批注记录文件、行号、改动前后、内容指纹与当时片段；原文变化时保留片段并提示核对，不把旧行号当成当前内容。对话批注绑定原生 turn/item 和选中文字的范围，可在历史分页中查找。

直接 Codex TUI、专用 Codex Desktop 和直接 Claude Code TUI 的共享运行时内置当前协作的批注工具，无需安装个人辅助 MCP。可以直接告诉它：“读取 Team Cross 批注和回复，核对原文后分析，并回复这条批注。”共享工具 `read_annotations` 和 `reply_to_annotation` 只访问当前协作；个人辅助客户端继续使用 `read_context kind=annotations` 和 `reply_to_annotation`。

保存批注或回复不会自动启动或补充模型轮次。模型需在用户提出要求后读取实际上下文，再判断如何处理；共享 Agent 的回复明确标为 Codex 或 Claude Code。新建协作会自动接入；Codex 旧协作恢复运行时后接入。Claude 旧 worker 的原生启动参数不会被改写，需要新建协作使用新工具；本版本创建的 Claude 协作恢复时会保留接入。

## 结束与恢复

“结束共享”关闭同事的原生连接和工具访问，并将输入归还发起者。当前执行与审批完成、专用客户端全部关闭后，Team Cross 会自动退出这次协作的后台 app-server，释放原生会话占用。会话、目录和代码继续保留，不要求提交、导出或填写结论；之后可从 Codex 打开，或在 Team Cross 中恢复并继续。

服务重启后，发起者从协作详情“恢复运行时”，继续已保存的同一个 fork，不再 fork 或创建 worktree。接收者本地保存加入记录与独立访问凭据；只要发起者仍共享且未主动离开，就可以从协作列表重新连接，无需重新使用邀请码。Team Cross 不自动删除任何原目录或协作 worktree。

## 开发与验证

从源码开发需要 Go 1.25.3+、Node 24+、pnpm；构建 App 还需要 macOS Command Line Tools / Swift。

```sh
pnpm install
make build
./bin/teamcross serve
# 构建本地 CLI、App、DMG、校验文件和 tap 定义；不上传产物
make release
make verify-release
make verify-homebrew
```

本地开发构建默认 `0.1.3-dev`，输出位于 `dist/release/0.1.3-dev/`。版本构建使用 `make release VERSION=0.1.3`，要求干净 checkout，并在重建 Web 资源后再次核对。使用相同 `VERSION` 运行两个安装验证目标。构建不上传产物。完整参数、隔离安装和签名入口见 [分发与首次体验](docs/agent-wiki/sources/distribution-and-onboarding.md)。


```sh
# 两个终端：前端 HMR 与 Go 服务
pnpm --filter @teamcross/web dev
./bin/teamcross serve --foreground --dev-web http://127.0.0.1:5173

# 工程门槛
go test ./...
go vet ./...
go test -race ./internal/collab ./internal/mcp ./internal/sharing ./internal/nativecodex
pnpm --filter @teamcross/web check
pnpm --filter @teamcross/web test
pnpm --filter @teamcross/web build
```

修改 Web 后必须更新 `internal/webassets/dist` 并重新构建 Go 二进制。真实模型测试默认跳过，显式使用独立目录启动；全部固定为 `gpt-5.6-luna`：

```sh
TEAMCROSS_LIVE_DIR=/private/tmp/teamcross-fresh-fixture \
  go test ./internal/collab -run '^TestLiveCodex$' -v -count=1 -timeout=7m
```

本轮安装、首次体验、TUI 与未完成项目见 [2026-09-10 验收记录](docs/agent-wiki/sources/validation/onboarding-macos-2026-09-10.md)；前一轮 Desktop 证据见 [2026-09-09 原生协作记录](docs/agent-wiki/sources/validation/native-collaboration-2026-09-09.md)，不自动视为本轮通过。后续修改按 [验证门槛](docs/agent-wiki/sources/validation/test-gates.md) 选择检查。原生 Desktop 指定 WebSocket 入口属于当前本机版本的实验接口，不等同于公开稳定的远程产品合同。

工程知识按 `sources/` 与 `wiki/` 分层维护。首次进入仓库先读 [AGENTS.md](AGENTS.md) 和 [Agent Wiki 索引](docs/agent-wiki/wiki/index.md)，完整导航见 [文档入口](docs/README.md)。

直接阅读：[架构](docs/agent-wiki/sources/architecture.md) · [接口](docs/agent-wiki/sources/protocol.md) · [产品流程](docs/agent-wiki/sources/product-flows.md)。
