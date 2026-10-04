# 接收会话配对与交给 Agent

本页维护配对身份、接收适配范围、明确发送与回执的领域契约及取舍。字段和路由见 [协议](../protocol.md#接收会话配对与请求)，验证方法见 [验证门槛](../validation/test-gates.md#接收会话配对)，最近已知结果及未覆盖路径见 [证据入口](../validation/evidence-map.md#配对与空间请求)。

## 统一交互

空间面板提供“连接当前会话”：自定义名称是可选展开项，不填写也可直接连接。用户明确点击后，宿主向当前对话发送一次 `connect_current_session(spaceId,name?)` 请求；Core 从工具传输核对身份，复用同一空间、同一会话的关联，并单独报告是否具备接收能力。空间 mention 可以省去寻找空间 ID，但选择引用本身不会绑定或订阅。

跨客户端仍可生成十分钟有效的一次性提示，粘贴到要接收任务的具体会话，由 Agent 调用 `pair_current_session`。批注、资源选择清单和设置复用这套接收配对。新的会话需要单独关联，不按最近使用的会话猜测目标。下面展示复制提示的路径。

配对完成后，在批注或选择清单点击“交给 Agent”，选择已配对目标，选择“分析后告诉我”或“分析并回复原批注”，编辑处理要求后发送。选择清单里的“生成读取入口”和批注的“复制提示”继续可用。配对可在设置移除，最近二十个请求的处理回执也可在设置查看。

保存批注本身不启动 Agent；只有明确发送才创建请求。配对不启用共同执行、不创建 fork、不接管输入权、不共享其他个人历史，也不订阅后续讨论。持续关注另走明确授权的 [空间事件订阅](space-events.md)。

```mermaid
sequenceDiagram
    participant U as 用户
    participant TC as Team Cross
    participant A as 目标 Agent 会话
    U->>TC: 为接收会话起名，生成配对提示
    U->>A: 首次粘贴一次性配对提示
    A->>TC: pair_current_session
    TC->>A: 核对当前身份与接收能力
    Note over TC,A: Channel 还需通知挑战与 confirm_pairing 回执
    TC-->>U: 核验通过后显示已配对
    U->>TC: 选择批注或材料，点击交给 Agent
    TC->>A: 投递请求 ID
    A->>TC: read_agent_request，按引用读取原文
    TC-->>U: 已读取请求
    A-->>U: 在目标会话给出分析建议
    A->>TC: finish_agent_request
    TC-->>U: Agent 报告的完成状态和摘要
```

## 身份、接收能力与范围

配对面向确定的原生会话，创建入口和用途不划分接收能力，见 [会话与接收能力](../product-core-and-glossary.md#会话与接收能力)。实际投递核对身份、连接、授权和运行状态；当前 Team Cross 的接入限制见下文。

空间内的公开批注与材料可以通过工作台发起空间请求。成员需将配对明确关联到空间；接收目标可分布于不同成员的 Core，记录和结果由空间托管端保存。此路径不自动公开个人配对列表或旧请求，也不要求把 A 的配对码粘贴到 B 的 Core。需要另起上下文时，可按需新建会话并绑定返回的原生 ID。详细语义见 [空间工作台](space-workbench.md)。

- Codex 身份来自 MCP `_meta` 中的当前 thread/turn；Claude 来自进程的 `CLAUDE_CODE_SESSION_ID` 和本次 `_meta.claudecode/toolUseId`。个人 Claude 的配对、接收核验和请求读取/完成都核对工具调用确实存在于该会话最新 transcript，防止恢复会话后使用过期进程身份。工具参数不能指定 Provider、Session 或接收连接。
- Codex 通过原生连接向确定的 `threadId` 提交输入；`thread/start`、`thread/fork` 和 `thread/resume` 分别建立或继续会话，后续均使用 `turn/start`。当前投递复用 `Session.RPC(turn/start)`，检查连接、输入授权、空闲状态及审批，同一请求 ID 不重复执行。投递到共同执行目标时，另遵守其输入归属和本空间引用范围。原生接口见 [App Server 文档](https://learn.chatgpt.com/docs/app-server)。
- 个人 Claude 使用独立于共享运行时的实验性 Channel 适配器。STDIO 声明 `experimental["claude/channel"]`，Core 将最小事件送到该连接，再发 `notifications/claude/channel`。首次配对必须收到通知中的随机挑战并通过 `confirm_pairing` 回传，写入 STDIO 本身不是接收证明。未开启 Channel、账户/组织不支持或宿主丢弃通知时，状态停留在 `verifying`，到期失效。
- 配对绑定用户选定的原生会话，不隐式新建会话或切换目标。创建与恢复运行时按明确操作执行，核对原生 ID，不猜测默认 app-server，也不把普通 MCP 日志通知当作模型唤醒。[工作台的新建入口](space-workbench.md#按需新建接收会话)是可选操作，不是接收请求的前提。
- 配对码、会话目标和选择引用均属于当前 Core 数据目录。跨设备配对码路由尚未接入，不能把 A 上的码直接交给只连接 B Core 的工具。已有 `joined-…` 引用仍按本机成员权限读取。

### 当前接入范围

已有 Codex 会话除 `a.sessions` / `a.receivers` 外，还可通过当前原生 daemon 的 `codex app-server proxy` 建立连接。它是字节隧道上的 WebSocket；先核对 `thread/loaded/list` 包含当前 MCP 证明的精确 thread，再向同一 thread 提交输入。连接发现不启动 daemon，不调用 `thread/start/fork/resume`，不改写当前模型或原生审批。目标忙碌或原生连接不可用时显示原因；解除关联只关闭 Team Cross 自己的代理，不结束用户的 daemon 或会话。Core 重启后需明确重新连接，不自动恢复借用的连接。实现见 [当前会话连接](../../../../internal/collab/agent_connections.go) 与 [原生代理](../../../../internal/nativecodex/process.go)。

ChatGPT 调用使用 `_meta["openai/session"]`，并结合可用的 subject/org 生成不透明本机关联键。它不是原生 `threadId`，不能用它调用 Codex 原生输入。`linked` 表示空间关联已保存，`paired` 才表示该适配已核验接收能力；ChatGPT 本机 mention / 面板连接目前只建立前者。Cloud 事件订阅是独立授权的接收方式，见 [空间事件](space-events.md)，不能由 `linked` 或 `ui/message` 的宿主回执推断已具备云端接收。

缺少可核对身份的调用返回 `unsupported`；相同会话关联可重试，丢失宿主消息回执时先查看当前对话，界面不自动重发。对应的实际验证范围见 [证据入口](../validation/evidence-map.md#配对与空间请求)。

Claude 的普通 MCP 配置不代表已启用 Channel。开发接入需按 [官方 Channel 文档](https://code.claude.com/docs/en/channels-reference) 在运行中的客户端显式启用对应服务；Team Cross 不自动改写用户的启动参数或账户设置。该适配仍为实验性，最近已知真实探测受宿主门槛阻塞，具体版本及失败范围见 [证据入口](../validation/evidence-map.md#配对与空间请求)。协议测试通过不能推广为任意账户上的真实模型验收。

## 投递与回执

请求保存用户处理要求、处理方式和不可变引用组。材料引用绑定固定版本；批注保留原批注身份；上下文仍需重新核对当前原文。发送及 `read_agent_request` 都重新检查引用可访问性，后续正文读取继续使用 `read_material`、`read_annotations`、`read_context` 等既有工具。

通知只带请求 ID。Agent 调用 `read_agent_request` 后才记录已读取；引用每页一项，按 `nextOffset` 继续；调用 `finish_agent_request` 才记录它报告的完成/失败与摘要。完成回执不等于批注回复已保存，回复仍须由 `reply_to_annotation` 单独返回保存回执。原生 `turn/start` 返回和 Channel 传输成功均不代表分析完成。

请求状态为 `submitting → submitted → received → completed/failed`，投递结果不明使用 `unknown`。接收或完成可以早于发送调用返回，后来的发送回执不得回退状态。请求在写入外部会话前落盘；Core 重启把未确认的 `submitting` 视为 `unknown`，不重放写入。相同请求 ID、相同内容返回已有状态；内容变化拒绝。

Channel 连接租约只存在内存。STDIO 结束会取消接收请求；Core 重启或接收连接失效后需要重新配对，不自动重放未确认事件。移除配对阻止后续读取与投递，已提交的原生输入不因此撤回。宿主自身的工具权限确认保持有效。

## MCP Events 的边界

[OpenAI MCP Events 文档](https://developers.openai.com/plugins/build/mcp-events) 当前描述的是支持 MCP 2.0 的插件及 webhook 订阅，宿主范围为 ChatGPT Work Cloud 和 dots。它要求可被该宿主访问的认证 MCP endpoint、订阅管理和签名 webhook，并不是给本机 STDIO 增加通知就能向任意 ChatGPT 会话发送输入。

已实现独立事件网关、MCP 2.0 发现/订阅和签名回调，授权、持久化及回执边界由 [空间事件契约](space-events.md) 维护。本机原生投递与 LAN/Tailcat 成员协作不依赖该网关；不配置公网 HTTPS 入口仍可正常加入空间、关联本机会话并接收明确请求。未发布公网服务，真实 ChatGPT Cloud 宿主验收由用户明确暂缓；协议测试不代表云端宿主通过。

实现入口：[Core](../../../../internal/collab/agents.go)、[路由](../../../../internal/collab/agents_http.go)、[MCP](../../../../internal/mcp/agents.go)、[界面](../../../../packages/web/src/components/AgentPairings.tsx)。字段见[协议](../protocol.md#接收会话配对与请求)，验证方法见[验证门槛](../validation/test-gates.md#接收会话配对)。
