Claude Code 单 worker、原生 TUI 与控制端接入验证

验证日期：2026-09-11。Team Cross 代码基线：`8869b6452f7e595382212d1ced1da32e4a47c726`。本机 Claude Code `2.1.268`，官方 Agent SDK `0.3.268`。真实生成请求全部经用户本机 CliProxyAPI，模型字段只出现 `gpt-5.6-luna`。这是同一台 Mac 上的隔离实验，未修改 Team Cross 产品代码，未进行两台 Mac 的 LAN 验收。

结论：当前版本能够以一个 A 侧后台 job 承载原生 TUI 和控制端输入。原生 TUI 可以通过实验转发层接入；输入归属可以在转发层强制检查；待审批命令可以在 B 失去输入权后交给 A 继续批准。控制端的一次性后台 `reply` 请求也能向同一个 job 发送消息。这条路径已经有运行证据，但尚未达到 Team Cross 现有 Codex provider 的完整结构化控制能力。

ACP 对语义适配有帮助。检查的实现为 `@agentclientprotocol/claude-agent-acp` `0.76.0`，固定提交 `4deace40379a62eb642559454930af4c590b3d49`。它通过 Agent SDK 的 `query({prompt, options})` 启动 CLI，并将 `canUseTool` 转为 ACP 的 `session/request_permission`。实现会先发出工具调用通知，再请求审批；取消信号会取消客户端请求，并用本地 abort race 释放等待。这些做法可以借鉴到 Team Cross 的事件顺序、审批取消和 provider 适配层。它没有在该路径中把原生 `claude attach` 接入一个已有后台 job。

