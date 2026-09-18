# Team Cross

**Team Cross 让团队在不改变既有工作区和工作流的前提下，安全地分享、审阅、临时移交或继续一个 coding-agent Session，让同事用自己的客户端一起继续。**

Team Cross 是 macOS 上的本地协作工具。可以从选定的会话历史创建只读空间，邀请同事阅读、批注并附上各自的调查；需要共同继续执行时，再创建新的原生会话 fork。空间与可操作会话托管在发起者的 Mac，通过局域网或实验性 Tailcat 连接；每位参与者可以让自己的 Codex / Claude Code 按需读取材料、辅助讨论。执行时还可使用本机 TUI 或 Codex 专用 Desktop 接力。

## 产品方向

> Team Cross 让团队在不改变既有工作区和工作流的前提下，安全地分享、审阅、临时移交或继续一个 coding-agent Session。

Team Cross 不替团队选择工作区、任务系统或仓库组织方式。它从不同工作流最终都会触达的
Agent Session 入手，为具体的一次工作增加共同视图、可定位批注、受限控制和可追溯接力，
同时让 Codex、Claude Code 等原生 UI 继续作为用户熟悉的个人执行界面。

一个很实用的产品判断规则：

> 它是否从一个具体 Session 出发，并帮助另一个人理解、审阅、控制或继续这次工作？

当前源码原型支持三人及以上、独立只读分享、多份会话材料及固定版本引用。空间包含参与者与邀请、已发布材料、批注与回复，以及零个或一个可操作会话；同一邀请链接可供多位同事加入，每位成员身份独立，执行时向指定成员交接输入。产品边界见 [协作空间设计契约](docs/agent-wiki/sources/decisions/collaboration-spaces.md)，本次验证范围见 [只读空间与材料验收](docs/agent-wiki/sources/validation/materials-2026-09-18.md)。这些源码能力尚未发布到安装渠道。

## 安装

