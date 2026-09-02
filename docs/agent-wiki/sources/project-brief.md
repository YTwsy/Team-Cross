---
title: Team Cross 项目简报
kind: source
status: seed
---

# Team Cross 项目简报

Team Cross 是一个 macOS-first、本地优先的开发上下文交接原型。它服务于两位都能访问
同一 Git 仓库的协作者：主机把当前代码基线、未提交改动、交接说明、evidence 和 Agent
运行状态组织成一个 Thread；接收者通过自己的本地 join proxy 查看、批注，并在取得控制
租约后驱动主机托管的 Agent。

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

- **Thread**：一次可持续、多轮的交接上下文；有 baseline commit 时拥有一个隔离
  worktree，unborn repository 中则保持只读。
- **Round**：不可变快照。Round 0 来自初始 capture；完成的 Agent Turn 或 Provider
  切换会追加新 Round。
- **Evidence**：仓库外或难以由 Git 表达的文本、日志、文件和 imported transcript。
- **Agent Run**：Team Cross 管理的 Provider Session，只能在 Thread worktree 中工作。
- **Share**：绑定单一 Thread 的临时远端协作入口，带独立 listener、secret、证书和
  到期时间。
- **Participant / Lease / Command**：多人观察、单 Controller 控制和可持久化重放的
  fencing 模型。

## 当前事实来源

- 产品范围与用户流程：`README.md`
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
