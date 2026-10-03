# Codex CLI 路径发现与设置体验修复

日期：2026-10-04。用户要求基于本机 `Meta` 新建分支修复路径发现，不引入由 Team Cross 管理 Codex 安装或更新的方案。

## 代码与环境

- 基线：本机 `Meta`，`ae66ff6906fada7fb480c9fa47873103ddc21768`。使用本机已有分支，不替换成当时落后的 `origin/Meta`。
- 实现提交：`def4ab0c96ca39b1b388946c525c285dd741f797`，分支 `codex/fix-codex-discovery`。工程和浏览器验证在提交前执行，产品代码及生成资源与该提交一致。验收脚本的按 ID 读取断言随后单独调整。
- 隔离 worktree：`/Users/wsy/.codex/worktrees/codex-discovery-fix/Team Cross`。原工作目录的 `codex/collaboration-flow` 及其中未提交工作保留。
- macOS 14.8.5 / 23J423，arm64；Go 1.27.1；Node 24.18.0；pnpm 11.19.0。
- 本机 `/Applications/ChatGPT.app`：`com.openai.codex`，版本 `26.930.31730`，build `12947`。
- 真实 CLI：`codex-cli 0.160.0`。测试二进制版本为 `0.2.6-dev.codex-discovery.20261004`，当时构建的 commit 字段为 `ae66ff6-dirty`，不将该字段当作已提交实现 SHA。

## 已确认的原因与修复

应用内旧路径 `Contents/Resources/codex` 已不存在，新路径为 `Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex`。本机保存的 CLI 配置仍指向旧路径。旧协作入口会立即拒绝这项失效配置，自动发现也只识别旧应用布局。插件接入的其他入口已分别支持新布局，导致同一产品的发现规则不一致。

旧设置页把检测结果回填到表单，普通保存可以把自动发现的 CLI、Desktop 或 Claude 路径固定下来；检测失败又把 CLI 表单值变成空白，隐藏了真正失效的保存值。没有据此推断用户具体在哪一次保存了该值。

修复统一已知应用布局，支持系统和用户 Applications、显式路径/命令、环境覆盖、常见命令目录及有时限的登录 Shell PATH 兜底。旧应用内路径只在同一应用里重新定位，普通自定义路径失效保持明确错误。设置页保留原始值，单独显示当前有效 CLI、版本、发现来源与恢复提示；自动模式保存仍保持空路径。重新检测刷新 Shell PATH 缓存且保留未保存的输入。旧 Core 未返回配置字段时禁止保存，避免把未知配置覆盖成空值。

