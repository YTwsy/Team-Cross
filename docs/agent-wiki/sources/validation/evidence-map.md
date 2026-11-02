# 验证范围与证据入口

本页维护会影响当前能力判断的证据摘要与入口。产品规则和实现范围由各领域契约维护，验证方法由 [test-gates](test-gates.md) 维护，完整执行结果保留在任务记录；本页不另建产品支持清单，也不累积全部测试日志。

报告中的验收截图仅本地保存，不随 Git 分发；截图位置与外部留存按[验收截图约定](../../README.md#验收截图)记录。以下验收结果仍以各报告的被测版本和环境为限。

本次整理核对点：2026-10-04，`Meta` / `2acdab3`。这是文档与实现范围的核对点，**不是下列报告的被测提交，也没有在本次整理中重跑验收**。各行保留报告自己的日期、版本和环境；“基线 + 当次改动”不能改写成基线提交已通过。表中没有覆盖的路径表示尚未在此页建立证据入口，不等于未实现、未测试或不支持。

## 一个空间多个协作会话

2026-10-23，`codex/native-session-workspace` / `73e503c` 加本次修改：移除目录独占，现有目录展示跟随 Codex `thread.cwd`。全量 Go、vet、相关 race、Web 209 项测试及构建通过；单 Mac、Codex `0.163.0-alpha.11.1`、Luna 实测同目录双会话、新目录执行与恢复通过，三个 Core 的真实 TUI/MCP 接力及最终原生网关恢复复验通过。记录见[原生目录验证](../../tasks/2026-10-23-native-session-workspace.md)。下述 2026-10-21 的目录租约是已被本次替代的历史规则；本次不新增真实 Claude 目录迁移、Desktop UI 或跨 Mac 证据。

2026-10-21，`Meta` 的 `f49b2a1` 加本任务实现及用户反馈调整：空间/会话身份、全体成员共同阅读、独立输入/审批、存储和目录租约的 Go 回归、vet 与相关 race 通过；Web 178 项测试、1049 条翻译检查、两套嵌入资源构建通过。真实浏览器复核统一阅读布局、宽屏评论默认展开、手动收起、草稿保留及 1440/1024/768、中英与深浅主题。记录见[多会话实现与后续调整](../../tasks/2026-10-21-multiple-sessions/README.md#后续调整全体成员共同阅读与统一阅读界面)。此前逐会话授权的检查、截图及 DMG 是历史阶段，不代表最终读取规则。

当前原生结果为单 Mac、三个 Core、两个 Codex 会话、CLI `0.163.0-alpha.11`、`gpt-5.6-luna`：真实个人 MCP 当前会话分享、直接 TUI、成员共同读取、独立输入与去重、结束一会话保留另一个、同 fork 恢复通过。前一实现阶段的 Tailcat 传输层与协作层同机联网检查通过，原生运行时为替身；本次读取与 UI 调整没有重跑 Tailcat。Claude `2.1.270` 的新 fixture 在来源创建阶段被本机模型代理连接失败阻塞，未进入多会话执行；本任务不重验专用 Desktop 八组合、两台 Mac、跨 NAT 或强制 DERP。旧 schema 3 / tcx3 报告不能直接证明当前新协议已通过。

## 顺流感知

2026-10-22，顺流感知分支的参与会话反馈修复：原生标题、旧占位补全、插件精确关联状态、并发连接去重、面板资源缓存键的 Go/Web 回归通过。production 面板加真实 Core 的隔离浏览器检查通过，宿主桥与身份为合成夹具，不扩大真实 Codex/Cloud 验收范围。记录见[参与会话反馈修复](../../tasks/2026-10-21-in-flow-implementation.md#2026-10-22-参与会话反馈修复)。

2026-10-21，`4d33a78` 基线上的 `codex/in-flow-awareness` 第一版实现。工程回归覆盖四类分发规则、明确开启基线、跨 Core 目标与成员隔离、绑定接收器空间限制、撤销、并发租约、磁盘重载和输出失败；Web 184 项测试、1067 条翻译检查与两套嵌入资源构建通过。真实浏览器在 1265×712 下核对个人目标开关、他人成员只读、保存留言后的待投递状态，以及中文浅色和英文深色显示。

真实原生证据为单 Mac、隔离 Core / home / 仓库、Codex CLI `0.163.0-alpha.11`、`gpt-5.6-luna`：受限模式通过一次 `read_space_brief` 的附带内容取得精确评论标记；信任模式通过 UserPromptSubmit Hook 取得标记，未调用 MCP。两条路径分别核对已投递版本，测试结束关闭所建客户端与 Core。[完整报告](../../tasks/2026-10-21-in-flow-implementation.md) 记录命令、修正的首次脚本判定和证据位置。

本轮不证明 Claude Hook、插件 Hook 的原生自动加载、Desktop/Cloud 宿主或两台 Mac 已验收。插件包导出及命令引号有自动化检查；没有可用 Hook 或 MCP 交互时不会即时投递。当前行为见 [顺流感知契约](../decisions/in-flow-awareness.md)。
## 本机数字分身

2026-11-02，`codex/shadow-activity` 的 `40b606a` 基线加本轮未提交工作区通过消息流与 Work 回报收敛验收。全量 Go/vet、shadow/collab/mcp/nativecodex 完整 race、Web 259 项测试、1381 条翻译检查及两套嵌入构建通过。专用 Luna 在隔离原生会话中验证 A/B 合批、C 忙时追加、准确个人记忆与回复、W 慢工具期间 S 回答，以及 start/report 阶段和完成回报；另以同 Mac 三个真实 Core、TLS 成员凭据复验连续输入、三方可见回报、离线手动重发、准确 thread 和记忆恢复。同讨论多作者、D1/C1 混合路由、丢失回执与发送失败由回放覆盖。新的完成流程不再以认识核对为前置条件，以下旧阶段的理解门槛只作当时证据。未做双 Mac、浏览器或完整默认 @ → Work → 个人会话的真实模型闭环。命令、源码散列、结果与清理见[本轮实施记录](../../tasks/2026-11-02-shadow-message-input-implementation.md)和[精简验收证据](../../tasks/2026-11-02-shadow-message-input-evidence.json)。

2026-10-24，分身委托返回通过全量 Go/vet、shadow/collab/mcp 完整 race、Web 259 项测试与构建。标准主机环境下，实际 Luna 创建并完成后台任务，固定结果经两套认证 Core 与真实 Hook HTTP 入口送回准确发起路线；主机与分身两级 outbox、无配对委托、静音、失败回滚、重启去重及历史窗口过期由回放覆盖。首次受限沙箱测试因嵌套 sandbox-exec 失败而超时，记录保留。发起会话为测试 transcript，不宣称接收模型已理解或双 Mac 验收；待答问题返回仍在后续范围。见[分身委托返回原会话](../../tasks/2026-10-24-shadow-activity-implementation.md#分身委托返回原会话)。

2026-10-24，分身消息与固定任务结果来源通过全量 Go/vet、shadow/collab/mcp 完整 race、Web 259 项测试和双嵌入构建。专用 Luna 经两套认证 Core 读取合成 v1 结果与原消息，并准确引用到简报；没有用当前 v2 替代旧依据。生成/重启/失败回滚与跨 Core 不可覆盖由回放验证，该阶段原生验证范围限于读取和引用，未覆盖准确原会话顺流回传。完整结果、模型纠错及清理见[分身固定结果与准确来源验证](../../tasks/2026-10-24-shadow-activity-implementation.md#分身固定结果与准确来源验证)。

2026-10-24，简报按条目更新、真实作者与状态、历史来源、保存回执与人工并发合并通过全部 Go/vet、shadow/collab/mcp 完整 race、Web 257 项测试及双嵌入构建。专用 Luna 在两套认证 Core 间读取实际测试讨论，合并真实并发更新并回读原命令及旧条目；中文浅色、英文深色与 1440/1024/768 浏览器复核了状态编辑、保留双方独立修改、历史来源及键盘返回，人的 unread=1/readRevision=0 保持。单 Mac 的该场景不等于完整默认 @/任务/顺流闭环。完整结果、首次失败与修复、来源及清理见[简报协作与历史来源验证](../../tasks/2026-10-24-shadow-activity-implementation.md#简报协作与历史来源验证)。

2026-10-24，活跃任务来源接收通过全 Go/vet、shadow/collab 完整 race、Web 250 项测试及两套嵌入构建。八项任务回放和四项 collab 回放覆盖静音后的独立读取、同一 W 接续、固定 snapshot、认证传输、分页、来源公平性与日志缺口持久化。最终专用 Luna 在等待和重启后合并接口 v2，保留 OIDC 与固定客户端 v3，并等待结果送达及所有回合结束。原生正文由回调提供；该阶段未覆盖简报写入，后续证据见上方；完整真实多 Core 模型问答与准确顺流回传仍待完成。完整来源、最终复验及环境边界见[活跃任务来源变化的验证](../../tasks/2026-10-24-shadow-activity-implementation.md#活跃任务来源变化的验证)。

2026-10-24，任务答复回写认识通过全 Go/vet、shadow/collab 完整 race、Web 250 项测试与两套嵌入构建。八项定向回放另覆盖多原问题原子提交及答复分页不提前消费。专用 Luna 在等待和重启后，真实调用 understand 将 v3 答案并入认识、保留 OIDC 事实，再于原 W 核对条件完成；两个 thread/start、两次恢复、七次工具调用。答复来源由夹具提供；独立原始业务变化随后由上方阶段单独验证，完整多 Core 问答顺流仍需继续。见[答复回写认识的验证](../../tasks/2026-10-24-shadow-activity-implementation.md#答复回写认识的验证)。

2026-10-24，多份认识关联任务通过全 Go/vet、shadow/collab 完整 race、Web 250 项测试与两套嵌入构建。专用 Luna 真实核对两份认识、等待、重启后在同一 W 处理一份变化并按原条件完成；两个 thread/start、两次恢复、五次动态工具调用。认识由受控夹具提供；任务答案回写及原始业务变化的后续独立证据见上方。完整来源、日志与边界见[任务关联多份认识的验证](../../tasks/2026-10-24-shadow-activity-implementation.md#任务关联多份认识的验证)。

2026-10-24，本人对已有关注对象的事实同步通过全 Go/vet 与 shadow、collab 完整 race。覆盖日志失败恢复、初始消费、当前偏好、远端身份、固定材料版本、保留边界和内部认识，不增加人的提醒或已读；该阶段没有新增原生模型运行。证据见[本人事实同步验证](../../tasks/2026-10-24-shadow-activity-implementation.md#本人事实同步验证)。

2026-10-24，明确原问题关系及本机提问详情通过全 Go/vet、相关三包完整 race、Web 250 项测试与两套嵌入构建。真实浏览器核对 1440/1024/768 CSS 像素、中英文与深浅主题，准确来源打开及键盘返回保持人的 unread=1、readRevision=2。专用 Luna 实际登记两个原问题关系，模拟回执丢失后重启接续，只发送一次提问；实际业务来源仍由夹具提供。完整记录、源码散列、截图和清理见[原问题关系与提问详情验证](../../tasks/2026-10-24-shadow-activity-implementation.md#原问题关系与提问详情验证)。本人事实同步另有上方独立证据；完整原生多 Core 问答与准确顺流回传仍未完成。

默认分身选择和个人动态中的准确事件处理状态此前已在独立 `codex/shadow-activity` 通过全 Go/vet、相关 race、Web 248 项测试、双嵌入构建及中英文浏览器复核。实际选择直接生效，旧任务保留原分身；处理详情与原问题阅读保持 2 组未读和 readRevision=0。另修正自发分身消息误显示等待接收，详情按需读取且不沿用原通知的来源身份。桌面与实际 684 CSS 像素窄布局均已查看；命令、截图、初次夹具问题和边界见[默认选择与状态验证](../../tasks/2026-10-24-shadow-activity-implementation.md#默认分身与动态处理状态验证)。该阶段没有新增真实模型验证。

前阶段的问题级接回、本人回答读取、显式交回和成员等待页通过范围见[问题交接与等待页验证](../../tasks/2026-10-24-shadow-activity-implementation.md#问题级接回与成员等待页验证)。该阶段曾修复等待详情内层误标已读；其 241 项 Web 测试与浏览器记录保留为当时证据，当前结果以上述新阶段为准。


`codex/shadow-activity` 在 Meta `616ff79` 上实施默认接手与任务接续。独立消费者、直接 @/普通关注分流、准确依赖、异步来源读取、真实作者、行动去重及 checkpoint/wait/finish 已通过 Go 回放与相关 race；当次命令、结果及剩余项见[实施记录](../../tasks/2026-10-24-shadow-activity-implementation.md)。2026-10-24 的专用 Codex 0.163.0-alpha.11.2 / gpt-5.6-luna 测试还证明：旧主段交接及旧 W 保留任务后升级，两个新原生会话实际使用新增工具，等待任务核对完成条件后结束，再次重启复用新主段。答复来源由夹具控制，见[结构化原生证据](../../tasks/2026-10-24-shadow-activity-evidence/direct-and-toolsets/native-toolsets-report.json)。

同日首次关注与 shadow_watch 管理已通过完整 Go/vet、相关 race、源记录失败与重复回执恢复测试。专用 Luna 实际调用 start/update/pause/resume/complete，固定基线和暂停后排队更新各形成一轮认识；重启复用原主段，内部观察不进入共享聊天，见[本阶段证据](../../tasks/2026-10-24-shadow-activity-implementation.md#首次关注与目标管理验证)。业务来源由夹具控制，不扩大为两台 Mac 或完整跨 Core 原生产品流程。关联 W 的目标变更交接已取得专用 Luna 真实证据：准确中断旧回合、修改目标、重启恢复原 W、实际 replan 后按新条件 finish；具体范围及最终工程检查见[目标控制验证](../../tasks/2026-10-24-shadow-activity-implementation.md#关注目标关联任务的验证)。当时新增界面、简报写入、准确顺流回传和完整问答闭环尚未完成，后续问题交接与等待页以本页新阶段证据为准，既有验收不能替代未覆盖的新路径。

2026-10-24，实际提问登记与直接回合的后续工作核对已通过全部 Go/vet、相关 race、Core 构建及 Web 238 项回归。Luna 实际调用 ask，在模拟丢失回执后跨重启只读找回原问题，消费真实依赖信号并核对条件完成；只发送一次问题。根提问/回复提问的实际作者、命令身份和准确回复链另由跨 Core HTTP/TLS 测试覆盖，见[提问与接续证据](../../tasks/2026-10-24-shadow-activity-implementation.md#实际提问与接续验证)。该组合尚未验证完整原生多 Core 公开 @ 问答到准确顺流返回；当阶段问题级接回及成员等待界面尚未实现，后续进展见下方。

2026-10-22 的启动模型选择已通过工程回归、Edge 界面、真实 Codex/Luna 和同 Mac 三 Core 验证：先选 low，再把测试 home 默认从 medium 改成 high，S1/S2/W/CM/M 及重启均保留 low。具体请求与原生确认值、故障修正和限制见 [启动配置记录](../../tasks/2026-10-21-local-shadow-design/implementation.md#分身启动模型选择2026-10-22)。模型目录保留第三方配置的结构覆盖不代表第三方模型已调用验收。新包 `0.2.6-dev.shadow-model.20261022` 来源为干净提交 `7752b75`，DMG/App/CLI 和隔离 Homebrew Formula/Cask 全部通过；见 [安装包记录](../../tasks/2026-10-21-local-shadow-design/implementation.md#启动模型选择安装包)。仍为 ad-hoc 签名、未公证的本地产物。

本地安装包 `0.2.6-dev.local-shadow.20261022` 来源为干净合并提交 `8cae276`：DMG/CLI/App 完整性、真实 App 生命周期及隔离 Core 升级通过；附加 Homebrew 配方测试超时，未完成全部安装链。详见[Meta 安装包记录](../../tasks/2026-10-21-local-shadow-design/implementation.md#meta-安装包2026-10-22)。ad-hoc 签名、未公证、未公开发布。

2026-10-22 合入 `Meta` 时补齐分身草稿与插件更新、分身活动与 Core 升级保护的集成；合并后的 Go test/vet、相关 race、Web 204 项与两套构建通过，见[合并回归记录](../../tasks/2026-10-21-local-shadow-design/implementation.md#合入-meta-与集成回归2026-10-22)。本轮没有扩大真实模型、跨 Mac 或插件宿主界面的验收范围。

2026-10-21，`codex/meta-to-main-20261021` / `4d33a78` 加本任务实现：Core 状态、共享传输、MCP 和 Web 回归通过，实际浏览器复核聊天输入、记忆与运行状态。Codex CLI `0.163.0-alpha.11`、`gpt-5.6-luna` 的专用真实会话验证配置/MCP 继承、主会话与独立 W 同时执行、CM、原生压缩、S1 → S2 后晚到结果、记忆纠正/忘掉与准确 thread 恢复。单 Mac 三 Core / TLS 验证全体成员共享历史、持久送达、同 ID 去重、离线失败不自动重发、Core 重启后手动重发与 stdio MCP 读取。

完整命令、结构化结果、夹具修正和未覆盖项见[实现与验证记录](../../tasks/2026-10-21-local-shadow-design/implementation.md)。这是开发版 Codex 路径；没有验证 Claude、第三方模型、任意 hooks/插件组合、两台 Mac、跨 NAT/DERP、长期统计质量或 ChatGPT 插件宿主界面。默认分段阈值尚未经过质量与成本调优，测试降低的触发线不能当作生产最优值。实际行为及复验方法分别由[领域契约](../decisions/local-shadows.md)和[验证门槛](test-gates.md#本机数字分身)维护。

## 如何使用和更新

1. 先确定具体路径：Provider、目标主机、客户端、接收方式和网络环境。原生平台有某个接口、Team Cross 已有适配、实际宿主通过验收分别判断。
2. 沿本页链接读取对应报告的结果和未覆盖项；测试脚本存在、构建通过或协议替身成功不能代替真实客户端结果。“受阻”表示当次验收未完成，不据此断言实现错误或平台永久不支持。
3. 对照当前分支的相关实现与报告被测范围。后续改动影响该路径时，标明需要复验的部分；没有对应结果时，只报告最近已知证据，不称当前版本已通过。未作差异核对的旧报告保留为历史参考。
4. 新结果改变已知范围、暴露阻塞或替代旧证据时，在同一变更中更新对应条目及领域契约中的当前限制，再归档报告。保留仍有解释价值的失败和未覆盖项；不用维护第二份原始结果。执行命令、环境细节和日志仍在任务记录中。

## 配对与空间请求

范围来源：[接收会话配对](../decisions/agent-pairing.md)、[空间工作台](../decisions/space-workbench.md)。下表按实际被测操作记录范围；会话创建方式用于复现场景，不划分原生投递能力。

| 具体路径 | 最近已知结果 | 被测代码与环境 | 证据与适用边界 |
| --- | --- | --- | --- |
| 个人 Desktop 会话绑定与独立接收生命周期 | 审批同步、精确停止、安全释放与同 ID 接回通过；个人 Desktop 独立 STDIO 主动推送仍未接入 | 2026-10-05；Meta `910e07b` + `codex/receiver-handoff` 实现；单 Mac、Codex `0.160.0`、隔离 Luna 会话 | [交接回归](../../tasks/finished_archived/2026-10-05-receiver-handoff.md)、[早先调查](../../tasks/finished_archived/2026-10-05-receiver-lifecycle-validation.md#执行结果)。两个独立真实 app-server 验证持锁拒绝、续写历史接回及模型实际使用前轮标记；Desktop 打开仅核对系统参数，不证明个人 Desktop UI 完整往返、列表刷新或 `agents` 的 `x`。 |
| 共同执行中的 Codex 新 fork 配对、投递、读取和完成回执 | 真实 TUI 通过；重复请求未重放，分析未擅自回复原批注 | 2026-10-03；配对首轮为 `9abee02` + 当次实现，工作台回归为 `fed1e0a` + 当次实现；单 Mac，Codex `0.159.0-alpha.12.1`，`gpt-5.6-luna` | [首轮报告](../../tasks/finished_archived/agent-pairing-2026-10-03/README.md#真实-codex-tui)、[后续回归](../../tasks/finished_archived/space-workbench-2026-10-03/README.md#真实模型与原生客户端)。覆盖该次 fork 的配对链，未验证连接其他已有会话的投递入口或专用 Desktop。 |
| 工作台新建 Codex 会话、简报接手、经明确授权的会话间请求 | 真实模型协作链通过；三个成员看到相同完成记录；暂停/停用保留会话；没有共同执行 | 2026-10-03；`fed1e0a` + 工作台当次实现；单 Mac、三个隔离 Core、真实 TLS、两名成员的独立接收会话；同上 Codex/Luna | [报告](../../tasks/finished_archived/space-workbench-2026-10-03/README.md#真实模型与原生客户端)、[结构化结果](../../tasks/finished_archived/space-workbench-2026-10-03/evidence/native-space.json)。模型链中的启动目录没有材料，不能据此声称已验证所有真实材料组合；材料与界面另看对应证据。不是三台 Mac。 |
| 接收记录从历史创建失败状态恢复后的首次投递与 Core 状态 | 工程、race、CLI/MCP 与真实三 Core/Luna 请求链通过；零回合原生空会话重启恢复另受历史落盘限制 | 2026-10-04；`1297b31` 修复工作树；macOS 14.8.5 arm64、Codex 0.160.0 | [报告](../../tasks/finished_archived/2026-10-04-receiver-deadlock.md)。真实链核对 3 条完成请求、去重与恢复后的状态查询；单 Mac，不等价于跨机或任意 Desktop 宿主配对。零回合 `no rollout found` 未修复，不能推广通过范围。 |
| 个人 Claude Channel 主动接收 | 实验适配有协议检查；真实接收受宿主门槛阻塞，未通过 | 2026-10-03；上述配对及工作台工作树；Claude Code `2.1.270` 提示 Channels 不可用；首轮模型代理也拒绝连接 | [首次探测](../../tasks/finished_archived/agent-pairing-2026-10-03/README.md#真实-claude-channel-探测)、[后续探测](../../tasks/finished_archived/space-workbench-2026-10-03/README.md#真实模型与原生客户端)。未伪造挑战；普通 Claude MCP 调用或共享 TUI 通过不能替代 Channel 接收。 |
| 测试前已加载的 Codex 会话连接、投递与回执 | 真实专用会话通过；同一 thread 完成连接、读取及结果回报；关闭借用代理保留 daemon | 2026-10-04；`ae66ff6` + `codex/collaboration-flow` 改动；单 Mac、Codex `0.160.0`、`gpt-5.6-luna` | [本次报告](../../tasks/finished_archived/2026-10-04-collaboration-flow/README.md#原生与-lan)。只连接精确已加载 ID，不启用共同执行；不能推广为任意 Desktop 版本已验收。 |
| LAN 三 Core 空间请求回归 | 真实 TLS 成员加入、两名成员原生接收、授权分派和一致结果通过；未启用 Cloud 网关 | 2026-10-04；同上工作树与 Codex/Luna，单 Mac 三 Core | [报告](../../tasks/finished_archived/2026-10-04-collaboration-flow/README.md#原生与-lan)、[结构化结果](../../tasks/finished_archived/2026-10-04-collaboration-flow/evidence/lan.json)。证明本地 LAN 路径不依赖 Cloud；不等价于两台 Mac 网络验收。 |
| MCP Events 协议与隔离网关 | 签名验证/投递、筛选、去重、持久化、暂停/撤销/到期和权限检查通过；真实 loopback CLI 网关通过 | 2026-10-04；同上工作树；回调为受控 HTTP transport，网关为真实 Core/CLI | [报告](../../tasks/finished_archived/2026-10-04-collaboration-flow/README.md#事件与界面)、[网关结果](../../tasks/finished_archived/2026-10-04-collaboration-flow/evidence/gateway.json)。真实 ChatGPT Cloud 宿主验收由用户暂缓，未部署公网。 |
| 本地 STDIO MCP Events 宿主订阅 | 最小服务协议对照通过；普通工具与准确 thread 核对通过；三组真实 app-server 订阅均受 hosted apps 限制拒绝 | 2026-10-07；Meta `6e9c890` + 验证脚本；单 Mac，Codex `0.160.0`，两条有效 Luna 会话；apps 开启对照无模型轮次 | [报告](../../tasks/finished_archived/2026-10-07-local-mcp-events/README.md)、[结构化结果](../../tasks/finished_archived/2026-10-07-local-mcp-events/evidence.json)。未达到空闲原会话接收与处理前提；外层客户端为探针，不是个人 Desktop UI / Cloud 验收；没有修改产品接入。 |
| 工作台布局、快捷连接与简报编辑界面 | 添加会话的明确确认、可选名称、固定来源勾选、草稿保留和保存版本通过；中英、深浅主题及 1440/1024/768 宽度复核 | 2026-10-04；`ae66ff6` + 同分支后续 UI 改动；154 项 Web 测试，真实 Core / HTTP / stdio 和 production 资源 | [界面复核](../../tasks/finished_archived/2026-10-04-collaboration-flow/README.md#界面优化复核)。2026-10-21 空间详情进一步调整为单一左栏、铺满右侧的连续对话、折叠评论及底部统一引用请求框，此历史记录不覆盖新布局；当前复验要求见 [空间工作台门槛](test-gates.md#空间工作台)。浏览器宿主消息回执为模拟；本轮不新增真实模型、LAN 跨 Core、Desktop 或 Cloud 宿主证据。 |

2026-10-04 的新增记录覆盖专用已有会话接入，之前记录仍只适用于各自被测路径。当前连接条件及 `unsupported` 的触发条件见 [配对契约](../decisions/agent-pairing.md#当前接入范围)。

ChatGPT 云端事件已实现本地协议和隔离入口，实际宿主接收仍未验收，边界见 [空间事件契约](../decisions/space-events.md)；本机 STDIO 与 `ui/message` 结果不作为云端订阅验收。

## ChatGPT 本机插件

范围来源：[本机插件](../decisions/chatgpt-local-plugin.md)。协议加载、宿主界面与模型读取分别取证；会话投递的证据见上节，不依赖插件面板是否打开。

| 具体路径 | 最近已知结果 | 被测代码与环境 | 证据与适用边界 |
| --- | --- | --- | --- |
| 新 Core/界面与驻留旧 MCP 路由能力不一致 | 真实旧 stdio 复现路由拒绝；能力检查、草稿回退和重载提示通过，跨 134 秒稳定，新进程动态恢复；中英、深浅主题与窄窗口通过 | 2026-10-24；`c7612db` 上 `codex/plugin-bridge-compatibility` 改动；旧 helper `8bc8397`，macOS 14.8.5 arm64，Web 238 项 | [实现、截图、真实进程与边界](../../tasks/2026-10-24-plugin-bridge-compatibility/README.md)。真实旧 MCP/Core/生产面板、模拟宿主；没有操作用户的 Codex 主窗口，不能承诺退出宿主后的草稿持久化。 |
| 已打开旧面板的两分钟回退与宿主免重启热更新 | 修复前真实浏览器复现；修复后旧加载器跨 132 秒稳定，草稿与故障回退通过；实际 ChatGPT 面板跨 166 秒稳定，先插件再菜单栏 App 复用新版 Core | 2026-10-23；产品 `e99cd67`，本地开发版 `0.2.6-dev.plugin-hot-update.20261023`；ChatGPT `26.1016.61520` / `21421`，单 Mac arm64；Web 218 项 | [根因、实际顺序、截图与限制](../../tasks/2026-10-23-plugin-hot-update/README.md)。完整退出/更新/打开顺序由隔离真实 stdio 验证；实际宿主保留已打开面板，ChatGPT 及其后台 PID 未变；不推广到尚无 bundle 更新能力的更早面板。 |
| 侧栏与对话入口的 UI 资源版本错配 | 已捕获侧栏 resources/read -32602；修复前复现、修复后 ServeUI/真实 stdio、工程回归及 DMG 成品验证通过；安装后的宿主双入口验收待完成 | 2026-10-22；`1f88599` 已合入 Meta，成品 `0.2.6-dev.sidebar-recovery.20261022`；单 Mac，Web 207 项测试 | [现场、修复及限制](../../tasks/2026-10-21-in-flow-implementation.md#2026-10-22-侧栏入口的资源版本错配)。对话工具打开正常不能替代侧栏验收；首次迁移需宿主重新加载旧 MCP，Hook 保存时面板撤销的独立原因未证实。 |
| 已安装构建优先、10 秒有界收尾与插件草稿 | 真实三构建及旧策略首次迁移通过；并发截止不延长、拒绝新业务、保留请求且不重发、Core 自主检测；DMG 验证通过 | 2026-10-22；产品 `acc12e4`，成品 `849ba25`；单 Mac 隔离 Core/stdio，Web 201 项测试 | [实现与成品记录](../../tasks/2026-10-21-in-flow-implementation.md#2026-10-22-已安装构建优先与有界收尾)。10 秒为自然收尾等待上限，进程启动另计；没有新增真实模型中断或实际宿主完整更新验收。 |
| 新 App 与旧 Core 并存时的连接状态、标题与升级阻塞 | 真实宿主确认旧 Core 404；正常切换后恢复原生标题和准确关联状态，一次连接消息后画面正常 | 2026-10-22；现场磁盘 `8a5564d`、旧运行 Core `583a12a`；代码修复 `d835321`，单 Mac | [现场与修复记录](../../tasks/2026-10-21-in-flow-implementation.md#2026-10-22-实际宿主的旧-core-阻塞)。原白屏未捕获独立脚本根因，恢复后未再现；接收能力仍未接通，不能推广为完整宿主热更新验收。 |
| 插件界面自动更新与保留草稿刷新 | 全量工程回归通过；真实浏览器验证自动更新、评论/留言恢复、弹窗操作和新代码启动失败回退，业务写入为零 | 2026-10-22；`codex/in-flow-awareness` 本次产品差异；production 面板、真实 Core、禁止 iframe 联网的 CSP | [更新记录与截图](../../tasks/2026-10-21-in-flow-implementation.md#2026-10-22-插件更新体验)。宿主桥和版本切换为合成；未新增真实 Codex 宿主、Cloud 或跨 Mac 验收。旧面板首次获得检测能力仍需宿主重新加载。 |
| 插件旧 Core 升级与助手 CLI 状态恢复 | 真实旧/新二进制与隔离 stdio 升级通过；助手设置一致性、原 ID 明确重试和未知创建不重放通过 | 2026-10-04；`af9f715` 上 `codex/fix-plugin-core-upgrade` 产品差异；macOS 14.8.5 arm64、CLI `0.160.0` | [专项报告](../../tasks/finished_archived/2026-10-04-core-upgrade.md)。CLI 范围不要求模型或截图；旧 Core 首次迁移只能使用其原有停止保护，不代表已有桌面面板自动重载。 |
| 完整 WebGUI 的插件资源、原生加载、业务桥和工作台页面 | 原生 app-server 加载与真实 Core/stdio 通过；独立浏览器完成页面、批注、简报等操作 | 2026-10-03；`fed1e0a` + 插件改动，后续集成为 `4556c18` 合入 `7ee5a0a`；macOS 14.8.5 arm64，Codex `0.159.0-alpha.12.1` | [完整界面及集成报告](../../tasks/finished_archived/2026-10-03-meta-plugin-webgui/README.md)。浏览器宿主消息回执为模拟；最终成品的 global/thread 入口、实际摆放及“带回当前对话”投递仍不能由这些检查证明。旧桌面探针不替代该成品。 |
| Composer mentions 搜索、固定版本/批注资源读取 | stdio、原生 app-server 与桌面兼容参数通过 | 2026-10-03；初版 `9c1c378`，参数修复 `53d3472`；Codex `0.159.0-alpha.12.1`，桌面请求格式来自 ChatGPT `26.930.21537` / build `12776` | [初版](../../tasks/finished_archived/2026-10-03-composer-mentions.md)、[参数修复](../../tasks/finished_archived/2026-10-03-composer-mentions-empty-results.md#修复与验证)。必须包含 `{query,path:[]}`，不能只用 query-only 检查替代实际宿主请求。 |
| Composer mentions 在桌面输入框搜索、选择和发送显示 | 用户实际确认通过，并发送材料 v1 与原批注引用 | 2026-10-03；`53d3472` 修复后用户重载桌面；原生引用来自已安装 `teamcross-ui` | [用户最终显示验收](../../tasks/finished_archived/2026-10-03-composer-mentions-empty-results.md#用户最终显示验收)。覆盖显示与引用选择，未包含模型正文分析或会话配对验收。不能沿用初版“尚需桌面验收”覆盖此后续结果。 |
| 显式安装/接管、App 同步、断开与新版 bootstrap 回执 | 原生 CLI、真实 App 生命周期及浏览器通过；成品主窗口体验待验收 | 2026-10-03；`7089781`，包 `0.2.6-dev.plugin-onboarding.20261003`；隔离 profile，Codex `0.159.0-alpha.12.1` | [接入报告](../../tasks/finished_archived/2026-10-03-chatgpt-plugin-onboarding.md)。报告之后 `d1b3452` 修改当前 Core 的绑定与恢复处理；本次未找到该修复对应的独立执行记录，涉及换绑的结论需要补证或复验，不能由 `7089781` 的通过自动覆盖。测试存在不等于当次执行结果。 |
| 空间 mention、当前会话连接、来源进展与简报草稿 | Core/MCP 与 Web 回归通过；真实浏览器使用 production 资源完成连接消息、结果导航、成员确认简报及通知连接管理 | 2026-10-04；`ae66ff6` + `codex/collaboration-flow`，真实 Core/stdio，1440/1024/768 像素、中英与深浅主题 | [本次报告与截图](../../tasks/finished_archived/2026-10-04-collaboration-flow/README.md#事件与界面)。宿主 message/context 回执为模拟，新增空间 mention 的真实 Desktop 显示及绑定未另做宿主验收；不覆盖已有材料 mention 的用户确认结论。 |

## 原生执行与协作模式

范围来源：[原生客户端](../decisions/native-clients-and-models.md)、[协作模式](../decisions/runtime-modes.md)、[Claude 接入](../decisions/claude-native-tui.md)。以下是相应入口的历史证据，未在本次文档整理中逐项复验当前原生客户端。

| 具体路径 | 最近已知结果 | 被测代码与环境 | 证据与适用边界 |
| --- | --- | --- | --- |
| Codex 新版应用内 CLI 的发现、失效路径恢复及直接 TUI/MCP | 工程、隔离设置页与真实 TUI/MCP 通过 | 2026-10-04；`def4ab0`（验证时为同一产品代码的未提交差异）及验收脚本读取修正；macOS 14.8.5 arm64，CLI `0.160.0`，应用 `26.930.31730`，Luna，两个同机 Core | [专项报告](../../tasks/finished_archived/2026-10-04-codex-discovery.md)。覆盖保存值/检测值分离、同应用重新定位、输入交接和同 fork 恢复；未重验新版专用 Desktop 全入口、文件工具执行或两台 Mac。首次脚本的摘要读取断言失败与按 ID 修正后的结果分别记录。 |
| Codex 原目录/worktree 的直接与辅助 TUI/Desktop | 八种组合有真实操作记录，包含执行与原生审批 | 2026-09-09；`codex/session-collaboration` 当次实现；单 Mac，CLI `0.153.1`、Desktop `26.901.31953`、Luna | [原生协作报告](../../tasks/finished_archived/native-collaboration-2026-09-09.md)。八组合未单列精确被测 SHA；`30a4ebb` 是之后的工程与恢复补充，不能当作全部 Desktop 重验。成员/邀请规则随后变化，旧报告不作为当前规则或全入口通过证明。 |
| Codex 与 Claude 的受限/信任模式、工具继承、批注与同 ID 恢复 | 四轮真实原生 TUI 检查通过 | 2026-09-16；`d3004d2` + 当次实现；macOS 14.8.5、Codex `0.154.0-alpha.6.2`、Claude `2.1.270`、Luna | [信任模式报告](../../tasks/finished_archived/trusted-runtime-2026-09-16.md)。覆盖专用 MCP/hook；不代表全部插件、电脑控制、Desktop 全局能力、OAuth/Keychain 或两台 Mac 已验收。 |
| Claude fork 进入个人 CLI/TUI 历史、同 worker 输入与恢复 | 真实执行及 `/resume` picker 通过 | 2026-09-14；`dcda482` + v0.1.4 当次实现；单 Mac、两个 Core，Claude `2.1.270`、Luna | [个人历史报告](../../tasks/finished_archived/claude-personal-history-2026-09-14.md)。不覆盖个人 Channel、跨磁盘 fallback、Claude Desktop/Web/Cloud；受限历史路径与后来信任模式按各自契约区分。 |
| 个人 Claude TUI 通过 MCP 辅助共享执行 | 原生配置加载、读取、授权后发送与撤销检查通过 | 2026-09-12；报告所列功能分支，未固定精确被测 SHA；两个同机 Core，Claude `2.1.268`、Luna | [辅助 MCP 报告](../../tasks/finished_archived/claude-assist-2026-09-12.md)。目标是共享 Claude worker；不证明个人会话接收 Channel。报告中“共享 worker 尚无批注 MCP”等当时限制不能覆盖后续实现。 |

## 空间材料与读取

2026-10-23，`codex/reading-experience` 基于 `8bc8397` 的未提交阅读器实现：六项展示、图片随版本保存、全文搜索和当前轮询下的阅读跟随已通过工程、隔离 Core/stdio 与真实浏览器检查；Web 229 项、翻译 1200 条。后续界面已移除正文搜索入口与自定义快捷键，报告中的搜索界面结果仅适用于当时实现；Core 检索接口保留。见[实现与验证记录](../../tasks/finished_archived/2026-10-23-reading-experience/README.md)。原生历史和 iframe 宿主为合成，未重验实际 Desktop、Cloud、Claude 图片格式或跨 Mac；实时同步链路仍另行实施。

范围来源：[协作空间](../decisions/collaboration-spaces.md)、[资源库](../decisions/resource-library.md)、[读取协议](../protocol.md#agent-按需读取视图)。下列合成历史与模拟运行时检查不证明真实 Provider 的全部导出格式。

| 具体路径 | 最近已知结果 | 被测代码与环境 | 证据与适用边界 |
| --- | --- | --- | --- |
| 同一链接多人加入、原空间启用执行、逐成员授权与关闭 | 自动化及真实浏览器通过 | 2026-09-19；`7a7f7b3` + 当次改动；单 Mac、三个 Core、TLS；原生运行时为替身 | [邀请与关闭报告](../../tasks/finished_archived/space-invitations-2026-09-19.md)。取代早期“一邀请一成员”的实现范围；不等于三台 Mac 或真实三方原生执行验收。 |
| `schema:3` 材料存储、上传、按条读取与浏览器展开 | 工程与真实浏览器闭环通过 | 2026-09-20；`codex/release-v0.2.0-rc.1` 中与报告同提交的实现；单 Mac、三个 Core、合成历史 | [内容寻址材料报告](../../tasks/finished_archived/content-addressed-materials-2026-09-20.md)。9 月 18 日 `schema:2` 材料记录仅供追溯；读取默认视图此后又调整，不能把当时参数与工具清单当作当前规范。 |
| Agent 按需视图、精确批注、UTF-16 分页与封装预算 | 自动化及真实 HTTP/MCP 检查通过 | 2026-09-27；`b9ebd73` + 当次改动；模拟原生运行时，未启动模型 | [按需读取报告](../../tasks/finished_archived/agent-context-reading-2026-09-27.md)。载荷数值属于固定 fixture，不是实际模型质量、token 收益或 Provider 读取成本基准。 |
| 资源库选择/固定读取入口与菜单栏速览页面 | 真实浏览器/stdio 与 App 生命周期有通过记录；速览原生桌面交互未完成验收 | 2026-09-22：`f88cb79` + 当次改动；2026-09-24：`a70c002` + 当次改动；macOS 14.8.5，合成材料 | [资源库](../../tasks/finished_archived/resource-library-2026-09-22.md)、[速览导航](../../tasks/finished_archived/quicklook-navigation-2026-09-24.md)。原生弹窗、热键、菜单和固定浮窗不能由浏览器消息替身或 App 编译推断；旧共享发送入口由后续统一配对契约替代。 |

## 网络与分发

范围来源：[输入与共享](../decisions/input-and-sharing.md)、[安装与首次体验](../distribution-and-onboarding.md)、[构建与发布](../releasing.md)。网络路径、打包、公开发布和桌面交互分别取证。

2026-10-22，本地顺流感知包 `0.2.6-dev.inflow.20261022`，干净提交 `cf8fc49`：DMG/CLI 校验、临时安装、图标与签名完整性、双 App 副本及隔离 Core 生命周期通过。arm64，ad-hoc 签名、未公证；未运行可选的真实插件启动同步、Homebrew 或公开安装检查。见[本地 DMG 记录](../../tasks/2026-10-21-in-flow-implementation.md#2026-10-22-本地-dmg)。

| 具体路径 | 最近已知结果 | 被测代码与环境 | 证据与适用边界 |
| --- | --- | --- | --- |
| Tailcat 成员与协作数据面 | 同机实网通过，后续覆盖同链接第三位成员与交接 | 2026-09-15：`42adb81` + 当次改动；2026-09-19：`7a7f7b3` + 当次改动；macOS 14.8.5，Tailcat `v0.6.0`，模拟原生运行时 | [传输报告](../../tasks/finished_archived/tailcat-transport-2026-09-15.md)、[多人补充](../../tasks/finished_archived/space-invitations-2026-09-19.md)。不证明两台物理 Mac、跨 NAT、切网恢复或持续强制 DERP；本页尚无这些路径的通过证据入口。 |
| 接收恢复修复 DMG、App 重复打开与 Core 复用 | 最终包的挂载/校验/图标/签名完整性、真实双 App 副本、重复打开和隔离 Core 生命周期通过 | 2026-10-05；`1297b31`，`0.2.6-dev.receiver-recovery.20261004`；macOS 14.8.5 arm64 | [产物与 App 记录](../../tasks/finished_archived/2026-10-04-receiver-deadlock.md#最终-dmg)。本地包，ad-hoc 签名、未公证；邀请由测试 helper 截获，未运行可选 App 插件同步分支，未替换用户安装或重启现场 Core。 |
| 正式 tag 的构建、公开资产复核、Homebrew 发布 | v0.2.4 当次完整链路通过 | 2026-09-23；`f671cb4` / `v0.2.4`；托管 `macos-15` arm64 runner，公开资产及 tap 检查 | [发布报告](../../tasks/finished_archived/release-v0.2.4-2026-09-23.md)。只说明该版本当时发布与复核结果，不宣称它仍是最新版本或 Meta 新功能已发布；ad-hoc 签名不等于 Developer ID/公证，也未增加真实模型或跨机验收。 |

本页只链接已定向核对的报告。需要回答未列出的路径时，按[检索规则](../../README.md#检索边界)继续查相应任务或提交/PR 中的执行说明；找到新证据后补充适用范围，不能因未命中本页就给出否定结论。

## 成员动态

2026-10-23，`codex/reading-experience` / `8bc8397` 加本任务工作树：结构化 @、回复到回复、三类成员关注、全局和空间概览内动态、主机已读与离线补交已实现。验证环境为单 Mac 三 Core 的真实 HTTP/TLS 与合成内容，以及专用 Codex `0.163.0-alpha.11.2` / `gpt-5.6-luna` 原目录和 worktree 两条真实最终答复链。当前行为见[契约](../decisions/member-activity.md)，工程、浏览器截图、原生检查及失败边界见[实现记录](../../tasks/2026-10-23-member-activity-implementation.md)。

真实原生结果只证明 app-server 轮次归属与已读，不扩大到 TUI/Desktop 交互、实验性 Claude、真实 ChatGPT 插件宿主、两台 Mac 或 Tailcat 网络。通用 `TestLiveCodex` 的写文件场景在本次环境停于沙箱审批，未作为通过项；专门的无工具成员动态回合通过。顺流、Cloud 与分身消费者进度仍独立。
