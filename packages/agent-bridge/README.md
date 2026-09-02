# Team Cross Agent Bridge

Agent Bridge 是由 Go Core 启动、只在本机运行的 Node.js 进程。它通过 stdin/stdout
传输 JSON-RPC 2.0，每一行是一条完整 JSON 消息。Provider 进程不会绑定网络 listener：

- Codex Adapter 把 `codex app-server --stdio` 作为子进程启动；
- Claude Adapter 为每个 managed Run 持有一个长生命周期 Streaming Input query；
- Mock Adapter 用于不消耗 Agent 账户的完整流程测试。

## 事件

Provider 的异步活动通过 JSON-RPC notification 发回 Go Core：

```json
{"jsonrpc":"2.0","method":"event","params":{"type":"message.delta","runId":"…"}}
```

统一事件包括 Run/Turn 状态、消息 delta、工具开始与完成、文件变化、input request
和错误。Go Core 会把这些事件写入 Thread 的 durable event log，再通过 SSE 提供给
WebGUI。

## RPC 表面

使用 `bridge.ping` 执行最小、无副作用的 readiness probe；使用 `bridge.info` 获取
完整 capability 列表。公开方法包括：

- `sessions.listStored`
- `sessions.readStored`
- `sessions.snapshot`
- `runs.create`
- `runs.importContext`
- `runs.send`
- `runs.steer`
- `runs.interrupt`
- `runs.respondInput`
- `runs.close`

Bridge 启动时必须先通过 protocol probe。探活失败可以安全重启一次，但不能自动重放
任何业务 RPC。运行中的 Bridge 如果崩溃，当前 managed Run 不会被假定为可恢复；原生
Provider Session 状态没有足够证据时必须 fail closed。

## 原生 Session 只读快照

`sessions.snapshot` 接受 `provider`、`sessionId`、可选 `cwd` 和 `limit`，只读取既有历史，
不创建 Run、Resume、Fork 或发送 Turn。返回 `source`、`capturedAt`、`entries`、
`truncated`、`warnings` 和 `capabilities`；Go Core 为结果分配持久快照 ID 并不可变保存。
`limit` 范围为 1–10000，默认 2000 条；单条文本最多约 64K 字符，总文本约 2M 字符。
未知、非文本、摘要化、未加载或超限内容有明确缺失标记，不把原始 JSON 当作完整回放。
输入历史始终是不可信 Evidence，不能自动升级为指令。

`entries.id` 由 Provider 身份、原生消息/工具 ID、Turn 和必要的重复项序号生成；缺少原生
ID 时使用内容派生值。批注仍必须绑定快照 ID，不能把当前历史变化原地写进旧快照。
Codex 的 Team Cross `sessionId` 对应 `thread.id`；`nativeIds.sessionId` 另存 Provider
的 Session tree root，二者不可混淆。`appServer` 来源并不能证明 Desktop 创建，故显示
`app-server`，不猜测为 `desktop`。

当前 `read` 在读取成功后为 true；`follow`、`open`、`resume`、`takeControl` 均为 false，
并附带未通过的验证边界。具体门槛见
[原生能力验收契约](../../docs/agent-wiki/sources/validation/native-capability-gates.md)。

可显式运行无 Session 副作用的安装版本协议检查：

```sh
TEAMCROSS_NATIVE_CAPABILITY_PROBE=1 pnpm --filter @teamcross/agent-bridge probe:native
```

该命令只查询版本、在临时目录生成协议定义，不连接现有 daemon、不枚举个人 Session、
不使用 Provider 配额；完成后清理自己的临时文件。它不替代 CLI/Desktop 原生会话验收。

## Provider 边界

`mock` 会在指定 worktree 中写入 `.teamcross-mock-output.txt`，用于验证
capture/share/diff/patch 全链路。

Claude 需要 `ANTHROPIC_API_KEY` 或受支持的云提供商凭据。SDK 使用 `dontAsk`、限定到
worktree 的显式 Edit/Write allowlist、强制原生 sandbox，并禁止 unsandboxed escape。
Bash tool 在启动子进程前会通过 SDK hook 移除模型凭据。

Codex 使用本机已有登录，通过 app-server initialize/capability probe 检查需要的方法；
运行时不依赖硬编码 CLI 版本号。workspace write root 只能是当前 Thread worktree。

## 开发

```sh
pnpm --filter @teamcross/agent-bridge build
pnpm --filter @teamcross/agent-bridge check
pnpm --filter @teamcross/agent-bridge test
```

Bridge 的协议类型和事件映射以 `src/protocol.ts`、`src/server.ts` 与各 Adapter 为事实
来源。若 RPC、Provider capability 或 sandbox 约束变化，应同步更新根目录
`AGENTS.md` 和 `docs/agent-wiki/` 中对应的稳定知识。
