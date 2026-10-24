# 分身默认接手成员动态：实施与验证

目标：实施[当前方案](2026-10-22-shadow-collaboration-and-watches.md)，包含默认接手、持续认识、正常协作、任务接续及准确结果回传。后续探索构思按方案分期，不以仅接通一段路由作为整体完成。

基线：`Meta` / `616ff79`。独立分支：`codex/shadow-activity`。原工作区的阅读体验、界面和嵌入资源改动保留；本次实施在仓库 `.worktrees/shadow-activity` 完成。该基线相对方案核对提交 `c7612db` 的变化未修改分身/成员动态的相关实现。

## 实施清单

- [ ] 本人成员动态的独立增量消费、接收确认、初始回顾、保留与断线恢复；人的已读不参与 Agent 水位。
- [x] 本空间本人的稳定默认分身、无分身/离线/暂停/改选行为与界面。
- [ ] 结构化分身作者、精确来源与作用于现有公开讨论的工具；正常协作无需逐次发送确认。
- [ ] 短观察上下文、按对象合批、有界认识与证据、逐对象处理位置及 `shadow_watch`。
- [ ] 真实事件来源、任务依赖与 checkpoint/wait/finish，结果轮/工作轮接续，早到答复与原生恢复。
- [ ] 简报条目更新、来源版本、原处结果与准确顺流返回。
- [ ] 原动态中的分身处理状态及待我处理，原生最终输出与共享回复分开。
- [ ] 重复动作、自身回环、本人抢先回答、材料撤回、默认分身交接与容量等故障验证。
- [ ] Go test/vet/race、Web check/test/build、两套嵌入资源、浏览器与指定模型的真实路径验证。
- [ ] 当前契约、协议和证据入口同步，测试客户端/进程按准确身份清理。

## 当前进展

2026-10-24，已从原工作区同步最新方案，包含公开 @ 连接双方分身、直接答复接续、普通关注合批观察及围绕任务关联多个关注对象。下面是当前独立分支的实际工程进展，整体目标仍未完成。

方案补充核对：本人更新已关注对象时，即使不产生本人的界面提醒，分身仍需同步相关事实；这与已经实现的准确问题答复接续分别验证。新的充分依据提前满足事实型依赖、原工作会话继续前收到相关变化列为后续候选，不增加首个可用闭环的验收范围。该次方案刷新仅修改设计及入口说明；后续本人事实同步、任务关联认识及任务答复回写的实现、验证分别记录如下。

### 已接通的路径

- 成员动态增加独立消费者、固定初始回顾、顺序增量和身份绑定回执；成员 Core 先持久接收再 ack。人的已读、动态 UI 分页和 Agent 消费位置独立，日志压缩保护未接收的范围。
- 默认分身由本空间本人最早创建的分身确定，新增分身不抢占。路线有 revision，未开始的观察与接续可以转交，已运行事项保留原归属。
- 成员 Core 保存独立 attention 记录，按对象合批读取准确来源、父回复和固定材料版本。失效材料使用 tombstone；观察失败、非法结果或存储失败不会冒充已理解，完成原生回合后可复用已保存结果恢复提交。
- 分身接入现有公开读取、回复、@、成员目录、请求、留言和分身聊天工具。跨 Core 保留真实 shadow 作者；临时咨询不自动扩大本人永久关注。稳定行动 ID 与实际回执跨轮保存，普通协作工具描述已取消逐次发送许可。
- 观察可产生实际接续回合，来源为真实事件，不伪造本人聊天。公开答复经原讨论工具发布，内部观察及动态来源的协调 final 不追加到 C1。
- 任务保存真实来源、父任务、目标 version、进展 revision、完成条件和检查点。工作轮/结果轮可继续安排任务与正常协作，主上下文只带索引，按 id 回读完整任务。

### 等待与结束语义

`shadow_task` 新增 checkpoint/wait/finish。wait 保存准确 discussion/request/job/time 依赖，原生回合结束后释放工作槽；completed 必须显式提交完成条件核对且没有未满足的依赖。原生 final 不再自动完成任务，未结束的成功工作轮进入 needs_followup。

Core 通过有界异步读取检查登记的业务对象，不运行等待模型，也不扫描全空间正文。讨论按原提问及后续回复链分页，覆盖本人自己的回答、等待前已到的答案及超过 UI 最近二十项的回答。新的信号将原任务重新排队，W 使用原 thread；coordination 任务进入当前主段。答复到达与答复充分分开，模型引用实际 signalIds 解除依赖；部分条件满足后可继续独立工作。

已覆盖等待重启、慢速来源读取期间的正常聊天、取消后的晚到读取、同一到期信号只唤醒一次，以及子任务完成不自动结束父目标。故障注入发现并修复了增量提交前修改共享 slice 的问题：现在磁盘保存失败不会提前推进内存 cursor。

### 直接处理与工具升级

直接 @/具体回复现在可以在一次接续回合内行动并保存认识。已有任务依赖优先路由，动态消费与任务读取共享持久接收身份；重启、先轮询后动态和保存失败均不再额外派一次观察。排队期间登记的依赖在直接回合提交前再次核对，改选默认分身后仍可把答复交还原任务。异步原文读取和路由不阻塞成员聊天，局部失败保留待处理项。

直接回合的证据包含本批实际正文及成功调用读取工具得到的准确来源；未经读取的额外引用仍被拒绝。任务的内部 Received 投递记录不进入共享 View 或模型任务包，接收、理解和业务完成保持独立。

新增 `toolsets.go` 保存实际注册摘要。原生工具只在 thread/start 注册，恢复不传假参数；旧主段在有待办且空闲后经笔记/接手轮升级。旧 W 在任务排队接续时升级，保留 J/目标版本、检查点、依赖、已做动作和原 W 身份，首次新轮同时取得有界的上一轮原生结果。工具不变时恢复同一 thread。已有未知输入先核对，未完成轮次或审批不被升级打断；交接失败暂停而不循环重试。

专用 Luna 已验证真实主段和 W 都实际调用升级后新增的工具，并且原等待任务解除依赖、核对完成条件后结束；再次重启复用升级后的主段。完整证据见下方新阶段记录。

### 活跃任务接收来源变化

`relatedFocuses` 默认 live，新增 snapshot 可固定登记时的认识和证据。live 任务通过成员 Core 查询准确对象的原始事件，取消普通关注或静音仍能取得必要事实；空间主机只收到对象和读取位置，不接收私人任务全文。材料包含其版本与讨论，会话包含终态轮次与讨论，本人写入同样接收。每页最多十六个信号，cursor 与待处理页同次保存，已有 understand 合并后继续下一页；各任务仍分别核对自己的完成条件。

接收路径最多四项并发，按上次完成时间安排，避免固定前几项慢来源长期挡住其他对象。等待/执行中的任务都可保存变化；暂停、完成、取消及已移除关系不接受旧读取结果。未理解的已呈现输入不引发无限模型重试。普通观察与任务共享认识回执，普通排队变化可去重，新直接问题仍保留行动路径；单条回执不会越过尚未处理的普通事件推进观察水位。

