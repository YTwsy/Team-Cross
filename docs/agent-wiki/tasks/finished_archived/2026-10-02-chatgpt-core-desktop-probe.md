# ChatGPT 本机 Core 面板桌面补验

状态：本轮已完成。实际桌面材料读取与批注回复、磁盘持久化及收尾均通过。本记录限定于以下提交和这台 Mac，不是正式产品安装或公开分发验收。

## 目标与环境

补验[上一轮记录](2026-10-01-chatgpt-local-plugin-probe.md)未完成的 Core 面板操作：在 ChatGPT for Mac 的本机 Work/Codex 环境，通过本地插件界面调用现有 Team Cross stdio MCP 读取材料、保存批注回复，不增加 Team Cross 云端后端、公网入站 MCP 地址或 Tunnel。

- 日期：2026-10-02，北京时间约 00:40–00:48。
- 被测来源：`Next`，`76ee81258eae0333d799697040f80ee94e8df14b`；未修改产品代码。
- ChatGPT：`26.928.31416` / build `12553`，bundle `com.openai.codex`。
- 原生运行时：`codex-cli 0.159.2`；macOS 14.8.5 / arm64；Go 1.27.1。
- 本次探针：`teamcross-core-probe`，`0.2.0`；独立本地 marketplace `teamcross-core-validation-20261002`。
- 独立源码和证据目录：[操作说明](</Users/wsy/Documents/Codex/2026-10-02/teamcross-core-desktop-validation/RUNBOOK.txt>)；独立 Git 分支 `codex/core-desktop-validation`，尚未提交。

本轮自动检查没有启动真实模型轮次。隔离 Core 的来源使用合成原生历史；不读取用户已有协作或历史，不启用共同执行。桌面操作由用户完成。需要模型打开面板时，观察请求明确限定专门测试会话和 `gpt-5.6-luna`；本轮没有采集用户个人会话的模型设置。

## 接入与隔离

实际链路是 MCP Apps HTML 面板 → 本地 stdio 适配器 → 当前构建的 `teamcross mcp --data-dir <fixture>` → `127.0.0.1:64664` 的隔离 Core。适配器是外部验证源码，尚未并入产品。

隔离 Core 以外部 Go overlay 运行，使用真实 `collab.App`、HTTP handler、材料发布管线和批注持久化。仅原生历史由既有 `fakeRuntime` 合成。独立数据目录为 `core-fixture-1/state`；材料标记为 `LOCAL-CORE-20261002`，对应一个空间、一份材料和一条原批注。

适配器及其 MCP 子进程使用默认禁止网络、仅允许 `localhost:64664` 出站的沙箱。实测该 Core 端口允许连接，其他回环端口及非回环地址均返回权限拒绝；Core 只监听 `127.0.0.1`。HTML 没有外部资源，界面 CSP 的外部连接/资源列表为空。

