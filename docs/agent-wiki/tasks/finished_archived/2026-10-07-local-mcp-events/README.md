# 本地 MCP Events 宿主能力检查

日期：2026-10-07（Asia/Shanghai）。本轮检查已收尾；本地事件流接入仍阻塞，没有新增产品接收能力。用户授权开始验证此前讨论的最小事件服务、目标宿主订阅、空闲后接收、原文读取和处理回执。

## 结论与环境

所测 Codex app-server 包含实验性 `mcpServer/event/stream/start` / `stop` 和通知接口，但向本地配置的 STDIO MCP 服务建立订阅时，返回 `-32600: MCP event subscriptions are only supported for hosted apps`。默认 MCP 协议路径、启用 `mcp_2026_07_28`、同时启用 `apps` 的三组检查均返回同一错误。不能把接口存在当作本地服务已获支持。

被测产品基线：Meta `6e9c890f8911ab9ee7990f33ba90d9e3840d28b3`；本次只新增验证脚本与文档，没有修改 Team Cross 产品代码。环境为单台 macOS 14.8.5 arm64，安装的 Codex CLI `0.160.0`；Desktop 为 `26.930.51102` / build `13100`。真实模型调用仅使用 `gpt-5.6-luna`，请求禁止模型回退，`thread/start` 返回的模型经断言核对。

稳定证据：[结构化结果](evidence.json)。复现入口：[verify-local-mcp-events.py](../../../../../scripts/verify-local-mcp-events.py)。原始 wire、stderr、测试配置和原生会话保留于 `/private/tmp/teamcross-mcp-events-20261007-k24axvn9`；重要结论、回执、接口错误、版本及清理检查已保存到本目录，不仅依赖临时路径。

## 执行与结果

先以独立 Python STDIO 服务实现草案的事件目录、`events/stream`、`events/poll`、心跳、取消、引用读取和完成回执。事件正文只含虚构空间/批注引用；源文标记在触发时随机生成，不提前交给模型。该服务不连接真实 Figma 或 Team Cross 空间，不提供普通订阅工具。

协议对照客户端明确发起流请求，收到 `active` 和关联事件，再读取来源、保存完成回执、以原游标重放同一事件 ID 并取消。该组通过，属于模拟宿主协议检查，不是模型自动接收验收。

| 检查 | 接入方式 | 结果 |
| --- | --- | --- |
| 服务端事件目录、流投递、引用读取、完成回执、同 ID 重放与取消 | 合成 STDIO 客户端，无模型 | 通过；证明探针服务能提供对应协议行为 |
| 默认 MCP 协议路径，`apps=false`，`mcp_2026_07_28=false` | 真实 app-server；独立 Luna 会话 | 模型实际调用连接工具，MCP 身份与目标 thread 一致；流订阅被 hosted apps 限制拒绝 |
| MCP 2.0 实验开关，`apps=false`，`mcp_2026_07_28=true` | 真实 app-server；另一独立 Luna 会话 | 同上；启用该开关未开放本地 STDIO 流订阅 |
| `apps=true`，`mcp_2026_07_28=true` | 真实 app-server；直接 `mcpServer/tool/call` 对照，无模型轮次 | 普通本地工具调用和 thread 核对通过；订阅仍返回同一错误，排除关闭 apps 造成的误判 |

两条有效 Luna 会话分别是 `01a11269-9685-7b80-91b8-1e5864c747da`、`01a11269-cc2e-73a0-b196-19737ec202cc`，首轮均已完成。之后通过安装版公开生成的实验性 schema 构造 `mcpServer/event/stream/start` 请求，没有追加 `turn/start`、队列输入、Channel 或定时任务。三组服务端实收方法均为 `initialize`、`notifications/initialized`、`tools/list`、`tools/call`；未收到 `events/list` 或 `events/stream`。

首轮预检查错误地关闭 `code_mode_host`，模型轮次结束但没有工具回执，stderr 报 `code-mode host is disabled`。随后修正隔离配置，用新的空目录执行上述有效检查。该预检查不纳入产品 Events 判断；摘要保留在 `excludedPreflight`，完整临时现场为 `/private/tmp/teamcross-mcp-events-20261007-y49q15nq`。本次共三次 Luna 模型轮次，其中两次具备有效 MCP 身份回执。

## 支持范围与未覆盖项

此结果证明当前安装版对本地配置 MCP 服务的事件订阅有明确限制。测试外层 app-server 客户端由探针提供，不能描述为个人 Desktop UI 已完成订阅和接收。

只读核对 Desktop 安装包，发现 `mcpServer/event/stream/start`、`stop`、`notification` 及向所属窗口转发 `mcp-event-notification` 的代码。这说明窗口事件路由已有实现；没有据此证明本地服务可订阅或空闲 Agent 可自动开始处理。

订阅前提被拒绝，因此原会话空闲后的事件到达、自动开始分析、真实模型读取事件原文和完成回执均未达到执行前提。没有向未订阅连接强行发送通知，也没有使用另一输入接口代替 Events。协议对照中的读取/完成回执不能推广为模型通过。

未覆盖真实 hosted app、ChatGPT Work Cloud、日常个人 Desktop 的运行实例、真实 Team Cross 空间、HTTP Events 流或两台 Mac。当前 Cloud webhook 集成仍以 [空间事件契约](../../../sources/decisions/space-events.md)和 [官方文档](https://developers.openai.com/plugins/build/mcp-events)为准；本次不扩大其验收范围。

若后续宿主版本明确开放本地订阅，应重新检查对应版本的实际订阅请求，再验证准确原会话的空闲接收、处理启动、引用读取、完成回执、取消和重连。现有最小探针可复用；自行增加宿主、改用托管入口或其他投递通道属于另一个支持目标。

## 复现与清理

仅在获得真实模型验证授权后运行，传入新的空测试目录；已有文件不会被覆盖。脚本为每组创建独立 Codex home 和测试仓库，只关联已有认证，不修改用户配置或使用用户已有会话。普通工具使用精确的测试工具授权，其他原生请求拒绝；验证线程限定只使用 fixture MCP 工具。

```sh
python3 scripts/verify-local-mcp-events.py \
  --fixture-dir /private/tmp/teamcross-mcp-events-fresh-fixture \
  --codex-bin /absolute/path/to/codex
```

本轮协议客户端、app-server、MCP 服务及其测试执行子进程均已关闭，核对所属进程组和服务端记录的 PID 后无遗留。原有 Desktop 主进程与 app-server 仍在。没有启动 Team Cross Core、其他浏览器或真实空间客户端，没有结束用户已有会话。

验证脚本完成语法检查、最小服务协议对照和上述真实宿主检查；文档检查相对链接、代码路径与 `git diff --check`。没有修改 Go 或 Web 产品代码，本轮不运行与该探针无关的 Go/Web 全量工程检查。
