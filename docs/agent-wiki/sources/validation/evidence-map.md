# 验证范围与证据入口

本页维护会影响当前能力判断的证据摘要与入口。产品规则和实现范围由各领域契约维护，验证方法由 [test-gates](test-gates.md) 维护，完整执行结果保留在任务记录；本页不另建产品支持清单，也不累积全部测试日志。

本次整理核对点：2026-10-04，`Meta` / `2acdab3`。这是文档与实现范围的核对点，**不是下列报告的被测提交，也没有在本次整理中重跑验收**。各行保留报告自己的日期、版本和环境；“基线 + 当次改动”不能改写成基线提交已通过。表中没有覆盖的路径表示尚未在此页建立证据入口，不等于未实现、未测试或不支持。

## 如何使用和更新

1. 先确定具体路径：Provider、目标主机、客户端、接收方式和网络环境。原生平台有某个接口、Team Cross 已有适配、实际宿主通过验收分别判断。
2. 沿本页链接读取对应报告的结果和未覆盖项；测试脚本存在、构建通过或协议替身成功不能代替真实客户端结果。“受阻”表示当次验收未完成，不据此断言实现错误或平台永久不支持。
3. 对照当前分支的相关实现与报告被测范围。后续改动影响该路径时，标明需要复验的部分；没有对应结果时，只报告最近已知证据，不称当前版本已通过。未作差异核对的旧报告保留为历史参考。
4. 新结果改变已知范围、暴露阻塞或替代旧证据时，在同一变更中更新对应条目及领域契约中的当前限制，再归档报告。保留仍有解释价值的失败和未覆盖项；不用维护第二份原始结果。执行命令、环境细节和日志仍在任务记录中。

## 配对与空间请求

范围来源：[接收会话配对](../decisions/agent-pairing.md)、[空间工作台](../decisions/space-workbench.md)。下表按实际被测操作记录范围；会话创建方式用于复现场景，不划分原生投递能力。

