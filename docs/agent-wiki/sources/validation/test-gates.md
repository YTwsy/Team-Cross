---
title: Team Cross 验证门槛
kind: validation
status: seed
---

# Team Cross 验证门槛

不同门槛证明的范围不同。结论必须明确说明实际运行了哪一层，不能用 Mock、本机进程或
初始化探活替代两台真实 Mac、真实 Provider Turn 或特定网络路径。

## 默认自动化门槛

Go Core：

```sh
go test ./...
go vet ./...
```

WebGUI：

```sh
pnpm --filter @teamcross/web check
pnpm --filter @teamcross/web test
pnpm --filter @teamcross/web build
```

Agent Bridge：

```sh
pnpm --filter @teamcross/agent-bridge check
pnpm --filter @teamcross/agent-bridge test
pnpm --filter @teamcross/agent-bridge build
```

这组门槛证明纯逻辑、Mock Adapter、事件映射、组件交互和 production build 在测试环境中
通过。它不证明主机凭据可用、两台 Mac 可达或公网 DERP relay 成功。

## 并发与生命周期门槛

修改 SQLite 写事务、SSE、租约、命令 fencing、Share 生命周期或 Bridge 进程管理时，
运行相关 race test，例如：

```sh
go test -race ./cmd/teamcross ./internal/bridgeclient ./internal/server ./internal/storage
```

这证明测试覆盖到的本机并发路径未触发 Go race detector；它不证明分布式时序在真实断网、
睡眠或换网条件下已经验收。

## 本机完整闭环

本机验收至少需要两个独立 data dir/进程，或一个主机与两个独立 join proxy，完成：

- 捕获真实 dirty Git 状态和选定 untracked 文件；
- 创建隔离 worktree，并确认原 checkout 未出现 Agent 文件；
- 运行 Mock Agent Turn、input request、interrupt 和 Round 封存；
- 创建 Share，并通过 join proxy 连接；
- 两位 participant 同时出现，只有一位获得 Controller；
- Controller 能发送 Agent 命令，Observer 仍能批注；
- 相同 `commandId` 重放不产生重复写入；
- SSE 使用 cursor 恢复；
- export patch 可在全新 baseline checkout 上 `git apply --binary`；
- 应用后的文件与 Thread worktree 一致。

这证明单机进程、API、浏览器和真实 Git 闭环。即使 Tailcat listener 预热成功，只要 join
最终选择的是 LAN，也不能宣称 Tailcat 数据路径或 DERP relay 已通过。

## 两台 Mac 网络验收

在宣称自动 fallback 可用于真实协作者前，至少覆盖：

- 同 LAN 且无 Tailscale，选择 LAN；
- mDNS 被阻断但邀请地址可达，仍选择 LAN；
- LAN 隔离且双方 Tailnet 可达，选择 Tailnet；
- Tailnet ACL 阻断或仅一端有 Tailscale，回退 Tailcat；
- Tailcat direct 不可达但 DERP 可用，仍成功，并取得底层明确的 relay 证据；
- 邀请过期或撤销后停止，不继续 fallback；
- 断线换网后重新择路，并从 SSE cursor 恢复。

每一项都要记录两端版本、实际选中 transport 和 handshake 结果。仅看到端口建立或
Tailcat 初始化不算通过。

## 真实 Provider 门槛

真实 Codex/Claude 测试必须 opt-in：

- Codex initialize probe 只证明 app-server 可启动并支持所需方法，不证明真实 Turn。
- Claude credential check 只证明配置存在，不证明 SDK Run 或 sandbox 行为。
- 真实 Turn 应在临时 repository/Thread 中运行，确认文件只能写入 Thread worktree、
  event 映射完整、interrupt/input/switch 符合 Provider 语义，并检查原 checkout 未变化。
- Claude 测试不得把 API key 传入 Bash tool 子进程。

## 当前初始原型的证据边界

截至 2026-09-03，首个提交候选树已经通过本文列出的默认自动化、关键 race test、本机
双 participant 闭环、真实 Codex app-server initialize 和 Tailcat ephemeral listener
初始化。由于验证时 Tailscale LocalAPI 未处于 Running 且未配置 Claude credential，尚
不能声称真实 Tailnet、强制 DERP relay、两台 Mac 或真实 Claude Turn 已验证。这个快照
只对应首个提交候选树；后续结论必须记录新的日期、提交或工作树身份与实际命令范围。

当这些状态变化时，更新本节或拆分新的带日期验证 source；不要把一次性完整日志写入 Wiki。
