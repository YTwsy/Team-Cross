# Team Cross

## 产品方向

> Team Cross 让团队在不改变既有工作区和工作流的前提下，安全地分享、审阅、临时移交或继续一个 coding-agent Session。

Team Cross 不替团队选择工作区、任务系统或仓库组织方式。它从不同工作流最终都会触达的
Agent Session 入手，为具体的一次工作增加共同视图、可定位批注、受限控制和可追溯接力，
同时让 Codex、Claude Code 等原生 UI 继续作为用户熟悉的个人执行界面。

一个很实用的产品判断规则：

> 它是否从一个具体 Session 出发，并帮助另一个人理解、审阅、控制或继续这次工作？

当前仓库实现的是 macOS-first、本地优先的 v0 原型：可以从已有 Native Session 创建
只读审阅 Thread，选择内容分享、精准批注并导出反馈；也可以捕获 Git 基线，在隔离
worktree 中显式创建新的 managed Session，或从封存 Round 独立 Fork。外部 Native
Session 仍只作为有来源、不可信的上下文导入，尚不能被实时 Follow、原地恢复或接管。

原始 checkout 永远不会被 Team Cross 自动应用 patch、提交或 cherry-pick。

## 原型工作流

`选择 Session → 预览 → 只读导入 → 选择分享范围 → join → 批注 → Markdown 反馈`

读取、预览、导入和分享都不会创建 Run、发送模型指令或恢复原生会话。Session-first
入口不使用原生 cwd 自动抓取代码：没有经用户明确捕获的 Git 基线也能审阅，但这样的
只读 Thread 不能执行 Agent。当前代码状态不是历史 Session 当时的代码状态。

已有只读审阅需要继续时，可明确选择一个代码仓库，预览当前基线与选定文件，创建独立
后继 Thread。它携带已保存的 Session 快照和精确指向这些材料的只读反馈 Evidence，
不必再次读取原生历史；此步骤本身仍不启动 Agent。

`只读审阅 → 选择独立代码基线 → 预览并确认 → 后继 Thread → 确认新 Session → 隔离执行`

也可以先捕获 Git 创建可执行 Thread，再导入参考 Session：

`capture → 导入参考 Session → 选择 Round → 确认新 Session → 隔离执行 → 新 Round → patch export`

从历史 Round 继续会创建新 Thread；最新 Round 只有在 worktree 与封存状态一致时才能
原 Thread 顺序继续。若有后来修改，选择 Fork，不覆盖或回退当前 worktree。
继续起点在选择时固定，后台新 Round 不会悄悄替换已确认的起点。代码审阅显示封存 Round
的 diff，批注区分 old/new 侧；当前 worktree 的实时 diff 单独查看，不冒充历史快照。

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
- 选择 Codex/Claude 历史，预览结构化消息、工具结果与缺失标记，创建只读审阅 Thread；
- 将 Session 快照追加到已有 Thread，绑定消息、工具结果、Evidence 或 Round 批注，
  复制或下载带引用的 Markdown 反馈，由 Owner 自己带回原生 UI；
- 启动 Mock、Codex 或 Claude managed Session，并在 Provider 之间切换；
- 通过可重放 SSE 展示消息、工具事件、文件变化与运行状态；
- 展示 Agent input request，并允许 Owner 或当前 Controller 回答；
- 查看和导出相对 Thread 基线的当前 binary patch；
- 附加、查看和下载文本、日志或文件证据；
- 创建普通批注或精确到文件行号的批注；
- 选择快照记录、Evidence、封存代码与实时 managed 输出，创建或撤销默认一小时有效的 Share；
- 从 Round 显式创建新 Session，或 Fork 独立 Thread；
- 导出和导入版本化 `.tcx.json` 离线交接包；导入本身不执行 Agent。

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
- 新 Share 默认只有 `view`、`annotate`，控制需要 Owner 单独启用，且明确共享
  封存代码与 managed Agent 实时输出。已分享快照不会因后续导入、封存或本机编辑而扩大。
- 内容范围由服务端统一执行，覆盖列表、详情、Evidence 下载、patch、批注与 SSE。
  改变范围或权限需要撤销旧 Share 并重新创建；隐藏内容不能通过其他路由绕过。
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

Adapter 分别报告读取、Follow、准确打开原生会话、Resume 和接管能力。读取历史成功或
Provider 提供 `thread/resume` 都不意味着单 Writer 已被证明；未验证能力保持关闭。
CLI/Desktop 的独立验收要求见
[原生能力门槛](docs/agent-wiki/sources/validation/native-capability-gates.md)。

只读 Follow 已有增量读取、持久游标、停止保护与 WebGUI 状态面板，但当前原生来源尚未
通过专用 CLI/Desktop 验收，因此入口保持禁用并显示原因。Follow 不创建 Run，不取得
输入权，也不会扩大已有 Share。原生实时分享已有独立范围授权：明确选择当前窗口和
未来内容类别，按不可变窗口保留旧批注；默认关闭，仍受真实 Follow 能力门槛限制。
准确打开 Codex Desktop 的入口同样受能力门控，202 只代表系统接收请求，不代表原生
会话已经验收。原生同会话接管与交还仍未交付。

## 离线交接

有 Git 基线的 Thread 可以在 WebGUI 选择 Round 和允许导出的上下文，确认后下载
`.tcx.json`。接收者在自己的主机导入，得到带来源 Thread/Round 引用的新 Thread 和
独立 Git 对象库。包接收完成后不再依赖发送方在线；继续工作使用接收者自己的 Provider
凭据，且必须再次显式创建新 Session。

交接包包含 baseline 可达 Git 历史与所选代码快照，不是仅含当前 diff；选择导出的
Evidence/Session 快照也会成为不可撤回的副本。分享撤销不会删除接收者已保存的数据。
包不转移凭据、Share secret、租约、进程或本机配置；双方不会自动同步或合并后续历史。

v1 有大小与路径限制，不支持 submodule、包含父目录跳转的 symlink 或完整环境复刻。
详细范围与失败语义见
[审阅与接力契约](docs/agent-wiki/sources/decisions/session-review-and-continuation.md)。

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

数据库通过版本化迁移保留旧 Thread/Round，新增不可变 SessionSnapshot、批注锚点、
Share 内容投影、Run 来源/Writer/能力绑定、只读 Follow 状态及不可变公开窗口。
schema v3 保留 v0/v1/v2 历史；升级前应备份数据目录，不支持降级打开新 schema。

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
- [Session 审阅与接力](docs/agent-wiki/wiki/concepts/session-review-and-continuation.md)：
  只读导入、范围分享、新 Session、Fork 和离线包的实现边界。

本仓库的 Markdown 文档默认使用简体中文；协议字段、API、命令、代码标识符和固定
wire value 保持英文原样。

## v0 暂不实现

当前原型不提供账户、长期身份、云端 rendezvous、外部活跃 Session 的 Attach、实时
Follow、准确的 Open in Provider、Resume 或热接管、原生 Session 跨 cwd 迁移、
自动 apply/commit/cherry-pick、完整环境复刻、同 Thread 离线双主同步、
MCP/Skill 包装或菜单栏 App。M3/M4 原生控制仍待真实能力验证，不能用本机 Mock 或
Fork 新 Session 代替同一原生会话接管验收。
