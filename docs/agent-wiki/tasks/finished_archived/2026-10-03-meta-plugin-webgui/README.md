# Meta：完整 WebGUI 作为本机插件界面

日期：2026-10-03。分支：`codex/meta-plugin-webgui`，从本地 `Meta` 的 `fed1e0aaac2851feb1f2ff798dd998cfdac30648` 新建。工程与浏览器检查针对该基点加本提交的改动执行；运行时产物的资源 hash 见 [verification.json](verification.json)。

环境：macOS 14.8.5 / arm64、Go 1.27.1、Node 24.18.0、ChatGPT 内置 `codex-cli 0.159.0-alpha.12.1`。

## 本次范围

按用户修正后的方案，插件使用完整 WebGUI；两种构建从同一个 `packages/web/src/main.tsx` 进入同一个 `App`。没有另写 Panel、插件专用 CSS 或材料/批注页面。已有业务组件与配对流程保留，新增宿主环境、API、存储及表单提交适配；既有读取入口在宿主支持时增加“带回当前对话”。Composer mentions 后续接入。

本次同时带入原本机插件包生命周期、Go MCP Apps 资源和历史探针来源。历史 2026-10-01 / 10-02 桌面回执仍只证明当时的探针，不升级为本提交的桌面验收。当前产品约束见[本机插件决策](../../../sources/decisions/chatgpt-local-plugin.md)。

## 通过的检查

- `go test ./...`、`go vet ./...`。
- `go test -race`：`collab`、`mcp`、`pluginpack`、`service`、`nativecodex`、`nativeclaude`、`sharing`、`cliinstall`。
- Web check、139 项测试、普通及插件双构建；两份嵌入资源同步。
- 隔离 CLI 配置下 export/install/status/upgrade/remove；绑定目录和其他配置保留。
- 同一个隔离安装经真实原生 app-server 加载：仅启用本插件，资源 hash 与构建一致，`read_selection` 能读取明确的版本 1。没有发送模型输入。
- 独立浏览器使用 opaque iframe，只有 `allow-scripts`，`connect-src 'none'`。真实 Go stdio、Core HTTP、存储、材料版本、原文批注与回复；原生来源历史为合成数据。
- 完整 App 导航：空间首页与详情、资源库、设置、创建、加入；加入页对专用无效邀请返回预期错误，未连接远端。
- 现有回复表单保存成功，并从 Core 持久记录中核对 requestId、回复 ID 和正文。配对表单创建等待目标后明确移除，没有把“等待”当作配对完成。
- 1440、1024、768 CSS 像素下无页面水平溢出；查看浅色、深色及中英文截图。
- 当前会话动作只发送一次宿主消息，附带所选材料的版本 1。宿主消息回执由测试页面模拟；未知回执与禁止自动重放由交互测试覆盖。
- 普通浏览器 WebGUI 对照检查：原有 HTTP 路径、资源库阅读、设置和配对页面正常；宿主专属动作不会出现在普通页面。
- fixture 未建立执行 fork，未发送共享模型输入。包含英文截图复核的三轮 fixture 都完成持久化断言并关闭；专用浏览器、宿主和 stdio 进程已结束，原生 app-server 退出码为 0。

浏览器测试发现并修复了严格 iframe 禁止原生表单导航导致提交事件未送达 React 的问题；适配仅转交原有处理器，保留表单验证。也修复了相对输出目录不能作为隔离 CLI 配置目录的问题，并检查宿主返回 `isError` 时不会报告投递成功。

## 保留证据与边界

[插件内的完整资源库](plugin-reading-1440.png)与[普通浏览器中的资源库](browser-webgui-reading-1440.png)共用同一页面；[768 像素深色](plugin-reading-768-dark.png)和[英文设置](plugin-settings-en-1440.png)展示原有响应式和语言功能。详细断言、回执 ID 和资源 hash 见 [verification.json](verification.json)。较大的临时日志与其他截图保留在本工作树的 `output/playwright/`，不作为唯一的长期证据。

未运行新的模型回合、两台 Mac LAN/Tailcat 或云端 ChatGPT 验证。真实桌面 global/thread 入口、页面实际摆放和当前会话消息投递仍待最后一次成品检查；按用户要求没有逐步操作或请求操作 Codex 主窗口。原生 MCP 加载成功与独立浏览器成功不替代这一步。
