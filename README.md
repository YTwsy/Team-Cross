# Team Cross

## 产品方向

> Team Cross 让团队在不改变既有工作区和工作流的前提下，安全地分享、审阅、临时移交或继续一个 coding-agent Session。

Team Cross 不替团队选择工作区、任务系统或仓库组织方式。它从不同工作流最终都会触达的
Agent Session 入手，为具体的一次工作增加共同视图、可定位批注、受限控制和可追溯接力，
同时让 Codex、Claude Code 等原生 UI 继续作为用户熟悉的个人执行界面。

一个很实用的产品判断规则：

> 它是否从一个具体 Session 出发，并帮助另一个人理解、审阅、控制或继续这次工作？

当前仓库实现的是 macOS-first、本地优先的 v0 原型：它捕获真实的 Git 基线与工作区改动，
在隔离 worktree 中运行 Team Cross 托管的 Codex 或 Claude Session，并向另一位协作者提供
受限的查看、批注和 Agent 控制能力。外部 Native Session 当前仍只能作为有来源、
不可信的 evidence 导入，尚不能被原地恢复或热接管。

原始 checkout 永远不会被 Team Cross 自动应用 patch、提交或 cherry-pick。

## 原型工作流

`capture → 隔离 worktree → Share → join → 批注或控制 Agent → 新 Round → patch export`

完整的产品闭环、技术分层和实时协作时序见
[产品与架构图解](docs/architecture.md)。

浏览器不会接触 Share secret，也不会直接连接 Tailcat。`teamcross join` 在本地
loopback proxy 中保管 secret、私网 TLS 连接、SPKI pin 和当前选中的 transport。

## 环境要求

- macOS 13 或更高版本
- Go 1.27
- Node.js 24
- pnpm 11
- Git
- 使用 Codex Adapter 时需要 Codex CLI
- 使用 Claude Adapter 时需要 `ANTHROPIC_API_KEY` 或 Claude Agent SDK
  支持的云提供商凭据

Tailscale 是可选依赖。LocalAPI 停止、未登录或不可访问都是正常降级条件。
Go Core 精确锁定 Tailcat v0.4.0，因为其 API 和 wire format 尚未承诺稳定。

## 构建

```sh
pnpm install
pnpm --filter @teamcross/agent-bridge build
pnpm --filter @teamcross/web build
go build -o bin/teamcross ./cmd/teamcross
```

也可以使用统一入口：

```sh
make build
```

WebGUI 会直接构建到 `internal/webassets/dist`，再由 Go 二进制嵌入。开发阶段有意
保留 Go 与 Node 两个运行时：发布原型可以内嵌 WebGUI，但托管 Agent 仍通过本地
Node Agent Bridge 运行。

## 启动主机

```sh
./bin/teamcross serve --repo .
```

管理员 API 只监听 loopback。WebGUI 当前可以：

- 预览并捕获 staged、unstaged、binary 与用户选定的 untracked 改动；
- 为 Thread 创建一个 detached 隔离 worktree；
- 启动 Mock、Codex 或 Claude managed Session，并在 Provider 之间切换；
- 通过可重放 SSE 展示消息、工具事件、文件变化与运行状态；
- 展示 Agent input request，并允许 Owner 或当前 Controller 回答；
- 查看和导出相对 Thread 基线的当前 binary patch；
- 附加、查看和下载文本、日志或文件证据；
- 创建普通批注或精确到文件行号的批注；
- 创建或撤销默认一小时有效的 Share。

前端开发模式：

```sh
pnpm --filter @teamcross/web dev
go run ./cmd/teamcross serve --repo . --dev-web http://127.0.0.1:5173 --no-open
```

## 加入 Share

从主机 WebGUI 复制 `tcx1.…` 邀请，在第二台 Mac 上运行：

```sh
./bin/teamcross join 'tcx1.…'
```

接收端按固定顺序执行连接阶段：

1. **LAN**：邀请内嵌的 RFC1918 地址与约 750 ms 的 mDNS browse 并发参与，
   整个阶段预算 1.5 秒。
2. **Tailnet**：仅当接收端自己的 Tailscale LocalAPI 也处于 Running 时尝试，
   阶段预算 3 秒。
3. **Tailcat**：使用临时 direct/DERP 路径，阶段预算 12 秒。

TCP 建立后仍必须通过邀请中的 Ed25519 证书 SPKI pin 和 Share handshake。普通网络
失败会进入下一阶段；已经命中正确指纹的主机若返回过期、撤销、凭据错误或协议不兼容，
则立即停止。

一次连接成功后 transport 保持固定。连接断开时，本地 proxy 才会重新从 LAN 开始选择，
浏览器的 SSE 则从最后一个事件序号继续。

## 多人协作模型

