---
title: Team Cross 项目简报
kind: source
status: seed
---

# Team Cross 项目简报

## 产品核心

> Team Cross 让团队在不改变既有工作区和工作流的前提下，安全地分享、审阅、临时移交或继续一个 coding-agent Session。

Team Cross 与团队 Workspace、任务分派和仓库组织正交。它从具体 Agent Session 出发，
增加共同视图、来源记录、可定位批注、受限控制和可追溯接力，不替代用户熟悉的 Codex、
Claude Code 等原生 Agent UI。交互遵循“双界面，单 Writer”：原生 UI 是个人执行界面，
Team Cross 是共享协作界面，同一个可写 Run 同一时刻只能有一个输入来源。

产品判断规则是：

> 一个很实用的产品判断规则：它是否从一个具体 Session 出发，并帮助另一个人理解、审阅、控制或继续这次工作？

核心要解决的是 Session 上下文散落、工作被困在发起者机器和界面、审阅意见无法绑定准确
状态、临时控制缺少安全边界，以及 `Resume Session`、`Continue from Round`、Provider
switch 和 `Fork Thread` 被含糊地统称为“继续”等问题。

## 当前原型

当前实现是 macOS-first、本地优先的开发上下文交接原型。Session-first 入口把历史
捕获成只读 SessionSnapshot/Thread，支持选择分享、精准批注与 Markdown 反馈，不启动
Agent，也不要求原生工作目录仍存在。另一条 Git capture 路径保存代码基线与改动，
允许 Owner 显式从封存 Round 创建新的 managed Session 或 Fork 独立 Thread。

接收者通过本地 join proxy 查看和批注；只有 Share 另外授权控制并取得租约时才能驱动
主机 managed Agent。离线交接包可以自包含 Git 基线与所选上下文，接收后 Fork 新
Thread，不要求继续连接来源仓库。原生 Follow/Open/同会话接管仍不可用。

它不是聊天工具、Git/PR/Issue 替代品、远程 Shell 或完整环境复制器。v0 的核心价值是：
在不自动修改原工作区的前提下，完成“我卡在这里 → 你获得必要上下文 → 你留下可定位
反馈或驱动 Agent → 我继续工作”的闭环。

## 当前系统形态

- `cmd/teamcross` 提供 `serve`、`join`、`doctor` 和 `version`。
- Go Core 负责 Git capture、隔离 worktree、SQLite/CAS、Thread/Round/Event、Share、
  transport、HTTP REST 和 SSE。
- React + TypeScript + Vite WebGUI 是主机与接收者的主要控制面。
- Node 24 Agent Bridge 通过 JSONL-RPC stdio 统一 Codex app-server、Claude Agent SDK
  和 Mock Adapter。
- macOS 持久化数据默认位于
  `~/Library/Application Support/Team Cross/`。

## 核心模型

- **Agent Session**：Provider 拥有的连续对话身份；`Native Session` 来自原生 UI/CLI，
  `Managed Session` 由 Team Cross 通过 Adapter 创建。
- **SessionSnapshot**：带来源、捕获时间、稳定 entry ID 和缺失标记的不可变只读历史。
- **Thread**：Team Cross 拥有的持久、追加式协作单元，可以按顺序连接多个 Session；有
  baseline commit 时拥有一个隔离 worktree，unborn repository 中则保持只读。
- **Agent Run**：一个 Session 在某台 host、execution root 和权限边界中的一次实际运行
  绑定。当前 v0 只创建和控制 managed Run。
- **Turn / Event**：Turn 是一次输入触发的 Agent 工作过程；Event 是其中消息、工具、文件
  和控制变化等细粒度事实。
- **Round**：有明确来源的不可变交接快照，而不是对话轮次。Round 0 来自初始 capture、
  Session 审阅或离线导入；
  完成或中断的 Agent Turn、Provider 切换等边界可以追加新 Round。
- **Evidence**：仓库外或难以由 Git 表达的文本、日志、文件和 imported transcript。
- **Share**：绑定单一 Thread 的临时远端协作入口，带独立 listener、secret、证书和
  到期时间。
- **Participant / Lease / Command**：多人观察、单 Controller 控制和可持久化重放的
  fencing 模型。

完整产品边界、动作语义和词汇定义见 `product-core-and-glossary.md`。

## 当前事实来源

- 产品范围与用户流程：`README.md`
- 产品核心与统一词汇：`docs/agent-wiki/sources/product-core-and-glossary.md`
- Agent 入口与不变量：`AGENTS.md`
- wire/API 规范：`docs/protocol.md`
- CLI 与进程生命周期：`cmd/teamcross/main.go`
- Thread、Agent 与 Share HTTP 行为：`internal/server/`
- Git capture、worktree 和 patch：`internal/gitstate/`
- SQLite schema 与持久化：`internal/storage/`
- 邀请与 transport：`internal/invite/`、`internal/share/`、`internal/transport/`
- Bridge 与 Provider Adapter：`packages/agent-bridge/src/`
- Web 控制面：`packages/web/src/`
- 自动化验证：各 Go `*_test.go`、`packages/web/src/test/` 和
  `packages/agent-bridge/test/`

## 维护规则

当产品目标、核心模型、系统形态或事实来源移动时更新本简报。实验性设想如果尚未进入
实现，不应写成当前能力。