面向 Apple Silicon、macOS 14 及以上。首个公开版本为 `v0.1.1`，当前预发布版本为 `v0.1.7-rc.1`；CLI、App 和 Homebrew 产物使用相同版本。GitHub Release 成功后，受保护的后续流程才会通过独立 tap PR 发布 Homebrew 定义。发布入口为 [GitHub Releases](https://github.com/YTwsy/Team-Cross/releases)，对应渠道的 Release 与公共 tap 验证完成后可使用以下方式。

| 方式 | 安装与打开 |
| --- | --- |
| DMG | 打开构建产物中的 DMG，将 `Team Cross.app` 拖入“应用程序”，然后打开 App |
| Homebrew 稳定版 App | `brew install --cask YTwsy/teamcross/team-cross`，然后打开 App 或运行 `teamcross` |
| Homebrew 稳定版 CLI | `brew install YTwsy/teamcross/teamcross`，然后运行 `teamcross` |
| Homebrew RC App | `brew install --cask YTwsy/teamcross/team-cross@rc`，然后打开 App 或运行 `teamcross` |
| Homebrew RC CLI | `brew install YTwsy/teamcross/teamcross-rc`，然后运行 `teamcross` |

安装后的运行不需要 Go、Node 或 pnpm。App 自带完整 CLI 和 Core，不要求 Homebrew。Cask 会同时安装 App 和 `teamcross` 命令；Formula 提供独立 CLI，稳定版与 RC 也使用不同定义。四种 Homebrew 定义都会安装同一个 App 或命令，因此只能选择一种；RC 必须显式安装，不会把稳定通道的普通 `brew upgrade` 自动切换到候选版。切换前退出 Team Cross，通过原渠道卸载再安装另一种，协作数据和工作目录保留。

通过 DMG 安装后，在菜单栏选择“命令行工具…”即可安装 `teamcross`；默认入口为 `/usr/local/bin/teamcross`，需要时通过系统授权。已有 Homebrew 或其他来源的命令不会被覆盖。相同位置升级 App 后，命令自动使用新的内置 CLI；移除入口仍可正常使用 App。使用自定义 shell 的用户需确保命令目录在 `PATH` 中。

从 Formula 切换到 App 时，先退出服务，按实际渠道运行 `brew uninstall --formula --force teamcross` 或 `brew uninstall --formula --force teamcross-rc`，再安装 Cask。反向切换使用 `brew uninstall --cask team-cross` 或 `brew uninstall --cask team-cross@rc`；DMG 用户在移除 App 前先从菜单移除命令入口。以上卸载保留协作数据与工作目录。

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

## 分享与审阅

1. 首页“发起协作”选择“先分享讨论”，搜索并明确选中本机 Session，点击“选择公开范围”。这一步只固定本机历史，不创建 fork，不要求 Git 目录，也不上传。
2. 选择“从这次提问开始”和“公开到这里为止”，或明确选择全部已结束对话。每轮的提问、回复及已保存工具过程一起公开；“建议先读哪里”只是阅读导航。
3. 点击“预览实际公开内容”，核对正文、工具内容和未导出说明，再“创建只读空间并发布”。正在进行的轮次和之后的新内容不会自动公开。
4. 选择 LAN 或 Tailcat，生成一份可供多位同事使用的邀请链接。成员均可“发布会话材料”，发布自己的一份或多份 Session；批注与回复可以同时引用多份材料的固定版本。
5. “阅读材料”才读取正文；个人 Agent 先 `list_materials`，再 `read_material`。作者“发布新版本”默认沿用上次公开范围，必须再次预览；旧批注仍指向旧版本。撤回停止继续读取，已读取副本和历史引用不能召回。

只读空间不开放原生操作或工作目录。需要执行时由发起者选择“启用共同执行”，再确定来源、目录与固定权限模式。空间保留原链接、成员、材料和讨论；在成员列表向需要参与执行的人“开放执行访问”，共享完整原生历史与工作目录，再单独交接输入。原只读链接继续只授予材料和讨论访问。

只读空间始终提供“关闭空间”，即使尚未生成邀请或无人加入也可以关闭。关闭状态会保存，材料和讨论保留；之后可“重新开放空间”。

个人 Agent 和终端均可完成发布。当前会话先用 `get_current_source` 核对身份，或明确从来源列表选择；不猜测最近会话。CLI 示例中的 ID、边界和哈希都应替换为上一步的实际结果：

```sh
teamcross sources --provider codex --json
teamcross freeze --provider codex --source <来源UUID> --json
teamcross publication-preview --draft <草稿ID> --title '接口调查' --start-turn <首轮ID> --end-turn <末轮ID> --json
teamcross space --title '接口调查' --request-id <新的空间UUID> --json
teamcross publish --id <空间ID> --preview-id <预览ID> --preview-hash <预览hash> --request-id <新的发布UUID> --json
teamcross invite --id <空间ID> --transport lan --request-id <邀请操作ID> --json
teamcross materials --id <空间ID> --json
teamcross read-material --id <空间ID> --material <材料ID> --version 1 --json
```

发布结果不明时用 `publication-status --id <空间ID> --request-id <原发布UUID>` 查询；显式重试保留原请求 ID 和预览。更新增加 `--material <材料ID> --base-version <当前版本>`，不会覆盖旧版。

## 共同继续执行

1. 在首页选择“发起协作 → 直接一起执行”，搜索并选择 Codex 或实验性的 Claude Code 来源，再选择已有已完成对话的会话。
2. 选择执行目录、协作模式和连接方式，查看起点，按需修改自动生成的名称，选择“创建并邀请”。第一版由用户在“局域网”和“Tailcat（实验性）”之间明确二选一，不自动降级或切换。共享失败时保留已创建的协作，在详情重试邀请，不重复创建 fork。

| 目录模式 | 创建时发生什么 | 后续代码在哪里执行 |
| --- | --- | --- |
| 使用原目录 | 保留原 Git 分支、暂存区和全部现有文件，只创建新会话 fork | 来源会话的原目录 |
| 创建新 worktree | 从确认的 `HEAD` 检出独立目录，创建 `codex/collab-<短 ID>` 分支；不复制暂存、未暂存、未跟踪或忽略的内容 | 新 worktree 中对应的来源子目录 |

协作模式在创建时确定，创建后不提供切换，恢复沿用同一模式。**受限模式**为默认，延续 Team Cross 的工具与权限限制；**信任模式**支持 Codex 和实验性的 Claude Code，沿用邀请者主机上的原生配置与权限，包括 MCP、插件、hooks、网络、命令，以及原生已启用的浏览器和电脑控制。工具使用邀请者的服务授权，操作可能访问工作目录之外的数据；创建时加载个人工具也可能触发 hooks。信任模式继承实际可用环境，仍遵循原生客户端、组织策略与系统授权，不把未安装或未授权的能力自动打开。详情和邀请确认页显示模式。完整边界见 [协作模式](docs/agent-wiki/sources/decisions/runtime-modes.md)。

创建不发送业务指令。来源会话和 Git 起点发生变化时，需要重新查看起点。当前支持普通 Git 仓库；含 submodule 的仓库暂不支持。

也可以从个人 Agent 的 Team Cross 工具或终端发起。先选择明确的本机来源、预览起点，再创建并邀请：

```sh
teamcross sources --provider codex --search '会话名称' --json
teamcross preview --provider codex --source <来源UUID> --workspace existing --json
teamcross share --provider codex --source <来源UUID> --workspace existing \
  --request-id <预览返回的requestId> --preview-hash <previewHash> --transport lan --json
```

`--workspace worktree` 创建独立工作目录，`--runtime-mode trusted` 显式选择信任模式，预览和创建必须使用相同设置；默认受限。`--provider claude` 选择实验性 Claude 来源，与调用工具的个人客户端无关。`sources` 支持 `--cursor` 翻页。以上命令按需启动 Core，不打开浏览器。

也可以告诉已接入 MCP 的个人 Agent：“把当前 Session 分享出去，使用原目录、受限模式和局域网。”工具先核对原生客户端传来的当前会话身份、预览现场，再登记分享请求；**本轮答复结束后** Core 才创建 fork 和邀请，使 fork 包含本轮完整对话。Agent 先返回请求 ID，下一轮通过 `get_share_request` 查询，成功后用 `create_invitation` 取回邀请。登记成功不等于邀请已生成，不要让 Agent 在同一轮循环等待自己结束。

终端可用 `teamcross share-status --id <请求ID> --json` 查询，或用 `teamcross cancel-share --id <请求ID>` 取消尚在等待的请求。已开始创建则保留实际结果；来源轮次或 Git 起点变化时停止，不自动换成新起点。Core 停止后未完成请求标记为中断，不在重启后自动创建。无法核对原生调用身份的客户端仍可走上面的明确来源流程，不能用“最近会话”替代“当前会话”。具体版本证据见 [个人 Agent 与 CLI 验证](docs/agent-wiki/sources/validation/agent-cli-collaboration-2026-09-16.md)。

`create` 只创建协作，`invite --id <协作ID> --transport lan|tailcat` 只生成或取回当前可复用链接；`share` 顺序完成两者。邀请失败返回已创建的协作及恢复提示，后续只运行 `invite`，不换 requestId 重新创建。相同 requestId 的创建重试复用原协作；结果不明时先查询 `collaborations --id <requestId>`。邀请链接与邀请码供用户自行转交，不自动发送给同事。

Codex 协作创建成功后，发起者可在详情顶部点击“在个人 Codex 中打开”，直接定位新的协作会话，无需先在 Desktop 列表中找到它。创建、同事对话和输入交接不会自动切换你的页面。该入口打开你平时使用的 Desktop；共享期间的操作继续使用直接客户端或辅助工具。结束共享并释放会话后，入口变为“在个人 Codex 中继续”。系统打开请求成功不代表新对话已实时同步。

## 邀请与加入

发起者创建后取得包含 App 链接、邀请码和安装指引的邀请。已安装的接收者点击 `teamcross://join?invite=…`，确认协作名、主机和访问范围后进入上下文页。未安装者先安装，再次点击链接；也可在“加入协作”中粘贴原始邀请码或 App 链接。显式 CLI 加入会自动启动 Core 并直接进入详情：

```sh
teamcross join 'tcx3.…'
```

终端可先用 `teamcross inspect-invitation --stdin --json` 读取邀请声明的名称、主机和模式，再通过 `teamcross join --stdin --no-open --json` 加入；两次分别输入同一份邀请。预览仅解析本地内容，实际加入才验证远端。加入 JSON 返回本机协作 `id`。个人 MCP 对应 `preview_invitation` 与 `join_collaboration`。

邀请使用临时 TLS 证书和主机指纹。同一链接可供多人加入，有效至关闭链接、重置链接或本次共享结束；每位加入者使用独立访问凭据。关闭客户端或暂时断线不撤销资格，B 重启 Core 后使用已保存凭据重新连接。B 主动离开后可通过仍有效的链接重新加入；A 结束共享或退出/重启 Core 后，需要重新开放并生成新链接。发起者需保持 Team Cross 运行。

连接方式有明确边界：

- **局域网：** 双方需要处于可互访的同一局域网；连接不依赖 Tailcat 公共服务。
- **Tailcat（实验性）：** 双方无需 Tailscale 账户，也不安装系统 TUN。Tailcat 通过 DERP 完成发现和初始连接，条件允许时使用点对点 UDP，否则可继续经 DERP 中继；可达性和质量依赖双方网络及所用 DERP。Tailcat 地址、预共享密钥、Team Cross 邀请 secret 和 TLS 指纹一起包含在邀请码中，因此邀请码应按秘密处理。

两种方式复用相同的链接加入、独立成员凭据、TLS 1.3 与 SPKI 指纹校验；不会先尝试局域网再暗中回退 Tailcat。当前同机实网测试已经覆盖 Tailcat 建链、加入、成员重连和直接客户端 WebSocket 桥接，但不等于两台 Mac、不同网络或强制 DERP 中继已经验收；完整范围见 [Tailcat 验收记录](docs/agent-wiki/sources/validation/tailcat-transport-2026-09-15.md)。

加入后可以选择两种参与方式，之后也能同时打开另一种入口：

- **直接操作：** 使用本机 Codex TUI，或专用于该协作的独立 Codex Desktop。客户端连接同一个共享 fork，实际执行在发起者主机。同一协作只保留一个直接客户端，切换前关闭原直接客户端。
- **用自己的客户端辅助：** 选择个人 Codex（TUI/Desktop）或 Claude Code TUI，保持自己的普通会话，通过 Team Cross 工具选择目标、读取历史、查看文件和改动、参与输入或添加批注。这些会话拥有各自的本地上下文。

多位同事使用同一链接分别加入。发起者可“关闭链接加入”或“重置邀请链接”，两者均不影响已有成员；也可以单独移除成员。持有效链接的人可以转发或重新加入，因此移除成员只撤销当前凭据，阻止其再次加入需关闭或重置链接。输入只交给选定成员；移除当前输入者会接回控制，不自动中断模型，其他成员继续参与。

参与方在线状态由各自的 Core 心跳维持，关闭浏览器不会被当作离开。接收者可申请或取消申请输入；发起者明确交接或接回，接收者也可以交还输入。申请本身不会自动交接或启动模型。直接客户端和辅助工具的写入统一检查输入归属，读取可以并行。Codex 协作可通过原生客户端或 MCP 补充、中断、回应审批；Claude 协作的这些操作使用原生 TUI。

界面分别展示启动请求、客户端连接和共享会话就绪状态；连接成功不等于会话已打开。专用 Desktop 第一次打开时可能需要完成登录或跳过引导，之后从左侧打开协作会话。它与原有 Desktop 使用独立应用数据目录；账户登录与客户端偏好在使用者本机处理，共享会话的模型调用仍由发起者运行时承担。此入口面向协作会话的历史、输入、审批与代码操作，Desktop 的其他全局功能不在首轮兼容承诺中。

## Claude Code 原生 TUI（实验性）

创建页选择「Claude Code · 实验性」。最低要求 Claude Code `2.1.268`，A/B 都需不低于该版本；`2.1.268` 是旧实现的已实测基线，后续正式版本不因版本号不同而拒绝；可在设置页指定 CLI 路径，或启动时传入 `--claude-bin`。A 上一个原生后台 job 承担执行，当前输入者的原生 TUI 处理输入、审批与中断。读取历史、创建 fork 和恢复不会发送业务 prompt。

Claude 协作通过原生 `--fork-session` 创建新会话。信任模式直接由 Claude 在个人配置目录管理新 fork；受限模式在发生首次业务输入并持久化后，由 Team Cross 只把这个新 fork 的单个 transcript 发布到 A 当前个人 Claude home 的 `projects`，它会像普通原生 fork 一样出现在 Claude Code CLI/TUI `/resume`，结束共享后可从个人历史继续。零输入 fork 可能尚未落盘，也不保证释放后可恢复；Team Cross 不把这个情况当成创建失败。受限模式下，每次协作使用独立 `claude-runtime` 作为 worker 配置目录，其中只放所选来源的逐字节快照、本次 fork、受限 settings、MCP、认证快照、daemon/job 和所有权状态；不会把整个个人 `projects` 暴露给协作 worker。Team Cross 不自行构造 Provider JSONL，也不把个人设置、插件、已有历史或认证文件作为写入目标；受限模式下，Team Cross 在个人 history 中只新增用户明确创建的 fork；信任模式的原生工具可能依授权修改个人资源。B 的直接 TUI 不保存 A 的 Provider 历史，Claude Desktop、Web 与 Cloud 的历史也不在本功能范围。项目仍处于 prerelease，旧独立 `claude-home` 协作不迁移，需要重新创建。

WebGUI 与辅助工具可查看已持久化的上下文、添加批注，并在空闲时发送文本。Claude 的补充、中断、审批与模型选择目前需要在原生 TUI 中操作；共享 worker 内置当前协作的批注读取与回复工具。信任模式在邀请者原生配置目录中创建和运行新 fork，复用个人 MCP、插件、hooks、权限及可用工具；受限模式继续关闭这些扩展。Claude Desktop 仍未接入，Claude 权限机制不等同于 Codex 的 OS 权限隔离。Claude 使用 A 的 API 路由；OAuth / Keychain 登录流程尚未验收。详见 [Claude 接入契约](docs/agent-wiki/sources/decisions/claude-native-tui.md) 与 [2026-09-14 个人 CLI/TUI 历史验收](docs/agent-wiki/sources/validation/claude-personal-history-2026-09-14.md)；[2026-09-12 实测记录](docs/agent-wiki/sources/validation/claude-native-tui-2026-09-12.md)属于切换前的旧实现。

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
| `list_source_sessions` / `preview_collaboration` | 选择本机来源、预览目录与固定协作模式 |
| `create_collaboration` / `create_invitation` | 根据已确认起点创建 fork，并单独生成或取回邀请 |
| `get_current_source` / `preview_current_share` / `share_current_session` | 核对并预览当前 Session，登记本轮结束后的分享 |
| `get_share_request` / `cancel_share_request` | 查询分享请求，或取消仍在等待的请求 |
| `preview_invitation` / `join_collaboration` | 预览并加入用户选定的邀请 |
| `open_client` | 获得输入权后打开新 TUI 或专用 Desktop 窗口 |
| `end_sharing` / `leave_collaboration` / `resume_collaboration` | 结束共享、主动离开或恢复同一协作运行时 |
| `request_input` / `cancel_input_request` | 接收者申请或取消输入，不自动交接 |
| `handoff_input` / `reclaim_input` / `return_input` | 发起者交接或接回，接收者交还输入；传入详情中的 `epoch` |
| `read_context` | 读取历史、事件、相对路径文件、当前 Git 改动与批注原文引用 |
| `send_input` | 开始一轮或对当前轮补充输入 |
| `interrupt_turn` | 中断指定当前轮 |
| `respond_to_request` | 回应原生审批或用户输入请求 |
| `add_annotation` | 保存整体意见，或带原文与结构化位置的批注；不自动注入模型 |
| `reply_to_annotation` | 回复已有原批注，返回原批注及全部回复；不创建嵌套批注 |

发送成功和执行完成分别表示不同状态。断线或响应超时时先查询实际结果，不自动重复写入。工具不会自动为输入添加参与者身份。

终端也可以查询协作和管理输入，无需打开浏览器：

```sh
teamcross collaborations --json
teamcross collaborations --id <协作ID> --json
teamcross input request --id <协作ID> --epoch <详情中的epoch>
teamcross input handoff --id <协作ID> --member <成员ID> --epoch <最新epoch>
```

`input` 还支持 `cancel`、`reclaim`、`return`。每次先查询详情，再传入看到的输入状态版本；过期操作会被拒绝，结果不明时先查询，不自动重放。发起者只能向已加入的同事交出输入，交出和交还需等待当前轮结束；接回输入不自动中断正在执行的轮次。交接关闭旧直接客户端，辅助工具仍可读取上下文。

取得输入后，用 `teamcross open --id <协作ID> --client tui` 打开新终端窗口，或 `--client desktop` 打开 Codex 专用窗口；`--print-command` 只取得启动计划。Claude 仅支持 TUI。启动请求成功后仍需查看 `clientState`，不把它等同于会话已打开。`end --id` 由发起者结束共享，`leave --id` 由接收者离开，`resume --id` 恢复已有 fork；这些动作保留原生会话与工作目录。

历史默认返回最近 8 轮；工具可将 `nextCursor` 传入 `read_context` 的 `cursor` 读取更早内容。WebGUI 可翻阅更早的对话，按轮次目录导航；已发布材料与协作上下文支持 Markdown 排版、代码高亮、工具过程折叠、专注阅读及原文切换。对话有新内容时先提示，点击后更新当前阅读内容。

WebGUI 的批注面板可以直接填写整体意见。选中对话文字、点击“批注这条消息”，或点击代码行旁的 `+`，会在原文旁的编辑卡中展示引用位置和原文；窄屏在消息下方展开。拖选同一文件、同一侧的连续代码行可添加多行批注。不同引用分别保留当前页草稿，关闭或按 Escape 收起后可以继续，支持 `⌘/Ctrl + Enter` 保存。已保存的原文批注会高亮，并提供就地阅读讨论的入口。

批注与回复中的“引用材料”可按标题或作者搜索，默认展示最新版本，历史版本按需展开；选中后显示固定版本标签，可以移除。点击“预览”才读取正文片段。

每条批注下可展开“回复”，按时间顺序讨论原来的意见。回复只有一层，始终归属原批注；人工和共享 Agent 的作者分别显示。收起回复或保存失败会保留当前页草稿，重试使用相同请求标识避免重复保存。上下文刷新保留草稿；浏览器整页刷新或离开页面不保证保留草稿。

点击已保存批注的“查看原位置”可以返回对应对话或代码。代码批注记录文件、行号、改动前后、内容指纹与当时片段；原文变化时保留片段并提示核对，不把旧行号当成当前内容。对话批注绑定原生 turn/item 和选中文字的范围，可在历史分页中查找。

直接 Codex TUI、专用 Codex Desktop 和直接 Claude Code TUI 的共享运行时内置当前空间的批注与材料工具，无需安装个人辅助 MCP。可以直接告诉它：“读取 Team Cross 批注和回复，按引用读取已发布材料，核对原文后回复这条批注。”共享工具 `read_annotations`、`reply_to_annotation`、`list_materials`、`read_material` 只访问当前空间；个人辅助客户端还可从自己的本机来源发布材料。

保存批注或回复不会自动启动或补充模型轮次。模型需在用户提出要求后读取实际上下文，再判断如何处理；共享 Agent 的回复明确标为 Codex 或 Claude Code。新建协作会自动接入；Codex 旧协作恢复运行时后接入。Claude 旧 worker 的原生启动参数不会被改写，需要新建协作使用新工具；本版本创建的 Claude 协作恢复时会保留接入。

## 结束与恢复

只读空间结束共享后，材料与讨论仍保存在托管端，重新邀请即可继续；不需要恢复模型运行时。启用执行和重置链接保留同次共享内的成员身份；结束整个共享后重新加入会获得新身份，不能用新身份更新旧身份发布的材料，可以重新发布独立材料。以下原生恢复规则仅适用于带可操作会话的空间。

“结束共享”关闭同事的原生连接和工具访问，并将输入归还发起者。当前执行与审批完成、专用客户端全部关闭后，Team Cross 会自动退出这次协作的后台 app-server，释放原生会话占用。会话、目录和代码继续保留，不要求提交、导出或填写结论；之后可从 Codex 打开，或在 Team Cross 中恢复并继续。

服务重启后，发起者从协作详情“恢复运行时”，继续已保存的同一个 fork，不再 fork 或创建 worktree。接收者本地保存加入记录与独立访问凭据；只要发起者仍共享且未主动离开，就可以从协作列表重新连接，无需重新使用邀请码。Team Cross 不自动删除任何原目录或协作 worktree。

## 开发与验证

从源码开发需要 Go 1.27.1+、Node 24+、pnpm；构建 App 还需要 macOS Command Line Tools / Swift。

```sh
pnpm install
make build
./bin/teamcross serve
# 构建本地 CLI、App、DMG、校验文件和 tap 定义；不上传产物
make release
make verify-release
make verify-homebrew
```

本地开发构建默认 `0.1.7-dev`，输出位于 `dist/release/0.1.7-dev/`。版本构建使用 `make release VERSION=0.1.7-rc.1`，要求干净 checkout，并在重建 Web 资源后再次核对。使用相同 `VERSION` 运行两个安装验证目标。构建不上传产物。

GitHub 对指向 `main` 的 PR 和 `main` push 运行 Go、race、Web 与 Homebrew 定义工程门槛。`Unsigned macOS release` workflow 可以手动构建、验证和保存一个不发布的 `X.Y.Z` 或 `X.Y.Z-rc.N` arm64 产物；推送可从 `origin/main` 到达的同版本 annotated tag 时，它才会生成 provenance 并创建 GitHub Release。RC 标记为 Pre-release，正式版本标记为 Latest；在取得 Developer ID 前，两者都明确使用 ad-hoc 签名且未经 Apple 公证。Release 创建后，独立 Homebrew workflow 重新下载公开资产、验证 checksum 与 attestation、隔离安装对应定义，再使用只覆盖 tap 仓库的短期 GitHub App token 创建 PR；tap CI 通过、自动合并及公共安装 smoke 完成后才更新 Release 中的 Homebrew 状态。Tailcat 公网 smoke 和两台 Mac 验收仍不在托管发布流水线中自动运行。完整参数、证据边界和发布顺序见 [分发与首次体验](docs/agent-wiki/sources/distribution-and-onboarding.md)。


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

为另一位同事生成邀请可使用 `teamcross invite --id <协作ID> --transport lan --request-id <新UUID>`，重试保留相同请求 ID。多人验证范围见 [2026-09-18 多成员基础](docs/agent-wiki/sources/validation/multi-member-2026-09-18.md)。
