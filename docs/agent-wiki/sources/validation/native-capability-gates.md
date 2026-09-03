---
title: 原生 Session 能力验收契约
kind: validation
status: accepted
---

# 原生 Session 能力验收契约

Team Cross 的能力标记表示已集成并验证的产品能力，而不是 Provider 协议中出现了同名
方法。只读历史、实时 Follow、准确打开原生 UI、同会话 Resume 和独占控制必须分开判断。

## 已实现的只读边界

`sessions.snapshot` 只调用历史读取方法，生成结构化文本候选，由 Go Core 不可变保存。
Codex 使用 `thread/read`，遇到 `paginated` history 时使用 `thread/turns/list` 的完整项
分页；Claude 使用 `getSessionMessages` 和可用时的 `getSessionInfo`。这些路径不得创建
Run、Resume、Fork 或发送 Turn。未知、非文本、缺失及超限材料必须有可见标记。

Codex 原生对话身份以 `thread.id` 为准；`thread.sessionId` 是 Session tree root，Fork
可能与来源共享它，因此不能以相同 `thread.sessionId` 证明“同一会话”。原始来源也必须
保留；`appServer` 可由不同客户端创建，不能自动标成 Desktop。真实专用 Desktop 任务
也观察到 `source: vscode`，该 token 同样不能唯一识别 VS Code 界面；保留为可选的
`providerSource`，`surface` 为 `unknown`，不参与身份、fencing 或能力准入。

## 分能力验收

| 能力 | 必须取得的证据 | 不足以证明的信号 |
| --- | --- | --- |
| Read | 指定身份的消息/工具映射、缺失标记、零执行调用链 | initialize 成功 |
| Follow | 原生端继续工作时被动收到或只读增量读取、去重、断线恢复和缺口标记 | `thread/read` 成功或刷新页面 |
| Open | 原生 UI 打开准确的原生对话 ID，且没有增加 Writer | 仅打开应用或项目目录 |
| Resume | 同一原生对话 ID 在隔离 worktree 恢复，并实际验证 cwd/sandbox | API 存在 `cwd` 参数或 Fork 成功 |
| Take Control | 旧原生输入失效、单 Writer fencing 持久化、崩溃恢复和交还 | 只有 Team Cross Controller 租约或原生状态 idle |

CLI 和 Desktop 单独执行上述验收；不能用 CLI 结果代替 Desktop。原生返回的
`canAcceptDirectInput` 或 `thread/unsubscribe` 不等同于对其他客户端的独占输入授权。

## 当前原生控制状态

2026-09-03 对本机 Codex CLI 0.152.1 进行了版本与生成协议定义的静态检查。可见
`thread/read`、历史分页和 `thread/resume`；Resume 有 cwd/runtime roots 覆盖项，且运行中
会话的 Resume 是重新加入该会话。协议形状本身没有证明 Team Cross 可以封锁原生 UI
的输入来源或完成 Desktop 交还，因此当次 `follow`、`open`、`resume`、`takeControl`
均保持关闭。后续只读 Follow 的运行时门控见下节，不改变 Open/Resume/接管边界。

这不是实际 Session/Turn 测试，也没有连接用户的活跃 daemon 或枚举个人会话。不得据此
声称 M3、M4 原生接管或任何真实 sandbox 行为已经验收。

后续真实测试必须显式 opt-in，并使用专用 repository、隔离 worktree 和专用原生 Session。
验收材料记录 CLI/Desktop 版本、实际原生 `thread.id`、每次 Run 的目录、输入来源与拒绝
旧 Writer 的证据，以及原 checkout 前后状态。若不能可靠关闭旧输入，停止启用该能力；
不得通过 UI 自动化、公开原始 app-server、复制凭据或创建新 Session 来替代安全门槛。

## 已实现的 Follow reader 与逐会话能力门控

`sessions.poll` 已提供限定到指定 Codex Session 的只读增量合同，Core 已实现持久游标、
不可变检查点、重试/重启重验能力、停止 fencing，WebGUI 可呈现并显式管理 reader。
测试覆盖初次尾部、边界更新、超过一页的补读、暂时断线、历史缺口、重置、重复结果与
长工具输出；它们使用合成 app-server/Provider 数据，不是 CLI/Desktop 原生验收。
Snapshot 与 Follow 的序列化结果另按实际 JSON 字节收口，测试覆盖大量需转义控制字符，
避免字符计数合格但单行超过 Bridge 通道限制。截断材料必须标记为不完整。

`bridge.info.methods` 出现 `sessions.poll` 只表示 RPC 合同可调用，不等于某来源的
`capabilities.follow=true`。本机初始检查也不再使用 `thread/list` 枚举一个无关 Session。
`Open in Provider` 仍禁用：仅证明 `codex app PATH`、应用注册 URL scheme 或静态路由
可解析，不能证明实际定位准确原生对话或不会产生第二个 Writer。