源码依据：[启动 SDK query](https://github.com/agentclientprotocol/claude-agent-acp/blob/4deace40379a62eb642559454930af4c590b3d49/src/acp-agent.ts#L8165)、[审批和取消](https://github.com/agentclientprotocol/claude-agent-acp/blob/4deace40379a62eb642559454930af4c590b3d49/src/acp-agent.ts#L6893)、[SDK 选项与 canUseTool](https://github.com/agentclientprotocol/claude-agent-acp/blob/4deace40379a62eb642559454930af4c590b3d49/src/acp-agent.ts#L8004)。本次对 ACP 做了源码核对，没有运行完整 ACP 服务与编辑器客户端；原生 worker 路径则做了下述实测。

实验使用的连接关系如下。A/B 表示协作角色；测试进程均运行在一台 Mac 上。

```text
B 原生 claude attach <短 ID>
    → B 专用配置中的本地 Unix socket 入口
    → loopback TCP
    → A 侧实验网关：检查协作 job、当前输入者和 epoch
    → Claude supervisor 的 control.sock
    → 一个 Claude 后台 job

A 控制端 → 同一网关 → 同一后台 job
    ├─ 后台 reply：发送普通消息
    └─ attach 终端流：接力审批、Esc 中断
```

本机捕获到的原生 attach 连接先发送一行 JSON 请求并收到一行 JSON 确认，随后承载 ANSI 终端输出与按键输入。实验网关转发这个流，没有自行重画 Claude 界面，也没有在交接时执行 `claude --resume` 创建另一个交互运行时。此前 `/status` 中的 `cc-socks` peer address 属于跨会话消息路径，不能据此把它当成 TUI attach 的传输端点。

| 验证项 | 实际结果 | 证据与边界 |
| --- | --- | --- |
| 先 fork，再建立后台执行端 | 通过 | 官方 SDK 离线 fork：零 fetch，内容保留，源会话哈希不变；fork ID 随后成为后台 session ID。 |
| B 原生 TUI 经转发连接 | 通过 | 未修改 Claude 可执行文件；原生 attach 收到后台渲染的界面，显示继承的历史。 |
| 转发前断线，不自动重放输入 | 通过指定故障点 | 丢弃一个尚未送达 worker 的输入包并断开连接；原生客户端自动重连，指定文本未进入会话。未覆盖“已送达但确认丢失”等全部故障点。 |
| 普通控制消息也检查输入权 | 通过 | B 持有输入时，A 的 `reply` 被网关拒绝；拒绝的文本未进入会话。 |
| A 接回时，B 旧批准失效 | 通过 | 输入归属由 B/epoch 1 改为 A/epoch 2 后，B 的批准按键被拦截；目标文件没有创建。随后断开 B，拒绝其重新 attach。 |
| A 接力处理原来的审批 | 通过 | A attach 后仍看到同一待审批命令，批准后生成目标文件；审批前后的工具调用 ID 完全一致，只有一个工具调用。 |
| A 控制端向同一个 job 发消息 | 通过 | 一次性 `reply` 收到 `ok: true`；会话中出现用户消息及 `TCX_A_CONTROL_OK` 回复。成功判断同时检查了结果，未把接收确认当成完成。 |
| 中断正在运行的命令 | 通过 | 命令先写 started 文件，再 `sleep 20`，最后写 finished 文件；经 A 终端流发送 Esc 后出现 Interrupted，等待超过 20 秒仍无 finished 文件。 |
| 后台 job 身份保持 | 通过 | 完整一轮前后，`claude agents --json --all` 报告同一 session ID、启动时间及 PID `29869`。 |
| B 独立配置，无历史副本 | 通过单独复核 | B 使用独立 `CLAUDE_CONFIG_DIR` 和空 cwd，仅建立本地 job 短 ID 目录与 socket 转发入口；看到 A 历史，B 的会话 JSONL 数量前后均为零；零生成请求。 |
| 完整结构化审批、取消 API | 尚未证实 | 上面的审批与中断通过终端流实现；没有证明同一个原生后台 worker 同时暴露 SDK/ACP 风格的 request-ID 审批和结构化 interrupt。 |
| 两台 Mac、TLS 与真实身份鉴权 | 未验证 | TCP 只监听本机回环地址；测试角色由专用入口区分，没有实现可发布的远程认证协议。 |

完整成功轮的 source ID 为 `22e4ecba-a09a-4a52-90ee-9788a9d1bfef`，fork/session ID 为 `c5a0243f-80a9-422f-8d29-07bda07a8cb2`。生成的批准文件内容为 `TCX_HANDOFF_OK\n`。同一轮包含 7 次生成请求；包含测试脚本修正前的迭代在内，本次共 13 次生成请求，模型均为 `gpt-5.6-luna`。独立 B 客户端检查的生成请求数为 0。详细字段见 [完整运行结果](evidence/live-gateway-summary.json) 和 [独立 B 配置结果](evidence/separate-client-summary.json)。

独立 B 配置的检查补充了一个接入要求：原生 `claude attach <短 ID>` 先通过本地 job 登记解析参数，只有转发 socket 时会报 `No job matching`。本版本中，在隔离配置下创建对应的空 `jobs/<短 ID>` 目录即可继续 attach，不需要复制 A 的 transcript 或 `state.json`。本实验的 B 请求没有携带原生 daemon auth 字段；网关在 A 侧连接上补入 A 的 daemon 凭据。该结果说明连接适配可行，不能据此宣称跨机器认证已经完成。产品实现仍需使用 Team Cross 自己的加入凭据验证 B，并避免让 B 获得 A 的 daemon 凭据。

同样需要区分审批规则与交接场景：B 拿着输入权时，B 可以直接批准自己触发的命令，A 不需要再批准一次。这里验证的是“审批尚未完成时，A 主动接回输入”的情况。Team Cross 当前 Codex 路径的 `Session.Respond` 也先检查当前输入者和待审批项，再将回应转发给 provider；见 [rpc.go](../../internal/collab/rpc.go) 的 `Session.Respond`。本次只验证 Bash 审批，不能推广成 Computer Use、浏览器操作和所有 MCP elicitation 都已验收。

与现有 Codex 实现对齐时，剩余工作可以具体分为以下几项：

1. 把 A 侧 worker 管理和 B 侧本地连接入口接进 Team Cross 的协作生命周期。短 ID 登记、原生终端能力字段、socket 清理和版本变化都要由适配层处理。
2. 为同一个原生后台 worker 补齐可靠的结构化状态与控制。官方 `claude agents --json` 可读取粗粒度状态；现有实验还没有提供完整事件流、可交接的 request-ID 审批或语义明确的“中断当前轮”API。解析终端文案和发送 Enter/Esc 不足以承担全部 WebGUI/MCP 语义。
3. 完成写入 requestId 的去重与查询，并覆盖写入已送达但确认丢失、繁忙时补充、supervisor/worker 重启等情况。当前原型不重试普通 `reply`，也不把 Claude 内部高层回复函数中的排队重试行为直接搬入 Team Cross。
4. 完成独立机器上的 TLS、加入凭据、断线/睡眠恢复及退出清理；检查每个转发操作的权限和会话范围。当前本地实验不代表这部分完成。
5. ACP 的审批顺序、取消和模型状态映射可作为参考实现；如果引入 ACP，仍需明确它控制的运行时就是承载原生 TUI 的 worker。直接启动 ACP 当前的 SDK query 会进入另一条运行时路径，不能默认把两者视为一个执行端。

这条原生 attach 路径依赖 Claude Code `2.1.268` 当前的内部 supervisor 协议。官方把 agent view 标为 research preview，并明确要求外部读取状态优先使用 `claude agents --json`，而非把 job 状态文件作为稳定接口。产品化应锁定并检查已验证的版本，不能把此次观察当成长期协议承诺。参见 [官方 agent view 文档](https://code.claude.com/docs/en/agent-view#read-session-state-from-a-script)。

本次保留的证据包括 [原生连接记录](evidence/worker-wire-summary.json)、[B 终端记录](evidence/gateway-native-b.txt)、[A 控制端终端记录](evidence/gateway-controller-a.txt)、[独立 B 原生终端记录](evidence/separate-client-native-b.txt)、[模型请求元数据](evidence/model-requests.jsonl) 和 [清理核对](evidence/cleanup-verification.json)。终端文本仅移除了控制序列，没有重建屏幕的光标布局，因此局部文字可能连在一起；判断以运行结果、工具 ID 和实际文件为主。

可审阅的测试代码在 [实验网关](prototype/controlled_gateway.py)、[完整运行脚本](prototype/live_gateway_test.py) 和 [独立客户端脚本](prototype/separate_client_probe.py)。这些文件保存的是本次实验原型，依赖专用 fixture、测试配置和报告所列 SDK 路径，尚不是可直接部署的 Team Cross 模块。第一次接力测试补齐了原生 caps 字段；第二次补齐工具调用落盘的等待；独立客户端测试定位了本地短 ID 解析和可缺省的 auth 字段。最终通过结果对应上述修正后的脚本。

所有本次测试客户端、worker、supervisor 和转发 socket 已关闭；按已记录 PID 及测试目录核对，没有剩余进程或打开文件。用户原有 Claude 配置和会话未修改。Team Cross 的 tracked 文件无改动，新增内容仅在本报告所在的 output 目录；没有提交、推送或更改产品支持范围。
