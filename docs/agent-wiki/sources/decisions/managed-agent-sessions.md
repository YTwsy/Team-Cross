---
title: Team Cross 托管 Agent Session 与 Run
kind: decision
status: accepted
---

# Team Cross 托管 Agent Session 与 Run

## 决策

Team Cross 只控制绑定到自己所创建 managed Session 的 Run。Session 是 Provider 拥有的
连续对话身份，Run 是该 Session 在已知 worktree、主机和权限边界中的实际运行绑定。Go
Core 通过本地 Node Agent Bridge 统一调用 Codex app-server、Claude Agent SDK 或 Mock
Adapter；Provider 进程不会直接暴露给远端网络。

已有 Codex/Claude transcript 只作为带来源、不可信的 evidence 导入。Team Cross 会在
隔离 worktree 中创建新的 managed Session 和对应 Run，不原地 resume、不保留外部 native
Session ID，也不热接管外部活跃进程。

## 原因

外部 Session 的 cwd、授权、sandbox、进程状态和原生协议生命周期不受 Team Cross
控制，因此无法建立边界已知的可写 Run。把 transcript 当作参考材料，可以保留推理背景，
同时避免把旧对话、网页内容或日志自动升级为操作指令。

统一 Bridge 让 WebGUI 和多人租约只面对一套稳定 RPC/event contract，Provider 差异
留在 Adapter 内部。

## Provider 约束

### Codex

- 本地启动 `codex app-server --stdio`。
- 使用主机现有 Codex 登录与 Provider 默认模型。
- `approvalPolicy: never`，`workspaceWrite` 只允许 Thread worktree。
- 运行前执行 initialize/capability probe，不依赖硬编码 CLI 版本号。

### Claude

- 使用 TypeScript Agent SDK Streaming Input。
- 需要 `ANTHROPIC_API_KEY` 或受支持的云提供商凭据，不复用 Claude.ai 订阅登录。
- 使用 `dontAsk`、显式工具 allowlist、原生 sandbox 与
  `allowUnsandboxedCommands: false`。
- Bash 子进程环境移除模型凭据。
- steer 定义为 interrupt 当前响应，再在同一 managed Session 中发送改向消息。

### 通用约束

- 工具网络默认关闭，由主机创建 Run 时决定。
- 同一 Thread 的 Provider 顺序共享一个 worktree，但不共享 native Session ID。
- Bridge 启动探活可以安全重试一次；业务 RPC 不自动重放。
- Bridge 崩溃后不能假定当前原生 Run 可以恢复。

## 跨 Agent 切换

切换只能由主机触发，并按 Thread 串行执行。`running`、`waiting` 或 `starting` Run 都被
视为活动状态，必须先完成或被主机 interrupt。切换采用 `prepare → commit → activate`
三阶段，而不是先销毁旧 Session：

1. **Prepare**：建立 Thread 写入栅栏，请求 outgoing Agent 生成结构化 summary，并等待
   对应 `turn.completed`；失败或超时后基于最新 worktree 生成确定性 fallback。随后捕获
   patch、事件范围和 evidence，准备 manifest，并预创建一个尚不可寻址的目标 Session。
2. **Commit**：持久化 outgoing immutable Round，并发布对应 context manifest。这个点
   之前失败会丢弃目标 Session，旧 Run 仍是当前 Run。
3. **Activate**：把旧 Run 逻辑冻结为只读历史，把目标 Run 设为当前 Run，立即持久化
   `agent.switched`；随后 best-effort 关闭旧 Provider Session，再向目标发送首条
   handoff prompt。

这样目标 Provider 初始化失败时不会先失去旧 Session，同时在任意时刻仍只有一个可接受
普通命令的 Run。commit 后首条 prompt 失败不会复活旧 Run；目标 Run 保持当前并记录失败，
由主机决定重试或再次切换。

Round commit 绑定带唯一后缀的 context manifest，目标 Agent 直接读取该路径。固定的
`contexts/<thread-id>.json` 只是便于人工查看的 best-effort 别名，发布失败不会使已经提交
的 Round 无效。

## 重新打开条件

只有 Provider 提供经过验证的跨进程、跨 cwd、可授权恢复协议，并且 Team Cross 能证明
其 sandbox 与原 Session 状态一致时，才重新讨论 native Session resume 或热接管。

## 事实来源

- `internal/bridgeclient/`
- `internal/server/agent.go`
- `internal/server/rounds.go`
- `packages/agent-bridge/src/`
- `packages/agent-bridge/test/`
