# 顺流感知第一版实现

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

- [中文浅色的待投递留言](2026-10-21-in-flow-evidence/note-pending.png)
- [英文深色与用户内容不翻译](2026-10-21-in-flow-evidence/note-english-dark.png)

发送未知结果、同 ID 查询/明确重试和校验失败保留草稿由 6 项新增界面交互测试覆盖。

## 2026-10-22 参与会话反馈修复

`4b3b7b6` 修复默认会话标题，并补全旧“当前会话”占位；后续本节同提交修复插件关联状态读取和面板资源缓存键。已关联时显示精确名称与接收状态；并发重复连接仍只有一个配对/目标，不重新创建原生会话。面板只读身份允许 Codex threadId 不附带轮次，模型工具的原有身份验证保持不变。状态未知、消息已发送和实际关联分别显示。

macOS 本机、Node 24.18.0：Go 全量 test/vet、`internal/collab` 与 `internal/mcp` race 通过；Web 20 个测试文件、189 项测试、1071 条翻译检查及两套嵌入构建通过。新回归覆盖重新打开、接收检查、送出后核对、解除关联、空间切换和缺少身份。

`TestCurrentConnectionBrowserFixture` 使用真实 Core/持久化和 production 面板，宿主桥与原生身份为合成夹具。浏览器在 1253×705 的中文浅色画面确认已关联名称及接收检查入口，解除关联后恢复连接入口；[截图](2026-10-21-in-flow-evidence/connection-linked.png)。首轮夹具转发遗漏 loopback Host 头，修正后通过；测试页面与两轮夹具均已关闭。没有新增真实 Codex 面板身份传递、Cloud 或两台 Mac 验收。

资源 URI 随 HTML 内容摘要变化，遵循官方缓存键规则。已打开面板仍由宿主刷新/重新打开，不宣称已实现自动热更新或全量草稿恢复。原有路由及 widget 状态恢复保持不变。

## 2026-10-22 本地 DMG

从干净提交 `cf8fc4956c9b4952b341b5ba3ec2341715b5b4da` 构建 `0.2.6-dev.inflow.20261022`，buildNumber 172，arm64、最低 macOS 14.0。产物保存在主仓库的 `dist/release/0.2.6-dev.inflow.20261022/`；[构建清单](2026-10-21-in-flow-evidence/release.json) 记录完整来源和校验值。DMG 的 SHA-256 为 `63c8222137c1b177d0f60ece9138607c59fae2e6a0adb6879cf75dbc4eb39cb8`。

标准 `build-release.py` 与 `verify-release.py` 通过。检查覆盖 CLI/App 版本及提交一致、DMG 挂载与临时安装、图标提取、签名完整性、真实双 App 副本协调、临时 CLI 安装/移除、已有命令保护及兼容 Core 复用；[结构化验证结果](2026-10-21-in-flow-evidence/dmg-verification.json)。脚本退出完成临时进程与挂载清理。构建使用 ad-hoc 签名，未使用 Developer ID、未公证；未运行可选的真实插件启动同步、Homebrew 或公开下载安装验证，未替换用户已安装的 App。

## 2026-10-22 插件更新体验

本轮接续上一节的资源版本化，新增当前 Core bundle 检测与同一 iframe 内的界面更新。可见面板约每 15 秒检查，回到前台时立即检查；没有未保存内容时自动刷新，有草稿时提示“保留草稿并刷新”。更新前保存当前编辑字段、引用、路由和稳定请求 ID，输入法组字及待完成写入暂缓更新；下载后再次判断，避免下载过程中输入被覆盖。摘要不符、读取失败和新代码启动失败均保留/恢复旧编辑器，失败版本等待明确重试。旧 stdio 通过已声明的只读路由读取当前 Core 嵌入资源，不依赖它自身的旧 bundle。

草稿按 Core 和编辑对象隔离，只在 iframe 内存传递；恢复不会发送或重试业务请求。首次从不具备检查器的旧面板迁移仍需宿主刷新一次；不兼容 MCP 声明、启动配置或协议更新也不由本机制代替。此前一节“仍需宿主刷新”的记录描述当时版本，本节新增能力的长期边界维护于[本机插件契约](../sources/decisions/chatgpt-local-plugin.md#一套-webgui两种运行环境)。

Go `test ./...`、`vet ./...` 及 `internal/collab`、`internal/mcp`、`internal/mcpassets` race 通过。Web check、21 个文件 198 项测试、1077 条翻译完整性检查和两套嵌入构建通过。新增 9 项交互回归覆盖无草稿、有草稿、下载中输入、待完成写入/组字、失败重试、摘要错误、结果未知留言只读核对、兄弟会话草稿与复用组件隔离。全量测试发现离开空间时新增 Hook 位于提前返回之后，已移除不必要的检查并复跑通过。

真实浏览器使用 `TestPluginUpdateBrowserFixture` 的 production 面板和真实 Core，宿主桥、原生身份及版本切换为合成。iframe CSP 使用 `connect-src 'none'` 和内联脚本/样式，确认执行代码版本从 0 自动变为 1；有评论草稿时停在旧版，点击后变为 2 并恢复原文；留言弹窗内也可点击刷新且保留正文。故障版本在启动时抛出异常，立即回退到 2 并保留草稿，显示明确重试，不自动循环。以上流程业务写入计数均为零。

- [有草稿时提示](2026-10-21-in-flow-evidence/plugin-update-draft-prompt.png)
- [刷新后恢复评论](2026-10-21-in-flow-evidence/plugin-update-draft-restored.png)
- [留言弹窗内的刷新入口](2026-10-21-in-flow-evidence/plugin-update-modal-prompt.png)
- [启动失败回退与草稿保留](2026-10-21-in-flow-evidence/plugin-update-rollback.png)

首轮夹具的合成 turnId 不一致，修正后继续；浏览器检查又发现弹窗之外的按钮不可点击和启动异常需等待超时，分别改为弹窗内提示及立即捕获启动错误，再完成最终复核。所有测试页已关闭，夹具按精确 finish 文件退出；原始日志留在 `/private/tmp/teamcross-refresh-*.log`。本轮未操作用户已安装 App/插件，也未新增真实 Codex 宿主、模型回合、Cloud 或两台 Mac 验收。
