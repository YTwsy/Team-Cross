# ChatGPT 本机 WebGUI 插件

## 范围与接入

Team Cross 的本机插件为 ChatGPT for Mac 的本机 Work/Codex 提供完整的现有 WebGUI。个人客户端通过 `teamcross mcp --ui` 的 stdio 读取嵌入式 MCP Apps 资源；MCP 进程使用现有 Go Backend 与认证的 loopback Core。面板不直接请求 Core，不需要公网入站、HTTPS 托管或 Team Cross 云端账号。远端协作者仍通过原有 LAN / Tailcat 连接本机 Core；本机插件没有改变这些网络条件。

这是本机客户端的分发与使用方式。普通网页 ChatGPT、云端 Work、公开插件目录提交和两台 Mac 的网络可用性分别验收，不从本机成功推断。官方入口见 [插件打包](https://developers.openai.com/plugins/build/plugins)、[MCP Apps UI](https://developers.openai.com/plugins/build/chatgpt-ui) 与 [扩展入口](https://developers.openai.com/plugins/build/extensions)。公开目录的远端端点要求不作为本机插件的架构前提。

## 包和生命周期

稳定 CLI 入口为 `teamcross plugin export|install|upgrade|status|remove`。默认源位于 `~/Library/Application Support/TeamCross/plugins/local`；可用 `--plugin-dir` 选择独立源，`--data-dir` 绑定独立 Core。更新已有包时，未显式传入 `--data-dir` 则保留原绑定。包复制当前 CLI 至自己的 `runtime/teamcross`，清单使用这个稳定绝对路径；无需额外 Python 或 Node 运行时，客户端缓存复制不会改变启动路径。

当前输出使用官方支持的 `.codex-plugin/plugin.json` 与 `.mcp.json` 兼容格式。安装检查须实际列出插件 MCP 并读取构建资源，而非只确认清单可见。将来迁移格式要重新执行对应客户端版本的原生加载门槛；具体版本与结果写入任务记录。

市场名为 `teamcross-local`，插件 ID 为 `teamcross@teamcross-local`，MCP 服务名为 `teamcross-ui`。升级只接受原来源和这个精确 ID；当前 CLI 没有 `plugin upgrade` 子命令，Team Cross 通过核对后的 remove/add 刷新自身缓存。其他 MCP、插件和来源配置保留。已有非 Team Cross 目录、同名异源市场及包内符号链接会被拒绝。卸载移除自身插件及来源注册，保留包、本机 Core 数据和材料；不删除会话或协作目录。

`upgrade` 更新磁盘上的包与安装缓存，不替换 ChatGPT 已运行的 stdio 进程或已打开的面板。更新后按[官方本机插件流程](https://developers.openai.com/plugins/build/plugins#install-a-local-plugin-manually)完全退出并重新打开 ChatGPT，再检查侧栏入口；新会话加载成功不能证明旧侧栏已经刷新。重启由用户在当前任务结束后进行，升级命令不结束 ChatGPT 或其他会话。

## 一套 WebGUI，两种运行环境

浏览器版与插件版都从 `packages/web/src/main.tsx` 启动，挂载相同的 `App`，加载相同的 `styles.css` 与 `library.css`。插件包含完整的导航、空间、创建与加入、材料阅读、原文批注、资源库、设置及接收会话配对页面，不维护另一套面板或插件专用布局。后续 WebGUI 页面与组件的修改同时进入两种构建；新增 API 需要同步检查宿主桥的路由声明。

插件环境还将原有 React 表单的点击提交交给相同的 `onSubmit`，保留必填验证，避免依赖 iframe 的原生表单导航权限；不改表单布局与业务处理器。

插件构建只替换 `environment.ts`：先完成 MCP Apps 握手，再通过 `setAPITransport` 把原来的 API 调用转成宿主 `tools/call`。Go 侧 `teamcross_ui_read` / `teamcross_ui_write` 只向 app 可见，按明确的 WebGUI 路由及读写类型访问本机 Core，原样返回 WebGUI 数据结构。Core 继续验证成员权限、输入归属和请求编号；业务写入由现有界面的明确操作触发。不会将 Core 控制、接收通道轮询、原生调用者身份或任意 URL 暴露为 UI 路由。面向模型的工具仍限定于查询、材料/引用读取和批注回复。

[空间工作台](space-workbench.md) 的请求、简报、参与会话和可选专用会话使用同一套页面。宿主桥明确声明这些页面所需的读写路由，包括本机独立接收会话的状态和人工审批；内部投递的 poll/claim、接收身份与任意原生 RPC 仍不开放。新增页面接口时须同步路由声明和读写隔离回归，避免普通 WebGUI 可用而插件报错。

`open_teamcross` 声明 global / thread 入口，资源声明 fullscreen，以容纳完整 WebGUI。入口、显示模式、`ui/message` 和上下文能力遵循 [OpenAI MCP Extensions 规范](https://github.com/openai/mcp-extensions/blob/900032d8bd7c1566202d0cb1666986584f932043/docs/spec.md)，实际显示与投递仍需对应桌面版本验收。自包含 HTML 将页面脚本、样式和语法高亮一并打包；iframe 不请求 localhost，也不需要放宽 Core 的来源检查或向浏览器交付 Core token。

语言继续来自 Core 的本机界面语言偏好。主题沿用 WebGUI 的系统、浅色、深色设置；浏览器保存在原 localStorage，插件沙盒通过薄存储适配保存在当前面板的 private widget state。页面路由与投递状态同样绑定 Core scope；它们不会自动成为模型上下文。无 widget state 时仅保留当前 iframe 的内存，不能承诺跨新会话或重载持久化。正文、批注草稿和回复逻辑沿用现有 WebGUI，不另设插件专用编辑器或草稿恢复契约。

## 带给当前个人 Agent

沿用资源库、材料和批注的选择操作及底部“生成读取入口”，最多 32 项，材料包含具体版本。生成入口调用现有 `library/bundles`；编号与权限语义遵守 [资源库契约](../protocol.md)。原有“交给 Agent”仍通过 Meta 的会话配对工作，插件不改变其接收能力范围。

宿主支持消息时，在同一个读取入口旁增加“带回当前对话”。用户点击后，桥在宿主支持时更新所选引用上下文，再发送一次 `ui/message`，提示当前个人 Agent 按 `read_selection` 读取具体编号与分页。它不向共享原生会话发送输入，不自动回复批注；消息回执也不等于 Agent 已读取或完成分析。宿主不支持消息时，原有复制读取提示仍可使用。

消息投递前保存结果未知状态，确认后标记已发送；结果不明时查看当前会话，不自动重放该编号的消息。上下文更新阶段失败而尚未发出消息时，可以明确重试。从 ChatGPT 原生消息选区采集片段仍属于后续宿主接入。

## 输入框引用

Composer mentions 直接接入宿主的原生输入框，不增加插件专用搜索界面。用户搜索空间名、材料标题、作者、版本或批注文字，选中某一材料版本或原批注后，将引用加入尚未发送的消息。搜索与选择只读，不改资源库勾选，不生成读取编号，不启动分析、回复或共同执行；用户发送消息时的处理要求由用户填写。

工具、ResourceLink 与 `resources/read` 遵循固定版本的 [OpenAI Composer At-Mentions 规范](https://github.com/openai/mcp-extensions/blob/ca16cb3bc015baaa1b849082d8755bbef18770cb/docs/spec.md#composer-at-mentions)。该规范将入口支持限定在桌面；不把本机 Work/Codex 的接入推广到普通 ChatGPT 网页、Web Work 或手机端。实际输入框渲染与选中引用仍需对应桌面版本验收。

搜索复用资源库引用和权限判断，包含已发布的全部版本及可见批注，不搜索私有会话或材料正文。空查询提供最近内容，结果最多 20 项，更多内容通过关键词缩小范围。材料标题带明确版本；批注引用绑定原批注，回复读取时更新。引用绑定安装包所选 Core，具体读取再次验证访问；撤回、离线或撤销成员权限后，不以缓存正文或搜索结果兜底。协议字段和分页规则维护于 [资源库接口](../protocol.md#个人资源库)。

## 实现与验证入口

- [Go UI MCP](../../../../internal/mcp/ui.go)、[stdio 协议](../../../../internal/mcp/server.go)、[嵌入资源](../../../../internal/mcpassets/embed.go)。
- [输入框搜索与资源读取](../../../../internal/mcp/mentions.go)、[资源库检索](../../../../internal/collab/mentions.go)。
- [本地包管理](../../../../internal/pluginpack/plugin.go)、[CLI](../../../../cmd/teamcross/plugin.go)。
- [共同入口](../../../../packages/web/src/main.tsx)、[完整 WebGUI](../../../../packages/web/src/App.tsx)、[插件环境](../../../../packages/web/src/plugin/environment.ts)、[宿主桥](../../../../packages/web/src/plugin/bridge.ts)、[自包含构建](../../../../packages/web/vite.plugin.config.ts)。
- 通过标准由 [本机插件验证门槛](../validation/test-gates.md#chatgpt-本机插件) 维护，具体版本结果保存在任务记录。