插件资源与入口按 [MCP Apps UI](https://developers.openai.com/plugins/build/chatgpt-ui)、[Extensions](https://developers.openai.com/plugins/build/extensions) 和[本地插件](https://developers.openai.com/plugins/build/plugins)文档编写。以上通过结果来自实际本机测试；公开目录的分发和审核要求未在本轮验收。

## 实际桌面证据

工具再次拒绝操作 `com.openai.codex` 主窗口，可操作的当前聊天 MCP Apps 面板列表仍为空。没有使用其他 UI 技术绕过限制。用户按请求读取测试材料后保存测试回复，并返回面板中的完整回执：

```text
status: saved
回复编号：1ded832b-4d06-4c38-bfb8-132ba58177f6
requestId: ui-core-82f80027-52c2-4b03-9c72-75cb083bab95
UI-CORE-REPLY:6e225b73-efe0-4b5c-9645-55570ade3741
```

该回执与同一个桌面插件进程的 HTML 读取、材料读取、回复保存日志逐项匹配。该进程 PID 为 `95485`，父进程 `37241` 是用户桌面自己的原生 app-server；与自动检查的 app-server / 适配器进程不同。run 为 `a5d2cab1-0cf7-4d0f-9ab4-1fe2ef759b52`，启动目录是本次安装缓存 `teamcross-core-validation-20261002/teamcross-core-probe/0.2.0`。

同一 run 读取的 HTML SHA-256 为 `ef85d858585bb3296ef4aa7b81c2c7abfec2b2b77a64c5f77e5f22b918da657c`，与新探针源码完全一致。桌面读取返回本次材料和批注 ID；UI 生成的 `ui-core-` requestId 区别于脚本操作。磁盘中该 requestId 仅有一条回复，回复编号和文本均与用户回执一致。

| 检查 | 结果 |
| --- | --- |
| 本地安装与来源范围 | 通过；只增加本次 marketplace 和插件配置块，其他配置字节及 marketplace 保留 |
| 原生缓存包启动、资源与材料读取 | 通过；其他 MCP/插件禁用，无模型轮次 |
| 实际桌面读取与保存 | 通过；用户回执与桌面进程日志、真实 Core 记录对应 |
| 回复持久化 | 通过；磁盘与观察快照一致，回复数为 1 |
| 输入与执行边界 | `forks=0`、没有 ExecutionRecord；原生 fixture 调用只有 `thread/read`、`thread/turns/list` |
| 网络沙箱与 listener | 通过；仅本次 Core 的回环端口允许连接，没有公网入站地址或 Tunnel |
| fixture 完成 | `TestChatGPTLocalCoreProbe` 通过，runner 退出码 0 |

自动脚本只做读取与资源检查，没有保存本轮回复。桌面观察使用人工回执，没有获取桌面截图；该限制不应被描述为截图复核通过。

## 收尾与证据

本轮预留最长 12 小时操作窗口。检测到 `ui-core-` 保存后经过两分钟展示宽限期，fixture 执行最终持久化与输入边界断言并正常退出。随后按记录中的 PID、父进程、实际 Python 命令、插件缓存工作目录或 MCP 数据目录核对，只结束本任务进程，并移除本次具名插件与 marketplace。

最终独立审计确认所有本轮测试进程退出、Core 端口关闭、用户桌面 app-server `37241` 仍然运行。个人设置和其他 marketplace 保留，没有回滚期间的其他设置变化。运行中的初始配置差异检查遗漏 marketplace 配置块而给出 false；独立审计在内存中只移除两个精确的本次配置块，预期 SHA-256 与实际卸载后配置完全相同，确认没有额外修改。原始诊断与补充依据均留在生命周期记录中。

- [安装核对](</Users/wsy/Documents/Codex/2026-10-02/teamcross-core-desktop-validation/evidence/desktop-install-state.json>)、[原生缓存检查](</Users/wsy/Documents/Codex/2026-10-02/teamcross-core-desktop-validation/evidence/core-plugin-check.json>)、[网络隔离](</Users/wsy/Documents/Codex/2026-10-02/teamcross-core-desktop-validation/evidence/core-network-policy-check.json>)。
- [用户桌面回执](</Users/wsy/Documents/Codex/2026-10-02/teamcross-core-desktop-validation/evidence/user-desktop-receipt.json>)、[桌面对照检查](</Users/wsy/Documents/Codex/2026-10-02/teamcross-core-desktop-validation/evidence/desktop-ui-check.json>)、[适配器日志](</Users/wsy/Documents/Codex/2026-10-02/teamcross-core-desktop-validation/evidence/core-probe.jsonl>)。
- [Core 最终结果](</Users/wsy/Documents/Codex/2026-10-02/teamcross-core-desktop-validation/core-fixture-1/result.json>)、[runner 日志](</Users/wsy/Documents/Codex/2026-10-02/teamcross-core-desktop-validation/evidence/core-fixture.log>)。
- [进程收尾](</Users/wsy/Documents/Codex/2026-10-02/teamcross-core-desktop-validation/evidence/desktop-process-cleanup.json>)、[配置范围审计](</Users/wsy/Documents/Codex/2026-10-02/teamcross-core-desktop-validation/evidence/cleanup-config-scope.json>)、[生命周期记录](</Users/wsy/Documents/Codex/2026-10-02/teamcross-core-desktop-validation/evidence/lifecycle.json>)、[最终审计](</Users/wsy/Documents/Codex/2026-10-02/teamcross-core-desktop-validation/evidence/final-audit.json>)。

源码、测试仓库、合成数据和证据保留在 Documents 中，没有上传。私有 `connection.json` 的控制 token 不写入报告或证据。由于主窗口自动化限制，不能代用户关闭其仍显示的面板；该探针的后端与临时安装均已撤销。

## 结论边界

这台 Mac 的本机 Work/Codex 已实际验证：本地插件可显示 MCP Apps 界面，并经现有 Team Cross stdio MCP 读取材料、保存批注回复，接入不需要增加 Team Cross 云端后端、公网入站或 Tunnel。它为后续开发本机插件界面提供依据，尚未改变产品的正式支持范围。

本轮未分别验收 global / thread 两个入口，未测试普通 Chat、网页或云端 Work、用户真实协作数据、共同执行、两台 Mac 的 LAN/Tailcat 或公开目录分发。本机 Core 不依赖公网入站，也不能推导 ChatGPT 模型离线或材料永不进入模型上下文。

按[任务记录规则](../../README.md#任务记录)归档本轮；上一轮超时结果保留原状，没有改写成此次通过结果。