原生实时 Share 已有独立授权与不可变公开窗口合同，不改变上述能力门槛。它只允许已经
成功读取的活动 Follow，并限制到明确类别和当前预览；使用合成 reader 测试公开窗口、
批注和 SSE，不能仅凭合成测试把 CLI/Desktop 的 `follow` 标记打开。

在本次专用真实会话读取证据基础上，Adapter 增加逐 Session、逐 reader generation 的
只读合同探测：必须返回 `paginated` metadata、完整稳定 item、最新边界和同一 Turn 的
反向游标重放。成功只设置 `follow:true`，不按 `cli`/`vscode` token 或版本号普遍开放；
每个后续页面仍验证格式。读连接退出/关闭或格式错误后必须重验，旧响应不能复活许可。
这是有界持久历史轮询，不是完整事件订阅；真实 Core/Share 组合结果另行记录。

Core 的 `sessions/open` HTTP 入口已实现但受 fresh `read/open` 能力限制。它只接受
Owner 明确确认，使用固定 bundle 和 UUID 链接，并拒绝历史 managed 同 ID 及未确认
Writer 状态。202 仅表示系统接受请求。合成 opener 测试不实际启动应用；此实现不能
替代真实 Open 验收，更不构成同一 Session 的安全交还。

## Desktop 深链的静态证据

2026-09-03 对本机 Codex Desktop 26.831.21537 / build 7579 的安装 bundle 做只读检查；
实际安装路径为 `/Applications/ChatGPT.app`，bundle ID 为 `com.openai.codex`。其
`Contents/Resources/app.asar` 内原生复制链接实现生成 `codex://threads/<thread.id>`，
主进程要求 `codex:` protocol、`threads` host 与 UUID 格式 conversationId；找到对应
Thread 后导航 `/local/<conversationId>`。这些证据比单纯注册 scheme 更具体，但不是
一次真实 Open 验收。

精确代码位置：`webview/assets/app-initial-b09f80199db1.js` 的 `cBi`（line 2845）；
`.vite/build/src-B8dS-jjl.js` 的 `GE/tD`（line 122）；
`.vite/build/main-7MZ5kTIG.js` 的 `localConversation` 分支（line 1485）。

外部 URL 解析不读取 `hostId` query，不能以追加该参数声称能准确定位远端机器。将来
生成链接只能带经验证的最小 UUID，不携带可触发额外行为的 prompt/browser/review 参数。
Renderer 同时含 `thread/resume` hydration 路径；尚未证明导航时是否触发，所以 Open
不能被当作已证明只读，managed Writer 存在时尤其不得直接启用。

本次没有启动应用、连接会话、读取个人 profile/历史或调用模型。能力仍为 `open:false`；
后续必须独立验证真实目标身份、cwd 和 Writer 边界。

## 可复现的静态检查

`packages/agent-bridge/scripts/probe-native-capabilities.mjs` 需要
`TEAMCROSS_NATIVE_CAPABILITY_PROBE=1`，只读取 CLI 版本并生成临时协议定义。输出明确
分开 `observed` 与 `acceptance`，实际 CLI/Desktop 验收始终标记为 `not-run`。

## 2026-09-03 显式 opt-in 的真实 Session 检查

用户授权使用专用仓库和会话后，新增了真实读取证据。后续模型调用固定为
`gpt-5.6-luna`；在指定模型前已完成的 Desktop 种子回合沿用应用默认设置，不能记作
Luna 回合。下面的证据不等于原生控制能力已经交付。

- Codex CLI 0.152.1 的专用 `codex exec` 会话完成三个 Luna 回合：原目录工作、原生
  继续、退出自有 CLI 进程后在隔离 worktree 使用同一个 `thread.id` 恢复。没有 Fork；
  第三个回合确认实际 cwd 为隔离目录，旧 source 与同级测试哨兵不可写。这里验证的是
  专用 CLI profile，不是 Team Cross managed Run 的实际 sandbox 参数，更不是交互
  TUI 的输入接管或第三方 Writer fencing。
- Desktop 专用任务通过 Codex App 的任务入口创建和继续，完成 Luna 文本输出与指定
  验收文件写入。实际编译的 Bridge 只请求该准确 ID 的 `thread/read` 和
  `thread/turns/list`，能读到新增消息、fileChange 和 commandExecution。采样期间没有
  `runs.*`、Resume、Fork 或 Session 枚举；重新启动 reader 后沿用 cursor 能继续补读，
  无更新时返回零 entries。未知/不展示材料仍带缺失标记，不声称完整原文回放。
- Desktop 的底层来源实际返回 `vscode`，因此不能把该值等同于 VS Code UI。界面
  来源与 Provider 原始 discriminator 已分开记录，不因一个测试会话而启用整个来源类别。
- 对无 Team Cross Writer 的专用 Desktop 任务执行固定 bundle/UUID 深链，系统请求
  返回成功且任务没有新增 Turn。可视准确定位仍需独立确认；不能由 exit 0、导航请求
  或任务状态推定 Open 验收完成。
