# Agent 开发指南

Team Cross 是一个 macOS-first、本地优先的开发上下文交接工具。它把 Git 基线与改动、
非 Git evidence、Agent 运行事件、人工批注和多人控制组合成可追溯的 Thread，同时坚持
原始 checkout 不被自动修改。

这个文件是 coding agent 进入仓库时的第一站。涉及 Thread 生命周期、Git capture、
Share transport、远端权限、Agent Adapter 或验收结论时，应先阅读这里，再进入对应的
Agent Wiki 页面。

## 先读这些

1. 读 `README.md`，了解面向用户的产品范围、运行方式和 v0 边界。
2. 读 `docs/agent-wiki/wiki/index.md`，获得面向 Agent 的项目导航。
3. 需要快速理解产品闭环、技术分层或实时协作时序时，读 `docs/architecture.md`。
4. 改 Git capture、worktree 或 patch export 时，读
   `docs/agent-wiki/wiki/concepts/git-capture-and-isolation.md`。
5. 改 Share、邀请或连接选择时，读
   `docs/agent-wiki/wiki/concepts/share-and-transport.md` 与 `docs/protocol.md`。
6. 改多人权限、租约或命令幂等时，读
   `docs/agent-wiki/wiki/concepts/collaboration-and-control.md`。
7. 改 Codex、Claude、Mock 或 Provider 切换时，读
   `docs/agent-wiki/wiki/concepts/agent-bridge-and-switching.md`。
8. 判断测试和验收结论时，读
   `docs/agent-wiki/wiki/concepts/validation-gates.md`。

## 产品与架构方向

- Go Core 负责 Git、SQLite/CAS、Thread/Round/Event、Share、transport、HTTP/SSE
  和 CLI。
- React WebGUI 是主机与接收者的主要控制面；写操作走 REST，事件流走 SSE。
- Node Agent Bridge 只通过 JSONL-RPC stdio 与 Go Core 通信，并封装 Codex
  app-server、Claude Agent SDK 和 Mock Adapter。
- `teamcross serve` 的管理员 API 只监听 loopback；远端只进入绑定单一 Thread 的
  Share API。
- 连接策略固定为 LAN → Tailnet → Tailcat。Tailscale 不可用是正常降级，不是 Core
  错误。
- v0 是 CLI + WebGUI，不要在普通任务中把产品方向改成 MCP、Skill、聊天工具或云服务。

## 不变量

- 不自动修改、apply、commit 或 cherry-pick 原始 checkout。
- 有 baseline commit 的 Thread 只拥有一个隔离 worktree；Codex 与 Claude 顺序共享它。
- Round 一旦创建便不可变。新的 Agent Turn、切换或回复应追加新 Round，而不是重写历史。
- 最终 patch 始终相对 Thread baseline 导出，并包含 tracked 与已捕获 untracked 内容。
- unborn repository 只能建立只读 Thread；出现首个 commit 前不得启动 managed Agent。
- 每个 Share 都有独立的临时证书、secret、listener 和到期时间；主机重启后必须重新分享。
- Share API 只能访问绑定 Thread，不能枚举其他 Thread、主机路径、设置或旧 Session。
- Observer 可以查看和批注；同时只有一个远端 Controller 可以 send、steer、interrupt
  或回答 input request。Owner 始终可以抢占。
- 远端写入必须保持 `commandId`、`expectedRevision` 与 `leaseEpoch` 的幂等和 fencing
  语义。
- 外部 Codex/Claude 历史只作为有来源、不可信的 evidence 导入；不原地 resume，不热接管
  原生 Session ID。
- Provider 的工具网络默认关闭。不要把模型 API 自身的网络与工具网络开关混为一谈。

## 连接与失败语义

- LAN 阶段同时使用邀请地址和短时间 mDNS browse；不能把 mDNS 作为唯一发现机制。
- Tailnet 只在接收端自己的 Tailscale LocalAPI 处于 Running 时参与。
- Tailcat 是最后一级临时 direct/DERP transport，`connBlob` 必须视为 opaque data。
- TCP 可达不代表连接成功；候选还必须通过 SPKI pin 和 Share handshake。
- timeout、refused、ACL 阻断继续下一级；正确指纹主机返回过期、撤销、401/403 或协议
  不兼容时立即停止。
- 连接成功后不做运行中热迁移。只有断线重连才重新从 LAN 开始，并从 SSE cursor 恢复。

## 验证

默认提交门槛：

```sh
go test ./...
go vet ./...
pnpm --filter @teamcross/web check
pnpm --filter @teamcross/web test
pnpm --filter @teamcross/web build
pnpm --filter @teamcross/agent-bridge check
pnpm --filter @teamcross/agent-bridge test
pnpm --filter @teamcross/agent-bridge build
```

修改共享状态、租约、Bridge 生命周期或并发事件写入时，还应运行相关 Go race test。

修改 `packages/web/src/` 后必须重新运行 Web 测试和 production build，并提交更新后的
`internal/webassets/dist/`。修改 Agent Bridge 后必须至少运行 typecheck、Vitest 与 Bridge
readiness probe。

Mock、本机两个进程或两个浏览器只能证明本机流程。没有实际运行对应环境时，不得宣称
两台 Mac、真实 Tailnet ACL、Tailcat DERP relay 或真实 Codex/Claude Turn 已验证。

如果沙箱阻止 Go cache 写入，把 `GOCACHE` 和 `GOMODCACHE` 指向 `/private/tmp` 下的
任务专用目录。

## 文档规则

- 面向用户与 Agent 的 Markdown 默认使用简体中文。
- 协议字段、HTTP 路由、命令、事件名、环境变量和代码标识符保持英文原样。
- `README.md` 负责用户认知，`docs/protocol.md` 负责 wire/API 规范，Agent Wiki 负责
  未来 Agent 的工程判断，代码和测试仍是最终事实来源。
- `docs/agent-wiki/sources/` 保存稳定事实、决策和验证契约；`wiki/` 保存短小、互链的
  任务上下文。
- 当功能改动改变长期判断时，在同一提交中同步更新对应 source 与 wiki concept。
- 一次性日志、临时命令输出、普通 TODO、未验证猜测和具体邀请 secret 不进入 Wiki。
