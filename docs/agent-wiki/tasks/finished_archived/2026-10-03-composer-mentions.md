# Composer mentions 成品接入记录

日期：2026-10-03。分支：`codex/meta-plugin-webgui`。产品提交：`9c1c3789791af7b37c0067fcee63de9b2c95b977`；工程检查覆盖该提交的工作树内容，提交后从该提交重建并升级现有测试插件。Meta 仍为 `fed1e0aaac2851feb1f2ff798dd998cfdac30648`。

## 本次交付

桌面输入框通过 `teamcross_search_mentions` 搜索材料版本与原批注，结果为可读取的 ResourceLink。沿用现有 Core、资源库权限和材料/批注分页，不改 WebGUI 页面，不改配对或共享执行协议。固定规范为 OpenAI MCP Extensions `ca16cb3bc015baaa1b849082d8755bbef18770cb` 的 Composer At-Mentions。当前契约见 [本机插件](../../sources/decisions/chatgpt-local-plugin.md#输入框引用) 与 [协议](../../sources/protocol.md#个人资源库)。

## 已执行的检查

- `go test ./...`、`go vet ./...` 通过。
- 验证契约列出的 8 个包全部通过 `go test -race`。
- Web check 核对 784 条翻译；Web 测试 12 个文件、139 项全部通过；浏览器与插件两种 production build 通过。
- 首次 Web 测试与 Go/race 并行运行时出现 11 个超时和 1 个选区断言失败。其他检查结束后使用 `--maxWorkers=1` 复跑全部 139 项通过；没有增加测试超时，也没有修改相关前端代码。两套嵌入产物与原提交一致。
- 新回归覆盖空查询、中文和完整批注文字、全部材料版本、20 项上限、稳定排序、本机语言、成员撤销、撤回、同源/不同 Core、规范 URI 和非法参数拒绝。搜索不改变资源库状态或创建执行。共享 listener 和通用/共享 MCP 不开放新搜索入口。
- `verify-chatgpt-plugin.py` 在隔离配置中完成 export/install/status/upgrade/remove，检查其他配置保留。真实 stdio 和 Codex 原生 app-server 均发现 app-only 的 `mentions/search` 元数据，并完成材料 v1、v2 与指定批注的资源读取、批注回复分页。
- 在真实 Core fixture 上先读取另一份已发布材料，再撤回它：新搜索不再返回材料，原 URI 读取返回错误；不同 Core URI 同样被拒绝。
- 中英文用户说明和来源文档的相对链接、Python 脚本语法及 `git diff --check` 通过。

## 关键证据

| 检查 | 结果 |
| --- | --- |
| 搜索“连接池 v1”，读取“连接池调查 · v1” | 正文包含 `PLUGIN-V1-20261002`，不包含 v2、其他空间或未发布标记 |
| 搜索“连接池 v2”，读取“连接池调查（补充复测） · v2” | 引用版本为 2，正文包含 `PLUGIN-V2-20261002` |
| 搜索原批注标记 | 精确读取 `ANNOTATION-SELECTED-20261002`，`nextRead` 继续读取同一批注回复 |
| 空查询与未发布标记查询 | 分别返回 20 项、0 项 |
| 真实 fixture 的结束检查 | `forks=0`、`executionEnabled=false`、没有 `turn/*` 或 `thread/fork` 调用 |
| 已安装源复核 | 原生加载成功、上述 mention 检查通过、嵌入 HTML 与构建相同，测试 app-server 退出码 0 |

验证环境：macOS 14.8.5、arm64、Go 1.27.1、Node 24.18.0、Codex CLI `0.159.0-alpha.12.1`。本次未发送真实模型输入。

临时原始报告位于 `/private/tmp/teamcross-mentions-20261003-ADQmli/acceptance-boundaries/acceptance.json` 和同级 `installed-native/native-plugin-check.json`；此处已保留关键结果，不依赖临时文件作为唯一长期证据。

## 本机安装与收尾

已升级原有 `teamcross@teamcross-local`，安装源为 `~/Library/Application Support/TeamCross/plugins/chatgpt-local`，版本仍为 `0.2.1-dev`。Core 数据绑定保留为 `~/Documents/Codex/2026-10-02/teamcross-plugin-product/demo-state`。升级前后 `~/.codex/config.toml` 字节相同。

安装运行文件 SHA-256：`9132d1702a07e73a843a7fbfd468b65e0d79bc169225782ba407f9d709942190`。

嵌入 HTML SHA-256：`9db514209521745c5acb447cba079900a84395133b59192acb996cb86069d2f3`。

原演示 Core 仍运行旧提交，核对精确进程与 `active=0` 后，通过不带 force 的正常 stop 替换为新提交；保留演示数据和原来运行状态供最终桌面验收。没有停止主 ChatGPT 进程或其他 Team Cross Core。临时 fixture 写入 finish 后通过最终断言并退出；独立 stdio/app-server 已关闭，临时验证路径下无剩余测试进程。

## 尚需一次桌面验收

本次未操作 Codex 主窗口，也未模拟输入框展示成功。结束当前任务后，完全退出并重新打开 ChatGPT。在支持此扩展的桌面输入框中输入 `@`，进入 Team Cross 搜索，选择“连接池调查 · v1”或指定批注，填写自己的分析请求再发送。核对引用标签、实际上下文和结果。协议、安装和原生资源读取成功不能单独证明这一 UI 步骤。没有额外测试窗口需要逐步确认。

当前安装仍使用既有独立演示 Core；本次不将其改绑到用户日常 Team Cross 数据。普通 ChatGPT 网页、手机端以及两台 Mac 的 LAN/Tailcat 网络均不在本次新增验收范围内。