- Core 的真实只读链路也已贯通：固定该专用 Desktop Session 的 14 条 entry，完成
  preview、只读 Thread、精确消息批注、Markdown 反馈和重启恢复。冻结快照与反馈保持
  不变，AgentRun/Share/Follow/Command 均为零，原生目录内容校验一致。该检查不包含
  分享 transport 或实时 Follow 状态机。

最初的 managed 写测试在零模型预检时停止：独立 app-server 的 `command/exec` 使用与
生产 `turn/start` 同形的 legacy `sandboxPolicy` 时，工具网络被拒绝，但隔离目录外
的专用 source/sibling 哨兵仍可写。当次未发送模型指令，也未创建 managed Run；
该结果只证明此预检路径没有满足目录边界，尚不能断言 `turn/start` 的
实际行为相同。需先分清入口和权限字段的优先级，不能以先前 CLI named profile 的
成功证据替代生产 Adapter 验收，更不能在未确认边界时继续模型写测试。

随后将 managed Adapter 改为独立进程、随机 named profile、显式 `local` environment，
并分开验证本地命令权限与 MCP/Apps 等外部工具限制。实际编译 Adapter 的零模型检查
已确认：准确的 Luna/model、cwd、roots 和 profile；worktree 可写，而 `.git`、`.codex`、
原 source、同级测试目录、实际 TMPDIR 及 `/tmp` 别名写入均被拒绝，工具网络也被拒绝。
网络开启的独立零模型 profile 检查只连接自有 loopback listener，未访问外部业务。
配置元数据、运行时 feature inventory 与指定新 Session 的 MCP 清单分别校验；空 MCP
配置表并不会清除继承服务器，因此必须按实际 key 逐项关闭。测试不修改个人全局配置。

真实 Turn 曾暴露两项预检无法发现的问题，失败材料保留，不计为通过：

- 第四个 CLI/managed Luna 回合中，原生 `userMessage` 被误映射为 `tool.started`，
  测试器因此把 prompt 中的命令当成已开始执行并过早关闭。现已增加工具类型白名单及
  回归；用户消息、计划、reasoning 和未知项不再伪造工具生命周期。该回合没有完成
  预定的实际命令/文件验收。
- 第五个回合未观察到命令或文件工具执行，模型报告 `code-mode host is disabled`。
  它说明仅禁用 feature 并通过配置检查不足以证明真实工具可用；需要区分本地执行
  宿主与模型工具面，不得把零模型 `command/exec` 成功替代真实 `turn/start` 验收。

这两个失败回合均取得准确 Turn 终态并关闭自有 app-server；没有产生预定 sleep，因而
不构成后台命令终止证明，也不构成任意原生 Writer 的关闭证明。

第六个 Luna 回合保留本地 Code Mode host 后，真实 commandExecution 的六处越界写入
与工具网络探测均被拒绝，worktree 写入、真实 fileChange 及精确文件字节通过。
随后故意启动的后台 sleep 暴露独立缺口：准确 Turn 已 interrupted、自有 app-server
已退出，但测试前从自有子树精确识别的 sleep 仍存活。测试器仅清理该自有进程，记录
整体验收失败；不能把此回合的工具边界通过写成 managed 关闭通过。

官方 `thread/backgroundTerminals/list`、`terminate`、`clean` 是正确的线程后台终端
接口，不是 `command/exec/terminate` 或 `process/kill` 的进程域。但当前实现可能在
OS 退出确认前清除登记或产生完成事件，且本机版本的列表未提供可核验的 `osPid`。
因此空列表、`terminated:true`、item completed 都不能单独洗掉已观察到的存活状态。
遇到无法确认的后台终端必须保留关闭失败与 Writer fence，不声称安全交接；这也是
继续暂停额外模型验收的原因。单纯无后台终端的关闭是另一个较窄的验证范围。

运行期的保守关闭 gate 已通过合成回归；当前安装版本的零模型新 Session、准确空后台
列表与自有 app-server 退出也通过。Core 重启后的持久 Writer fence 尚未完成，相关
草稿未纳入提交：不能把内存中关闭失败的保护推广成重启保证。关闭不确定时只能选择
从已封存 Round Fork 到独立 Thread，不能以重启绕过同 worktree 的未知旧 Writer。

上述测试没有建立能封锁 Desktop 旧输入的第三方接口，也没有持久原生 Writer 交接协议。
终止测试中自有 CLI 进程不代表能够识别并封锁任意原生 Writer。同 ID 技术性 Resume
不能替代“接管 → 崩溃恢复 → 交还”的完整验收，`resume`/`takeControl` 继续关闭。
真实双 Mac、Tailnet/DERP、交互 TUI 与 Claude 仍未在本轮验收。一次性完整日志、测试
会话 ID 和临时路径只留在专用测试材料中，不进入稳定 Wiki。

## 官方来源

- [Codex App Server](https://learn.chatgpt.com/docs/app-server)：只读历史、Resume/Fork
  身份和协议生成接口。
- [Remote connections](https://learn.chatgpt.com/docs/remote-connections)：原生 Desktop
  的跨主机工作流，不代表可供第三方复用的受限接管 API。
