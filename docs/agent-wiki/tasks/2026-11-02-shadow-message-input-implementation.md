# 分身消息流与 Work 收敛实施

- 日期：2026-11-02；分支：`codex/shadow-activity`。
- 被测状态：`40b606a67f5d650456cf9b6c82750a1cc5866017` 基线，加工作树已有修改和本轮未提交实现；不是干净基线的验收结果。
- 用户授权：按[消息流收敛方案](2026-10-28-shadow-message-input-design.md)实施，优先跑通多人连续输入不串来源、Work 执行时主会话可聊天，再推进普通补问和 Work 回报。
- 本轮结果：两个优先场景已接通，普通消息退出任务建模，Work 日常接口已收敛到 start/report；完成工程及真实 Luna 验证。实现已在 `bd6f7fb` 保存；后续 Meta 合并和安装另留验收记录。
- 当前契约：[本机数字分身](../sources/decisions/local-shadows.md)、[协议](../sources/protocol.md#本机数字分身)；长期证据保存于[精简验收 JSON](2026-11-02-shadow-message-input-evidence.json)，包含源码及原始报告散列、断言结果和单次时延样本。

## 实际实现

| 范围 | 本轮行为与实现 |
| --- | --- |
| 多人连续输入 | [input.go](../../../internal/shadow/input.go) 保存每条作者、来源和回复关系。空闲 S 有界合批 start，活动 S 以准确 expectedTurnId steer 增量；不并行创建第二个主轮 |
| 准确归属 | 多人同轮的记忆、任务及协作写入选择 inputId；一个讨论包内多条动态使用各自 origins[].id，拒绝默认归给第一条；同讨论后来新到的直接输入仍能进入当前 S |
| Work 期间聊天 | [tool_dispatch.go](../../../internal/shadow/tool_dispatch.go) 按原生 thread 排队工具调用。慢 W 工具不占调度循环，S 可继续收发；运行时更换后不向新连接回答旧请求 |
| 普通补问 | shadow_reply 或原讨论回复工具直接保存/发送，未建 coordination、awaitReply 或普通条件等待；直接输入不要求 final 认识 JSON，也不把一轮结束当成每条问题都已回答 |
| Work 回报 | [task_reports.go](../../../internal/shadow/task_reports.go) 将 report 的 continue/wait/complete/fail 映射到既有生命周期。Core 绑定当前任务与版本，模型只提交进展、真实依赖或完成依据 |
| 直接送达 | 共享聊天走 ReplyOutbox，讨论复用持久行动与稳定 requestId；Work 的阶段与结果不经过必经的 S 改写轮。已有讨论回复可关联 publishedActionId，不再发一份 |
| 完成与恢复 | 完成条件、真实依赖、版本和本人接回继续校验；understand/contextReviews 不再阻塞完成。固定结果和发送意图同次落盘，发送失败不撤销已完成结果；重试执行轮有独立报告身份，同文字的新结果不会被旧报告吞掉 |

普通输入最多 16 包/96 KiB 一批、128 包一轮。长正文为明确的部分预览，原文仍可按准确来源读取，shadow_history 支持长消息分页。压缩、换段、工具升级或审批期间保留待投递输入；没有不断重置的合批计时器。

start/steer 发送前持久保存意图；明确拒绝过期 turn 时先重读状态，再决定下一步。传输结果未知时按原生 userMessage 的批次标记核对，没有证据便不自动重发。输入受理、公开回复、业务完成和通知送达分别保存。[Codex active-turn steer 协议](https://learn.chatgpt.com/docs/app-server#steer-an-active-turn)只保证追加受理，不保证即时回答。

## 验收场景与证据边界

1. **多人连续输入**：隔离原生 fixture 先让 A/B 合批，C 在同一活动 turn 的慢工具期间加入；实际 Luna 分别回复 A/B/C，个人记忆正确归属，普通聊天创建零 Work。同 Mac 三个真实 Core 另以成员凭据并发发言，三包进入同一原生 turn、三方读取相同真实作者与回复目标。
2. **Work 期间主会话聊天**：明确 Work 在独立 W 报告阶段进展，然后进入本地门控慢工具；新问题由 S 回答后才释放该工具。此时 Work 仍为 running，之后提交实际证据和完成核对，结果直接回原消息；没有 S 的 result 改写轮。
3. **恢复与边界**：三个 Core 的真实验证还覆盖离线不自动补发、手动同 ID 重试、原 thread 和记忆恢复、创建设置不被后来默认配置覆盖、真实 stdio MCP 读取。回放覆盖同讨论多作者、D1/C1 混合来源、磁盘失败、结束竞争、未知 steer 回执、讨论报告丢失回执、已发回复复用、旧版本/取消保护及同目标重试后相同文字的独立结果。

模型固定 `gpt-5.6-luna`，推理强度 low；Codex CLI `0.166.0-alpha.5.1`，macOS arm64，Go `1.27.1`，Web 使用 Node `24.18.0`。原生测试为全新 home/repo 与专用会话；门控工具和提示词用于确定性覆盖调度与来源机制，不代表任意长对话的质量评估。

当前真实模型证据覆盖共享聊天 C1 与独立 W；D1 的同讨论多作者和混合路由由回放覆盖。未新增双 Mac、浏览器或 ChatGPT 插件宿主验收，未跑完整真实模型的“默认 @ → Work → 原个人会话”闭环。没有前端源码变更，本轮用 Web 回归覆盖既有界面；两套嵌入资源重建后无差异。

## 执行检查

| 检查 | 结果 |
| --- | --- |
| go test ./... | 全部通过 |
| go vet ./... | 通过 |
| go test -race ./internal/shadow ./internal/collab ./internal/mcp ./internal/nativecodex | 四包通过 |
| Web check/test/build | 1381 条翻译检查；29 个测试文件、259 项测试通过；两套嵌入构建通过 |
| verify-shadow-native.py --message-inputs | 真实批量输入、steer、记忆归属、Work 期间聊天及直接报告通过 |
| verify-local-shadow.py --message-inputs | 同 Mac 三 Core TLS 成员输入、回报、恢复和 stdio MCP 通过 |
| 文件检查 | Python AST、gofmt、相对链接、索引入口及 git diff --check |

运行时使用任务专用 GOCACHE，没有修改全局 Node、个人 Codex 配置或个人会话。复验命令如下，fixture-dir 每次必须使用全新路径：

```sh
python3 scripts/verify-shadow-native.py --message-inputs \
  --fixture-dir /private/tmp/teamcross-shadow-native-fresh \
  --codex-bin /absolute/path/to/codex \
  --codex-auth /absolute/path/to/auth.json

python3 scripts/verify-local-shadow.py --message-inputs \
  --fixture-dir /private/tmp/teamcross-shadow-core-fresh \
  --teamcross-bin /absolute/path/to/teamcross \
  --codex-bin /absolute/path/to/codex \
  --codex-auth /absolute/path/to/auth.json
```

完整本地日志位于 `/private/tmp/teamcross-shadow-input-20261102/`；最终原始报告目录和散列由精简证据记录。时延样本包含人为等待门控，只能说明本次调用/回复发生的时间，不能据此宣称性能提升或固定秒数 SLA。中途修正过持久化测试夹具的初始状态、同讨论合包归属及重试报告身份；最终通过结果取收尾复验，不把早期通过扩大到后来改动。

测试结束关闭本次创建的 Core 与其原生进程，核对准确 PID/父子关系；没有按程序名终止用户客户端，也未创建浏览器页面。原始日志保留本地，精简证据不保存邀请 secret、认证内容或真实成员机器名。

## 保留与延后

保留原有跨 Core 权限、作者/来源、去重与 Outbox、显式 Work 的依赖和恢复、S 换段及记忆维护。旧 Questions/Contexts/Understandings 和相关内部回放继续保留，默认工具 schema 已不再列出 ask/questions/understand、relatedFocuses/contextReviews；本轮没有为了删代码而重写可靠性底座。

延后侧边问答、15 秒群发言轮转、普通观察流水线的重做、复杂 watch、多认识联动、多级派活和在途原生回合自动搬迁。依赖旧 schema 的高级原生验收脚本属于历史专用入口，需改写后才能作为当前默认接口的证据。现有受控回放通过不等于这些完整产品路径都已重新验收。

开始时已有 12 个未提交文件，先保留差异与散列；其中 9 个文件字节未改，另 3 个在原修改上扩展。具体名单与源码散列见精简证据，备份留在 `/private/tmp/teamcross-shadow-input-20261102/preexisting/`。没有覆盖主工作区或提交夹具、日志、认证及构建产物。
