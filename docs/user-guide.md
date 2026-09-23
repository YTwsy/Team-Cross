# Team Cross 使用指南

[返回 README](../README.md) · [文档导航](README.md) · [开发与验证](development.md)

这里保存安装、分享、加入、原生客户端、MCP 与恢复的详细操作。产品概览和当前下载入口见 [README](../README.md)；具体版本的变化与升级限制以 [GitHub Releases](https://github.com/YTwsy/Team-Cross/releases) 为准。

- [安装与升级](#安装与升级) · [打开与停止服务](#打开-team-cross)
- [分享与审阅](#分享与审阅) · [共同继续执行](#共同继续执行) · [邀请与加入](#邀请与加入)
- [Claude Code 原生 TUI](#claude-code-原生-tui实验性) · [模型与推理强度](#模型与推理强度)
- [个人 Agent 与 MCP](#个人-agent-与-mcp) · [阅读与批注](#阅读与批注) · [结束与恢复](#结束与恢复)

下文 A 指发起者及其 Mac，B 指一位接收者；同一空间可以有多位接收者，各自使用独立成员身份。

## 安装与升级

面向 Apple Silicon、macOS 14 及以上。当前正式版 `v0.2.3` 的 DMG、CLI 与稳定 Homebrew 入口见 [README 安装](../README.md#安装)。安装后运行不需要 Go、Node 或 pnpm。

App 自带完整 CLI 和 Core，不要求 Homebrew。Cask 同时安装 App 和 `teamcross` 命令，Formula 提供独立 CLI。App 与 CLI、稳定命名与 RC 命名的 Homebrew 定义会占用同一 App 或命令入口，因此只选一种。RC 必须显式安装，普通 `brew upgrade` 不会把无后缀渠道切换到 RC。

通过 DMG 安装后，在菜单栏选择“命令行工具…”即可安装 `teamcross`；默认入口为 `/usr/local/bin/teamcross`，需要时通过系统授权。已有 Homebrew 或其他来源的命令不会被覆盖。相同位置升级 App 后，命令自动使用新的内置 CLI；移除入口仍可正常使用 App。使用自定义 shell 的用户需确保命令目录在 `PATH` 中。

升级或切换渠道前，先退出正在运行的 Team Cross，再通过原渠道更新或卸载。安装程序保留协作数据和工作目录，但 `v0.2.3` 不加载或迁移旧 `schema:2` 材料，旧数据仍保留在磁盘。详细变化见 [Release](https://github.com/YTwsy/Team-Cross/releases/tag/v0.2.3)。

从 Formula 切换到 App 时，退出服务后按实际渠道运行 `brew uninstall --formula --force teamcross` 或 `brew uninstall --formula --force teamcross-rc`，再安装 Cask。反向切换使用 `brew uninstall --cask team-cross` 或 `brew uninstall --cask team-cross@rc`；DMG 用户在移除 App 前先从菜单移除命令入口。以上卸载保留协作数据与工作目录。

无后缀定义 `YTwsy/teamcross/teamcross`、`YTwsy/teamcross/team-cross` 与 RC 定义分别维护，不代表相同版本。体验当前材料与讨论功能请使用 README 的稳定版入口；具体渠道版本和安装状态以对应 Release 为准。

当前安装包使用 ad-hoc 签名，未经 Developer ID 签名或 Apple 公证。首次打开如果受到系统提示，先尝试打开 App，再按 [Apple 的说明](https://support.apple.com/zh-cn/102445) 在“系统设置 → 隐私与安全性”中允许打开。构建清单记录每个安装包的签名和公证状态。

阅读已发布材料不需要先安装 Codex 或准备本地仓库。发起协作、直接操作或使用 MCP 辅助时，按页面提示使用对应的 Codex 或 Claude Code。客户端接入范围见 [原生客户端与模型](agent-wiki/wiki/concepts/native-clients-and-models.md)，各版本的实际测试范围见 [验收导航](agent-wiki/wiki/concepts/validation-gates.md)。

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
2. 点击目录定位正文，在对应轮次选择“从这轮开始”和“到这轮结束”，也可使用“全部已结束对话”或“最近 3 轮”。每轮的提问、回复及已保存工具过程一起分享，起止之间保持连续；点击目录不会改变范围。“建议同事从这轮读起”只设置阅读导航，范围缩小后若建议起点被排除，会清除该建议并提示。
3. 顶部范围摘要和阅读操作随滚动吸顶；点击其中的“预览分享内容”，在确认页设置分享标题、核对选中范围的正文、工具内容和导出说明，再“创建只读空间并发布”。“N 处导出说明”可展开查看含义，具体缺失原因在相应轮次正文中，计数不代表缺少的轮数。长工具输出可按需读取，折叠内容也属于分享范围。返回调整范围会保留此前的阅读位置和展开状态；确认页始终从分享范围开头展示。正在进行的轮次和之后的新内容不会自动公开。
4. 选择 LAN 或 Tailcat，生成一份可供多位同事使用的邀请链接。成员均可“发布会话材料”，发布自己的一份或多份 Session；批注与回复可以同时引用多份材料的固定版本。
5. “阅读材料”才读取正文；个人 Agent 先 `list_materials`，再 `read_material`。默认流完整返回对话、折叠长工具输出；需要全文时按返回的 `turnId + itemId` 单独分页读取。作者“发布新版本”默认沿用上次公开范围，必须再次预览；旧批注仍指向旧版本。撤回停止继续读取，已读取副本和历史引用不能召回。

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

协作模式在创建时确定，创建后不提供切换，恢复沿用同一模式。**受限模式**为默认，延续 Team Cross 的工具与权限限制；**信任模式**支持 Codex 和实验性的 Claude Code，沿用邀请者主机上的原生配置与权限，包括 MCP、插件、hooks、网络、命令，以及原生已启用的浏览器和电脑控制。工具使用邀请者的服务授权，操作可能访问工作目录之外的数据；创建时加载个人工具也可能触发 hooks。信任模式继承实际可用环境，仍遵循原生客户端、组织策略与系统授权，不把未安装或未授权的能力自动打开。详情和邀请确认页显示模式。完整边界见 [协作模式](agent-wiki/sources/decisions/runtime-modes.md)。

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

终端可用 `teamcross share-status --id <请求ID> --json` 查询，或用 `teamcross cancel-share --id <请求ID>` 取消尚在等待的请求。已开始创建则保留实际结果；来源轮次或 Git 起点变化时停止，不自动换成新起点。Core 停止后未完成请求标记为中断，不在重启后自动创建。无法核对原生调用身份的客户端仍可走上面的明确来源流程，不能用“最近会话”替代“当前会话”。具体版本证据见 [个人 Agent 与 CLI 验证](agent-wiki/sources/validation/agent-cli-collaboration-2026-09-16.md)。

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

两种方式复用相同的链接加入、独立成员凭据、TLS 1.3 与 SPKI 指纹校验；不会先尝试局域网再暗中回退 Tailcat。当前同机实网测试已经覆盖 Tailcat 建链、加入、成员重连和直接客户端 WebSocket 桥接，但不等于两台 Mac、不同网络或强制 DERP 中继已经验收；完整范围见 [Tailcat 验收记录](agent-wiki/sources/validation/tailcat-transport-2026-09-15.md)。

只读空间的成员可以阅读、发布材料和参与讨论。空间启用执行并向你开放执行访问后，还可以选择以下入口：

- **直接操作：** 取得输入权后，使用本机 Codex TUI，或专用于该协作的独立 Codex Desktop。客户端连接同一个共享 fork，实际执行在发起者主机。同一协作只保留一个直接客户端，切换前关闭原直接客户端。Claude 协作使用实验性的原生 TUI。
- **用自己的客户端辅助：** 选择个人 Codex（TUI/Desktop）或 Claude Code TUI，保持自己的普通会话，通过 Team Cross 工具选择目标、读取历史、查看文件和改动、参与输入或添加批注。这些会话拥有各自的本地上下文。

多位同事使用同一链接分别加入。发起者可“关闭链接加入”或“重置邀请链接”，两者均不影响已有成员；也可以单独移除成员。持有效链接的人可以转发或重新加入，因此移除成员只撤销当前凭据，阻止其再次加入需关闭或重置链接。输入只交给选定成员；移除当前输入者会接回控制，不自动中断模型，其他成员继续参与。

参与方在线状态由各自的 Core 心跳维持，关闭浏览器不会被当作离开。接收者可申请或取消申请输入；发起者明确交接或接回，接收者也可以交还输入。申请本身不会自动交接或启动模型。直接客户端和辅助工具的写入统一检查输入归属，读取可以并行。Codex 协作可通过原生客户端或 MCP 补充、中断、回应审批；Claude 协作的这些操作使用原生 TUI。

界面分别展示启动请求、客户端连接和共享会话就绪状态；连接成功不等于会话已打开。专用 Desktop 第一次打开时可能需要完成登录或跳过引导，之后从左侧打开协作会话。它与原有 Desktop 使用独立应用数据目录；账户登录与客户端偏好在使用者本机处理，共享会话的模型调用仍由发起者运行时承担。此入口面向协作会话的历史、输入、审批与代码操作，Desktop 的其他全局功能不在首轮兼容承诺中。

## Claude Code 原生 TUI（实验性）

创建页选择「Claude Code · 实验性」。最低要求 Claude Code `2.1.268`，A/B 都需不低于该版本；`2.1.268` 是旧实现的已实测基线，后续正式版本不因版本号不同而拒绝；可在设置页指定 CLI 路径，或启动时传入 `--claude-bin`。A 上一个原生后台 job 承担执行，当前输入者的原生 TUI 处理输入、审批与中断。读取历史、创建 fork 和恢复不会发送业务 prompt。

Claude 协作通过原生 `--fork-session` 创建新会话。信任模式直接由 Claude 在个人配置目录管理新 fork；受限模式在发生首次业务输入并持久化后，由 Team Cross 只把这个新 fork 的单个 transcript 发布到 A 当前个人 Claude home 的 `projects`，它会像普通原生 fork 一样出现在 Claude Code CLI/TUI `/resume`，结束共享后可从个人历史继续。零输入 fork 可能尚未落盘，也不保证释放后可恢复；Team Cross 不把这个情况当成创建失败。受限模式下，每次协作使用独立 `claude-runtime` 作为 worker 配置目录，其中只放所选来源的逐字节快照、本次 fork、受限 settings、MCP、认证快照、daemon/job 和所有权状态；不会把整个个人 `projects` 暴露给协作 worker。Team Cross 不自行构造 Provider JSONL，也不把个人设置、插件、已有历史或认证文件作为写入目标；受限模式下，Team Cross 在个人 history 中只新增用户明确创建的 fork；信任模式的原生工具可能依授权修改个人资源。B 的直接 TUI 不保存 A 的 Provider 历史，Claude Desktop、Web 与 Cloud 的历史也不在本功能范围。项目仍处于 prerelease，旧独立 `claude-home` 协作不迁移，需要重新创建。

WebGUI 与辅助工具可查看已持久化的上下文、添加批注，并在空闲时发送文本。Claude 的补充、中断、审批与模型选择目前需要在原生 TUI 中操作；共享 worker 内置当前协作的批注读取与回复工具。信任模式在邀请者原生配置目录中创建和运行新 fork，复用个人 MCP、插件、hooks、权限及可用工具；受限模式继续关闭这些扩展。Claude Desktop 仍未接入，Claude 权限机制不等同于 Codex 的 OS 权限隔离。Claude 使用 A 的 API 路由；OAuth / Keychain 登录流程尚未验收。详见 [Claude 接入契约](agent-wiki/sources/decisions/claude-native-tui.md) 与 [2026-09-14 个人 CLI/TUI 历史验收](agent-wiki/sources/validation/claude-personal-history-2026-09-14.md)；[2026-09-12 实测记录](agent-wiki/sources/validation/claude-native-tui-2026-09-12.md)属于切换前的旧实现。

## 模型与推理强度

创建协作时继承来源会话已保存的模型、Provider 和推理强度；来源缺少相应信息时使用 A 的 Codex 配置。后续在原生客户端中选择模型或推理强度，协作网关保留这些选择，工具未指定设置时沿用共享会话当前配置。

恢复会读取该协作会话最新持久化的设置。详情中的“技术信息”展示 Codex 已确认的当前模型与推理强度；离线时标注为最近确认的模型。普通辅助 TUI/Desktop 的本地模型由使用者自行配置。`gpt-5.6-luna` 仅用于本仓库的真实模型测试。

## 个人 Agent 与 MCP

在“设置与连接”或“用自己的客户端辅助”中选择 Codex / Claude Code，点击对应的“接入本机”按钮。也可以使用该页面给出的准确命令：

```sh
codex mcp add teamcross -- /absolute/path/to/teamcross mcp
claude mcp add --transport stdio --scope user teamcross -- /absolute/path/to/teamcross mcp
```

若使用自定义数据目录，在 `mcp` 后追加 `--data-dir /absolute/path`。配置使用稳定的 Homebrew `opt` 路径或已安装 App 内的绝对路径。Codex TUI 和 Desktop 共用配置；Claude 写入个人 user 范围配置，已有客户端请在 `/mcp` 中重新连接或重新打开。设置页分别显示配置、协议探测，以及 Codex / Claude 各自的实际工具调用证据；MCP 握手不启动 Core，首次工具调用可无浏览器启动 Core。

个人 Claude 与 Codex 复用同一套 Team Cross 工具，可以辅助任一 Provider 的协作；实际写入能力取决于目标协作及当前输入归属。个人对话与模型调用在本机，发往共享会话的任务在 A 执行。对应结果见 [个人 Claude MCP 验收](agent-wiki/sources/validation/claude-assist-2026-09-12.md)。项目同名配置、显式禁用或组织策略可能影响工具加载，页面检测不覆盖其他目录。

可以告诉自己的客户端：“使用 Team Cross 列出协作，查看这次协作的上下文和当前状态。”

### 常用工具

个人 MCP 的常用工具如下；参数与完整接口见 [协议](agent-wiki/sources/protocol.md)。

| 工具 | 用途 |
| --- | --- |
| `freeze_source_session` / `read_publication_draft` / `preview_publication` | 冻结本机历史、核对私有草稿并预览选定的公开范围 |
| `create_readonly_space` / `publish_material` | 创建只读空间并发布已确认材料，无需创建执行 fork |
| `get_publication_status` / `withdraw_material` | 查询发布结果，或撤回自己的材料以停止后续读取 |
| `list_materials` / `read_material` | 列出空间材料并按固定版本、按需读取正文 |
| `read_selection` | 按资源库生成的本机编号读取一组材料、批注和上下文，按 `nextOffset` 继续 |
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

### 终端查询与输入管理

终端也可以查询协作和管理输入，无需打开浏览器：

```sh
teamcross collaborations --json
teamcross collaborations --id <协作ID> --json
teamcross input request --id <协作ID> --epoch <详情中的epoch>
teamcross input handoff --id <协作ID> --member <成员ID> --epoch <最新epoch>
```

`input` 还支持 `cancel`、`reclaim`、`return`。每次先查询详情，再传入看到的输入状态版本；过期操作会被拒绝，结果不明时先查询，不自动重放。发起者只能向已加入的同事交出输入，交出和交还需等待当前轮结束；接回输入不自动中断正在执行的轮次。交接关闭旧直接客户端，辅助工具仍可读取上下文。

取得输入后，用 `teamcross open --id <协作ID> --client tui` 打开新终端窗口，或 `--client desktop` 打开 Codex 专用窗口；`--print-command` 只取得启动计划。Claude 仅支持 TUI。启动请求成功后仍需查看 `clientState`，不把它等同于会话已打开。`end --id` 由发起者结束共享，`leave --id` 由接收者离开，`resume --id` 恢复已有 fork；这些动作保留原生会话与工作目录。

## 阅读与批注

启用共同执行后，可在阅读面板标题栏切换“已发布会话材料”和“协作上下文”，无需上下寻找两部分。正文随整页连续滚动，顶部的内容切换、目录和专注阅读工具栏会吸顶。切换保留各自的阅读段落、已打开版本和工具过程，专注阅读改变宽度时也会尽量保持当前段落；点击批注的“查看原位置”会自动进入对应内容并定位。只读空间使用同样的阅读方式，只显示已发布材料。协作标题旁显示当前模式，点击可展开权限范围说明。

阅读区域较窄时，点击工具栏中的“目录”选择轮次；宽屏专注阅读可显示侧边目录。成员信息初始完整展示，双栏布局滚动进入阅读时会平滑折叠，回到上方时展开，也可以手动展开继续查看。窄屏中成员信息顺序排在正文下方，不自动折叠。批注仍可在右侧或原文旁查看与编辑。

点击“阅读材料”后，正文直接在这张材料卡片内展开。卡片顶部集中显示版本、来源和操作；向下阅读时，吸顶工具栏仍可切换其他材料或收起当前材料，已读版本会恢复阅读位置。作者的“发布新版本”和“撤回”靠右显示。切换历史版本时，标题、公开轮数、“加入选择”和收藏都对应当前查看的版本。收藏成功会显示高亮星标与“已收藏”，再次点击可取消；保存失败时会在按钮旁说明原因。

历史从最近 8 轮开始；正文按轮对齐，对话完整返回，长工具输出默认折叠。工具可将 `nextCursor` 传入 `read_context` 的 `cursor` 继续本页或读取更早内容，也可用响应中的 `pageCursor + turnId + itemId + startOffset` 单独读完一条输出。WebGUI 可翻阅更早的对话，按轮次目录导航；已发布材料与协作上下文支持 Markdown 排版、代码高亮、工具过程折叠、专注阅读及原文切换。对话有新内容时先提示，点击后更新当前阅读内容。

WebGUI 的批注面板可以直接填写整体意见。选中对话文字、点击“批注这条消息”，或点击代码行旁的 `+`，会在原文旁的编辑卡中展示引用位置和原文；窄屏在消息下方展开。拖选同一文件、同一侧的连续代码行可添加多行批注。不同引用分别保留当前页草稿，关闭或按 Escape 收起后可以继续，支持 `⌘/Ctrl + Enter` 保存。已保存的原文批注会高亮，并提供就地阅读讨论的入口。

批注与回复中的“引用材料”可按标题或作者搜索，默认展示最新版本，历史版本按需展开；选中后显示固定版本标签，可以移除。点击“预览”才读取正文片段。

每条批注下可展开“回复”，按时间顺序讨论原来的意见。回复只有一层，始终归属原批注；人工和共享 Agent 的作者分别显示。收起回复或保存失败会保留当前页草稿，重试使用相同请求标识避免重复保存。上下文刷新保留草稿；浏览器整页刷新或离开页面不保证保留草稿。

点击已保存批注的“查看原位置”可以返回对应对话或代码。代码批注记录文件、行号、改动前后、内容指纹与当时片段；原文变化时保留片段并提示核对，不把旧行号当成当前内容。对话批注绑定原生 turn/item 和选中文字的范围，可在历史分页中查找。

直接 Codex TUI、专用 Codex Desktop 和直接 Claude Code TUI 的共享运行时内置当前空间的批注与材料工具，无需安装个人辅助 MCP。可以直接告诉它：“读取 Team Cross 批注和回复，按引用读取已发布材料；长工具输出需要时按条展开，核对原文后回复这条批注。”共享工具 `read_annotations`、`reply_to_annotation`、`list_materials`、`read_material` 只访问当前空间且仍保持四个；个人辅助客户端还可从自己的本机来源发布材料，并用 `read_publication_draft` 按需读取私有冻结草稿。

保存批注或回复不会自动启动或补充模型轮次。模型需在用户提出要求后读取实际上下文，再判断如何处理；共享 Agent 的回复明确标为 Codex 或 Claude Code。新建协作会自动接入；Codex 旧协作恢复运行时后接入。Claude 旧 worker 的原生启动参数不会被改写，需要新建协作使用新工具；创建时已接入这些工具的 Claude 协作，恢复时会保留接入。

## 资源库与速览

WebGUI 左侧的“资源库”汇集你发起或加入过的空间内容。可以查看最近使用、我参与的、我批注的和已收藏，按材料、批注或上下文筛选，也可以搜索标题、Session 与批注。列表按 Session 分组，材料保留发布版本，批注保留原文与回复。点击标题阅读，勾选加入底部选择清单，点击星标收藏；切换筛选或页面后选择仍保留。

阅读、勾选和收藏时，条目保持原位；后台同步会更新内容与状态。需要按最新使用记录重排时，点击资源库顶部的“刷新”或重新加载页面。

在材料阅读器、原文批注或协作上下文旁也可以点击“加入选择”。多种内容可以一起选，跨空间的内容可交给个人 Agent 分析。点击“生成读取入口”后，编号和“复制读取提示”直接显示在底部选择栏，不弹出大窗口。复制一次提示并粘贴到已经接入 Team Cross MCP 的本机个人 Codex 或 Claude Code，让它通过 `read_selection` 读取整组内容。详细提示和所选内容可以展开查看，需要时再明确要求 Agent 回复原批注。

每个入口固定生成时的选择，材料固定版本，执行上下文读取时仍需核对当前原文。修改选择不会改变已有入口，点击“重新生成”可更新；编号 7 天后到期，只在生成它的本机数据目录有效。收起入口不会清空选择。读取会重新检查权限，撤回或失去访问的内容不能靠编号继续读取。原有“设置与连接”页仍可配置个人 Agent。

所选内容都来自同一空间时，还可以点击“发送到共享会话”。核对目标、引用和处理要求后确认，页面沿现有输入通道发送。目标必须可执行、在线、空闲且由你持有输入；跨空间内容或等待审批时不能发送。已接收不等于执行完成，结果不明时先查看所在会话。

菜单栏 App 左键图标打开“资源速览”，也可使用 `Control + Option + T`（快捷键可用时）。速览与浏览器共用选择，可搜索、收藏、生成读取入口，或固定为小浮窗；完整材料与长讨论在 WebGUI 中打开。右键图标或打开速览的服务菜单，可继续使用原有设置、CLI 管理和退出功能。

## 结束与恢复

只读空间结束共享后，材料与讨论仍保存在托管端，重新邀请即可继续；不需要恢复模型运行时。启用执行和重置链接保留同次共享内的成员身份；结束整个共享后重新加入会获得新身份，不能用新身份更新旧身份发布的材料，可以重新发布独立材料。以下原生恢复规则仅适用于带可操作会话的空间。

“结束共享”关闭同事的原生连接和工具访问，并将输入归还发起者。当前执行与审批完成、专用客户端全部关闭后，Team Cross 会自动退出这次协作的后台 app-server，释放原生会话占用。会话、目录和代码继续保留，不要求提交、导出或填写结论；之后可从 Codex 打开，或在 Team Cross 中恢复并继续。

服务重启后，发起者从协作详情“恢复运行时”，继续已保存的同一个 fork，不再 fork 或创建 worktree。接收者本地保存加入记录与独立访问凭据；只要发起者仍共享且未主动离开，就可以从协作列表重新连接，无需重新使用邀请码。Team Cross 不自动删除任何原目录或协作 worktree。