- 同一个 Share 可以有多个 Observer，他们都能查看和批注。
- 同时只有一个远端参与者可以持有 60 秒 Controller 租约。
- WebGUI 每 20 秒续约一次 Controller 租约。
- Agent 写命令携带 `commandId`、`expectedRevision` 和 `leaseEpoch`；SQLite
  持久化幂等结果，并拒绝陈旧 revision 或 fencing token。
- 主机 Owner 始终可以撤销 Share 或抢占远端租约。
- 远端不能枚举其他 Thread、查看主机路径、切换 Provider、导入旧 Session、改变
  工具网络设置、创建 Share，或附加主机本地证据。

## Agent 边界

Node Bridge 通过 stdio 上的 JSONL-RPC 与 Go Core 通信。Codex app-server 只是本地
子进程，不会暴露到 LAN、Tailnet 或 Tailcat。

Codex Run 使用主机现有的 Codex 登录、`approvalPolicy: never`，并把
`workspaceWrite` 限定到 Thread worktree。Claude 使用 TypeScript Agent SDK 的
Streaming Input 模式、`dontAsk`、显式工具 allowlist、强制原生 sandbox，并从 Bash
子进程环境中移除模型凭据。两个 Provider 的工具网络默认关闭，只能由主机在创建 Run
时选择开启。

每个完成的 managed Agent Turn 都会封存新的不可变 Round，包含事件范围、当前 patch、
文件清单和 evidence 引用。Provider 切换还会保存 outgoing semantic summary；如果生成
失败，则使用确定性 manifest 兜底。目标 Provider 在同一 worktree 中创建全新的 managed
Session，并读取 context manifest。导入的原生 Session 只作为带来源的、不可信 evidence，
Team Cross 不恢复或热接管外部 Session ID。

## 数据目录

macOS 上的持久化状态位于：

```text
~/Library/Application Support/Team Cross/
├── teamcross.db
├── objects/sha256/<哈希前两位>/<其余哈希>
├── worktrees/<thread-id>/…
└── contexts/
    ├── <thread-id>-<unique>.json  # Round 绑定、目标 Agent 实际读取的版本
    └── <thread-id>.json           # 便于查看的 best-effort 别名
```

SQLite 启用 WAL、foreign keys、busy timeout、单调事件序号、不可变 Round 和带 fencing
的控制租约。大 payload 按 SHA-256 在对象存储中去重。untracked capture 的上限为单文件
5 MiB、单 Round 20 MiB；超限内容只保留元数据。

主机重启会主动使所有临时 Share runtime 失效，但 Thread、Round、worktree、evidence、
annotation 和 event 会继续保留。

## 诊断与测试

```sh
./bin/teamcross doctor
go test ./...
pnpm test
```

`doctor` 检查 Git、SQLite、Node/Bridge、真实 Codex app-server initialize handshake、
Claude 凭据、Tailscale LocalAPI 和临时 Tailcat 初始化。缺少 Tailscale 或 Claude 凭据
属于可选能力告警，不会阻止 Core 启动。

真实 Codex/Claude Turn 和公共 Tailcat DERP 测试必须显式 opt-in，因为它们会消耗主机
凭据、配额或公共网络资源。普通自动化测试使用 Mock Adapter 与可注入连接实现。

当前自动化与本机双进程验证不等于两台真实 Mac、真实 Tailnet ACL、强制 DERP relay
或真实 Provider Turn 已经通过。完整证据边界见
[Agent Wiki 验证门槛](docs/agent-wiki/wiki/concepts/validation-gates.md)。

## 文档

- [Agent 开发指南](AGENTS.md)：coding agent 进入仓库后的第一站。
- [产品模型与统一词汇](docs/agent-wiki/wiki/concepts/product-model-and-glossary.md)：
  产品核心、问题边界以及 Session、Thread、Run、Turn、Round 等词汇的规范定义。
- [产品与架构图解](docs/architecture.md)：产品闭环、技术分层与实时协作时序。
- [协议 v1](docs/protocol.md)：邀请、Share API、幂等和 SSE wire contract。
- [Agent Wiki](docs/agent-wiki/README.md)：稳定决策、事实来源和任务上下文。
- [Agent Bridge](packages/agent-bridge/README.md)：本地 Bridge 的组件边界与调试入口。

本仓库的 Markdown 文档默认使用简体中文；协议字段、API、命令、代码标识符和固定
wire value 保持英文原样。

## v0 暂不实现

当前原型不提供账户、长期身份、云端 rendezvous、外部活跃 Session 的 Attach、实时
Follow、Resume 或热接管、原生 Session 跨 cwd 迁移、自动 apply/commit/cherry-pick、
完整环境复刻、离线 `.tcx` bundle、MCP/Skill 包装或菜单栏 App。
