# ChatGPT 本地插件与 MCP Apps 界面验证

状态：本轮收尾。基础桌面界面已通过，隔离 Core 接口已通过；Core 面板的实际桌面按钮未确认，补验需要新的 fixture。本文记录本次执行结果，不是产品接入承诺。

## 目标与环境

验证 Team Cross 的高级用户界面能否沿用本机 stdio 接入，不引入 Team Cross 云端后端、公网入站 MCP 端点或 Tunnel。先使用完全独立的假数据探针；界面验证通过后才接隔离 Core。

- 日期：2026-10-01。
- Team Cross 来源：`Next`，`76ee81258eae0333d799697040f80ee94e8df14b`；未修改产品代码，未调用现有协作工具。
- 本机 ChatGPT：`26.928.31416`，build `12553`；bundle `com.openai.codex`。
- 原生运行时：`codex-cli 0.159.2`，macOS 14.8.5 / arm64。
- 真实模型：独立仓库与 ephemeral 测试会话，运行时确认 `modelProvider=openai`、`model=gpt-5.6-luna`、`reasoningEffort=low`。
- 探针版本：`0.1.0`；本地 marketplace `teamcross-probe-20261001`。

## 已执行检查

| 检查 | 结果与边界 |
| --- | --- |
| stdio MCP 初始化、工具与资源列表 | 通过 |
| `probe_echo` 随机编号往返 | 直接协议与原生 app-server 均通过 |
| `text/html;profile=mcp-app` 资源读取 | 与原始 HTML 字节完全一致；SHA-256 为 `f77e199014e53bd7ac71882035c268c79c434e71a68715488780534e07b129db` |
| 本地插件安装、缓存包启动 | 通过；原生 inventory 和模型事件确认插件 ID |
| gpt-5.6-luna 调用安装后的插件 | 默认配置和单次启用 `enable_mcp_apps` 两轮均通过；随机编号与本机日志匹配 |
| 原生界面信息 | `_meta.ui.resourceUri`、global/thread 元数据保留，模型调用附带 `mcpAppResourceUri`；两轮 `mcpAppUi` 都为 null，但后续桌面人工观察实际渲染成功 |
| 实际桌面界面与按钮 | 用户确认页面显示“本机调用成功，随机编号匹配”；同一进程日志记录对应 HTML 读取与按钮生成的 UUID，网络 guard 为 denied |
| 网络约束 | 探针以 `(deny network*)` 沙箱启动；联网 canary 被系统拒绝，直接/原生检查时无网络描述符；真实模型仍使用正常 OpenAI 连接 |
| CLI 安装/移除完整往返 | 通过；移除后个人配置字节和其他 marketplace 列表恢复原状 |

