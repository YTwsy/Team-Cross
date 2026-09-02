# Agent Wiki 索引

这个 Wiki 是 Team Cross 面向 coding agent 的工程上下文层。它从 `../sources/` 整理稳定
知识，并指向仍然作为最终事实来源的代码、测试与协议文档。

当任务涉及 Git 隔离、Share transport、多人控制、Agent Adapter 或验收结论时，从这里
开始。

## 核心概念

- [运行时架构](concepts/runtime-architecture.md)：Go Core、React WebGUI、Node Bridge、
  SQLite/CAS 和本地进程的职责边界。
- [Git capture 与隔离](concepts/git-capture-and-isolation.md)：baseline、dirty snapshot、
  Thread worktree 和 binary patch 的完整生命周期。
- [Share 与连接选择](concepts/share-and-transport.md)：临时 Share runtime、`tcx1` 邀请、
  LAN → Tailnet → Tailcat 和 join proxy。
- [协作与控制](concepts/collaboration-and-control.md)：Observer、Controller、Owner、
  lease、revision、command fencing 与远端 API 边界。
- [Agent Bridge 与切换](concepts/agent-bridge-and-switching.md)：Codex、Claude、Mock、
  managed Session、imported evidence 和跨 Provider handoff。
- [验证门槛](concepts/validation-gates.md)：自动化、本机闭环、两台 Mac 和真实 Provider
  分别能够证明什么。

## 项目形态

Team Cross 当前是 CLI + 本地 WebGUI：主机运行 `teamcross serve --repo .`，接收者运行
`teamcross join <invite>`。Go Core 管理 durable state 与 network transport；浏览器只访问
本机 loopback；Provider 进程通过 stdio Bridge 工作。它不是远程 Shell、聊天系统、Git
托管平台或云协作服务。

Thread 是长期追加式档案，Round 是不可变交接快照，Event 是细粒度实时记录，Evidence
承载 Git 之外的参考材料。有 baseline commit 的 Thread 最多对应一个隔离 worktree；
unborn Thread 保持只读。所有 Agent 输出都相对同一 baseline 导出，原始 checkout 不被
自动修改。

## 事实来源

- 用户范围与操作流程：`README.md`
- 产品闭环与架构时序图：`docs/architecture.md`
- Agent 入口和硬边界：`AGENTS.md`
- Invitation/Share API：`docs/protocol.md`
- CLI 与主进程：`cmd/teamcross/main.go`
- Core HTTP/Thread/Share/Agent：`internal/server/`
- Git：`internal/gitstate/`
- SQLite/CAS：`internal/storage/`
- Invitation 与 transport：`internal/invite/`、`internal/share/`、`internal/transport/`
- Agent Bridge：`packages/agent-bridge/src/`
- WebGUI：`packages/web/src/`
- 验证契约：`../sources/validation/test-gates.md`

当这些事实来源的变化会影响未来 Agent 判断时，应在同一提交中更新对应 source 与
concept。
