# 一个空间多个协作会话

2026-10-21；`Meta`，基线 `f49b2a1547490e4ef90eb41b08f695aef2737158` 加本任务改动。环境：macOS 14.8.5 / 23J423，arm64，单台 Mac。先完成多会话实现与本地试用 DMG，随后按用户反馈简化为全体成员共同阅读并调整阅读界面，随本次提交收录。没有推送、公开发布或替换用户安装。

**阶段说明：下文“实现范围”至“本地多会话试用 DMG”记录最初版本。其逐会话阅读授权方案已由[后续调整](#后续调整全体成员共同阅读与统一阅读界面)取代；旧截图、源码快照和 DMG 保留用于追溯，不作为最终权限和界面的证据。**

## 实现范围

空间统一保存成员、邀请、材料、公开讨论和工作台。空间内支持零到多个共同执行会话，每个会话分别保存原生 fork、执行目录、固定协作模式、运行状态、输入者、审批、事件及请求去重。个人接收会话和每空间至多一个专用协调角色继续独立存在。

- `spaceId`、`executionId` 与 Provider `sessionId` 分开；创建 UUID 即 executionId，预览返回确定的空间与会话目标，结果不明时查询原对象。
- HTTP、MCP、CLI、原生连接、资源引用、会话批注和请求接收目标均绑定准确会话。HTTP 拒绝裸 executionId 和错误父空间，不自动选择唯一会话。
- 邀请只授予空间成员资格。发起者按“成员 × 会话”开放执行访问；实际写入仍要求该会话当前输入权。未授权会话不返回真实标题、原生身份、目录、模型、历史或私有批注。
- 暂停等待已接收工作与审批结束后释放实例；停止某会话共享另撤销其授权。关闭空间才结束全空间访问。恢复不重建 fork，不恢复已撤销授权，不自动重放输入。
- 目录占用以真实 worktree 身份判断，处理符号链接和子目录。不同 worktree 可以并行；占用直到运行实例关闭后才解除。此协调仅覆盖本 Core 管理的实例，不控制用户外部程序。
- UI 统一空间外壳和会话侧栏，URL 可定位到会话；每会话保留草稿、阅读与文件状态。空间概览直接显示执行、待处理和暂停数量，待处理项进入准确会话。材料、请求和简报保持空间范围。
- 存储为 schema 4：空间 `collaboration.json` 与 `sessions/<executionId>/execution.json` 分开。邀请使用 tcx4。旧记录只显示 unsupported，原数据、原目录、worktree 和原生历史保留，不自动迁移或删除。

长期契约已同步到 [协作空间](../../sources/decisions/collaboration-spaces.md)、[生命周期](../../sources/decisions/workspace-and-lifecycle.md)、[核心词汇](../../sources/product-core-and-glossary.md) 和 [协议](../../sources/protocol.md)。

## 工程检查

| 检查 | 本轮结果 |
| --- | --- |
| `go test ./...` | 通过，见 [结果](evidence/go-test.txt) |
| `go vet ./...` | 通过，无诊断 |
| 相关 race | collab、mcp、mcpevents、sharing、nativecodex、nativeclaude、service、cliinstall、pluginpack 均通过，见 [结果](evidence/go-race.txt) |
| Web check | 通过，1056 条中英消息检查 |
| Web test | 18 个文件、177 项测试通过 |
| Web build | 通过，更新 webassets 与 mcpassets 两套嵌入资源；Vite 仍有大 chunk 的提示 |
| Swift | 三个 macOS 源文件 typecheck 和 arm64 优化构建通过；没有启动用户 App |
| Python 验证入口 | 五个迁移脚本语法检查通过；实际执行范围见下节，不把语法检查当作原生验收 |

执行隔离测试覆盖三成员、两会话、相同 requestId、原生审批 ID、输入者与授权代次、父空间校验、空间与会话 ID 冲突及并发创建、会话私有批注、兄弟运行时 token 隔离、暂停恢复、创建失败、旧记录保留与目录互斥。Web 测试覆盖快速切换、晚到保存、撤权清除私有内容、同片段的不同会话引用、划选正文加入资源库保留会话身份，以及概览直接跳转。

首次完整回归的失败来自仍使用裸执行 ID 的批注 MCP 测试。迁移为 `{id:spaceId, executionId}` 后完整回归通过，产品保留严格的目标校验。早期浏览器 fixture 曾在发布材料后保留了测试替身的无效来源目录；恢复合成来源后通过。最终 fixture 使用合成设备名称，不读取个人原生历史。

## 真实 Codex 与网络

[Codex 结构化结果](evidence/codex-native.json) 来自 `scripts/verify-agent-cli.py --provider codex`，CLI `0.163.0-alpha.10.2`，模型仅 `gpt-5.6-luna`，专用仓库、独立 profile、三个 Core、同一空间两个会话：

1. 真实个人 MCP 核对当前来源并登记分享；来源本轮结束后，新的 fork 包含最后 assistant 答复。
2. B 与 C 通过同一链接加入，加入本身没有执行授权。B 只获第一个会话访问，C 只获第二个会话访问。
3. B 使用真实 Codex TUI 接入第一个会话；MCP/CLI 完成批注、回复、输入申请、交接、交还与接回。
4. 两个会话在不同工作目录接收相同 requestId 的不同输入，分别产生自己的标记；历史不串线，重试去重。
5. 结束第一个会话后，空间与第二会话仍开放，C 的输入归属保持；恢复第一个会话沿用原生 ID，不恢复成员授权。

最终二进制在完成身份冲突保护、严格会话路由与 UI 收尾后再次通过上述原生闭环。初次脚本遗漏多人交接所需的明确 member 参数，修正后通过；最终运行没有错误或清理失败。完整临时目录为 `/private/tmp/teamcross-multi-native-20261021-c`，本页长期保留必要摘要，二进制 SHA-256 记录在 checks.json；该临时目录不是长期证据的唯一来源。

Tailcat 的传输层和协作层显式联网检查均通过，见 [传输结果](evidence/tailcat-sharing.txt)、[协作结果](evidence/tailcat-collab.txt)。这里是同机真实 Tailcat 数据面和合成 Provider，不是两台物理 Mac、跨 NAT 或持续强制 DERP 验收。

## Claude 阻塞及未覆盖项

[Claude 尝试结果](evidence/claude-attempt.json) 使用 `2.1.270`、专用 fixture 与限制为 Luna 的本机 guard。配置的本机代理拒绝连接，个人来源创建失败；CLI 同时输出 unrecognized_model。本轮没有进入创建共享 fork、TUI 交接或双会话执行，不能据此判断这些路径通过，也没有为绕过失败更换模型或修改用户代理配置。

本轮未重验专用 Desktop 全组合、个人 Desktop 主窗口交互、两台 Mac LAN、跨 NAT、强制 DERP、Claude 多会话真实执行与信任模式四组合。对应旧报告仅说明当时被测版本，不能直接覆盖 schema 4 / tcx4。其他 Python 入口已迁移明确会话目标并通过语法检查，未逐个运行真实模型。

## 浏览器与清理

使用最终 production 嵌入资源、真实 Core/HTTP/存储、三成员合成 fixture。完成了每会话评论与文件草稿隔离、空间简报草稿切换保留、材料返回、B/C 相反授权范围、折叠导航的可访问名称及概览统计复核。1440/1024/768 宽度、中英文、浅深主题均检查；窄屏上下文标签可以横向滚动，整页没有横向溢出。

- [1440 会话详情](screenshots/owner-session-1440.png)
- [1440 空间概览](screenshots/owner-overview-1440.png)
- [1024 未授权会话](screenshots/bob-restricted-1024.png)
- [768 英文深色会话](screenshots/carol-768-dark-en.png)

浏览器期间修正了空间标题被共享详情控件挤压、折叠会话缺少可访问名称、会话统计藏在管理折叠区的问题。最终统计与跳转有交互回归；截图只展示合成内容，不能证明真实 Provider 执行。

真实原生脚本关闭自己的 PTY、三个 Core 和测试 daemon；失败的 Claude 尝试同样完成清理。各浏览器 fixture 通过 finish 文件退出，测试页关闭并恢复 viewport。最终检查摘要见 [checks.json](evidence/checks.json)。测试仓库、worktree 与证据保留，不按进程名终止个人 Codex、Claude 或浏览器。

## 本地多会话试用 DMG

2026-10-21 按用户要求执行 `make release VERSION=0.2.6-dev.multi-session.20261021` 和同版本 `make verify-release`，均通过。产物位于 `dist/release/0.2.6-dev.multi-session.20261021/`，保留之前的构建目录。完整清单与检查摘要见 [DMG 证据](evidence/dmg-0.2.6-dev.multi-session.20261021.json)。

- 来源为上述基线加未提交多会话改动，`dirty: true`；104 个已记录源码文件的 SHA-256 与通过工程及原生检查时的快照一致。构建重新生成两套 Web 嵌入资源。
- DMG 为 `Team-Cross-0.2.6-dev.multi-session.20261021-arm64.dmg`，11,566,825 字节，Apple Silicon / macOS 14+；ad-hoc 签名，没有 Developer ID 签名或 Apple 公证。
- SHA-256：`a3d72f005106dc6ab4289540df68b66a794c531c1553db83f2476be58215f172`。
- 临时目录内完成 DMG 挂载与 App 复制、校验和、图标反解、签名完整性、CLI/App 版本一致、命令安装与移除、Core 生命周期及真实菜单栏 App 多副本检查。未运行 Homebrew 安装或新增真实模型验证。
- 验证结束后 DMG 已卸载，临时 App/Core 进程无残留；用户安装未替换。旧空间格式不迁移，新版应新建空间体验，多人参与端需使用支持 tcx4 的同版客户端。


## 后续调整：全体成员共同阅读与统一阅读界面

用户明确要求：所有空间成员均可读取已发布材料与协作会话，当前阶段只按协作会话管理输入权。本次提交同时包含此前未提交的多会话实现和以下调整；前面的 DMG 没有重建，因此不包含本节变更。

- 移除逐成员、逐会话的阅读 grant、HTTP/CLI/MCP 授权操作和界面按钮。已加入与后来加入的成员均可读取已有和新增会话的完整历史、目录、文件、改动及讨论。输入归属、epoch、审批和直接客户端限制按会话保持独立；撤销空间成员仍终止其全部访问。
- “结束共同执行”接回输入并排空当前工作，保留全体成员阅读和讨论；同空间其他会话继续运行。恢复沿用同一原生 fork 和目录，输入者为发起者。
- 协作会话复用材料的标题、来源头像、消息和正文布局。顶部只保留当前输入者、输入交接和主要客户端入口，模式/目录/暂停/结束归入会话操作菜单。修复会话与材料同时高亮、折叠导航隐藏头像及窄屏工具栏拥挤。
- 共用右侧评论列，随会话或材料切换。大于 1100 CSS 像素默认展开，窄屏默认收起；手动选择按当前页面的宽/窄布局分别保留。各会话及空间的评论草稿继续独立，后台保存仍绑定原目标。
- 同步中英文 README、用户指南、协议、领域契约与验证门槛。

### 本阶段工程结果

| 检查 | 结果 |
| --- | --- |
| Go 全量测试 | 通过，见 [结果](evidence/shared-reading-go-test.txt) |
| Go vet | 通过，无诊断 |
| 相关 race | 九个契约要求的包全部通过，见 [结果](evidence/shared-reading-go-race.txt) |
| Web check / test | 1049 条消息，18 文件 / 178 项测试通过，见 [check](evidence/shared-reading-web-check.txt)、[test](evidence/shared-reading-web-test.txt) |
| Web build | 两套生产嵌入资源已更新，见 [构建](evidence/shared-reading-web-build.txt)；仍有既有大 chunk 提示 |
| 验证脚本 | 五个改动 Python 入口语法检查通过；最终 MCP 回归通过 |

Go 增补覆盖既有/新成员对既有/新增会话共同可读、无输入权拒绝写入、成员撤销取消旧请求与重新加入不复活旧凭据、错误父空间拒绝，以及结束后阅读仍可用。Web 覆盖宽屏默认评论、宽窄布局切换、手动收起选择、独立草稿和晚到保存。

### 本阶段原生与界面结果

[真实 Codex 结果](evidence/shared-reading-codex-native.json) 使用 CLI `0.163.0-alpha.11`、`gpt-5.6-luna`、专用仓库/独立 profile/三个 Core/两个会话/loopback TLS。个人 MCP 当前来源分享、最后一轮包含于 fork、B/C 对全部会话读取、真实 TUI、批注与回复、输入申请/交接/交还/接回、并行执行与去重、结束一会话保留空间及兄弟运行、同 fork 恢复均通过。该阶段没有更换模型，也没有使用个人会话。

真实浏览器使用最终生产资源、真实 Core/HTTP/存储和合成历史，核对共同阅读、会话/材料切换、独立评论草稿、非输入者的申请入口、宽屏默认评论与手动关闭保持、1024/768 窄屏及英文深色。截图仅证明界面，不证明模型执行：

- [1440 会话与评论](screenshots/shared-reading-owner-1440.png)
- [1440 材料阅读对照](screenshots/shared-reading-material-1440.png)
- [1440 成员阅读与输入申请](screenshots/shared-reading-member-1440.png)
- [1024 会话布局](screenshots/shared-reading-owner-1024.png)
- [768 折叠侧栏与紧凑输入交接](screenshots/shared-reading-owner-768-sidebar.png)
- [768 标题区建议阅读起点](screenshots/shared-reading-material-768.png)
- [768 英文深色](screenshots/shared-reading-member-768-dark-en.png)

本阶段没有重新验证 Tailcat 数据面、专用/个人 Desktop 全组合、Claude 原生多会话或跨 Mac 网络；前一阶段 Tailcat 成功和 Claude 阻塞继续按各自范围报告。仅改变原生工具说明后追加 MCP 检查；最后的 CSS 修正重新构建两套嵌入资源并复核浏览器，没有因此重复真实模型回合。

用户补充的窄屏截图显示会话图标越界和新增按钮文案竖排漏出。最终版本将折叠导航边距缩为 6px，并隐藏文字、保留按钮可访问名称；在 768 CSS 像素下三个按钮的 scrollWidth 与 clientWidth 一致，会话头像可见，新增按钮只显示加号，整页没有水平溢出。

后续三条界面反馈也纳入最终构建：上下文阅读工具与加入选择/刷新取消旧底部偏移，在 768 和 1024 CSS 像素下垂直居中对齐；输入交接采用紧凑成员行，按钮放在右侧，并移除重复折叠控件，三人中文面板约 243px 高；建议阅读起点移入材料标题区，与来源和版本同排，复核点击后的正文定位。178 项 Web 测试再次通过，翻译为 1049 条，两套嵌入资源重新生成，上述截图均来自最终资源。

最终浏览器夹具通过 finish 正常退出，测试标签页关闭并恢复 viewport；按专用测试目录核对没有本阶段进程残留。最终 CLI 构建通过，622 个变更文档相对文件链接均可达，`git diff --check` 通过。源码快照、构建摘要和清理记录见 [最终检查](evidence/shared-reading-checks.json)，浏览器夹具退出结果见 [记录](evidence/shared-reading-browser-final.txt)。用户自行打开的标签页和已有 Core 保留。