界面资源与入口元数据按 [MCP Apps UI 文档](https://developers.openai.com/plugins/build/chatgpt-ui) 和 [Extensions 文档](https://developers.openai.com/plugins/build/extensions) 编写。实际桌面证据来自用户观察与日志对应；未分别确认 global 和 thread 两个入口。

本轮使用[本地 marketplace](https://developers.openai.com/plugins/build/plugins)和[本机 stdio MCP](https://learn.chatgpt.com/docs/extend/mcp)。[公网域名要求](https://developers.openai.com/plugins/deploy/app-review)对应远程 MCP 的公开审核，不能作为本地插件启动与界面展示的前提；本地测试成功也不证明已经符合公开目录上架要求。

隔离脚本显式禁用其他 MCP 服务器。最初的空配置表尝试会合并个人配置，只触发了工具/资源清单读取；未调用现有 Team Cross 工具，也未启动真实模型轮次。后续原生与模型检查确认只有探针连接，其他服务器为 disabled。[当前 MCP 实现](../../../../internal/mcp/server.go) 仅在 `tools/call` 进入 Core。

## 证据保存位置

探针、可复现脚本和 JSON/JSONL 证据已保留在独立的 Documents 目录，不使用 `/tmp`；该目录已初始化独立 Git 仓库，分支 `codex/local-plugin-probe`，尚未提交。证据仅在本机保存，没有上传。

- [操作说明](</Users/wsy/Documents/Codex/2026-10-01/teamcross-local-plugin-probe/RUNBOOK.txt>)。
- [直接协议检查](</Users/wsy/Documents/Codex/2026-10-01/teamcross-local-plugin-probe/evidence/stdio-check.json>)。
- [原生默认配置检查](</Users/wsy/Documents/Codex/2026-10-01/teamcross-local-plugin-probe/evidence/native-default.json>) 与 [实验开关检查](</Users/wsy/Documents/Codex/2026-10-01/teamcross-local-plugin-probe/evidence/native-apps-enabled.json>)。
- [默认配置模型检查](</Users/wsy/Documents/Codex/2026-10-01/teamcross-local-plugin-probe/evidence/plugin-check.json>) 与 [实验开关模型检查](</Users/wsy/Documents/Codex/2026-10-01/teamcross-local-plugin-probe/evidence/plugin-apps-enabled-check.json>)。
- [CLI 安装往返](</Users/wsy/Documents/Codex/2026-10-01/teamcross-local-plugin-probe/evidence/cli-install-check.json>)、[当前桌面安装](</Users/wsy/Documents/Codex/2026-10-01/teamcross-local-plugin-probe/evidence/desktop-install-state.json>)、[假数据日志](</Users/wsy/Documents/Codex/2026-10-01/teamcross-local-plugin-probe/evidence/probe.jsonl>)。
- [基础桌面实际观察](</Users/wsy/Documents/Codex/2026-10-01/teamcross-local-plugin-probe/evidence/desktop-ui-check.json>)：run `c83a1299-d470-4f7f-ab2b-a434fb7ddeb4`，按钮编号 `2be5f232-21f0-4f21-aad6-a4385e1da8c4`。该编号区别于各脚本使用的带前缀编号。

## 隔离 Team Cross Core

没有修改产品源码。通过外部 Go overlay 添加本次 fixture，使用当前真实 `collab.App`、HTTP handler、材料存储和批注持久化；原生来源历史由既有 `fakeRuntime` 合成，不读取用户会话，也不运行协作模型。独立 CLI 从本次提交构建，显式使用 fixture 数据目录。

- Core 数据目录：[隔离 fixture](</Users/wsy/Documents/Codex/2026-10-01/teamcross-local-plugin-probe/core-fixture-1>)，只创建一个只读空间、一份材料和一条原批注。
- Core 监听 `127.0.0.1:49241`；没有启用分享 listener、邀请或共同执行。
- 第二个本地插件 `teamcross-core-probe` 版本 `0.1.0` 通过 stdio 调用现有 Team Cross MCP 的 `read_material`、`read_context` 和 `reply_to_annotation`，增加 MCP Apps HTML 资源。该适配器是独立验证源码，尚未并入产品。
- 适配器和其 MCP 子进程以默认禁止网络、仅允许 `localhost:49241` 出站的沙箱启动；Core 端口可连接，其他回环端口 canary 被拒绝。界面 CSP 不允许外部连接或资源。

| Core 检查 | 结果 |
| --- | --- |
| 现有 stdio MCP 读取真实材料与批注 | 通过，材料标记 `LOCAL-CORE-20261001` |
| 回复保存与重复请求 | 通过，`status=saved`；同一 `requestId` 返回同一个 reply ID，磁盘上仅一条 |
| 原生 app-server 调用适配器 | 读取、回复、资源字节和隔离检查均通过 |
| 已安装插件缓存启动与读取 | 通过，确认插件 ID、缓存目录、资源与来源一致 |
| 输入与执行边界 | `forks=0`，没有 ExecutionRecord；原生 fixture 调用只有 `thread/read` 与 `thread/turns/list`，没有 `turn/*` 或 `thread/fork` |
| 第二个面板实际桌面按钮 | 未确认；25 分钟观察窗口内未记录桌面进程启动或按钮调用 |

证据：[stdio Core](</Users/wsy/Documents/Codex/2026-10-01/teamcross-local-plugin-probe/evidence/core-stdio-check.json>)、[原生 Core](</Users/wsy/Documents/Codex/2026-10-01/teamcross-local-plugin-probe/evidence/core-native-check.json>)、[已安装 Core 插件](</Users/wsy/Documents/Codex/2026-10-01/teamcross-local-plugin-probe/evidence/core-plugin-check.json>)、[网络沙箱](</Users/wsy/Documents/Codex/2026-10-01/teamcross-local-plugin-probe/evidence/core-network-policy-check.json>)、[Core 插件日志](</Users/wsy/Documents/Codex/2026-10-01/teamcross-local-plugin-probe/evidence/core-probe.jsonl>)。私有 `connection.json` 中的本机控制 token 不进入文档或这些证据。

## 桌面门槛与接续

Computer Use 返回禁止操作 `com.openai.codex` 的安全限制；可访问的 MCP Apps 后端只能操作当前聊天里已经打开的面板，检查时列表为空。没有用其他 UI 技术绕过限制。

曾为人工观察留下具名的本地 marketplace 和两个探针；已在收尾时撤销，只移除本任务配置，其他个人设置与 marketplace 保留。自动检查启动的 app-server 和其子进程已结束；残留的三个桌面探针已按 PID、父进程、Python 实际可执行文件和缓存工作目录核对后停止。桌面自己的 app-server 未终止。

基础页面已通过。Core 面板的具名两步观察请求未获得回复；隔离 Core 于北京时间 19:37 按 25 分钟超时保护退出，承载 fixture 的 `go test` 返回 1（`isolated Core fixture expired`），不能记为该 Go 命令通过。退出后独立审计确认两个脚本回复仍在磁盘、ID 与最后快照一致、没有共同执行或 Provider 输入，Core PID 已退出。

补验时创建新 fixture，再打开“Team Cross 本机材料与批注”，依次点击“读取测试材料”和“保存测试回复”；检查材料标记及“本机回复已保存，requestId 匹配”，对应日志和持久化记录。本次证据没有把原生 RPC 调用替代为实际桌面按钮证据。

本轮结论限定于这台 Mac 的本机 Work/Codex 环境。现有协作数据、普通 Chat、网页/云端 Work、两个入口分别展示、两台 Mac 的 LAN/Tailcat 协作和公开分发均未在本轮验证。无需 Team Cross 云端后端或公网入站不等于 ChatGPT 模型离线，也不构成材料永不进入模型上下文的承诺。

收尾证据：[Core 退出后审计](</Users/wsy/Documents/Codex/2026-10-01/teamcross-local-plugin-probe/evidence/core-final-audit.json>)、[探针进程清理](</Users/wsy/Documents/Codex/2026-10-01/teamcross-local-plugin-probe/evidence/desktop-process-cleanup.json>)、[具名卸载](</Users/wsy/Documents/Codex/2026-10-01/teamcross-local-plugin-probe/evidence/desktop-uninstall-state.json>)。本次卸载只移除具名配置，其他 marketplace 列表相同；最终配置哈希与更早的 CLI 往返起点不同，未回滚期间的其他配置内容。

探针源码、证据、隔离数据及用户已有会话均保留。Computer Use 限制使本轮无法代用户关闭主窗口中的探针面板；其后端进程与安装已经撤销。按 [任务记录规则](../../README.md#任务记录) 归档本轮记录。
