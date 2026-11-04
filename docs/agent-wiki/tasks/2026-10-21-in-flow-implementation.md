# 顺流感知第一版实现

> 截图仅本地保存，路径相对本文，不随 Git 分发；保存与引用方式见[验收截图约定](../README.md#验收截图)。

分支：`codex/in-flow-awareness`。从 `codex/meta-to-main-20261021` 的 `4d33a78d1efc85f80d821f0508b01830024f8e46` 创建独立 worktree，原目录的本地影子任务不包含在本次改动中。此记录只描述本分支第一版实现及被测范围。

## 已确认范围

- 明确开启的空间关联接收简报更新；协作会话接收自己的新评论与回复，使用准确 executionId，无逐条关注。
- 主动留言指定已有接收目标，空间内可见，不唤醒模型；请求结果只回到可核对的发起会话。
- 普通空间讨论、材料评论不自动广播；不因使用过 MCP、插件或相同 cwd 建立订阅。
- MCP 和 UserPromptSubmit Hook 共用分发与已投递记录。输出成功后记录；没有模型 ack、已读、理解或完成推断。
- 不增加主机断线通知链、不重放原生输入、不修改 AGENTS.override.md。

## 实现与验证清单

- [x] 持久化开关、定向留言及投递记录，精确作用域和访问撤销。
- [x] 简报、会话评论/回复、定向留言、本人请求结果的有限批次分发。
- [x] MCP 尾部内容、总响应预算、失败不提交投递记录。
- [x] Hook 命令与配置入口，准确会话身份，失败静默且不启动 Core。
- [x] Web 开关、留言与状态；MCP 明确启用和留言工具。
- [x] 跨 Core、并发、重启、关联撤销与输出失败回归。
- [x] Go test/vet/race；Web check/test/build；两套嵌入资源；必要的真实浏览器/原生 Hook 验证。
- [x] 更新当前契约、协议和验证证据，完成范围逐项核对。

## 实现边界

主要规则维护在 [顺流感知契约](../sources/decisions/in-flow-awareness.md)，接口见 [协议](../sources/protocol.md#顺流感知接口)。新增持久字段在空间 Workbench，临时批次仅在内存。Hook 与 MCP 在成功写出后记录实际批次版本，租约不视为已投递；普通业务请求的状态不随投递记账改变。

关联撤销后重新关联不会自动恢复开关；多个目标别名不互相覆盖，模型配置遇到歧义要求在界面选准确关联。留言的明确校验失败允许编辑原草稿；结果不明只核对原 ID，确认未找到后可明确重试同一 ID 与正文。

## 工程验证

以下命令通过（Node 24.18.0）：

```sh
go test ./...
go vet ./...
go test -race ./internal/collab ./internal/mcp ./internal/mcpevents ./internal/sharing ./internal/nativecodex ./internal/nativeclaude ./internal/service ./internal/cliinstall ./internal/pluginpack
pnpm --filter @teamcross/web check
pnpm --filter @teamcross/web test
pnpm --filter @teamcross/web build
```

Web 19 个文件、184 项测试、1067 条翻译完整性检查通过；两套嵌入资源均更新。Vite 保留现有大 chunk 提示。Go 全量最初因旧 runtime 测试只预期一次内部调用而失败，测试已区分原工具调用和额外的被动准备调用，修正后通过。最后的投递记账不触碰业务状态与插件引号路径补充检查单独复跑 `internal/collab`、`internal/pluginpack` 的 test、race 和 vet，全部通过；最终二进制原生复核也通过。测试中的排队超时场景使用人工请求，避免其结果混入重启去重场景。

日志在 `/private/tmp/teamcross-inflow-{go-test,go-vet,race,web-check,web-test,web-build,final-go,final-race}.log`。这些本机原始日志不作为长期产品规则。

## 真实 Codex 原生验证

[验证脚本](../../../scripts/verify-inflow.py) 创建全新 home、仓库、Core、来源会话与受限/信任两个 fork。仅使用 `gpt-5.6-luna`，Codex `0.163.0-alpha.11`，单 Mac。测试评论包含随机标记，用户 prompt 中不提供该标记。

- 受限模式：仅调用一次 `read_space_brief`，正常工具响应附带精确评论，模型返回该随机标记，落盘版本为 1。
- 信任模式：原生审阅 fixture 的精确 Hook；不调用 MCP，模型从 UserPromptSubmit 的 additionalContext 返回标记，落盘版本为 1。
- 不修改用户个人配置、已有会话、全局插件安装；脚本退出关闭自己启动的 TUI 与 Core。

首轮原生 MCP 已取得正确标记，但脚本错误地读取 `thread.turns` 而不是投影后的 `segments`，导致判定超时。修正后第二轮全部通过；失败证据保留在 `/private/tmp/teamcross-inflow-native-20261021`，第二轮在同名 `-r2` 目录。最终二进制复核使用 `-final` 目录，两种模式全部通过，清理无错误；结构化结果及二进制 SHA-256 见 [native.json](2026-10-21-in-flow-evidence/native.json)。

本轮没有真实 Claude Hook、插件 Hook 原生自动加载、Desktop、Cloud 或两台 Mac 验收；它们不能由 CLI 配置、协议测试或单 Mac TUI 推断为通过。受限模式仍禁用 Hook。没有正常交互时，留言保持待投递。

## 真实浏览器

使用 `TestWorkbenchBrowserFixture` 的真实 Core / HTTP / production 资源，原生运行时与数据为合成 fixture。在 1265×712 下核对个人开关与他人只读、保存定向留言、待投递说明和中英主题显示；不能宣称未检查的其他宽度已通过。浏览器页已关闭，fixture 通过指定 finish 文件退出。

- 中文浅色的待投递留言（`2026-10-21-in-flow-evidence/note-pending.png`）
- 英文深色与用户内容不翻译（`2026-10-21-in-flow-evidence/note-english-dark.png`）

发送未知结果、同 ID 查询/明确重试和校验失败保留草稿由 6 项新增界面交互测试覆盖。

## 2026-10-22 参与会话反馈修复

`4b3b7b6` 修复默认会话标题，并补全旧“当前会话”占位；后续本节同提交修复插件关联状态读取和面板资源缓存键。已关联时显示精确名称与接收状态；并发重复连接仍只有一个配对/目标，不重新创建原生会话。面板只读身份允许 Codex threadId 不附带轮次，模型工具的原有身份验证保持不变。状态未知、消息已发送和实际关联分别显示。

macOS 本机、Node 24.18.0：Go 全量 test/vet、`internal/collab` 与 `internal/mcp` race 通过；Web 20 个测试文件、189 项测试、1071 条翻译检查及两套嵌入构建通过。新回归覆盖重新打开、接收检查、送出后核对、解除关联、空间切换和缺少身份。

`TestCurrentConnectionBrowserFixture` 使用真实 Core/持久化和 production 面板，宿主桥与原生身份为合成夹具。浏览器在 1253×705 的中文浅色画面确认已关联名称及接收检查入口，解除关联后恢复连接入口；截图（`2026-10-21-in-flow-evidence/connection-linked.png`）。首轮夹具转发遗漏 loopback Host 头，修正后通过；测试页面与两轮夹具均已关闭。没有新增真实 Codex 面板身份传递、Cloud 或两台 Mac 验收。

资源 URI 随 HTML 内容摘要变化，遵循官方缓存键规则。已打开面板仍由宿主刷新/重新打开，不宣称已实现自动热更新或全量草稿恢复。原有路由及 widget 状态恢复保持不变。

## 2026-10-22 本地 DMG

从干净提交 `cf8fc4956c9b4952b341b5ba3ec2341715b5b4da` 构建 `0.2.6-dev.inflow.20261022`，buildNumber 172，arm64、最低 macOS 14.0。产物保存在主仓库的 `dist/release/0.2.6-dev.inflow.20261022/`；[构建清单](2026-10-21-in-flow-evidence/release.json) 记录完整来源和校验值。DMG 的 SHA-256 为 `63c8222137c1b177d0f60ece9138607c59fae2e6a0adb6879cf75dbc4eb39cb8`。

标准 `build-release.py` 与 `verify-release.py` 通过。检查覆盖 CLI/App 版本及提交一致、DMG 挂载与临时安装、图标提取、签名完整性、真实双 App 副本协调、临时 CLI 安装/移除、已有命令保护及兼容 Core 复用；[结构化验证结果](2026-10-21-in-flow-evidence/dmg-verification.json)。脚本退出完成临时进程与挂载清理。构建使用 ad-hoc 签名，未使用 Developer ID、未公证；未运行可选的真实插件启动同步、Homebrew 或公开下载安装验证，未替换用户已安装的 App。

## 2026-10-22 插件更新体验

本轮接续上一节的资源版本化，新增当前 Core bundle 检测与同一 iframe 内的界面更新。可见面板约每 15 秒检查，回到前台时立即检查；没有未保存内容时自动刷新，有草稿时提示“保留草稿并刷新”。更新前保存当前编辑字段、引用、路由和稳定请求 ID，输入法组字及待完成写入暂缓更新；下载后再次判断，避免下载过程中输入被覆盖。摘要不符、读取失败和新代码启动失败均保留/恢复旧编辑器，失败版本等待明确重试。旧 stdio 通过已声明的只读路由读取当前 Core 嵌入资源，不依赖它自身的旧 bundle。

草稿按 Core 和编辑对象隔离，只在 iframe 内存传递；恢复不会发送或重试业务请求。首次从不具备检查器的旧面板迁移仍需宿主刷新一次；不兼容 MCP 声明、启动配置或协议更新也不由本机制代替。此前一节“仍需宿主刷新”的记录描述当时版本，本节新增能力的长期边界维护于[本机插件契约](../sources/decisions/chatgpt-local-plugin.md#一套-webgui两种运行环境)。

Go `test ./...`、`vet ./...` 及 `internal/collab`、`internal/mcp`、`internal/mcpassets` race 通过。Web check、21 个文件 198 项测试、1077 条翻译完整性检查和两套嵌入构建通过。新增 9 项交互回归覆盖无草稿、有草稿、下载中输入、待完成写入/组字、失败重试、摘要错误、结果未知留言只读核对、兄弟会话草稿与复用组件隔离。全量测试发现离开空间时新增 Hook 位于提前返回之后，已移除不必要的检查并复跑通过。

真实浏览器使用 `TestPluginUpdateBrowserFixture` 的 production 面板和真实 Core，宿主桥、原生身份及版本切换为合成。iframe CSP 使用 `connect-src 'none'` 和内联脚本/样式，确认执行代码版本从 0 自动变为 1；有评论草稿时停在旧版，点击后变为 2 并恢复原文；留言弹窗内也可点击刷新且保留正文。故障版本在启动时抛出异常，立即回退到 2 并保留草稿，显示明确重试，不自动循环。以上流程业务写入计数均为零。

- 有草稿时提示（`2026-10-21-in-flow-evidence/plugin-update-draft-prompt.png`）
- 刷新后恢复评论（`2026-10-21-in-flow-evidence/plugin-update-draft-restored.png`）
- 留言弹窗内的刷新入口（`2026-10-21-in-flow-evidence/plugin-update-modal-prompt.png`）
- 启动失败回退与草稿保留（`2026-10-21-in-flow-evidence/plugin-update-rollback.png`）

首轮夹具的合成 turnId 不一致，修正后继续；浏览器检查又发现弹窗之外的按钮不可点击和启动异常需等待超时，分别改为弹窗内提示及立即捕获启动错误，再完成最终复核。所有测试页已关闭，夹具按精确 finish 文件退出；原始日志留在 `/private/tmp/teamcross-refresh-*.log`。本轮未操作用户已安装 App/插件，也未新增真实 Codex 宿主、模型回合、Cloud 或两台 Mac 验收。


## 2026-10-22 插件更新版 DMG

从干净提交 `8a5564d0971e728df973386461151380ca45fc5f` 构建 `0.2.6-dev.plugin-refresh.20261022`，buildNumber 174，arm64、最低 macOS 14.0。产物位于主仓库 `dist/release/0.2.6-dev.plugin-refresh.20261022/`，保留上一版安装包；[构建清单](2026-10-21-in-flow-evidence/plugin-refresh-release.json)。DMG SHA-256 为 `3937f3d1680335d7ecef3c5e0f914764da6d42ad8667dfba1b2f7e0b3390ad42`。

`build-release.py` 与 `verify-release.py` 通过。验证覆盖校验值、DMG 挂载与临时安装、图标、签名完整性、App/CLI 来源一致、双 App 实例协调、CLI 安装/移除与既有命令保护、兼容 Core 复用及生命周期；[结构化验证结果](2026-10-21-in-flow-evidence/plugin-refresh-dmg-verification.json)。临时进程、挂载和浏览器页面已清理。本包采用 ad-hoc 签名，未使用 Developer ID、未公证；未执行可选的真实插件启动同步或公开安装验收，未替换用户已安装 App。

## 2026-10-22 实际宿主的旧 Core 阻塞

用户安装更新版后，插件显示当前会话查询 404、旧占位标题，并反馈点击连接后立即白屏。只读核对确认磁盘 App 是 `8a5564d` / `0.2.6-dev.plugin-refresh.20261022`，运行中 Core 仍是 `583a12a` / `0.2.6-dev.in-flow.20261021`；`active=0`、`upgradeBlocked=true`，新增版本接口确实返回 404。Agent 请求中存在 10 月 4 日持久化的 `unknown` 记录，旧 `UpgradeBusy` 将其永久当成活动。这里是已安装文件与运行中服务不一致，不能归结为用户没安装 DMG 或单纯面板缓存。

现场恢复再次核对无活动协作和无进行中请求，使用正常 `stop`（无 force）与已安装 helper 的 `serve` 完成 Core 切换。请求账本前后相等，未知请求没有删除、改写或重发；未停止 Codex 或用户会话。真实宿主的既有插件面板随后通过原轮询显示两个原生标题、“当前会话已关联”和“尚未接通接收”，404 消失。明确点击一次“重新检查接收连接”，提交中与消息确认后 DOM 均正常，画面未白屏；本机截图保存在 `/private/tmp/teamcross-live-connection-recovered.png`，含真实用户标题，不纳入 Git。

以上实际宿主证据覆盖读取身份、关联状态、标题回填及一次连接消息后的页面稳定性；不等于原生主动接收已接通，也不证明原白屏的独立根因已经捕获。此次没有观察到对应脚本异常，不能将模拟浏览器更新回退的通过扩大为真实宿主全面热更新验收。

`d835321` 修复持久化未知回执阻塞升级，原有运行中请求/接收连接保护保留。MCP 在 Core 尚未切换时返回明确 `core_update_pending`；连接查询出错后停止“正在核对”，旧 404 给出正常重启 Team Cross 的提示，重试成功自动恢复关联状态，不发送连接消息。Go 全量 test/vet、`internal/collab`、`internal/mcp`、`internal/service` race 通过；Web 21 个文件 199 项测试、1079 条翻译检查及两套资源构建通过。

## 2026-10-22 Core 恢复修复的升级与 DMG 验证

真实三构建升级验证通过：旧构建 `ae66ff6906fada7fb480c9fa47873103ddc21768`、修复构建 `d835321ccadbd52e804fda2391adb5426ebe2078`、下一构建 `08105e67915ab0e54e4bfd5ab4c068318eddad60` 均使用同一版本号 `0.2.6-dev.upgrade-check`。覆盖常驻旧 MCP、同一浏览器地址、并发升级、后续 App 替换、进行中投递阻止升级、未知回执不阻止升级且原样保留，以及数据、启动选项、身份与鉴权；[结构化结果](2026-10-21-in-flow-evidence/plugin-recovery-core-upgrade.json)。实际发现的 Codex CLI 为 `0.163.0-alpha.11`，没有调用模型。首轮误用已包含 CLI 发现修复的 `af9f715` 作为旧基线，未满足脚本预期的旧路径故障前提；换成真正修复前的提交、使用新的隔离目录后全项通过。两轮脚本均执行自身进程清理。

从干净提交 `08105e67915ab0e54e4bfd5ab4c068318eddad60` 构建 `0.2.6-dev.plugin-recovery.20261022`，buildNumber 177，arm64、最低 macOS 14.0。产物位于主仓库 `dist/release/0.2.6-dev.plugin-recovery.20261022/`，保留已有安装包；[构建清单](2026-10-21-in-flow-evidence/plugin-recovery-release.json)。DMG SHA-256 为 `d6dee8ff28363e7ee5aa77e992fbca64be71cd0c72321b893d476fdc9f907853`。

标准 `build-release.py` 与 `verify-release.py` 通过，覆盖校验值、DMG 挂载与临时安装、图标、签名完整性、App/CLI 来源一致、双 App 实例、CLI 安装/移除与已有命令保护、兼容 Core 复用和生命周期；[结构化验证结果](2026-10-21-in-flow-evidence/plugin-recovery-dmg-verification.json)。验证脚本已清理自身临时进程与挂载。没有替换用户已安装 App；本包使用 ad-hoc 签名，未使用 Developer ID、未公证。可选的真实插件启动同步与公开安装检查未执行，不能把上述隔离升级验证视为它们通过。


## 2026-10-22 已安装构建优先与有界收尾

`acc12e4` 按用户确认的策略保留插件按需启动，不引入退出禁启标记。Core 检查绑定安装的文件变化，发现构建变化后由该安装的短生命周期 helper 完成切换；多个入口共用启动锁。新版 Core 的收尾截止时间固定为首次请求后的 10 秒，重复请求不延长；已有写入和明确结果回报可完成，空闲轮询取消，历史未知状态和接收心跳不否决升级。到期取消剩余请求并正常关闭托管运行时，个人宿主不关闭。10 秒指等待既有工作自然完成的上限，实际进程退出与新服务启动另需耗时。

插件继续保留原编辑器和草稿，仅明确读取遇到升级状态时有界重试；写入和宿主消息不重发，未知留言继续使用原请求 ID 核对。后台服务切换不等同于界面 bundle 替换，有草稿时仍显示“保留草稿并刷新”。首次迁移的旧 Core 没有新检测器，需新版 App/helper 参与，并使用其原有鉴权停止入口；旧进程的准入保护不能由新调用方补齐。

Go 全量 test/vet、service/collab/mcp/cmd 的 race 通过；最后调整的 service 收尾竞争另跑 race 通过。Web check、21 个文件 201 项测试、1080 条 UI 翻译检查和两套嵌入资源构建通过。首次回归暴露旧测试的更新提示预期、草稿检查点结构预期以及未声明安装归属的合成 Core 兼容边界；调整测试和旧归属处理后复跑通过。源码相对链接、Python 语法及 diff 检查通过。


真实升级验证使用同版本号 `0.2.6-dev.upgrade-check` 的 `ae66ff6`、`acc12e4`、`849ba25`，另外用 `08105e6` 覆盖旧活动否决策略的首次迁移；[结构化结果](2026-10-21-in-flow-evidence/priority-upgrade-core.json)。最终一轮：未完成请求的切换共 10.45 秒，旧策略首次迁移 10.22 秒；两者包括进程启动，收尾截止时间固定 10 秒。没有客户端触发时自动检测并切换约 2.04 秒。四个并发启动收敛到同一 PID，重复升级请求返回同一截止时间，新业务请求得到 503，未知和已提交请求记录保留且不重发。实际 Codex CLI 为 `0.163.0-alpha.11`，没有调用模型。首次完整三构建通过后，扩充迁移场景的第二轮误选不含旧活动否决字段的早期基线，检查前提失败；增加准确旧策略构建后第三轮通过。各轮均清理自己启动的 Core/stdio/helper。

从干净提交 `849ba25ab8480d777d2123c3dd8186ad9355e103` 构建 `0.2.6-dev.priority-upgrade.20261022`，buildNumber 180，arm64、最低 macOS 14.0；[构建清单](2026-10-21-in-flow-evidence/priority-upgrade-release.json)。产物保存在主仓库 `dist/release/0.2.6-dev.priority-upgrade.20261022/`，原安装包保留。DMG SHA-256 为 `c79db364a6ce57c00d30da04c495f0888ec7e78c307b49d954a8193b967f94b3`。

标准构建及 DMG 成品验证通过，覆盖校验值、挂载和临时安装、图标、ad-hoc 签名完整性、App/CLI 构建一致、双 App 实例协调、CLI 安装/移除、已有命令保护、兼容服务复用及生命周期；[结构化结果](2026-10-21-in-flow-evidence/priority-upgrade-dmg-verification.json)。临时进程和挂载已清理，没有替换用户已安装 App。未使用 Developer ID、未公证；本轮没有新增真实模型运行中断、实际宿主全流程、Cloud 或跨 Mac 验收。草稿与重连交互由真实组件/宿主桥模拟回归覆盖；原有真实浏览器截图不当作本构建新增验收。成品来源提交后的改动仅补充验证脚本与证据，不改变安装包内的产品代码。


全部升级验收完成后，从干净提交 `0d75cf9d999c124e51220801813400d9f66d886c` 重新构建最终交付版 `0.2.6-dev.priority-upgrade.20261022.2`，buildNumber 181；[最终构建清单](2026-10-21-in-flow-evidence/priority-upgrade-final-release.json)、[最终成品验证](2026-10-21-in-flow-evidence/priority-upgrade-final-verification.json)。标准 build/verify 流程再次通过，CLI/App 来源、校验、签名完整性、挂载安装、实例协调与生命周期均通过。最终 DMG SHA-256 为 `50f612b733d54040f6cdc89a0ac1f77debcdf0cbd64e09eb83e195c01f9f8f5a`，位于主仓库 `dist/release/0.2.6-dev.priority-upgrade.20261022.2/`；前一候选包保留。最终包仍为 arm64、macOS 14+、ad-hoc 签名且未公证。


## 2026-10-22 合入 Meta 与组合验证

将 `codex/in-flow-awareness` 的 `c6d4ad8` 合入已包含本机分身和启动模型设置的 Meta `1b187f0`。保留两边功能，重新生成 WebGUI 与插件嵌入资源；翻译和测试冲突按双方新增内容合并。组合检查发现分身原来只参与 `UpgradeBusy`，因此将同一分身活动判断接入新 `UpgradeWorkBusy`：已接收队列、原生轮次和后台任务可获得固定收尾时间，空闲或暂停队列不额外延迟，到期仍按统一 10 秒规则切换。分身契约同步更新，回归同时检查旧状态报告与新收尾判断。

合并工作树的 Go 全量 test/vet，以及 `internal/service`、`internal/shadow`、`internal/collab`、`internal/mcp`、`cmd/teamcross` 的 race 均通过。Node 24.18.0 下 Web check、22 个文件 207 项测试、1155 条翻译检查和双资源构建通过；Markdown 相对链接和 `git diff --check` 通过。未进行新的真实模型或跨 Mac 验收。


合并提交为 `a7691c4df27a571bc722905fbeae9cda4f661862`。隔离真实 Core/stdio 升级验证通过，旧基线 `ae66ff6`、已含新策略的 `acc12e4` 与合并构建使用同一版本号；另从已交付分身模型包 `7752b75` 验证旧活动否决策略迁移到合并构建。[升级结果](2026-10-21-in-flow-evidence/meta-priority-core-upgrade.json)记录并发升级收敛、旧 MCP 不复活旧 Core、安装替换自动发现、截止时间不延长、新写入拒绝、原地址和启动参数保留，以及未知/已提交请求不重发。本轮忙碌切换共 10.43 秒，旧分身版迁移共 10.22 秒，自动检测切换约 2.03 秒；包含进程退出与新服务启动耗时。Codex CLI 为 `0.163.0-alpha.11.1`，未调用模型。

全部上述检查完成后，从干净合并提交构建 `0.2.6-dev.meta-priority.20261022`，buildNumber 191，arm64、最低 macOS 14.0，包含 Meta 的本机分身和启动模型设置。产物位于主仓库 `dist/release/0.2.6-dev.meta-priority.20261022/`；[构建清单](2026-10-21-in-flow-evidence/meta-priority-release.json)。DMG SHA-256 为 `b45f462f28a821a7afb8b68f5d8fb974bfbe3fc1e1519ca9897e3df276aad4e1`，保留以前的安装包。

标准 `build-release.py` 与 `verify-release.py` 通过；[成品验证](2026-10-21-in-flow-evidence/meta-priority-dmg-verification.json)覆盖校验值、DMG 挂载与临时安装、图标、签名完整性、App/CLI 构建一致、双 App 实例、CLI 安装/移除与已有命令保护、兼容 Core 复用及生命周期。测试进程与挂载已核对清理，未替换用户已安装 App。本包为 ad-hoc 签名，未使用 Developer ID、未公证；未新增真实模型中断、真实宿主全流程、Cloud、跨 Mac 或 Homebrew 安装验收。成品来源之后仅补充本段和结构化证据，不改变产品代码。


## 2026-10-22 侧栏入口的资源版本错配

用户在启用 UserPromptSubmit Hook 后反馈白屏。当前对话通过 `open_teamcross` 打开的面板可显示，随后用户明确纠正：从 Codex 插件列表/侧栏打开仍然失败，不能把前一路正常当作已恢复。现场 App 与 Core 均为 `a7691c4` / `0.2.6-dev.meta-priority.20261022`，Core 没有再次落后；本轮未重启用户 Core、修改 Hook 信任或结束用户会话。

2026-10-22 19:47–19:52（Asia/Shanghai）的宿主日志记录侧栏调用 `open_teamcross` 成功，但 `resources/read` 请求 `workspace-c26d0014c2c2321b24456972.html` 连续收到 `-32602 / Unknown Team Cross resource or different local Core`，页面内容尚未返回。另可见旧面板请求 `workspace-07d625b5e6b6640ee4bb7e44.html`，以及在新 App 安装前已启动、仍存活的 MCP 进程。代码只接受本进程内嵌资源的准确摘要；宿主入口发现与会话 MCP 生命周期不一致，形成可复现的 UI 地址版本错配。Hook 保存附近还出现宿主面板撤销/重建，撤销的独立原因不能仅凭日志断言。

修复保留内容摘要作为发现时的缓存分代地址，将严格限定的 Team Cross 面板地址作为启动别名，由当前 MCP 返回自己的静态 HTML，再由已存在的面板更新器检查当前 Core。没有放宽材料/批注引用的 Core 绑定、版本或访问检查；静态资源读取不拉起 Core、不写用户数据。已有旧 MCP 进程须由宿主重新加载后才能取得本次代码，首次迁移不能靠替换磁盘文件热改内存。

验证：先新增真实 `ServeUI` 资源回归，修复前复现相同 -32602，修复后通过；覆盖旧、新、模拟后续版本、固定 v1、错误 authority/scheme/路径/摘要/查询/fragment、静态读取无 Core 数据，以及既有材料跨 Core 拒绝。全量 `go test ./...`、`go vet ./...`、`go test -race ./internal/mcp` 通过；Node 24.18.0 下 Web check（1155 条消息）、22 个文件 207 项测试与两套资源 build 通过，嵌入文件没有差异。

额外用当前安装版与修复版的真实 CLI/stdio 子进程对比，前者拒绝旧摘要和模拟后续摘要，后者全部读到完整页面；其他应用 URI 仍拒绝，两者均未创建 Core 数据目录。见[结构化 stdio 结果](2026-10-21-in-flow-evidence/sidebar-resource-stdio.json)。子进程已正常退出，隔离数据目录由夹具清理。没有调用模型、改写用户安装、自动重放消息或清除宿主缓存；本轮成品安装后的实际侧栏与对话入口双路验收仍需单独完成。


本次修复提交 `1f8859985b54fed42707ae6e481135de7f0bbbab` 已快进合入本地 Meta，未推送远程。从该干净提交构建 `0.2.6-dev.sidebar-recovery.20261022`，buildNumber 193，arm64 / macOS 14+；[构建清单](2026-10-21-in-flow-evidence/sidebar-recovery-release.json)。产物在主仓库 `dist/release/0.2.6-dev.sidebar-recovery.20261022/`，原安装包保留；DMG SHA-256 为 `24e1e1ceea7663d99276c1f4d8363c647abf6994016b3a6815f51e4765ded1f9`。

[成品验证](2026-10-21-in-flow-evidence/sidebar-recovery-dmg-verification.json)通过标准 DMG 校验、挂载临时安装、图标、签名完整性、App/CLI 一致性、双实例协调、CLI 安装/移除和已有命令保护、兼容 Core 复用；另直接用成品 helper 验证四种页面地址均返回完整 HTML 且不创建 Core 数据。第一次打包在独立缓存重复下载依赖时主动中止，随后复用已验证的本机 Go 缓存完成标准构建；前端资源此前已完成重新构建且无差异。验证进程与挂载已清理。成品是 ad-hoc 签名，未使用 Developer ID、未公证；没有替换用户已安装 App，首次迁移需安装后完整重开 Codex，让旧 MCP 加载本次兼容代码。实际侧栏/对话双入口的最终显示仍待用户宿主复核，不能由上述协议和成品测试替代。