| 具体路径 | 最近已知结果 | 被测代码与环境 | 证据与适用边界 |
| --- | --- | --- | --- |
| 共同执行中的 Codex 新 fork 配对、投递、读取和完成回执 | 真实 TUI 通过；重复请求未重放，分析未擅自回复原批注 | 2026-10-03；配对首轮为 `9abee02` + 当次实现，工作台回归为 `fed1e0a` + 当次实现；单 Mac，Codex `0.159.0-alpha.12.1`，`gpt-5.6-luna` | [首轮报告](../../tasks/finished_archived/agent-pairing-2026-10-03/README.md#真实-codex-tui)、[后续回归](../../tasks/finished_archived/space-workbench-2026-10-03/README.md#真实模型与原生客户端)。覆盖该次 fork 的配对链，未验证连接其他已有会话的投递入口或专用 Desktop。 |
| 工作台新建 Codex 会话、简报接手、经明确授权的会话间请求 | 真实模型协作链通过；三个成员看到相同完成记录；暂停/停用保留会话；没有共同执行 | 2026-10-03；`fed1e0a` + 工作台当次实现；单 Mac、三个隔离 Core、真实 TLS、两名成员的独立接收会话；同上 Codex/Luna | [报告](../../tasks/finished_archived/space-workbench-2026-10-03/README.md#真实模型与原生客户端)、[结构化结果](../../tasks/finished_archived/space-workbench-2026-10-03/evidence/native-space.json)。模型链中的启动目录没有材料，不能据此声称已验证所有真实材料组合；材料与界面另看对应证据。不是三台 Mac。 |
| 个人 Claude Channel 主动接收 | 实验适配有协议检查；真实接收受宿主门槛阻塞，未通过 | 2026-10-03；上述配对及工作台工作树；Claude Code `2.1.270` 提示 Channels 不可用；首轮模型代理也拒绝连接 | [首次探测](../../tasks/finished_archived/agent-pairing-2026-10-03/README.md#真实-claude-channel-探测)、[后续探测](../../tasks/finished_archived/space-workbench-2026-10-03/README.md#真实模型与原生客户端)。未伪造挑战；普通 Claude MCP 调用或共享 TUI 通过不能替代 Channel 接收。 |

上述记录未覆盖将测试前已存在的 Codex 会话接入投递连接的流程。当前连接查找限制及 `unsupported` 的触发条件见 [配对契约](../decisions/agent-pairing.md#当前接入范围)；未覆盖该流程不表示已有会话缺少原生接收能力。

ChatGPT 云端 MCP Events/webhook 尚未接入，边界见 [契约](../decisions/agent-pairing.md#mcp-events-的边界)；本机 STDIO 与 `ui/message` 结果不作为云端订阅验收。

## ChatGPT 本机插件

范围来源：[本机插件](../decisions/chatgpt-local-plugin.md)。协议加载、宿主界面与模型读取分别取证；会话投递的证据见上节，不依赖插件面板是否打开。

| 具体路径 | 最近已知结果 | 被测代码与环境 | 证据与适用边界 |
| --- | --- | --- | --- |
| 完整 WebGUI 的插件资源、原生加载、业务桥和工作台页面 | 原生 app-server 加载与真实 Core/stdio 通过；独立浏览器完成页面、批注、简报等操作 | 2026-10-03；`fed1e0a` + 插件改动，后续集成为 `4556c18` 合入 `7ee5a0a`；macOS 14.8.5 arm64，Codex `0.159.0-alpha.12.1` | [完整界面及集成报告](../../tasks/finished_archived/2026-10-03-meta-plugin-webgui/README.md)。浏览器宿主消息回执为模拟；最终成品的 global/thread 入口、实际摆放及“带回当前对话”投递仍不能由这些检查证明。旧桌面探针不替代该成品。 |
| Composer mentions 搜索、固定版本/批注资源读取 | stdio、原生 app-server 与桌面兼容参数通过 | 2026-10-03；初版 `9c1c378`，参数修复 `53d3472`；Codex `0.159.0-alpha.12.1`，桌面请求格式来自 ChatGPT `26.930.21537` / build `12776` | [初版](../../tasks/finished_archived/2026-10-03-composer-mentions.md)、[参数修复](../../tasks/finished_archived/2026-10-03-composer-mentions-empty-results.md#修复与验证)。必须包含 `{query,path:[]}`，不能只用 query-only 检查替代实际宿主请求。 |
| Composer mentions 在桌面输入框搜索、选择和发送显示 | 用户实际确认通过，并发送材料 v1 与原批注引用 | 2026-10-03；`53d3472` 修复后用户重载桌面；原生引用来自已安装 `teamcross-ui` | [用户最终显示验收](../../tasks/finished_archived/2026-10-03-composer-mentions-empty-results.md#用户最终显示验收)。覆盖显示与引用选择，未包含模型正文分析或会话配对验收。不能沿用初版“尚需桌面验收”覆盖此后续结果。 |
| 显式安装/接管、App 同步、断开与新版 bootstrap 回执 | 原生 CLI、真实 App 生命周期及浏览器通过；成品主窗口体验待验收 | 2026-10-03；`7089781`，包 `0.2.6-dev.plugin-onboarding.20261003`；隔离 profile，Codex `0.159.0-alpha.12.1` | [接入报告](../../tasks/finished_archived/2026-10-03-chatgpt-plugin-onboarding.md)。报告之后 `d1b3452` 修改当前 Core 的绑定与恢复处理；本次未找到该修复对应的独立执行记录，涉及换绑的结论需要补证或复验，不能由 `7089781` 的通过自动覆盖。测试存在不等于当次执行结果。 |

## 原生执行与协作模式

范围来源：[原生客户端](../decisions/native-clients-and-models.md)、[协作模式](../decisions/runtime-modes.md)、[Claude 接入](../decisions/claude-native-tui.md)。以下是相应入口的历史证据，未在本次文档整理中逐项复验当前原生客户端。

| 具体路径 | 最近已知结果 | 被测代码与环境 | 证据与适用边界 |
| --- | --- | --- | --- |
| Codex 原目录/worktree 的直接与辅助 TUI/Desktop | 八种组合有真实操作记录，包含执行与原生审批 | 2026-09-09；`codex/session-collaboration` 当次实现；单 Mac，CLI `0.153.1`、Desktop `26.901.31953`、Luna | [原生协作报告](../../tasks/finished_archived/native-collaboration-2026-09-09.md)。八组合未单列精确被测 SHA；`30a4ebb` 是之后的工程与恢复补充，不能当作全部 Desktop 重验。成员/邀请规则随后变化，旧报告不作为当前规则或全入口通过证明。 |
| Codex 与 Claude 的受限/信任模式、工具继承、批注与同 ID 恢复 | 四轮真实原生 TUI 检查通过 | 2026-09-16；`d3004d2` + 当次实现；macOS 14.8.5、Codex `0.154.0-alpha.6.2`、Claude `2.1.270`、Luna | [信任模式报告](../../tasks/finished_archived/trusted-runtime-2026-09-16.md)。覆盖专用 MCP/hook；不代表全部插件、电脑控制、Desktop 全局能力、OAuth/Keychain 或两台 Mac 已验收。 |
| Claude fork 进入个人 CLI/TUI 历史、同 worker 输入与恢复 | 真实执行及 `/resume` picker 通过 | 2026-09-14；`dcda482` + v0.1.4 当次实现；单 Mac、两个 Core，Claude `2.1.270`、Luna | [个人历史报告](../../tasks/finished_archived/claude-personal-history-2026-09-14.md)。不覆盖个人 Channel、跨磁盘 fallback、Claude Desktop/Web/Cloud；受限历史路径与后来信任模式按各自契约区分。 |
| 个人 Claude TUI 通过 MCP 辅助共享执行 | 原生配置加载、读取、授权后发送与撤销检查通过 | 2026-09-12；报告所列功能分支，未固定精确被测 SHA；两个同机 Core，Claude `2.1.268`、Luna | [辅助 MCP 报告](../../tasks/finished_archived/claude-assist-2026-09-12.md)。目标是共享 Claude worker；不证明个人会话接收 Channel。报告中“共享 worker 尚无批注 MCP”等当时限制不能覆盖后续实现。 |

## 空间材料与读取

范围来源：[协作空间](../decisions/collaboration-spaces.md)、[资源库](../decisions/resource-library.md)、[读取协议](../protocol.md#agent-按需读取视图)。下列合成历史与模拟运行时检查不证明真实 Provider 的全部导出格式。

| 具体路径 | 最近已知结果 | 被测代码与环境 | 证据与适用边界 |
| --- | --- | --- | --- |
| 同一链接多人加入、原空间启用执行、逐成员授权与关闭 | 自动化及真实浏览器通过 | 2026-09-19；`7a7f7b3` + 当次改动；单 Mac、三个 Core、TLS；原生运行时为替身 | [邀请与关闭报告](../../tasks/finished_archived/space-invitations-2026-09-19.md)。取代早期“一邀请一成员”的实现范围；不等于三台 Mac 或真实三方原生执行验收。 |
| `schema:3` 材料存储、上传、按条读取与浏览器展开 | 工程与真实浏览器闭环通过 | 2026-09-20；`codex/release-v0.2.0-rc.1` 中与报告同提交的实现；单 Mac、三个 Core、合成历史 | [内容寻址材料报告](../../tasks/finished_archived/content-addressed-materials-2026-09-20.md)。9 月 18 日 `schema:2` 材料记录仅供追溯；读取默认视图此后又调整，不能把当时参数与工具清单当作当前规范。 |
| Agent 按需视图、精确批注、UTF-16 分页与封装预算 | 自动化及真实 HTTP/MCP 检查通过 | 2026-09-27；`b9ebd73` + 当次改动；模拟原生运行时，未启动模型 | [按需读取报告](../../tasks/finished_archived/agent-context-reading-2026-09-27.md)。载荷数值属于固定 fixture，不是实际模型质量、token 收益或 Provider 读取成本基准。 |
| 资源库选择/固定读取入口与菜单栏速览页面 | 真实浏览器/stdio 与 App 生命周期有通过记录；速览原生桌面交互未完成验收 | 2026-09-22：`f88cb79` + 当次改动；2026-09-24：`a70c002` + 当次改动；macOS 14.8.5，合成材料 | [资源库](../../tasks/finished_archived/resource-library-2026-09-22.md)、[速览导航](../../tasks/finished_archived/quicklook-navigation-2026-09-24.md)。原生弹窗、热键、菜单和固定浮窗不能由浏览器消息替身或 App 编译推断；旧共享发送入口由后续统一配对契约替代。 |

## 网络与分发

范围来源：[输入与共享](../decisions/input-and-sharing.md)、[安装与首次体验](../distribution-and-onboarding.md)、[构建与发布](../releasing.md)。网络路径、打包、公开发布和桌面交互分别取证。

| 具体路径 | 最近已知结果 | 被测代码与环境 | 证据与适用边界 |
| --- | --- | --- | --- |
| Tailcat 成员与协作数据面 | 同机实网通过，后续覆盖同链接第三位成员与交接 | 2026-09-15：`42adb81` + 当次改动；2026-09-19：`7a7f7b3` + 当次改动；macOS 14.8.5，Tailcat `v0.6.0`，模拟原生运行时 | [传输报告](../../tasks/finished_archived/tailcat-transport-2026-09-15.md)、[多人补充](../../tasks/finished_archived/space-invitations-2026-09-19.md)。不证明两台物理 Mac、跨 NAT、切网恢复或持续强制 DERP；本页尚无这些路径的通过证据入口。 |
| 正式 tag 的构建、公开资产复核、Homebrew 发布 | v0.2.4 当次完整链路通过 | 2026-09-23；`f671cb4` / `v0.2.4`；托管 `macos-15` arm64 runner，公开资产及 tap 检查 | [发布报告](../../tasks/finished_archived/release-v0.2.4-2026-09-23.md)。只说明该版本当时发布与复核结果，不宣称它仍是最新版本或 Meta 新功能已发布；ad-hoc 签名不等于 Developer ID/公证，也未增加真实模型或跨机验收。 |

本页只链接已定向核对的报告。需要回答未列出的路径时，按[检索规则](../../README.md#检索边界)继续查相应任务或提交/PR 中的执行说明；找到新证据后补充适用范围，不能因未命中本页就给出否定结论。
