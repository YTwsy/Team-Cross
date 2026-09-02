# Agent Bridge 与 Provider 切换

当任务涉及 Codex、Claude、Mock、Bridge RPC、native Session、sandbox、input request 或
handoff 时，先读本页。

## 统一 contract

Go Core 只依赖 JSONL-RPC 方法与统一 event：

- session：`sessions.listStored`、`sessions.readStored`
- run：`runs.create`、`runs.importContext`、`runs.send`、`runs.steer`、
  `runs.interrupt`、`runs.respondInput`、`runs.close`
- event：Run/Turn lifecycle、message delta/completed、tool lifecycle、file changed、
  input requested/resolved、run error

Provider-specific protocol、通知形状和进程生命周期留在 Adapter 中。

## managed Session

只有由 Team Cross 创建、worktree 和权限边界已知的 Session 才能接收远端控制。Codex
app-server 与 Claude SDK 都运行在主机账户和配额下；远端参与者没有自己的 Provider
身份，也不能改变模型、授权或工具网络。

外部 transcript 可以读取后保存为 evidence，但其中的文本、网页和工具记录都是参考材料，
不能自动成为操作指令。

## Codex

Codex Adapter 启动 `codex app-server --stdio`，先 initialize，再做 capability probe。
不要把实验性 WebSocket 暴露到 Share network。workspace write 只能包含 Thread worktree，
approval policy 保持 `never`。

## Claude

Claude Adapter 使用长生命周期 Streaming Input client。工具 allowlist 和原生 sandbox 必须
同时成立；sandbox 不可用时 fail closed。模型 credential 可以供 SDK 调用，但 Bash tool
子进程环境必须移除它。

Claude 没有与 Codex 完全相同的 steer 原语，因此 Team Cross 的 steer 语义是：interrupt
当前响应，然后在同一 managed Session 发送改向消息。UI 和 event 不应把它描述成 token
级别原地改写。

## Provider 切换顺序

同一 Thread 的切换由写入栅栏串行化；栅栏期间拒绝新的 send、steer、interrupt、input、
import 和第二次 switch。

1. 确认旧 Run 不处于 `running`、`waiting` 或 `starting`；否则要求 Owner 先完成或
   interrupt。
2. 请求 outgoing Agent 生成结构化 handoff summary，并等待该 Turn 的
   `turn.completed`。
3. 若 summary 失败或超时，interrupt 该 Turn，并基于最新 worktree 生成确定性 fallback。
4. 捕获当前 patch、完整事件范围、文件与 evidence，生成尚未发布的 context manifest。
5. 在同一 worktree 预创建目标 Provider Session，但不把它设为当前 Run，也不发送 prompt。
6. 持久化 outgoing immutable Round，并安全发布 manifest；这是 commit point。
7. 把旧 Run 逻辑冻结为只读历史，把目标 Run 设为当前 Run。
8. 立即持久化 `agent.switched`。
9. best-effort 关闭旧 Provider Session；底层关闭失败不能让旧 Run 重新获得写权限。
10. 第一条输入提供 summary、文件清单、未决问题和 context manifest path。

summary 失败时使用确定性 manifest。Core 不分析完整 transcript 后自行宣称“最重要”的
语义内容。commit point 之前目标创建、Round 或 manifest 失败时丢弃目标 Run，旧 Run继续
作为当前 Run；commit 之后首条 prompt 失败时不回滚到旧 Provider。

Round commit 绑定带唯一后缀的 manifest，目标 Agent 直接读取该路径。固定的
`contexts/<thread-id>.json` 只是便于人工查看的 best-effort 别名。

## Bridge 生命周期

`bridge.ping` 是无副作用探活。启动阶段失败可以重启一次，因为尚未发送业务 RPC；一旦
Run 已建立，Bridge 崩溃不能自动创建新 Session 并重放旧命令，否则可能造成重复工具动作
和分叉 Provider 状态。

## 相关来源

- `../../sources/decisions/managed-agent-sessions.md`
- `packages/agent-bridge/README.md`
- `packages/agent-bridge/src/`
- `internal/bridgeclient/`
- `internal/server/agent.go`
- `internal/server/rounds.go`
