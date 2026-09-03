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

已有 Codex/Claude transcript 只作为带来源、不可信的 SessionSnapshot/Evidence 导入。
读取、预览、导入和分享均不得创建 Run 或发送 prompt。只有 Owner 明确执行
`Continue from Round` 或 Provider switch 时，才在隔离 worktree 中创建新的 managed
Session 和对应 Run。来源 native Session ID 保留用于追溯，不被用来原地 resume 或
热接管外部活跃进程。

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
- managed Run 创建时允许真实 Session ID 尚未返回；未知值不能用 Run ID 或临时占位符
  冒充。Core 从创建响应或 `run.started` / `run.status` 采用第一个真实 ID，并原子更新
  持久 Run 与 binding；晚到的空值、旧占位符和重复事件不能回退身份。不同的真实 ID
  会被拒绝并封锁该 Run 后续命令，不能隐式切换 Session。身份补全保留 Continue/Fork
  来源与执行边界，也不会复活 archived/closed Run。
- 身份冲突可能发生在旧 Agent 仍执行时。对该 Run 执行同 worktree 的 switch 或
  Continue，必须先确认旧 Writer 关闭成功；关闭失败不得 prepare/create/send 新 Run。
  普通非冲突切换仍先 prepare；目标初始化失败不关闭旧 Run。初始化成功后，所有替换
  都必须在目标首条输入前确认旧 Writer 关闭，不能仅依赖当时是否已报告身份冲突。
  关闭失败则禁用从未 send 的目标 Run，保留旧 archived 引用供 Owner 显式重试关闭，
  不恢复旧输入权限，也不把未确认关闭的进程记录成已关闭。
- 激活时旧 Run 归档或 `agent.switched` / `agent.continued` 持久化失败，也必须进入相同
  补偿路径；不能在关闭栅栏之前留下可通过普通 send 访问的零输入目标。
- Adapter 从第一次 close 起永久封锁该 Run 的新输入，但错误/超时不报告 closed。
  Codex 必须等待准确 Turn 的 `turn/completed` 终态，中断 ACK 本身不算关闭；待返回的
  turn/start 身份和 steer 也须收口。未知 start 结果保持 fail-closed，不猜成 idle。
  Claude 先执行 SDK query close，再等待接收循环与可用的 iterator cleanup；关闭抛错
  或未结束保持可重试，不提前丢弃路由。两 Adapter 都合并并发 close，并保留有界超时。
  Codex 中断/终态的协议依据见 [官方 App Server 文档](https://learn.chatgpt.com/docs/app-server)。
- 延迟的历史 Run `turn.completed` 仍可记录为事件，但不得捕获当前 worktree 或追加
  Round；入队时与取得 Round 写入锁后都要确认它仍是当前 Writer。
- `sessions.snapshot` 与所有 `runs.*` 执行路径分离；能力标记遵循
  [原生能力门槛](../validation/native-capability-gates.md)。
- 从 Round 创建新 Session 与 Provider switch 是独立动作，前者只读取所选封存上下文，
  不向 outgoing Agent 请求总结。详见
  [审阅与接力](session-review-and-continuation.md)。

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
   `agent.switched`；随后确认旧 Provider Session 关闭成功，再向目标发送首条 handoff
   prompt。旧关闭失败时禁用零输入目标，保留旧 archived 引用供 Owner 显式重试。
   归档或激活审计事件持久化失败同样禁用零输入目标，不恢复旧输入权限。

这样目标 Provider 初始化失败时不会先失去旧 Session，同时在任意时刻仍只有一个可接受
普通命令的 Run。首条 prompt 已发送后的失败不会复活旧 Run；目标 Run 保持当前并记录
失败，由主机决定重试或再次切换。关闭旧 Writer 失败的回退仅恢复不可写的历史引用，
不清除 archived/identity conflict，也不自动执行任何 Agent 输入。

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
