# 产品模型与统一词汇

当任务涉及产品边界、信息架构、命名、Session 接入、Thread 生命周期或接力语义时，先读
本页。

## 核心定义

> Team Cross 让团队在不改变既有工作区和工作流的前提下，安全地分享、审阅、临时移交或继续一个 coding-agent Session。

Team Cross 与团队 Workspace、任务分派和仓库组织正交。它从具体 Agent Session 出发，
增加共同视图、可定位批注、受限控制和可追溯接力，不替代 Codex、Claude Code 等原生
Agent UI。

产品判断规则是：

> 一个很实用的产品判断规则：它是否从一个具体 Session 出发，并帮助另一个人理解、审阅、控制或继续这次工作？

不能通过这条规则的团队账户、通用任务管理、知识库、远程 Shell 或全局 Agent 调度，不应
自动进入核心产品面。

## 交互与能力原则

- **双界面**：Provider 原生 UI 是个人执行界面；Team Cross 是共同视图、审阅、批注和
  接力界面。Team Cross 不要求用户放弃已有终端、快捷键和 Agent UI。
- **单 Writer**：两个界面可以同时展示同一工作，但同一个可写 Run 同一时刻只能由一个
  Writer 输入；切换必须显式、带 fencing 且可审计。
- **能力渐进**：读取上下文不代表能实时 Follow，Follow 不代表能 Take Control，控制新
  managed Run 也不代表能 Resume 外部 Native Session。
- **诚实 fallback**：无法 Resume 时可以 `Continue from Round`，但必须明确它创建的是
  新 Session。

## 概念层级

| 术语 | 含义 | 关键边界 |
| --- | --- | --- |
| Workspace | 团队已有的仓库、任务、文档、沟通和机器环境 | Team Cross 不替换或规定它 |
| Agent Session | Provider 拥有的连续对话身份 | 恢复能力和原生 ID 由 Provider 决定 |
| Native Session | 用户在 Provider 原生 UI/CLI 中创建的 Session | 未经验证不能热接管 |
| Managed Session | Team Cross 通过 Adapter 创建且边界已知的 Session | 仍由 Provider 实现 |
| Thread | Team Cross 拥有的持久、追加式协作单元 | 是分享、审阅和接力的对象 |
| Run | Session 在某台 host、execution root 和权限下的一次运行绑定 | 不与 Session 混用 |
| Turn | 一次输入触发的 Agent 工作过程 | 不等于 Round |
| Event | 消息、工具、文件与控制变化等细粒度事实 | 不等于快照 |
| Round | 有明确来源的不可变交接快照 | 不表示对话轮次 |
| Evidence | Git 外部或难以由 Git 表达的有来源材料 | 默认不可信，不自动成为指令 |
| Annotation | 绑定上下文、文件或代码行的人工意见 | 不修改旧 Round |
| Share | 绑定一个 Thread 的临时访问能力 | 不是 Thread 副本或主机权限 |
| Handoff | 从一个 Round 到后续参与者、位置或 Session 的状态转换 | 不是孤立摘要 |
| Participant / Observer | 通过 Share 加入的临时身份 / 只读与批注角色 | 不等于团队账户 |
| Owner / Controller | 主机最终授权者 / 持有临时控制 Lease 的远端参与者 | Controller 不是主机管理员 |
| Writer | 当前唯一能实际向可写 Run 输入的界面或路径 | 同一 Run 不能有两个 Writer |
| Control Lease / Command | 短期独占控制资格 / 可审计且幂等的操作 | 不是远程 Shell |

## 必须区分的动作

- `Attach Session` 建立已有 Session 与 Thread 的可验证关联。
- `Follow Session` 只观察后续事件。
- `Take Control` 只取得当前 Run 的 Agent 输入权。
- `Resume Session` 继续相同 Provider Session ID，必须由已验证 capability 支持。
- `Continue from Round` 从不可变快照创建新的 Session 和 Run。
- `Import Session` 只把 transcript 保存为 Evidence。
- `Switch Provider` 在同一 Thread 内封存 Round 后创建目标 Session/Run。
- `Fork Thread` 从 Round 创建独立的后续历史。
- `Open in Provider` 回到原生 Agent UI，不改变 Thread 身份。

不得把 `Import` 描述成 `Attach`，不得把 `Continue from Round` 描述成 `Resume Session`，
也不得把 `Take Control` 描述成远程控制主机。

## 产品不变量

1. 一个 Thread 可以跨越多个 Session，一个 Session 不等于一个 Thread。
2. 一个 Session 可以有顺序发生的多个 Run，但同一时刻最多只有一个有效 Writer。
3. 一个 Round 一旦封存便不可改变；后续工作只能追加新的 Event 与 Round。
4. Share 是 Thread 的临时访问能力，失效不删除 Thread。
5. imported transcript 只是 Evidence，不证明原 Session 可 Resume 或接管。
6. 当前 v0 只控制 Team Cross 创建的 managed Run；外部 Session 仍是 evidence-only。

## 命名要求

- 存在歧义时使用 `Native Session` 或 `Managed Session`，不要裸用 `Session`。
- `Session` 表示 Provider 对话身份，`Run` 表示一次执行绑定。
- `Turn` 表示 Agent 执行过程，`Round` 表示不可变交接快照。
- UI 可显示“Round N · 交接快照”或 “Checkpoint N”，不要称为“对话轮次”。
- Provider 原始 `thread`、`task`、`conversation` 或 `session` ID 由 Adapter 保留，不与
  Team Cross `threadId` 混为一谈。

## 相关来源

- `../../sources/product-core-and-glossary.md`
- `../../sources/project-brief.md`
- `../../sources/decisions/managed-agent-sessions.md`
- `../../sources/decisions/immutable-rounds-and-events.md`