主机保留策略可能压缩旧事件，此时以 source_resync_partial 明确要求按有界当前来源核对，不声称缺失历史已审阅。待处理页合并后，historyGap 继续保留缺失区间并跨重启保存。已有读取工具负责正文、父项与撤回 tombstone，来源通道只提供元数据及短预览。本阶段的完整工程检查、认证传输回放与专用原生回合已通过，证据见[活跃任务来源变化的验证](#活跃任务来源变化的验证)；完整产品验证与更广泛的保留、诊断工作仍在下列剩余项中推进。

### 简报条目、历史来源与并发合并

成员、参与会话及分身现可用同一业务接口更新简报。update_space_brief 按稳定条目 ID 修改决定与建议、问题和进展；保留真实作者、状态与准确来源，未改动条目保留原作者和时间。发生版本竞争时返回最新简报，分身可合并后继续，无需逐次人工确认。read_space_brief 支持精确修订、单条目与保存回执，readBack 明确查询入口；过期历史不替换为最新正文。

界面支持条目状态和历史来源，人工并发编辑通过原修订、草稿和当前修订合并。独立修改和新增条目自动保留，同一条目或议题的分歧并排显示；关闭合并弹窗不丢失原草稿。结果不明先查询原命令，避免第二次保存。该阶段的工程、真实 Luna/双 Core 及浏览器证据见[简报协作验证](#简报协作与历史来源验证)。

### 后续必须完成

1. 实际提问与任务的持久登记、丢失回执的准确来源核对、直接回合的真实后续工作约束和成员可见提问索引已接通；本人修改已有关注对象的事实同步已接通。任务现可关联多份已形成的认识并在变化后接续，直接吸收的答案通过 understand 回写认识；明确 live 关联的来源变化现可独立于普通关注偏好接入，snapshot 保留固定事实。后续继续核对自然协作中任务关系的建立及完整业务接续。普通回复工具仍可用于直接事实答复；会等待答案并继续的工作需要用 ask 或明确任务依赖登记，不能仅凭回复正文自动猜测任务关系。
2. 本人问题级接回、准确来源合并的等待视图和 ask 新讨论的明确原问题关系已接通，成员可分别查看各任务的条件及下一步；继续补跨问题的待决合并和完整产品交互。普通未登记写入不自动取得 ask 的关联语义。
3. 简报按条目写入与简报修订/条目来源已完成；分身消息和固定任务结果现可按准确来源读取，结果保留目标版本、完成条件核对及证据。继续补证据变化后的针对性更正、准确任务结果顺流回传和关联关注的评审内容。任务回传还需捕获原调用会话、建立独立投递记录，并将尚未送达的结果纳入保留语义；当前 256 份来源窗口不等于回传必达。
4. 默认分身选择和个人动态处理状态已接通，展示准确事件与任务的接收、处理、等待、完成及暂停；列表只带摘要，展开按需读取明细。该状态不代替真实业务对象最终送达跟踪，旧回执的当前有效性及更多本机诊断继续在第 5 项推进。中英文、双嵌入资源及浏览器结果见本阶段记录。
5. 队列公平性、已处理 attention/action 记录的有界保留、可见本机诊断、多分身重复答复、发送前本人已回答的竞争检查。请求/分身消息的持久受理与最终送达仍需分别核对，不能只用行动缓存宣称完成。
6. 默认接手、公开 @ 问答到准确结果回传的完整产品验证。新增原生工具升级已通过，但其受控答复来源不替代真实多 Core 讨论、待决项与顺流的闭环；单 Mac 结果也不等于多 Mac 验收。

### 实际提问与接续一致性

`shadow_task ask` 先原子保存问题、任务依赖与稳定行动，再通过既有讨论工具发送。现有 W 可登记问题后继续独立工作，新建 coordination/work 任务可以直接 waiting；questions 提供有界回读入口。问题与公开 requestId 分开编号，记录真实正文、收件成员、答复条件、下一步及任务/原事件关系。明确使用同一 questionId 可让两个任务等待同一事实。

写入回执丢失时，主机按原命令及实际 shadow/member 查回准确根项或回复；读取不重新发问。准确问题尚未绑定时，早到的个人动态答复也先核对原问题及回复链再接入任务。来源绑定、答复信号和处理位置共同持久保存，失败回滚不提前消费。Q 的 saved 不伪造原写入 action 回执，也不表示答案已充分。

直接处理的 continue/needs_owner 由 Core 从实际登记或修改的任务、真实提问和收件人生成依据。没有实际后续安排的最终文字会保留为失败认识，既有动作仍可核对；no_action 不结束原任务。关联 J/Q 索引保存到本对象认识，私有提问身份与传输核对信息不进入共享 View。

### 首次关注与认识目标

2026-10-24，关注保存与内部 follow_changed 信号共用源记录 outbox；手动与自动关注均可建立有界基线。消费时只接收当前偏好 revision，初始化不进入人的动态、空间全部列表、未读或已读。当前材料版本、根讨论及最多八条近期回复、最近已完成轮次在保存时固定；空会话明确没有已完成轮次。实际读取的来源才进入认识证据，成员 Core 收到静音及取消控制后重新核对未开始的普通工作。

新增 shadow_watch 的 read/list/start/update/pause/resume/complete。start 先保存本机意图，再复用成员原有关注；目标版本与认识 revision 分开，真实成员消息是来源。读列表有界，完整认识按 id 取得；暂停保留队列，结束目标保留认识。丢失远端回执与重启后使用原 requestId 核对，已确认旧命令不会把后来被本人取消的关注再打开。

全量故障回放发现此前行动账本在失败回滚后可能变为 nil：JSON 的 omitempty 丢掉了空 map。已让行动与观察 thread 账本在持久快照中保留空对象，并补充连续失败及恢复后写入的确定性回归。另修正新关注控制 map 首次克隆后的初始化；保留失败记录，不把首次检查写成全部通过。

专用 gpt-5.6-luna 实际执行开始关注、基线理解、修改目标、暂停、重启、恢复排队动态和完成目标。主 thread 在重启后保持相同身份，五个成员请求只产生十条共享消息（请求与对应回复），两轮内部认识未进入 C1。该模型测试的业务来源受夹具控制；真实 TLS、源记录原子性和别名映射另由 collab 工程测试覆盖，不宣称已经完成跨 Core 原生产品闭环。完整证据见下方本阶段记录。

### 关注目标与任务控制

2026-10-24，J 的真实触发来源与私有 follow 目标绑定分开。普通关注分析及绑定父任务派生的任务默认跟随目标；直接 @ 的明确问题和独立成员委托默认独立。所属成员可以通过自然语言让 Agent 在 start 中关联已有 followId；这不增加第二套关注配置或逐次确认。

目标改变会推进 J.version 并保存停止意图。Core 中断准确的工作或结果回合，终止确认后再进入 paused、排队重核或 cancelled；停止回执不代表已停止，未知输入先通过原生历史核对。即使分身已暂停接收，仍处理已有运行任务的目标停止。调度前和原生完成回调都检查当前关注版本，防止晚到 final 抢先完成旧目标。

检查点、原任务来源、已做动作和答复跨暂停与重启保留。暂停期间到达的准确答复存入原任务，不启动模型；恢复回原 W，通过 replan 保存新要求、完成条件和保留依赖，其他旧证据仍留在原生记录。完成关注取消尚未结束的绑定任务，已完成任务保持原版本事实，不将旧通知转为新目标完成。私有绑定不进入共享 View 或主段任务索引，重核轮的内部输出也不追加为共享聊天。

本阶段没有修改前端。新增 paused 和目标重核状态仍需后续界面呈现；任务与多个关注对象的来源关系、问题级接回、准确顺流结果及完整跨 Core 流程仍在剩余清单中。

## 前阶段验证记录

环境为本机 macOS、Meta `616ff79` 加此分支未提交改动；原生协议回放使用 fake native，通信使用测试夹具中的真实 HTTP/TLS。未调用真实模型、未启动交互浏览器、未修改原工作区的产品代码。测试源文件清单及散列见[源码状态](2026-10-24-shadow-activity-evidence/source-state.json)。

已确认的中间检查：

- 生命周期首次回放暴露存储失败时 cursor 提前前移，修复后任务/依赖定向测试通过：shadow 6.335s、collab 2.895s。
- 新增慢读取、取消、时间依赖、等待环和有界索引后，定向任务/依赖测试通过：shadow 6.857s、collab 3.115s。
- 在最终任务索引与补充测试之前，完整受影响包测试通过：shadow 15.594s、collab 46.290s、mcp 0.550s；spacecontext 无独立测试。此结果仅作阶段记录，最终检查见下方。

该阶段检查均返回退出码 0，当时再次核对的 48 个修改/新增 Go 文件散列与测试前状态一致；后续直接处理与工具升级以新阶段证据为准：

| 检查 | 实际结果与证据 |
| --- | --- |
| `go test ./... -count=1` | 全仓库通过；shadow 22.226s、collab 56.494s。见 [Go 测试](2026-10-24-shadow-activity-evidence/go-test.log) |
| 相关 shadow/collab race | 本轮任务、依赖、观察、消费者、来源、作者、行动及更新/升级路径通过；shadow 17.804s、collab 16.931s。完整筛选表达式见 [检查记录](2026-10-24-shadow-activity-evidence/checks.json)，输出见 [race 日志](2026-10-24-shadow-activity-evidence/race.log) |
| `go vet ./...` | 退出码 0，无诊断输出；记录见 checks.json，空输出保存在 [vet 日志](2026-10-24-shadow-activity-evidence/go-vet.log) |
| 文档与差异 | 当前契约、协议和证据入口已更新；相对路径、入口及 `git diff --check` 在收尾时核对 |

Web 检查、嵌入构建、浏览器和新增真实模型路径尚未执行。真实原生验收需显式提供专用夹具，本轮普通 Go 测试没有替代它。本轮未手动启动原生模型服务或浏览器；测试进程均已结束，夹具通过各自 cleanup 关闭测试 Core/传输。

## 直接处理与工具升级验证

2026-10-24，在同一独立工作树继续实施；同步当前 591 行设计方案。源码与脚本状态见[本阶段散列](2026-10-24-shadow-activity-evidence/direct-and-toolsets/source-state.json)，完整命令与退出码见[检查记录](2026-10-24-shadow-activity-evidence/direct-and-toolsets/checks.json)。

| 检查 | 结果与范围 |
| --- | --- |
| 全仓库 `go test ./... -count=1` | 退出码 0；shadow 54.563s、collab 52.659s；[日志](2026-10-24-shadow-activity-evidence/direct-and-toolsets/go-test.log) |
| `go vet ./...` | 退出码 0、无诊断；[日志](2026-10-24-shadow-activity-evidence/direct-and-toolsets/go-vet.log) |
| shadow/collab 相关 race | 退出码 0；shadow 46.846s、collab 10.711s；覆盖直接/观察分流、依赖、来源、消费者和工具升级；[日志](2026-10-24-shadow-activity-evidence/direct-and-toolsets/race.log) |
| 工具升级定向回放 | 5 项通过，3.488s；旧主段、活跃轮次/审批、任务与动作保留、注册顺序、失败停止重试；[日志](2026-10-24-shadow-activity-evidence/direct-and-toolsets/toolsets-unit.log) |
| 真实原生工具升级 | `TestNativeShadowToolsetUpgrade` 通过，74.72s；Codex 0.163.0-alpha.11.2、gpt-5.6-luna/openai/low，专用 home/repo；[日志](2026-10-24-shadow-activity-evidence/direct-and-toolsets/native-toolsets.log)、[结构化证据](2026-10-24-shadow-activity-evidence/direct-and-toolsets/native-toolsets-report.json) |

真实测试共创建四个原生 thread：旧/新主段及旧/新工作会话，发生三次准确恢复；两个新会话都实际调用新增只读工具，再次重启后的新主段再次调用成功。原任务保持业务目标版本，依赖被真实工具调用解除，完成条件核对后结束。答复来源是受控夹具，此项只证明原生升级与任务机制，不证明默认 @ 的完整多 Core 产品流程。

回放发现迁移 W 时仅保留结构化检查点会遗漏上一轮原生最终输出，已补为首次新 W 输入附带有界结果及原 thread/turn 引用；旧完整历史保留。测试夹具的缺少依赖读取入口和空行动 map 另已修正，未放宽产品校验。

新阶段没有前端改动，尚未执行新增界面的 Web/嵌入资源/浏览器验收。原生测试已正常退出并通过各 Engine.Close 关闭专属 app-server；`lsof` 对本次 fixture 目录核对无打开文件句柄。未创建浏览器或真实团队消息，未修改个人原生配置及原工作区产品代码。

## 首次关注与目标管理验证

2026-10-24，Meta `616ff79` 加同一独立工作树改动。当前 64 个修改／新增 Go 与 Python 文件的状态见[源码散列](2026-10-24-shadow-activity-evidence/follows-and-watches/source-state.json)，完整命令及退出码见[检查记录](2026-10-24-shadow-activity-evidence/follows-and-watches/checks.json)。

| 检查 | 实际结果与范围 |
| --- | --- |
| 全仓库 Go 测试 | 退出码 0；shadow 54.652s、collab 59.911s；[日志](2026-10-24-shadow-activity-evidence/follows-and-watches/go-test.log) |
| 相关 shadow/collab race | 退出码 0；shadow 41.678s、collab 9.179s；覆盖关注、消费者、观察、目标管理及行动回滚；[日志](2026-10-24-shadow-activity-evidence/follows-and-watches/race.log) |
| `go vet ./...` | 退出码 0，无诊断；[日志](2026-10-24-shadow-activity-evidence/follows-and-watches/go-vet.log) |
| 定向故障与恢复回放 | shadow 1.474s、collab 3.553s；[日志](2026-10-24-shadow-activity-evidence/follows-and-watches/focused-recovery.log) |
| 原生关注接续 | `TestNativeShadowWatch` 通过，78.98s；Codex 0.163.0-alpha.11.2、gpt-5.6-luna/openai/low；[日志](2026-10-24-shadow-activity-evidence/follows-and-watches/native-watch.log)、[结构化报告](2026-10-24-shadow-activity-evidence/follows-and-watches/native-watch-report.json) |

真实测试记录了 read/start/read/update/read/pause/read/resume/read/complete 十次 shadow_watch 调用。只调用一次关注保存，读取两次受控来源；三个原生 thread 分别是共享主段和两轮内部认识，重启准确恢复一次原主段。最终 goalVersion 为 5、state 为 completed，认识及来源仍在，共享消息数为十，没有为观察伪造本人消息或创建执行任务。

首次全量检查暴露空行动 map 回滚后崩溃，导致该次 shadow 测试超时；该次 collab 检查另有旧 outbox 数量断言未包含新的私有初始化。修复及更新断言后执行上方完整检查，原失败证据保存在[修复前日志](2026-10-24-shadow-activity-evidence/follows-and-watches/pre-fix-full-check.log)，不把失败检查列为通过。

原生运行在新的专用目录，认证仅通过 symlink 引用。测试结束后所有 Go 会话退出，Engine.Close 关闭其原生进程；按该 fixture 的准确目录核对进程列表及 lsof 均为空。本阶段无前端改动；新增界面、完整多 Core 原生问答和准确顺流回传仍未验收，Web／两套嵌入资源／浏览器检查留在对应实现阶段。

## 关注目标关联任务的验证

本阶段实现位于独立 `codex/shadow-activity` 工作树；不是已合入 Meta 的声明。真实原生场景使用专用 Luna、全新 home/repo 和只引用认证的符号链接；没有使用个人会话作为测试现场。

- 原生 `TestNativeShadowTaskFollow` 通过，91.31 秒（package 91.780 秒）。真实模型依次 start 关注与任务、保存 checkpoint、进入隔离 MCP 等待工具；pause 后准确原生 turn 收到中断且终止，再 update 目标、重启并 resume。原 W 实际调用 replan 和 finish，任务及结果版本均为 4，完成条件已经更新。
- 原生共创建两个 thread（主段与 W），重启分别准确恢复一次，共两个 resume；没有为目标重核替换 W。动态工具共十三次调用，一次准确原生中断。两次隔离测试工具原生确认只由测试夹具处理，不改变产品的继承配置或原生审批行为。
- 工程回放覆盖独立直接 @ 不被暂停、普通关注与子任务绑定、多个目标变更、停止回执与终止的区别、旧结果先于调度检查到达、暂停接收时仍停止旧目标工作、暂停期间答复保存、重启后依赖和原 W 保留、replan 后核对条件、保存失败回滚和私有信息的共享投影。
- 最后复核又在共享 View 中去除了触发来源里的私有 focusId/goalVersion/followGoal；公开 source 引用仍在。此投影及回归断言发生于真实原生通过之后，不改变原生测试所用的成员消息来源或目标调度路径；最终 Go/race 再覆盖这项修改。真实运行和最终工程检查各有来源哈希记录。

原生前两次运行未完成：首次夹具未处理自己的 MCP 确认，第二次夹具按未加运行时归属的请求编号去重，在重启后漏掉新的确认。第三次修复夹具后通过。失败日志单独保留，不把前两次描述为产品完成验收，也不把真实模型验证替换成模拟回放。

证据：[真实日志](2026-10-24-shadow-activity-evidence/task-follow-control/native.log)、[实际调用与最终状态](2026-10-24-shadow-activity-evidence/task-follow-control/native-report.json)、[原生来源](2026-10-24-shadow-activity-evidence/task-follow-control/native-source-state.json)、[首次夹具失败](2026-10-24-shadow-activity-evidence/task-follow-control/native-initial-fixture-failure.log)、[重启夹具失败](2026-10-24-shadow-activity-evidence/task-follow-control/native-restart-fixture-failure.log)。最终工程命令与返回值见[检查清单](2026-10-24-shadow-activity-evidence/task-follow-control/checks.json)。

此项仍是单 Mac、受控关注来源与本地测试 MCP 的真实原生验证。不能据此宣称双 Mac、公开 @ 的完整跨 Core 闭环、问题级本人接回、新状态界面或准确顺流回传已验收。

本阶段最终工程检查：

| 检查 | 结果与范围 |
| --- | --- |
| 全部 Go test | 通过，shadow 61.735 秒、collab 59.021 秒；[日志](2026-10-24-shadow-activity-evidence/task-follow-control/go-test.log) |
| 相关 race | 通过，shadow 54.996 秒；覆盖任务、注意力、后台更新和关注生命周期；[日志](2026-10-24-shadow-activity-evidence/task-follow-control/race.log) |
| go vet / Core build | 均通过；[vet](2026-10-24-shadow-activity-evidence/task-follow-control/go-vet.log)、[build](2026-10-24-shadow-activity-evidence/task-follow-control/go-build.log) |
| Web check / test / build | Node v24.19.0、pnpm 11.19.0 下通过，核对 1,265 条翻译；25 个文件、238 项测试通过；两套嵌入资源重建后与基线无差异；[check](2026-10-24-shadow-activity-evidence/task-follow-control/web-check.log)、[test](2026-10-24-shadow-activity-evidence/task-follow-control/web-test.log)、[build](2026-10-24-shadow-activity-evidence/task-follow-control/web-build.log) |
| 来源及文档 | [最终来源哈希](2026-10-24-shadow-activity-evidence/task-follow-control/source-state.json)、[链接与索引检查](2026-10-24-shadow-activity-evidence/task-follow-control/document-checks.json)；原生运行后的共享投影差异单独列出 |

原生三次专用夹具均已关闭。按精确 fixture 路径核对进程及 lsof，没有残留运行进程或打开句柄。现有前端检查只证明既有界面回归与构建通过，没有新增状态界面的浏览器验收。


## 实际提问与接续验证

2026-10-24，在同一独立分支完成 `shadow_task ask/questions`、准确提问来源恢复、直接回合的真实后续工作核对。新增回归覆盖：发送前已落盘、存盘失败不发问、稳定 ID 的内容冲突、丢失回执重启、早到答案、来源绑定失败回滚、同一问题明确供多个任务使用、修改既有任务的关联记录、把同事待答错误说成本人待答，以及 no_action 保留真实等待任务。真实 Core/TLS 回归分别验证根提问和回复提问，按实际 shadow/member/action 查回来源，保留本机/主机空间别名，避免另一分身或错误回复分支串入。

最终验证对应[源码快照](2026-10-24-shadow-activity-evidence/question-continuation/source-state.json)，完整命令和环境见[检查清单](2026-10-24-shadow-activity-evidence/question-continuation/checks.json)：

| 检查 | 结果与范围 |
| --- | --- |
| 全部 Go test | 通过，包并发为 2；collab 50.395 秒、shadow 62.322 秒、service 2.952 秒；[日志](2026-10-24-shadow-activity-evidence/question-continuation/go-test.log) |
| 相关 race | 通过，shadow 56.982 秒、collab 5.992 秒；覆盖任务、关注、后台、直接处理和准确依赖来源；[日志](2026-10-24-shadow-activity-evidence/question-continuation/race.log) |
| go vet / Core build | 均通过；[vet](2026-10-24-shadow-activity-evidence/question-continuation/go-vet.log)、[build](2026-10-24-shadow-activity-evidence/question-continuation/go-build.log) |
| Web check / test / build | Node v24.19.0 / pnpm 11.19.0，通过 1,265 条翻译、25 个测试文件共 238 项；两套嵌入资源与基线无差异；[check](2026-10-24-shadow-activity-evidence/question-continuation/web-check.log)、[test](2026-10-24-shadow-activity-evidence/question-continuation/web-test.log)、[build](2026-10-24-shadow-activity-evidence/question-continuation/web-build.log) |
| 真实原生提问与恢复 | Codex 0.163.0-alpha.11.2 / gpt-5.6-luna / low，48.96 秒通过；2 次 thread/start、1 次准确恢复、4 次动态工具调用，问题只写一次；[日志](2026-10-24-shadow-activity-evidence/question-continuation/native.log)、[结构化结果](2026-10-24-shadow-activity-evidence/question-continuation/native-report.json) |
| 文档与来源 | 核对相对链接、源码哈希、脚本语法、索引入口及 diff 空白；[检查结果](2026-10-24-shadow-activity-evidence/question-continuation/document-checks.json) |

原生模型实际调用 ask 创建等待任务；夹具模拟远端问题已保存而回执丢失。Core 重启后只读找回准确问题并接收本人的版本答复，原任务创建工作会话，模型依据实际依赖信号解除等待并 finish completed。Q 的 delivery 已确认 saved，而原 action 回执仍保持 unknown，未伪造网络确认；24 次依赖读取不产生额外模型观察。该测试证明新增原生工具与持久任务恢复，讨论来源为受控回调；真实业务来源及作者核对由独立 collab HTTP/TLS 测试覆盖，二者不合称为完整多 Core 原生产品链路。

首次完整 Go 并行批次的既有 service active 升级用例，在三秒构建探测后返回 installation_unavailable；[首次日志](2026-10-24-shadow-activity-evidence/question-continuation/go-test-initial.log)保留。未修改 service 实现，单项复核及最终完整检查均通过，未确认其环境根因。首轮原生测试也通过；完善既有任务操作的运行关联后，又在全新 b 夹具对最终源码重新验证，早期证据单独保留。

a/b 两次专用原生夹具均已关闭；按准确路径核对进程及 lsof，没有遗留进程或打开句柄。本阶段未新增用户界面，既有 Web 回归不视为新状态界面的浏览器验收。整体方案的剩余工作仍以上方“后续必须完成”为准。


## 问题级接回与成员等待页验证

2026-10-24，继续在独立 `codex/shadow-activity` 工作树推进。问题级“这条我来／交回分身”、同空间本人所有本机分身的等待索引、本机 UI 接口和动态页交互已经接通；本阶段不表示已经合入 Meta 或完成整体目标。

- 交接按准确根问题／回复保存稳定命令与版本。发送意图提交和接回按同一记录锁排序：接回后新的准确代答会被保留为 held_for_owner，接回前已提交的写入仍可收到真实回执。混合任务的其他问题继续处理；已接回的单一问题也不能通过另起根讨论继续追问。问题及任务的首次 ask 登记同样在接回检查之后，避免留下没有发送的假等待。
- 原讨论读取固定接回时的回复位置，区分本人和其他成员的回答；答案、游标和状态同次保存，失败回滚。交回保留问题原文、已有答案和实际任务，不伪造事件序号或 C1 用户消息。普通关注暂停／改目标不使明确交回失效，旧问题结果也不覆盖新关注目标认识。实际本人来源的 handling/handoff 工具和本机页面共用状态。
- “别人等我／我等别人／待接续”直接投影实际依赖，按准确问题合并多个任务，逐条保留等待条件。答复已到、发送状态待核对和内部等待分别显示。翻页绑定投影版本；修改默认分身不丢失本人回复分工。问题详情提供准确摘录、原文定位和无确认交接。
- 浏览器检查发现 Discussion 内层可见性观察器会从等待详情清掉未读，已增加明确的读回执开关，并补带 IntersectionObserver 的交互回归。最终夹具中打开问题、接回、交回之后仍保留原来的 2 组未读。普通动态／讨论的正常可见已读行为继续由原路径维护。

验证证据位于 [question-handling](2026-10-24-shadow-activity-evidence/question-handling/go-checks.json)：

| 检查 | 结果与实际范围 |
| --- | --- |
| 全仓库 Go test | 通过；shadow 63.607 秒、collab 51.296 秒；[日志](2026-10-24-shadow-activity-evidence/question-handling/go-test.log) |
| 相关 race | 通过；shadow 57.672 秒、collab 6.459 秒；包含任务、注意力、关注、交接、等待与依赖；[日志](2026-10-24-shadow-activity-evidence/question-handling/race.log) |
| go vet / Core build | 均通过。前端读回执修复后重新构建 Core，包含最终两套嵌入资源；[原检查](2026-10-24-shadow-activity-evidence/question-handling/go-checks.json)与[最终构建](2026-10-24-shadow-activity-evidence/question-handling/final-build.json) |
| Web check / test / build | Node v24.19.0、pnpm 11.19.0；1,284 条翻译、26 文件 241 测试通过，两套嵌入资源已更新；[命令记录](2026-10-24-shadow-activity-evidence/question-handling/web-checks.json)、[测试](2026-10-24-shadow-activity-evidence/question-handling/web-test.log) |
| 实际浏览器 | 单 Mac 三 Core 夹具，实际 HTTP/TLS 讨论与本机页面；暂停分身使用受控任务记录。中文初版窄窗口 523×615、最终中文／英文 1150×647 已查看截图；[最终状态](2026-10-24-shadow-activity-evidence/question-handling/browser-final.json)、[中文列表](2026-10-24-shadow-activity-evidence/question-handling/waiting-zh-final.jpg)、[准确问题交接](2026-10-24-shadow-activity-evidence/question-handling/takeover-zh-final.jpg)、[英文列表](2026-10-24-shadow-activity-evidence/question-handling/waiting-en-final.jpg) |
| 发现并修复的界面问题 | 内层 Discussion 可见读取影响未读；[发现记录](2026-10-24-shadow-activity-evidence/question-handling/browser-regression.json)，修复后的新夹具再次完整核对，未把初次现象记为通过 |
| 来源和清理 | [最终来源](2026-10-24-shadow-activity-evidence/question-handling/final-source-state.json)列出 301 项源码哈希和 Go 检查后仅前端读回执修复的差异；三个浏览器夹具都正常退出，页签已关闭，准确路径进程检查无残留测试进程，lsof 无句柄，临时 Core 目录已由测试 cleanup 删除；[清理记录](2026-10-24-shadow-activity-evidence/question-handling/cleanup.json) |

此阶段没有新增真实模型运行，不将 fake native 回放、真实 Core 读取或浏览器夹具称为原生多 Core 公开问答闭环。多来源任务新根讨论的明确问题关联、语义上的待决问题合并、本人未显式接回时抢先答复的竞争、默认分身选择／统一状态、简报写入和准确顺流回传仍按上方剩余清单推进。

## 默认分身与动态处理状态验证

2026-10-24，继续在 Meta `616ff79` 上的独立 `codex/shadow-activity` 实施。本阶段补上前阶段尚未完成的默认分身选择与事件处理状态；原工作区的阅读体验改动没有参与本阶段构建。

- “接手我的动态”列出本人分身，选择后直接提交版本化路线；结果不明时保留同一命令，旧回执不回滚较新选择，并发变更后可以重新选择。Core 的就绪状态与尚未创建原生 app-server 分开。
- 个人动态显示准确事件的接收、处理、等待、待本人回答和任务完成。旧任务保留原分身，未开始的动态使用新默认；同讨论的新事件不借用旧任务的完成。历史发送回执和业务完成分别呈现。
- 列表只带状态摘要，展开才读取该事件的任务与动作明细。原始主机页仍为 64 KiB，本机状态附加后最多 96 KiB，单事件详情最多 32 KiB；私有投影不进入共享 /v2、空间全部动态或远端缓存。
- 来源检查发现处理动作可能指向另一个问题，已改为按实际来源构建阅读对象，去掉原通知的预览、关注对象和已读身份。两层阅读观察器均尊重 preserveReadState。
- 首次浏览器复核发现本人分身自发回复被显示为等待接收。消费者本来就排除此类自发事件，现已让个人动态只保留其实际作者和正文，不附加错误等待状态。保留[修复前记录](2026-10-24-shadow-activity-evidence/default-and-status/before-own-output-fix/browser-after-selection.json)。相关回归最初只登记共享分身而没有建立本机 Engine，真实回复工具因此拒绝；补齐暂停本机 Engine 后定向及全量检查通过，未把[不完整夹具的失败](2026-10-24-shadow-activity-evidence/default-and-status/incomplete-fixture/go-test.log)记为通过。

最终检查对应的源码散列分别保存在 [Go 来源](2026-10-24-shadow-activity-evidence/default-and-status/go-source-state.json)与[Web 来源](2026-10-24-shadow-activity-evidence/default-and-status/web-source-state.json)，检查期间源码未变化。

| 检查 | 结果与实际范围 |
| --- | --- |
| 全仓库 Go test / vet | 退出码 0；shadow 68.227 秒、collab 56.957 秒；[命令与退出码](2026-10-24-shadow-activity-evidence/default-and-status/go-checks.json)、[测试日志](2026-10-24-shadow-activity-evidence/default-and-status/go-test.log)、[vet](2026-10-24-shadow-activity-evidence/default-and-status/go-vet.log) |
| 相关 race | 状态、默认路线、成员动态、消费者、attention 与问题交接通过；shadow 41.639 秒、collab 8.842 秒、mcp 1.340 秒；[日志](2026-10-24-shadow-activity-evidence/default-and-status/race.log) |
| Web check / test / build | 1,335 条翻译、27 文件 248 测试通过，两套嵌入资源更新；[命令与退出码](2026-10-24-shadow-activity-evidence/default-and-status/web-checks.json)、[测试](2026-10-24-shadow-activity-evidence/default-and-status/web-test.log)、[构建](2026-10-24-shadow-activity-evidence/default-and-status/web-build.log) |
| 实际浏览器 | 单 Mac 三 Core，真实 HTTP/TLS 讨论、两位暂停分身与受控任务/动作记录，无模型。默认改选直接生效，未开始事件归新分身，等待的两项旧任务保留原归属；[结构化结果](2026-10-24-shadow-activity-evidence/default-and-status/browser-final.json)、[最终夹具退出](2026-10-24-shadow-activity-evidence/default-and-status/browser.log) |
| 已读与来源 | 初始和打开处理详情／实际提问后的 unread=2、readRevision=0。随后普通材料阅读按原规则推进为 unread=1、revision=1；默认选择、后续处理查看及语言切换不再改变该值。[详情前](2026-10-24-shadow-activity-evidence/default-and-status/browser-initial.json)、[详情后](2026-10-24-shadow-activity-evidence/default-and-status/browser-after-source.json)、[切换后](2026-10-24-shadow-activity-evidence/default-and-status/browser-after-selection.json) |
| 截图 | 中文[默认选择](2026-10-24-shadow-activity-evidence/default-and-status/default-zh.jpg)、[动态](2026-10-24-shadow-activity-evidence/default-and-status/activity-zh.jpg)、[准确问题](2026-10-24-shadow-activity-evidence/default-and-status/source-zh.jpg)；英文[动态](2026-10-24-shadow-activity-evidence/default-and-status/activity-en.jpg)、[窄动态](2026-10-24-shadow-activity-evidence/default-and-status/activity-en-narrow.jpg)、[窄默认选择](2026-10-24-shadow-activity-evidence/default-and-status/default-en-narrow.jpg)。桌面 DOM 宽 1150 CSS 像素；窗口覆盖请求 768，实际 DOM/scrollWidth 均为 684，按实际尺寸记录，无横向溢出，覆盖值已恢复。 |

本阶段没有新增真实模型调用，也没有完成最终业务送达核对、完整原生多 Core 公开问答、简报更新或准确结果顺流。这些工作继续按剩余清单推进；整体目标仍未完成。测试页面已关闭，两个浏览器夹具均通过 finish 正常退出；精确路径清理与文档检查见[收尾记录](2026-10-24-shadow-activity-evidence/default-and-status/cleanup-and-docs.json)。

## 原问题关系与提问详情验证

2026-10-24，Meta `616ff79` 加独立分支未提交改动。ask 保存明确 `respondsTo`，实际回复仍保留真实父项；同一 requestId 的重试沿用已持久关系，不能改绑到后来处理的事项。成员从分身原有“记忆与任务”入口查看提问，按需打开原问题、实际已发问题，以及各任务的独立条件和下一步。索引与详情有响应预算和版本化分页，只属于本机所属成员。

证据目录为 [question-relations](2026-10-24-shadow-activity-evidence/question-relations/checks.json)。完整 Go test、vet 和 shadow/collab/mcp 三包完整 race 均通过；检查期间源码未变。Go test 中 shadow 75.250 秒，完整 race 中 shadow 82.151 秒、collab 95.728 秒、mcp 3.174 秒。浏览器发现后台任务列表仍把 waiting 原样显示，修正为复用已有状态文案后重新运行 Web check/test/build：1,349 条翻译、28 文件 250 项测试及两套嵌入资源构建通过。来源核对确认 Go 检查后只修改了 SpaceShadows.tsx 和相应嵌入资源，见[最终散列](2026-10-24-shadow-activity-evidence/question-relations/final-source-state.json)和[最终 Web 检查](2026-10-24-shadow-activity-evidence/question-relations/web-final-checks.json)。

专用 `gpt-5.6-luna` 原生回合通过，实际调用 ask、读取原问题并保存两个关系；模拟写入成功但回执丢失，重启后核对问题及答案并完成任务。questionWrites=1、relationReads=1，见[原生结果](2026-10-24-shadow-activity-evidence/question-relations/native.json)和[日志](2026-10-24-shadow-activity-evidence/question-relations/native.log)。来源和业务发送由测试回调提供；真实 HTTP/TLS 身份及来源仍由 collab 回归独立验证，不把两类证据合称完整原生跨 Core 产品闭环。

实际浏览器使用同 Mac 三 Core、真实 HTTP/TLS 与暂停分身的受控任务记录。最终详情已查看中文浅色 [1024](2026-10-24-shadow-activity-evidence/question-relations/detail-final-1024-zh.jpg)、[768](2026-10-24-shadow-activity-evidence/question-relations/detail-final-768-zh.jpg)，以及英文深色 [1440](2026-10-24-shadow-activity-evidence/question-relations/detail-final-1440-en-dark.jpg)、[768](2026-10-24-shadow-activity-evidence/question-relations/detail-final-768-en-dark.jpg) 截图；实际 DOM 宽与 scrollWidth 相等。Tab 到“查看已发问题”、Enter 打开、Escape 返回详情及列表均按实际焦点验证。已发问题与原始具体回复各自显示准确正文，查看前后 unread=1、readRevision=2；此前普通材料阅读已推进该基线，未将其记为处理详情的变更。详见[浏览器记录](2026-10-24-shadow-activity-evidence/question-relations/browser-final.json)、[查看前](2026-10-24-shadow-activity-evidence/question-relations/browser-read-before.json)和[查看来源后](2026-10-24-shadow-activity-evidence/question-relations/browser-read-after-source.json)。

两个浏览器夹具均通过 finish 正常退出；临时 Core 数据目录已由测试清理，四个确切监听端口不再存在，页面已关闭、视口覆盖已恢复，见[清理记录](2026-10-24-shadow-activity-evidence/question-relations/cleanup.json)。本阶段未完成多关注对象的任务影响接续、简报条目写入或准确结果顺流返回；本人事实同步随后单独推进和取证。

## 本人事实同步验证

2026-10-24，源记录在本人写入前核对有效关注，以独立 agentRecipients 保存 own_source_change；人的 Recipients 和已读不变。首次参与的自动关注继续使用固定初始化，当前取消或静音会抑制普通事实变化，自发 shadow 写入不进入这条通道。普通观察和直接处理包含本人身份与精确 memberChanges，已登记问题仍按准确依赖接续。

工程回放覆盖源提交后日志写入失败与恢复、首次消费者创建前的本人变化、取消和静音、远端成员身份与本机空间映射、材料 v2 固定来源、消费者接收前的保留边界，以及本人补齐事实后不重复创建任务或共享消息。该阶段没有新增真实模型运行。

| 检查 | 结果与证据 |
| --- | --- |
| 全部 Go test | 通过；collab 53.422 秒、shadow 69.952 秒；[日志](2026-10-24-shadow-activity-evidence/own-source-changes/go-test.log) |
| go vet | 退出码 0，无诊断；[日志](2026-10-24-shadow-activity-evidence/own-source-changes/go-vet.log) |
| shadow 与 collab 完整 race | 通过；shadow 76.898 秒、collab 65.208 秒；[日志](2026-10-24-shadow-activity-evidence/own-source-changes/race.log) |
| 来源 | [检查清单](2026-10-24-shadow-activity-evidence/own-source-changes/checks.json)、[源码快照](2026-10-24-shadow-activity-evidence/own-source-changes/source-state.json)、[检查期间源码变化为空](2026-10-24-shadow-activity-evidence/own-source-changes/changed-during-checks.json) |

此处证明独立消费者与认识处理的工程路径。它与下方关联认识的真实工具验证分别取证，不组合宣称已跑通原生多 Core 问答和顺流全链路。

## 任务关联多份认识的验证

2026-10-24，在独立分支接通 relatedFocuses/contextReviews。任务可引用最多八份认识，记录各自用途、上次核对事实和当前事实；变化后接续原 J/W，主上下文仍为索引，工作包只展开变化对象。目标控制继续由原 follow 绑定负责，其他来源变化不改任务目标版本。认识核对必须使用当前任务返回的 revision；未核对的新依据不能被等待或完成操作丢弃。可以直接等待关联认识的变化，无需虚构一条向人的提问。

确定性回放覆盖双来源中的单项变化、原 W 重启、旧版本核对拒绝、保存失败回滚、wait 后晚到事实、无关更新及同事实观察不唤醒、超预算快照、私有投影，以及实际观察结果接回既有工作。原生输入另保存实际提供过的认识指纹；模型遗漏核对时保留 needs_followup，只有后来到达的新事实才自动接续，避免用同一份未核对内容反复启动模型。观察上下文包含正在使用本对象的任务摘要，覆盖同成员旧默认分身；同名对象不能跨成员或空间串入。

| 检查 | 结果与证据 |
| --- | --- |
| 全部 Go test | 通过；shadow 75.859 秒、collab 57.372 秒；[日志](2026-10-24-shadow-activity-evidence/task-contexts/go-test.log) |
| go vet | 退出码 0，无诊断；[日志](2026-10-24-shadow-activity-evidence/task-contexts/go-vet.log) |
| shadow 与 collab 完整 race | 通过；shadow 82.826 秒、collab 64.519 秒；[日志](2026-10-24-shadow-activity-evidence/task-contexts/race.log) |
| Web check/test/build | 完整检查通过后，将通用任务 waiting 文案改为“等待新信息”，再次通过 1,350 条翻译检查、28 个文件 250 项测试与两套嵌入资源构建；[最终检查](2026-10-24-shadow-activity-evidence/task-contexts/web-final-checks.json)、[测试日志](2026-10-24-shadow-activity-evidence/task-contexts/web-final-test.log) |
| 专用原生接续 | Codex 0.163.0-alpha.11.2 / gpt-5.6-luna / openai / low；57.91 秒通过（package 58.214 秒），两个 thread/start、两次准确恢复、五次真实动态工具调用；[日志](2026-10-24-shadow-activity-evidence/task-contexts/native.log)、[报告](2026-10-24-shadow-activity-evidence/task-contexts/native-report.json) |
| 来源与清理 | [工程命令](2026-10-24-shadow-activity-evidence/task-contexts/checks.json)、[验证来源](2026-10-24-shadow-activity-evidence/task-contexts/source-state.json)、[最终来源](2026-10-24-shadow-activity-evidence/task-contexts/final-source-state.json)、[文档检查](2026-10-24-shadow-activity-evidence/task-contexts/document-checks.json)、[专用目录核对](2026-10-24-shadow-activity-evidence/task-contexts/cleanup.json) |

真实工作会话先核对接口 v1 和固定客户端验证后等待；Core 重启并提供接口 v2 后，同一 W 核对变化、保留客户端 v3 验证，按原两项条件完成唯一任务，业务 version 仍为 1。v2 的实际结论为超时可能已经写入、重试需幂等键。来源为专用夹具已形成的认识；测试证明原生工具、上下文与持久接续，不证明真实团队材料发布和多 Core 原生流程。

工程检查期间源码未变；随后只改通用等待文案及其嵌入资源，Go 与原生验证涉及的来源未变，Web 已重新完整检查。本阶段属于后台和协议工作附带的状态文案调整，未新增浏览器截图验收。原生测试正常退出并关闭所建 Engine；精确 fixture 目录的 lsof 无打开句柄，工作区沙盒不允许 ps 全局进程查询，没有按名称终止用户进程。

此阶段关联读取的是已经形成的认识。活跃任务引用的业务来源独立于普通关注偏好持续接收仍未完成；答案回写的后续增量见下方。简报写入、证据更正和准确任务结果顺流继续按完整设计推进。

## 答复回写认识的验证

2026-10-24，在同一独立分支补齐任务直接吸收答复后的认识同步。status 和工作包按实际答复来源、原触发对象及已登记 respondsTo 给出有界目标；工作轮用 shadow_task understand 保存当前认识、准确答复依据及仍未解决的问题。认识、答案处理回执与稳定 requestId 的结果在同一成员 attention 写入中提交，不需要另建协调任务。新认识沿已有 contexts 关联接续其他工作；任务是否已满足仍由自己的 resolutions 和完成条件确定。

版本变化时回读合并，失败不推进答案处理；晚到旧观察不会覆盖任务刚保存的事实，已发动作保留，旧输入按最新认识重新观察。答案先于个人动态时仍可建立对应对象认识；动态后来到达沿任务接收身份去重。分批认识只确认显式列出的答案，未回写部分阻止 wait/completed/needs_input 提前丢弃。

| 检查 | 实际结果与证据 |
| --- | --- |
| 全部 Go test | 退出码 0，shadow 76.517 秒、collab 54.543 秒；[日志](2026-10-24-shadow-activity-evidence/task-understanding/go-test.log)、[命令](2026-10-24-shadow-activity-evidence/task-understanding/checks.json) |
| go vet | 退出码 0，无诊断；[日志](2026-10-24-shadow-activity-evidence/task-understanding/go-vet.log) |
| shadow/collab 完整 race | 退出码 0，shadow 83.703 秒、collab 64.697 秒；[日志](2026-10-24-shadow-activity-evidence/task-understanding/race.log) |
| 八项定向回放与补充 race | 路由后回写、关联任务接续、重启/早到答案、存储失败、无效更新、多原问题原子提交、分页不提前消费、旧观察及已发动作保留；[单元日志](2026-10-24-shadow-activity-evidence/task-understanding/understanding-unit.log)、[race 日志](2026-10-24-shadow-activity-evidence/task-understanding/understanding-race.log)、[命令](2026-10-24-shadow-activity-evidence/task-understanding/supplemental-checks.json) |
| Web check/test/build | 按仓库 pnpm 工作区命令通过 1,350 条翻译检查、28 个文件 250 项测试及两套嵌入构建；[命令](2026-10-24-shadow-activity-evidence/task-understanding/web-checks.json)、[测试日志](2026-10-24-shadow-activity-evidence/task-understanding/web-test.log)、[构建日志](2026-10-24-shadow-activity-evidence/task-understanding/web-build.log) |
| 专用原生工具与恢复 | Codex CLI 0.163.0-alpha.11.2 / gpt-5.6-luna / openai / low；测试 67.50 秒通过，两个 thread/start、两次恢复，实际调用 start/status/wait/status/understand/status/finish。原 J/W 与目标 version 1 保持，认识同时保留 OIDC 和新答复 v3，并按原条件完成；[日志](2026-10-24-shadow-activity-evidence/task-understanding/native.log)、[精简结果](2026-10-24-shadow-activity-evidence/task-understanding/native-summary.json)、[完整报告](2026-10-24-shadow-activity-evidence/task-understanding/native-report.json) |
| 来源与清理 | [检查前来源](2026-10-24-shadow-activity-evidence/task-understanding/source-state.json)、[最终来源](2026-10-24-shadow-activity-evidence/task-understanding/final-source-state.json)、[夹具与命令](2026-10-24-shadow-activity-evidence/task-understanding/native-fixture.json)、[进程资源核对](2026-10-24-shadow-activity-evidence/task-understanding/cleanup.json) |

初次 Web 检查误用了 npm 的 workspace 参数，未执行项目检查；[错误记录](2026-10-24-shadow-activity-evidence/task-understanding/web-check-npm-attempt.log)保留，随后按项目 pnpm 命令全部通过。全量 Go 与原生验证之后只新增了多原问题原子性和跨依赖答复分页两个测试，产品源码、工具说明和脚本未变；八项定向测试与对应 race 在最终测试文件上重新运行。完整来源散列记录这一差异。

真实原生答复由专用夹具提供，证明模型实际使用认识回写工具及同一工作会话恢复，不证明真实团队讨论或多 Core 顺流全链路。专用测试已退出并关闭其引擎，准确夹具目录的 lsof 未发现打开句柄；本阶段没有启动浏览器或更改前端交互。当时尚未覆盖的原始业务变化接收随后按下节独立推进；简报按条目写入、证据变化后的答复更正及准确任务顺流返回仍待完成。

## 活跃任务来源变化的验证

2026-10-24，基于 Meta `616ff795f6e7b2eec0e41b2943070e3f707db60c` 及 `codex/shadow-activity` 的未提交改动，接通明确 live 关联的原始来源读取。普通关注取消或静音后，活跃任务仍按登记对象取得实际变化；snapshot 关系保留固定认识及证据，不参与轮询。原始事件与读取位置同次落盘，任务通过既有 understand 写回认识后再继续读取，最后各自核对完成条件。

八项任务来源回放覆盖同一 W 的重启接续、固定证据、磁盘失败回滚、非法及过时返回、来源公平性、wait 后到达的新输入、已理解普通变化与直接问题分别处理，以及认识合并后的历史缺口跨重启保留。四项 collab 回放使用实际认证传输及源记录，覆盖本人和同事的二十五条回复分两页取得、静音和人的已读保持、材料子讨论和撤回、准确执行轮次隔离，以及持久日志压缩后的有界重建。该通道不把全空间正文交给模型，也不使用人的已读作为任务位置。

| 检查 | 结果与证据 |
| --- | --- |
| 最终全部 Go test | 退出码 0，shadow 78.662 秒、collab 55.893 秒；[日志](2026-10-24-shadow-activity-evidence/task-sources/final/go-test.log)、[命令](2026-10-24-shadow-activity-evidence/task-sources/final/checks.json) |
| 最终 go vet | 退出码 0，无诊断；[日志](2026-10-24-shadow-activity-evidence/task-sources/final/go-vet.log) |
| 最终 shadow/collab 完整 race | 退出码 0，shadow 84.861 秒、collab 68.147 秒；[日志](2026-10-24-shadow-activity-evidence/task-sources/final/go-race.log) |
| Web check/test/build | 1,350 条翻译检查、28 个文件 250 项测试及两套嵌入构建通过；[命令](2026-10-24-shadow-activity-evidence/task-sources/web-checks.json)、[测试日志](2026-10-24-shadow-activity-evidence/task-sources/web-test.log)、[构建日志](2026-10-24-shadow-activity-evidence/task-sources/web-build.log) |
| 最终专用原生接续 | Codex CLI 0.163.0-alpha.11.2 / gpt-5.6-luna / openai / low；测试 106.51 秒通过，两个 thread/start、两次恢复，十七次实际动态工具调用；[日志](2026-10-24-shadow-activity-evidence/task-sources/final/native.log)、[精简结果](2026-10-24-shadow-activity-evidence/task-sources/final/native-summary.json)、[完整报告](2026-10-24-shadow-activity-evidence/task-sources/final/native-report.json) |
| 来源与清理 | [最终检查前来源](2026-10-24-shadow-activity-evidence/task-sources/final/source-state.json)、[最终来源核对](2026-10-24-shadow-activity-evidence/task-sources/final/final-source-state.json)、[专用夹具](2026-10-24-shadow-activity-evidence/task-sources/final/fixture.json)、[清理记录](2026-10-24-shadow-activity-evidence/task-sources/final/cleanup.json)、[文档检查](2026-10-24-shadow-activity-evidence/task-sources/final/document-checks.json) |

原生测试先登记 live 接口和 snapshot 客户端验证两份关系，等待后重启并静音普通关注。恢复后，原 J/W 实际合并接口 v2 的超时写入语义，保留 OIDC 和固定客户端 v3；共享认识中的客户端 v4 没有进入原任务的固定范围，snapshot 来源也没有被查询。两次 understand 调用形成一份持久命令和答案回执，原目标 version 1 保持；最终 ResultEventID 与 DeliveredEventID 相等，全部六个已启动回合均为 completed。

首轮完整检查与原生结果保留在[初次检查](2026-10-24-shadow-activity-evidence/task-sources/checks.json)。随后增加 historyGap 持久化及其回放，并把原生夹具退出条件收紧为结果已送达、所有启动轮次已结束；因此在新夹具上重新执行了全部 Go test/vet、完整 race 和原生回合。两轮之间只改动 task_sources.go 及两个相关测试文件，Web 来源和嵌入资源未变；最终检查期间产品来源未变。

原生来源正文由专用回调控制，证明真实工具调用、原 W 恢复、认识合并和分身内部结果送达。认证传输、业务来源与分页由上述 collab 回放独立证明；两类证据不合称完整多 Core 模型问答或准确顺流回传，更不扩展为两台 Mac 验收。本阶段没有新增前端交互或浏览器验收。专用原生测试正常退出并关闭引擎，精确夹具目录的 lsof 无打开句柄，没有按进程名称批量终止程序。整版方案继续按上方“后续必须完成”推进。


## 简报协作与历史来源验证

2026-10-24，Meta `616ff79` 加独立 `codex/shadow-activity` 未提交改动。新增简报条目写入、实际作者与状态、固定修订读取、稳定保存回执以及人工编辑合并。简报正文、历史与回执在同一业务保存中提交，失败回滚；旧正文超出保留范围后明确不可用，去重回执仍保留。空间工具与分身任务证据沿用同一 SpaceSourceRef，不再另造来源格式。

| 检查 | 实际结果与证据 |
| --- | --- |
| 全部 Go test | 退出码 0，collab 64.785 秒、shadow 79.323 秒、mcp 0.582 秒；[日志](2026-10-24-shadow-activity-evidence/brief/engineering/go-test.log)、[命令](2026-10-24-shadow-activity-evidence/brief/engineering/go-checks.json) |
| go vet 与三包完整 race | vet 退出码 0；race 的 shadow 88.358 秒、collab 93.384 秒、mcp 3.055 秒；[vet](2026-10-24-shadow-activity-evidence/brief/engineering/go-vet.log)、[race](2026-10-24-shadow-activity-evidence/brief/engineering/go-race.log) |
| Web check/test/build | 最终通过 1377 条翻译检查、29 个文件 257 项测试与两套嵌入构建；[命令](2026-10-24-shadow-activity-evidence/brief/engineering/web-checks.json)、[测试](2026-10-24-shadow-activity-evidence/brief/engineering/web-test.log)、[构建](2026-10-24-shadow-activity-evidence/brief/engineering/web-build.log)、[构建后嵌入检查](2026-10-24-shadow-activity-evidence/brief/engineering/embed-test.log) |
| 专用 Luna 与真实认证传输 | 单 Mac、两套 Core、gpt-5.6-luna；测试 56.29 秒通过。模型真实读取测试讨论，遭遇实际并发写入后合并、回读命令回执及旧修订；[命令](2026-10-24-shadow-activity-evidence/brief/native-final/native-checks.json)、[日志](2026-10-24-shadow-activity-evidence/brief/native-final/native.log)、[实际工具与最终正文](2026-10-24-shadow-activity-evidence/brief/native-final/native-report.json) |
| 真实浏览器 | 1440/1024/768 CSS 像素，中文浅色与英文深色，条目状态、编辑保存、准确历史来源、键盘返回及真实并发合并均复核；[行为断言](2026-10-24-shadow-activity-evidence/brief/browser/final-browser-checks.json)、[最终夹具](2026-10-24-shadow-activity-evidence/brief/browser/teamcross-brief-browser-final-20261024-b96c18a2-fixture.json)、[正常结束](2026-10-24-shadow-activity-evidence/brief/browser/teamcross-brief-browser-final-20261024-b96c18a2.log) |
| 截图 | [中文简报](2026-10-24-shadow-activity-evidence/brief/browser/final-brief-zh-light-1440.png)、[历史条目](2026-10-24-shadow-activity-evidence/brief/browser/final-history-zh-light.png)、[并发合并](2026-10-24-shadow-activity-evidence/brief/browser/final-merge-zh-light-1440.png)、[窄窗口合并](2026-10-24-shadow-activity-evidence/brief/browser/final-merge-zh-light-768.png)、[窄窗口编辑](2026-10-24-shadow-activity-evidence/brief/browser/final-editor-zh-light-768.png)、[英文深色](2026-10-24-shadow-activity-evidence/brief/browser/final-brief-en-dark-1024.png)、[英文编辑](2026-10-24-shadow-activity-evidence/brief/browser/final-editor-en-dark-1024.png) |
| 来源与清理 | [Go 来源](2026-10-24-shadow-activity-evidence/brief/engineering/go-source-state.json)、[最终 Web 来源](2026-10-24-shadow-activity-evidence/brief/engineering/source-state.json)、[原生来源](2026-10-24-shadow-activity-evidence/brief/native-final/source-state.json)、[最终来源核对](2026-10-24-shadow-activity-evidence/brief/final-source-state.json)、[文档检查](2026-10-24-shadow-activity-evidence/brief/document-checks.json)、[清理](2026-10-24-shadow-activity-evidence/brief/cleanup.json) |

首次原生回合实际合并成功，但将简报保存命令误交 get_space_request 查询，没有完成准确回执核对；[首次日志](2026-10-24-shadow-activity-evidence/brief/native/native.log)与[报告](2026-10-24-shadow-activity-evidence/brief/native/native-report.json)保留。补充 readBack 与明确工具说明后，在新夹具复验通过。最终回合中，重复位置的 itemId 和混用讨论/请求来源的两次无效参数被业务校验拒绝，模型修正后仅保存一次自身更新；最终修订为 3，同事进展与原问题均保留，真实 shadow/member 作者和准确回复依据一致。

第一次全 Go 检查发现新增 HTTP 测试夹具未使用 loopback Host，以及运行时工具数量断言未计入 update_space_brief；修正测试后全量复验通过，[首次结果](2026-10-24-shadow-activity-evidence/brief/engineering/go-test-initial.log)保留。原生成功后 Go 产品逻辑及工具说明未变，仅这两个测试修正；最终 Web 随后补充人工合并体验并独立全量复验，构建后核对嵌入资源。详细来源差异见最终来源核对。

首次浏览器复核暴露“保留草稿但只能取消后重新编辑”的交互缺口。补上条目级合并并覆盖不同修改、删除与证据冲突、旧快照过期、退出合并保留草稿及空来源后，重启专用夹具复核最终资源。实际界面从简报修订 3 开始编辑，测试 Core 并发保存修订 4；页面检测到新版后禁止旧基线直接保存，编辑者比较议题差异并保留自己的修改，最终保存修订 5，同事新增进展和自己的状态修改同时保留。历史来源仍读取修订 1 的原条目。阅读及编辑前后保持 unread=1、readRevision=0，来源弹窗关闭后焦点返回原按钮；三个宽度未见横向溢出，控制台无错误或警告。

最终一致性检查另将空间概览和请求接手上下文的状态显示对齐，保留建议、已确认内容、问题状态及进展。全量 Web 和双嵌入构建随后再次通过；新夹具复核了[概览状态](2026-10-24-shadow-activity-evidence/brief/browser/labels-overview-zh-light-1440.png)与[接手上下文](2026-10-24-shadow-activity-evidence/brief/browser/labels-request-context-zh-light.png)，控制台无诊断，源码见[补充浏览器来源](2026-10-24-shadow-activity-evidence/brief/browser/labels-source-state.json)。

Playwright 使用独立临时缓存和浏览器配置；受限环境不能启动 Chrome 时以本次测试范围在沙箱外启动。两次测试 Core 正常退出，专用浏览器关闭；精确原生夹具/浏览器目录及 PID 的 lsof 无打开句柄，三个测试监听端口均已关闭。未按进程名终止用户程序。

本阶段真实模型通过认证 Core 读取和写入合成空间的业务对象，较前阶段受控来源回调增加了简报传输证据；仍不代表两台 Mac 或完整默认 @ → 提问 → 任务接续 → 顺流回传闭环。整版方案继续按上方剩余项推进。

## 分身固定结果与准确来源验证

2026-10-24，在 `codex/shadow-activity` 的 Meta 基线 `616ff79` 上继续完成准确任务回传的来源基础。本阶段保存不可变 TaskResult，与任务状态在同一次持久替换中落盘；目标更新、接续和换段保留旧结果及其条件、检查点和证据。原生 final 未经业务核对仍为 needs_followup。已同步消息的正文、实际作者、父项及原始时间不能被后续同步覆盖。

`SpaceSourceRef` 增加 shadowId 与 shadowMessageId／shadowResultId。个人、绑定运行时和分身工具的 read_space_shadow 可按准确 ID 读取；Core source 路由支持同一身份核对及跨 Core 别名映射。结果保留最近 256 份，普通读取只给最近 20 份索引与总数，正文按需加载；过期或缺失明确不可用。简报可引用两类来源，来源弹窗显示固定正文及结果状态，失效时清除旧正文，不推进人的已读。

| 检查 | 本阶段结果与证据 |
| --- | --- |
| 持久及真实传输回放 | 新增任务结果回放验证 native final 与业务完成、更新目标后历史不变、重启、失败保存回滚、保留边界及投影不含私有运行字段。跨 Core 回放验证认证读取、准确选择器、旧版本、不可覆盖、过期及空间关闭；见[结果快照测试](../../../internal/shadow/task_results_test.go)和[来源传输测试](../../../internal/collab/shadow_result_sources_test.go)。 |
| Go 工程门槛 | 全量 go test、go vet，以及 shadow/collab/mcp 完整 race 均退出 0；耗时分别 104.347、1.097、133.444 秒。[检查记录](2026-10-24-shadow-activity-evidence/task-results/engineering/go-checks.json)，对应 go-test/go-vet/go-race 日志在同目录。 |
| Web 与嵌入资源 | check 通过 1381 条翻译检查；29 个测试文件、259 项测试通过；Web 与插件嵌入构建成功。[检查记录](2026-10-24-shadow-activity-evidence/task-results/engineering/web-checks.json)。本阶段仅为已有来源弹窗补充两类读取和状态，按服务/协议改动的门槛使用交互测试，没有新增浏览器验收。 |
| 专用真实模型 | `--result-sources` 使用实际 gpt-5.6-luna/openai，通过两套认证 Core 读取指定 v1 结果与原消息，并将两处准确来源保存到简报；当前 v2 未替代旧依据。两次非 UUID 更新请求被服务拒绝，模型自行纠正后只产生一次简报修订。[原生报告](2026-10-24-shadow-activity-evidence/task-results/native/report.json)、[退出状态](2026-10-24-shadow-activity-evidence/task-results/native/status.json)。 |
| 源码与清理 | 工程检查开始后产品源码无变化；专用原生检查退出 0，精确 fixture 目录 lsof 无打开句柄。[源码与清理核对](2026-10-24-shadow-activity-evidence/task-results/cleanup-and-source-check.json)。 |

原生验证的历史任务结果和消息是合成业务数据，真实模型验证的是读取、准确引用和简报写入；任务结果的生成、重启及原子保存由回放独立验证。此结果不等于真实后台任务到原工作会话的完整回传，更不代表两台 Mac 验收。

下一步在 invokeWorkbenchTool 的 send_space_shadow 分支保存已核对 CallerSource 与原消息的本机关系；共享侧使用不透明路线，利用 TaskResult.messageId 和固定结果 ID 建立独立返回记录。MCP/Hook 输出预算、租约、实际送达水位与人的已读继续独立；待答问题需要关联实际问题及当前状态，回答后使尚未投递的旧提示失效。接通返回时同时处理待投递结果的保留，不能直接依赖上述来源历史窗口。