长期规则见 [原生客户端决策](../../sources/decisions/native-clients-and-models.md#cli-发现与配置)，字段见 [协议](../../sources/protocol.md)。Codex 安装和更新继续由用户自己的安装渠道管理。

## 工程验证

| 检查 | 结果 |
| --- | --- |
| `go test ./...` | 通过，最终嵌入资源更新后重跑 |
| `go vet ./...` | 通过 |
| `go test -race ./internal/nativecodex ./internal/collab ./internal/mcp ./internal/pluginpack` | 通过 |
| Web `check` | 通过，937 项中英文消息检查 |
| Web `test` | 15 个文件、155 个测试通过 |
| Web `build` | 两个生产构建通过；更新 `internal/webassets/dist` 与 `internal/mcpassets/dist` |
| `go build` | 本机 arm64 CLI 通过 |
| 文档相对链接与 `git diff --check` | 通过 |

Vite 仍报告主 chunk 超过 500 kB 的体积提示；构建成功，本任务不做无关拆包。回归覆盖新旧应用布局、同应用迁移、有效自定义路径保持优先、失效路径/环境覆盖不切换安装、权限检查、无效 symlink、相对 PATH 排除、命令名的 Shell 兜底、缓存刷新、表单不固定检测结果、失效路径可见、未保存输入保留、丢失保存响应不重放及中英文界面。

## 真实浏览器

用当前构建启动独立 Core，数据和原生配置位于该 worktree 的 `output/codex-discovery/browser-fixture/`；不操作用户正在运行的 43210 服务。Codex 路径指向本机真实应用；最初的 Claude 版本探针为测试脚本，不将其当作 Claude 原生接入验收。

1. 保存失效的旧应用内路径并指定同一 Desktop。API 同时返回原始旧值、新有效路径、`recoveredFrom` 与真实 `0.160.0` 版本；页面原样显示旧路径并提示自动恢复。
2. 点击恢复自动发现并保存，检查持久化 CLI 值为空。随后清空 Desktop 和 Claude 配置并保存；检测结果仍可用，三个保存值均为空。
3. 输入 `/missing/custom-codex` 后重新检测，确认未发出设置 POST，输入未覆盖。明确保存后显示“文件不存在”，原值仍可见；恢复自动发现后再次读取真实 CLI。
4. 检查 1440、1024、768 CSS 像素 × 深浅主题，实际查看六组截图；长路径换行，页面宽度等于视口，无水平溢出。768 像素英文页面及完整客户端面板另行截图复核，保存入口可用。
5. 键盘从重新检测 Tab 到自动发现；英文 DOM 语言与视口均正确。新页面控制台检查无错误、无警告。旧测试 Core 退出时旧页面曾产生连接拒绝日志，未将其混同为新页面产品错误。

浏览器原始截图和可重跑检查代码保留在本机该 worktree 的 `output/codex-discovery/`。目录被 Git 忽略；本报告永久保存检查动作、结果与边界，不以本机截图路径作为唯一证据。

## 真实 Codex CLI 与 TUI

在专用 Git 仓库、专用原生配置和两个同机 Core 上使用 `gpt-5.6-luna`。只通过专用配置中的认证引用使用现有登录，不恢复用户普通会话；没有控制 ChatGPT 主窗口。

首次运行 [verify-agent-cli.py](../../../../scripts/verify-agent-cli.py) 已完成真实来源回合和包含最终回复的新 fork，但停在批注回复断言：脚本从无 ID 的摘要目录寻找正文，现行协议要求按批注 ID 读取。临时副本增加 `annotationId` 与 `includeQuote:false` 后完整通过；随后把同样的一行参数修正放回正式脚本，并用新 fixture 重跑。该调整不改变产品读取协议。

正式脚本完整通过：`error` 与 `cleanupErrors` 为空，`currentTurnIncluded/cliJoinNoBrowser/mcpAnnotationsAndReply/inputRelay/directTUI/sameForkResume` 均为 true。

正式入口：

```sh
python3 scripts/verify-agent-cli.py \
  --provider codex \
  --fixture-dir '/absolute/worktree/output/codex-discovery/native-cli-final' \
  --teamcross-bin '/absolute/worktree/output/codex-discovery/teamcross' \
  --codex-bin '/Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex'
```

本次验收检查来源身份、最终回复进入 fork、CLI 无浏览器加入、批注回复按 ID 读取、非输入者拒绝、交接/交还/接回、真实直接 TUI 达到 `session_ready`、MCP 发送并读取 Luna 回复、重复请求、结束后同一 fork 恢复及测试仓库原文件保持不变。原始摘要保留在 `output/codex-discovery/native-cli-final/evidence/summary.json`；上面的执行步骤与结果在本报告留存。

## 结论边界与收尾

本次证明新布局 CLI 的发现、旧路径恢复、设置保存和真实 TUI/MCP 接入链；不据此推广专用 Desktop 全入口、原生登录/审批界面、文件工具执行、其他模型、Claude 原生协议、两台 Mac、Tailcat 或新安装包验收。尤其“CLI 可运行”与“专用 Desktop 已连接/已登录”分别判断，旧八组合报告不能证明新版 Desktop 已重验。

本次启动的测试 Core、真实 CLI/app-server、PTY 和命名 Playwright 浏览器关闭，按测试 PID 与数据目录核对；原有用户客户端和协作保留。测试数据、截图及隔离 worktree 保留供后续复核。没有构建或安装 DMG、推送、创建 PR、合并或发布。
