# 本机数字分身实现与验证

2026-10-21，`codex/meta-to-main-20261021` / `4d33a78` 加本任务改动。目标是跑通本机 Codex 的共享聊天、分段、记忆与后台任务接续。当前行为由[领域契约](../../sources/decisions/local-shadows.md)维护，复验方法见[验证门槛](../../sources/validation/test-gates.md#本机数字分身)，原设计及独立 HTML 演示保留在[任务入口](README.md)。

## 实现结果

- 空间成员可为本机 Codex 创建分身，指定名称与工作目录；全空间共享同一持续对话，消息保留真实成员作者。不会导入个人旧会话 P0。
- WebGUI 有共享聊天、持久送达回执、失败手动重发、共享记忆/后台任务、原生上下文信息以及仅本机所有者可用的暂停/恢复/核对/审批入口。MCP 提供 `list_space_shadows`、`read_space_shadow`、`send_space_shadow`。
- C1 保存聊天和当前段指针；首条消息创建真正负责回答的 S1。话题变化不新建主 Session；CM 按原生用量提出压缩/换段建议，压缩仍由 Codex 原生完成。S1 写交接，S2 接手确认后才更新指针；旧建议与压缩前用量失效。
- 显式记忆保存、纠正与忘掉通过动态工具持久化，个人归属、来源和 revision 可核对；空闲 M 只产生候选，过时结果不能覆盖显式变更。
- J 属于 C1，W 从创建时就是独立原生 thread。主会话可继续回复，W 跨 S1 → S2 保持同 ID，晚到结果带版本进入当前段。取消/变更先核对原生轮次，不把旧版本当成当前完成。
- Core 保存创建/投递意图与准确原生 ID。结果未知时停止自动投递，按原生历史核对；不会因为回执丢失再运行一次。空间结束、成员退出停止对应分身进程，暂停则保留已接收工作。

实现入口：[状态与原生接入](../../../../internal/shadow/)、[空间桥](../../../../internal/collab/shadows.go)、[界面](../../../../packages/web/src/components/SpaceShadows.tsx)。使用原生有效 home 与权限，继承配置而不改个人配置；没有额外只读隔离或全插件兼容保证。

## 工程验证

以下是本任务工作区的执行结果；真实模型报告与模拟回归分别判断。

| 检查 | 结果与覆盖 |
| --- | --- |
| `go test ./...` | 通过；含空间/MCP/旧有共同执行回归，新原生验收默认 opt-in，不随普通测试调用模型 |
| `go vet ./...` | 通过 |
| 相关 race | `internal/shadow`、`internal/collab`、`internal/mcp` 通过；此前本轮全套相关包亦通过。后续新增的暂停后关闭/退出检查单独以 race 复验 |
| Web check | 通过；1118 条中英消息检查 |
| Web test | 19 个文件、181 项通过 |
| Web production build | 通过；已更新 `internal/webassets/dist/` 与 `internal/mcpassets/dist/` |
| 浏览器 | Edge、实际 Core 与 production 资源；共享聊天、作者、送达、记忆与原生运行信息可见，修正聊天区高度与内滚动后输入框保持可见。最后截图使用已结束测试的真实历史，显示离线；不是在线回执的唯一证据 |

模拟原生回归覆盖持久化失败不得确认送达、相同 ID 去重和作者不变、原生回执丢失后不重放、S2 后 W 结果去重、成员记忆隔离/更正/忘掉、过时 M 水位、任务改版/取消后的旧结果、CM 超时与原生压缩使旧决定失效，以及压缩后恢复新用量。三 Core 协议回归核对执行成员才能确认送达、超时不自动补发、手动同 ID 重试、跨空间隔离与服务端作者/时间不被接收者改写。UI MCP 白名单允许显式界面入口，拒绝内部同步、注册和原生 RPC。

## 真实 Codex 接续

CLI `0.163.0-alpha.11`、`gpt-5.6-luna`、`openai`、`low`，单 Mac，隔离 home/repo 与全新原生会话。认证仅通过符号链接引用，不读写个人配置或旧会话；只回应本夹具两项只读 MCP 的精确审批。最终复验入口：

```sh
GOCACHE=/private/tmp/teamcross-shadow-gocache \
python3 scripts/verify-shadow-native.py \
  --fixture-dir /private/tmp/teamcross-shadow-native-20261021-final \
  --codex-bin /absolute/path/to/codex \
  --codex-auth /absolute/path/to/auth.json
```

通过项见[结构化原生结果](evidence/native.json)：继承的 developer instruction / MCP 标记；B 的记忆归属；W 运行时 C 的短问题得到回复；真实 CM、原生压缩、S2 接手；同一 W 的证据进入 S2；B 纠正/忘掉记忆；关闭后准确恢复同一主 thread；空闲 M 推进合法水位。

为确定性触发维护，测试暂时降低 Core 的 CM 预算线，模型与原生窗口不变。CM 选择 compact 后，验收显式请求换段来补齐 S1 → S2 路径；因此不把该结果描述成默认阈值已经调优。W 使用等待夹具文件的受控 MCP 读取，未授权任意 shell 提权。

初次有效通过的目录为 `teamcross-shadow-native-20261021-c`，135.9 秒。随后修正压缩后的用量、维护超时、本机诊断范围与恢复设置显示，最终 fresh 复验通过，耗时 164.65 秒，以上述 `final` 报告为准。早期失败与修正：

- `native-a` 使用 `approval_policy=never`，原生 MCP 被拒绝；改为夹具按精确请求审批，并未替产品绕过权限。
- `native-b` 的后台 shell 在本机嵌套沙箱报 `sandbox_apply: Operation not permitted`，提升请求未获批准、测试超时；夹具改用受控只读 MCP 等待，不更改用户安全配置。该失败不是所有 shell/Harness 组合可用的证明。

## 空间完整流程

[三 Core 脚本](../../../../scripts/verify-local-shadow.py)使用本次构建、真实 TLS 成员传输和同一 CLI/Luna，在单 Mac 上完成：

1. A 创建空间，B/C 加入；B 创建分身，C 发出带显式记忆要求的消息。
2. 首次回执仍为 sending；B 持久保存后，A/B/C 均看到相同作者、已送达消息和真实模型答复，重复同 ID 没有第二次执行。
3. 停止 B，消息显示发送失败；B 重启后未自动补发。C 手动重发原 ID，仍在原 native thread 回答并使用已有记忆。
4. C 的真实 stdio MCP 读取同一结果；全过程没有建立共同执行 fork。

通过证据：[空间流程结果](evidence/core-flow.json)。输出目录 `teamcross-shadow-core-20261021-c`；其后 UI 使用最新 production 资源重开已完成记录，未追加模型轮次。

脚本曾有两处夹具错误：首次缺少导入 helper 的日志脱敏变量，修正后停止了该次精确数据目录所属的残留 Core；第二次重启 helper 仍传 `/usr/bin/false`，改为保存真实 Codex 启动参数后完整重跑通过。没有把这两次失败计入通过结果。

## 当前边界与后续 eval

- 只有 Codex 路径；未验证 Claude、第三方模型、任意 hooks/插件/电脑控制组合或中途更换 Harness。有效原生配置继承和具体工具组合验收分开报告。
- 单 Mac 多 Core 与 TLS 成员链已测；没有两台 Mac、跨 NAT、强制 DERP、真实 ChatGPT 插件宿主 UI 或专用 Desktop 全入口的新验收。
- 创建原生 thread 的回执丢失且无准确 ID 时保留 unknown 并暂停；首版没有猜测“最近会话”的自动修复。原生审批可影响可用性，不能把在线当成工具全部可用。
- 记忆候选不自动成为事实；忘掉不删除聊天档案。历史检索是本地文本扫描，尚非大规模语义索引；原生最终消息才进入共享聊天，当前未做逐 token 流式展示。
- 已有固定流程与故障回归，尚未做长期对话质量/成本的统计基线。下一阶段固定样本与保留集，每次只改一个阈值或提示词，先守住记忆归属、任务接续、零重复执行与真实送达，再比较约束保留率、延迟和 token；失败可退回已记录配置。

本任务的独立 HTML 仍为说明演示；这次新增产品界面不作为内联 ChatGPT 可视化交付。构建为开发验证版本，没有打包、发布或推送远端。

## 收尾

相对链接检查覆盖 15 份本任务相关 Markdown、455 个本地链接；独立演示 JavaScript 语法检查与 `git diff --check` 通过。最终 Core 构建成功。两轮三 Core 夹具均已按精确进程退出，原生验收关闭所属 app-server，专用浏览器页已关闭；测试证据与隔离目录保留，不删除用户原有会话或其他任务文件。

该目录继续保留为分身设计与后续 eval 的接续入口；本轮 Codex 实现已收尾。未跟踪的 `2026-10-21-in-flow-piggyback-design.md` 属于无关任务，未修改或纳入提交。


## 合入 Meta 与集成回归（2026-10-22）

将 `codex/meta-to-main-20261021` 的 `80eb86b` 合入 `Meta` / `447cca5`。在独立干净检出目录执行，保留 Meta 已有顺流感知、插件面板更新和 Core 恢复实现；原目录及未跟踪的其他任务文件保持原状。

合并解决 MCP 工具清单、翻译、证据索引与生成资源冲突，受限运行时工具由双方各自的 15 项合为 18 项。额外补齐两处跨功能集成：分身编辑器使用 Core/空间/分身/请求语义键保存刷新草稿，结果不明的创建继续沿用原请求 ID；Core 自动升级保护分身接受的队列和仍在运行的主轮、后台、维护、压缩、审批，暂停接收不掩盖正在执行的工作。空闲运行时与离线未知轮次本身不永久阻塞升级。

- `go test ./...`、`go vet ./...` 通过。
- `internal/shadow`、`collab`、`mcp`、`mcpevents`、`sharing`、`nativecodex`、`nativeclaude`、`service`、`cliinstall`、`pluginpack` 的 race 回归全部通过。
- Web check 通过，1146 条翻译；22 个测试文件、204 项通过；两套 production 嵌入资源重新构建。
- 新回归验证未发送正文跨刷新恢复、不同 Core 不串草稿、未知创建刷新后零自动提交且手动重试同 ID、无共同执行时独立分身也阻止升级、暂停后的后台任务继续受到保护，以及维护/审批/压缩和锁竞争下的保守判断。
- 首次新 UI 用例错误地匹配原始网络异常；产品按已有契约转换为连接中断提示。修正测试断言后全套通过，未为测试改变产品错误处理。
- 本轮为合并与生命周期集成，未追加真实模型轮次或新的跨 Mac/插件宿主界面验收。之前的原生报告仍只证明其标注版本与环境。
